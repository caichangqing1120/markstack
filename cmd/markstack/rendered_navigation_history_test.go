package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestRenderedNavigationRetainsStructureHistory(test *testing.T) {
	for _, navigation := range []string{"pointer", "keyboard", "outline", "find", "modes", "selection"} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{740, 1200} {
				test.Run(fmt.Sprintf("%s/dark-%v/width-%d", navigation, dark, width), func(context *testing.T) {
					const prefix = "<!-- untouched -->\n\n"
					const suffix = "\n\n## Target\n\nTail **keep** &amp; <!-- opaque -->\n"
					state, driver, filename, original := renderedSourceCollisionFixture(context, prefix+"abcdef"+suffix, dark, width)
					clickID(context, driver, renderedRows.At(1))
					for range 2 {
						state.rich.input.MoveTo(2, false)
						if err := driver.Key(gfx.KeyEnter, 0); err != nil {
							context.Fatal(err)
						}
					}
					current := state.session.Current()
					beforeNavigation, documentID := current.Text(), current.ID
					switch navigation {
					case "pointer":
						clickID(context, driver, renderedRows.At(4))
					case "keyboard":
						if err := driver.Key(gfx.KeyDown, 0); err != nil {
							context.Fatal(err)
						}
					case "selection":
						if err := driver.Key(gfx.KeyUp, gfx.ModifierShift); err != nil {
							context.Fatal(err)
						}
						if !state.selection.active {
							context.Fatal("navigation did not create a cross-block selection")
						}
						clickID(context, driver, renderedRows.At(4))
					case "outline":
						state.outline = true
						if err := driver.Resize(max(width, 1100), 780); err != nil {
							context.Fatal(err)
						}
						clickID(context, driver, outlineRows.At(0))
						if err := driver.Resize(width, 780); err != nil {
							context.Fatal(err)
						}
					case "find":
						clickID(context, driver, findDocumentID)
						if err := driver.Type("Target"); err != nil {
							context.Fatal(err)
						}
						if err := driver.Key(gfx.KeyEscape, 0); err != nil {
							context.Fatal(err)
						}
					case "modes":
						for _, id := range []gfx.ID{editModeID, renderedModeID, splitModeID, readModeID, renderedModeID} {
							clickID(context, driver, id)
							if state.session.Current().ID != documentID || current.Text() != beforeNavigation {
								context.Fatal("mode navigation changed document or committed source")
							}
						}
						clickID(context, driver, renderedRows.At(4))
					}
					if state.mode != renderedMode || !state.rich.active || state.rich.index != 4 || current.Text() != beforeNavigation || current.ID != documentID || driver.Snapshot().Focused != richInputID {
						context.Fatalf("navigation did not reach its supported target: mode=%v active=%v row=%d focus=%v source=%q", state.mode, state.rich.active, state.rich.index, driver.Snapshot().Focused, current.Text())
					}
					for cycle := 0; cycle < 2; cycle++ {
						for _, step := range []struct {
							command rune
							body    string
							row     int
							caret   int
						}{
							{'z', "ab\n\ncdef", 2, 2},
							{'z', "abcdef", 1, 2},
							{'y', "ab\n\ncdef", 2, 0},
							{'y', "ab\n\ncd\n\nef", 3, 0},
						} {
							if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
								context.Fatal(err)
							}
							if state.mode != renderedMode || !state.rich.active || state.rich.index != step.row || state.rich.input.Selection() != (text.Range{Start: step.caret, End: step.caret}) || driver.Snapshot().Focused != richInputID {
								context.Fatalf("navigation lost structure history: command=%c mode=%v active=%v row=%d", step.command, state.mode, state.rich.active, state.rich.index)
							}
							wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(prefix+step.body+suffix, "\n", "\r\n"))...)
							expectRenderedCollisionSave(context, state, driver, filename, wanted)
						}
					}
					for range 2 {
						if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
							context.Fatal(err)
						}
					}
					if current.Editor.CanUndo() {
						context.Fatal("navigation invented an extra source undo transaction")
					}
					expectRenderedCollisionSave(context, state, driver, filename, original)
				})
			}
		}
	}
}

