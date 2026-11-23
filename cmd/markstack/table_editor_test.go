package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/ui"
)

func TestRenderedTableCellFocusPreservesRowShading(t *testing.T) {
	original := []byte("\xef\xbb\xbf| Left | Center | Right |\r\n| :--- | :---: | ---: |\r\n| one | two | three |\r\n| four | five | six |\r\n| seven | eight | nine |\r\n\r\nTail stays **unchanged**.\r\n")
	for _, dark := range []bool{false, true} {
		for _, width := range []int{740, 1200} {
			for row := 0; row < 4; row++ {
				for column := 0; column < 3; column++ {
					t.Run(fmt.Sprintf("dark-%v/width-%d/row-%d/column-%d", dark, width, row, column), func(t *testing.T) {
						path := filepath.Join(t.TempDir(), "table.md")
						if err := os.WriteFile(path, original, 0600); err != nil {
							t.Fatal(err)
						}
						state, driver := testDriver(t)
						state.dark = dark
						state.openFile(path)
						if err := driver.Resize(width, 780); err != nil {
							t.Fatal(err)
						}
						clickID(t, driver, renderedModeID)
						palette := referenceMarkdownColors(dark).palette
						want := palette.background
						if row%2 == 0 {
							want = palette.sidebar
						}
						clickID(t, driver, renderedTableCells.At(row*3+column))
						frame := driver.Snapshot()
						if frame.Focused != tableInputID || frame.Editor == nil {
							t.Fatal("table input did not take focus")
						}
						var cell ui.Rect
						for _, hit := range frame.Hits {
							if hit.ID == tableInputID {
								cell = hit.Bounds
								break
							}
						}
						if cell.Width <= 0 || cell.Height <= 0 {
							t.Fatal("active table cell has no bounds")
						}
						found := false
						var painted ui.Color
						point := ui.Point{X: cell.X + 4, Y: cell.Y + cell.Height - 4}
						for _, primitive := range frame.Primitives {
							box := primitive.Bounds
							if point.X >= box.X && point.X < box.X+box.Width && point.Y >= box.Y && point.Y < box.Y+box.Height {
								found = true
								painted = primitive.Color
							}
						}
						if !found {
							t.Fatal("active table cell did not paint its background")
						}
						if painted != want {
							t.Errorf("active row %d fill=%v, want reading row fill %v", row, painted, want)
						}
						if current := state.session.Current(); current.Dirty() || current.Text() != string(original[3:]) {
							t.Fatalf("focusing the cell changed the source: dirty=%v text=%q", current.Dirty(), current.Text())
						}
						if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
							t.Fatal(err)
						}
						if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, original) {
							t.Fatalf("no-edit save changed BOM/CRLF or source bytes: %q, %v", saved, err)
						}
					})
				}
			}
		}
	}
}

func TestRenderedTableCellEditsPhysicalEmptyCellWithoutRewritingNeighbors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "table.md")
	original := []byte("\xef\xbb\xbf# Head\r\n\r\n| A | B |\r\n| --- | --- |\r\n| old |  |\r\n\r\nTail\r\n")
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedTableCells.At(3))
	if driver.Snapshot().Focused != tableInputID || state.mode != renderedMode {
		t.Fatalf("table cell did not focus native input: mode=%v focus=%v", state.mode, driver.Snapshot().Focused)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	want := []byte("\xef\xbb\xbf# Head\r\n\r\n| A | B |\r\n| --- | --- |\r\n| old |  新|\r\n\r\nTail\r\n")
	if got := state.session.Current().Text(); got != string(want[3:]) || state.mode != renderedMode {
		t.Fatalf("rendered table edit=%q mode=%v", got, state.mode)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, want) {
		t.Fatalf("saved table bytes=%q, %v", data, err)
	}
}

func TestRenderedTableCellHeaderEditRoundTripsThroughSourceUndo(t *testing.T) {
	state, driver := testDriver(t)
	source := "| Heading | Note |\n| :--- | ---: |\n| row | value |\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedTableCells.At(0))
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "| Heading新 | Note |\n| :--- | ---: |\n| row | value |\n" {
		t.Fatalf("header cell source=%q", got)
	}
	clickID(t, driver, editModeID)
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != source {
		t.Fatalf("source Undo after header edit=%q", got)
	}
}

