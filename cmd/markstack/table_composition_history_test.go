package main

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"

	"markstack/internal/markdownedit"
)

func TestRenderedTableUnmarkHistoryKeepsNativeSelection(test *testing.T) {
	unicodePrefix := "\U0001f469\u200d\U0001f4bb e\u0301"
	for _, scenario := range []struct {
		name     string
		before   string
		target   text.Range
		marked   string
		selected app.TextRange
		backward bool
		explicit bool
	}{
		{"interior-caret", "abZ", text.Range{Start: 1, End: 2}, "\u4e2d\u6587", app.TextRange{Start: 1, End: 1}, false, false},
		{"interior-selection", "abZ", text.Range{Start: 1, End: 2}, "\u4e2d\u6587", app.TextRange{Start: 0, End: 1}, false, false},
		{"unicode-prefix", unicodePrefix + "B\u5c3e", text.Range{Start: utf8.RuneCountInString(unicodePrefix), End: utf8.RuneCountInString(unicodePrefix) + 1}, "\u4f60\u597d", app.TextRange{Start: 1, End: 1}, false, false},
		{"repeated-text", "aaaZ", text.Range{Start: 1, End: 2}, "aa", app.TextRange{Start: 1, End: 1}, false, false},
		{"empty-marked", "abZ", text.Range{Start: 1, End: 2}, "", app.TextRange{}, false, false},
		{"empty-cell", "b", text.Range{Start: 0, End: 1}, "", app.TextRange{}, false, false},
		{"backward-origin", "a\u4e2d\u5c3eZ", text.Range{Start: 1, End: 3}, "ni", app.TextRange{Start: 1, End: 1}, true, false},
		{"backward-origin-explicit", "a\u4e2d\u5c3eZ", text.Range{Start: 1, End: 3}, "ni", app.TextRange{Start: 1, End: 1}, true, true},
	} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{740, 1200} {
				test.Run(fmt.Sprintf("%s/dark-%v/width-%d", scenario.name, dark, width), func(context *testing.T) {
					source := "# Keep\n\n| A |\n| --- |\n| " + scenario.before + " |\n\nTail **keep** &amp; <!-- opaque -->\n"
					state, driver, filename, original := renderedSourceCollisionFixture(context, source, dark, width)
					clickID(context, driver, renderedTableCells.At(1))
					current := state.session.Current()
					beforeSource := current.Text()
					target := scenario.target
					if scenario.backward {
						target.Start, target.End = target.End, target.Start
					}
					state.table.input.SetSelection(target)
					event := app.TextEvent{Kind: app.TextSetMarked, Text: scenario.marked, MarkedSelection: scenario.selected}
					if scenario.explicit {
						event.HasReplacement = true
						event.Replacement = app.TextRange{Start: scenario.target.Start, End: scenario.target.End}
					}
					if _, err := state.onTableCellTextEvent(event); err != nil {
						context.Fatal(err)
					}
					if scenario.backward {
						marked, _ := state.table.input.Composition()
						event.HasReplacement = true
						event.Replacement = app.TextRange{Start: marked.Start, End: marked.End}
						if _, err := state.onTableCellTextEvent(event); err != nil {
							context.Fatal(err)
						}
					}
					if err := driver.Resize(width, 780); err != nil {
						context.Fatal(err)
					}
					if !state.tableComposing() || current.Text() != beforeSource || current.Dirty() {
						context.Fatal("marked text entered committed source")
					}
					expectRenderedCollisionSave(context, state, driver, filename, original)
					if !state.tableComposing() {
						context.Fatal("blocked save ended the native composition")
					}
					if _, err := state.onTableCellTextEvent(app.TextEvent{Kind: app.TextUnmark}); err != nil {
						context.Fatal(err)
					}
					if err := driver.Resize(width, 780); err != nil {
						context.Fatal(err)
					}
					letters := []rune(scenario.before)
					committed := string(letters[:scenario.target.Start]) + scenario.marked + string(letters[scenario.target.End:])
					wantedSource := strings.Replace(beforeSource, scenario.before, committed, 1)
					wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(wantedSource)...)
					selected := text.Range{Start: scenario.target.Start + scenario.selected.Start, End: scenario.target.Start + scenario.selected.End}
					if current.Text() != wantedSource || state.tableComposing() || state.table.input.Selection() != selected {
						context.Fatalf("Unmark lost committed text/selection: text=%q selection=%+v", current.Text(), state.table.input.Selection())
					}
					expectRenderedCollisionSave(context, state, driver, filename, wanted)
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
					if state.mode != renderedMode || !state.table.active || state.table.input.Selection() != scenario.target || state.table.input.Caret() != target.End {
						context.Fatalf("Undo lost original selection direction: mode=%v selection=%+v caret=%d, want %+v caret=%d", state.mode, state.table.input.Selection(), state.table.input.Caret(), scenario.target, target.End)
					}
					expectRenderedCollisionSave(context, state, driver, filename, original)
					if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
						context.Fatal(err)
					}
					selection := text.Range{}
					if state.table.input != nil {
						selection = state.table.input.Selection()
					}
					if state.mode != renderedMode || !state.table.active || selection != selected {
						context.Fatalf("Redo lost native selection: mode=%v active=%v selection=%+v, want %+v", state.mode, state.table.active, selection, selected)
					}
					expectRenderedCollisionSave(context, state, driver, filename, wanted)
					if err := driver.Type("\u7eed"); err != nil {
						context.Fatal(err)
					}
					committedLetters := []rune(committed)
					continued := string(committedLetters[:selected.Start]) + "\u7eed" + string(committedLetters[selected.End:])
					continuedSource := strings.Replace(beforeSource, scenario.before, continued, 1)
					if committed == "" {
						continuedSource = strings.Replace(wantedSource, "|  |\r\n", "|  \u7eed|\r\n", 1)
					}
					continuedBytes := append([]byte{0xef, 0xbb, 0xbf}, []byte(continuedSource)...)
					expectRenderedCollisionSave(context, state, driver, filename, continuedBytes)
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
					if state.mode != renderedMode || !state.table.active || state.table.input.Selection() != selected {
						context.Fatal("typing Undo did not restore the committed native selection")
					}
					expectRenderedCollisionSave(context, state, driver, filename, wanted)
					clickID(context, driver, editModeID)
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
					expectRenderedCollisionSave(context, state, driver, filename, original)
					if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
						context.Fatal(err)
					}
					prefix := strings.Index(current.Text(), "| "+committed+" |") + len("| ")
					start := utf8.RuneCountInString(current.Text()[:prefix])
					if selection := current.Editor.Selection(); selection.Start != start+selected.Start || selection.End != start+selected.End {
						context.Fatalf("Source Redo lost the native selection: %+v, want %d..%d", selection, start+selected.Start, start+selected.End)
					}
					expectRenderedCollisionSave(context, state, driver, filename, wanted)
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
					expectRenderedCollisionSave(context, state, driver, filename, original)
				})
			}
		}
	}
}

