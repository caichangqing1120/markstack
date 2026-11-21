package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"markstack/internal/session"
)

func TestQuitConfirmationKeepsNativeComposition(test *testing.T) {
	owners := append(append([]compositionCancelCase{}, cancelCompositionCases...), sourceKindCompositionCases...)
	for _, owner := range owners {
		for _, encoding := range []string{"lf", "bom-crlf"} {
			for _, dirty := range []bool{false, true} {
				name := owner.name + "/" + encoding + "/clean"
				if dirty {
					name = owner.name + "/" + encoding + "/dirty"
				}
				test.Run(name, func(context *testing.T) {
					state, driver, filename, original, encode := openMenuSaveFixture(context, owner, encoding)
					if dirty {
						if err := driver.SelectText(0, 1); err != nil {
							context.Fatal(err)
						}
						if err := driver.Type("X"); err != nil {
							context.Fatal(err)
						}
					}
					if err := driver.SelectText(1, 0); err != nil {
						context.Fatal(err)
					}
					if err := driver.Compose("ni"); err != nil {
						context.Fatal(err)
					}
					input := owner.editor(state)
					focus := compositionFocusByCase[owner.name]
					before := app.TextStateOf(input)
					bounds := driver.Snapshot().Editor.Bounds
					current, source := state.session.Current(), state.session.Current().Editor
					baseline, recovery := source.CommittedString(), state.session.RecoverySnapshot()
					allowed := false
					state.requestClose(func() { allowed = true })
					if err := driver.Resize(1201, 780); err != nil {
						context.Fatal(err)
					}
					requireNativeComposition(context, driver, focus, before, "Quit opened")
					if driver.Snapshot().Editor.Bounds != bounds {
						context.Fatal("Quit changed the composing native input frame")
					}
					pressID(context, driver, saveConfirmID)
					requireNativeComposition(context, driver, focus, before, "Save press")
					releaseID(context, driver, saveConfirmID)
					requireNativeComposition(context, driver, focus, before, "Save refusal")
					if state.message != session.ErrCompositionPending.Error() || state.session.Pending() == nil || allowed {
						context.Fatal("composing Save did not preserve the Quit confirmation")
					}
					for _, modifiers := range []gfx.Modifiers{0, gfx.ModifierShift} {
						if err := driver.Key(gfx.KeyTab, modifiers); err != nil {
							context.Fatal(err)
						}
						requireNativeComposition(context, driver, focus, before, "composing Tab")
					}
					background := hitBounds(context, driver, sidebarToggleID)
					sidebar := state.sidebar
					for _, button := range []gfx.PointerButton{gfx.PointerButtonPrimary, gfx.PointerButtonSecondary} {
						if target, hit := driver.HitTest(background.X+background.Width/2, background.Y+background.Height/2); hit && target == sidebarToggleID {
							context.Fatal("the modal backdrop exposed an underlying toolbar hit")
						}
						if err := driver.ClickButton(background.X+background.Width/2, background.Y+background.Height/2, button); err != nil {
							context.Fatal(err)
						}
						requireNativeComposition(context, driver, focus, before, "modal background")
						if state.session.Current() != current || state.session.Pending() == nil || state.sidebar != sidebar {
							context.Fatal("a background click reached a document/toolbar action")
						}
					}
					if err := driver.Compose("nihao"); err != nil {
						context.Fatal(err)
					}
					continued := app.TextStateOf(input)
					requireCompositionExtended(context, "in confirmation", before, continued)
					requireNativeComposition(context, driver, focus, continued, "continued in confirmation")
					pressID(context, driver, cancelConfirmID)
					requireNativeComposition(context, driver, focus, continued, "Cancel press")
					releaseID(context, driver, cancelConfirmID)
					requireNativeComposition(context, driver, focus, continued, "Quit canceled")
					if state.session.Pending() != nil || state.allowClose != nil || allowed {
						context.Fatal("Cancel failed to stop Quit")
					}
					if err := driver.Compose("nihaoma"); err != nil {
						context.Fatal(err)
					}
					requireCompositionExtended(context, "after Cancel", continued, app.TextStateOf(input))
					if state.session.Current() != current || current.Editor != source || owner.editor(state) != input || source.CommittedString() != baseline || !reflect.DeepEqual(state.session.RecoverySnapshot(), recovery) {
						context.Fatal("Quit focus handling changed owner/source/recovery identity")
					}
					if driver.Snapshot().Editor.Bounds != bounds {
						context.Fatal("Quit/Cancel changed the native input frame")
					}
					if data, err := os.ReadFile(filename); err != nil || !bytes.Equal(data, encode(original)) {
						context.Fatal("Quit/Cancel preedit reached disk")
					}
					cancelCompositionFixture(context, state, owner)
					if dirty && !source.Undo() {
						context.Fatal("Quit/Cancel consumed earlier source history")
					}
					if current.Text() != original || current.Dirty() {
						context.Fatal("original source did not survive Quit/Cancel")
					}
				})
			}
		}
	}
}

