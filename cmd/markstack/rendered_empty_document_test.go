package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestRenderedOrdinaryEmptyDocumentAcceptsInput(test *testing.T) {
	for _, scenario := range []struct {
		name, source, wanted string
		remove, cut          bool
	}{
		{"single-paragraph-cut", "Solo\n", "New\n", true, true},
		{"single-paragraph-delete", "Solo\n", "New\n", true, false},
		{"single-paragraph-cut-no-newline", "Solo", "New", true, true},
		{"initial-empty", "", "New", false, false},
		{"initial-line-endings", "\n\n", "New\n\n", false, false},
	} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{740, 1200} {
				test.Run(fmt.Sprintf("%s/dark-%v/width-%d", scenario.name, dark, width), func(context *testing.T) {
					state, driver, filename, original := renderedSourceCollisionFixture(context, scenario.source, dark, width)
					if scenario.remove {
						clickID(context, driver, renderedRows.At(0))
						if _, err := driver.ShortcutChord(primaryModifier(), 'a'); err != nil {
							context.Fatal(err)
						}
						if state.rich.input.Selection().Start != 0 || state.rich.input.Selection().End != 4 {
							context.Fatal("removal fixture did not select all visible text")
						}
						if scenario.cut {
							if _, err := driver.ShortcutChord(primaryModifier(), 'x'); err != nil {
								context.Fatal(err)
							}
							if driver.Clipboard() != "Solo" {
								context.Fatalf("cut clipboard=%q", driver.Clipboard())
							}
						} else if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
							context.Fatal(err)
						}
					} else {
						current := state.session.Current()
						if current.Dirty() || current.Editor.CanUndo() || current.Editor.CanRedo() {
							context.Fatal("opening empty rendered input created an edit")
						}
						expectRenderedCollisionSave(context, state, driver, filename, original)
					}
					found := false
					for _, hit := range driver.Snapshot().Hits {
						found = found || hit.ID == richInputID
					}
					if state.mode != renderedMode || !found {
						context.Fatalf("ordinary empty document lost rendered input: mode=%v active=%v", state.mode, state.rich.active)
					}
					clickID(context, driver, richInputID)
					if err := driver.Type("New"); err != nil {
						context.Fatal(err)
					}
					expected := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(scenario.wanted, "\n", "\r\n"))...)
					expectRenderedCollisionSave(context, state, driver, filename, expected)
				})
			}
		}
	}
}

func TestRenderedOrdinaryEmptyDocumentHistoryAfterNavigation(test *testing.T) {
	for _, scenario := range []struct {
		name, source, prefix, suffix string
		cut                          bool
	}{
		{"initial-empty", "", "", "", false},
		{"initial-line-endings", "\n\n", "", "\n\n", false},
		{"cut", "Solo\n", "", "\n", true},
		{"cut-leading-lines", "\n\nSolo\n", "\n\n", "\n", true},
	} {
		for _, transition := range []struct {
			name string
			mode gfx.ID
		}{{"tab", gfx.ID{}}, {"source", editModeID}, {"split", splitModeID}, {"read", readModeID}} {
			for _, dark := range []bool{false, true} {
				test.Run(fmt.Sprintf("%s/%s/dark-%v", scenario.name, transition.name, dark), func(context *testing.T) {
					state, driver, filename, original := renderedSourceCollisionFixture(context, scenario.source, dark, 740)
					current, tab := state.session.Current(), state.session.Active
					if scenario.cut {
						clickID(context, driver, renderedRows.At(0))
						for _, command := range []rune{'a', 'x'} {
							if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
								context.Fatal(err)
							}
						}
					}
					bytesFor := func(value string) []byte {
						return append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(scenario.prefix+value+scenario.suffix, "\n", "\r\n"))...)
					}
					expectRenderedCollisionSave(context, state, driver, filename, bytesFor(""))
					clickID(context, driver, richInputID)
					if err := driver.Compose("ni"); err != nil {
						context.Fatal(err)
					}
					expectRenderedCollisionSave(context, state, driver, filename, bytesFor(""))
					if _, err := state.onRenderedTextEvent(app.TextEvent{Kind: app.TextCancelComposition}); err != nil {
						context.Fatal(err)
					}
					if err := driver.Pump(); err != nil {
						context.Fatal(err)
					}
					if transition.mode == (gfx.ID{}) {
						other := filepath.Join(context.TempDir(), "Other.txt")
						if err := os.WriteFile(other, []byte("Other"), 0640); err != nil {
							context.Fatal(err)
						}
						if !state.openFile(other) {
							context.Fatal("other tab did not open")
						}
						state.invalidator.Invalidate()
						if err := driver.Pump(); err != nil {
							context.Fatal(err)
						}
						clickID(context, driver, tabRows.At(tab))
					} else {
						clickID(context, driver, transition.mode)
						if state.rich.input != nil || state.rich.active {
							context.Fatal("inactive rendered view retained a native editor")
						}
					}
					if state.mode != renderedMode {
						clickID(context, driver, renderedModeID)
					}
					assertInput := func(value string) {
						context.Helper()
						if state.session.Current() != current || state.mode != renderedMode || !state.rich.active || state.rich.input.String() != value || driver.Snapshot().Focused != richInputID {
							context.Fatalf("empty navigation/history lost its input: mode=%v active=%v", state.mode, state.rich.active)
						}
					}
					assertInput("")
					if err := driver.Type("New"); err != nil {
						context.Fatal(err)
					}
					assertInput("New")
					expectRenderedCollisionSave(context, state, driver, filename, bytesFor("New"))
					for cycle := 0; cycle < 2; cycle++ {
						if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
							context.Fatal(err)
						}
						assertInput("")
						expectRenderedCollisionSave(context, state, driver, filename, bytesFor(""))
						if scenario.cut {
							if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
								context.Fatal(err)
							}
							assertInput("Solo")
							expectRenderedCollisionSave(context, state, driver, filename, original)
							if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
								context.Fatal(err)
							}
							assertInput("")
						}
						if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
							context.Fatal(err)
						}
						assertInput("New")
						expectRenderedCollisionSave(context, state, driver, filename, bytesFor("New"))
					}
				})
			}
		}
	}
}