func TestRenderedTableUnmarkDoesNotInventCommittedHistory(test *testing.T) {
	for _, scenario := range []string{"cancel", "same-text", "repeat-unmark"} {
		test.Run(scenario, func(context *testing.T) {
			source := "| A |\n| --- |\n| abZ |\n"
			state, driver, filename, original := renderedSourceCollisionFixture(context, source, false, 1200)
			clickID(context, driver, renderedTableCells.At(1))
			state.table.input.SetSelection(text.Range{Start: 1, End: 2})
			marked := "b"
			if scenario != "same-text" {
				marked = "\u4e2d"
			}
			if _, err := state.onTableCellTextEvent(app.TextEvent{Kind: app.TextSetMarked, Text: marked, MarkedSelection: app.TextRange{Start: 0, End: 1}}); err != nil {
				context.Fatal(err)
			}
			kind := app.TextUnmark
			if scenario == "cancel" {
				kind = app.TextCancelComposition
			}
			if _, err := state.onTableCellTextEvent(app.TextEvent{Kind: kind}); err != nil {
				context.Fatal(err)
			}
			if _, err := state.onTableCellTextEvent(app.TextEvent{Kind: app.TextUnmark}); err != nil {
				context.Fatal(err)
			}
			if err := driver.Resize(1200, 780); err != nil {
				context.Fatal(err)
			}
			current := state.session.Current()
			if scenario == "repeat-unmark" {
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
			}
			if current.Composing() || current.Editor.CanUndo() {
				context.Fatal("Unmark replay/cancel created an extra source history unit")
			}
			expectRenderedCollisionSave(context, state, driver, filename, original)
		})
	}
}