func TestRenderedTableCellPasteHasOneUndoStep(t *testing.T) {
	state, driver := testDriver(t)
	source := "| A |\n| --- |\n|  |\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedTableCells.At(1))
	driver.SetClipboard("乙")
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "| A |\n| --- |\n|  乙|\n" {
		t.Fatalf("table paste=%q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != source {
		t.Fatalf("table paste undo=%q", got)
	}
}

func TestRenderedTableCellUndoAfterTabNavigationUsesDocumentHistory(t *testing.T) {
	state, driver := testDriver(t)
	source := "| A | B |\n| --- | --- |\n| one |  |\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedTableCells.At(3))
	if err := driver.Type("乙"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyTab, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyTab, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != source || !state.table.active || state.table.input.String() != "" {
		t.Fatalf("undo after navigation=%q active=%v input=%q", got, state.table.active, state.table.input.String())
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "| A | B |\n| --- | --- |\n| one |  乙|\n" || !state.table.active || state.table.input.String() != "乙" {
		t.Fatalf("redo after navigation=%q active=%v input=%q", got, state.table.active, state.table.input.String())
	}
}

func TestRenderedTableCellRepeatedUndoDoesNotRestoreUndoneText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "table.md")
	source := "| A |\n| --- |\n| old |\n"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedTableCells.At(1))
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
			t.Fatal(err)
		}
		if got := state.session.Current().Text(); got != source || state.session.Current().Dirty() {
			t.Fatalf("Undo %d restored already-undone text: %q dirty=%v", attempt+1, got, state.session.Current().Dirty())
		}
	}
	clickID(t, driver, editModeID)
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != source {
		t.Fatalf("Source Undo resurrected table text: %q", got)
	}
}

func TestRenderedTableCellClearingUndoRestoresOriginalPadding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "table.md")
	source := "| A |\n| --- |\n| old |\n"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedTableCells.At(1))
	if _, err := driver.ShortcutChord(primaryModifier(), 'a'); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != source || state.session.Current().Dirty() {
		t.Fatalf("Undo changed original table padding: %q dirty=%v", got, state.session.Current().Dirty())
	}
}

func TestRenderedTableCellPreeditUndoRedoNeverCommitsSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "table.md")
	source := "| A |\n| --- |\n| old |\n"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedTableCells.At(1))
	if err := driver.Compose("pin"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []rune{'z', 'y'} {
		if _, err := driver.ShortcutChord(primaryModifier(), key); err != nil {
			t.Fatal(err)
		}
	}
	if got := state.session.Current().Text(); got != source || state.session.Current().Dirty() {
		t.Fatalf("preedit history leaked to source: %q dirty=%v", got, state.session.Current().Dirty())
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != source {
		t.Fatalf("preedit history saved to disk: %q, %v", data, err)
	}
}

func TestRenderedTableCellFocusPreservesAlignedTextPosition(t *testing.T) {
	for _, testcase := range []struct {
		name   string
		column int
		value  string
	}{
		{"center", 0, "middle"},
		{"right", 1, "rightmost"},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			state, driver := testDriver(t)
			state.session.Current().Replace("| Center | Right |\n| :---: | ---: |\n| middle | rightmost |\n")
			clickID(t, driver, renderedModeID)
			var idleX float32
			found := false
			for _, item := range driver.Snapshot().Texts {
				if item.Value == testcase.value {
					idleX, found = item.Bounds.X, true
					break
				}
			}
			if !found {
				t.Fatalf("missing rendered %s cell", testcase.value)
			}
			clickID(t, driver, renderedTableCells.At(2+testcase.column))
			frame := driver.Snapshot()
			if frame.Editor == nil || frame.Focused != tableInputID {
				t.Fatal("native table input not focused")
			}
			for _, item := range frame.Texts {
				if item.Value == testcase.value {
					if difference := item.Bounds.X - idleX; difference < -1 || difference > 1 {
						t.Errorf("aligned cell jumped horizontally by %.1f px on focus", difference)
					}
					return
				}
			}
			t.Fatal("focused cell text is not painted")
		})
	}
}

