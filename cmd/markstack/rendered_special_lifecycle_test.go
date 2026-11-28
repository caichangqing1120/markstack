package main

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func specialHistoryInput(state *model, editor string) (*text.Editor, gfx.ID) {
	if editor == "table" {
		return state.table.input, tableInputID
	}
	return state.codeBlock.input, codeInputID
}

func specialHistoryEvent(state *model, editor string, event app.TextEvent) (app.TextState, error) {
	if editor == "table" {
		return state.onTableCellTextEvent(event)
	}
	return state.onCodeBlockTextEvent(event)
}

func TestRenderedSpecialRefusalKeepsSourceHistorySelection(test *testing.T) {
	for _, editor := range []string{"fence", "table"} {
		for _, direction := range []string{"caret", "forward", "reverse"} {
			for _, dark := range []bool{false, true} {
				for _, width := range []int{740, 1200} {
					test.Run(fmt.Sprintf("%s/%s/dark-%v/width-%d", editor, direction, dark, width), func(context *testing.T) {
						const needle = "ab\u4e2dd"
						prefix := "> ```go\n> " + needle + "\n> ```\n\n"
						if editor == "table" {
							prefix = "| A | B |\n| --- | --- |\n| **" + needle + "** | keep |\n\n"
						}
						state, driver, filename, original := renderedSourceCollisionFixture(context, prefix+"## Target\n", dark, width)
						current := state.session.Current()
						clickID(context, driver, editModeID)
						position := utf8.RuneCountInString(current.Text()[:strings.Index(current.Text(), needle)]) + 1
						selection := text.Range{Start: position, End: position}
						if direction == "forward" {
							selection.End += 2
						} else if direction == "reverse" {
							selection.Start += 2
						}
						current.Editor.SetSelection(selection)
						if err := driver.Type("X"); err != nil {
							context.Fatal(err)
						}
						changed := append([]byte{0xef, 0xbb, 0xbf}, []byte(current.Text())...)
						for range 2 {
							for _, command := range []rune{'z', 'y'} {
								clickID(context, driver, renderedModeID)
								activateSpecialHistoryTarget(context, state, driver)
								if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
									context.Fatal(err)
								}
								wantedSelection, wanted := selection, original
								if command == 'y' {
									wantedSelection, wanted = text.Range{Start: position + 1, End: position + 1}, changed
								}
								ordered := text.Range{Start: min(wantedSelection.Start, wantedSelection.End), End: max(wantedSelection.Start, wantedSelection.End)}
								if current.Editor.Selection() != ordered || current.Editor.Caret() != wantedSelection.End {
									context.Fatalf("refused special view changed Source history selection: command=%c got=%+v caret=%d want=%+v", command, current.Editor.Selection(), current.Editor.Caret(), wantedSelection)
								}
								if state.mode != editMode || state.rich.active || state.table.active || state.codeBlock.active || driver.Snapshot().Focused != sourceID {
									context.Fatal("unsupported special target did not explicitly return ownership to Source")
								}
								if current.Editor.CanUndo() != (command == 'y') || current.Editor.CanRedo() != (command == 'z') {
									context.Fatal("special refusal added or discarded actual Source history units")
								}
								expectRenderedCollisionSave(context, state, driver, filename, wanted)
							}
						}
					})
				}
			}
		}
	}
}

func TestRenderedSpecialSourceFallbackPreservesHistorySelection(test *testing.T) {
	for _, editor := range []string{"fence", "table"} {
		for _, direction := range []string{"caret", "forward", "reverse"} {
			for _, dark := range []bool{false, true} {
				for _, width := range []int{740, 1200} {
					test.Run(fmt.Sprintf("%s/%s/dark-%v/width-%d", editor, direction, dark, width), func(context *testing.T) {
						state, driver, filename, original := renderedSourceCollisionFixture(context, specialHistoryPrefix(editor)+"## Target\n", dark, width)
						current := state.session.Current()
						clickID(context, driver, editModeID)
						position := utf8.RuneCountInString(current.Text()[:strings.Index(current.Text(), "## Target")])
						selection := text.Range{Start: position + 1, End: position + 1}
						if direction == "forward" {
							selection = text.Range{Start: position, End: position + 2}
						} else if direction == "reverse" {
							selection = text.Range{Start: position + 2, End: position}
						}
						current.Editor.SetSelection(selection)
						if err := driver.Type("#"); err != nil {
							context.Fatal(err)
						}
						changed := append([]byte{0xef, 0xbb, 0xbf}, []byte(current.Text())...)
						for range 2 {
							for _, command := range []rune{'z', 'y'} {
								clickID(context, driver, renderedModeID)
								activateHistorySpecial(context, state, driver, editor)
								if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
									context.Fatal(err)
								}
								wantedSelection, wanted := selection, original
								if command == 'y' {
									caret := min(selection.Start, selection.End) + 1
									wantedSelection, wanted = text.Range{Start: caret, End: caret}, changed
								}
								ordered := text.Range{Start: min(wantedSelection.Start, wantedSelection.End), End: max(wantedSelection.Start, wantedSelection.End)}
								if current.Editor.Selection() != ordered || current.Editor.Caret() != wantedSelection.End {
									context.Fatalf("native Source fallback changed actual history selection: command=%c got=%+v caret=%d want=%+v", command, current.Editor.Selection(), current.Editor.Caret(), wantedSelection)
								}
								if state.mode != editMode || state.rich.active || state.table.active || state.codeBlock.active || driver.Snapshot().Focused != sourceID {
									context.Fatal("hidden heading syntax did not explicitly return ownership to Source")
								}
								expectRenderedCollisionSave(context, state, driver, filename, wanted)
							}
						}
					})
				}
			}
		}
	}
}

