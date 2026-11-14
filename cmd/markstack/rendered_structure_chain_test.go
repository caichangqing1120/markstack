package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestRenderedStructureChainKeepsEveryTransaction(test *testing.T) {
	for _, scenario := range []struct {
		name             string
		body             string
		row              int
		caret            int
		firstKey         gfx.Key
		first            string
		firstRow         int
		firstCaret       int
		secondCaret      int
		secondKey        gfx.Key
		second           string
		secondRow        int
		secondAfterCaret int
		continued        string
	}{
		{"split", "abcd", 1, 2, gfx.KeyEnter, "ab\n\ncd", 2, 0, 1, gfx.KeyEnter, "ab\n\nc\n\nd", 3, 0, "ab\n\nc\n\nRd"},
		{"merge", "Left\n\nMiddle\n\nRight", 3, 0, gfx.KeyBackspace, "Left\n\nMiddleRight", 2, 6, 0, gfx.KeyBackspace, "LeftMiddleRight", 1, 4, "LeftRMiddleRight"},
		{"split-merge-cycle", "abcd", 1, 2, gfx.KeyEnter, "ab\n\ncd", 2, 0, 0, gfx.KeyBackspace, "abcd", 1, 2, "abRcd"},
		{"heading", "## abcd", 1, 2, gfx.KeyEnter, "## ab\n\ncd", 2, 0, 1, gfx.KeyEnter, "## ab\n\nc\n\nd", 3, 0, "## ab\n\nc\n\nRd"},
		{"unicode", "\u4e2d\u6587\U0001f469\u200d\U0001f4bbe\u0301Z", 1, 2, gfx.KeyEnter, "\u4e2d\u6587\n\n\U0001f469\u200d\U0001f4bbe\u0301Z", 2, 0, 3, gfx.KeyEnter, "\u4e2d\u6587\n\n\U0001f469\u200d\U0001f4bb\n\ne\u0301Z", 3, 0, "\u4e2d\u6587\n\n\U0001f469\u200d\U0001f4bb\n\nRe\u0301Z"},
	} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{740, 1200} {
				test.Run(fmt.Sprintf("%s/dark-%v/width-%d", scenario.name, dark, width), func(context *testing.T) {
					const prefix = "<!-- untouched -->\n\n"
					const suffix = "\n\nTail **keep** &amp; <!-- opaque -->\n"
					state, driver, filename, original := renderedSourceCollisionFixture(context, prefix+scenario.body+suffix, dark, width)
					clickID(context, driver, renderedRows.At(scenario.row))
					if state.mode != renderedMode || !state.rich.active {
						context.Fatal("supported structure-chain fixture did not activate")
					}
					state.rich.input.MoveTo(scenario.caret, false)
					if err := driver.Key(scenario.firstKey, 0); err != nil {
						context.Fatal(err)
					}
					state.rich.input.MoveTo(scenario.secondCaret, false)
					if err := driver.Key(scenario.secondKey, 0); err != nil {
						context.Fatal(err)
					}
					bytesFor := func(body string) []byte {
						return append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(prefix+body+suffix, "\n", "\r\n"))...)
					}
					assertPosition := func(row, caret int) {
						context.Helper()
						if state.mode != renderedMode || !state.rich.active || state.rich.index != row || state.rich.input.Selection() != (text.Range{Start: caret, End: caret}) || driver.Snapshot().Focused != richInputID {
							context.Fatalf("structure chain lost its rendered position: mode=%v active=%v row=%d, want row=%d caret=%d", state.mode, state.rich.active, state.rich.index, row, caret)
						}
					}
					assertPosition(scenario.secondRow, scenario.secondAfterCaret)
					expectRenderedCollisionSave(context, state, driver, filename, bytesFor(scenario.second))
					for cycle := 0; cycle < 2; cycle++ {
						for _, step := range []struct {
							command rune
							body    string
							row     int
							caret   int
						}{
							{'z', scenario.first, scenario.firstRow, scenario.secondCaret},
							{'z', scenario.body, scenario.row, scenario.caret},
							{'y', scenario.first, scenario.firstRow, scenario.firstCaret},
							{'y', scenario.second, scenario.secondRow, scenario.secondAfterCaret},
						} {
							if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
								context.Fatal(err)
							}
							assertPosition(step.row, step.caret)
							expectRenderedCollisionSave(context, state, driver, filename, bytesFor(step.body))
						}
					}
					if err := driver.Type("R"); err != nil {
						context.Fatal(err)
					}
					expectRenderedCollisionSave(context, state, driver, filename, bytesFor(scenario.continued))
					for _, step := range []struct {
						body  string
						row   int
						caret int
					}{{scenario.second, scenario.secondRow, scenario.secondAfterCaret}, {scenario.first, scenario.firstRow, scenario.secondCaret}, {scenario.body, scenario.row, scenario.caret}} {
						if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
							context.Fatal(err)
						}
						assertPosition(step.row, step.caret)
						expectRenderedCollisionSave(context, state, driver, filename, bytesFor(step.body))
					}
					if state.session.Current().Editor.CanUndo() {
						context.Fatal("structure metadata invented additional source transactions")
					}
					expectRenderedCollisionSave(context, state, driver, filename, original)
					state.rich.input.MoveTo(state.rich.input.RuneCount(), false)
					if err := driver.Type("N"); err != nil {
						context.Fatal(err)
					}
					for _, step := range []struct {
						command rune
						body    string
					}{{0, scenario.body + "N"}, {'z', scenario.body}, {'y', scenario.body + "N"}, {'y', scenario.body + "N"}} {
						if step.command != 0 {
							if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
								context.Fatal(err)
							}
						}
						if state.mode != renderedMode || !state.rich.active {
							context.Fatal("obsolete structure redo displaced a new source branch")
						}
						expectRenderedCollisionSave(context, state, driver, filename, bytesFor(step.body))
					}
				})
			}
		}
	}
}