func TestQuitConfirmationRegainsKeyboardModalityAfterComposition(test *testing.T) {
	owners := append(append([]compositionCancelCase{}, cancelCompositionCases...), sourceKindCompositionCases...)
	for _, owner := range owners {
		for _, encoding := range []string{"lf", "bom-crlf"} {
			test.Run(owner.name+"/"+encoding, func(context *testing.T) {
				state, driver, _, _, _ := openMenuSaveFixture(context, owner, encoding)
				if err := driver.SelectText(0, 1); err != nil {
					context.Fatal(err)
				}
				if err := driver.Compose("ni"); err != nil {
					context.Fatal(err)
				}
				allowed := false
				state.requestClose(func() { allowed = true })
				cancelCompositionFixture(context, state, owner)
				if err := driver.Resize(1201, 780); err != nil {
					context.Fatal(err)
				}
				if state.session.Pending() == nil || driver.Snapshot().Focused != cancelConfirmID || driver.Snapshot().Editor != nil {
					context.Fatalf("ending IME lost confirmation ownership/modality: pending=%v focus=%v", state.session.Pending(), driver.Snapshot().Focused)
				}
				for range 3 {
					if err := driver.Key(gfx.KeyTab, 0); err != nil {
						context.Fatal(err)
					}
					focus := driver.Snapshot().Focused
					if focus != cancelConfirmID && focus != discardConfirmID && focus != saveConfirmID {
						context.Fatal("normal modal Tab escaped the confirmation")
					}
				}
				if driver.Snapshot().Focused != cancelConfirmID {
					context.Fatal("normal modal Tab did not cycle back to Cancel")
				}
				if err := driver.Key(gfx.KeyEnter, 0); err != nil {
					context.Fatal(err)
				}
				if state.session.Pending() != nil || allowed {
					context.Fatalf("normal modal Cancel activation failed: pending=%v allowed=%v focus=%v", state.session.Pending(), allowed, driver.Snapshot().Focused)
				}
			})
		}
	}
}

func TestQuitConfirmationWithoutCompositionKeepsKeyboardModal(test *testing.T) {
	owners := append(append([]compositionCancelCase{}, cancelCompositionCases...), sourceKindCompositionCases...)
	for _, owner := range owners {
		test.Run(owner.name, func(context *testing.T) {
			state, driver, _, _, _ := openMenuSaveFixture(context, owner, "bom-crlf")
			if err := driver.SelectText(0, 1); err != nil {
				context.Fatal(err)
			}
			if err := driver.Type("X"); err != nil {
				context.Fatal(err)
			}
			state.requestClose(func() { context.Fatal("Quit allowed without a decision") })
			if err := driver.Resize(1201, 780); err != nil {
				context.Fatal(err)
			}
			if driver.Snapshot().Focused != cancelConfirmID || driver.Snapshot().Editor != nil {
				context.Fatal("normal confirmation no longer focuses Cancel")
			}
			if err := driver.Key(gfx.KeyEscape, 0); err != nil {
				context.Fatal(err)
			}
			if state.session.Pending() != nil {
				context.Fatal("normal confirmation Escape did not cancel")
			}
		})
	}
}

