package main

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

type affinityPaintedRow struct {
	start, end int
	y, endX    float32
}

func affinityDriver(t *testing.T, source string) (*model, *app.Driver, *gfx.Canvas) {
	t.Helper()
	engine := app.PlatformTextEngine()
	if engine == nil {
		t.Skip("platform text engine unavailable")
	}
	state := newModel()
	state.mode = renderedMode
	state.session.Current().Replace(source)
	canvas := &gfx.Canvas{}
	canvas.SetTextEngine(engine)
	driver, err := app.NewDriver(newConfig(state), canvas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	if err := driver.Resize(740, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedRows.At(0))
	return state, driver, canvas
}

func affinityPaintedRows(t *testing.T, state *model, canvas *gfx.Canvas, value string) []affinityPaintedRow {
	t.Helper()
	var rows []affinityPaintedRow
	var visible strings.Builder
	for _, label := range canvas.Texts() {
		if label.Style.Color.Alpha <= 0 || label.Value == "" || !label.HasClip || label.ClipOf != state.previewRect ||
			label.Bounds.Y < state.rich.rect.Y || label.Bounds.Y >= state.rich.rect.Y+state.rich.rect.Height {
			continue
		}
		if len(rows) == 0 || rows[len(rows)-1].y != label.Bounds.Y {
			rows = append(rows, affinityPaintedRow{start: visible.Len(), y: label.Bounds.Y})
		}
		visible.WriteString(label.Value)
		row := &rows[len(rows)-1]
		row.end = visible.Len()
		row.endX = label.Bounds.X + canvas.TextCaretStyle(label.Value, label.Style, len(label.Value))
	}
	if visible.String() != value || len(rows) < 3 {
		t.Fatalf("painted rows do not cover the controlled text: rows=%+v visible=%q want=%q", rows, visible.String(), value)
	}
	return rows
}

func TestRenderedInteriorLineEdgesKeepVisualAffinity(t *testing.T) {
	value := strings.Repeat("排版文字", 20)
	for _, test := range []struct{ name, source, visible string }{
		{"plain", value, value},
		{"bold", "**" + value + "**", value},
		{"italic", "*" + value + "*", value},
		{"mixed", "前**" + value + "**后", "前" + value + "后"},
		{"heading", "# " + value, value},
		{"graphemes", strings.Repeat("段👩‍💻e\u0301文", 20), strings.Repeat("段👩‍💻e\u0301文", 20)},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := test.source + "\n\nTail\n"
			state, driver, canvas := affinityDriver(t, source)
			rows := affinityPaintedRows(t, state, canvas, test.visible)
			checkCaret := func(stage string, offset int, row affinityPaintedRow) {
				t.Helper()
				caret, ok := state.rich.CaretBounds()
				if got := byteOffsetOfRune(test.visible, state.rich.input.Caret()); got != offset || !ok || math.Abs(float64(caret.Y-row.y)) > 0.01 {
					t.Errorf("%s caret offset=%d rect=%+v ok=%v, want byte=%d on row y=%v", stage, got, caret, ok, offset, row.y)
				}
			}
			for _, row := range rows[:len(rows)-1] {
				inside := gfx.GraphemeStart(test.visible, row.start+(row.end-row.start)/2)
				position := utf8.RuneCountInString(test.visible[:inside])
				if err := driver.SelectText(position, position); err != nil {
					t.Fatal(err)
				}
				for repeat := 0; repeat < 2; repeat++ {
					if err := driver.Key(gfx.KeyEnd, 0); err != nil {
						t.Fatal(err)
					}
					checkCaret("End", row.end, row)
				}
				if err := driver.Key(gfx.KeyHome, gfx.ModifierShift); err != nil {
					t.Fatal(err)
				}
				checkCaret("Shift+Home", row.start, row)
				selection := state.rich.input.Selection()
				if selection.Start != utf8.RuneCountInString(test.visible[:row.start]) || selection.End != utf8.RuneCountInString(test.visible[:row.end]) {
					t.Errorf("Shift+Home selected a different visual row: %+v", selection)
				}
				if _, err := driver.ShortcutChord(primaryModifier(), 'c'); err != nil {
					t.Fatal(err)
				}
				if got := driver.Clipboard(); got != test.visible[row.start:row.end] {
					t.Errorf("row Copy=%q, want %q", got, test.visible[row.start:row.end])
				}
				if err := driver.Key(gfx.KeyEnd, gfx.ModifierShift); err != nil {
					t.Fatal(err)
				}
				checkCaret("Shift+End", row.end, row)
				if err := driver.Key(gfx.KeyLeft, gfx.ModifierMeta); err != nil {
					t.Fatal(err)
				}
				checkCaret("Command+Left", row.start, row)
				if err := driver.Key(gfx.KeyRight, gfx.ModifierMeta); err != nil {
					t.Fatal(err)
				}
				checkCaret("Command+Right", row.end, row)
			}
			if state.session.Current().Text() != source {
				t.Fatal("visual-row navigation changed source")
			}
		})
	}
}

