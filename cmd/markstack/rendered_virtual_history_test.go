package main

import (
	"fmt"
	"strings"
	"testing"
	"unsafe"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestRenderedVirtualHistoryRetainsEarlierSplits(test *testing.T) {
	for _, between := range []bool{false, true} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{740, 1200} {
				test.Run(fmt.Sprintf("between-%v/dark-%v/width-%d", between, dark, width), func(context *testing.T) {
					const prefix = "<!-- untouched -->\n\n"
					suffix := "\n"
					if between {
						suffix = "\n\nTail **keep** &amp; <!-- opaque -->\n"
					}
					state, driver, filename, original := renderedSourceCollisionFixture(context, prefix+"abcdef"+suffix, dark, width)
					clickID(context, driver, renderedRows.At(1))
					for range 2 {
						state.rich.input.MoveTo(2, false)
						if err := driver.Key(gfx.KeyEnter, 0); err != nil {
							context.Fatal(err)
						}
					}
					state.rich.input.MoveTo(2, false)
					if err := driver.Key(gfx.KeyEnter, 0); err != nil {
						context.Fatal(err)
					}
					if state.mode != renderedMode || !state.rich.virtual {
						context.Fatal("existing block-end Enter did not activate a virtual paragraph")
					}
					for cycle := 0; cycle < 2; cycle++ {
						for _, step := range []struct {
							command rune
							body    string
							row     int
							caret   int
							virtual bool
						}{
							{'z', "ab\n\ncd\n\nef", 3, 2, false},
							{'z', "ab\n\ncdef", 2, 2, false},
							{'z', "abcdef", 1, 2, false},
							{'y', "ab\n\ncdef", 2, 0, false},
							{'y', "ab\n\ncd\n\nef", 3, 0, false},
							{'y', "ab\n\ncd\n\nef\n\n", 4, 0, true},
						} {
							if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
								context.Fatal(err)
							}
							if state.mode != renderedMode || !state.rich.active || state.rich.virtual != step.virtual || state.rich.index != step.row || state.rich.input.Caret() != step.caret || driver.Snapshot().Focused != richInputID {
								context.Fatalf("virtual history lost an earlier split: command=%c mode=%v active=%v row=%d", step.command, state.mode, state.rich.active, state.rich.index)
							}
							wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(prefix+step.body+suffix, "\n", "\r\n"))...)
							expectRenderedCollisionSave(context, state, driver, filename, wanted)
						}
					}
					for range 3 {
						if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
							context.Fatal(err)
						}
					}
					if state.session.Current().Editor.CanUndo() {
						context.Fatal("virtual view invented a Source undo unit")
					}
					expectRenderedCollisionSave(context, state, driver, filename, original)
				})
			}
		}
	}
}

