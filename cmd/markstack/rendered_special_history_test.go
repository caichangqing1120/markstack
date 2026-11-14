package main

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/text"
)

func specialHistoryPrefix(editor string) string {
	if editor == "table" {
		return "| A | B |\n| --- | --- |\n| 1 | 2 |\n\n"
	}
	return "```go\ncode\n```\n\n"
}

func activateHistorySpecial(context *testing.T, state *model, driver *app.Driver, editor string) {
	context.Helper()
	current := state.session.Current()
	before, selection := current.Text(), current.Editor.Selection()
	canUndo, canRedo, dirty := current.Editor.CanUndo(), current.Editor.CanRedo(), current.Dirty()
	focus := tableInputID
	if editor == "table" {
		clickID(context, driver, renderedTableCells.At(0))
		if !state.table.active || state.table.input == nil {
			context.Fatal("table history fixture did not activate the actual cell")
		}
	} else {
		index := -1
		for position, block := range state.parsed.Blocks {
			if block.Kind == markdown.Code {
				index = position
				break
			}
		}
		if index < 0 {
			context.Fatal("fence history fixture has no actual code block")
		}
		clickID(context, driver, renderedRows.At(index))
		focus = codeInputID
		if !state.codeBlock.active || state.codeBlock.input == nil {
			context.Fatal("fence history fixture did not activate the actual code input")
		}
	}
	if state.rich.active || state.rich.input != nil || state.selection.active || driver.Snapshot().Focused != focus || current.Text() != before || current.Editor.Selection() != selection || current.Editor.CanUndo() != canUndo || current.Editor.CanRedo() != canRedo || current.Dirty() != dirty {
		context.Fatal("special-editor navigation retained hidden input or changed Source/history")
	}
}

func activateSpecialHistoryTarget(context *testing.T, state *model, driver *app.Driver) {
	context.Helper()
	for index, block := range state.parsed.Blocks {
		if block.Kind == markdown.Heading && renderedPlain(&state.parsed, index) == "Target" {
			clickID(context, driver, renderedRows.At(index))
			if state.mode != renderedMode || !state.rich.active || state.rich.input.String() != "Target" || driver.Snapshot().Focused != richInputID {
				context.Fatal("special-editor history fixture did not return to the actual heading")
			}
			return
		}
	}
	context.Fatal("special-editor history fixture has no target heading")
}

func TestRenderedSpecialNavigationRetainsOwnedHistory(test *testing.T) {
	for _, editor := range []string{"table", "fence"} {
		for _, kind := range []string{"split", "virtual", "format", "cross", "selection", "redo", "virtual-text"} {
			for _, dark := range []bool{false, true} {
				for _, width := range []int{740, 1200} {
					test.Run(fmt.Sprintf("%s/%s/dark-%v/width-%d", editor, kind, dark, width), func(context *testing.T) {
						prefix := specialHistoryPrefix(editor)
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
						for range 2 {
							if kind == "selection" {
								if err := driver.Key(gfx.KeyUp, gfx.ModifierShift); err != nil {
									context.Fatal(err)
								}
								if !state.selection.active {
									context.Fatal("special-editor selection carrier fixture did not activate")
								}
							}
							activateHistorySpecial(context, state, driver, editor)
							activateSpecialHistoryTarget(context, state, driver)
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
									context.Fatalf("special-editor navigation lost owned history: command=%c mode=%v active=%v row=%d", command, state.mode, state.rich.active, state.rich.index)
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
							context.Fatal("special-editor navigation invented extra Source history")
						}
					})
				}
			}
		}
	}
}