func TestRenderedSoftWrapPointerKeepsBothSidesOfTheSameOffset(t *testing.T) {
	value := strings.Repeat("排版文字", 20)
	graphemes := strings.Repeat("段👩‍💻e\u0301文", 20)
	for _, test := range []struct{ name, source, visible string }{
		{"plain", value + "\n\nTail\n", value},
		{"bold", "**" + value + "**\n\nTail\n", value},
		{"graphemes", graphemes + "\n\nTail\n", graphemes},
		{"bold-graphemes", "**" + graphemes + "**\n\nTail\n", graphemes},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, driver, canvas := affinityDriver(t, test.source)
			rows := affinityPaintedRows(t, state, canvas, test.visible)
			ending := rows[0]
			if err := driver.Click(min(ending.endX+2, state.rich.rect.X+state.rich.rect.Width-1), ending.y+10); err != nil {
				t.Fatal(err)
			}
			caret, ok := state.rich.CaretBounds()
			if got := byteOffsetOfRune(test.visible, state.rich.input.Caret()); got != ending.end || !ok || caret.Y != ending.y || math.Abs(float64(caret.X-ending.endX)) > 0.1 {
				t.Errorf("row-end click lost its side: byte=%d caret=%+v ok=%v, want byte=%d x/y=%v/%v", got, caret, ok, ending.end, ending.endX, ending.y)
			}
			if err := driver.Click(state.rich.rect.X+1, rows[1].y+10); err != nil {
				t.Fatal(err)
			}
			caret, ok = state.rich.CaretBounds()
			if got := byteOffsetOfRune(test.visible, state.rich.input.Caret()); got != ending.end || !ok || caret.Y != rows[1].y {
				t.Errorf("next-row click retained stale upstream affinity: byte=%d caret=%+v ok=%v", got, caret, ok)
			}
			if state.session.Current().Text() != test.source {
				t.Fatal("soft-wrap affinity clicks changed source")
			}
		})
	}
}

func TestRenderedVerticalMovementStartsFromAffinedLineEnd(t *testing.T) {
	value := strings.Repeat("排版文字", 24)
	graphemes := strings.Repeat("段👩‍💻e\u0301文", 24)
	for _, test := range []struct{ name, source, visible string }{
		{"plain", value + "\n\nTail\n", value},
		{"bold", "**" + value + "**\n\nTail\n", value},
		{"graphemes", graphemes + "\n\nTail\n", graphemes},
		{"bold-graphemes", "**" + graphemes + "**\n\nTail\n", graphemes},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, driver, canvas := affinityDriver(t, test.source)
			rows := affinityPaintedRows(t, state, canvas, test.visible)
			middle := rows[1]
			interior := gfx.GraphemeStart(test.visible, middle.start+(middle.end-middle.start)/2)
			position := utf8.RuneCountInString(test.visible[:interior])
			if err := driver.SelectText(position, position); err != nil {
				t.Fatal(err)
			}
			if err := driver.Key(gfx.KeyEnd, 0); err != nil {
				t.Fatal(err)
			}
			for _, step := range []struct {
				key gfx.Key
				row int
			}{{gfx.KeyUp, 0}, {gfx.KeyDown, 1}, {gfx.KeyDown, 2}, {gfx.KeyUp, 1}} {
				if err := driver.Key(step.key, 0); err != nil {
					t.Fatal(err)
				}
				caret, ok := state.rich.CaretBounds()
				if !ok || caret.Y != rows[step.row].y {
					t.Errorf("vertical key %v caret=%+v ok=%v, want row %d at y=%v", step.key, caret, ok, step.row, rows[step.row].y)
				}
				if offset := byteOffsetOfRune(test.visible, state.rich.input.Caret()); gfx.GraphemeStart(test.visible, offset) != offset {
					t.Errorf("vertical key %v split a visible grapheme at byte %d", step.key, offset)
				}
			}
			if state.session.Current().Text() != test.source {
				t.Fatal("vertical movement changed source")
			}
		})
	}
}

