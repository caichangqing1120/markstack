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

func tabHistoryRoundTrip(context *testing.T, state *model, driver *app.Driver, otherKind string, reopen bool) {
	context.Helper()
	current, tab := state.session.Current(), state.session.Active
	before, selection, caret := current.Text(), current.Editor.Selection(), current.Editor.Caret()
	dirty, canUndo, canRedo := current.Dirty(), current.Editor.CanUndo(), current.Editor.CanRedo()
	extension, body := ".md", "# Other\n\nkeep\n"
	switch otherKind {
	case "plain":
		extension, body = ".txt", "Other text\n"
	case "code":
		extension, body = ".go", "package other\n"
	case "rich-html":
		extension, body = ".mh", "<p>Other</p>\n"
	}
	other := filepath.Join(filepath.Dir(current.Path), "other"+extension)
	otherBytes := []byte(body)
	if _, err := os.Stat(other); os.IsNotExist(err) {
		if err := os.WriteFile(other, otherBytes, 0640); err != nil {
			context.Fatal(err)
		}
	}
	if !state.openFile(other) {
		context.Fatal("tab fixture did not open its other document")
	}
	state.invalidator.Invalidate()
	if err := driver.Pump(); err != nil {
		context.Fatal(err)
	}
	if state.session.Current() == current || state.session.Current().Path != other || state.rich.active || state.rich.input != nil || state.table.input != nil || state.codeBlock.input != nil || state.selection.active {
		context.Fatal("tab navigation retained an inactive native editor or failed to switch documents")
	}
	if reopen {
		if !state.openFile(current.Path) {
			context.Fatal("tab fixture did not reopen the existing document")
		}
		state.invalidator.Invalidate()
		if err := driver.Pump(); err != nil {
			context.Fatal(err)
		}
	} else {
		clickID(context, driver, tabRows.At(tab))
	}
	if state.session.Current() != current || current.Text() != before || current.Editor.Selection() != selection || current.Editor.Caret() != caret || current.Dirty() != dirty || current.Editor.CanUndo() != canUndo || current.Editor.CanRedo() != canRedo {
		context.Fatal("tab traversal changed actual Source identity, selection, dirty state or history")
	}
	if actual, err := os.ReadFile(other); err != nil || !bytes.Equal(actual, otherBytes) {
		context.Fatal("tab traversal changed the other document")
	}
	if state.mode != renderedMode {
		clickID(context, driver, renderedModeID)
	}
	activateSpecialHistoryTarget(context, state, driver)
}

func TestRenderedTabNavigationRetainsOwnedHistory(test *testing.T) {
	for _, kind := range []string{"split", "virtual", "format", "cross", "selection", "redo", "virtual-text"} {
		for _, otherKind := range []string{"markdown", "plain", "code", "rich-html"} {
			for _, dark := range []bool{false, true} {
				for _, width := range []int{740, 1200} {
					test.Run(fmt.Sprintf("%s/%s/dark-%v/width-%d", kind, otherKind, dark, width), func(context *testing.T) {
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
						}
						state, driver, filename, original := renderedSourceCollisionFixture(context, prefix+body+suffix, dark, width)
						clickID(context, driver, renderedRows.At(1))
						if kind == "format" {
							state.rich.input.SetSelection(beforeSelection)
							state.applyBoldBlock()
							state.invalidator.Invalidate()
							if err := driver.Pump(); err != nil {
								context.Fatal(err)
							}
						} else if kind == "cross" {
							state.rich.input.SetSelection(beforeSelection)
							if err := driver.Type("X"); err != nil {
								context.Fatal(err)
							}
						} else {
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
								if err := driver.Key(gfx.KeyUp, gfx.ModifierShift); err != nil {
									context.Fatal(err)
								}
								if !state.selection.active {
									context.Fatal("tab history selection fixture did not activate")
								}
							}
							tabHistoryRoundTrip(context, state, driver, otherKind, cycle == 1)
							commands := []rune{'z', 'y'}
							if kind == "redo" {
								commands = []rune{'y', 'z'}
							}
							for _, command := range commands {
								if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
									context.Fatal(err)
								}
								wantedBody, row, selection, virtual := beforeBody, beforeRow, beforeSelection, beforeVirtual
								if command == 'y' {
									wantedBody, row, selection, virtual = afterBody, afterRow, afterSelection, afterVirtual
								}
								ordered := text.Range{Start: min(selection.Start, selection.End), End: max(selection.Start, selection.End)}
								if state.mode != renderedMode || !state.rich.active || state.rich.virtual != virtual || state.rich.index != row || state.rich.input.Selection() != ordered || state.rich.input.Caret() != selection.End || driver.Snapshot().Focused != richInputID {
									context.Fatalf("tab navigation lost owned history target: command=%c mode=%v active=%v row=%d", command, state.mode, state.rich.active, state.rich.index)
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
							context.Fatal("tab navigation invented extra Source history")
						}
					})
				}
			}
		}
	}
}

func TestRenderedTabNavigationRetainsSpecialSourceCoordinate(test *testing.T) {
	for _, editor := range []string{"table", "fence"} {
		for _, otherKind := range []string{"markdown", "plain", "code", "rich-html"} {
			for _, dark := range []bool{false, true} {
				for _, width := range []int{740, 1200} {
					test.Run(fmt.Sprintf("%s/%s/dark-%v/width-%d", editor, otherKind, dark, width), func(context *testing.T) {
						state, driver, filename, original := renderedSourceCollisionFixture(context, specialHistoryPrefix(editor)+"## Target\n", dark, width)
						activateHistorySpecial(context, state, driver, editor)
						input, focus := specialHistoryInput(state, editor)
						before := input.String()
						caret := min(1, input.RuneCount())
						input.MoveTo(caret, false)
						if err := driver.Type("X"); err != nil {
							context.Fatal(err)
						}
						current := state.session.Current()
						changed := append([]byte{0xef, 0xbb, 0xbf}, []byte(current.Text())...)
						position := byteOffsetOfRune(before, caret)
						after := before[:position] + "X" + before[position:]
						for cycle := range 2 {
							tabHistoryRoundTrip(context, state, driver, otherKind, cycle == 1)
							for _, command := range []rune{'z', 'y'} {
								if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
									context.Fatal(err)
								}
								wanted, value, wantedCaret, coordinate := original, before, caret, 0
								if command == 'y' {
									wanted, value, wantedCaret, coordinate = changed, after, caret+1, 1
								}
								input, _ = specialHistoryInput(state, editor)
								active := state.table.active
								if editor == "fence" {
									active = state.codeBlock.active
								}
								if state.mode != renderedMode || !active || input == nil || input.String() != value || input.Caret() != wantedCaret || driver.Snapshot().Focused != focus || state.rich.appliedEdits+state.rich.sourceHistoryOffset != coordinate {
									context.Fatal("tab traversal lost special target, caret or actual Source coordinate")
								}
								expectRenderedCollisionSave(context, state, driver, filename, wanted)
							}
						}
						if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
							context.Fatal(err)
						}
						expectRenderedCollisionSave(context, state, driver, filename, original)
						if current.Editor.CanUndo() {
							context.Fatal("special tab navigation invented Source history")
						}
					})
				}
			}
		}
	}
}