func TestRenderedVirtualHistoryReplaysEmptyParagraphsAndLists(test *testing.T) {
	type stage struct {
		body    string
		visible string
		row     int
		caret   int
		virtual bool
		depth   int
	}
	for _, scenario := range []struct {
		name   string
		stages []stage
	}{
		{"paragraph", []stage{{"abcd", "abcd", 1, 4, false, 0}, {"abcd\n\n", "", 2, 0, true, 0}, {"abcd\n\n\n", "", 2, 0, true, 1}, {"abcd\n\n\nQ", "Q", 2, 1, true, 1}, {"abcd\n\n\nQ\n\n", "", 3, 0, true, 0}, {"abcd\n\n\nQ\n\nN", "N", 3, 1, true, 0}}},
		{"heading", []stage{{"## abcd", "abcd", 1, 4, false, 0}, {"## abcd\n\n", "", 2, 0, true, 0}, {"## abcd\n\n\n", "", 2, 0, true, 1}, {"## abcd\n\n\nQ", "Q", 2, 1, true, 1}, {"## abcd\n\n\nQ\n\n", "", 3, 0, true, 0}, {"## abcd\n\n\nQ\n\nN", "N", 3, 1, true, 0}}},
		{"unordered", []stage{{"- abcd", "abcd", 1, 4, false, 0}, {"- abcd\n- ", "", 2, 0, true, 0}, {"- abcd\n\n", "", 2, 0, true, 0}, {"- abcd\n\nQ", "Q", 2, 1, true, 0}, {"- abcd\n\nQ\n\n", "", 3, 0, true, 0}, {"- abcd\n\nQ\n\nN", "N", 3, 1, true, 0}}},
		{"ordered", []stage{{"9. abcd", "abcd", 1, 4, false, 0}, {"9. abcd\n10. ", "", 2, 0, true, 0}, {"9. abcd\n\n", "", 2, 0, true, 0}, {"9. abcd\n\nQ", "Q", 2, 1, true, 0}, {"9. abcd\n\nQ\n\n", "", 3, 0, true, 0}, {"9. abcd\n\nQ\n\nN", "N", 3, 1, true, 0}}},
		{"list-between", []stage{{"- abcd\n- Later", "abcd", 1, 4, false, 0}, {"- abcd\n- \n- Later", "", 2, 0, true, 0}, {"- abcd\n\n\n- Later", "", 2, 0, true, 0}, {"- abcd\n\nQ\n- Later", "Q", 2, 1, true, 0}}},
	} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{740, 1200} {
				test.Run(fmt.Sprintf("%s/dark-%v/width-%d", scenario.name, dark, width), func(context *testing.T) {
					const prefix = "<!-- untouched -->\n\n"
					const suffix = "\n"
					state, driver, filename, original := renderedSourceCollisionFixture(context, prefix+scenario.stages[0].body+suffix, dark, width)
					clickID(context, driver, renderedRows.At(1))
					check := func(position int) {
						context.Helper()
						wanted := scenario.stages[position]
						if state.mode != renderedMode || !state.rich.active || state.rich.virtual != wanted.virtual || state.rich.index != wanted.row || state.rich.input.String() != wanted.visible || state.rich.input.Selection() != (text.Range{Start: wanted.caret, End: wanted.caret}) || state.virtualParagraphDepth() != wanted.depth || driver.Snapshot().Focused != richInputID {
							context.Fatalf("virtual history lost stage%d: mode=%v active=%v virtual=%v row=%d source=%q selection=%+v message=%q", position, state.mode, state.rich.active, state.rich.virtual, state.rich.index, state.session.Current().Text(), state.session.Current().Editor.Selection(), state.message)
						}
						bytes := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(prefix+wanted.body+suffix, "\n", "\r\n"))...)
						expectRenderedCollisionSave(context, state, driver, filename, bytes)
					}
					for position := 1; position < len(scenario.stages); position++ {
						if value := scenario.stages[position].visible; value != "" {
							if err := driver.Type(value); err != nil {
								context.Fatal(err)
							}
						} else if err := driver.Key(gfx.KeyEnter, 0); err != nil {
							context.Fatal(err)
						}
						check(position)
					}
					for cycle := 0; cycle < 2; cycle++ {
						for position := len(scenario.stages) - 2; position >= 0; position-- {
							if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
								context.Fatal(err)
							}
							check(position)
						}
						if state.session.Current().Editor.CanUndo() {
							context.Fatal("virtual history invented an extra undo unit")
						}
						for position := 1; position < len(scenario.stages); position++ {
							if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
								context.Fatal(err)
							}
							check(position)
						}
					}
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
					check(len(scenario.stages) - 2)
					before := state.session.Current().Text()
					if err := driver.Compose("ni"); err != nil {
						context.Fatal(err)
					}
					if _, err := state.onRenderedTextEvent(app.TextEvent{Kind: app.TextCancelComposition}); err != nil {
						context.Fatal(err)
					}
					if err := driver.Pump(); err != nil {
						context.Fatal(err)
					}
					if state.session.Current().Text() != before {
						context.Fatal("cancelled virtual preedit changed Source")
					}
					if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
						context.Fatal(err)
					}
					check(len(scenario.stages) - 1)
					for position := len(scenario.stages) - 2; position >= 0; position-- {
						if scenario.stages[position+1].visible == "" {
							if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
								context.Fatal(err)
							}
						} else if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
							context.Fatal(err)
						}
						check(position)
					}
					expectRenderedCollisionSave(context, state, driver, filename, original)
					if err := driver.Type("Z"); err != nil {
						context.Fatal(err)
					}
					before = state.session.Current().Text()
					if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
						context.Fatal(err)
					}
					if state.mode != renderedMode || state.session.Current().Text() != before || state.session.Current().Editor.CanRedo() {
						context.Fatal("new source branch retained obsolete virtual redo")
					}
				})
			}
		}
	}
}