func TestRenderedSpecialRedoLifecycle(test *testing.T) {
	for _, editor := range []string{"table", "fence"} {
		for _, operation := range []string{"cancel", "empty-cancel", "noop", "branch"} {
			test.Run(fmt.Sprintf("%s/%s", editor, operation), func(context *testing.T) {
				state, driver, filename, original := renderedSourceCollisionFixture(context, specialHistoryPrefix(editor)+"abcd\n\n## Target\n", false, 1200)
				clickID(context, driver, renderedRows.At(1))
				state.rich.input.MoveTo(2, false)
				if err := driver.Key(gfx.KeyEnter, 0); err != nil {
					context.Fatal(err)
				}
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
				activateHistorySpecial(context, state, driver, editor)
				input, focus := specialHistoryInput(state, editor)
				before := input.String()
				current := state.session.Current()
				if operation == "cancel" || operation == "empty-cancel" {
					if err := driver.Compose("ni"); err != nil {
						context.Fatal(err)
					}
					if operation == "empty-cancel" {
						if _, err := specialHistoryEvent(state, editor, app.TextEvent{Kind: app.TextSetMarked, Text: ""}); err != nil {
							context.Fatal(err)
						}
					}
					if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
						context.Fatal(err)
					}
					if _, composing := input.Composition(); !composing || driver.Snapshot().Focused != focus || current.Dirty() || len(state.rich.structureRedo) != 1 {
						context.Fatal("special preedit save lost owner or changed committed Source/history")
					}
					if _, err := specialHistoryEvent(state, editor, app.TextEvent{Kind: app.TextCancelComposition}); err != nil {
						context.Fatal(err)
					}
					if err := driver.Pump(); err != nil {
						context.Fatal(err)
					}
				} else if operation == "noop" {
					input.SetSelection(text.Range{Start: 0, End: input.RuneCount()})
					if err := driver.Type(before); err != nil {
						context.Fatal(err)
					}
				} else {
					input.MoveTo(input.RuneCount(), false)
					if err := driver.Type("Z"); err != nil {
						context.Fatal(err)
					}
					if len(state.rich.structureRedo) != 0 || current.Editor.CanRedo() || state.rich.sourceHistoryOffset != 1 {
						context.Fatal("actual special Source branch retained structural redo or lost its real coordinate")
					}
					changed := current.Text()
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
					expectRenderedCollisionSave(context, state, driver, filename, original)
					activateSpecialHistoryTarget(context, state, driver)
					if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
						context.Fatal(err)
					}
					input, activeFocus := specialHistoryInput(state, editor)
					if state.mode != renderedMode || input == nil || input.String() != before+"Z" || input.Caret() != utf8.RuneCountInString(before)+1 || driver.Snapshot().Focused != activeFocus || current.Text() != changed || len(state.rich.structureRedo) != 0 {
						context.Fatal("stale structural redo intercepted the actual special Source branch")
					}
					expectRenderedCollisionSave(context, state, driver, filename, append([]byte{0xef, 0xbb, 0xbf}, []byte(changed)...))
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
					expectRenderedCollisionSave(context, state, driver, filename, original)
					return
				}
				if len(state.rich.structureRedo) != 1 || !current.Editor.CanRedo() || state.rich.sourceHistoryOffset != 0 {
					context.Fatal("cancelled/no-op special input discarded redo or advanced Source history")
				}
				expectRenderedCollisionSave(context, state, driver, filename, original)
				activateSpecialHistoryTarget(context, state, driver)
				if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
					context.Fatal(err)
				}
				if state.mode != renderedMode || !state.rich.active || state.rich.index != 2 || state.rich.input.String() != "cd" || state.rich.input.Caret() != 0 {
					context.Fatal("cancelled/no-op special input lost the original structure target")
				}
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
				expectRenderedCollisionSave(context, state, driver, filename, original)
			})
		}
	}
}