func TestQuitConfirmationOtherDocumentPreservesCompositionOwner(test *testing.T) {
	owners := append(append([]compositionCancelCase{}, cancelCompositionCases...), sourceKindCompositionCases...)
	for _, owner := range owners {
		test.Run(owner.name, func(context *testing.T) {
			state, driver, _, _, _ := openMenuSaveFixture(context, owner, "bom-crlf")
			current := state.session.Current()
			earlier := state.session.NewDocument()
			earlier.Editor.Insert("earlier committed draft")
			state.session.Tabs = []*session.Document{earlier, current}
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
			before := app.TextStateOf(owner.editor(state))
			focus := compositionFocusByCase[owner.name]
			state.requestClose(func() { context.Fatal("Quit ignored pending composition") })
			if err := driver.Resize(1201, 780); err != nil {
				context.Fatal(err)
			}
			if state.session.Pending() == nil || state.session.Pending().Document != earlier {
				context.Fatal("fixture confirmation order changed")
			}
			requireNativeComposition(context, driver, focus, before, "earlier document confirmation")
			target := filepath.Join(context.TempDir(), "earlier.md")
			if err := driver.QueueSaveFile(target, nil); err != nil {
				context.Fatal(err)
			}
			clickID(context, driver, saveConfirmID)
			requireNativeComposition(context, driver, focus, before, "other document saved")
			if state.session.Pending() == nil || state.session.Pending().Document != current || earlier.Dirty() {
				context.Fatal("saving another document lost the composition's confirmation")
			}
			if data, err := os.ReadFile(target); err != nil || string(data) != "earlier committed draft" {
				context.Fatal("other document Save wrote incorrect content")
			}
			clickID(context, driver, cancelConfirmID)
			requireNativeComposition(context, driver, focus, before, "mixed Quit canceled")
			cancelCompositionFixture(context, state, owner)
		})
	}
}

func TestQuitConfirmationCommittedInputCanBeSaved(test *testing.T) {
	owners := append(append([]compositionCancelCase{}, cancelCompositionCases...), sourceKindCompositionCases...)
	for _, owner := range owners {
		for _, encoding := range []string{"lf", "bom-crlf"} {
			test.Run(owner.name+"/"+encoding, func(context *testing.T) {
				state, driver, filename, original, encode := openMenuSaveFixture(context, owner, encoding)
				if err := driver.SelectText(0, 1); err != nil {
					context.Fatal(err)
				}
				if err := driver.Compose("ni"); err != nil {
					context.Fatal(err)
				}
				allowed := 0
				state.requestClose(func() { allowed++ })
				if err := driver.Resize(1201, 780); err != nil {
					context.Fatal(err)
				}
				if err := driver.Type("X"); err != nil {
					context.Fatal(err)
				}
				wanted := owner.commitSource
				if wanted == "" {
					_, size := utf8.DecodeRuneInString(original)
					wanted = "X" + original[size:]
				}
				if encoding == "bom-crlf" {
					wanted = strings.ReplaceAll(strings.ReplaceAll(wanted, "\r\n", "\n"), "\n", "\r\n")
				}
				if state.session.Current().Text() != wanted || state.session.Pending() == nil || driver.Snapshot().Focused != cancelConfirmID || driver.Snapshot().Editor != nil {
					context.Fatal("committed input did not regain ordinary confirmation modality")
				}
				clickID(context, driver, saveConfirmID)
				if allowed != 1 || state.session.Pending() != nil || state.session.Current().Dirty() || state.recoveryDiscarded {
					context.Fatal("committed Save did not finish Quit safely")
				}
				if data, err := os.ReadFile(filename); err != nil || !bytes.Equal(data, encode(wanted)) {
					context.Fatal("Quit Save did not write exactly committed source")
				}
				if !state.session.Current().Editor.Undo() {
					context.Fatal("Quit Save consumed committed source history")
				}
				if err := state.session.Save(state.session.Active, ""); err != nil {
					context.Fatal(err)
				}
				if data, err := os.ReadFile(filename); err != nil || !bytes.Equal(data, encode(original)) {
					context.Fatal("original bytes did not survive committed Quit Save")
				}
			})
		}
	}
}