func TestRenderedVirtualHistoryPreservesReplacementSelection(test *testing.T) {
	for _, backward := range []bool{false, true} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{740, 1200} {
				test.Run(fmt.Sprintf("backward-%v/dark-%v/width-%d", backward, dark, width), func(context *testing.T) {
					state, driver, _, _ := renderedSourceCollisionFixture(context, "abcd\n", dark, width)
					clickID(context, driver, renderedRows.At(0))
					if err := driver.Key(gfx.KeyEnter, 0); err != nil {
						context.Fatal(err)
					}
					const original = "\u4e2d\u6587e\u0301Z"
					if err := driver.Type(original); err != nil {
						context.Fatal(err)
					}
					selection := text.Range{Start: 1, End: 4}
					if backward {
						selection.Start, selection.End = selection.End, selection.Start
					}
					state.rich.input.SetSelection(selection)
					if err := driver.Type("R"); err != nil {
						context.Fatal(err)
					}
					state.rich.input.MoveTo(3, false)
					if err := driver.Key(gfx.KeyEnter, 0); err != nil {
						context.Fatal(err)
					}
					for cycle := 0; cycle < 2; cycle++ {
						for _, step := range []struct {
							command rune
							value   string
							caret   int
							selects bool
						}{
							{'z', "\u4e2dRZ", 3, false},
							{'z', original, selection.End, true},
							{'y', "\u4e2dRZ", 2, false},
							{'y', "", 0, false},
						} {
							if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
								context.Fatal(err)
							}
							wanted := text.Range{Start: step.caret, End: step.caret}
							if step.selects {
								wanted = text.Range{Start: 1, End: 4}
							}
							if state.mode != renderedMode || !state.rich.active || !state.rich.virtual || state.rich.input.String() != step.value || state.rich.input.Selection() != wanted || state.rich.input.Caret() != step.caret || driver.Snapshot().Focused != richInputID {
								context.Fatalf("virtual replacement lost its visible selection: command=%c value=%q selection=%+v caret=%d", step.command, state.rich.input.String(), state.rich.input.Selection(), state.rich.input.Caret())
							}
						}
					}
				})
			}
		}
	}
}

func TestRenderedVirtualHistorySharesSourceCapacity(test *testing.T) {
	const prefix = "<!-- untouched -->\n\n"
	state, driver, filename, _ := renderedSourceCollisionFixture(test, prefix+"abcd\n", false, 1200)
	clickID(test, driver, renderedRows.At(1))
	for range 260 {
		if result := state.insertRenderedNewline(); !result.Handled || !state.rich.virtual {
			test.Fatal("capacity fixture failed to create an existing virtual paragraph")
		}
	}
	for _, command := range []app.TextCommand{app.TextUndo, app.TextRedo} {
		for operation := 1; operation <= 256; operation++ {
			if _, err := state.onRenderedTextEvent(app.TextEvent{Kind: app.TextCommandEvent, Command: command}); err != nil {
				test.Fatal(err)
			}
			position := 260 - operation
			if command == app.TextRedo {
				position = 4 + operation
			}
			wanted := strings.ReplaceAll(prefix+"abcd", "\n", "\r\n") + strings.Repeat("\r\n", position+2)
			if state.mode != renderedMode || !state.rich.active || !state.rich.virtual || state.rich.input.String() != "" || state.virtualParagraphDepth() != position-1 || state.rich.appliedEdits+state.rich.sourceHistoryOffset != position || state.session.Current().Text() != wanted || len(state.rich.structureUndo) > 256 || len(state.rich.structureRedo) > 256 {
				test.Fatalf("virtual capacity replay lost Source unit%d", position)
			}
		}
		if command == app.TextUndo && (state.session.Current().Editor.CanUndo() || len(state.rich.structureUndo) != 0) {
			test.Fatal("virtual metadata outlived Source undo capacity")
		}
		if command == app.TextRedo && (state.session.Current().Editor.CanRedo() || len(state.rich.structureRedo) != 0) {
			test.Fatal("virtual metadata invented Source redo units")
		}
	}
	if err := driver.Pump(); err != nil {
		test.Fatal(err)
	}
	wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(state.session.Current().Text())...)
	expectRenderedCollisionSave(test, state, driver, filename, wanted)
}

func TestRenderedVirtualHistoryRejectsStaleIdentity(test *testing.T) {
	for _, stale := range []string{"source", "document", "position", "range"} {
		test.Run(stale, func(context *testing.T) {
			state, driver, _, _ := renderedSourceCollisionFixture(context, "abcd\n", false, 1200)
			clickID(context, driver, renderedRows.At(0))
			if err := driver.Key(gfx.KeyEnter, 0); err != nil {
				context.Fatal(err)
			}
			current := state.session.Current()
			switch stale {
			case "source":
				current.Editor.Insert("external")
			case "document":
				state.rich.documentID++
			case "position":
				state.rich.sourceHistoryOffset++
			case "range":
				state.rich.virtualStart++
			}
			before, selection := current.Text(), current.Editor.Selection()
			canUndo, canRedo := current.Editor.CanUndo(), current.Editor.CanRedo()
			if _, handled := state.stepStructureHistory(true); handled || current.Text() != before || current.Editor.Selection() != selection || current.Editor.CanUndo() != canUndo || current.Editor.CanRedo() != canRedo {
				context.Fatal("stale virtual metadata consumed authoritative Source history")
			}
		})
	}
}