func TestRenderedNavigationRetainsVirtualHistory(test *testing.T) {
	for _, keyboard := range []bool{false, true} {
		for _, typed := range []bool{false, true} {
			for _, dark := range []bool{false, true} {
				for _, width := range []int{740, 1200} {
					test.Run(fmt.Sprintf("keyboard-%v/typed-%v/dark-%v/width-%d", keyboard, typed, dark, width), func(context *testing.T) {
						const prefix = "<!-- untouched -->\n\n"
						state, driver, filename, original := renderedSourceCollisionFixture(context, prefix+"abcd\n", dark, width)
						clickID(context, driver, renderedRows.At(1))
						state.rich.input.MoveTo(4, false)
						if err := driver.Key(gfx.KeyEnter, 0); err != nil {
							context.Fatal(err)
						}
						if typed {
							if err := driver.Type("Q"); err != nil {
								context.Fatal(err)
							}
						}
						beforeNavigation := state.session.Current().Text()
						if keyboard {
							if err := driver.Key(gfx.KeyUp, 0); err != nil {
								context.Fatal(err)
							}
						} else {
							clickID(context, driver, renderedRows.At(1))
						}
						if state.mode != renderedMode || !state.rich.active || state.rich.virtual || state.rich.index != 1 || state.session.Current().Text() != beforeNavigation || driver.Snapshot().Focused != richInputID {
							context.Fatal("navigation did not leave the existing virtual input for the supported original paragraph")
						}
						type stage struct {
							command rune
							body    string
							virtual bool
							caret   int
						}
						steps := []stage{{'z', "abcd\n", false, 4}, {'y', "abcd\n\n\n", true, 0}}
						if typed {
							steps = append([]stage{{'z', "abcd\n\n\n", true, 0}}, steps...)
							steps = append(steps, stage{'y', "abcd\n\nQ\n", true, 1})
						}
						for cycle := 0; cycle < 2; cycle++ {
							for _, step := range steps {
								if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
									context.Fatal(err)
								}
								row := 1
								if step.virtual {
									row = 2
								}
								if state.mode != renderedMode || !state.rich.active || state.rich.index != row || state.rich.virtual != step.virtual || state.rich.input.Selection() != (text.Range{Start: step.caret, End: step.caret}) || driver.Snapshot().Focused != richInputID {
									context.Fatalf("navigation lost virtual history: command=%c mode=%v active=%v virtual=%v row=%d", step.command, state.mode, state.rich.active, state.rich.virtual, state.rich.index)
								}
								wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(prefix+step.body, "\n", "\r\n"))...)
								expectRenderedCollisionSave(context, state, driver, filename, wanted)
							}
						}
						undoCount := 1
						if typed {
							undoCount++
						}
						for range undoCount {
							if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
								context.Fatal(err)
							}
						}
						expectRenderedCollisionSave(context, state, driver, filename, original)
					})
				}
			}
		}
	}
}