func TestQuitConfirmationDiscardedComposerStopsUntilCancel(test *testing.T) {
	owners := append(append([]compositionCancelCase{}, cancelCompositionCases...), sourceKindCompositionCases...)
	for _, owner := range owners {
		for _, encoding := range []string{"lf", "bom-crlf"} {
			test.Run(owner.name+"/"+encoding, func(context *testing.T) {
				state, driver, filename, original, encode := openMenuSaveFixture(context, owner, encoding)
				current := state.session.Current()
				later := state.session.NewDocument()
				later.Editor.Insert("later draft")
				state.session.Tabs, state.session.Active = []*session.Document{current, later}, 0
				if err := driver.Resize(1201, 780); err != nil {
					context.Fatal(err)
				}
				if err := driver.SelectText(1, 0); err != nil {
					context.Fatal(err)
				}
				if err := driver.Compose("ni"); err != nil {
					context.Fatal(err)
				}
				input := owner.editor(state)
				before, recovery := app.TextStateOf(input), state.session.RecoverySnapshot()
				state.requestClose(func() { context.Fatal("Quit ignored the remaining draft") })
				if err := driver.Resize(1201, 780); err != nil {
					context.Fatal(err)
				}
				clickID(context, driver, discardConfirmID)
				if pending := state.session.Pending(); pending == nil || pending.Document != later {
					context.Fatal("Discard did not advance to the later draft")
				}
				if driver.Snapshot().Editor != nil || state.confirmationCompositionFocus() != (gfx.ID{}) || driver.Snapshot().Focused != cancelConfirmID {
					context.Fatal("the discarded document retained native input during another document's decision")
				}
				if err := driver.Type("X"); err == nil {
					context.Fatal("native typing reached the discarded document")
				}
				clickID(context, driver, cancelConfirmID)
				if state.session.Pending() != nil || owner.editor(state) != input || app.TextStateOf(input) != before || !reflect.DeepEqual(state.session.RecoverySnapshot(), recovery) {
					context.Fatal("canceling a later decision lost an earlier composition or draft")
				}
				requireNativeComposition(context, driver, compositionFocusByCase[owner.name], before, "later decision canceled")
				if err := driver.Compose("nihao"); err != nil {
					context.Fatal(err)
				}
				requireCompositionExtended(context, "after later Cancel", before, app.TextStateOf(input))
				if data, err := os.ReadFile(filename); err != nil || !bytes.Equal(data, encode(original)) {
					context.Fatal("Discard/Cancel wrote preedit to disk")
				}
			})
		}
	}
}

func TestQuitConfirmationLateCommitInvalidatesDiscard(test *testing.T) {
	owners := append(append([]compositionCancelCase{}, cancelCompositionCases...), sourceKindCompositionCases...)
	for _, owner := range owners {
		for _, encoding := range []string{"lf", "bom-crlf"} {
			test.Run(owner.name+"/"+encoding, func(context *testing.T) {
				state, driver, filename, original, encode := openMenuSaveFixture(context, owner, encoding)
				current := state.session.Current()
				later := state.session.NewDocument()
				later.Editor.Insert("later draft")
				state.session.Tabs, state.session.Active = []*session.Document{current, later}, 0
				if err := driver.Resize(1201, 780); err != nil {
					context.Fatal(err)
				}
				if err := driver.SelectText(0, 1); err != nil {
					context.Fatal(err)
				}
				if err := driver.Compose("ni"); err != nil {
					context.Fatal(err)
				}
				config := newConfig(state)
				queued := *config.TextInput(app.TextInputContext{ID: compositionFocusByCase[owner.name]})
				allowed := 0
				state.requestClose(func() { allowed++ })
				if err := driver.Resize(1201, 780); err != nil {
					context.Fatal(err)
				}
				clickID(context, driver, discardConfirmID)
				if pending := state.session.Pending(); pending == nil || pending.Document != later {
					context.Fatal("Discard did not advance to the later draft")
				}
				pressID(context, driver, discardConfirmID)
				event := app.TextEvent{Kind: app.TextInsert, Text: "X"}
				if queued.OnEvent != nil {
					if _, err := queued.OnEvent(event); err != nil {
						context.Fatal(err)
					}
				} else {
					event.Apply(queued.Editor)
				}
				if err := driver.Resize(1201, 780); err != nil {
					context.Fatal(err)
				}
				releaseID(context, driver, discardConfirmID)
				if !current.Dirty() || current.Text() == original || strings.Contains(current.Text(), "ni") || state.session.Pending() == nil || state.session.Pending().Document != current || allowed != 0 || state.recoveryDiscarded {
					context.Fatal("a late committed callback retained a stale Discard approval")
				}
				committed := current.Text()
				clickID(context, driver, saveConfirmID)
				if current.Dirty() || state.session.Pending() == nil || state.session.Pending().Document != later || allowed != 0 {
					context.Fatal("saving the newly confirmed commit lost the later decision")
				}
				if data, err := os.ReadFile(filename); err != nil || !bytes.Equal(data, encode(committed)) {
					context.Fatal("the renewed Save did not write exactly the committed source")
				}
				clickID(context, driver, cancelConfirmID)
				if allowed != 0 || !later.Dirty() || state.recoveryDiscarded {
					context.Fatal("Cancel lost the remaining draft")
				}
			})
		}
	}
}