func TestRenderedTableSourceHistoryKeepsSelectionDirection(test *testing.T) {
	for _, scenario := range []struct {
		name      string
		target    text.Range
		caret     int
		narrowKey gfx.Key
		continued string
	}{
		{"forward", text.Range{Start: 1, End: 3}, 3, gfx.KeyLeft, "aQ\u5c3eZ"},
		{"backward", text.Range{Start: 3, End: 1}, 1, gfx.KeyRight, "a\u4e2dQZ"},
	} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{740, 1200} {
				test.Run(fmt.Sprintf("%s/dark-%v/width-%d", scenario.name, dark, width), func(context *testing.T) {
					source := "# Keep\n\n| A |\n| --- |\n| a\u4e2d\u5c3eZ |\n\nTail **keep** &amp; <!-- opaque -->\n"
					state, driver, filename, original := renderedSourceCollisionFixture(context, source, dark, width)
					clickID(context, driver, editModeID)
					current := state.session.Current()
					before := current.Text()
					prefix := strings.Index(before, "| a\u4e2d\u5c3eZ |") + len("| ")
					start := utf8.RuneCountInString(before[:prefix])
					current.Editor.SetSelection(text.Range{Start: start + scenario.target.Start, End: start + scenario.target.End})
					if err := driver.Type("X"); err != nil {
						context.Fatal(err)
					}
					edited := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.Replace(before, "a\u4e2d\u5c3eZ", "aXZ", 1))...)
					expectRenderedCollisionSave(context, state, driver, filename, edited)
					clickID(context, driver, renderedModeID)
					clickID(context, driver, renderedTableCells.At(1))
					for _, command := range []rune{'z', 'y', 'z'} {
						if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
							context.Fatal(err)
						}
						if command == 'y' {
							expectRenderedCollisionSave(context, state, driver, filename, edited)
							continue
						}
						if state.mode != renderedMode || !state.table.active || state.table.input == nil {
							context.Fatal("same-cell Source history abandoned rendered editing")
						}
						if selection := current.Editor.Selection(); selection != (text.Range{Start: start + 1, End: start + 3}) || current.Editor.Caret() != start+scenario.caret {
							context.Fatalf("Source history direction = %+v caret %d", selection, current.Editor.Caret())
						}
						if selection := state.table.input.Selection(); selection != (text.Range{Start: 1, End: 3}) || state.table.input.Caret() != scenario.caret {
							context.Fatalf("rendered history direction = %+v caret %d, want caret %d", selection, state.table.input.Caret(), scenario.caret)
						}
						expectRenderedCollisionSave(context, state, driver, filename, original)
					}
					if err := driver.Key(scenario.narrowKey, gfx.ModifierShift); err != nil {
						context.Fatal(err)
					}
					if err := driver.Type("Q"); err != nil {
						context.Fatal(err)
					}
					continued := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.Replace(before, "a\u4e2d\u5c3eZ", scenario.continued, 1))...)
					expectRenderedCollisionSave(context, state, driver, filename, continued)
					for _, command := range []rune{'z', 'y', 'z'} {
						if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
							context.Fatal(err)
						}
						wanted := original
						if command == 'y' {
							wanted = continued
						}
						expectRenderedCollisionSave(context, state, driver, filename, wanted)
					}
				})
			}
		}
	}
}

func TestTableCellSourceChangeRejectsOtherIntervals(test *testing.T) {
	base := "| A | B |\n| --- | --- |\n|  | keep |\n"
	for _, scenario := range []struct {
		name       string
		before     string
		after      string
		wrongCell  bool
		acceptable bool
	}{
		{"same-cell-padding", base, strings.Replace(base, "|  | keep |", "| b | keep |", 1), false, true},
		{"optional-outer-pipes", "A | B\n--- | ---\nold | keep\n", "A | B\n--- | ---\nnew | keep\n", false, true},
		{"neighbor-cell", base, strings.Replace(base, "keep", "changed", 1), false, false},
		{"outside-table", "# Before\n\n" + base, "# After\n\n" + base, false, false},
		{"readonly-target", strings.Replace(base, "|  | keep |", "| **b** | keep |", 1), strings.Replace(base, "|  | keep |", "| **c** | keep |", 1), false, false},
		{"different-column", base, base, true, false},
	} {
		test.Run(scenario.name, func(context *testing.T) {
			cell := func(source string) markdownedit.Cell {
				context.Helper()
				for _, block := range markdownedit.Parse([]byte(source)) {
					if block.Kind == markdownedit.Table && len(block.Cells) == 4 {
						return block.Cells[2]
					}
				}
				context.Fatal("fixture has no two-column physical table")
				return markdownedit.Cell{}
			}
			before, after := cell(scenario.before), cell(scenario.after)
			if scenario.wrongCell {
				after.Column++
			}
			if allowed := tableCellSourceChange(scenario.before, scenario.after, before, after); allowed != scenario.acceptable {
				context.Fatalf("cell-local source identity = %v, want %v", allowed, scenario.acceptable)
			}
		})
	}
}

func TestRenderedTableHistoryRestoresChangedNeighborCell(test *testing.T) {
	source := "| A | B |\n| --- | --- |\n| first | second |\n"
	state, driver, filename, original := renderedSourceCollisionFixture(test, source, false, 1200)
	clickID(test, driver, renderedTableCells.At(2))
	if err := driver.Type("X"); err != nil {
		test.Fatal(err)
	}
	clickID(test, driver, renderedTableCells.At(3))
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		test.Fatal(err)
	}
	if state.mode != renderedMode || !state.table.active || state.table.row != 1 || state.table.column != 0 || state.table.input.String() != "first" || state.table.input.Caret() != 5 || driver.Snapshot().Focused != tableInputID {
		test.Fatal("neighboring-cell history did not restore the actual changed cell")
	}
	expectRenderedCollisionSave(test, state, driver, filename, original)
	if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
		test.Fatal(err)
	}
	if state.mode != renderedMode || !state.table.active || state.table.row != 1 || state.table.column != 0 || state.table.input.String() != "firstX" || state.table.input.Caret() != 6 || driver.Snapshot().Focused != tableInputID {
		test.Fatal("neighboring-cell redo was rebound to the wrong cell")
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		test.Fatal(err)
	}
	expectRenderedCollisionSave(test, state, driver, filename, original)
}