func TestRenderedTableCellKeepsFullNativeBoundsForNonemptyCells(t *testing.T) {
	for _, testcase := range []struct {
		name  string
		index int
	}{
		{"left", 3},
		{"center", 4},
		{"right", 5},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			state, driver := testDriver(t)
			source := "| Left | Center | Right |\n| --- | :---: | ---: |\n| leftmost | middle | rightmost |\n"
			state.session.Current().Replace(source)
			clickID(t, driver, renderedModeID)
			clickID(t, driver, renderedTableCells.At(testcase.index))
			frame := driver.Snapshot()
			if frame.Editor == nil {
				t.Fatal("native cell input is not focused")
			}
			var cell ui.Rect
			for _, hit := range frame.Hits {
				if hit.ID == tableInputID {
					cell = hit.Bounds
					break
				}
			}
			want := ui.Rect{X: cell.X + 13, Y: cell.Y + 6, Width: max(10, cell.Width-26), Height: max(20, cell.Height-12)}
			widthDifference := frame.Editor.Bounds.Width - want.Width
			if cell.Width == 0 || frame.Editor.Bounds.X != want.X || frame.Editor.Bounds.Y != want.Y || frame.Editor.Bounds.Height != want.Height || widthDifference < -0.001 || widthDifference > 0.001 {
				t.Fatalf("native input bounds = %+v, want full cell %+v", frame.Editor.Bounds, want)
			}
			if err := driver.Compose("ni'hao lengthy preedit"); err != nil {
				t.Fatal(err)
			}
			if after := driver.Snapshot().Editor; after == nil || after.Bounds != frame.Editor.Bounds || state.session.Current().Text() != source {
				t.Fatalf("preedit moved input or changed source: editor=%+v source=%q", after, state.session.Current().Text())
			}
		})
	}
}

func TestRenderedTableColumnsStayPutDuringCellTyping(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("| Center | Right |\n| :---: | ---: |\n| middle | data |\n")
	clickID(t, driver, renderedModeID)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	before := append([]float32(nil), state.preview.blocks[0].table.columns...)
	clickID(t, driver, renderedTableCells.At(3))
	if err := driver.Type("你好世界"); err != nil {
		t.Fatal(err)
	}
	after := state.preview.blocks[0].table.columns
	if len(after) != len(before) || after[0] != before[0] || after[1] != before[1] {
		t.Fatalf("typing shifted active table columns from %v to %v", before, after)
	}
	if state.session.Current().Text() != "| Center | Right |\n| :---: | ---: |\n| middle | data你好世界 |\n" {
		t.Fatalf("typing changed table source unexpectedly: %q", state.session.Current().Text())
	}
	if err := driver.Resize(900, 520); err != nil {
		t.Fatal(err)
	}
	resized := append([]float32(nil), state.preview.blocks[0].table.columns...)
	if state.preview.blocks[0].table.width <= before[0]+before[1] {
		t.Fatal("active table did not respond to viewport resize")
	}
	if err := driver.Type("尾"); err != nil {
		t.Fatal(err)
	}
	afterResize := state.preview.blocks[0].table.columns
	if len(afterResize) != len(resized) || afterResize[0] != resized[0] || afterResize[1] != resized[1] {
		t.Fatalf("typing after resize shifted columns from %v to %v", resized, afterResize)
	}
}

func TestRenderedTableCellPaintsTextOnlyOnce(t *testing.T) {
	for _, testcase := range []struct {
		name  string
		index int
		value string
		other string
	}{
		{"center", 2, "middle", "data"},
		{"right", 3, "data", "middle"},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			state, driver := testDriver(t)
			state.session.Current().Replace("| Center | Right |\n| :---: | ---: |\n| middle | data |\n")
			clickID(t, driver, renderedModeID)
			clickID(t, driver, renderedTableCells.At(testcase.index))
			frame := driver.Snapshot()
			if frame.Editor == nil || frame.Editor.Value != testcase.value || frame.Editor.Color.Alpha != 0 || !frame.Editor.HideNativeOverlays {
				t.Fatalf("native editor = %+v, want %q", frame.Editor, testcase.value)
			}
			activeLabels, otherLabels := 0, 0
			for _, label := range frame.Texts {
				if label.Value == testcase.value {
					activeLabels++
				}
				if label.Value == testcase.other {
					otherLabels++
				}
			}
			if activeLabels != 1 || otherLabels != 1 {
				t.Fatalf("active text labels=%d, neighbor labels=%d; want one each", activeLabels, otherLabels)
			}
		})
	}
}