func TestQuitConfirmationRejectsPathAfterPendingDocumentChanges(test *testing.T) {
	owner := cancelCompositionCases[0]
	state, driver, filename, original, encode := openMenuSaveFixture(test, owner, "bom-crlf")
	current := state.session.Current()
	later := state.session.NewDocument()
	later.Editor.Insert("later draft")
	state.session.Tabs, state.session.Active = []*session.Document{current, later}, 0
	if err := driver.Paint(); err != nil {
		test.Fatal(err)
	}
	if err := driver.SelectText(0, 1); err != nil {
		test.Fatal(err)
	}
	if err := driver.Compose("ni"); err != nil {
		test.Fatal(err)
	}
	config := newConfig(state)
	queued := *config.TextInput(app.TextInputContext{ID: compositionFocusByCase[owner.name]})
	allowed := 0
	state.requestClose(func() { allowed++ })
	if err := driver.Paint(); err != nil {
		test.Fatal(err)
	}
	clickID(test, driver, discardConfirmID)
	if pending := state.session.Pending(); pending == nil || pending.Document != later {
		test.Fatal("Discard did not advance to the untitled draft")
	}
	destination := filepath.Join(test.TempDir(), "later.md")
	state.resolveWithSavePicker(session.Save, func(options app.SaveFileOptions) (string, error) {
		if options.SuggestedName != later.Title {
			test.Fatal("the save picker did not belong to the untitled draft")
		}
		if _, err := queued.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: "X"}); err != nil {
			test.Fatal(err)
		}
		if err := driver.Paint(); err != nil {
			test.Fatal(err)
		}
		if pending := state.session.Pending(); pending == nil || pending.Document != current {
			test.Fatal("late commit did not reopen the first decision")
		}
		return destination, nil
	})
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		test.Fatalf("the stale picker wrote to the untitled draft's destination: %v", err)
	}
	if data, err := os.ReadFile(filename); err != nil || !bytes.Equal(data, encode(original)) || !current.Dirty() || !later.Dirty() || allowed != 0 {
		test.Fatal("rejecting the picker result did not preserve both drafts and the original file")
	}
	if pending := state.session.Pending(); pending == nil || pending.Document != current || current.Path != filename || later.Path != "" {
		test.Fatal("rejecting the picker result changed confirmation ownership or document paths")
	}
	committed := current.Text()
	if err := driver.Paint(); err != nil {
		test.Fatal(err)
	}
	clickID(test, driver, saveConfirmID)
	if data, err := os.ReadFile(filename); err != nil || !bytes.Equal(data, encode(committed)) {
		test.Fatal("a fresh Save did not write the first draft to its original file")
	}
	if pending := state.session.Pending(); pending == nil || pending.Document != later || !later.Dirty() || allowed != 0 {
		test.Fatal("a fresh Save did not keep the other confirmation pending")
	}
}

