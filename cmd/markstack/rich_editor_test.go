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
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

func TestRenderedItalicCaretUsesStyledFontAdvance(t *testing.T) {
	engine := app.PlatformTextEngine()
	if _, ok := engine.(gfx.StyledTextEngine); !ok {
		t.Skip("styled platform text engine unavailable")
	}
	const prefix, emphasis = "before ", "Italic affine"
	var canvas gfx.Canvas
	canvas.SetTextEngine(engine)
	input := text.NewEditor(prefix + emphasis + " after")
	input.MoveTo(len([]rune(prefix+emphasis)), false)
	rich := renderedEditor{
		mixed: true, wrapCanvas: &canvas, wrapSize: 20, wrapX: 10, wrapY: 20,
		inlineSpans: []markdown.Span{{Text: prefix}, {Text: emphasis, Style: markdown.Italic}, {Text: " after"}},
		input:       input,
	}
	caret, ok := rich.CaretBounds()
	want := float32(10) + canvas.TextWidthWeight(prefix, 20, gfx.WeightRegular) + canvas.TextWidthStyle(emphasis, gfx.TextStyle{Size: 20, Weight: gfx.WeightRegular, Italic: true})
	if !ok || caret.X < want-0.25 || caret.X > want+0.25 {
		t.Fatalf("italic caret x=%v, reader advance x=%v", caret.X, want)
	}
	if offset, hit := rich.OffsetAt(ui.Point{X: want, Y: 21}); !hit || offset != len(prefix+emphasis) {
		t.Fatalf("italic click offset=%d, hit=%v", offset, hit)
	}
}

func TestRenderedParagraphTypingAndSourceRoundTrip(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Title\n\nBody\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	if driver.Snapshot().Focused != richInputID {
		t.Fatal("rendered paragraph did not focus native input")
	}
	if err := driver.Type("中文"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\n\nBody中文\n" {
		t.Fatalf("rendered typing changed source to %q", got)
	}
	clickID(t, driver, editModeID)
	if state.session.Current().Text() != "# Title\n\nBody中文\n" || state.mode != editMode {
		t.Fatal("switching to source lost the rendered edit")
	}
	clickID(t, driver, renderedModeID)
	if !hasText(driver, "Body中文") {
		t.Fatal("returning to rendered mode lost visible content")
	}
}

func TestSourceModeButtonFocusesEditorForImmediateUndo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mode-undo.md")
	if err := os.WriteFile(path, []byte("# Heading\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, editModeID)
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Heading\n" || state.session.Current().Dirty() {
		t.Fatalf("source undo after mode button = %q dirty=%v focused=%v", got, state.session.Current().Dirty(), driver.Snapshot().Focused)
	}
}

