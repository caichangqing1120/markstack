package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestRenderedDocumentShortcutRestoresSpecialHistory(test *testing.T) {
	for _, editor := range []string{"table", "fence"} {
		for _, reopen := range []bool{false, true} {
			for _, dark := range []bool{false, true} {
				for _, width := range []int{740, 1200} {
					test.Run(fmt.Sprintf("%s/reopen-%v/dark-%v/width-%d", editor, reopen, dark, width), func(context *testing.T) {
						state, driver, filename, original := renderedSourceCollisionFixture(context, specialHistoryPrefix(editor)+"## Target\n", dark, width)
						activateHistorySpecial(context, state, driver, editor)
						input, focus := specialHistoryInput(state, editor)
						before := input.String()
						caret := input.RuneCount()
						input.MoveTo(caret, false)
						if err := driver.Type("Q"); err != nil {
							context.Fatal(err)
						}
						changed := append([]byte{0xef, 0xbb, 0xbf}, []byte(state.session.Current().Text())...)
						for cycle := range 2 {
							for _, command := range []rune{'z', 'y'} {
								navigateDocumentWithoutInput(context, state, driver, reopen)
								key, modifiers := command, primaryModifier()
								if command == 'y' && cycle == 1 {
									key, modifiers = 'z', modifiers|gfx.ModifierShift
								}
								if handled, err := driver.ShortcutChord(modifiers, key); err != nil || !handled {
									context.Fatal("inactive special history command was not handled")
								}
								input, _ = specialHistoryInput(state, editor)
								wanted, position, saved := before, caret, original
								if command == 'y' {
									wanted, position, saved = before+"Q", caret+1, changed
								}
								if state.mode != renderedMode || input == nil || input.String() != wanted || input.Caret() != position || driver.Snapshot().Focused != focus {
									context.Fatal("inactive shortcut did not restore the actual table or fence target")
								}
								expectRenderedCollisionSave(context, state, driver, filename, saved)
							}
						}
						if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
							context.Fatal(err)
						}
						expectRenderedCollisionSave(context, state, driver, filename, original)
						if state.session.Current().Editor.CanUndo() {
							context.Fatal("inactive special shortcut introduced an extra Source unit")
						}
					})
				}
			}
		}
	}
}

func TestRenderedDocumentShortcutPreservesEmptyHistory(test *testing.T) {
	state, driver, filename, original := renderedSourceCollisionFixture(test, "abcd\n\n## Target\n", false, 1200)
	current := state.session.Current()
	selection, focus := current.Editor.Selection(), driver.Snapshot().Focused
	for _, command := range []struct {
		key       rune
		modifiers gfx.Modifiers
	}{{'z', primaryModifier()}, {'y', primaryModifier()}, {'z', primaryModifier() | gfx.ModifierShift}} {
		if _, err := driver.ShortcutChord(command.modifiers, command.key); err != nil {
			test.Fatal(err)
		}
		if current.Text() != string(original[3:]) || current.Dirty() || current.Editor.Selection() != selection || current.Editor.CanUndo() || current.Editor.CanRedo() || state.mode != renderedMode || state.rich.input != nil || driver.Snapshot().Focused != focus {
			test.Fatal("empty document history created a unit, activated an input or moved focus")
		}
	}
	expectRenderedCollisionSave(test, state, driver, filename, original)
}

func TestRenderedDocumentShortcutPreservesOtherModifiers(test *testing.T) {
	state, driver, _, _ := renderedSourceCollisionFixture(test, "abcd\n\n## Target\n", false, 1200)
	clickID(test, driver, renderedRows.At(0))
	state.rich.input.MoveTo(2, false)
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		test.Fatal(err)
	}
	navigateDocumentWithoutInput(test, state, driver, false)
	current := state.session.Current()
	before, selection := current.Text(), current.Editor.Selection()
	for _, command := range []struct {
		key       rune
		modifiers gfx.Modifiers
	}{{'z', 0}, {'z', primaryModifier() | gfx.ModifierAlt}, {'y', primaryModifier() | gfx.ModifierShift}} {
		if handled, err := driver.ShortcutChord(command.modifiers, command.key); err != nil || handled {
			test.Fatal("document history accepted an unrelated modifier combination")
		}
		if current.Text() != before || current.Editor.Selection() != selection || !current.Editor.CanUndo() || current.Editor.CanRedo() || state.rich.input != nil || state.mode != renderedMode {
			test.Fatal("unrelated shortcut changed document history")
		}
	}
}