func TestRenderedStructureChainRetainsCrossStyleSelections(test *testing.T) {
	for _, scenario := range []struct {
		name        string
		insert      string
		after       string
		caret       int
		split       string
		afterInsert int
	}{
		{"replacement", "X", "aXgh", 2, "aX\n\ngh", 2},
		{"identical-visible", "b cd ef", "ab cd efgh", 4, "ab c\n\nd efgh", 8},
	} {
		for _, backward := range []bool{false, true} {
			for _, dark := range []bool{false, true} {
				for _, width := range []int{740, 1200} {
					test.Run(fmt.Sprintf("%s/backward-%v/dark-%v/width-%d", scenario.name, backward, dark, width), func(context *testing.T) {
						const prefix = "<!-- untouched -->\n\n"
						const body = "ab **cd** efgh"
						const suffix = "\n\nTail **keep** &amp; <!-- opaque -->\n"
						state, driver, filename, original := renderedSourceCollisionFixture(context, prefix+body+suffix, dark, width)
						clickID(context, driver, renderedRows.At(1))
						target := text.Range{Start: 1, End: 8}
						if backward {
							target.Start, target.End = target.End, target.Start
						}
						state.rich.input.SetSelection(target)
						if err := driver.Type(scenario.insert); err != nil {
							context.Fatal(err)
						}
						state.rich.input.MoveTo(scenario.caret, false)
						if err := driver.Key(gfx.KeyEnter, 0); err != nil {
							context.Fatal(err)
						}
						bytesFor := func(body string) []byte {
							return append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(prefix+body+suffix, "\n", "\r\n"))...)
						}
						expectRenderedCollisionSave(context, state, driver, filename, bytesFor(scenario.split))
						for cycle := 0; cycle < 2; cycle++ {
							for _, step := range []struct {
								command rune
								bytes   []byte
								row     int
								caret   int
								selects bool
							}{
								{'z', bytesFor(scenario.after), 1, scenario.caret, false},
								{'z', original, 1, target.End, true},
								{'y', bytesFor(scenario.after), 1, scenario.afterInsert, false},
								{'y', bytesFor(scenario.split), 2, 0, false},
							} {
								if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
									context.Fatal(err)
								}
								wanted := text.Range{Start: step.caret, End: step.caret}
								if step.selects {
									wanted = text.Range{Start: 1, End: 8}
								}
								if state.mode != renderedMode || !state.rich.active || state.rich.index != step.row || state.rich.input.Selection() != wanted || state.rich.input.Caret() != step.caret || driver.Snapshot().Focused != richInputID {
									context.Fatal("rebuilt native cache lost the owned cross-style history selection")
								}
								expectRenderedCollisionSave(context, state, driver, filename, step.bytes)
							}
						}
					})
				}
			}
		}
	}
}