func TestRenderedNavigationRetainsCrossStyleHistory(test *testing.T) {
	for _, format := range []bool{false, true} {
		for _, backward := range []bool{false, true} {
			for _, dark := range []bool{false, true} {
				for _, width := range []int{740, 1200} {
					test.Run(fmt.Sprintf("format-%v/backward-%v/dark-%v/width-%d", format, backward, dark, width), func(context *testing.T) {
						const prefix = "<!-- untouched -->\n\n"
						const suffix = "\n\n## Target\n\nTail **keep** &amp; <!-- opaque -->\n"
						body, changed := "ab **cd** efgh", "aXgh"
						beforeSelection, afterSelection := text.Range{Start: 1, End: 8}, text.Range{Start: 2, End: 2}
						if format {
							body, changed = "abcd", "a**bc**d"
							beforeSelection = text.Range{Start: 1, End: 3}
						}
						if backward {
							beforeSelection.Start, beforeSelection.End = beforeSelection.End, beforeSelection.Start
						}
						if format {
							afterSelection = beforeSelection
						}
						state, driver, filename, original := renderedSourceCollisionFixture(context, prefix+body+suffix, dark, width)
						clickID(context, driver, renderedRows.At(1))
						state.rich.input.SetSelection(beforeSelection)
						if format {
							state.applyBoldBlock()
							if err := driver.Pump(); err != nil {
								context.Fatal(err)
							}
						} else if err := driver.Type("X"); err != nil {
							context.Fatal(err)
						}
						clickID(context, driver, renderedRows.At(2))
						if state.mode != renderedMode || !state.rich.active || state.rich.index != 2 {
							context.Fatal("cross-style navigation failed to reach the target heading")
						}
						for cycle := 0; cycle < 2; cycle++ {
							for _, step := range []struct {
								command   rune
								body      string
								selection text.Range
							}{{'z', body, beforeSelection}, {'y', changed, afterSelection}} {
								if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
									context.Fatal(err)
								}
								wantedSelection := text.Range{Start: min(step.selection.Start, step.selection.End), End: max(step.selection.Start, step.selection.End)}
								if state.mode != renderedMode || !state.rich.active || state.rich.index != 1 || state.rich.input.Selection() != wantedSelection || state.rich.input.Caret() != step.selection.End || driver.Snapshot().Focused != richInputID {
									context.Fatalf("navigation lost cross-style history position: command=%c mode=%v active=%v row=%d", step.command, state.mode, state.rich.active, state.rich.index)
								}
								wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(prefix+step.body+suffix, "\n", "\r\n"))...)
								expectRenderedCollisionSave(context, state, driver, filename, wanted)
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

func TestRenderedNavigationRejectsStaleHistoryCarrier(test *testing.T) {
	for _, selection := range []bool{false, true} {
		for _, stale := range []string{"document", "source"} {
			test.Run(fmt.Sprintf("selection-%v/%s", selection, stale), func(context *testing.T) {
				state, driver, _, _ := renderedSourceCollisionFixture(context, "abcd\n\n## Target\n", false, 1200)
				clickID(context, driver, renderedRows.At(0))
				state.rich.input.MoveTo(2, false)
				if err := driver.Key(gfx.KeyEnter, 0); err != nil {
					context.Fatal(err)
				}
				if selection {
					if err := driver.Key(gfx.KeyUp, gfx.ModifierShift); err != nil {
						context.Fatal(err)
					}
					if !state.selection.active {
						context.Fatal("history carrier fixture did not create a selection")
					}
					if stale == "document" {
						state.selection.documentID++
					} else {
						state.selection.source += "stale"
					}
				} else if stale == "document" {
					state.rich.documentID++
				} else {
					state.rich.source += "stale"
				}
				current := state.session.Current()
				before, sourceSelection := current.Text(), current.Editor.Selection()
				state.activateRendered(2)
				if len(state.rich.structureUndo) != 0 || len(state.rich.crossUndo) != 0 || state.rich.sourceHistoryOffset != 0 || current.Text() != before || current.Editor.Selection() != sourceSelection {
					context.Fatal("navigation reused a stale document/source history carrier")
				}
			})
		}
	}
}

func TestRenderedNavigationFailedHistoryKeepsCurrentNativeState(test *testing.T) {
	for _, virtual := range []bool{false, true} {
		test.Run(fmt.Sprintf("virtual-%v", virtual), func(context *testing.T) {
			state, driver, _, _ := renderedSourceCollisionFixture(context, "abcd\n\n## Target\n", false, 1200)
			clickID(context, driver, renderedRows.At(0))
			targetIndex := 1
			if virtual {
				state.rich.input.MoveTo(4, false)
				if err := driver.Key(gfx.KeyEnter, 0); err != nil {
					context.Fatal(err)
				}
				if err := driver.Type("Q"); err != nil {
					context.Fatal(err)
				}
				targetIndex = 2
			} else {
				state.rich.input.SetSelection(text.Range{Start: 1, End: 3})
				state.applyBoldBlock()
				if err := driver.Pump(); err != nil {
					context.Fatal(err)
				}
			}
			clickID(context, driver, renderedRows.At(targetIndex))
			if !state.rich.active || state.rich.input.String() != "Target" || len(state.rich.crossUndo) == 0 {
				context.Fatal("failed-history fixture did not reach the target with owned cross history")
			}
			state.rich.crossUndo[len(state.rich.crossUndo)-1].beforeSourceHash[0] ^= 0xff
			current, input := state.session.Current(), state.rich.input
			beforeSource, sourceSelection := current.Text(), current.Editor.Selection()
			beforeNative := app.TextStateOf(input)
			beforePosition := state.rich.sourceHistoryOffset
			canUndo, canRedo := current.Editor.CanUndo(), current.Editor.CanRedo()
			result, handled := state.stepCrossHistory(true, false)
			if !handled || current.Text() != beforeSource || current.Editor.Selection() != sourceSelection || current.Editor.CanUndo() != canUndo || current.Editor.CanRedo() != canRedo || state.rich.input != input || state.rich.index != targetIndex || state.rich.sourceHistoryOffset != beforePosition {
				context.Fatal("failed navigation history changed authoritative history or the current view")
			}
			if result.Value != beforeNative.Value || result.Selection != beforeNative.Selection || result.SelectionReversed != beforeNative.SelectionReversed {
				context.Fatal("failed navigation history reported another block to the native input method")
			}
		})
	}
}