func TestRenderedVirtualHistoryNewBranchAndNoOpKeepRealCoordinates(test *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, width := range []int{740, 1200} {
			test.Run(fmt.Sprintf("dark-%v/width-%d", dark, width), func(context *testing.T) {
				state, driver, filename, original := renderedSourceCollisionFixture(context, "abcd\n", dark, width)
				clickID(context, driver, renderedRows.At(0))
				for range 2 {
					if err := driver.Key(gfx.KeyEnter, 0); err != nil {
						context.Fatal(err)
					}
				}
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
				for _, event := range []app.TextEvent{{Kind: app.TextSetMarked, Text: "pin"}, {Kind: app.TextCancelComposition}} {
					if _, err := state.onRenderedTextEvent(event); err != nil {
						context.Fatal(err)
					}
				}
				if len(state.rich.structureRedo) != 1 || state.rich.appliedEdits+state.rich.sourceHistoryOffset != 1 {
					context.Fatal("cancelled virtual composition discarded structural redo")
				}
				if err := driver.Type("Z"); err != nil {
					context.Fatal(err)
				}
				if len(state.rich.structureRedo) != 0 || state.session.Current().Editor.CanRedo() {
					context.Fatal("real virtual input retained obsolete structure redo")
				}
				position := state.rich.appliedEdits + state.rich.sourceHistoryOffset
				historyCount := len(state.rich.crossUndo)
				state.rich.input.SetSelection(text.Range{Start: 0, End: 1})
				if err := driver.Type("Z"); err != nil {
					context.Fatal(err)
				}
				if state.rich.appliedEdits+state.rich.sourceHistoryOffset != position || len(state.rich.crossUndo) != historyCount {
					context.Fatal("identical virtual replacement invented a Source history unit")
				}
				for range 3 {
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
				}
				if state.mode != renderedMode || !state.rich.active || state.rich.virtual || state.session.Current().Editor.CanUndo() {
					context.Fatal("virtual no-op shifted the structural history boundary")
				}
				expectRenderedCollisionSave(context, state, driver, filename, original)
			})
		}
	}
}

func TestRenderedVirtualHistoryRetainsEarlierFormatting(test *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, width := range []int{740, 1200} {
			test.Run(fmt.Sprintf("dark-%v/width-%d", dark, width), func(context *testing.T) {
				state, driver, filename, _ := renderedSourceCollisionFixture(context, "abcd\n", dark, width)
				clickID(context, driver, renderedRows.At(0))
				state.applyHeadingLevel(2)
				if err := driver.Pump(); err != nil {
					context.Fatal(err)
				}
				if err := driver.Key(gfx.KeyEnter, 0); err != nil {
					context.Fatal(err)
				}
				if err := driver.Type("Q"); err != nil {
					context.Fatal(err)
				}
				if err := driver.Key(gfx.KeyEnter, 0); err != nil {
					context.Fatal(err)
				}
				for cycle := 0; cycle < 2; cycle++ {
					for _, step := range []struct {
						command rune
						body    string
						visible string
						virtual bool
						caret   int
					}{
						{'z', "## abcd\n\nQ\n", "Q", true, 1},
						{'z', "## abcd\n\n\n", "", true, 0},
						{'z', "## abcd\n", "abcd", false, 4},
						{'z', "abcd\n", "abcd", false, 4},
						{'y', "## abcd\n", "abcd", false, 4},
						{'y', "## abcd\n\n\n", "", true, 0},
						{'y', "## abcd\n\nQ\n", "Q", true, 1},
						{'y', "## abcd\n\nQ\n\n\n", "", true, 0},
					} {
						if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
							context.Fatal(err)
						}
						if state.mode != renderedMode || !state.rich.active || state.rich.virtual != step.virtual || state.rich.input.String() != step.visible || state.rich.input.Caret() != step.caret || driver.Snapshot().Focused != richInputID {
							context.Fatal("virtual structure restoration lost earlier owned formatting")
						}
						wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(step.body, "\n", "\r\n"))...)
						expectRenderedCollisionSave(context, state, driver, filename, wanted)
					}
				}
			})
		}
	}
}