func TestRenderedStructureChainSurvivesSelectionCancellation(test *testing.T) {
	for _, escape := range []bool{false, true} {
		for _, redo := range []bool{false, true} {
			for _, dark := range []bool{false, true} {
				for _, width := range []int{740, 1200} {
					test.Run(fmt.Sprintf("escape-%v/redo-%v/dark-%v/width-%d", escape, redo, dark, width), func(context *testing.T) {
						const prefix = "<!-- untouched -->\n\n"
						const suffix = "\n\nTail **keep** &amp; <!-- opaque -->\n"
						state, driver, filename, original := renderedSourceCollisionFixture(context, prefix+"ab **cd** efgh"+suffix, dark, width)
						clickID(context, driver, renderedRows.At(1))
						state.rich.input.SetSelection(text.Range{Start: 1, End: 8})
						if err := driver.Type("X"); err != nil {
							context.Fatal(err)
						}
						for _, caret := range []int{2, 1} {
							state.rich.input.MoveTo(caret, false)
							if err := driver.Key(gfx.KeyEnter, 0); err != nil {
								context.Fatal(err)
							}
						}
						if redo {
							if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
								context.Fatal(err)
							}
						}
						row, caret := state.rich.index, state.rich.input.Caret()
						before := state.session.Current().Text()
						if err := driver.Key(gfx.KeyUp, gfx.ModifierShift); err != nil {
							context.Fatal(err)
						}
						if !state.selection.active || driver.Snapshot().Focused != renderedSelectionID {
							context.Fatal("selection cancellation fixture did not cross a block")
						}
						key := gfx.KeyDown
						modifiers := gfx.ModifierShift
						if escape {
							key, modifiers = gfx.KeyEscape, 0
						}
						if err := driver.Key(key, modifiers); err != nil {
							context.Fatal(err)
						}
						if state.selection.active || !state.rich.active || state.rich.index != row || state.rich.input.Caret() != caret || state.session.Current().Text() != before || driver.Snapshot().Focused != richInputID {
							context.Fatal("cancelling a cross-block selection changed its anchor or source")
						}
						if redo {
							if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
								context.Fatal(err)
							}
							if state.mode != renderedMode || !state.rich.active || state.rich.index != 3 || state.rich.input.Caret() != 0 {
								context.Fatal("selection cancellation discarded pending structure redo")
							}
						}
						for _, step := range []struct {
							body  string
							row   int
							caret int
						}{{"aX\n\ngh", 2, 1}, {"aXgh", 1, 2}, {"ab **cd** efgh", 1, 8}} {
							if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
								context.Fatal(err)
							}
							if state.mode != renderedMode || !state.rich.active || state.rich.index != step.row || state.rich.input.Caret() != step.caret || driver.Snapshot().Focused != richInputID {
								context.Fatal("selection cancellation discarded owned rendered history")
							}
							wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(prefix+step.body+suffix, "\n", "\r\n"))...)
							expectRenderedCollisionSave(context, state, driver, filename, wanted)
						}
						if state.rich.input.Selection() != (text.Range{Start: 1, End: 8}) || state.session.Current().Editor.CanUndo() {
							context.Fatal("selection cancellation changed the original source history or cross-style selection")
						}
						expectRenderedCollisionSave(context, state, driver, filename, original)
					})
				}
			}
		}
	}
}