func TestRenderedInitialEmptyDocumentKeepsSourcePosition(test *testing.T) {
	for _, scenario := range []struct {
		name   string
		caret  int
		wanted string
	}{
		{"start", 0, "Q\r\n\r\n"},
		{"middle", 2, "\r\nQ\r\n"},
		{"inside-crlf", 3, "\r\nQ\r\n"},
		{"end", 4, "\r\n\r\nQ"},
	} {
		test.Run(scenario.name, func(context *testing.T) {
			state, driver, filename, original := renderedSourceCollisionFixture(context, "\n\n", false, 740)
			clickID(context, driver, editModeID)
			current := state.session.Current()
			current.Editor.MoveTo(scenario.caret, false)
			selection := current.Editor.Selection()
			clickID(context, driver, renderedModeID)
			if current.Dirty() || current.Editor.CanUndo() || current.Editor.CanRedo() || current.Editor.Selection() != selection {
				context.Fatal("empty-view activation mutated Source or its selection/history")
			}
			clickID(context, driver, richInputID)
			if err := driver.Type("Q"); err != nil {
				context.Fatal(err)
			}
			expectRenderedCollisionSave(context, state, driver, filename, append([]byte{0xef, 0xbb, 0xbf}, []byte(scenario.wanted)...))
			if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
				context.Fatal(err)
			}
			expectRenderedCollisionSave(context, state, driver, filename, original)
		})
	}
}

func TestRenderedEmptyBootstrapDoesNotConsumeMarkdownMarkers(test *testing.T) {
	for _, source := range []string{"# \n", "- \n", "> \n", "<div>opaque</div>\n", "```\n```\n", "    \n"} {
		test.Run(fmt.Sprintf("source-%q", source), func(context *testing.T) {
			state, driver, filename, original := renderedSourceCollisionFixture(context, source, false, 740)
			if state.rich.virtual || state.rich.active || state.session.Current().Dirty() {
				context.Fatal("nonempty syntax was treated as a plain empty document")
			}
			expectRenderedCollisionSave(context, state, driver, filename, original)
			stored, err := os.ReadFile(filename)
			if err != nil || !bytes.Equal(stored, original) {
				context.Fatal("empty bootstrap changed untouched Markdown syntax")
			}
		})
	}
}

func TestRenderedFinalParagraphDeletionKeepsNativeUndoGroup(test *testing.T) {
	state, driver, filename, original := renderedSourceCollisionFixture(test, "Solo\n", false, 740)
	clickID(test, driver, renderedRows.At(0))
	reference := text.NewEditor("Solo")
	reference.MoveTo(4, false)
	for range 4 {
		app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextDeleteBackward}.Apply(reference)
		if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
			test.Fatal(err)
		}
	}
	if !reference.Undo() || reference.String() != "Solo" {
		test.Fatal("native deletion control did not restore its coalesced group")
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		test.Fatal(err)
	}
	if state.mode != renderedMode || !state.rich.active || state.rich.input.String() != "Solo" {
		test.Fatal("deleting the final character split the native Undo group")
	}
	expectRenderedCollisionSave(test, state, driver, filename, original)
}

func TestRenderedEmptyCutSourceCaretKeepsHistory(test *testing.T) {
	for _, scenario := range []struct {
		name   string
		caret  int
		wanted string
	}{
		{"start", 0, "Q\r\n\r\n\r\n"},
		{"middle", 2, "\r\nQ\r\n\r\n"},
		{"end", 6, "\r\n\r\n\r\nQ"},
	} {
		test.Run(scenario.name, func(context *testing.T) {
			state, driver, filename, original := renderedSourceCollisionFixture(context, "\n\nSolo\n", false, 740)
			clickID(context, driver, renderedRows.At(0))
			for _, command := range []rune{'a', 'x'} {
				if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
					context.Fatal(err)
				}
			}
			clickID(context, driver, editModeID)
			state.session.Current().Editor.MoveTo(scenario.caret, false)
			clickID(context, driver, renderedModeID)
			clickID(context, driver, richInputID)
			if err := driver.Type("Q"); err != nil {
				context.Fatal(err)
			}
			expectRenderedCollisionSave(context, state, driver, filename, append([]byte{0xef, 0xbb, 0xbf}, []byte(scenario.wanted)...))
			for range 2 {
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
			}
			if state.mode != renderedMode || !state.rich.active || state.rich.input.String() != "Solo" {
				context.Fatal("moving the empty Source caret lost rendered Cut history")
			}
			expectRenderedCollisionSave(context, state, driver, filename, original)
		})
	}
}