func TestRenderedTableCellPointerAndKeyboardSelectAlignedText(test *testing.T) {
	state, driver := testDriver(test)
	const source = "| Left | Right |\n| --- | ---: |\n| keep | 字词 |\n"
	state.session.Current().Replace(source)
	clickID(test, driver, renderedModeID)
	clickID(test, driver, renderedTableCells.At(3))
	frame := driver.Snapshot()
	if frame.Editor == nil {
		test.Fatal("native cell input is not focused")
	}
	bounds := frame.Editor.Bounds
	left, right, middle := bounds.X+2, bounds.X+bounds.Width-1, bounds.Y+bounds.Height/2
	if err := driver.Press(right, middle, gfx.PointerButtonPrimary); err != nil {
		test.Fatal(err)
	}
	if err := driver.Move(left, middle); err != nil {
		test.Fatal(err)
	}
	if err := driver.Release(left, middle, gfx.PointerButtonPrimary); err != nil {
		test.Fatal(err)
	}
	if selected := app.TextStateOf(state.table.input); selected.Selection != (app.TextRange{Start: 0, End: 2}) || !selected.SelectionReversed {
		test.Fatalf("backward drag did not select the two visible characters: %+v", selected)
	}
	if err := driver.Key(gfx.KeyEnd, 0); err != nil {
		test.Fatal(err)
	}
	if selection := state.table.input.Selection(); selection.Start != 2 || selection.End != 2 {
		test.Fatalf("End did not collapse the selection at the line end: %+v", selection)
	}
	if err := driver.Key(gfx.KeyHome, gfx.ModifierShift); err != nil {
		test.Fatal(err)
	}
	if selected := app.TextStateOf(state.table.input); selected.Selection != (app.TextRange{Start: 0, End: 2}) || !selected.SelectionReversed {
		test.Fatalf("Shift+Home did not extend selection from the end: %+v", selected)
	}
	if state.session.Current().Text() != source {
		test.Fatal("selection changed Markdown source")
	}
}

func TestRenderedTableCellAlignedCompositionKeepsNativeBounds(t *testing.T) {
	for _, testcase := range []struct {
		name  string
		index int
	}{
		{"center header", 0},
		{"right empty", 3},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			state, driver := testDriver(t)
			state.session.Current().Replace("| Center | Right |\n| :---: | ---: |\n| old |  |\n")
			clickID(t, driver, renderedModeID)
			clickID(t, driver, renderedTableCells.At(testcase.index))
			initial := driver.Snapshot().Editor
			if initial == nil {
				t.Fatal("native cell input is not focused")
			}
			for _, marked := range []string{"n", "ni", "ni'hao"} {
				if err := driver.Compose(marked); err != nil {
					t.Fatal(err)
				}
				current := driver.Snapshot().Editor
				if current == nil || current.Bounds != initial.Bounds {
					t.Fatalf("preedit %q moved native input: initial=%+v current=%+v", marked, initial.Bounds, current)
				}
				if state.session.Current().Text() != "| Center | Right |\n| :---: | ---: |\n| old |  |\n" {
					t.Fatalf("preedit %q changed source", marked)
				}
			}
		})
	}
}

