package main

import (
	"bytes"
	"os"
	"reflect"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"

	"markstack/internal/session"
)

func TestProjectedCompositionBlocksCleanQuit(test *testing.T) {
	state, driver, filename, original, encode := openMenuSaveFixture(test, cancelCompositionCases[0], "bom-crlf")
	current := state.session.Current()
	if err := driver.SelectText(0, 1); err != nil {
		test.Fatal(err)
	}
	if err := driver.Compose("ni"); err != nil {
		test.Fatal(err)
	}
	if current.Dirty() || current.Composing() {
		test.Fatal("fixture is not a clean projected composition")
	}
	allowed := 0
	state.requestClose(func() { allowed++ })
	confirmation := state.session.Pending()
	if allowed != 0 || confirmation == nil || confirmation.Action != session.Quit || confirmation.Document != current {
		test.Fatalf("Quit bypassed projected composition: allowed=%d pending=%+v", allowed, confirmation)
	}
	if data, err := os.ReadFile(filename); err != nil || !bytes.Equal(data, encode(original)) {
		test.Fatalf("Quit wrote uncommitted input: %q / %v", data, err)
	}
}

func TestApplicationQuitPendingCompositionRequiresDecision(test *testing.T) {
	owners := append(append([]compositionCancelCase{}, cancelCompositionCases...), sourceKindCompositionCases...)
	for _, owner := range owners {
		for _, encoding := range []string{"lf", "bom-crlf"} {
			for _, dirty := range []bool{false, true} {
				for _, decision := range []session.Decision{session.Cancel, session.Discard} {
					name := owner.name + "/" + encoding + "/clean/cancel"
					if dirty {
						name = owner.name + "/" + encoding + "/dirty/cancel"
					}
					if decision == session.Discard {
						name += "-discard"
					}
					test.Run(name, func(context *testing.T) {
						state, driver, filename, original, encode := openMenuSaveFixture(context, owner, encoding)
						current := state.session.Current()
						if dirty {
							if err := driver.SelectText(0, 1); err != nil {
								context.Fatal(err)
							}
							if err := driver.Type("X"); err != nil {
								context.Fatal(err)
							}
						}
						baseline := current.Editor.CommittedString()
						if err := driver.SelectText(0, 1); err != nil {
							context.Fatal(err)
						}
						if err := driver.Compose("ni"); err != nil {
							context.Fatal(err)
						}
						input, source, mode := owner.editor(state), current.Editor, state.mode
						before := app.TextStateOf(input)
						recovery := state.session.RecoverySnapshot()
						allowed := 0
						state.requestClose(func() { allowed++ })
						if err := driver.Resize(1201, 780); err != nil {
							context.Fatal(err)
						}
						pending := state.session.Pending()
						if allowed != 0 || pending == nil || pending.Action != session.Quit || pending.Document != current {
							context.Fatalf("Quit missed composition: allowed=%d pending=%+v source-composing=%v", allowed, pending, current.Composing())
						}
						state.requestClose(func() { allowed += 100 })
						clickID(context, driver, saveConfirmID)
						if state.message != session.ErrCompositionPending.Error() || allowed != 0 || state.session.Pending() == nil || state.session.Pending().Document != current {
							context.Fatal("Save lost the pending Quit or failed to refuse preedit")
						}
						if current.Editor != source || owner.editor(state) != input || state.mode != mode || current.Editor.CommittedString() != baseline || app.TextStateOf(input) != before || !reflect.DeepEqual(state.session.RecoverySnapshot(), recovery) {
							context.Fatal("Quit/Save changed input/source/history/recovery identity")
						}
						control := cancelConfirmID
						if decision == session.Discard {
							control = discardConfirmID
						}
						clickID(context, driver, control)
						if state.session.Pending() != nil || state.allowClose != nil {
							context.Fatal("Quit decision did not finish the existing transition")
						}
						if decision == session.Cancel && (allowed != 0 || state.recoveryDiscarded) {
							context.Fatal("Cancel allowed Quit or discarded committed recovery")
						}
						if decision == session.Discard && (allowed != 1 || !state.recoveryDiscarded) {
							context.Fatalf("Discard did not invoke the original allow callback exactly once: %d", allowed)
						}
						if app.TextStateOf(input) != before || current.Editor.CommittedString() != baseline {
							context.Fatal("Quit decision mutated the model-owned input")
						}
						if data, err := os.ReadFile(filename); err != nil || !bytes.Equal(data, encode(original)) {
							context.Fatalf("Quit wrote source/preedit to disk: %q / %v", data, err)
						}
						if info, err := os.Stat(filename); err != nil || info.Mode().Perm() != 0640 {
							context.Fatal("Quit changed file permissions")
						}
						cancelCompositionFixture(context, state, owner)
						if dirty && !current.Editor.Undo() {
							context.Fatal("Quit consumed earlier source history")
						}
						if current.Text() != original || current.Dirty() {
							context.Fatal("original source/history did not survive Quit")
						}
					})
				}
			}
		}
	}
}