func TestRenderedInitialEmptyDocumentEnterSaveAsAndSourceBranch(test *testing.T) {
	for _, scenario := range []struct {
		name, source, entered string
	}{
		{"empty", "", "\nQ"},
		{"line-endings", "\n\n", "\r\nQ\r\n\r\n"},
	} {
		test.Run(scenario.name, func(context *testing.T) {
			state, driver, filename, original := renderedSourceCollisionFixture(context, scenario.source, false, 740)
			clickID(context, driver, richInputID)
			if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
				context.Fatal(err)
			}
			if state.session.Current().Editor.CanUndo() || state.session.Current().Dirty() {
				context.Fatal("empty root Backspace created a source edit")
			}
			if err := driver.Key(gfx.KeyEnter, 0); err != nil {
				context.Fatal(err)
			}
			if err := driver.Type("Q"); err != nil {
				context.Fatal(err)
			}
			other := filepath.Join(context.TempDir(), "Saved.md")
			if err := state.session.Save(state.session.Active, other); err != nil {
				context.Fatal(err)
			}
			stored, err := os.ReadFile(other)
			if err != nil || !bytes.Equal(stored, append([]byte{0xef, 0xbb, 0xbf}, []byte(scenario.entered)...)) {
				context.Fatalf("empty document Save As bytes=%q error=%v", stored, err)
			}
			stored, err = os.ReadFile(filename)
			if err != nil || !bytes.Equal(stored, original) {
				context.Fatal("Save As modified the original empty document")
			}
			for range 2 {
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
			}
			if state.mode != renderedMode || !state.rich.active || state.session.Current().Text() != strings.ReplaceAll(scenario.source, "\n", "\r\n") {
				context.Fatal("Enter/typing Undo did not restore the original empty input")
			}
			clickID(context, driver, editModeID)
			if err := driver.Type("B"); err != nil {
				context.Fatal(err)
			}
			branch := state.session.Current().Text()
			if !strings.Contains(branch, "B") || state.session.Current().Editor.CanRedo() {
				context.Fatal("new Source edit retained obsolete empty-rendered Redo")
			}
			clickID(context, driver, renderedModeID)
			if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
				context.Fatal(err)
			}
			if state.session.Current().Text() != branch {
				context.Fatal("returning to rendered mode revived an old source branch")
			}
		})
	}
}

func TestRenderedPrePaintEmptyDeletionKeepsCommittedInputHistory(test *testing.T) {
	for _, scenario := range []struct {
		name, source string
		paste        bool
	}{
		{"paragraph-insert", "Solo\n", false},
		{"paragraph-paste", "Solo\n", true},
		{"initial-empty", "", false},
		{"initial-line-endings", "\n", false},
	} {
		test.Run(scenario.name, func(context *testing.T) {
			state, driver, filename, original := renderedSourceCollisionFixture(context, scenario.source, false, 740)
			if strings.HasPrefix(scenario.source, "Solo") {
				clickID(context, driver, renderedRows.At(0))
			} else {
				clickID(context, driver, richInputID)
			}
			state.rich.input.SetSelection(text.Range{Start: 0, End: state.rich.input.RuneCount()})
			kind := app.TextInsert
			if scenario.paste {
				kind = app.TextPaste
			}
			if _, err := state.onRenderedTextEvent(app.TextEvent{Kind: kind, Text: "New"}); err != nil {
				context.Fatal(err)
			}
			state.rich.input.SetSelection(text.Range{Start: 0, End: state.rich.input.RuneCount()})
			if _, err := state.onRenderedTextEvent(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextDeleteBackward}); err != nil {
				context.Fatal(err)
			}
			if _, err := state.onRenderedTextEvent(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextUndo}); err != nil {
				context.Fatal(err)
			}
			suffix := ""
			if strings.HasSuffix(scenario.source, "\n") {
				suffix = "\r\n"
			}
			if state.rich.input.String() != "New" || state.session.Current().Text() != "New"+suffix {
				context.Fatalf("pre-paint deletion lost committed input history: input=%q source=%q message=%q", state.rich.input.String(), state.session.Current().Text(), state.message)
			}
			if err := driver.Pump(); err != nil {
				context.Fatal(err)
			}
			expectRenderedCollisionSave(context, state, driver, filename, append([]byte{0xef, 0xbb, 0xbf}, []byte("New"+suffix)...))
			if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
				context.Fatal(err)
			}
			expectRenderedCollisionSave(context, state, driver, filename, original)
		})
	}
}
