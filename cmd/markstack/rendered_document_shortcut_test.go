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

func navigateDocumentWithoutInput(context *testing.T, state *model, driver *app.Driver, reopen bool) {
	context.Helper()
	current, tab := state.session.Current(), state.session.Active
	before, selection := current.Text(), current.Editor.Selection()
	other := filepath.Join(filepath.Dir(current.Path), "other-shortcut.md")
	if err := os.WriteFile(other, []byte("# Other\n"), 0640); err != nil {
		context.Fatal(err)
	}
	if !state.openFile(other) {
		context.Fatal("shortcut fixture did not open its other document")
	}
	state.invalidator.Invalidate()
	if err := driver.Pump(); err != nil {
		context.Fatal(err)
	}
	if reopen {
		if !state.openFile(current.Path) {
			context.Fatal("shortcut fixture did not reopen the existing document")
		}
		state.invalidator.Invalidate()
		if err := driver.Pump(); err != nil {
			context.Fatal(err)
		}
	} else {
		clickID(context, driver, tabRows.At(tab))
	}
	if state.session.Current() != current || current.Text() != before || current.Editor.Selection() != selection || state.mode != renderedMode || state.rich.input != nil || state.table.input != nil || state.codeBlock.input != nil {
		context.Fatal("shortcut fixture did not return to the unchanged document without an active input")
	}
}

func TestRenderedDocumentShortcutRestoresOwnedHistory(test *testing.T) {
	for _, kind := range []string{"split", "virtual", "virtual-text", "format", "cross", "selection", "redo", "typing"} {
		for _, reopen := range []bool{false, true} {
			for _, dark := range []bool{false, true} {
				for _, width := range []int{740, 1200} {
					test.Run(fmt.Sprintf("%s/reopen-%v/dark-%v/width-%d", kind, reopen, dark, width), func(context *testing.T) {
						const prefix = "<!-- untouched -->\n\n"
						const suffix = "\n\n## Target\n\nTail **keep** &amp; <!-- opaque -->\n"
						body, beforeBody, afterBody := "abcd", "abcd", "ab\n\ncd"
						beforeRow, afterRow := 1, 2
						beforeSelection, afterSelection := text.Range{Start: 2, End: 2}, text.Range{}
						beforeVirtual, afterVirtual := false, false
						switch kind {
						case "virtual":
							afterBody, afterVirtual = "abcd\n\n", true
							beforeSelection = text.Range{Start: 4, End: 4}
						case "virtual-text":
							beforeBody, afterBody = "abcd\n\n", "abcd\n\nQ"
							beforeRow, beforeVirtual, afterVirtual = 2, true, true
							beforeSelection, afterSelection = text.Range{}, text.Range{Start: 1, End: 1}
						case "format":
							afterBody, afterRow = "a**bc**d", 1
							beforeSelection, afterSelection = text.Range{Start: 3, End: 1}, text.Range{Start: 3, End: 1}
						case "cross":
							body, beforeBody, afterBody = "ab **cd** efgh", "ab **cd** efgh", "aXgh"
							afterRow = 1
							beforeSelection, afterSelection = text.Range{Start: 8, End: 1}, text.Range{Start: 2, End: 2}
						case "typing":
							afterBody, afterRow = "abcdQ", 1
							beforeSelection, afterSelection = text.Range{Start: 4, End: 4}, text.Range{Start: 5, End: 5}
						}
						state, driver, filename, original := renderedSourceCollisionFixture(context, prefix+body+suffix, dark, width)
						clickID(context, driver, renderedRows.At(1))
						switch kind {
						case "format":
							state.rich.input.SetSelection(beforeSelection)
							state.applyBoldBlock()
							state.invalidator.Invalidate()
							if err := driver.Pump(); err != nil {
								context.Fatal(err)
							}
						case "cross":
							state.rich.input.SetSelection(beforeSelection)
							if err := driver.Type("X"); err != nil {
								context.Fatal(err)
							}
						case "typing":
							state.rich.input.MoveTo(4, false)
							if err := driver.Type("Q"); err != nil {
								context.Fatal(err)
							}
						default:
							caret := 2
							if kind == "virtual" || kind == "virtual-text" {
								caret = 4
							}
							state.rich.input.MoveTo(caret, false)
							if err := driver.Key(gfx.KeyEnter, 0); err != nil {
								context.Fatal(err)
							}
							if kind == "virtual-text" {
								if err := driver.Type("Q"); err != nil {
									context.Fatal(err)
								}
							} else if kind == "redo" {
								if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
									context.Fatal(err)
								}
							}
						}
						for cycle := range 2 {
							if kind == "selection" {
								if err := driver.Key(gfx.KeyUp, gfx.ModifierShift); err != nil || !state.selection.active {
									context.Fatal("shortcut fixture did not create a cross-block selection")
								}
							}
							commands := []rune{'z', 'y'}
							if kind == "redo" {
								commands = []rune{'y', 'z'}
							}
							for _, command := range commands {
								navigateDocumentWithoutInput(context, state, driver, reopen)
								key, modifiers := command, primaryModifier()
								if command == 'y' && cycle == 1 {
									key, modifiers = 'z', modifiers|gfx.ModifierShift
								}
								if handled, err := driver.ShortcutChord(modifiers, key); err != nil || !handled {
									context.Fatalf("inactive document did not handle its history command: %v", err)
								}
								wantedBody, row, selection, virtual := beforeBody, beforeRow, beforeSelection, beforeVirtual
								if command == 'y' {
									wantedBody, row, selection, virtual = afterBody, afterRow, afterSelection, afterVirtual
								}
								ordered := text.Range{Start: min(selection.Start, selection.End), End: max(selection.Start, selection.End)}
								if state.mode != renderedMode || !state.rich.active || state.rich.virtual != virtual || state.rich.index != row || state.rich.input.Selection() != ordered || state.rich.input.Caret() != selection.End || driver.Snapshot().Focused != richInputID {
									context.Fatalf("inactive shortcut lost the actual history target: command=%c mode=%v active=%v row=%d", command, state.mode, state.rich.active, state.rich.index)
								}
								wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(prefix+wantedBody+suffix, "\n", "\r\n"))...)
								expectRenderedCollisionSave(context, state, driver, filename, wanted)
							}
						}
						if kind != "redo" {
							if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
								context.Fatal(err)
							}
						}
						if kind == "virtual-text" {
							if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
								context.Fatal(err)
							}
						}
						expectRenderedCollisionSave(context, state, driver, filename, original)
						if state.session.Current().Editor.CanUndo() {
							context.Fatal("inactive shortcuts introduced an extra Source history unit")
						}
					})
				}
			}
		}
	}
}