func TestRenderedSpecialHistoryInterleavesSourceUnits(test *testing.T) {
	for _, editor := range []string{"table", "fence"} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{740, 1200} {
				test.Run(fmt.Sprintf("%s/dark-%v/width-%d", editor, dark, width), func(context *testing.T) {
					state, driver, filename, original := renderedSourceCollisionFixture(context, specialHistoryPrefix(editor)+"abcd\n\n## Target\n", dark, width)
					clickID(context, driver, renderedRows.At(1))
					state.rich.input.MoveTo(2, false)
					if err := driver.Key(gfx.KeyEnter, 0); err != nil {
						context.Fatal(err)
					}
					current := state.session.Current()
					afterSplit := current.Text()
					activateHistorySpecial(context, state, driver, editor)
					input, focus := state.table.input, tableInputID
					if editor == "fence" {
						input, focus = state.codeBlock.input, codeInputID
					}
					beforeText := input.String()
					input.MoveTo(utf8.RuneCountInString(beforeText), false)
					if err := driver.Type("X"); err != nil {
						context.Fatal(err)
					}
					afterSpecial := current.Text()
					beforeFragment, afterFragment := "| A |", "| AX |"
					if editor == "fence" {
						beforeFragment, afterFragment = "code\r\n", "codeX\r\n"
					}
					if afterSpecial != strings.Replace(afterSplit, beforeFragment, afterFragment, 1) {
						context.Fatal("special-editor native edit fixture changed the wrong Source interval")
					}
					activateSpecialHistoryTarget(context, state, driver)
					for range 2 {
						for _, step := range []struct {
							command rune
							source  string
							special bool
							value   string
							caret   int
						}{
							{'z', afterSplit, true, beforeText, utf8.RuneCountInString(beforeText)},
							{'z', string(original[3:]), false, "abcd", 2},
							{'y', afterSplit, false, "cd", 0},
							{'y', afterSpecial, true, beforeText + "X", utf8.RuneCountInString(beforeText) + 1},
						} {
							if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
								context.Fatal(err)
							}
							activeInput, activeFocus, active := state.rich.input, richInputID, state.rich.active
							if step.special {
								activeInput, activeFocus, active = state.table.input, focus, state.table.active
								if editor == "fence" {
									activeInput, active = state.codeBlock.input, state.codeBlock.active
								}
							}
							if current.Text() != step.source || state.mode != renderedMode || !active || activeInput == nil || activeInput.String() != step.value || activeInput.Caret() != step.caret || driver.Snapshot().Focused != activeFocus {
								context.Fatalf("interleaved special history lost actual Source target: command=%c mode=%v", step.command, state.mode)
							}
							expectRenderedCollisionSave(context, state, driver, filename, append([]byte{0xef, 0xbb, 0xbf}, []byte(step.source)...))
						}
					}
					for range 2 {
						if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
							context.Fatal(err)
						}
					}
					expectRenderedCollisionSave(context, state, driver, filename, original)
					if current.Editor.CanUndo() {
						context.Fatal("interleaved special history invented extra Source units")
					}
				})
			}
		}
	}
}

func TestRenderedSpecialHistoryUsesSourceCaret(test *testing.T) {
	for _, editor := range []string{"table", "fence"} {
		values := []string{"A", "aaaa", "a" + string(rune(0x4e2d)) + "b"}
		if editor == "fence" {
			values = []string{"code", "aaaa", "a" + string(rune(0x4e2d)) + "\nb"}
		}
		for _, value := range values {
			for _, caret := range []int{0, utf8.RuneCountInString(value) / 2, utf8.RuneCountInString(value)} {
				test.Run(fmt.Sprintf("%s/%q/caret-%d", editor, value, caret), func(context *testing.T) {
					prefix := specialHistoryPrefix(editor)
					if editor == "table" {
						prefix = strings.Replace(prefix, "| A |", "| "+value+" |", 1)
					} else {
						prefix = strings.Replace(prefix, "code", value, 1)
					}
					state, driver, filename, original := renderedSourceCollisionFixture(context, prefix+"abcd\n\n## Target\n", false, 1200)
					activateHistorySpecial(context, state, driver, editor)
					input := state.table.input
					if editor == "fence" {
						input = state.codeBlock.input
					}
					if input.String() != value {
						context.Fatal("special-editor caret fixture has the wrong visible text")
					}
					input.MoveTo(caret, false)
					caret = input.Caret()
					if err := driver.Type("X"); err != nil {
						context.Fatal(err)
					}
					changed := state.session.Current().Text()
					for range 2 {
						for _, step := range []struct {
							command rune
							caret   int
							source  string
						}{
							{'z', caret, string(original[3:])},
							{'y', caret + 1, changed},
						} {
							if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
								context.Fatal(err)
							}
							activeInput, active, focus := state.table.input, state.table.active, tableInputID
							if editor == "fence" {
								activeInput, active, focus = state.codeBlock.input, state.codeBlock.active, codeInputID
							}
							if state.mode != renderedMode || !active || activeInput == nil || activeInput.Selection() != (text.Range{Start: step.caret, End: step.caret}) || driver.Snapshot().Focused != focus || state.session.Current().Text() != step.source {
								context.Fatalf("special-editor Source history lost actual caret: command=%c wanted=%d", step.command, step.caret)
							}
							expectRenderedCollisionSave(context, state, driver, filename, append([]byte{0xef, 0xbb, 0xbf}, []byte(step.source)...))
						}
					}
				})
			}
		}
	}
}

