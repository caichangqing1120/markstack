package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"

	"markstack/internal/session"
	"markstack/internal/workspace"
)

type findHistoryFixture struct {
	state    *model
	driver   *app.Driver
	filename string
	original string
	bom      bool
}

func openFindHistoryFixture(test *testing.T, suffix, encoding string) findHistoryFixture {
	test.Helper()
	directory := test.TempDir()
	for _, key := range []string{"SETTINGS", "RECOVERY", "SESSION", "RECENT"} {
		test.Setenv("MARKSTACK_"+key+"_PATH", filepath.Join(directory, strings.ToLower(key)+".json"))
	}
	original := "outside \u540d\u79f00\n    \u540d\u79f01 e\u0301 \U0001f469\u200d\U0001f4bb\n\t\u540d\u79f02 \u03b1\noutside \u540d\u79f03\n"
	bom := encoding == "bom-crlf"
	if bom {
		original = strings.ReplaceAll(original, "\n", "\r\n")
	}
	filename := filepath.Join(directory, "sample."+suffix)
	encoded := original
	if bom {
		encoded = "\ufeff" + original
	}
	if err := os.WriteFile(filename, []byte(encoded), 0640); err != nil {
		test.Fatal(err)
	}
	state, driver := testDriver(test)
	if !state.openFile(filename) {
		test.Fatal(state.message)
	}
	if err := driver.Resize(740, 520); err != nil {
		test.Fatal(err)
	}
	clickID(test, driver, sourceID)
	return findHistoryFixture{state, driver, filename, original, bom}
}

func (fixture findHistoryFixture) checkDisk(test *testing.T, source string) {
	test.Helper()
	if fixture.bom {
		source = "\ufeff" + source
	}
	data, err := os.ReadFile(fixture.filename)
	info, statErr := os.Stat(fixture.filename)
	if err != nil || statErr != nil || !bytes.Equal(data, []byte(source)) || info.Mode().Perm() != 0640 {
		test.Fatalf("saved bytes=%q want=%q read=%v stat=%v", data, source, err, statErr)
	}
}

func TestSourceReplaceAllUndoRestoresActualSelection(test *testing.T) {
	for _, suffix := range []string{"go", "txt"} {
		for _, encoding := range []string{"lf", "bom-crlf"} {
			for _, scope := range []string{"document", "selection"} {
				for _, query := range []string{"literal", "regex"} {
					for _, selection := range []string{"match", "partial", "reverse", "caret"} {
						test.Run(suffix+"/"+encoding+"/"+scope+"/"+query+"/"+selection, func(context *testing.T) {
							fixture := openFindHistoryFixture(context, suffix, encoding)
							state, driver := fixture.state, fixture.driver
							current := state.session.Current()
							input, documentID := current.Editor, current.ID
							startByte := strings.Index(fixture.original, "\u540d\u79f01")
							endByte := strings.Index(fixture.original, "\u540d\u79f02") + len("\u540d\u79f02")
							start, end := utf8.RuneCountInString(fixture.original[:startByte]), utf8.RuneCountInString(fixture.original[:endByte])
							if err := driver.SelectText(start, end); err != nil {
								context.Fatal(err)
							}
							clickID(context, driver, findDocumentID)
							pattern := "\u540d\u79f0"
							if query == "regex" {
								pattern = "\u540d\u79f0[0-9]"
							}
							if err := driver.Type(pattern); err != nil {
								context.Fatal(err)
							}
							if query == "regex" {
								clickID(context, driver, findRegexID)
							}
							if scope == "selection" {
								clickID(context, driver, findScopeID)
							}
							clickID(context, driver, replaceInputID)
							if err := driver.Type("\u9879\u76ee"); err != nil {
								context.Fatal(err)
							}
							wanted := fixture.original
							for _, digit := range []string{"0", "1", "2", "3"} {
								if scope == "selection" && (digit == "0" || digit == "3") {
									continue
								}
								replacement := "\u9879\u76ee"
								if query == "literal" {
									replacement += digit
								}
								wanted = strings.Replace(wanted, "\u540d\u79f0"+digit, replacement, 1)
							}
							switch selection {
							case "partial":
								input.SetSelection(text.Range{Start: start + 1, End: end})
							case "reverse":
								input.SetSelection(text.Range{Start: end, End: start + 1})
							case "caret":
								input.MoveTo(start+1, false)
							}
							before, caret := input.Selection(), input.Caret()
							clickID(context, driver, replaceAllID)
							clickID(context, driver, findCloseID)
							if current.Text() != wanted || !current.Dirty() || current.Editor != input || current.ID != documentID {
								context.Fatalf("replacement changed identity or wrong source: %q want %q", current.Text(), wanted)
							}
							after, afterCaret := input.Selection(), input.Caret()
							fixture.checkDisk(context, fixture.original)
							for round := 0; round < 2; round++ {
								if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
									context.Fatal(err)
								}
								if current.Text() != fixture.original || input.Selection() != before || input.Caret() != caret || input.CanUndo() {
									context.Fatalf("Undo source=%q selection=%+v caret=%d, want original / %+v caret=%d", current.Text(), input.Selection(), input.Caret(), before, caret)
								}
								if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
									context.Fatal(err)
								}
								if current.Text() != wanted || input.Selection() != after || input.Caret() != afterCaret {
									context.Fatalf("Redo lost result/selection: %q %+v caret=%d", current.Text(), input.Selection(), input.Caret())
								}
							}
							if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
								context.Fatal(err)
							}
							letters := []rune(fixture.original)
							typed := string(letters[:before.Start]) + "\u7eed" + string(letters[before.End:])
							if err := driver.Type("\u7eed"); err != nil {
								context.Fatal(err)
							}
							if current.Text() != typed || input.CanRedo() {
								context.Fatalf("typing after Undo changed wrong interval or retained stale Redo: %q want %q", current.Text(), typed)
							}
							if err := state.session.Save(state.session.Active, ""); err != nil {
								context.Fatal(err)
							}
							fixture.checkDisk(context, typed)
							if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
								context.Fatal(err)
							}
							if current.Text() != fixture.original || input.Selection() != before || input.Caret() != caret {
								context.Fatal("typing Undo did not restore the original selection")
							}
							if err := state.session.Save(state.session.Active, ""); err != nil {
								context.Fatal(err)
							}
							fixture.checkDisk(context, fixture.original)
						})
					}
				}
			}
		}
	}
}