func TestRenderedVirtualHistoryDetachesSourceFragments(test *testing.T) {
	source := strings.Repeat("a", 1<<20) + "\r\n"
	fragment := source[len(source)-2:]
	rich := renderedEditor{virtual: true, source: source, virtualSuffix: fragment, virtualInsert: fragment}
	virtual := rich.virtualState()
	if unsafe.StringData(virtual.suffix) == unsafe.StringData(fragment) || unsafe.StringData(virtual.insert) == unsafe.StringData(fragment) {
		test.Fatal("bounded virtual descriptor retains the full Source backing string")
	}
	if virtual.suffix != fragment || virtual.insert != fragment {
		test.Fatal("detached virtual fragments changed their bytes")
	}
}

func TestRenderedVirtualHistoryPreservesOrdinaryCaret(test *testing.T) {
	for _, scenario := range []struct {
		name              string
		before, after     string
		caret, afterCaret int
		key               gfx.Key
	}{
		{"repeated-insert", "AAA", "AAAA", 0, 1, 0},
		{"backspace", "abc", "ab", 3, 2, gfx.KeyBackspace},
		{"delete-forward", "abc", "ac", 1, 1, gfx.KeyDelete},
	} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{740, 1200} {
				test.Run(fmt.Sprintf("%s/dark-%v/width-%d", scenario.name, dark, width), func(context *testing.T) {
					state, driver, filename, _ := renderedSourceCollisionFixture(context, "abcd\n", dark, width)
					clickID(context, driver, renderedRows.At(0))
					if err := driver.Key(gfx.KeyEnter, 0); err != nil {
						context.Fatal(err)
					}
					if err := driver.Type(scenario.before); err != nil {
						context.Fatal(err)
					}
					state.rich.input.MoveTo(scenario.caret, false)
					if scenario.key != 0 {
						if err := driver.Key(scenario.key, 0); err != nil {
							context.Fatal(err)
						}
					} else if err := driver.Type("A"); err != nil {
						context.Fatal(err)
					}
					end := state.rich.input.RuneCount()
					state.rich.input.MoveTo(end, false)
					if err := driver.Key(gfx.KeyEnter, 0); err != nil {
						context.Fatal(err)
					}
					for cycle := 0; cycle < 2; cycle++ {
						for _, step := range []struct {
							command rune
							value   string
							caret   int
						}{
							{'z', scenario.after, end},
							{'z', scenario.before, scenario.caret},
							{'y', scenario.after, scenario.afterCaret},
							{'y', "", 0},
						} {
							if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
								context.Fatal(err)
							}
							if state.mode != renderedMode || !state.rich.active || !state.rich.virtual || state.rich.input.String() != step.value || state.rich.input.Selection() != (text.Range{Start: step.caret, End: step.caret}) || driver.Snapshot().Focused != richInputID {
								context.Fatal("ordinary virtual history lost its native caret identity")
							}
							body := "abcd\n\n" + step.value + "\n"
							if step.value == "" {
								body = "abcd\n\n" + scenario.after + "\n\n\n"
							}
							wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(body, "\n", "\r\n"))...)
							expectRenderedCollisionSave(context, state, driver, filename, wanted)
						}
					}
				})
			}
		}
	}
}

func TestRenderedVirtualHistoryKeepsNativeTypingGroup(test *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, width := range []int{740, 1200} {
			test.Run(fmt.Sprintf("dark-%v/width-%d", dark, width), func(context *testing.T) {
				state, driver, _, _ := renderedSourceCollisionFixture(context, "abcd\n", dark, width)
				clickID(context, driver, renderedRows.At(0))
				if err := driver.Key(gfx.KeyEnter, 0); err != nil {
					context.Fatal(err)
				}
				for _, value := range []string{"a", "b", "c"} {
					if err := driver.Type(value); err != nil {
						context.Fatal(err)
					}
				}
				for cycle := 0; cycle < 2; cycle++ {
					for _, step := range []struct {
						command rune
						value   string
						applied int
					}{
						{'z', "", 0}, {'y', "abc", 3},
					} {
						if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
							context.Fatal(err)
						}
						if state.mode != renderedMode || !state.rich.virtual || state.rich.input.String() != step.value || state.rich.appliedEdits != step.applied || len(state.rich.crossUndo) != step.applied || len(state.rich.crossRedo) != 3-step.applied {
							context.Fatal("virtual selection annotations changed native continuous typing groups")
						}
					}
				}
			})
		}
	}
}