func TestSourceModeShortcutFocusesEditorForImmediateUndo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shortcut-undo.md")
	if err := os.WriteFile(path, []byte("# Heading\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierAlt, '1'); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Heading\n" || state.session.Current().Dirty() {
		t.Fatalf("source undo after mode shortcut = %q dirty=%v focused=%v", got, state.session.Current().Dirty(), driver.Snapshot().Focused)
	}
}

func TestRenderedWrappedParagraphUsesAllVisualRows(t *testing.T) {
	state, driver := testDriver(t)
	paragraph := strings.Repeat("长段落包含多个汉字和自然换行位置，用于验证活动输入仍在排版画布内。", 5)
	state.session.Current().Replace("# Title\n\n" + paragraph + "\n\n## Tail\n")
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	if height := state.preview.blocks[1].height; height < 60 {
		t.Fatalf("fixture did not wrap: height=%v", height)
	}
	clickID(t, driver, renderedRows.At(1))
	if state.rich.rect.Height < state.preview.blocks[1].height-1 {
		t.Fatalf("active paragraph cropped to %v of %v pixels", state.rich.rect.Height, state.preview.blocks[1].height)
	}
	for _, line := range state.rich.wrapRows {
		width := state.rich.wrapCanvas.TextWidthWeight(state.rich.input.String()[line.Start:line.End], state.rich.wrapSize, state.rich.wrapWeight)
		if width > state.rich.rect.Width+0.5 {
			t.Fatalf("wrapped row width=%v exceeds input=%v: %q", width, state.rich.rect.Width, state.rich.input.String()[line.Start:line.End])
		}
	}
	textRows := map[float32]bool{}
	for _, label := range driver.Snapshot().Texts {
		if strings.Contains(paragraph, label.Value) && len(label.Value) > 3 {
			textRows[label.Bounds.Y] = true
		}
	}
	if len(textRows) < 2 {
		t.Fatalf("active paragraph paints %d visual rows", len(textRows))
	}
}

func TestRenderedWrappedParagraphClickPlacesCaretOnSecondRow(t *testing.T) {
	state, driver := testDriver(t)
	paragraph := strings.Repeat("这段正文需要通过原生换行定位光标。", 8)
	state.session.Current().Replace(paragraph + "\n")
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if len(state.rich.wrapRows) < 2 {
		t.Fatal("fixture did not wrap")
	}
	second := state.rich.wrapRows[1]
	if err := driver.Click(state.rich.wrapX+5, state.rich.wrapY+32); err != nil {
		t.Fatal(err)
	}
	caret := byteOffsetOfRune(state.rich.input.String(), state.rich.input.Caret())
	if caret < second.Start || caret > second.End {
		t.Fatalf("second row click caret=%d row=%+v", caret, second)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != paragraph[:caret]+"新"+paragraph[caret:]+"\n" {
		t.Fatalf("wrapped click edit changed wrong source range: %q", got)
	}
}

func TestRenderedWrappedParagraphArrowMovesWithinVisualRows(t *testing.T) {
	state, driver := testDriver(t)
	paragraph := strings.Repeat("一个很长的段落需要按视觉行移动光标。", 8)
	state.session.Current().Replace(paragraph + "\n\nTail\n")
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if len(state.rich.wrapRows) < 2 {
		t.Fatal("fixture did not wrap")
	}
	state.rich.input.MoveTo(0, false)
	if err := driver.Key(gfx.KeyDown, 0); err != nil {
		t.Fatal(err)
	}
	if !state.rich.active || state.rich.index != 0 || byteOffsetOfRune(state.rich.input.String(), state.rich.input.Caret()) < state.rich.wrapRows[1].Start {
		t.Fatalf("Down left wrapped paragraph: active=%v index=%d caret=%d", state.rich.active, state.rich.index, state.rich.input.Caret())
	}
	if err := driver.Key(gfx.KeyUp, 0); err != nil {
		t.Fatal(err)
	}
	if state.rich.index != 0 || state.rich.input.Caret() != 0 {
		t.Fatalf("Up did not return to first row: index=%d caret=%d", state.rich.index, state.rich.input.Caret())
	}
}

func TestRenderedLongFixtureRowsStayWithinInputBounds(t *testing.T) {
	source, err := os.ReadFile("../../examples/Rendered-Long.md")
	if err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.session.Current().Replace(string(source))
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	if err := driver.Click(270, 218); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	for _, line := range state.rich.wrapRows {
		width := state.rich.wrapCanvas.TextWidthWeight(state.rich.input.String()[line.Start:line.End], state.rich.wrapSize, state.rich.wrapWeight)
		if width > state.rich.rect.Width+0.5 {
			t.Fatalf("long fixture row width=%v exceeds input=%v: %q", width, state.rich.rect.Width, state.rich.input.String()[line.Start:line.End])
		}
	}
}

func TestNativeWrappedLongFixtureRowsFitTheirInput(t *testing.T) {
	engine := app.PlatformTextEngine()
	if engine == nil {
		t.Skip("platform text engine unavailable")
	}
	source, err := os.ReadFile("../../examples/Rendered-Long.md")
	if err != nil {
		t.Fatal(err)
	}
	state := newModel()
	state.session.Current().Replace(string(source))
	var canvas gfx.Canvas
	canvas.SetTextEngine(engine)
	driver, err := app.NewDriver(newConfig(state), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	if err := driver.Click(330, 190); err != nil {
		t.Fatal(err)
	}
	if err := driver.Click(270, 218); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if !state.rich.wrapped {
		t.Fatal("native fixture did not wrap")
	}
	for _, line := range state.rich.wrapRows {
		width := canvas.TextWidthWeight(state.rich.input.String()[line.Start:line.End], state.rich.wrapSize, state.rich.wrapWeight)
		if width > state.rich.rect.Width+0.5 {
			t.Fatalf("native row width=%v exceeds input=%v: %q", width, state.rich.rect.Width, state.rich.input.String()[line.Start:line.End])
		}
	}
}

func TestRenderedLongParagraphScrollsToFocusedCaret(t *testing.T) {
	state, driver := testDriver(t)
	paragraph := strings.Repeat("一段连续输入会产生很多行，光标必须仍然可见。", 35)
	state.session.Current().Replace(paragraph + "\n")
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	state.rich.input.MoveTo(state.rich.input.RuneCount(), false)
	if err := driver.Resize(741, 520); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := driver.Pump(); err != nil {
			t.Fatal(err)
		}
	}
	caret, ok := state.rich.CaretBounds()
	if !ok || state.previewScroll <= 0 || caret.Y < state.previewRect.Y || caret.Y+caret.Height > state.previewRect.Y+state.previewRect.Height {
		t.Fatalf("long caret not visible: scroll=%v caret=%+v viewport=%+v", state.previewScroll, caret, state.previewRect)
	}
}

func TestRenderedWrappedSelectionReplacesVisibleRangeAndUndoes(t *testing.T) {
	state, driver := testDriver(t)
	paragraph := strings.Repeat("多行选区需要维持源区间准确。", 9)
	source := paragraph + "\n\nTail\n"
	state.session.Current().Replace(source)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	state.rich.input.MoveTo(0, false)
	if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	selection := state.rich.input.Selection()
	if state.mode != renderedMode || state.selection.active || selection.Start != 0 || selection.End <= 0 {
		t.Fatalf("wrapped selection mode=%v range=%+v", state.mode, selection)
	}
	selectedBytes := byteOffsetOfRune(paragraph, selection.End)
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "新"+paragraph[selectedBytes:]+"\n\nTail\n" {
		t.Fatalf("wrapped replacement = %q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != source {
		t.Fatalf("wrapped undo = %q", got)
	}
}

func TestRenderedSingleBoldAndLinkLabelsRemainEditable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inline.md")
	if err := os.WriteFile(path, []byte("# Title\n\n**bold**\n\n[label](https://example.com)\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	if state.mode != renderedMode || !state.rich.active {
		t.Fatalf("single bold block fell back to source: %q", state.session.Current().Text())
	}
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\n\n**bold**\n\n[label](https://example.com)\n" {
		t.Fatalf("styled undo source=%q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedRows.At(2))
	if state.mode != renderedMode || !state.rich.active {
		t.Fatalf("single link block fell back to source: %q", state.session.Current().Text())
	}
	if err := driver.Type("Y"); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	want := []byte("# Title\n\n**boldX**\n\n[labelY](https://example.com)\n")
	if err != nil || !bytes.Equal(got, want) || state.mode != renderedMode {
		t.Fatalf("rendered inline edit = %q, mode=%v, err=%v", got, state.mode, err)
	}
}

func TestRenderedMixedInlinePlainTailEditsWithoutRemovingBold(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("before **bold** after\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if state.mode != renderedMode || !state.rich.active || state.rich.input.String() != "before bold after" {
		t.Fatalf("mixed block mode=%v source=%q", state.mode, state.session.Current().Text())
	}
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "before **bold** afterX\n" || state.mode != renderedMode {
		t.Fatalf("mixed tail edit mode=%v source=%q", state.mode, got)
	}
}

func TestRenderedMixedWrappedParagraphStaysEditable(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("before " + strings.Repeat("中", 44) + " **boldword** after *italicword* [link](https://example.invalid/stays) tail\n\nTail\n")
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	if state.preview.blocks[0].height <= 30 {
		t.Fatalf("fixture did not wrap: height=%v", state.preview.blocks[0].height)
	}
	clickID(t, driver, renderedRows.At(0))
	if state.mode != renderedMode || !state.rich.active {
		t.Fatalf("wrapped mixed paragraph fell back to Source: mode=%v message=%q", state.mode, state.message)
	}
}

func TestRenderedMixedWrappedSecondRowPatchesOnlyBoldLeaf(t *testing.T) {
	state, driver := testDriver(t)
	source := "before " + strings.Repeat("中", 44) + " **boldword** after *italicword* [link](https://example.invalid/stays) tail\n\nTail\n"
	state.session.Current().Replace(source)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if state.mode != renderedMode || !state.rich.active {
		t.Fatalf("mixed wrapped editor unavailable: mode=%v message=%q", state.mode, state.message)
	}
	offset := strings.Index(state.rich.input.String(), "boldword") + len("bo")
	caret, ok := state.preview.blocks[0].view.CaretAt(offset)
	if !ok || caret.Y < state.previewRect.Y+46+state.preview.blocks[0].top+26 {
		t.Fatalf("bold caret is not on a wrapped row: %+v ok=%v", caret, ok)
	}
	if err := driver.Click(caret.X+0.2, caret.Y+caret.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(source, "**boldword**", "**bo新ldword**", 1)
	if got := state.session.Current().Text(); got != want || state.mode != renderedMode {
		t.Fatalf("mixed wrapped patch=%q, mode=%v, want=%q", got, state.mode, want)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != source {
		t.Fatalf("mixed wrapped undo=%q", got)
	}
}

func TestRenderedMixedWrappedShiftDownSelectsVisualRow(t *testing.T) {
	state, driver := testDriver(t)
	bold := strings.Repeat("boldword", 16)
	state.session.Current().Replace("prefix **" + bold + "** tail\n\nTail\n")
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	start := len("prefix ") + len("boldword")
	caret, ok := state.preview.blocks[0].view.CaretAt(start)
	if !ok {
		t.Fatal("bold caret unavailable")
	}
	if err := driver.Click(caret.X+0.2, caret.Y+caret.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if !state.rich.active || state.rich.input == nil {
		t.Fatalf("Shift+Down left mixed block: mode=%v cross=%v", state.mode, state.selection.active)
	}
	selection := state.rich.input.Selection()
	if state.mode != renderedMode || !state.rich.active || state.selection.active || selection.End <= selection.Start {
		t.Fatalf("mixed visual selection mode=%v active=%v cross=%v range=%+v", state.mode, state.rich.active, state.selection.active, selection)
	}
}

func TestRenderedMixedWrappedSelectionKeepsBoldWrapperAndUndo(t *testing.T) {
	state, driver := testDriver(t)
	bold := strings.Repeat("boldword", 16)
	source := "prefix **" + bold + "** tail\n\nTail\n"
	state.session.Current().Replace(source)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	visible := state.rich.input.String()
	start := len("prefix ") + len("boldword")
	caret, ok := state.preview.blocks[0].view.CaretAt(start)
	if !ok {
		t.Fatal("bold caret unavailable")
	}
	if err := driver.Click(caret.X+0.2, caret.Y+caret.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	selection := state.rich.input.Selection()
	from, to := byteOffsetOfRune(visible, selection.Start), byteOffsetOfRune(visible, selection.End)
	if from < len("prefix ") || to <= from || to > len("prefix ")+len(bold) {
		t.Fatalf("selection crossed the bold leaf: [%d,%d) in %q", from, to, visible)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	wantBold := bold[:from-len("prefix ")] + "新" + bold[to-len("prefix "):]
	want := "prefix **" + wantBold + "** tail\n\nTail\n"
	if got := state.session.Current().Text(); got != want || state.mode != renderedMode {
		t.Fatalf("wrapped bold selection = %q mode=%v, want=%q", got, state.mode, want)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != source {
		t.Fatalf("wrapped selection undo=%q", got)
	}
}

func TestRenderedMixedWrappedSavePreservesBOMCRLFAndNeighbors(t *testing.T) {
	bold := strings.Repeat("boldword", 16)
	source := "# Header\r\n\r\nprefix **" + bold + "** tail\r\n\r\n## Tail\r\n"
	path := filepath.Join(t.TempDir(), "wrapped-mixed.md")
	if err := os.WriteFile(path, append([]byte{0xef, 0xbb, 0xbf}, source...), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	caret, ok := state.preview.blocks[1].view.CaretAt(len("prefix bo"))
	if !ok {
		t.Fatal("bold caret unavailable")
	}
	if err := driver.Click(caret.X+0.2, caret.Y+caret.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	want := append([]byte{0xef, 0xbb, 0xbf}, strings.Replace(source, "**"+bold+"**", "**bo新"+bold[2:]+"**", 1)...)
	if err != nil || !bytes.Equal(data, want) {
		t.Fatalf("wrapped mixed save = %q, want %q, err=%v", data, want, err)
	}
}

func TestRenderedMixedWrappedCompositionCommitsOneLeaf(t *testing.T) {
	state, driver := testDriver(t)
	bold := strings.Repeat("boldword", 16)
	source := "prefix **" + bold + "** [link](https://example.invalid/stays)\n\nTail\n"
	state.session.Current().Replace(source)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	caret, ok := state.preview.blocks[0].view.CaretAt(len("prefix bo"))
	if !ok {
		t.Fatal("bold caret unavailable")
	}
	if err := driver.Click(caret.X+0.2, caret.Y+caret.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := driver.Compose("pin"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != source || state.mode != renderedMode {
		t.Fatalf("preedit changed source: %q mode=%v", got, state.mode)
	}
	if err := driver.Type("拼音"); err != nil {
		t.Fatal(err)
	}
	want := "prefix **bo拼音" + bold[2:] + "** [link](https://example.invalid/stays)\n\nTail\n"
	if got := state.session.Current().Text(); got != want || state.mode != renderedMode {
		t.Fatalf("mixed IME commit=%q mode=%v, want=%q", got, state.mode, want)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != source {
		t.Fatalf("mixed IME undo=%q", got)
	}
}

func TestRenderedMixedWrappedCrossLeafReplacementRejectsWithoutDataLoss(t *testing.T) {
	state, driver := testDriver(t)
	source := "before " + strings.Repeat("中", 44) + " **boldword** after *italicword* [link](https://example.invalid/stays) tail\n\nTail\n"
	state.session.Current().Replace(source)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	visible := state.rich.input.String()
	from := utf8.RuneCountInString(visible[:strings.Index(visible, "boldword")])
	to := utf8.RuneCountInString(visible[:strings.Index(visible, "link")+len("li")])
	state.rich.input.MoveTo(from, false)
	state.rich.input.MoveTo(to, true)
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	if state.rich.input == nil {
		t.Fatalf("cross-leaf rejection lost active input: mode=%v message=%q", state.mode, state.message)
	}
	if got := state.session.Current().Text(); got != source || state.rich.input.String() != visible || state.mode != renderedMode || state.message == "" || !hasText(driver, state.message) {
		t.Fatalf("cross-leaf rejection mode=%v source=%q input=%q message=%q", state.mode, got, state.rich.input.String(), state.message)
	}
	state.rich.input.MoveTo(from+2, false)
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); !strings.Contains(got, "**bo新ldword**") || !strings.Contains(got, "[link](https://example.invalid/stays)") || state.message != "" {
		t.Fatalf("valid edit after rejection = %q, message=%q", got, state.message)
	}
}

func TestRenderedMixedWrappedCrossLeafCompositionDoesNotStart(t *testing.T) {
	state, driver := testDriver(t)
	source := "before **boldword** after *italicword* [link](https://example.invalid/stays)\n"
	state.session.Current().Replace(source)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	visible := state.rich.input.String()
	start := utf8.RuneCountInString(visible[:strings.Index(visible, "boldword")])
	end := utf8.RuneCountInString(visible[:strings.Index(visible, "link")+len("li")])
	state.rich.input.MoveTo(start, false)
	state.rich.input.MoveTo(end, true)
	if err := driver.Compose("pin"); err != nil {
		t.Fatal(err)
	}
	if _, composing := state.rich.input.Composition(); composing || state.rich.input.String() != visible || state.session.Current().Text() != source || state.message == "" {
		t.Fatalf("cross-leaf preedit source=%q visible=%q composing=%v message=%q", state.session.Current().Text(), state.rich.input.String(), composing, state.message)
	}
}

func TestRenderedMixedWrappedCrossStyleEditPreservesLinkAndHistory(t *testing.T) {
	state, driver := testDriver(t)
	prefix := "before " + strings.Repeat("中", 44) + " "
	source := prefix + "**boldword** after *italicword* [link](https://example.invalid/stays) tail\n\nTail\n"
	state.session.Current().Replace(source)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	visible := state.rich.input.String()
	from := utf8.RuneCountInString(visible[:strings.Index(visible, "boldword")+len("bo")])
	to := utf8.RuneCountInString(visible[:strings.Index(visible, "italicword")+len("italic")])
	state.rich.input.MoveTo(from, false)
	state.rich.input.MoveTo(to, true)
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	want := prefix + "**bo**X*word* [link](https://example.invalid/stays) tail\n\nTail\n"
	if got := state.session.Current().Text(); got != want || state.mode != renderedMode || !state.rich.active {
		t.Fatalf("cross-style edit=%q mode=%v active=%v, want=%q", got, state.mode, state.rich.active, want)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != source || state.mode != renderedMode {
		t.Fatalf("cross-style undo=%q mode=%v", got, state.mode)
	}
	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != want || state.mode != renderedMode {
		t.Fatalf("cross-style redo=%q mode=%v", got, state.mode)
	}
	state.resetRenderedInputHistory()
	if len(state.rich.crossUndo) != 0 || len(state.rich.crossRedo) != 0 || state.rich.appliedEdits != 0 || state.rich.undoneEdits != 0 {
		t.Fatalf("format boundary retained cross history: undo=%d redo=%d applied=%d undone=%d", len(state.rich.crossUndo), len(state.rich.crossRedo), state.rich.appliedEdits, state.rich.undoneEdits)
	}
}

func TestRenderedSuccessiveCrossStyleEditsUndoAndRedoInOrder(t *testing.T) {
	state, driver := testDriver(t)
	source := "before **bold** after *italicword* [link](https://example.invalid/stays) tail\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	selectVisible := func(start, end int) {
		state.rich.input.MoveTo(utf8.RuneCountInString(state.rich.input.String()[:start]), false)
		state.rich.input.MoveTo(utf8.RuneCountInString(state.rich.input.String()[:end]), true)
	}
	value := state.rich.input.String()
	selectVisible(strings.Index(value, "bold"), strings.Index(value, "italicword")+len("italic"))
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	first := "before X*word* [link](https://example.invalid/stays) tail\n"
	if got := state.session.Current().Text(); got != first {
		t.Fatalf("first cross edit=%q", got)
	}
	value = state.rich.input.String()
	selectVisible(strings.Index(value, "X"), strings.Index(value, "word")+2)
	if err := driver.Type("Y"); err != nil {
		t.Fatal(err)
	}
	second := "before Y*rd* [link](https://example.invalid/stays) tail\n"
	if got := state.session.Current().Text(); got != second {
		t.Fatalf("second cross edit=%q", got)
	}
	for _, want := range []string{first, source} {
		if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
			t.Fatal(err)
		}
		if got := state.session.Current().Text(); got != want {
			t.Fatalf("cross undo=%q, want=%q", got, want)
		}
	}
	for _, want := range []string{first, second} {
		if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
			t.Fatal(err)
		}
		if got := state.session.Current().Text(); got != want {
			t.Fatalf("cross redo=%q, want=%q", got, want)
		}
	}
}

func TestRenderedCrossStyleThenSingleLeafHistoryStaysOrdered(t *testing.T) {
	state, driver := testDriver(t)
	source := "before **bold** after *italicword* [link](https://example.invalid/stays) tail\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	value := state.rich.input.String()
	from := utf8.RuneCountInString(value[:strings.Index(value, "bold")])
	to := utf8.RuneCountInString(value[:strings.Index(value, "italicword")+len("italic")])
	state.rich.input.MoveTo(from, false)
	state.rich.input.MoveTo(to, true)
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	first := "before X*word* [link](https://example.invalid/stays) tail\n"
	state.rich.input.MoveTo(state.rich.input.RuneCount(), false)
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	second := "before X*word* [link](https://example.invalid/stays) tail新\n"
	if got := state.session.Current().Text(); got != second {
		t.Fatalf("mixed history=%q, want=%q", got, second)
	}
	for _, want := range []string{first, source} {
		if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
			t.Fatal(err)
		}
		if got := state.session.Current().Text(); got != want {
			t.Fatalf("mixed undo=%q, want=%q", got, want)
		}
	}
	for _, want := range []string{first, second} {
		if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
			t.Fatal(err)
		}
		if got := state.session.Current().Text(); got != want {
			t.Fatalf("mixed redo=%q, want=%q", got, want)
		}
	}
}

func TestRenderedCrossStyleCompositionCommitsWithoutChangingLink(t *testing.T) {
	state, driver := testDriver(t)
	source := "before **bold** after *italicword* [link](https://example.invalid/stays) tail\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	value := state.rich.input.String()
	from := utf8.RuneCountInString(value[:strings.Index(value, "bold")+len("bo")])
	to := utf8.RuneCountInString(value[:strings.Index(value, "italicword")+len("italic")])
	state.rich.input.MoveTo(from, false)
	state.rich.input.MoveTo(to, true)
	if err := driver.Compose("pin"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != source {
		t.Fatalf("cross-style preedit changed source=%q", got)
	}
	if err := driver.Type("拼音"); err != nil {
		t.Fatal(err)
	}
	want := "before **bo**拼音*word* [link](https://example.invalid/stays) tail\n"
	if got := state.session.Current().Text(); got != want || state.mode != renderedMode {
		t.Fatalf("cross-style IME commit=%q mode=%v, want=%q", got, state.mode, want)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != source {
		t.Fatalf("cross-style IME undo=%q", got)
	}
}

func TestRenderedCrossStyleSavePreservesBOMCRLFAndOtherBlocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cross-style.md")
	source := "# Title\r\n\r\nbefore **bold** after *italicword* [link](https://example.invalid/stays) tail\r\n\r\n## Tail\r\n"
	if err := os.WriteFile(path, append([]byte{0xef, 0xbb, 0xbf}, source...), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	value := state.rich.input.String()
	from := utf8.RuneCountInString(value[:strings.Index(value, "bold")+len("bo")])
	to := utf8.RuneCountInString(value[:strings.Index(value, "italicword")+len("italic")])
	state.rich.input.MoveTo(from, false)
	state.rich.input.MoveTo(to, true)
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	want := "# Title\r\n\r\nbefore **bo**新*word* [link](https://example.invalid/stays) tail\r\n\r\n## Tail\r\n"
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, append([]byte{0xef, 0xbb, 0xbf}, want...)) || state.session.Current().Dirty() {
		t.Fatalf("cross-style save=%q dirty=%v err=%v", data, state.session.Current().Dirty(), err)
	}
}

func TestRenderedMixedInlineKeepsStyledSpansWhileFocused(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("before **bold** after\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	boldVisible := false
	for _, label := range driver.Snapshot().Texts {
		if label.Value == "bold" && label.Style.Weight >= gfx.WeightSemibold {
			boldVisible = true
		}
	}
	if !boldVisible {
		t.Fatal("active mixed paragraph lost its bold rendered span")
	}
	if state.rich.rect.Width == 0 || driver.Snapshot().Focused != richInputID {
		t.Fatal("styled text kept but native input lost focus")
	}
}

func TestRenderedMixedInlineClickPatchesOnlyBoldLeaf(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("before **bold** after\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	canvas := state.rich.wrapCanvas
	x := state.rich.wrapX + canvas.TextWidthWeight("before ", 16, gfx.WeightRegular) + canvas.TextWidthWeight("bo", 16, gfx.WeightSemibold)
	if err := driver.Click(x, state.rich.wrapY+8); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "before **bo新ld** after\n" || state.mode != renderedMode {
		t.Fatalf("mixed bold click mode=%v source=%q", state.mode, got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "before **bold** after\n" {
		t.Fatalf("mixed bold undo = %q", got)
	}
}

func TestRenderedMixedWholeSelectionCanReplaceStylesAndUndo(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("before **bold** after\n\nTail\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	state.rich.input.MoveTo(7, false)
	state.rich.input.MoveTo(state.rich.input.RuneCount(), true)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "before X\n\nTail\n" || state.mode != renderedMode {
		t.Fatalf("cross-style replacement mode=%v source=%q", state.mode, got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "before **bold** after\n\nTail\n" {
		t.Fatalf("cross-style undo=%q", got)
	}
}

func TestRenderedInvalidLinkLabelKeepsTypedSource(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("[label](https://example.com)\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	driver.SetClipboard("](/other)")
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "[label](/other)](https://example.com)\n" || state.mode != editMode {
		t.Fatalf("invalid link label mode=%v source=%q", state.mode, got)
	}
}

func TestRenderedStyledUndoRedoSharesSourceHistory(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("**bold**\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	for _, value := range []string{"A", "B"} {
		if err := driver.Type(value); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "**bold**\n" {
		t.Fatalf("styled coalesced undo = %q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "**boldAB**\n" {
		t.Fatalf("styled coalesced redo = %q", got)
	}
	clickID(t, driver, editModeID)
	clickID(t, driver, sourceID)
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "**boldA**\n" {
		t.Fatalf("styled source undo = %q", got)
	}
}

func TestRenderedBoldCompositionKeepsMarkupUntilCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bold-ime.md")
	if err := os.WriteFile(path, []byte("\xef\xbb\xbf**文字**\r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Compose("pin"); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Dirty() || state.session.Current().Text() != "**文字**\r\n" {
		t.Fatalf("bold preedit changed document: %q", state.session.Current().Text())
	}
	if err := driver.Type("拼音"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "**文字拼音**\r\n" {
		t.Fatalf("bold commit = %q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "**文字**\r\n" || state.session.Current().Dirty() {
		t.Fatalf("bold undo = %q dirty=%v", got, state.session.Current().Dirty())
	}
}

func TestRenderedSingleBoldKeepsActiveWeight(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("**bold**\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	snapshot := driver.Snapshot()
	for i, hit := range snapshot.Hits {
		if hit.ID == richInputID {
			text, _ := snapshot.HitTextAt(i)
			if text.Weight != gfx.WeightBold {
				t.Fatalf("active bold weight=%v", text.Weight)
			}
			return
		}
	}
	t.Fatal("active bold input is missing")
}

func TestRenderedBlockAccessibilityActivationFocusesInput(t *testing.T) {
	state := newModel()
	state.session.Current().Replace("# Title\n\nBody\n")
	state.mode = renderedMode
	var canvas gfx.Canvas
	driver, err := app.NewDriver(newConfig(state), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	canvas.BeginEventPass(renderedRows.At(1), gfx.ClickInfo{})
	state.paint(&canvas, ui.Size{Width: 1200, Height: 780})
	canvas.EndEventPass()
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := driver.Pump(); err != nil {
			t.Fatal(err)
		}
	}
	if !state.rich.active || state.rich.block.Visible != "Body" || driver.Snapshot().Focused != richInputID {
		t.Fatalf("accessibility activation: active=%v focused=%v", state.rich.active, driver.Snapshot().Focused)
	}
}

func TestRenderedHeadingListAndUndo(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		row  int
		want string
	}{
		{"heading", "# 标题\n\n段落\n", 0, "# 标题续\n\n段落\n"},
		{"list", "- first\n- second\n", 1, "- first\n- second续\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, driver := testDriver(t)
			state.session.Current().Replace(test.body)
			clickID(t, driver, renderedModeID)
			clickID(t, driver, renderedRows.At(test.row))
			if err := driver.Type("续"); err != nil {
				t.Fatal(err)
			}
			if got := state.session.Current().Text(); got != test.want {
				t.Fatalf("source = %q, want %q", got, test.want)
			}
			if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
				t.Fatal(err)
			}
			if got := state.session.Current().Text(); got != test.body {
				t.Fatalf("rendered undo = %q", got)
			}
		})
	}
}

func TestRenderedUndoDoesNotReappearInSourceHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.md")
	if err := os.WriteFile(path, []byte("# Title\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, editModeID)
	clickID(t, driver, sourceID)
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\n" {
		t.Fatalf("second undo resurrected an already undone edit: %q", got)
	}
}

func TestRenderedCoalescedUndoRedoSharesSourceHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "coalesced.md")
	if err := os.WriteFile(path, []byte("# Title\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	for _, value := range []string{"A", "B"} {
		if err := driver.Type(value); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\n" {
		t.Fatalf("coalesced undo = %q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# TitleAB\n" {
		t.Fatalf("coalesced redo = %q rich=%q applied=%d undone=%d canRedo=%v sourceCanRedo=%v message=%q", got, state.rich.input.String(), state.rich.appliedEdits, state.rich.undoneEdits, state.rich.input.CanRedo(), state.session.Current().Editor.CanRedo(), state.message)
	}
	clickID(t, driver, editModeID)
	clickID(t, driver, sourceID)
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# TitleA\n" {
		t.Fatalf("source history after rich redo = %q", got)
	}
}

func TestRenderedUndoDoesNotCrossEarlierSourceEdit(t *testing.T) {
	state, driver := testDriver(t)
	clickID(t, driver, sourceID)
	if err := driver.Type("# Title\n"); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\n" {
		t.Fatalf("undo reached an earlier source edit: %q", got)
	}
}

func TestRenderedUndoCanRevertPreviousSourceEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source-first.md")
	if err := os.WriteFile(path, []byte("# Title\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, sourceID)
	if err := driver.SelectText(7, 7); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\n" || state.session.Current().Dirty() {
		t.Fatalf("rendered undo missed prior source edit: %q dirty=%v", got, state.session.Current().Dirty())
	}
	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# TitleX\n" || !state.session.Current().Dirty() {
		t.Fatalf("rendered redo missed prior source edit: %q dirty=%v", got, state.session.Current().Dirty())
	}
}

func TestRenderedUndoFallsBackWhenPriorBlockStructureChanges(t *testing.T) {
	state, driver := testDriver(t)
	clickID(t, driver, sourceID)
	if err := driver.Type("# First\n\n# Second\n"); err != nil {
		t.Fatal(err)
	}
	if err := driver.SelectText(0, 9); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type(""); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Second\n" {
		t.Fatalf("setup removed wrong block: %q", got)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# First\n\n# Second\n" || state.mode != editMode {
		t.Fatalf("structural undo mode=%v source=%q", state.mode, got)
	}
}

func TestRenderedUnsupportedBlockFallsBackToSource(t *testing.T) {
	state, driver := testDriver(t)
	source := "# Heading\n\n```go\nfmt.Println(1)\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	if state.mode != editMode || state.session.Current().Text() != source {
		t.Fatalf("unsupported edit mode=%v source=%q", state.mode, state.session.Current().Text())
	}
	if !strings.Contains(state.message, "源码") {
		t.Fatalf("no explicit fallback message: %q", state.message)
	}
	if caret := state.session.Current().Editor.Caret(); caret != strings.Index(source, "fmt.Println") {
		t.Fatalf("source fallback caret = %d, want code line", caret)
	}
}

func TestRenderedSyntaxInputStaysInRenderedMode(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("Plain\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Type(" **bold**"); err != nil {
		t.Fatal(err)
	}
	if state.mode != renderedMode || state.session.Current().Text() != "Plain **bold**\n" || !state.rich.active || state.rich.input.String() != "Plain bold" {
		t.Fatalf("bold did not render: mode=%v text=%q", state.mode, state.session.Current().Text())
	}
}

func TestRenderedInputBoundsClearOnReadMode(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("Plain\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if state.rich.rect.Width == 0 {
		t.Fatal("rendered input has no bounds")
	}
	clickID(t, driver, readModeID)
	if state.rich.rect.Width != 0 {
		t.Fatalf("read mode retained invisible input bounds: %+v", state.rich.rect)
	}
}

func TestRenderedEditSavesOnlyTouchedBytes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sample.md")
	source := []byte("\xef\xbb\xbf# 标题\r\n\r\nBody\r\n\r\n<!-- raw -->\r\n")
	if err := os.WriteFile(path, source, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	if err := driver.Type(" plus"); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "\xef\xbb\xbf# 标题\r\n\r\nBody plus\r\n\r\n<!-- raw -->\r\n" {
		t.Fatalf("saved source = %q, %v", got, err)
	}
	if state.session.Current().Dirty() {
		t.Fatal("saved rendered edit remains dirty")
	}
}

func TestRenderedRealExampleCanEditFirstHeading(t *testing.T) {
	source, err := os.ReadFile("../../examples/Welcome.md")
	if err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.session.Current().Replace(string(source))
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if driver.Snapshot().Focused != richInputID {
		t.Fatalf("first heading in mixed document fell back to %v: %s", state.mode, state.message)
	}
	if err := driver.Type("笔记"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); !strings.HasPrefix(got, "# 墨栈笔记\n") || !strings.HasSuffix(got, "```\n") {
		t.Fatalf("mixed document changed outside heading: %q", got)
	}
}

func TestRenderedRealExampleEditsMultiwordPlainBlocks(t *testing.T) {
	source, err := os.ReadFile("../../examples/Welcome.md")
	if err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.session.Current().Replace(string(source))
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(3))
	if state.mode != renderedMode || driver.Snapshot().Focused != richInputID {
		t.Fatal("long ordinary paragraph fell back to source")
	}
	if err := driver.SelectText(state.rich.input.RuneCount(), state.rich.input.RuneCount()); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type(" 新"); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedRows.At(10))
	if state.mode != renderedMode || driver.Snapshot().Focused != richInputID {
		t.Fatal("multiword heading fell back to source")
	}
	if err := driver.Type("2026"); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(string(source), "分栏模式可以同时阅读排版结果。", "分栏模式可以同时阅读排版结果。 新", 1)
	want = strings.Replace(want, "## Markdown 示例", "## Markdown 示例2026", 1)
	if got := state.session.Current().Text(); got != want {
		t.Fatalf("ordinary blocks changed unrelated Markdown: %q", got)
	}
}

func TestRenderedActiveHeadingHasNoInputBorder(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Heading\n\nBody\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	primary := ui.LightTheme().Primary
	for _, rectangle := range driver.Snapshot().Primitives {
		if rectangle.Color == primary && rectangle.Bounds.Y >= state.rich.rect.Y-2 && rectangle.Bounds.Y <= state.rich.rect.Y+state.rich.rect.Height+2 {
			t.Fatalf("active text drew a prominent form field: %+v", rectangle)
		}
	}
}

func TestRenderedMixedATXHeadingKeepsStyleAndSourceHistory(t *testing.T) {
	state, driver := testDriver(t)
	source := "# Title续**粗** tail\n\nAfter\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if state.mode != renderedMode || !state.rich.active || state.rich.input.String() != "Title续粗 tail" || driver.Snapshot().Focused != richInputID {
		t.Fatalf("mixed heading mode=%v active=%v focus=%v", state.mode, state.rich.active, driver.Snapshot().Focused)
	}
	state.rich.input.MoveTo(utf8.RuneCountInString("Title续"), false)
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	want := "# Title续新**粗** tail\n\nAfter\n"
	if got := state.session.Current().Text(); got != want || state.mode != renderedMode {
		t.Fatalf("mixed heading edit=%q mode=%v", got, state.mode)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != source {
		t.Fatalf("mixed heading Undo=%q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != want {
		t.Fatalf("mixed heading Redo=%q", got)
	}
}

func TestRenderedHeadingFocusKeepsTextBoxAndRule(t *testing.T) {
	engine := app.PlatformTextEngine()
	if engine == nil {
		t.Skip("platform text engine unavailable")
	}
	for level := 1; level <= 6; level++ {
		t.Run(strings.Repeat("#", level), func(t *testing.T) {
			state := newModel()
			state.session.Current().Replace(strings.Repeat("#", level) + " HeadingValue\n\nTail\n")
			var canvas gfx.Canvas
			canvas.SetTextEngine(engine)
			driver, err := app.NewDriver(newConfig(state), &canvas)
			if err != nil {
				t.Fatal(err)
			}
			defer driver.Close()
			if err := driver.Resize(740, 520); err != nil {
				t.Fatal(err)
			}
			clickID(t, driver, renderedModeID)
			before := driver.Snapshot()
			var reading ui.Rect
			for _, label := range before.Texts {
				if label.Value == "HeadingValue" {
					reading = label.Bounds
				}
			}
			if reading.Height == 0 {
				t.Fatal("reader heading not painted")
			}
			var rules []ui.Rect
			for _, primitive := range before.Primitives {
				if primitive.Bounds.Height == 1 && primitive.Bounds.Width > 400 && primitive.Bounds.Y > reading.Y && primitive.Bounds.Y < reading.Y+reading.Height+10 {
					rules = append(rules, primitive.Bounds)
				}
			}
			if level <= 2 && len(rules) != 1 {
				t.Fatalf("reader heading rules = %d", len(rules))
			}
			clickID(t, driver, renderedRows.At(0))
			after := driver.Snapshot()
			var input ui.Rect
			for _, label := range after.Texts {
				if label.Value == "HeadingValue" && label.Style.Color.Alpha > 0 {
					input = label.Bounds
				}
			}
			if input.X != reading.X || input.Y != reading.Y || input.Height != reading.Height {
				t.Errorf("focus moved heading text box: read=%+v input=%+v", reading, input)
			}
			for _, rule := range rules {
				found := false
				for _, primitive := range after.Primitives {
					if primitive.Bounds == rule {
						found = true
					}
				}
				if !found {
					t.Errorf("focus removed heading rule at %+v", rule)
				}
			}
		})
	}
}

func TestRenderedHeadingPreeditReservesRowsWithoutTouchingSource(t *testing.T) {
	engine := app.PlatformTextEngine()
	if engine == nil {
		t.Skip("platform text engine unavailable")
	}
	path := filepath.Join(t.TempDir(), "Heading.md")
	original := []byte("\xef\xbb\xbf# Title\r\n\r\nTail\r\n")
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	state := newModel()
	state.openFile(path)
	var canvas gfx.Canvas
	canvas.SetTextEngine(engine)
	driver, err := app.NewDriver(newConfig(state), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	before := state.session.Current().Text()
	if err := driver.Compose(strings.Repeat("nihaoshijie", 16)); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != before || state.session.Current().Dirty() {
		t.Fatal("heading preedit changed committed source")
	}
	if len(state.rich.wrapRows) < 3 {
		t.Fatal("heading preedit did not wrap")
	}
	wantHeight := float32(len(state.rich.wrapRows)) * 43
	if state.rich.rect.Height != wantHeight {
		t.Errorf("heading preedit input height=%v, want %v", state.rich.rect.Height, wantHeight)
	}
	tailTop := state.previewRect.Y + 46 - state.previewScroll + state.preview.blocks[1].top
	if tailTop < state.rich.rect.Y+wantHeight+16 {
		t.Errorf("heading preedit overlaps Tail: input=%+v rows=%d tail=%v", state.rich.rect, len(state.rich.wrapRows), tailTop)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, original) {
		t.Fatalf("heading preedit reached disk: %q, %v", saved, err)
	}
	if err := driver.Type("你"); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, original) || state.session.Current().Dirty() {
		t.Fatalf("heading commit/Undo/Save changed original bytes: %q, %v", saved, err)
	}
}

func TestRenderedPlainBlocksReservePreeditRows(t *testing.T) {
	engine := app.PlatformTextEngine()
	if engine == nil {
		t.Skip("platform text engine unavailable")
	}
	for _, prefix := range []string{"", "- ", "> "} {
		t.Run(prefix, func(t *testing.T) {
			state := newModel()
			source := prefix + "甲乙\n\nTail\n"
			state.session.Current().Replace(source)
			var canvas gfx.Canvas
			canvas.SetTextEngine(engine)
			driver, err := app.NewDriver(newConfig(state), &canvas)
			if err != nil {
				t.Fatal(err)
			}
			defer driver.Close()
			if err := driver.Resize(740, 520); err != nil {
				t.Fatal(err)
			}
			clickID(t, driver, renderedModeID)
			var readingColor ui.Color
			var readingMarker gfx.Text
			for _, label := range driver.Snapshot().Texts {
				if label.Value == "甲乙" {
					readingColor = label.Style.Color
				}
				if label.Value == "•" {
					readingMarker = label
				}
			}
			if readingColor.Alpha == 0 {
				t.Fatal("reader text color missing")
			}
			clickID(t, driver, renderedRows.At(0))
			if !state.rich.active {
				t.Fatal("plain block did not activate")
			}
			if err := driver.Compose(strings.Repeat("文", 80)); err != nil {
				t.Fatal(err)
			}
			if len(state.rich.wrapRows) < 3 || state.session.Current().Text() != source {
				t.Fatalf("plain preedit rows=%d source=%q", len(state.rich.wrapRows), state.session.Current().Text())
			}
			wantHeight := float32(len(state.rich.wrapRows)) * 26
			if state.rich.rect.Height != wantHeight {
				t.Errorf("plain preedit input height=%v, want %v", state.rich.rect.Height, wantHeight)
			}
			tailTop := state.previewRect.Y + 46 - state.previewScroll + state.preview.blocks[1].top
			if tailTop < state.rich.rect.Y+wantHeight+16 {
				t.Errorf("plain preedit overlaps Tail: input=%+v tail=%v", state.rich.rect, tailTop)
			}
			for _, label := range driver.Snapshot().Texts {
				if strings.HasPrefix(label.Value, "甲乙") && label.Style.Color.Alpha > 0 && label.Style.Color != readingColor {
					t.Errorf("preedit changed reader text color: read=%+v edit=%+v", readingColor, label.Style.Color)
				}
				if label.Value == "•" && (label.Bounds.Y != readingMarker.Bounds.Y || label.Bounds.Height != readingMarker.Bounds.Height || label.Style.Color != readingMarker.Style.Color) {
					t.Errorf("preedit moved or recolored its list marker: read=%+v edit=%+v", readingMarker, label)
				}
			}
		})
	}
}

func TestRenderedNativeSurfaceStaysStableWhileCompositionWraps(t *testing.T) {
	engine := app.PlatformTextEngine()
	if engine == nil {
		t.Skip("platform text engine unavailable")
	}
	for _, virtual := range []bool{false, true} {
		name := "heading"
		if virtual {
			name = "new-paragraph"
		}
		t.Run(name, func(t *testing.T) {
			state := newModel()
			source := "# 甲乙丙丁戊己庚辛壬癸子丑\n\nTail\n"
			if virtual {
				source = "# Title\n\nBody\n\nTail\n"
			}
			state.session.Current().Replace(source)
			var canvas gfx.Canvas
			canvas.SetTextEngine(engine)
			driver, err := app.NewDriver(newConfig(state), &canvas)
			if err != nil {
				t.Fatal(err)
			}
			defer driver.Close()
			if err := driver.Resize(740, 520); err != nil {
				t.Fatal(err)
			}
			clickID(t, driver, renderedModeID)
			index := 0
			if virtual {
				index = 1
			}
			clickID(t, driver, renderedRows.At(index))
			if virtual {
				if err := driver.Key(gfx.KeyEnd, 0); err != nil {
					t.Fatal(err)
				}
				if err := driver.Key(gfx.KeyEnter, 0); err != nil {
					t.Fatal(err)
				}
			}
			hit := func() (gfx.Hit, gfx.HitText) {
				t.Helper()
				snapshot := driver.Snapshot()
				for i, item := range snapshot.Hits {
					if item.ID == richInputID {
						text, ok := snapshot.HitTextAt(i)
						if !ok {
							t.Fatal("rendered native input has no text geometry")
						}
						return item, text
					}
				}
				t.Fatal("rendered native input missing")
				return gfx.Hit{}, gfx.HitText{}
			}
			before, beforeText := hit()
			beforeHeight := state.rich.rect.Height
			beforeSource := state.session.Current().Text()
			if beforeText.Color.Alpha != 0 || !before.HideNativeOverlays {
				t.Errorf("initial native surface still owns visible glyphs/overlays: %+v", before)
			}
			if _, ok := beforeText.Placement.CaretBounds(); !ok {
				t.Error("initial native surface has no application caret placement")
			}
			if err := driver.Compose(strings.Repeat("nihao", 20)); err != nil {
				t.Fatal(err)
			}
			after, afterText := hit()
			if state.rich.rect.Height <= beforeHeight || state.session.Current().Text() != beforeSource {
				t.Fatalf("composition did not reflow without source mutation: rect=%+v source=%q", state.rich.rect, state.session.Current().Text())
			}
			if beforeText.Bounds != afterText.Bounds || beforeText.Color != afterText.Color || before.HideNativeOverlays != after.HideNativeOverlays {
				t.Errorf("composition changed native binding presentation: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestRenderedPlainCaretUsesVisualRTLBoundary(t *testing.T) {
	engine := app.PlatformTextEngine()
	if engine == nil {
		t.Skip("platform text engine unavailable")
	}
	state := newModel()
	const value = "אבגד"
	state.session.Current().Replace(value + "\n")
	var canvas gfx.Canvas
	canvas.SetTextEngine(engine)
	driver, err := app.NewDriver(newConfig(state), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.SelectText(0, 0); err != nil {
		t.Fatal(err)
	}
	caret, ok := state.rich.CaretBounds()
	want := state.rich.rect.X + canvas.TextWidthWeight(value, 16, gfx.WeightRegular)
	if !ok || math.Abs(float64(caret.X-want)) > 0.1 {
		t.Fatalf("RTL logical start caret=%+v visible=%v, want visual right edge %v", caret, ok, want)
	}
}

func TestRenderedVirtualPreeditCannotBeDiscardedByBlockActivation(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Title\n\nBody\n\nTail\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	if err := driver.Key(gfx.KeyEnd, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Compose("pin"); err != nil {
		t.Fatal(err)
	}
	before := state.session.Current().Text()
	state.activateRendered(2)
	_, composing := state.rich.input.Composition()
	if !state.rich.virtual || state.rich.input.String() != "pin" || !composing || state.session.Current().Text() != before {
		t.Fatalf("block activation discarded pending virtual input: virtual=%v value=%q composing=%v source=%q", state.rich.virtual, state.rich.input.String(), composing, state.session.Current().Text())
	}
}

func TestRenderedVirtualPreeditKeepsFollowingContentAndSourceIdentity(t *testing.T) {
	engine := app.PlatformTextEngine()
	if engine == nil {
		t.Skip("platform text engine unavailable")
	}
	for _, test := range []struct {
		name      string
		following string
		label     string
	}{
		{"paragraph", "Untouched", "Untouched"},
		{"heading", "## Untouched", "Untouched"},
		{"code", "```go\nuntouched()\n```", "untouched()"},
		{"table", "| Untouched |\n| --- |\n| Cell |", "Untouched"},
		{"end", "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "# Title\n\nBody\n"
			want := "# Title\n\nBody\n\n新\n"
			if test.following != "" {
				source += "\n" + test.following + "\n"
				want += "\n" + test.following + "\n"
			}
			path := filepath.Join(t.TempDir(), "Virtual.md")
			original := []byte("\ufeff" + strings.ReplaceAll(source, "\n", "\r\n"))
			if err := os.WriteFile(path, original, 0644); err != nil {
				t.Fatal(err)
			}
			state := newModel()
			state.openFile(path)
			var canvas gfx.Canvas
			canvas.SetTextEngine(engine)
			driver, err := app.NewDriver(newConfig(state), &canvas)
			if err != nil {
				t.Fatal(err)
			}
			defer driver.Close()
			if err := driver.Resize(740, 520); err != nil {
				t.Fatal(err)
			}
			clickID(t, driver, renderedModeID)
			var followingStyle gfx.TextStyle
			var followingY float32
			var bodyBounds ui.Rect
			for _, label := range driver.Snapshot().Texts {
				if label.Value == test.label {
					followingStyle = label.Style
					followingY = label.Bounds.Y
				}
				if label.Value == "Body" {
					bodyBounds = label.Bounds
				}
			}
			clickID(t, driver, renderedRows.At(1))
			if err := driver.Key(gfx.KeyEnd, 0); err != nil {
				t.Fatal(err)
			}
			if err := driver.Key(gfx.KeyEnter, 0); err != nil {
				t.Fatal(err)
			}
			if !state.rich.virtual {
				t.Fatal("Enter did not create a virtual paragraph")
			}
			slotY := state.rich.rect.Y
			before := state.session.Current().Text()
			if err := driver.Compose(strings.Repeat("文", 80)); err != nil {
				t.Fatal(err)
			}
			if state.session.Current().Text() != before || state.rich.wrapSize != 16 || !state.rich.wrapped || state.rich.rect.Height < 78 {
				t.Errorf("virtual preedit inherited following block geometry: rect=%+v size=%v wrapped=%v source=%q", state.rich.rect, state.rich.wrapSize, state.rich.wrapped, state.session.Current().Text())
			}
			if test.label != "" {
				gap := followingY - bodyBounds.Y - bodyBounds.Height
				wantY := state.rich.rect.Y + state.rich.rect.Height + gap
				found := false
				for _, label := range driver.Snapshot().Texts {
					if label.Value != test.label {
						continue
					}
					found = true
					if label.Style != followingStyle || math.Abs(float64(label.Bounds.Y-wantY)) > 0.1 {
						t.Errorf("virtual preedit changed following content: %+v", label)
					}
				}
				if !found {
					t.Error("virtual preedit hid following content")
				}
			}
			if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
				t.Fatal(err)
			}
			if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, original) {
				t.Fatalf("virtual preedit reached disk: %q, %v", saved, err)
			}
			if err := driver.Type("新"); err != nil {
				t.Fatal(err)
			}
			if state.session.Current().Text() != strings.ReplaceAll(want, "\n", "\r\n") {
				t.Fatalf("virtual commit changed following source: %q", state.session.Current().Text())
			}
			staticCopies := 0
			for _, label := range driver.Snapshot().Texts {
				if label.Value == "新" && label.Style.Color.Alpha > 0 {
					staticCopies++
					if label.Bounds.Y != slotY || label.Bounds.Height != 26 || label.Style.Size != 16 {
						t.Errorf("virtual commit moved its visible text: %+v", label)
					}
				}
			}
			nativeFound := false
			snapshot := driver.Snapshot()
			for i, hit := range snapshot.Hits {
				text, _ := snapshot.HitTextAt(i)
				if hit.ID == richInputID && hit.Value == "新" && text.Bounds == state.previewRect && text.Size == 16 && text.Color.Alpha == 0 && hit.HideNativeOverlays {
					nativeFound = true
				}
			}
			if staticCopies != 1 || !nativeFound || strings.Contains(state.message, "finish text composition") {
				t.Errorf("virtual commit lost its native surface or left duplicate text/error: static=%d native=%v message=%q", staticCopies, nativeFound, state.message)
			}
			if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
				t.Fatal(err)
			}
			if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, []byte("\ufeff"+strings.ReplaceAll(want, "\n", "\r\n"))) {
				t.Fatalf("virtual Save changed BOM/CRLF: %q, %v", saved, err)
			}
			for range 2 {
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
				t.Fatal(err)
			}
			if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, original) {
				t.Fatalf("virtual Undo/Save did not restore original: %q, %v", saved, err)
			}
		})
	}
}

func TestRenderedWrappedHeadingPaintsOnlyMarkedCompositionUnderlines(t *testing.T) {
	engine := app.PlatformTextEngine()
	if engine == nil {
		t.Skip("platform text engine unavailable")
	}
	state := newModel()
	source := "# 甲乙\n\nTail\n"
	state.session.Current().Replace(source)
	var canvas gfx.Canvas
	canvas.SetTextEngine(engine)
	driver, err := app.NewDriver(newConfig(state), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Compose(strings.Repeat("文", 20)); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != source || len(state.rich.wrapRows) != 2 {
		t.Fatalf("unexpected heading composition: source=%q rows=%d", state.session.Current().Text(), len(state.rich.wrapRows))
	}
	want := []ui.Rect{
		{X: state.rich.rect.X + canvas.TextWidthWeight("甲乙", 36, gfx.WeightBold), Y: state.rich.rect.Y + 41, Width: canvas.TextWidthWeight(strings.Repeat("文", 12), 36, gfx.WeightBold), Height: 1},
		{X: state.rich.rect.X, Y: state.rich.rect.Y + 84, Width: canvas.TextWidthWeight(strings.Repeat("文", 8), 36, gfx.WeightBold), Height: 1},
	}
	near := func(first, second ui.Rect) bool {
		return math.Abs(float64(first.X-second.X)) < 0.01 && math.Abs(float64(first.Y-second.Y)) < 0.01 &&
			math.Abs(float64(first.Width-second.Width)) < 0.01 && first.Height == second.Height
	}
	for _, underline := range want {
		found := false
		for _, primitive := range driver.Snapshot().Primitives {
			if near(primitive.Bounds, underline) {
				found = true
			}
		}
		if !found {
			t.Errorf("missing marked-only underline at %+v", underline)
		}
	}
	if err := driver.Type("好"); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != "# 甲乙好\n\nTail\n" {
		t.Fatalf("heading composition commit changed unexpected bytes: %q", state.session.Current().Text())
	}
	for _, primitive := range driver.Snapshot().Primitives {
		for _, underline := range want {
			if near(primitive.Bounds, underline) {
				t.Errorf("composition underline survived commit: %+v", underline)
			}
		}
	}
}

func TestRenderedWrappedHeadingsKeepReaderLineGeometry(t *testing.T) {
	engine := app.PlatformTextEngine()
	if engine == nil {
		t.Skip("platform text engine unavailable")
	}
	const title = "Alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima mike november oscar papa quebec romeo sierra tango uniform victor whiskey xray yankee zulu"
	for level := 1; level <= 6; level++ {
		t.Run(strings.Repeat("#", level), func(t *testing.T) {
			state := newModel()
			prefix := strings.Repeat("#", level) + " "
			source := prefix + title + "\n\nTail\n"
			state.session.Current().Replace(source)
			var canvas gfx.Canvas
			canvas.SetTextEngine(engine)
			driver, err := app.NewDriver(newConfig(state), &canvas)
			if err != nil {
				t.Fatal(err)
			}
			defer driver.Close()
			if err := driver.Resize(740, 520); err != nil {
				t.Fatal(err)
			}
			clickID(t, driver, renderedModeID)
			rows := func() []gfx.Text {
				var result []gfx.Text
				for _, label := range driver.Snapshot().Texts {
					value := strings.TrimSpace(label.Value)
					if value != "" && strings.Contains(title, value) && label.Style.Color.Alpha > 0 {
						result = append(result, label)
					}
				}
				return result
			}
			reading := rows()
			if len(reading) < 3 {
				t.Fatalf("heading needs three visible reader rows, got %+v", reading)
			}
			clickID(t, driver, renderedRows.At(0))
			editing := rows()
			if len(editing) != len(reading) {
				t.Fatalf("focus changed wrapped row count: reading=%d editing=%d", len(reading), len(editing))
			}
			for index, row := range editing {
				if row.Bounds.Y < reading[index].Bounds.Y-0.1 || row.Bounds.Y > reading[index].Bounds.Y+0.1 {
					t.Errorf("row %d moved on focus: read=%+v edit=%+v", index, reading[index].Bounds, row.Bounds)
				}
			}
			if caret, ok := state.rich.CaretBounds(); !ok || caret.Height > reading[1].Bounds.Y-reading[0].Bounds.Y+0.1 {
				t.Errorf("heading caret exceeds its reader line: %+v, visible=%v", caret, ok)
			}
			target := reading[2]
			offset := strings.Index(title, strings.TrimSpace(target.Value))
			if offset < 0 {
				t.Fatal("reader row has no source identity")
			}
			if err := driver.Click(target.Bounds.X+2, target.Bounds.Y+8); err != nil {
				t.Fatal(err)
			}
			if err := driver.Type("X"); err != nil {
				t.Fatal(err)
			}
			want := prefix + title[:offset] + "X" + title[offset:] + "\n\nTail\n"
			if got := state.session.Current().Text(); got != want {
				t.Errorf("third-row click patched wrong source: got %q want %q", got, want)
			}
		})
	}
}

func TestRenderedLongMixedHeadingFitsNarrowInput(t *testing.T) {
	engine := app.PlatformTextEngine()
	if engine == nil {
		t.Skip("platform text engine unavailable")
	}
	state := newModel()
	state.session.Current().Replace("# Title续**粗** " + strings.Repeat("多行段落需要按视觉位置选择文字。", 8) + "\n")
	var canvas gfx.Canvas
	canvas.SetTextEngine(engine)
	driver, err := app.NewDriver(newConfig(state), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	state.rich.input.MoveTo(state.rich.input.RuneCount(), false)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := driver.Pump(); err != nil {
			t.Fatal(err)
		}
	}
	if state.mode != renderedMode || !state.rich.active {
		t.Fatalf("long mixed heading mode=%v active=%v", state.mode, state.rich.active)
	}
	if state.previewScroll <= 0 {
		caret, ok := state.rich.CaretBounds()
		t.Fatalf("mixed heading did not reveal its last-line caret: mixed=%v wrapped=%v focus=%v caret=%+v visible=%v viewport=%+v", state.rich.mixed, state.rich.wrapped, driver.Snapshot().Focused, caret, ok, state.previewRect)
	}
	if caret, ok := state.rich.CaretBounds(); !ok || caret.Y+caret.Height > state.previewRect.Y+state.previewRect.Height+1 {
		t.Fatalf("mixed heading caret %+v visible=%v exceeds viewport %+v", caret, ok, state.previewRect)
	}
	snapshot := driver.Snapshot()
	var input ui.Rect
	for _, hit := range snapshot.Hits {
		if hit.ID == richInputID {
			input = hit.Bounds
			break
		}
	}
	if input.Width == 0 {
		t.Fatal("rendered input hit missing")
	}
	if caret, ok := state.rich.CaretBounds(); !ok || caret.Y+caret.Height > input.Y+input.Height+1 {
		t.Errorf("last-line caret %+v visible=%v extends past input %+v", caret, ok, input)
	}
	checked := 0
	for _, label := range snapshot.Texts {
		if label.Value == "" || label.Style.Mono || label.Bounds.X < input.X-1 || label.Bounds.X > input.X+input.Width+1 || label.Bounds.Y < input.Y-1 || label.Bounds.Y > input.Y+input.Height+1 {
			continue
		}
		width := canvas.TextWidthStyle(label.Value, label.Style)
		if label.Bounds.X+width > input.X+input.Width+1 || label.Bounds.Y+label.Bounds.Height > input.Y+input.Height+1 {
			t.Errorf("native line %q at %+v measured %gpx outside input %+v", label.Value, label.Bounds, width, input)
		}
		checked++
	}
	if checked < 5 {
		t.Fatalf("only %d native heading pieces checked", checked)
	}
	for _, problem := range nativeTextOverlapProblems(&canvas, snapshot) {
		t.Fatalf("long mixed heading layout=%+v", problem)
	}
	for _, problem := range headless.CheckLayout(snapshot) {
		if problem.Kind == headless.LayoutCollide {
			t.Fatalf("long mixed heading layout=%+v", problem)
		}
	}
}

func TestRenderedNestedATXHeadingFallsBackToSource(t *testing.T) {
	state, driver := testDriver(t)
	source := "# ***nested*** tail\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if state.mode != editMode || state.session.Current().Text() != source || driver.Snapshot().Focused != sourceID {
		t.Fatalf("nested heading did not fall back: mode=%v source=%q focus=%v", state.mode, state.session.Current().Text(), driver.Snapshot().Focused)
	}
}

func TestRenderedHeadingKeepsReadingWeightWhileEditing(t *testing.T) {
	for _, test := range []struct {
		name   string
		body   string
		weight float32
	}{
		{"h1", "# Heading\n", gfx.WeightBold},
		{"h2", "## Section\n", gfx.WeightSemibold},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, driver := testDriver(t)
			state.session.Current().Replace(test.body)
			clickID(t, driver, renderedModeID)
			clickID(t, driver, renderedRows.At(0))
			if editor := driver.Snapshot().Editor; editor == nil || editor.Weight != test.weight {
				t.Fatalf("editing weight = %+v, want %v", editor, test.weight)
			}
		})
	}
}

func TestRenderedFirstClickPlacesCaretAtVisibleText(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Heading\n")
	clickID(t, driver, renderedModeID)
	x, _ := state.markdownColumn(state.previewRect)
	y := state.previewRect.Y + 46 + state.preview.blocks[0].top + 18
	if err := driver.Click(x+2, y); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# XHeading\n" {
		t.Fatalf("first click ignored text position: %q", got)
	}
}

func TestRenderedArrowDownMovesToNextEditableBlock(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# 标题\n\n正文\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Key(gfx.KeyDown, 0); err != nil {
		t.Fatal(err)
	}
	if !state.rich.active || state.rich.index != 1 || driver.Snapshot().Focused != richInputID {
		t.Fatalf("down stayed on row %d, focus=%v", state.rich.index, driver.Snapshot().Focused)
	}
	if err := driver.Type("续"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# 标题\n\n正文续\n" {
		t.Fatalf("typing after down = %q", got)
	}
}

func TestRenderedShiftDownCopiesAndCutsAcrossEditableBlocks(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Title\n\nBody\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if state.mode != renderedMode || !state.selection.active {
		t.Fatalf("shift-down did not create a rendered selection: mode=%v", state.mode)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'c'); err != nil {
		t.Fatal(err)
	}
	if got := driver.Clipboard(); got != "\nBody" {
		t.Fatalf("copied visible text = %q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'x'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\n" || state.mode != renderedMode || !state.rich.active {
		t.Fatalf("cross-block cut = %q mode=%v active=%v", got, state.mode, state.rich.active)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\n\nBody\n" || state.mode != renderedMode || !state.rich.active {
		t.Fatalf("cross-block undo = %q mode=%v active=%v", got, state.mode, state.rich.active)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\n" || state.mode != renderedMode || !state.rich.active {
		t.Fatalf("cross-block redo = %q mode=%v active=%v", got, state.mode, state.rich.active)
	}
}

func TestRenderedShiftSelectionCopiesButCannotCutReadOnlyBlock(t *testing.T) {
	state, driver := testDriver(t)
	source := "# Title\n\n```go\ncode\n```\n\nBody\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if !state.selection.active {
		t.Fatal("read-only code block cannot be selected for copying")
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'c'); err != nil {
		t.Fatal(err)
	}
	if got := driver.Clipboard(); got != "\ncode" {
		t.Fatalf("read-only visible copy = %q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'x'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != source || state.mode != renderedMode || !state.selection.active {
		t.Fatal("cut rewrote a read-only block")
	}
}

func TestRenderedCopyAcrossFormattedParagraphAndTable(t *testing.T) {
	state, driver := testDriver(t)
	source := "# Title\n\n**bold** text\n\n| A | B |\n|---|---|\n| 1 | 2 |\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	for range 2 {
		if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'c'); err != nil {
		t.Fatal(err)
	}
	if got := driver.Clipboard(); got != "\nbold text\nA\tB\n1\t2" {
		t.Fatalf("formatted/table copy = %q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'x'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != source || state.mode != renderedMode {
		t.Fatal("cut rewrote formatted or table source")
	}
}

func TestRenderedPasteReplacesEditableSelectionAndCanUndo(t *testing.T) {
	state, driver := testDriver(t)
	source := "# Title\n\nBody\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	driver.SetClipboard(" plus")
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title plus\n" || state.mode != renderedMode || !state.rich.active {
		t.Fatalf("cross-block paste = %q mode=%v active=%v", got, state.mode, state.rich.active)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != source {
		t.Fatalf("paste undo = %q", got)
	}
}

func TestRenderedCrossBlockPlainMergeKeepsEditingAndHistory(t *testing.T) {
	state, driver := testDriver(t)
	source := "# Title\n\nBody\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	driver.SetClipboard(" plus")
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title plus\n" || state.mode != renderedMode || !state.rich.active || state.rich.input.String() != "Title plus" || driver.Snapshot().Focused != richInputID {
		t.Fatalf("plain merge did not keep rendered focus: %q mode=%v active=%v focus=%v", got, state.mode, state.rich.active, driver.Snapshot().Focused)
	}
	if err := driver.Type("!"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title plus!\n" {
		t.Fatalf("typing after plain merge=%q", got)
	}
	for _, want := range []string{"# Title plus\n", source} {
		if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
			t.Fatal(err)
		}
		if got := state.session.Current().Text(); got != want || state.mode != renderedMode || !state.rich.active || driver.Snapshot().Focused != richInputID {
			t.Fatalf("rendered Undo=%q mode=%v active=%v focus=%v, want %q", got, state.mode, state.rich.active, driver.Snapshot().Focused, want)
		}
	}
	for _, want := range []string{"# Title plus\n", "# Title plus!\n"} {
		if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
			t.Fatal(err)
		}
		if got := state.session.Current().Text(); got != want || state.mode != renderedMode || !state.rich.active || driver.Snapshot().Focused != richInputID {
			t.Fatalf("rendered Redo=%q mode=%v active=%v focus=%v, want %q", got, state.mode, state.rich.active, driver.Snapshot().Focused, want)
		}
	}
	if err := driver.Type("?"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title plus!?\n" {
		t.Fatalf("typing after structural Redo used stale caret: %q", got)
	}
}

func TestRenderedCrossBlockUndoThenNewTypingDiscardsStructuralRedo(t *testing.T) {
	state, driver := testDriver(t)
	source := "# Title\n\nBody\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	driver.SetClipboard(" plus")
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != source || state.mode != renderedMode {
		t.Fatalf("structural Undo=%q mode=%v", got, state.mode)
	}
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	want := "# TitleX\n\nBody\n"
	if got := state.session.Current().Text(); got != want {
		t.Fatalf("new branch=%q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != source {
		t.Fatalf("new branch Undo=%q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != want || state.mode != renderedMode || !state.rich.active {
		t.Fatalf("new branch Redo=%q mode=%v active=%v", got, state.mode, state.rich.active)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != want {
		t.Fatalf("obsolete structural Redo reappeared: %q", got)
	}
}

func TestRenderedCrossBlockPlainMergeKeepsBOMCRLFThroughHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plain-merge.md")
	source := []byte("\xef\xbb\xbf# 标题\r\n\r\n正文\r\n")
	if err := os.WriteFile(path, source, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	driver.SetClipboard("续")
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	want := []byte("\xef\xbb\xbf# 标题续\r\n")
	for _, step := range []struct {
		shortcut rune
		bytes    []byte
	}{
		{0, want},
		{'z', source},
		{'y', want},
	} {
		if step.shortcut != 0 {
			if _, err := driver.ShortcutChord(primaryModifier(), step.shortcut); err != nil {
				t.Fatal(err)
			}
		}
		if state.mode != renderedMode || !state.rich.active || driver.Snapshot().Focused != richInputID {
			t.Fatalf("history step %q left rendered editor: mode=%v active=%v focus=%v", step.shortcut, state.mode, state.rich.active, driver.Snapshot().Focused)
		}
		if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
			t.Fatal(err)
		}
		if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, step.bytes) {
			t.Fatalf("history step %q saved %q, want %q, err=%v", step.shortcut, data, step.bytes, err)
		}
	}
}

func TestRenderedStructuralAndLocalRedoKeepCaretBeforeLongSuffix(t *testing.T) {
	state, driver := testDriver(t)
	paragraph := strings.Repeat("长段落保留的尾文。", 8)
	source := "# Title\n\n" + paragraph + "\n"
	state.session.Current().Replace(source)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	caret, ok := state.preview.blocks[1].view.CaretAt(len("长"))
	if !ok {
		t.Fatal("no caret inside long paragraph")
	}
	startX, startY := state.rich.rect.X+300, state.rich.rect.Y+15
	targetY := caret.Y + caret.Height/2
	if err := driver.Press(startX, startY, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := driver.Move(caret.X, targetY); err != nil {
		t.Fatal(err)
	}
	if err := driver.Release(caret.X, targetY, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if !state.selection.editable || state.selection.lastOffset != len("长") {
		t.Fatalf("partial plain selection=%+v", state.selection)
	}
	driver.SetClipboard("续")
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("!"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
			t.Fatal(err)
		}
	}
	if got := state.session.Current().Text(); got != source {
		t.Fatalf("structural and local Undo=%q", got)
	}
	for range 2 {
		if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
			t.Fatal(err)
		}
	}
	if err := driver.Type("?"); err != nil {
		t.Fatal(err)
	}
	if got, want := state.session.Current().Text(), "# Title续!?"+paragraph[len("长"):]+"\n"; got != want || state.mode != renderedMode {
		t.Fatalf("typing after nested Redo=%q mode=%v, want %q", got, state.mode, want)
	}
}

func TestRenderedPasteRefusesReadOnlySelection(t *testing.T) {
	state, driver := testDriver(t)
	source := "# Title\n\n```go\ncode\n```\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	driver.SetClipboard("unsafe")
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != source || state.mode != renderedMode || !state.selection.active {
		t.Fatal("paste rewrote a read-only code block")
	}
}

func TestRenderedCrossBlockPasteKeepsDocumentNewlines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crlf.md")
	source := []byte("\xef\xbb\xbf# 标题\r\n\r\n正文\r\n\r\n尾段\r\n")
	if err := os.WriteFile(path, source, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	driver.SetClipboard("新\n行")
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	if state.mode != editMode {
		t.Fatalf("multiline cross-block paste stayed rendered: mode=%v", state.mode)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	want := []byte("\xef\xbb\xbf# 标题新\r\n行\r\n\r\n尾段\r\n")
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("CRLF paste = %q, want %q, err=%v", got, want, err)
	}
}

func TestRenderedNewlineKeepsCRLF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "enter.md")
	source := []byte("\xef\xbb\xbf# 标题\r\n\r\n正文\r\n")
	if err := os.WriteFile(path, source, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if state.mode != renderedMode || !state.rich.virtual {
		t.Fatalf("Enter left rendered mode=%v source=%q", state.mode, state.session.Current().Text())
	}
	if got := state.session.Current().Text(); got != "# 标题\r\n\r\n正文\r\n\r\n\r\n" {
		t.Fatalf("rendered Enter mixed line endings: %q", got)
	}
	if err := driver.Type("新段"); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	want := []byte("\xef\xbb\xbf# 标题\r\n\r\n正文\r\n\r\n新段\r\n")
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("rendered Enter save = %q, want %q, err=%v", got, want, err)
	}
}

func TestRenderedEnterAtBlockEndCreatesEditableParagraph(t *testing.T) {
	path := filepath.Join(t.TempDir(), "enter-rendered.md")
	source := []byte("# Title\n\nBody\n")
	if err := os.WriteFile(path, source, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if state.mode != renderedMode || !state.rich.active || !state.rich.virtual || state.session.Current().Text() != "# Title\n\nBody\n\n\n" {
		t.Fatalf("rendered Enter mode=%v virtual=%v source=%q", state.mode, state.rich.virtual, state.session.Current().Text())
	}
	if err := driver.Type("Next"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\n\nBody\n\nNext\n" || state.mode != renderedMode {
		t.Fatalf("typing new paragraph mode=%v source=%q", state.mode, got)
	}
	if err := driver.Resize(1202, 780); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("!"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\n\nBody\n\nNext!\n" {
		t.Fatalf("continued input duplicated paragraph: %q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\n\nBody\n\n\n" || state.mode != renderedMode {
		t.Fatalf("undo new paragraph mode=%v source=%q", state.mode, got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\n\nBody\n\nNext!\n" {
		t.Fatalf("redo paragraph source=%q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\n\nBody\n" {
		t.Fatalf("undo Enter source=%q", got)
	}
}

func TestRenderedEnterWithoutFinalNewlineStaysRendered(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("Body")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if state.mode != renderedMode || !state.rich.virtual || state.session.Current().Text() != "Body\n\n" {
		t.Fatalf("new paragraph without final newline: mode=%v source=%q", state.mode, state.session.Current().Text())
	}
	if err := driver.Type("Next"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "Body\n\nNext" {
		t.Fatalf("new paragraph rewrote final newline policy: %q", got)
	}
}

func TestRenderedEnterBetweenParagraphsKeepsFollowingBlock(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Title\n\nBody\n\nTail\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if state.mode != renderedMode || !state.rich.virtual || state.session.Current().Text() != "# Title\n\nBody\n\n\n\nTail\n" {
		t.Fatalf("inserting between paragraphs: mode=%v source=%q", state.mode, state.session.Current().Text())
	}
	if err := driver.Type("Next"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\n\nBody\n\nNext\n\nTail\n" || state.mode != renderedMode {
		t.Fatalf("inserted paragraph affected next block: mode=%v source=%q", state.mode, got)
	}
	if err := driver.Resize(1202, 780); err != nil {
		t.Fatal(err)
	}
	tailY := state.previewRect.Y + 46 - state.previewScroll + state.preview.blocks[3].top
	if state.rich.rect.Width <= 0 || state.rich.rect.Y+state.rich.rect.Height > tailY {
		t.Fatalf("new paragraph is not laid out ahead of Tail: editor=%+v tail=%+v", state.rich.rect, state.preview.blocks[3])
	}
}

func TestRenderedEnterAgainAfterTypingParagraph(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("Body\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("Next"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if state.mode != renderedMode || !state.rich.virtual || state.session.Current().Text() != "Body\n\nNext\n\n\n" {
		t.Fatalf("second Enter left rendered mode=%v source=%q", state.mode, state.session.Current().Text())
	}
	if err := driver.Type("Third"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "Body\n\nNext\n\nThird\n" {
		t.Fatalf("second new paragraph source=%q", got)
	}
	for _, want := range []string{"Body\n\nNext\n\n\n", "Body\n\nNext\n", "Body\n\n\n", "Body\n"} {
		if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
			t.Fatal(err)
		}
		if got := state.session.Current().Text(); got != want {
			t.Fatalf("undo across paragraphs = %q, want %q", got, want)
		}
	}
}

func TestRenderedEnterInsidePlainParagraphStaysRendered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "split.md")
	if err := os.WriteFile(path, []byte("\xef\xbb\xbf# Title\r\n\r\nBodyText\r\n\r\nTail\r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	state.rich.input.MoveTo(4, false)
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if state.mode != renderedMode || !state.rich.active || state.rich.input.String() != "Text" || state.rich.input.Caret() != 0 {
		t.Fatalf("split paragraph mode=%v active=%v source=%q", state.mode, state.rich.active, state.session.Current().Text())
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	want := []byte("\xef\xbb\xbf# Title\r\n\r\nBody\r\n\r\n新Text\r\n\r\nTail\r\n")
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("split save = %q, want %q, err=%v", got, want, err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\r\n\r\nBody\r\n\r\nText\r\n\r\nTail\r\n" {
		t.Fatalf("undo right paragraph = %q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\r\n\r\nBodyText\r\n\r\nTail\r\n" {
		t.Fatalf("undo split = %q", got)
	}
}

func TestRenderedEnterInsideParagraphFallsBackWhenRightBecomesSyntax(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("Body# Heading\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	state.rich.input.MoveTo(4, false)
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if state.mode != editMode || state.session.Current().Text() != "Body\n# Heading\n" {
		t.Fatalf("syntax split mode=%v source=%q", state.mode, state.session.Current().Text())
	}
}

func TestRenderedEnterInsideParagraphWithArrowKeys(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Title\n\nBodyText\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	for range 4 {
		if err := driver.Key(gfx.KeyLeft, 0); err != nil {
			t.Fatal(err)
		}
	}
	if caret := state.rich.input.Caret(); caret != 4 {
		t.Fatalf("arrow keys left caret at %d", caret)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); state.mode != renderedMode || got != "# Title\n\nBody\n\nText\n" {
		t.Fatalf("keyboard split mode=%v source=%q", state.mode, got)
	}
}

func TestRenderedEnterAfterHeadingCreatesPlainParagraph(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Title\n\nBody\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if state.mode != renderedMode || !state.rich.virtual || state.session.Current().Text() != "# Title\n\n\n\nBody\n" {
		t.Fatalf("heading Enter mode=%v source=%q", state.mode, state.session.Current().Text())
	}
	if err := driver.Type("Intro"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\n\nIntro\n\nBody\n" {
		t.Fatalf("heading to paragraph source=%q", got)
	}
}

func TestRenderedEnterInsideHeadingCreatesPlainRightParagraph(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# TitleText\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	state.rich.input.MoveTo(5, false)
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if state.mode != renderedMode || !state.rich.active || state.rich.input.String() != "Text" {
		t.Fatalf("heading split mode=%v source=%q", state.mode, state.session.Current().Text())
	}
	if err := driver.Type("New"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\n\nNewText\n" {
		t.Fatalf("heading split source=%q", got)
	}
}

func TestRenderedBackspaceAtParagraphStartMergesPlainBlocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "merge.md")
	if err := os.WriteFile(path, []byte("\xef\xbb\xbf# Title\r\n\r\nBody\r\n\r\nTail\r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(2))
	state.rich.input.MoveTo(0, false)
	if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
		t.Fatal(err)
	}
	if state.mode != renderedMode || !state.rich.active || state.rich.input.String() != "BodyTail" || state.rich.input.Caret() != 4 {
		t.Fatalf("merge mode=%v source=%q", state.mode, state.session.Current().Text())
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	want := []byte("\xef\xbb\xbf# Title\r\n\r\nBodyTail\r\n")
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("merged save = %q, want %q, err=%v", got, want, err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\r\n\r\nBody\r\n\r\nTail\r\n" {
		t.Fatalf("undo merge source=%q", got)
	}
}

func TestRenderedBackspaceInEmptyParagraphReturnsToPreviousBlock(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Title\n\nBody\n\nTail\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if !state.rich.virtual {
		t.Fatal("Enter did not create an empty editable paragraph")
	}
	if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\n\nBody\n\nTail\n" || state.mode != renderedMode || !state.rich.active || state.rich.input.String() != "Body" || state.rich.input.Caret() != 4 {
		t.Fatalf("backspace empty paragraph mode=%v source=%q", state.mode, got)
	}
}

func TestRenderedBackspaceAtParagraphStartCanJoinHeading(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Title\n\nBody\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	state.rich.input.MoveTo(0, false)
	if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
		t.Fatal(err)
	}
	if state.mode != renderedMode || !state.rich.active || state.rich.input.String() != "TitleBody" || state.session.Current().Text() != "# TitleBody\n" {
		t.Fatalf("heading merge mode=%v source=%q", state.mode, state.session.Current().Text())
	}
}

func TestRenderedBackspaceDoesNotRewriteFormattedPreviousBlock(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("**bold**\n\nTail\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	state.rich.input.MoveTo(0, false)
	if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "**bold**\n\nTail\n" || state.mode != renderedMode {
		t.Fatalf("formatted predecessor mutated: mode=%v source=%q", state.mode, got)
	}
}

func TestRenderedUnorderedListEnterCreatesEditableNextItem(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("- one\n- two\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if state.mode != renderedMode || !state.rich.virtual || state.session.Current().Text() != "- one\n- two\n- \n" {
		t.Fatalf("list Enter mode=%v source=%q", state.mode, state.session.Current().Text())
	}
	if err := driver.Type("three"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "- one\n- two\n- three\n" || state.mode != renderedMode {
		t.Fatalf("new list item mode=%v source=%q", state.mode, got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "- one\n- two\n- \n" {
		t.Fatalf("undo list input = %q", got)
	}
	if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "- one\n- two\n" || state.mode != renderedMode {
		t.Fatalf("delete empty list item mode=%v source=%q", state.mode, got)
	}
}

func TestRenderedListEnterBetweenItemsPreservesCRLFAndFollowingItem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "list.md")
	if err := os.WriteFile(path, []byte("\xef\xbb\xbf* one\r\n* two\r\n* three\r\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if state.mode != renderedMode || !state.rich.virtual || state.rich.rect.Width == 0 || state.session.Current().Text() != "* one\r\n* two\r\n* \r\n* three\r\n" {
		t.Fatalf("middle list Enter mode=%v rect=%+v source=%q", state.mode, state.rich.rect, state.session.Current().Text())
	}
	if err := driver.Type("new"); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	want := []byte("\xef\xbb\xbf* one\r\n* two\r\n* new\r\n* three\r\n")
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("middle list save = %q, want %q, err=%v", got, want, err)
	}
}

func TestRenderedEnterInEmptyParagraphDoesNotInsertAtDocumentStart(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Title\n\nBody\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	for range 2 {
		if err := driver.Key(gfx.KeyEnter, 0); err != nil {
			t.Fatal(err)
		}
	}
	if got := state.session.Current().Text(); got != "# Title\n\nBody\n\n\n\n" || state.mode != renderedMode || !state.rich.virtual {
		t.Fatalf("empty paragraph Enter mode=%v source=%q", state.mode, got)
	}
	if err := driver.Type("Next"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\n\nBody\n\n\nNext\n" {
		t.Fatalf("after empty paragraph Enter = %q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\n\nBody\n\n\n" || !state.rich.virtual {
		t.Fatalf("backspace second empty paragraph = %q", got)
	}
	if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\n\nBody\n" || state.mode != renderedMode || state.rich.virtual {
		t.Fatalf("backspace first empty paragraph mode=%v source=%q", state.mode, got)
	}
}

func TestRenderedEnterInEmptyListItemExitsList(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("- one\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "- one\n\n\n" || state.mode != renderedMode || !state.rich.virtual {
		t.Fatalf("exit list mode=%v source=%q", state.mode, got)
	}
	if err := driver.Type("Body"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "- one\n\nBody\n" || state.mode != renderedMode {
		t.Fatalf("paragraph after list = %q mode=%v", got, state.mode)
	}
}

func TestRenderedEmptyListExitAndBackspaceInMiddlePreserveLaterItem(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("- one\n- two\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	for range 2 {
		if err := driver.Key(gfx.KeyEnter, 0); err != nil {
			t.Fatal(err)
		}
	}
	if got := state.session.Current().Text(); got != "- one\n\n\n- two\n" || state.mode != renderedMode {
		t.Fatalf("middle list exit mode=%v source=%q", state.mode, got)
	}
	if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "- one\n- \n- two\n" || !state.rich.virtual {
		t.Fatalf("restore empty list item source=%q", got)
	}
	if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "- one\n- two\n" || state.mode != renderedMode {
		t.Fatalf("restore original list source=%q mode=%v", got, state.mode)
	}
}

func TestRenderedEmptyListExitInMiddleAcceptsParagraph(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("- one\n- two\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	for range 2 {
		if err := driver.Key(gfx.KeyEnter, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := driver.Type("Body"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "- one\n\nBody\n- two\n" || state.mode != renderedMode {
		t.Fatalf("paragraph amid list mode=%v source=%q", state.mode, got)
	}
}

func TestRenderedOrderedListEnterIncrementsNumberAndDrawsMarker(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("8. one\n9. two\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "8. one\n9. two\n10. \n" || state.mode != renderedMode || !state.rich.virtual {
		t.Fatalf("ordered Enter mode=%v source=%q", state.mode, got)
	}
	if !hasText(driver, "10.") {
		t.Fatal("empty ordered item did not show number 10")
	}
	if err := driver.Type("three"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "8. one\n9. two\n10. three\n" || !hasText(driver, "10.") {
		t.Fatalf("ordered item input = %q", got)
	}
}

func TestRenderedTaskCheckboxAndLabelStayNativeAndUndoable(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Title\n\n- [ ] Task\n- [x] Done\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, taskChecks.At(1))
	if got := state.session.Current().Text(); got != "# Title\n\n- [x] Task\n- [x] Done\n" || state.mode != renderedMode {
		t.Fatalf("task toggle mode=%v source=%q", state.mode, got)
	}
	clickID(t, driver, renderedRows.At(1))
	if !state.rich.active || state.mode != renderedMode {
		t.Fatal("task label did not activate native rendered input")
	}
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\n\n- [x] TaskX\n- [x] Done\n" {
		t.Fatalf("task label edit = %q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\n\n- [x] Task\n- [x] Done\n" {
		t.Fatalf("task label undo = %q", got)
	}
}

func TestRenderedTaskToggleKeepsActiveLabelFocus(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("- [ ] Task\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	clickID(t, driver, taskChecks.At(0))
	if driver.Snapshot().Focused != richInputID {
		t.Fatalf("checkbox stole active label focus: %v", driver.Snapshot().Focused)
	}
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "- [x] TaskX\n" {
		t.Fatalf("task input after checkbox = %q", got)
	}
}

func TestRenderedCompositionCannotStartCrossBlockSelection(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Title\n\nBody\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Compose("pinyin"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if state.selection.active || state.session.Current().Text() != "# Title\n\nBody\n" {
		t.Fatal("cross-block selection committed unfinished IME input")
	}
}

func TestRenderedShiftUpCopiesVisibleText(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Title\n\nBody\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	if err := driver.Key(gfx.KeyUp, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'c'); err != nil {
		t.Fatal(err)
	}
	if got := driver.Clipboard(); got != "Title\nBody" {
		t.Fatalf("reverse selection copied %q", got)
	}
}

func TestRenderedCrossBlockCutUndoRestoresBOMCRLF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preserve.md")
	source := []byte("\xef\xbb\xbf# 标题\r\n\r\n正文\r\n")
	if err := os.WriteFile(path, source, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'x'); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, source) || state.session.Current().Dirty() {
		t.Fatalf("cut/undo/save changed source: %q dirty=%v err=%v", got, state.session.Current().Dirty(), err)
	}
}

func TestRenderedEscapeClearsSelectionWithoutChangingSource(t *testing.T) {
	state, driver := testDriver(t)
	source := "# Title\n\nBody\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if state.selection.active || state.mode != renderedMode || state.session.Current().Text() != source {
		t.Fatalf("Escape kept a selection or modified source: %+v", state.selection)
	}
}

func TestRenderedShiftClickUsesVisibleTargetOffset(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Title\n\nBody\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	x, _ := state.markdownColumn(state.previewRect)
	y := state.previewRect.Y + 46 + state.preview.blocks[1].top + 12
	if err := driver.KeyDown(gfx.KeyShiftLeft, ""); err != nil {
		t.Fatal(err)
	}
	if err := driver.Click(x+2, y); err != nil {
		t.Fatal(err)
	}
	if err := driver.KeyUp(gfx.KeyShiftLeft, ""); err != nil {
		t.Fatal(err)
	}
	if !state.selection.active {
		t.Fatal("Shift-click did not create a rendered selection")
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'c'); err != nil {
		t.Fatal(err)
	}
	if got := driver.Clipboard(); got != "\n" {
		t.Fatalf("Shift-click copied %q, want only the paragraph boundary", got)
	}
}

func TestRenderedShiftDownExtendsSelectionAcrossThreeBlocks(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Title\n\nBody\n\nMore\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	for range 2 {
		if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'c'); err != nil {
		t.Fatal(err)
	}
	if got := driver.Clipboard(); got != "\nBody\nMore" {
		t.Fatalf("extended selection copied %q", got)
	}
	if err := driver.Key(gfx.KeyUp, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'c'); err != nil {
		t.Fatal(err)
	}
	if got := driver.Clipboard(); got != "\nBody" {
		t.Fatalf("shrunk selection copied %q", got)
	}
}

func TestRenderedPointerDragSelectsAcrossBlocks(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Title\n\nBody\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	startX, startY := state.rich.rect.X+120, state.rich.rect.Y+15
	targetX := state.rich.rect.X + 80
	targetY := state.previewRect.Y + 46 + state.preview.blocks[1].top + 12
	if err := driver.Press(startX, startY, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := driver.Move(targetX, targetY); err != nil {
		t.Fatal(err)
	}
	if err := driver.Release(targetX, targetY, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if !state.selection.active {
		t.Fatal("drag did not make a cross-block selection")
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'c'); err != nil {
		t.Fatal(err)
	}
	if got := driver.Clipboard(); got != "\nBody" {
		t.Fatalf("drag copied %q", got)
	}
}

func TestRenderedPointerDragIntoWrappedParagraphUsesVisualRow(t *testing.T) {
	state, driver := testDriver(t)
	paragraph := strings.Repeat("多行段落需要按视觉位置选择文字。", 8)
	state.session.Current().Replace("# Title\n\n" + paragraph + "\n")
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	startX, startY := state.rich.rect.X+300, state.rich.rect.Y+15
	targetX := state.rich.rect.X + 80
	targetY := state.previewRect.Y + 46 + state.preview.blocks[1].top + 38
	expected, ok := state.preview.blocks[1].view.OffsetAt(ui.Point{X: targetX, Y: targetY})
	if !ok || expected <= 10 || expected >= len(paragraph) {
		t.Fatalf("fixture did not hit a later visual row: offset=%d ok=%v", expected, ok)
	}
	if err := driver.Press(startX, startY, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := driver.Move(targetX, targetY); err != nil {
		t.Fatal(err)
	}
	if err := driver.Release(targetX, targetY, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if !state.selection.active {
		t.Fatal("drag to wrapped paragraph did not create a selection")
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'c'); err != nil {
		t.Fatal(err)
	}
	if got, want := driver.Clipboard(), "\n"+paragraph[:expected]; got != want {
		t.Fatalf("wrapped cross-block copy=%q, want %q", got, want)
	}
	secondY := state.previewRect.Y + 46 + state.preview.blocks[1].top + 26
	for _, primitive := range driver.Snapshot().Primitives {
		if primitive.Color.Alpha == 0.25 && primitive.Bounds.Y >= secondY-1 && primitive.Bounds.Y <= secondY+1 && primitive.Bounds.Width > 0 {
			return
		}
	}
	t.Fatal("wrapped cross-block selection has no highlight on its second visual row")
}

func TestRenderedPointerDragIntoWrappedFormattedParagraphReplacesSafeRange(t *testing.T) {
	state, driver := testDriver(t)
	plain := strings.Repeat("多行段落需要按视觉位置选择文字。", 8)
	visible := "加粗 " + plain
	source := "# Title\n\n**加粗** " + plain + "\n"
	state.session.Current().Replace(source)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	startX, startY := state.rich.rect.X+300, state.rich.rect.Y+15
	targetX := state.rich.rect.X + 80
	targetY := state.previewRect.Y + 46 + state.preview.blocks[1].top + 38
	view := &state.preview.blocks[1].view
	expected, ok := view.OffsetAt(ui.Point{X: targetX, Y: targetY})
	if !ok || view.TextLen() != len(visible) || expected <= len("加粗 ")+10 || expected >= len(visible) {
		t.Fatalf("fixture did not hit a later formatted row: offset=%d len=%d ok=%v", expected, view.TextLen(), ok)
	}
	if err := driver.Press(startX, startY, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := driver.Move(targetX, targetY); err != nil {
		t.Fatal(err)
	}
	if err := driver.Release(targetX, targetY, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if !state.selection.active || !state.selection.editable {
		t.Fatalf("formatted cross-block selection state=%+v", state.selection)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'c'); err != nil {
		t.Fatal(err)
	}
	if got, want := driver.Clipboard(), "\n"+visible[:expected]; got != want {
		t.Fatalf("formatted wrapped copy=%q, want %q", got, want)
	}
	secondY := state.previewRect.Y + 46 + state.preview.blocks[1].top + 26
	highlight := false
	for _, primitive := range driver.Snapshot().Primitives {
		if primitive.Color.Alpha == 0.25 && primitive.Bounds.Y >= secondY-1 && primitive.Bounds.Y <= secondY+1 && primitive.Bounds.Width > 0 {
			highlight = true
			break
		}
	}
	if !highlight {
		t.Fatal("formatted wrapped selection has no second-row highlight")
	}
	for _, problem := range headless.CheckLayout(driver.Snapshot()) {
		if problem.Kind == headless.LayoutOverlap || problem.Kind == headless.LayoutSpill || problem.Kind == headless.LayoutCollide {
			t.Fatalf("formatted wrapped selection layout=%+v", problem)
		}
	}
	driver.SetClipboard("续")
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	want := "# Title续" + plain[expected-len("加粗 "):] + "\n"
	if got := state.session.Current().Text(); got != want || state.mode != renderedMode || !state.rich.active {
		t.Fatalf("formatted cross-block paste=%q mode=%v active=%v, want %q", got, state.mode, state.rich.active, want)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != source {
		t.Fatalf("formatted cross-block undo=%q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != want || state.mode != renderedMode {
		t.Fatalf("formatted cross-block redo=%q mode=%v", got, state.mode)
	}
	if err := driver.Type("!"); err != nil {
		t.Fatal(err)
	}
	if got, expectedSource := state.session.Current().Text(), "# Title续!"+plain[expected-len("加粗 "):]+"\n"; got != expectedSource {
		t.Fatalf("typing after formatted Redo=%q, want %q", got, expectedSource)
	}
}

func TestRenderedCrossBlockSelectionRefusesPartialLinkLeaf(t *testing.T) {
	for _, testcase := range []struct {
		name, body, visible, selected string
	}{
		{"link", "[链接](https://example.invalid/keep) 尾段", "链接 尾段", "链"},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			state, driver := testDriver(t)
			source := "# Title\n\n" + testcase.body + "\n"
			state.session.Current().Replace(source)
			if err := driver.Resize(740, 520); err != nil {
				t.Fatal(err)
			}
			clickID(t, driver, renderedModeID)
			clickID(t, driver, renderedRows.At(0))
			view := &state.preview.blocks[1].view
			if view.TextLen() != len(testcase.visible) {
				t.Fatalf("rendered leaf length=%d, want %d", view.TextLen(), len(testcase.visible))
			}
			caret, ok := view.CaretAt(len(testcase.selected))
			if !ok {
				t.Fatal("no caret inside formatted leaf")
			}
			startX, startY := state.rich.rect.X+300, state.rich.rect.Y+15
			targetY := caret.Y + caret.Height/2
			if err := driver.Press(startX, startY, gfx.PointerButtonPrimary); err != nil {
				t.Fatal(err)
			}
			if err := driver.Move(caret.X, targetY); err != nil {
				t.Fatal(err)
			}
			if err := driver.Release(caret.X, targetY, gfx.PointerButtonPrimary); err != nil {
				t.Fatal(err)
			}
			if !state.selection.active || state.selection.editable {
				t.Fatalf("partial formatted selection=%+v", state.selection)
			}
			if _, err := driver.ShortcutChord(primaryModifier(), 'x'); err != nil {
				t.Fatal(err)
			}
			driver.SetClipboard("替换")
			if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
				t.Fatal(err)
			}
			if got := state.session.Current().Text(); got != source || state.mode != renderedMode {
				t.Fatalf("unsafe cross-block edit=%q mode=%v", got, state.mode)
			}
		})
	}
}

func TestRenderedCrossBlockPasteKeepsPartialBoldSuffixAndBOMCRLF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial-bold.md")
	source := []byte("\xef\xbb\xbf# Title\r\n\r\n**加粗** 尾段\r\n\r\nTail\r\n")
	if err := os.WriteFile(path, source, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	caret, ok := state.preview.blocks[1].view.CaretAt(len("加"))
	if !ok {
		t.Fatal("no caret inside bold suffix")
	}
	startX, startY := state.rich.rect.X+300, state.rich.rect.Y+15
	targetY := caret.Y + caret.Height/2
	if err := driver.Press(startX, startY, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := driver.Move(caret.X, targetY); err != nil {
		t.Fatal(err)
	}
	if err := driver.Release(caret.X, targetY, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if !state.selection.active || !state.selection.editable || state.selection.lastOffset != len("加") {
		t.Fatalf("partial bold selection=%+v", state.selection)
	}
	driver.SetClipboard("续")
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	want := []byte("\xef\xbb\xbf# Title续**粗** 尾段\r\n\r\nTail\r\n")
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, want) || state.mode != renderedMode || !state.rich.active {
		t.Fatalf("partial bold paste=%q, want %q, mode=%v active=%v err=%v", data, want, state.mode, state.rich.active, err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, source) {
		t.Fatalf("partial bold Undo=%q, err=%v", data, err)
	}
}

func TestRenderedCrossBlockCutKeepsPartialBoldPrefixAndBOMCRLF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial-bold-prefix.md")
	source := []byte("\xef\xbb\xbf**加粗** 尾段\r\n\r\nTail\r\n")
	if err := os.WriteFile(path, source, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	caret, ok := state.preview.blocks[0].view.CaretAt(len("加"))
	if !ok {
		t.Fatal("no caret after bold prefix")
	}
	if err := driver.Click(caret.X, caret.Y+caret.Height/2); err != nil {
		t.Fatal(err)
	}
	if got := byteOffsetOfRune(state.rich.input.String(), state.rich.input.Caret()); got != len("加") {
		t.Fatalf("bold prefix caret=%d", got)
	}
	if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if !state.selection.active || !state.selection.editable {
		t.Fatalf("bold prefix selection=%+v", state.selection)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'x'); err != nil {
		t.Fatal(err)
	}
	if got, want := driver.Clipboard(), "粗 尾段\nTail"; got != want {
		t.Fatalf("bold prefix cut copied %q, want %q", got, want)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	want := []byte("\xef\xbb\xbf**加**\r\n")
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, want) || state.mode != renderedMode || !state.rich.active {
		t.Fatalf("bold prefix cut=%q, want %q, mode=%v active=%v err=%v", data, want, state.mode, state.rich.active, err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, source) {
		t.Fatalf("bold prefix Undo=%q, err=%v", data, err)
	}
}

func TestRenderedCrossBlockPasteIntoSingleBoldLeafKeepsSuffix(t *testing.T) {
	state, driver := testDriver(t)
	source := "# Title\n\n**bold**\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	caret, ok := state.preview.blocks[1].view.CaretAt(len("bo"))
	if !ok {
		t.Fatal("no caret inside single bold block")
	}
	startX, startY := state.rich.rect.X+300, state.rich.rect.Y+15
	targetY := caret.Y + caret.Height/2
	if err := driver.Press(startX, startY, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := driver.Move(caret.X, targetY); err != nil {
		t.Fatal(err)
	}
	if err := driver.Release(caret.X, targetY, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if !state.selection.active || !state.selection.editable {
		t.Fatalf("single bold selection=%+v", state.selection)
	}
	driver.SetClipboard("X")
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	if got, want := state.session.Current().Text(), "# TitleX**ld**\n"; got != want || state.mode != renderedMode || !state.rich.active {
		t.Fatalf("single bold paste=%q mode=%v active=%v, want %q", got, state.mode, state.rich.active, want)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != source {
		t.Fatalf("single bold Undo=%q", got)
	}
}

func TestRenderedPartialBoldMergeStaysRenderedThroughHistory(t *testing.T) {
	state, driver := testDriver(t)
	source := "# Title\n\n**bold** tail\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	caret, ok := state.preview.blocks[1].view.CaretAt(len("bo"))
	if !ok {
		t.Fatal("no caret inside bold suffix")
	}
	startX, startY := state.rich.rect.X+300, state.rich.rect.Y+15
	targetY := caret.Y + caret.Height/2
	if err := driver.Press(startX, startY, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := driver.Move(caret.X, targetY); err != nil {
		t.Fatal(err)
	}
	if err := driver.Release(caret.X, targetY, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	driver.SetClipboard("X")
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# TitleX**ld** tail\n" || state.mode != renderedMode || !state.rich.active || state.rich.input == nil {
		t.Fatalf("partial bold merge=%q mode=%v active=%v", got, state.mode, state.rich.active)
	}
	if state.rich.input.String() != "TitleXld tail" || state.rich.input.Caret() != len("TitleX") {
		t.Fatalf("partial bold input=%q caret=%d", state.rich.input.String(), state.rich.input.Caret())
	}
	if err := driver.Type("Y"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# TitleXY**ld** tail\n" {
		t.Fatalf("typing after partial bold merge=%q", got)
	}
	for _, problem := range headless.CheckLayout(driver.Snapshot()) {
		if problem.Kind == headless.LayoutOverlap || problem.Kind == headless.LayoutSpill || problem.Kind == headless.LayoutCollide {
			t.Fatalf("active mixed heading layout=%+v", problem)
		}
	}
	for _, want := range []string{"# TitleX**ld** tail\n", source} {
		if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
			t.Fatal(err)
		}
		if got := state.session.Current().Text(); got != want || state.mode != renderedMode || !state.rich.active {
			t.Fatalf("partial bold Undo=%q mode=%v active=%v, want %q", got, state.mode, state.rich.active, want)
		}
	}
	for _, want := range []string{"# TitleX**ld** tail\n", "# TitleXY**ld** tail\n"} {
		if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
			t.Fatal(err)
		}
		if got := state.session.Current().Text(); got != want || state.mode != renderedMode || !state.rich.active {
			t.Fatalf("partial bold Redo=%q mode=%v active=%v, want %q", got, state.mode, state.rich.active, want)
		}
	}
	if err := driver.Type("Z"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# TitleXYZ**ld** tail\n" {
		t.Fatalf("partial bold redo caret or marker drift=%q", got)
	}
}

func TestRenderedCrossBlockFormattedPastePreservesBOMCRLFAndLink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "formatted.md")
	source := []byte("\xef\xbb\xbf# 标题\r\n\r\n**加粗** 后文保留\r\n\r\n[ref](https://example.invalid/keep)\r\n")
	if err := os.WriteFile(path, source, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	view := &state.preview.blocks[1].view
	selected := len("加粗 后")
	caret, ok := view.CaretAt(selected)
	if !ok {
		t.Fatal("no caret after selected plain suffix")
	}
	startX, startY := state.rich.rect.X+300, state.rich.rect.Y+15
	targetY := caret.Y + caret.Height/2
	if err := driver.Press(startX, startY, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := driver.Move(caret.X, targetY); err != nil {
		t.Fatal(err)
	}
	if err := driver.Release(caret.X, targetY, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if !state.selection.active || !state.selection.editable || state.selection.lastOffset != selected {
		t.Fatalf("formatted selection at plain boundary=%+v", state.selection)
	}
	driver.SetClipboard("续")
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	want := []byte("\xef\xbb\xbf# 标题续文保留\r\n\r\n[ref](https://example.invalid/keep)\r\n")
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, want) {
		t.Fatalf("formatted paste saved=%q, want %q, err=%v", data, want, err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, source) {
		t.Fatalf("formatted paste Undo saved=%q, err=%v", data, err)
	}
}

func TestRenderedCrossBlockCutWholeLinkPreservesFollowingSource(t *testing.T) {
	state, driver := testDriver(t)
	source := "# Title\n\n[链接](https://example.invalid/selected)\n\nTail [ref](https://example.invalid/keep)\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if !state.selection.editable {
		t.Fatalf("whole link selection cannot remove its complete source: %+v", state.selection)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'x'); err != nil {
		t.Fatal(err)
	}
	if got, want := driver.Clipboard(), "\n链接"; got != want {
		t.Fatalf("whole link cut copied %q, want %q", got, want)
	}
	if got, want := state.session.Current().Text(), "# Title\n\nTail [ref](https://example.invalid/keep)\n"; got != want {
		t.Fatalf("whole link cut=%q, want %q", got, want)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != source {
		t.Fatalf("whole link cut Undo=%q", got)
	}
}

func TestRenderedPointerDragHonorsPressedAnchor(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Title\n\nBody\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	startX, startY := state.rich.rect.X+2, state.rich.rect.Y+15
	targetX := state.rich.rect.X + 80
	targetY := state.previewRect.Y + 46 + state.preview.blocks[1].top + 12
	if err := driver.Press(startX, startY, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := driver.Move(targetX, targetY); err != nil {
		t.Fatal(err)
	}
	if err := driver.Release(targetX, targetY, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'c'); err != nil {
		t.Fatal(err)
	}
	if got := driver.Clipboard(); got != "Title\nBody" {
		t.Fatalf("drag from heading start copied %q", got)
	}
}

func TestRenderedClickDoesNotSplitEmojiGrapheme(t *testing.T) {
	engine := app.PlatformTextEngine()
	if engine == nil {
		t.Skip("platform text engine unavailable")
	}
	state := newModel()
	state.session.Current().Replace("# A👩‍💻B\n")
	canvas := &gfx.Canvas{}
	canvas.SetTextEngine(engine)
	driver, err := app.NewDriver(newConfig(state), canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	clickID(t, driver, renderedModeID)
	x, _ := state.markdownColumn(state.previewRect)
	value := "A👩‍💻B"
	before := canvas.TextCaret(value, 36, gfx.WeightBold, len("A"))
	after := canvas.TextCaret(value, 36, gfx.WeightBold, len("A👩‍💻"))
	y := state.previewRect.Y + 46 + state.preview.blocks[0].top + 18
	if err := driver.Click(x+(before+after)/2, y); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	got := state.session.Current().Text()
	if got != "# AX👩‍💻B\n" && got != "# A👩‍💻XB\n" {
		t.Fatalf("click split a grapheme cluster: %q", got)
	}
}

func TestRenderedEditableHeadingAfterSetextFallback(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("Setext\n======\n\n# Later\n")
	clickID(t, driver, renderedModeID)
	index := len(state.parsed.Blocks) - 1
	clickID(t, driver, renderedRows.At(index))
	if driver.Snapshot().Focused != richInputID {
		t.Fatalf("later heading could not be edited: %s", state.message)
	}
	if err := driver.Type("续"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "Setext\n======\n\n# Later续\n" {
		t.Fatalf("source after later heading edit = %q", got)
	}
}

func TestRenderedEditableHeadingAfterNestedList(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("- parent\n  - nested\n\n# Later\n")
	clickID(t, driver, renderedModeID)
	index := len(state.parsed.Blocks) - 1
	clickID(t, driver, renderedRows.At(index))
	if driver.Snapshot().Focused != richInputID {
		t.Fatalf("later heading could not be edited: %s", state.message)
	}
	if err := driver.Type("续"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "- parent\n  - nested\n\n# Later续\n" {
		t.Fatalf("source after nested list = %q", got)
	}
}

func TestRenderedEditUpdatesDirtyTabInSameFrame(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Heading\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Type("续"); err != nil {
		t.Fatal(err)
	}
	if !hasText(driver, "• 未命名-1.md") {
		t.Fatal("current rendered edit did not update the tab's dirty indicator")
	}
}

func TestRenderedUndoClearsDirtyTabInSameFrame(t *testing.T) {
	path := filepath.Join(t.TempDir(), "undo.md")
	if err := os.WriteFile(path, []byte("# Heading\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Type("续"); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Dirty() || hasText(driver, "• undo.md") {
		t.Fatalf("undo left a dirty tab: text=%q dirty=%v", state.session.Current().Text(), state.session.Current().Dirty())
	}
}

func TestRenderedCompositionCommitsOnlyAfterIMEFinishes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ime.md")
	if err := os.WriteFile(path, []byte("# 原文\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Compose("pin"); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != "# 原文\n" || state.session.Current().Dirty() {
		t.Fatalf("composition changed document: %q", state.session.Current().Text())
	}
	if err := driver.Compose("pinyin"); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != "# 原文\n" || state.session.Current().Dirty() {
		t.Fatalf("updated composition changed document: %q", state.session.Current().Text())
	}
	if err := driver.Type("拼音"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# 原文拼音\n" {
		t.Fatalf("IME commit = %q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# 原文\n" || state.session.Current().Dirty() {
		t.Fatalf("IME undo = %q dirty=%v", got, state.session.Current().Dirty())
	}
}

func TestRenderedSaveConflictRetainsEditedBuffer(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "conflict.md")
	if err := os.WriteFile(path, []byte("# Original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Type(" local"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# External\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state.saveActive(false)
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "# External\n" || state.session.Current().Text() != "# Original local\n" || !state.session.Current().Dirty() {
		t.Fatalf("conflict file=%q buffer=%q dirty=%v err=%v", data, state.session.Current().Text(), state.session.Current().Dirty(), err)
	}
}

func TestRenderedCloseSaveConflictKeepsPendingDraft(t *testing.T) {
	path := filepath.Join(t.TempDir(), "close-conflict.md")
	if err := os.WriteFile(path, []byte("# Original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Type(" local"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# External\n"), 0644); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, tabClose.At(state.session.Active))
	if state.session.Pending() == nil {
		t.Fatal("rendered dirty tab closed without confirmation")
	}
	clickID(t, driver, saveConfirmID)
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "# External\n" || state.session.Pending() == nil || state.session.Current().Text() != "# Original local\n" {
		t.Fatalf("failed close save file=%q pending=%v draft=%q err=%v", data, state.session.Pending(), state.session.Current().Text(), err)
	}
	clickID(t, driver, cancelConfirmID)
	if state.session.Pending() != nil || !state.session.Current().Dirty() {
		t.Fatal("cancel after conflict lost rendered draft")
	}
}