func TestSourceReplaceAllKeepsInterleavedHistory(test *testing.T) {
	for _, suffix := range []string{"go", "txt"} {
		for _, encoding := range []string{"lf", "bom-crlf"} {
			test.Run(suffix+"/"+encoding, func(context *testing.T) {
				fixture := openFindHistoryFixture(context, suffix, encoding)
				state, driver := fixture.state, fixture.driver
				input := state.session.Current().Editor
				if err := driver.SelectText(0, 7); err != nil {
					context.Fatal(err)
				}
				if err := driver.Type("earlier"); err != nil {
					context.Fatal(err)
				}
				earlier := strings.Replace(fixture.original, "outside", "earlier", 1)
				clickID(context, driver, findDocumentID)
				if err := driver.Type("\u540d\u79f0"); err != nil {
					context.Fatal(err)
				}
				clickID(context, driver, replaceInputID)
				if err := driver.Type("\u9879\u76ee"); err != nil {
					context.Fatal(err)
				}
				input.SetSelection(text.Range{Start: 12, End: 8})
				clickID(context, driver, replaceAllID)
				clickID(context, driver, findCloseID)
				replaced := strings.ReplaceAll(earlier, "\u540d\u79f0", "\u9879\u76ee")
				if state.session.Current().Text() != replaced {
					context.Fatal("interleaved Replace All did not run")
				}
				if err := driver.Type(" later"); err != nil {
					context.Fatal(err)
				}
				later := replaced + " later"
				for round := 0; round < 2; round++ {
					for _, expected := range []string{replaced, earlier, fixture.original} {
						if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
							context.Fatal(err)
						}
						if state.session.Current().Text() != expected {
							context.Fatalf("interleaved Undo=%q want=%q", state.session.Current().Text(), expected)
						}
						if expected == earlier && (input.Selection() != (text.Range{Start: 8, End: 12}) || input.Caret() != 8) {
							context.Fatal("interleaved Undo lost reversed selection")
						}
					}
					for _, expected := range []string{earlier, replaced, later} {
						if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
							context.Fatal(err)
						}
						if state.session.Current().Text() != expected {
							context.Fatalf("interleaved Redo=%q want=%q", state.session.Current().Text(), expected)
						}
					}
				}
				for range 3 {
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
				}
				fixture.checkDisk(context, fixture.original)
			})
		}
	}
}

func TestSourceReplaceAllGuardsDoNotMutateHistory(test *testing.T) {
	for _, suffix := range []string{"go", "txt"} {
		for _, scenario := range []string{"no-op", "no-match", "bad-regex", "pending", "external-change"} {
			test.Run(suffix+"/"+scenario, func(context *testing.T) {
				fixture := openFindHistoryFixture(context, suffix, "bom-crlf")
				state, driver := fixture.state, fixture.driver
				input := state.session.Current().Editor
				state.openDocumentFind()
				state.findEditor.Insert("\u540d\u79f0")
				state.replaceEditor.Insert("\u9879\u76ee")
				if scenario == "no-op" {
					state.replaceEditor = text.NewEditor("\u540d\u79f0")
				} else if scenario == "no-match" {
					state.findEditor = text.NewEditor("absent")
				} else if scenario == "bad-regex" {
					state.findRegex = true
					state.findEditor = text.NewEditor("[")
				}
				state.syncDocumentFind()
				input.SetSelection(text.Range{Start: 12, End: 8})
				if scenario == "pending" {
					input.SetMarkedText("ni", text.Range{Start: 2, End: 2})
				}
				before, selected, caret := input.String(), input.Selection(), input.Caret()
				state.replaceDocumentMatches(true)
				if scenario == "external-change" {
					if err := os.WriteFile(fixture.filename, []byte("external\r\n"), 0640); err != nil {
						context.Fatal(err)
					}
					if err := state.session.Save(state.session.Active, ""); !errors.Is(err, workspace.ErrConflict) {
						context.Fatalf("external Save=%v", err)
					}
					if data, _ := os.ReadFile(fixture.filename); string(data) != "external\r\n" {
						context.Fatal("external file was overwritten")
					}
					return
				}
				if input.String() != before || input.Selection() != selected || input.Caret() != caret || state.session.Current().Dirty() || len(state.session.RecoverySnapshot().Drafts) != 0 {
					context.Fatal("refused/no-op replacement changed committed state or selection")
				}
				if scenario == "pending" {
					if _, composing := input.Composition(); !composing {
						context.Fatal("replacement discarded pending composition")
					}
					state.saveActive(false)
					if state.message != session.ErrCompositionPending.Error() {
						context.Fatal("pending composition became savable")
					}
					input.CancelComposition()
				}
				if input.CanUndo() || input.CanRedo() {
					context.Fatal("refused/no-op replacement invented history")
				}
				if err := driver.Resize(740, 520); err != nil {
					context.Fatal(err)
				}
				fixture.checkDisk(context, fixture.original)
			})
		}
	}
}