func TestApplicationQuitMixedDocumentsKeepsProjectedConfirmation(test *testing.T) {
	for _, owner := range cancelCompositionCases[:4] {
		for _, encoding := range []string{"lf", "bom-crlf"} {
			for _, cancel := range []bool{false, true} {
				name := owner.name + "/" + encoding + "/discard"
				if cancel {
					name = owner.name + "/" + encoding + "/cancel"
				}
				test.Run(name, func(context *testing.T) {
					state, driver, filename, original, encode := openMenuSaveFixture(context, owner, encoding)
					projected := state.session.Current()
					earlier := state.session.NewDocument()
					earlier.Editor.Insert("earlier committed draft")
					later := state.session.NewDocument()
					later.Editor.Insert("later committed draft")
					state.session.Tabs = []*session.Document{earlier, projected, later}
					state.session.Active = 1
					if err := driver.Resize(1201, 780); err != nil {
						context.Fatal(err)
					}
					if err := driver.SelectText(0, 1); err != nil {
						context.Fatal(err)
					}
					if err := driver.Compose("ni"); err != nil {
						context.Fatal(err)
					}
					allowed := 0
					state.requestClose(func() { allowed++ })
					if state.session.Pending() == nil || state.session.Pending().Document != earlier {
						context.Fatal("Quit did not preserve tab confirmation order")
					}
					state.resolve(session.Discard)
					if allowed != 0 || state.session.Pending() == nil || state.session.Pending().Document != projected {
						context.Fatal("Quit skipped the clean projected composition after an earlier decision")
					}
					state.resolve(session.Save)
					if state.message != session.ErrCompositionPending.Error() || state.session.Pending() == nil || state.session.Pending().Document != projected {
						context.Fatal("projected Save was not refused in the existing transition")
					}
					cancelCompositionFixture(context, state, owner)
					if state.session.Pending() == nil || state.session.Pending().Document != projected {
						context.Fatal("ending preedit lost the confirmation's document ownership")
					}
					state.requestClose(func() { allowed += 100 })
					state.resolve(session.Save)
					if state.session.Pending() == nil || state.session.Pending().Document != later {
						context.Fatal("Save after preedit ended did not advance to the later draft")
					}
					if cancel {
						state.resolve(session.Cancel)
						if allowed != 0 || state.allowClose != nil || state.quitDiscarded || !earlier.Dirty() || !later.Dirty() {
							context.Fatal("Cancel lost earlier/later committed drafts or allowed Quit")
						}
						state.requestClose(func() { allowed += 10 })
						if state.session.Pending() == nil || state.session.Pending().Document != earlier {
							context.Fatal("a new Quit inherited stale approved decisions")
						}
						state.resolve(session.Cancel)
					} else {
						state.resolve(session.Discard)
						if allowed != 1 || !state.recoveryDiscarded || state.allowClose != nil {
							context.Fatal("completed Quit did not retain original callback/discard intent")
						}
					}
					if data, err := os.ReadFile(filename); err != nil || !bytes.Equal(data, encode(original)) {
						context.Fatalf("mixed Quit changed projected disk: %q / %v", data, err)
					}
				})
			}
		}
	}
}

func TestApplicationQuitCleanUnmarkedOwnersRemainImmediate(test *testing.T) {
	owners := append(append([]compositionCancelCase{}, cancelCompositionCases...), sourceKindCompositionCases...)
	for _, owner := range owners {
		for _, encoding := range []string{"lf", "bom-crlf"} {
			test.Run(owner.name+"/"+encoding, func(context *testing.T) {
				state, _, _, _, _ := openMenuSaveFixture(context, owner, encoding)
				allowed := 0
				state.requestClose(func() { allowed++ })
				if allowed != 1 || state.session.Pending() != nil {
					context.Fatal("clean unmarked document no longer quits immediately")
				}
			})
		}
	}
}

func TestApplicationQuitIgnoresUnownedProjection(test *testing.T) {
	for index, owner := range cancelCompositionCases[:4] {
		for _, encoding := range []string{"lf", "bom-crlf"} {
			for _, inactive := range []bool{false, true} {
				name := owner.name + "/" + encoding + "/stale"
				if inactive {
					name = owner.name + "/" + encoding + "/inactive"
				}
				test.Run(name, func(context *testing.T) {
					state, _, _, _, _ := openMenuSaveFixture(context, owner, encoding)
					input := owner.editor(state)
					input.SetSelection(text.Range{Start: 0, End: 1})
					input.SetMarkedText("ni", text.Range{Start: 2, End: 2})
					switch index {
					case 0:
						if inactive {
							state.rich.active = false
						} else {
							state.rich.documentID += 1000
						}
					case 1:
						if inactive {
							state.codeBlock.active = false
						} else {
							state.codeBlock.documentID += 1000
						}
					case 2:
						if inactive {
							state.table.active = false
						} else {
							state.table.documentID += 1000
						}
					case 3:
						if inactive {
							state.html.active = false
						} else {
							state.html.documentID += 1000
						}
					}
					allowed := false
					state.requestClose(func() { allowed = true })
					if !allowed || state.session.Pending() != nil {
						context.Fatal("a stale/inactive projection created a phantom Quit confirmation")
					}
					input.CancelComposition()
				})
			}
		}
	}
}