func TestRenderedLineEdgeReplacementPreservesBOMCRLFAndUndo(t *testing.T) {
	value := strings.Repeat("排版文字", 20)
	source := "**" + value + "**\r\n\r\nTail\r\n<!--keep-->\r\n"
	path := filepath.Join(t.TempDir(), "Affinity.md")
	original := append([]byte{0xef, 0xbb, 0xbf}, []byte(source)...)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	state, driver, canvas := affinityDriver(t, source)
	state.openFile(path)
	if err := driver.Resize(740, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedRows.At(0))
	rows := affinityPaintedRows(t, state, canvas, value)
	row := rows[1]
	position := utf8.RuneCountInString(value[:row.start]) + 2
	if err := driver.SelectText(position, position); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnd, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyHome, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	want := append([]byte{0xef, 0xbb, 0xbf}, []byte("**"+value[:row.start]+"新"+value[row.end:]+"**\r\n\r\nTail\r\n<!--keep-->\r\n")...)
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, want) {
		t.Fatalf("visual row replacement changed wrong source bytes: %q, %v", saved, err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, original) {
		t.Fatalf("visual row Undo/Save did not restore original: %q, %v", saved, err)
	}
}

func TestRenderedUpstreamCaretLeavesReaderAndMainCanvasUntouched(t *testing.T) {
	value := strings.Repeat("排版文字", 20)
	state, driver, canvas := affinityDriver(t, "**"+value+"**\n\nTail\n")
	rows := affinityPaintedRows(t, state, canvas, value)
	if err := driver.SelectText(2, 2); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnd, 0); err != nil {
		t.Fatal(err)
	}
	reader := &state.preview.blocks[0].view
	before, ok := reader.CaretAt(rows[0].end)
	if !ok || before.Y != rows[1].y {
		t.Fatalf("reader's default boundary side changed: %+v, %v", before, ok)
	}
	labels, rectangles := len(canvas.Texts()), len(canvas.Rectangles())
	for repeat := 0; repeat < 5; repeat++ {
		caret, ok := state.rich.CaretBounds()
		if !ok || caret.Y != rows[0].y {
			t.Fatalf("upstream caret missing: %+v, %v", caret, ok)
		}
	}
	if after, ok := reader.CaretAt(rows[0].end); !ok || after != before {
		t.Errorf("caret query moved the source reader: before=%+v after=%+v ok=%v", before, after, ok)
	}
	if len(canvas.Texts()) != labels || len(canvas.Rectangles()) != rectangles {
		t.Fatal("caret measurement added hidden text or selection rectangles to the visible canvas")
	}
}

func TestRenderedExplicitSelectionClearsSoftWrapAffinity(t *testing.T) {
	value := strings.Repeat("排版文字", 20)
	state, driver, canvas := affinityDriver(t, "**"+value+"**\n\nTail\n")
	rows := affinityPaintedRows(t, state, canvas, value)
	if err := driver.SelectText(2, 2); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnd, 0); err != nil {
		t.Fatal(err)
	}
	position := utf8.RuneCountInString(value[:rows[0].end])
	if err := driver.SelectText(position, position); err != nil {
		t.Fatal(err)
	}
	if caret, ok := state.rich.CaretBounds(); !ok || caret.Y != rows[1].y {
		t.Errorf("explicit selection inherited the previous End-key side: %+v, %v", caret, ok)
	}
}