func TestRenderedSpecialHistoryChangesNativeTargets(test *testing.T) {
	for _, scenario := range []string{"table-fence", "fence-table", "table-cell", "table-copy", "fence-copy"} {
		for _, caret := range []int{0, 2, 4} {
			for _, dark := range []bool{false, true} {
				for _, width := range []int{740, 1200} {
					test.Run(fmt.Sprintf("%s/caret-%d/dark-%v/width-%d", scenario, caret, dark, width), func(context *testing.T) {
						tableSource := "| ab\u4e2dd | keep |\n| --- | --- |\n| 1 | 2 |\n\n"
						codeSource := "```go\nab\u4e2dd\n```\n\n"
						edited, source := "table", tableSource+codeSource
						if scenario == "fence-table" {
							edited = "fence"
						} else if scenario == "fence-copy" {
							edited, source = "fence", codeSource+codeSource
						} else if scenario == "table-copy" {
							source = tableSource + tableSource
						}
						state, driver, filename, original := renderedSourceCollisionFixture(context, source+"## Target\n", dark, width)
						activateHistorySpecial(context, state, driver, edited)
						input, focus := specialHistoryInput(state, edited)
						before := input.String()
						targetIndex := state.table.renderedIndex
						if edited == "fence" {
							targetIndex = state.codeBlock.renderedIndex
						}
						input.MoveTo(caret, false)
						if err := driver.Type("X"); err != nil {
							context.Fatal(err)
						}
						current := state.session.Current()
						changed := append([]byte{0xef, 0xbb, 0xbf}, []byte(current.Text())...)
						position := byteOffsetOfRune(before, caret)
						after := before[:position] + "X" + before[position:]
						for range 2 {
							for _, command := range []rune{'z', 'y'} {
								switch scenario {
								case "table-fence":
									activateHistorySpecial(context, state, driver, "fence")
								case "fence-table":
									activateHistorySpecial(context, state, driver, "table")
								case "table-cell":
									clickID(context, driver, renderedTableCells.At(1))
									if !state.table.active || state.table.column != 1 {
										context.Fatal("other-cell history fixture did not activate")
									}
								case "table-copy":
									clickID(context, driver, renderedTableCells.At(4))
									if !state.table.active || state.table.renderedIndex != 1 {
										context.Fatal("duplicate-table history fixture did not activate")
									}
								case "fence-copy":
									clickID(context, driver, renderedRows.At(1))
									if !state.codeBlock.active || state.codeBlock.renderedIndex != 1 {
										context.Fatal("duplicate-fence history fixture did not activate")
									}
								}
								if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
									context.Fatal(err)
								}
								wanted, value, wantedCaret := original, before, caret
								if command == 'y' {
									wanted, value, wantedCaret = changed, after, caret+1
								}
								expectRenderedCollisionSave(context, state, driver, filename, wanted)
								input, _ = specialHistoryInput(state, edited)
								active, index := state.table.active && state.table.row == 0 && state.table.column == 0, state.table.renderedIndex
								if edited == "fence" {
									active, index = state.codeBlock.active, state.codeBlock.renderedIndex
								}
								if state.mode != renderedMode || !active || index != targetIndex || input == nil || input.String() != value || input.Selection() != (text.Range{Start: wantedCaret, End: wantedCaret}) || driver.Snapshot().Focused != focus {
									context.Fatalf("native history handoff lost actual target: command=%c mode=%v active=%v index=%d", command, state.mode, active, index)
								}
								if current.Editor.CanUndo() != (command == 'y') || current.Editor.CanRedo() != (command == 'z') {
									context.Fatal("native history handoff added or discarded Source units")
								}
							}
						}
						if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
							context.Fatal(err)
						}
						expectRenderedCollisionSave(context, state, driver, filename, original)
					})
				}
			}
		}
	}
}