func TestRenderedEmptyRightCellShowsCompositionWithinColumn(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("| Center | Right |\n| :---: | ---: |\n| old |  |\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedTableCells.At(3))
	frame := driver.Snapshot()
	if frame.Editor == nil || frame.Editor.Bounds.Width < 150 {
		t.Fatalf("empty right cell clips native preedit: editor=%+v", frame.Editor)
	}
	for _, hit := range frame.Hits {
		if hit.ID == tableInputID {
			if frame.Editor.Bounds.X < hit.Bounds.X+12 || frame.Editor.Bounds.X+frame.Editor.Bounds.Width > hit.Bounds.X+hit.Bounds.Width-12 {
				t.Fatalf("native preedit extends outside cell: editor=%+v cell=%+v", frame.Editor.Bounds, hit.Bounds)
			}
			return
		}
	}
	t.Fatal("missing active cell hit")
}

func TestRenderedTableCellDuplicateTableTargetsSecondSource(t *testing.T) {
	state, driver := testDriver(t)
	source := "| A |\n| --- |\n| same |\n\n| A |\n| --- |\n| same |\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedTableCells.At(3))
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "| A |\n| --- |\n| same |\n\n| A |\n| --- |\n| sameX |\n" {
		t.Fatalf("edited wrong duplicate table: %q", got)
	}
}

func TestRenderedTableCellComplexContentFallsBackToSource(t *testing.T) {
	state, driver := testDriver(t)
	source := "| A | B |\n| --- | --- |\n| **bold** | plain |\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedTableCells.At(2))
	if state.mode != editMode || driver.Snapshot().Focused != sourceID || state.session.Current().Text() != source {
		t.Fatalf("complex table cell was not safely routed to Source: mode=%v focus=%v source=%q", state.mode, driver.Snapshot().Focused, state.session.Current().Text())
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
}

func TestRenderedTableCellTabNavigatesExistingCells(t *testing.T) {
	state, driver := testDriver(t)
	source := "| A | B |\n| --- | --- |\n| one | two |\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedTableCells.At(0))
	if err := driver.Key(gfx.KeyTab, 0); err != nil {
		t.Fatal(err)
	}
	if !state.table.active || state.table.row != 0 || state.table.column != 1 || driver.Snapshot().Focused != tableInputID {
		t.Fatalf("Tab did not move to next cell: state=%+v focus=%v", state.table, driver.Snapshot().Focused)
	}
	if err := driver.Key(gfx.KeyTab, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if !state.table.active || state.table.row != 0 || state.table.column != 0 || state.session.Current().Text() != source {
		t.Fatalf("Shift+Tab changed source or lost cell: state=%+v", state.table)
	}
}

func TestRenderedTableCellCompositionBlocksAutoAndManualSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "table.md")
	original := []byte("| A |\n| --- |\n| old |\n")
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	state.preferences.AutoSave = true
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	state.autoSave.clock = func() time.Time { return now }
	state.autoSave.delay = time.Second
	t.Cleanup(state.stopAutoSave)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedTableCells.At(1))
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Compose("pin"); err != nil {
		t.Fatal(err)
	}
	state.scheduleAutoSave()
	now = now.Add(2 * time.Second)
	state.drainAutoSave()
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, original) || strings.Contains(state.session.Current().Text(), "pin") {
		t.Fatalf("preedit allowed auto-save: %q, %v source=%q", data, err, state.session.Current().Text())
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, original) || !strings.Contains(state.message, "composition") {
		t.Fatalf("preedit allowed manual Save: %q, %v message=%q", data, err, state.message)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(state.message, "composition") {
		t.Fatalf("committed cell retained a preedit warning: %q", state.message)
	}
	state.scheduleAutoSave()
	now = now.Add(2 * time.Second)
	state.drainAutoSave()
	if data, err := os.ReadFile(path); err != nil || string(data) != "| A |\n| --- |\n| oldX新 |\n" {
		t.Fatalf("committed cell not auto-saved: %q, %v", data, err)
	}
}