func TestRenderedLineEdgeArtifacts(t *testing.T) {
	artifacts := os.Getenv("MARKSTACK_AFFINITY_ARTIFACT_DIR")
	if artifacts == "" {
		t.Skip("acceptance artifact directory not set")
	}
	if err := os.MkdirAll(artifacts, 0755); err != nil {
		t.Fatal(err)
	}
	value := strings.Repeat("排版文字", 20)
	source := "**" + value + "**\r\n\r\nTail\r\n<!--keep-->\r\n"
	path := filepath.Join(t.TempDir(), "LineEdges.md")
	original := append([]byte{0xef, 0xbb, 0xbf}, []byte(source)...)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	state := newModel()
	state.mode = renderedMode
	var canvas gfx.Canvas
	session, err := headless.Open(newConfig(state), &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	state.openFile(path)
	if err := session.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	driver := session.Driver()
	clickID(t, driver, renderedRows.At(0))
	rows := affinityPaintedRows(t, state, &canvas, value)
	row := rows[1]
	position := utf8.RuneCountInString(value[:row.start]) + 2
	if err := driver.SelectText(position, position); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnd, 0); err != nil {
		t.Fatal(err)
	}
	if caret, ok := state.rich.CaretBounds(); !ok || caret.Y != row.y {
		t.Fatalf("artifact End moved to a different visual row: %+v, %v", caret, ok)
	}
	if err := session.Screenshot(filepath.Join(artifacts, "rendered-line-edge-end-740.png")); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyHome, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'c'); err != nil {
		t.Fatal(err)
	}
	if driver.Clipboard() != value[row.start:row.end] {
		t.Fatal("artifact row selection copied different text")
	}
	if err := session.Screenshot(filepath.Join(artifacts, "rendered-line-edge-selection-740.png")); err != nil {
		t.Fatal(err)
	}
	for _, problem := range headless.CheckLayout(session.Snapshot()) {
		t.Errorf("line-edge screenshot layout=%+v", problem)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	want := append([]byte{0xef, 0xbb, 0xbf}, []byte("**"+value[:row.start]+"新"+value[row.end:]+"**\r\n\r\nTail\r\n<!--keep-->\r\n")...)
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, want) {
		t.Fatalf("artifact replacement changed wrong bytes: %q, %v", saved, err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, original) {
		t.Fatalf("artifact Undo/Save did not restore the original: %q, %v", saved, err)
	}
}

func TestRenderedGraphemeReflowKeepsVisualRowsAligned(t *testing.T) {
	value := strings.Repeat("段👩‍💻e\u0301文", 30)
	state, driver, canvas := affinityDriver(t, value+"\n\nTail\n")
	for _, width := range []int{740, 1200} {
		if err := driver.Resize(width, 780); err != nil {
			t.Fatal(err)
		}
		rows := affinityPaintedRows(t, state, canvas, value)
		row := rows[len(rows)/2]
		if gfx.GraphemeStart(value, row.start) != row.start || row.end < len(value) && gfx.GraphemeStart(value, row.end) != row.end {
			t.Fatalf("%dpx painted row %+v split a grapheme", width, row)
		}
		position := utf8.RuneCountInString(value[:gfx.GraphemeStart(value, row.start+(row.end-row.start)/2)])
		if err := driver.SelectText(position, position); err != nil {
			t.Fatal(err)
		}
		if err := driver.Key(gfx.KeyEnd, 0); err != nil {
			t.Fatal(err)
		}
		caret, ok := state.rich.CaretBounds()
		if got := byteOffsetOfRune(value, state.rich.input.Caret()); got != row.end || !ok || caret.Y != row.y {
			t.Errorf("%dpx End caret byte=%d rect=%+v ok=%v, want byte=%d y=%v", width, got, caret, ok, row.end, row.y)
		}
		if err := driver.Key(gfx.KeyHome, gfx.ModifierShift); err != nil {
			t.Fatal(err)
		}
		selection := state.rich.input.Selection()
		if selection.Start != utf8.RuneCountInString(value[:row.start]) || selection.End != utf8.RuneCountInString(value[:row.end]) {
			t.Errorf("%dpx reflowed Shift+Home selected %+v, want row %+v", width, selection, row)
		}
		if _, err := driver.ShortcutChord(primaryModifier(), 'c'); err != nil {
			t.Fatal(err)
		}
		if got := driver.Clipboard(); got != value[row.start:row.end] {
			t.Errorf("%dpx reflowed row Copy=%q, want %q", width, got, value[row.start:row.end])
		}
		if err := driver.Key(gfx.KeyLeft, gfx.ModifierMeta); err != nil {
			t.Fatal(err)
		}
		if got := byteOffsetOfRune(value, state.rich.input.Caret()); got != row.start {
			t.Errorf("%dpx reflowed Command+Left caret byte=%d, want %d", width, got, row.start)
		}
	}
	if state.session.Current().Text() != value+"\n\nTail\n" {
		t.Fatal("reflowed visual-row navigation changed source")
	}
}