func TestRenderedDocumentShortcutKeepsPendingSourceComposition(test *testing.T) {
	state, _, filename, original := renderedSourceCollisionFixture(test, "abcd\n\n## Target\n", false, 1200)
	current := state.session.Current()
	current.Editor.MoveTo(2, false)
	current.Editor.Insert("P")
	current.Editor.SetMarkedText("ni", text.Range{Start: 2, End: 2})
	before := current.Editor.CommittedString()
	marked, composing := current.Editor.Composition()
	if !composing {
		test.Fatal("composition fixture did not create marked text")
	}
	result := state.onShortcut(gfx.KeyEvent{Text: "z", Modifiers: primaryModifier()})
	if after, stillComposing := current.Editor.Composition(); !result.Handled || result.Focus != sourceID || !stillComposing || after != marked || current.Editor.CommittedString() != before {
		test.Fatal("unfocused document shortcut committed or undid a pending Source composition")
	}
	current.Editor.CancelComposition()
	if actual, err := os.ReadFile(filename); err != nil || !bytes.Equal(actual, original) {
		test.Fatal("composition shortcut wrote marked text to disk")
	}
}

func TestRenderedDocumentShortcutKeepsSourceFallbackSelection(test *testing.T) {
	state, driver, filename, original := renderedSourceCollisionFixture(test, "# Title\n\nTail keep\n", false, 1200)
	clickID(test, driver, editModeID)
	clickID(test, driver, sourceID)
	current := state.session.Current()
	current.Editor.SetSelection(text.Range{Start: 0, End: 1})
	if err := driver.Type("X"); err != nil {
		test.Fatal(err)
	}
	clickID(test, driver, renderedModeID)
	if state.rich.input != nil || current.Text() != "X Title\r\n\r\nTail keep\r\n" {
		test.Fatal("Source fallback fixture did not change the hidden heading marker")
	}
	if handled, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil || !handled {
		test.Fatal("unsupported inactive history was not handled")
	}
	if state.mode != editMode || state.rich.input != nil || current.Text() != string(original[3:]) || current.Editor.Selection() != (text.Range{Start: 0, End: 1}) || current.Editor.Caret() != 1 || driver.Snapshot().Focused != sourceID {
		test.Fatal("Source fallback lost the actual hidden-syntax selection or consumed another unit")
	}
	expectRenderedCollisionSave(test, state, driver, filename, original)
}

func TestRenderedDocumentShortcutRejectsStaleInactiveAnnotations(test *testing.T) {
	for _, kind := range []string{"structure", "cross"} {
		test.Run(kind, func(context *testing.T) {
			state, driver, filename, original := renderedSourceCollisionFixture(context, "abcd\n\n## Target\n", false, 1200)
			current, tab := state.session.Current(), state.session.Active
			clickID(context, driver, renderedRows.At(0))
			if kind == "structure" {
				state.rich.input.MoveTo(2, false)
				if err := driver.Key(gfx.KeyEnter, 0); err != nil {
					context.Fatal(err)
				}
			} else {
				state.rich.input.SetSelection(text.Range{Start: 3, End: 1})
				state.applyBoldBlock()
				state.invalidator.Invalidate()
				if err := driver.Pump(); err != nil {
					context.Fatal(err)
				}
			}
			if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
				context.Fatal(err)
			}
			other := filepath.Join(filepath.Dir(filename), "stale-other.md")
			if err := os.WriteFile(other, []byte("# Other\n"), 0640); err != nil || !state.openFile(other) {
				context.Fatal("stale shortcut fixture did not open its other document")
			}
			state.invalidator.Invalidate()
			if err := driver.Pump(); err != nil {
				context.Fatal(err)
			}
			position := strings.Index(current.Text(), "Target") + len("Target")
			current.Editor.MoveTo(utf8.RuneCountInString(current.Text()[:position]), false)
			current.Editor.Insert("P")
			if !current.Editor.Undo() || current.Text() != string(original[3:]) {
				context.Fatal("stale shortcut fixture did not return a different branch to the same bytes")
			}
			selection, caret := current.Editor.Selection(), current.Editor.Caret()
			clickID(context, driver, tabRows.At(tab))
			if state.rich.input != nil || len(state.rich.structureRedo)+len(state.rich.crossRedo) != 1 {
				context.Fatal("stale shortcut fixture lost its inactive annotation")
			}
			if handled, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil || !handled {
				context.Fatal("stale annotation command was not handled safely")
			}
			wanted := original
			if kind == "structure" {
				wanted = bytes.Replace(original, []byte("Target"), []byte("TargetP"), 1)
				if state.mode != editMode || driver.Snapshot().Focused != sourceID || !current.Editor.CanUndo() || current.Editor.CanRedo() {
					context.Fatal("stale structure annotation did not preserve the true Source Redo and explicit fallback")
				}
			} else if state.mode != renderedMode || state.rich.input != nil || current.Editor.CanUndo() || !current.Editor.CanRedo() || current.Editor.Selection() != selection || current.Editor.Caret() != caret || state.message == "" {
				context.Fatal("rejected stale cross annotation consumed history, moved Source selection or created an input")
			}
			expectRenderedCollisionSave(context, state, driver, filename, wanted)
		})
	}
}