func TestRenderedStructureChainInterleavesInputFormatAndEarlierCycles(test *testing.T) {
	for _, earlierCycle := range []bool{false, true} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{740, 1200} {
				test.Run(fmt.Sprintf("earlier-%v/dark-%v/width-%d", earlierCycle, dark, width), func(context *testing.T) {
					const prefix = "<!-- untouched -->\n\n"
					const suffix = "\n\nTail **keep** &amp; <!-- opaque -->\n"
					state, driver, filename, original := renderedSourceCollisionFixture(context, prefix+"abcdef"+suffix, dark, width)
					clickID(context, driver, renderedRows.At(1))
					if earlierCycle {
						if err := driver.Type("T"); err != nil {
							context.Fatal(err)
						}
						if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
							context.Fatal(err)
						}
					}
					state.rich.input.MoveTo(2, false)
					if err := driver.Key(gfx.KeyEnter, 0); err != nil {
						context.Fatal(err)
					}
					if err := driver.Type("P"); err != nil {
						context.Fatal(err)
					}
					state.applyBoldBlock()
					if err := driver.Pump(); err != nil {
						context.Fatal(err)
					}
					state.rich.input.MoveTo(3, false)
					if err := driver.Key(gfx.KeyEnter, 0); err != nil {
						context.Fatal(err)
					}
					if err := driver.Type("Q"); err != nil {
						context.Fatal(err)
					}
					state.applyHeadingLevel(2)
					if err := driver.Pump(); err != nil {
						context.Fatal(err)
					}
					stages := []struct {
						body      string
						row       int
						caret     int
						undoCaret int
					}{
						{"abcdef", 1, 2, 2},
						{"ab\n\ncdef", 2, 0, 0},
						{"ab\n\nPcdef", 2, 1, 1},
						{"ab\n\n**Pcdef**", 2, 1, 3},
						{"ab\n\n**Pcd**\n\n**ef**", 3, 0, 0},
						{"ab\n\n**Pcd**\n\n**Qef**", 3, 1, 1},
						{"ab\n\n**Pcd**\n\n## **Qef**", 3, 1, 1},
					}
					bytesFor := func(body string) []byte {
						return append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(prefix+body+suffix, "\n", "\r\n"))...)
					}
					check := func(position int, undo bool) {
						context.Helper()
						stage := stages[position]
						caret := stage.caret
						if undo {
							caret = stage.undoCaret
						}
						if state.mode != renderedMode || !state.rich.active || state.rich.index != stage.row || state.rich.input.Caret() != caret || driver.Snapshot().Focused != richInputID {
							context.Fatalf("mixed structure chain lost position %d: mode=%v active=%v row=%d", position, state.mode, state.rich.active, state.rich.index)
						}
						expectRenderedCollisionSave(context, state, driver, filename, bytesFor(stage.body))
					}
					check(len(stages)-1, false)
					for cycle := 0; cycle < 2; cycle++ {
						for position := len(stages) - 2; position >= 0; position-- {
							if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
								context.Fatal(err)
							}
							check(position, true)
						}
						if earlierCycle {
							for _, body := range []string{"abcdefT", "abcdef"} {
								if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
									context.Fatal(err)
								}
								if state.mode != renderedMode || !state.rich.active {
									context.Fatal("earlier same-source history consumed a structure annotation")
								}
								expectRenderedCollisionSave(context, state, driver, filename, bytesFor(body))
							}
						}
						if state.session.Current().Editor.CanUndo() {
							context.Fatal("mixed structure annotations invented source units")
						}
						for _, event := range []app.TextEvent{{Kind: app.TextSetMarked, Text: "pin"}, {Kind: app.TextCancelComposition}} {
							if _, err := state.onRenderedTextEvent(event); err != nil {
								context.Fatal(err)
							}
							state.syncRendered(state.session.Current())
						}
						if err := driver.Pump(); err != nil {
							context.Fatal(err)
						}
						expectRenderedCollisionSave(context, state, driver, filename, original)
						if earlierCycle {
							for _, body := range []string{"abcdefT", "abcdef"} {
								if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
									context.Fatal(err)
								}
								expectRenderedCollisionSave(context, state, driver, filename, bytesFor(body))
							}
						}
						for position := 1; position < len(stages); position++ {
							if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
								context.Fatal(err)
							}
							check(position, false)
						}
					}
					for position := len(stages) - 2; position >= 0; position-- {
						if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
							context.Fatal(err)
						}
						check(position, true)
					}
					if err := driver.Type("N"); err != nil {
						context.Fatal(err)
					}
					state.applyBoldBlock()
					if err := driver.Pump(); err != nil {
						context.Fatal(err)
					}
					for _, step := range []struct {
						command rune
						body    string
					}{{'z', "abNcdef"}, {'z', "abcdef"}, {'y', "abNcdef"}, {'y', "**abNcdef**"}, {'y', "**abNcdef**"}} {
						if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
							context.Fatal(err)
						}
						if state.mode != renderedMode || !state.rich.active || len(state.rich.structureRedo) != 0 {
							context.Fatal("obsolete mixed structure redo returned on the new branch")
						}
						expectRenderedCollisionSave(context, state, driver, filename, bytesFor(step.body))
					}
				})
			}
		}
	}
}

func TestRenderedStructureChainRetainsConsecutiveCrossBlockPastes(test *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, width := range []int{740, 1200} {
			test.Run(fmt.Sprintf("dark-%v/width-%d", dark, width), func(context *testing.T) {
				const prefix = "<!-- untouched -->\n\n"
				const suffix = "\n\nTail **keep** &amp; <!-- opaque -->\n"
				const body = "# Title\n\nBody\n\nTailText"
				state, driver, filename, original := renderedSourceCollisionFixture(context, prefix+body+suffix, dark, width)
				clickID(context, driver, renderedRows.At(1))
				for _, inserted := range []string{" plus", " more"} {
					if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
						context.Fatal(err)
					}
					driver.SetClipboard(inserted)
					if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
						context.Fatal(err)
					}
				}
				bytesFor := func(body string) []byte {
					return append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(prefix+body+suffix, "\n", "\r\n"))...)
				}
				expectRenderedCollisionSave(context, state, driver, filename, bytesFor("# Title plus more"))
				for cycle := 0; cycle < 2; cycle++ {
					for _, step := range []struct {
						command rune
						body    string
						caret   int
					}{{'z', "# Title plus\n\nTailText", 10}, {'z', body, 5}, {'y', "# Title plus\n\nTailText", 10}, {'y', "# Title plus more", 15}} {
						if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
							context.Fatal(err)
						}
						if state.mode != renderedMode || !state.rich.active || state.rich.index != 1 || state.rich.input.Selection() != (text.Range{Start: step.caret, End: step.caret}) || driver.Snapshot().Focused != richInputID {
							context.Fatal("cross-block selection discarded the earlier structure history")
						}
						expectRenderedCollisionSave(context, state, driver, filename, bytesFor(step.body))
					}
				}
				for range 2 {
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
				}
				expectRenderedCollisionSave(context, state, driver, filename, original)
			})
		}
	}
}