func TestRenderedDocumentShortcutLeavesTextInputRouting(test *testing.T) {
	for _, target := range []gfx.ID{sourceID, richInputID, tableInputID, codeInputID, htmlInputID, searchID, workspaceQueryID, treeNameID, findInputID, replaceInputID, linkInputID} {
		test.Run(fmt.Sprint(target), func(context *testing.T) {
			state, driver, _, _ := renderedSourceCollisionFixture(context, "abcd\n\n## Target\n", false, 1200)
			clickID(context, driver, renderedRows.At(0))
			state.rich.input.MoveTo(2, false)
			if err := driver.Key(gfx.KeyEnter, 0); err != nil {
				context.Fatal(err)
			}
			navigateDocumentWithoutInput(context, state, driver, false)
			current := state.session.Current()
			before, selection := current.Text(), current.Editor.Selection()
			for _, key := range []rune{'z', 'y'} {
				result := state.onShortcut(gfx.KeyEvent{Target: target, Text: string(key), Modifiers: primaryModifier()})
				if result.Handled || current.Text() != before || current.Editor.Selection() != selection || !current.Editor.CanUndo() || current.Editor.CanRedo() {
					context.Fatal("document shortcut consumed a native input's history or changed Source")
				}
			}
		})
	}
}

func TestRenderedDocumentShortcutKeepsFocusedFieldHistory(test *testing.T) {
	for _, field := range []string{"filter", "find"} {
		for _, compose := range []bool{false, true} {
			test.Run(fmt.Sprintf("%s/compose-%v", field, compose), func(context *testing.T) {
				state, driver, filename, original := renderedSourceCollisionFixture(context, "abcd\n\n## Target\n", false, 1200)
				clickID(context, driver, renderedRows.At(0))
				state.rich.input.MoveTo(2, false)
				if err := driver.Key(gfx.KeyEnter, 0); err != nil {
					context.Fatal(err)
				}
				navigateDocumentWithoutInput(context, state, driver, false)
				focus, editor := searchID, state.search
				if field == "find" {
					state.openDocumentFind()
					focus, editor = findInputID, state.findEditor
					state.invalidator.Invalidate()
					if err := driver.Pump(); err != nil {
						context.Fatal(err)
					}
				}
				clickID(context, driver, focus)
				current := state.session.Current()
				before, selection := current.Text(), current.Editor.Selection()
				var err error
				if compose {
					err = driver.Compose("ni")
				} else {
					err = driver.Type("unmatched-query")
				}
				if err != nil {
					context.Fatal(err)
				}
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
				if _, composing := editor.Composition(); composing || editor.String() != "" || driver.Snapshot().Focused != focus || current.Text() != before || current.Editor.Selection() != selection || !current.Editor.CanUndo() || current.Editor.CanRedo() {
					context.Fatal("document history stole the focused field's Undo or composition cancel")
				}
				if actual, err := os.ReadFile(filename); err != nil || !bytes.Equal(actual, original) {
					context.Fatal("auxiliary field history changed the original file")
				}
			})
		}
	}
}