func TestRenderedTableCellUndoRedoSharesSourceHistory(t *testing.T) {
	state, driver := testDriver(t)
	source := "| A |\n| --- |\n| old |\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedTableCells.At(1))
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != source {
		t.Fatalf("table Undo=%q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "| A |\n| --- |\n| old新 |\n" {
		t.Fatalf("table Redo=%q", got)
	}
	clickID(t, driver, editModeID)
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != source {
		t.Fatalf("source Undo after table Redo=%q", got)
	}
}

func TestRenderedTableCellRedoRestoresCaretForNextInput(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("| A |\n| --- |\n| old |\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedTableCells.At(1))
	if err := driver.Type("你好"); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("尾"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "| A |\n| --- |\n| old你好尾 |\n" {
		t.Fatalf("next input after Redo used stale caret: %q", got)
	}
}

func TestRenderedTableCellClearingKeepsRenderedInput(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("| A |\n| --- |\n| old |\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedTableCells.At(1))
	if _, err := driver.ShortcutChord(primaryModifier(), 'a'); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "| A |\n| --- |\n|  |\n" || state.mode != renderedMode || !state.table.active {
		t.Fatalf("cleared cell lost rendered editing: %q mode=%v active=%v", got, state.mode, state.table.active)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "| A |\n| --- |\n|  新|\n" || state.mode != renderedMode {
		t.Fatalf("refilled cell=%q mode=%v", got, state.mode)
	}
}

func TestRenderedTableCellFocusLeavesTableForAnotherBlock(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Head\n\n| A |\n| --- |\n| old |\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedTableCells.At(1))
	clickID(t, driver, renderedRows.At(0))
	if state.table.active || !state.rich.active || driver.Snapshot().Focused != richInputID {
		t.Fatalf("table focus survived block switch: table=%v rich=%v focus=%v", state.table.active, state.rich.active, driver.Snapshot().Focused)
	}
}

func TestRenderedTableCellExternalConflictAndPendingSaveKeepDraft(t *testing.T) {
	path := filepath.Join(t.TempDir(), "table.md")
	if err := os.WriteFile(path, []byte("| A |\n| --- |\n| old |\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedTableCells.At(1))
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("| A |\n| --- |\n| external |\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "| A |\n| --- |\n| external |\n" || !state.session.Current().Dirty() {
		t.Fatalf("conflict lost external bytes or dirty draft: %q, %v", data, err)
	}
	clickID(t, driver, tabClose.At(state.session.Active))
	if state.session.Pending() == nil {
		t.Fatal("dirty table tab closed without confirmation")
	}
	clickID(t, driver, saveConfirmID)
	if data, err := os.ReadFile(path); err != nil || string(data) != "| A |\n| --- |\n| external |\n" || state.session.Pending() == nil || !state.session.Current().Dirty() {
		t.Fatalf("pending Save overwrote external edit or lost draft: %q, %v", data, err)
	}
}

func TestRenderedTableCellStructuralInputSwitchesToSourceWithoutDroppingText(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("| A |\n| --- |\n| old |\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedTableCells.At(1))
	if err := driver.Type("|"); err != nil {
		t.Fatal(err)
	}
	if state.mode != editMode || state.session.Current().Text() != "| A |\n| --- |\n| old| |\n" || driver.Snapshot().Focused != sourceID {
		t.Fatalf("unsafe input was lost or source focus missing: %q mode=%v focus=%v", state.session.Current().Text(), state.mode, driver.Snapshot().Focused)
	}
	if err := driver.Type("X"); err != nil || state.session.Current().Text() != "| A |\n| --- |\n| old|X |\n" {
		t.Fatalf("continued Source input=%q, %v", state.session.Current().Text(), err)
	}
}

func TestRenderedTableCellParserPaddedCellLocatesItsSourceRow(t *testing.T) {
	state, driver := testDriver(t)
	source := "# Head\n\n| A | B | C |\n| --- | --- | --- |\n| one | two |\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedTableCells.At(5))
	if state.mode != editMode || state.session.Current().Text() != source || state.session.Current().Editor.Caret() < len([]rune("# Head\n\n| A | B | C |\n| --- | --- | --- |\n| one")) {
		t.Fatalf("missing cell did not focus its source row: mode=%v caret=%d", state.mode, state.session.Current().Editor.Caret())
	}
}

func TestRenderedTableCellCompositionDefersTabUntilCommit(t *testing.T) {
	state, driver := testDriver(t)
	source := "| A | B |\n| --- | --- |\n| one | two |\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedTableCells.At(2))
	if err := driver.Compose("pin"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyTab, 0); err != nil {
		t.Fatal(err)
	}
	if !state.table.active || state.table.row != 1 || state.table.column != 0 || state.session.Current().Text() != source {
		t.Fatal("Tab committed preedit or moved to another cell")
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "| A | B |\n| --- | --- |\n| one新 | two |\n" {
		t.Fatalf("committed composition=%q", got)
	}
}

func TestRenderedTableCellAfterNestedListDoesNotGuessSourceMapping(t *testing.T) {
	state, driver := testDriver(t)
	source := "- parent\n  - nested\n\n| A |\n| --- |\n| target |\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	var tableHit gfx.ID
	for index := range state.parsed.Blocks {
		if state.parsed.Blocks[index].Kind == markdown.Table {
			tableHit = renderedTableCells.At(1)
			break
		}
	}
	if tableHit.IsZero() {
		t.Fatal("fixture did not render a table")
	}
	clickID(t, driver, tableHit)
	if state.mode != editMode || state.session.Current().Text() != source || state.session.Current().Editor.Caret() < len([]rune("- parent\n  - nested\n\n")) {
		t.Fatalf("nested structure mislocated table: mode=%v caret=%d", state.mode, state.session.Current().Editor.Caret())
	}
}

func TestRenderedTableInsertionThenCellTypingKeepsNeighborsAndHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inserted-table.md")
	source := "# Title\n\nBody\n\nTail\n"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	clickID(t, driver, headingMenuID)
	clickID(t, driver, blockFormats.At(5))
	inserted := "# Title\n\nBody\n\n| 列 1 | 列 2 |\n| --- | --- |\n|     |     |\n\nTail\n"
	if state.mode != editMode || state.session.Current().Text() != inserted {
		t.Fatalf("table insertion mode=%v source=%q", state.mode, state.session.Current().Text())
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedTableCells.At(0))
	if !state.table.active || driver.Snapshot().Focused != tableInputID {
		t.Fatalf("inserted table did not focus a native cell: active=%v focus=%v", state.table.active, driver.Snapshot().Focused)
	}
	if err := driver.Type("甲"); err != nil {
		t.Fatal(err)
	}
	want := "# Title\n\nBody\n\n| 列 1甲 | 列 2 |\n| --- | --- |\n|     |     |\n\nTail\n"
	if got := state.session.Current().Text(); got != want || state.mode != renderedMode {
		t.Fatalf("typing after table insertion mode=%v source=%q, want %q", state.mode, got, want)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || string(saved) != want {
		t.Fatalf("inserted table save=%q, %v", saved, err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != inserted {
		t.Fatalf("inserted table Undo=%q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != want {
		t.Fatalf("inserted table Redo=%q", got)
	}
}

func TestRenderedTableInteriorCutRedoKeepsRepeatedTextCaret(test *testing.T) {
	for _, body := range []string{"aaaZ", "\u54c8\u54c8\u54c8Z"} {
		test.Run(body, func(context *testing.T) {
			source := "| A |\n| --- |\n| " + body + " |\n\nTail **keep**\n"
			state, driver, filename, original := renderedSourceCollisionFixture(context, source, false, 1200)
			clickID(context, driver, renderedTableCells.At(1))
			if err := driver.Key(gfx.KeyHome, 0); err != nil {
				context.Fatal(err)
			}
			if err := driver.Key(gfx.KeyRight, 0); err != nil {
				context.Fatal(err)
			}
			if err := driver.Key(gfx.KeyRight, gfx.ModifierShift); err != nil {
				context.Fatal(err)
			}
			for _, key := range []rune{'x', 'z'} {
				if _, err := driver.ShortcutChord(primaryModifier(), key); err != nil {
					context.Fatal(err)
				}
			}
			if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
				context.Fatal(err)
			}
			selection := state.table.input.Selection()
			if selection.Start != 1 || selection.End != 1 {
				context.Fatalf("repeated-text Redo caret=%+v, want 1", selection)
			}
			if err := driver.Type("X"); err != nil {
				context.Fatal(err)
			}
			letters := []rune(body)
			replacement := string(letters[:1]) + "X" + string(letters[2:])
			wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(strings.Replace(source, body, replacement, 1), "\n", "\r\n"))...)
			expectRenderedCollisionSave(context, state, driver, filename, wanted)
			for range 2 {
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
			}
			expectRenderedCollisionSave(context, state, driver, filename, original)
		})
	}
}