func TestQuitConfirmationRejectsActivationOnUnrefreshedPrompt(test *testing.T) {
	for _, owner := range sourceKindCompositionCases {
		for _, activation := range []string{"pointer", "keyboard"} {
			test.Run(owner.name+"/"+activation, func(context *testing.T) {
				state, driver, filename, original, encode := openMenuSaveFixture(context, owner, "bom-crlf")
				current := state.session.Current()
				later := state.session.NewDocument()
				later.Editor.Insert("later draft")
				state.session.Tabs, state.session.Active = []*session.Document{current, later}, 0
				if err := driver.Paint(); err != nil {
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
				if err := driver.Paint(); err != nil {
					context.Fatal(err)
				}
				clickID(context, driver, discardConfirmID)
				if pending := state.session.Pending(); pending == nil || pending.Document != later {
					context.Fatal("Discard did not advance to the later draft")
				}
				if activation == "pointer" {
					bounds := hitBounds(context, driver, discardConfirmID)
					if err := driver.Move(bounds.X+bounds.Width/2, bounds.Y+bounds.Height/2); err != nil {
						context.Fatal(err)
					}
				} else {
					if err := driver.Key(gfx.KeyTab, 0); err != nil {
						context.Fatal(err)
					}
					if driver.Snapshot().Focused != discardConfirmID {
						context.Fatal("Tab did not focus Discard on the later confirmation")
					}
				}
				if err := driver.Paint(); err != nil {
					context.Fatal(err)
				}
				app.TextEvent{Kind: app.TextInsert, Text: "X"}.Apply(current.Editor)
				if pending := state.session.Pending(); pending == nil || pending.Document != current {
					context.Fatal("late commit did not reopen the first decision")
				}
				if activation == "pointer" {
					pressID(context, driver, discardConfirmID)
					releaseID(context, driver, discardConfirmID)
				} else if err := driver.Key(gfx.KeyEnter, 0); err != nil {
					context.Fatal(err)
				}
				if pending := state.session.Pending(); pending == nil || pending.Document != current || allowed != 0 || state.recoveryDiscarded {
					context.Fatal("activation on the unrefreshed prompt discarded the newly committed draft")
				}
				if data, err := os.ReadFile(filename); err != nil || !bytes.Equal(data, encode(original)) || !current.Dirty() || !later.Dirty() {
					context.Fatal("the rejected press did not preserve disk bytes and both drafts")
				}
				committed := current.Text()
				clickID(context, driver, saveConfirmID)
				if data, err := os.ReadFile(filename); err != nil || !bytes.Equal(data, encode(committed)) || current.Dirty() {
					context.Fatal("a fresh Save did not write the newly committed draft")
				}
				if pending := state.session.Pending(); pending == nil || pending.Document != later || !later.Dirty() || allowed != 0 {
					context.Fatal("a fresh Save did not retain the other draft for confirmation")
				}
			})
		}
	}
}

func TestQuitConfirmationKeepsBackgroundAccessibilityInert(test *testing.T) {
	owners := append(append([]compositionCancelCase{}, cancelCompositionCases...), sourceKindCompositionCases...)
	for _, owner := range owners {
		for _, composing := range []bool{false, true} {
			name := owner.name + "/ordinary"
			if composing {
				name = owner.name + "/composing"
			}
			test.Run(name, func(context *testing.T) {
				state, driver, _, _, _ := openMenuSaveFixture(context, owner, "lf")
				if err := driver.SelectText(0, 1); err != nil {
					context.Fatal(err)
				}
				if composing {
					if err := driver.Compose("ni"); err != nil {
						context.Fatal(err)
					}
				} else if err := driver.Type("X"); err != nil {
					context.Fatal(err)
				}
				state.requestClose(func() { context.Fatal("Quit ignored the pending decision") })
				if err := driver.Resize(1201, 780); err != nil {
					context.Fatal(err)
				}
				var canvas gfx.Canvas
				canvas.SetTree(&controlTree)
				state.paint(&canvas, ui.Size{Width: 1201, Height: 780})
				trap := canvas.TrappedFocusScope()
				if trap.IsZero() {
					context.Fatal("Quit has no modal scope to hide background accessibility nodes")
				}
				composer := compositionFocusByCase[owner.name]
				foundComposer := false
				for _, hit := range canvas.Hits() {
					if hit.Scope != trap || hit.Block || hit.Backdrop || hit.IsLabel {
						continue
					}
					switch hit.ID {
					case cancelConfirmID, discardConfirmID, saveConfirmID:
					case composer:
						if !composing {
							context.Fatal("ordinary confirmation exposed the background editor")
						}
						foundComposer = true
					default:
						context.Fatalf("Quit exposes a background accessibility control: %v", hit.ID)
					}
					if hit.Disabled {
						context.Fatal("an allowed modal control was disabled")
					}
				}
				if composing && !foundComposer {
					context.Fatal("the modal accessibility scope lost the composing editor")
				}
			})
		}
	}
}