func TestRenderedSpecialRejectsStaleNativeHistoryOwner(test *testing.T) {
	for _, editor := range []string{"table", "fence"} {
		for _, stale := range []string{"document", "source"} {
			test.Run(fmt.Sprintf("%s/%s", editor, stale), func(context *testing.T) {
				state, driver, _, _ := renderedSourceCollisionFixture(context, specialHistoryPrefix(editor)+"abcd\n\n## Target\n", false, 1200)
				clickID(context, driver, renderedRows.At(1))
				state.rich.input.MoveTo(2, false)
				if err := driver.Key(gfx.KeyEnter, 0); err != nil {
					context.Fatal(err)
				}
				activateHistorySpecial(context, state, driver, editor)
				input, _ := specialHistoryInput(state, editor)
				beforeState := app.TextStateOf(input)
				current := state.session.Current()
				before, selection := current.Text(), current.Editor.Selection()
				canUndo, canRedo := current.Editor.CanUndo(), current.Editor.CanRedo()
				if editor == "table" {
					if stale == "document" {
						state.table.documentID++
					} else {
						state.table.source += "stale"
					}
				} else if stale == "document" {
					state.codeBlock.documentID++
				} else {
					state.codeBlock.source += "stale"
				}
				result, err := specialHistoryEvent(state, editor, app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextUndo})
				if err != nil || result.Value != beforeState.Value || result.Selection != beforeState.Selection || current.Text() != before || current.Editor.Selection() != selection || current.Editor.CanUndo() != canUndo || current.Editor.CanRedo() != canRedo {
					context.Fatal("stale special native owner consumed current Source history or lost its native state")
				}
			})
		}
	}
}

func TestRenderedSpecialRejectsStaleCarrier(test *testing.T) {
	for _, editor := range []string{"table", "fence"} {
		for _, stale := range []string{"document", "source", "coordinate"} {
			test.Run(fmt.Sprintf("%s/%s", editor, stale), func(context *testing.T) {
				state, driver, filename, original := renderedSourceCollisionFixture(context, specialHistoryPrefix(editor)+"abcd\n\n## Target\n", false, 1200)
				clickID(context, driver, renderedRows.At(1))
				state.rich.input.MoveTo(2, false)
				if err := driver.Key(gfx.KeyEnter, 0); err != nil {
					context.Fatal(err)
				}
				activateHistorySpecial(context, state, driver, editor)
				switch stale {
				case "document":
					state.rich.documentID++
				case "source":
					state.rich.source += "stale"
				case "coordinate":
					state.rich.sourceHistoryOffset++
				}
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
				if state.mode != editMode || state.rich.active || driver.Snapshot().Focused != sourceID {
					context.Fatal("unowned special history silently retained the wrong active editor")
				}
				expectRenderedCollisionSave(context, state, driver, filename, original)
			})
		}
	}
}

func TestRenderedSpecialHistoryRespectsSourceCapacity(test *testing.T) {
	for _, editor := range []string{"table", "fence"} {
		test.Run(editor, func(context *testing.T) {
			state, driver, _, _ := renderedSourceCollisionFixture(context, specialHistoryPrefix(editor)+"abcd\n\n## Target\n", false, 1200)
			clickID(context, driver, renderedRows.At(1))
			state.rich.input.MoveTo(2, false)
			if err := driver.Key(gfx.KeyEnter, 0); err != nil {
				context.Fatal(err)
			}
			activateHistorySpecial(context, state, driver, editor)
			for range 260 {
				if err := driver.Type("X"); err != nil {
					context.Fatal(err)
				}
			}
			if state.rich.sourceHistoryOffset != 261 || len(state.rich.structureUndo) != 0 || len(state.rich.structureRedo) != 0 {
				context.Fatal("special Source commits retained out-of-capacity structure metadata")
			}
			for position := 260; position >= 5; position-- {
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
				if state.mode != renderedMode || state.rich.sourceHistoryOffset != position {
					context.Fatal("special Source Undo lost its actual coordinate")
				}
			}
			if state.session.Current().Editor.CanUndo() {
				context.Fatal("special Source history exceeded the pinned 256-unit capacity")
			}
			for position := 6; position <= 261; position++ {
				if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
					context.Fatal(err)
				}
				if state.mode != renderedMode || state.rich.sourceHistoryOffset != position {
					context.Fatal("special Source Redo lost its actual coordinate")
				}
			}
			if state.session.Current().Editor.CanRedo() {
				context.Fatal("special Source history invented another Redo unit")
			}
		})
	}
}
