package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"

	"markstack/internal/session"
	"markstack/internal/workspace"
)

func TestMarkdownCodeFontRemainsStableAcrossEditingModes(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("```go\nfirst\nsecond\n```\n\nTail\n")
	line := func() gfx.Text {
		t.Helper()
		for _, label := range driver.Snapshot().Texts {
			if label.Value == "first" {
				return label
			}
		}
		t.Fatal("first code line was not painted")
		return gfx.Text{}
	}
	clickID(t, driver, readModeID)
	reading := line()
	if family := gfx.MonoFamilyName(reading.Style.MonoFace); family != "Courier" {
		t.Errorf("reading code family = %q, want Courier", family)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	editing := line()
	if family := gfx.MonoFamilyName(editing.Style.MonoFace); family != "Courier" {
		t.Errorf("active code family = %q, want Courier", family)
	}
	if reading.Style.Size != editing.Style.Size || reading.CellWidth != editing.CellWidth {
		t.Errorf("focus changed code metrics: reading=%+v editing=%+v", reading, editing)
	}
	if err := driver.Click(editing.Bounds.X+editing.CellWidth*2+0.1, editing.Bounds.Y+editing.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "```go\nfiXrst\nsecond\n```\n\nTail\n" {
		t.Fatalf("classic code click used different metrics: %q", got)
	}
	clickID(t, driver, editModeID)
	foundSource := false
	for _, label := range driver.Snapshot().Texts {
		if label.Value == "fiXrst" {
			foundSource = true
			if label.Style.MonoFace != gfx.MonoSystem {
				t.Errorf("Markdown font leaked into Source editor: %+v", label.Style)
			}
		}
	}
	if !foundSource {
		t.Fatal("source editor did not paint the edited code")
	}
}

func TestRenderedFencedCodeEditsOnlyItsBody(t *testing.T) {
	state, driver := testDriver(t)
	source := "# Title\n\n```go\npackage main\n```\n\nTail\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	if state.mode != renderedMode || !state.codeBlock.active || driver.Snapshot().Focused != codeInputID {
		t.Fatalf("code block did not acquire native focus: mode=%v active=%v focused=%v", state.mode, state.codeBlock.active, driver.Snapshot().Focused)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	want := "# Title\n\n```go\npackage main新\n```\n\nTail\n"
	if got := state.session.Current().Text(); got != want || state.mode != renderedMode {
		t.Fatalf("code body edit = %q mode=%v", got, state.mode)
	}
}

func TestRenderedFencedCodeMultilineIMEPreservesBOMCRLF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "code.md")
	original := []byte("\xef\xbb\xbf# Head\r\n\r\n```go\r\none\r\ntwo\r\n```\r\n\r\nTail\r\n")
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	if !state.codeBlock.active || state.codeBlock.input.String() != "one\ntwo" {
		t.Fatalf("CRLF code body = %+v", state.codeBlock)
	}
	before := state.session.Current().Text()
	if err := driver.Compose("ni"); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != before || state.session.Current().Dirty() {
		t.Fatal("code IME preedit changed source")
	}
	if err := driver.Type("你"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	want := []byte("\xef\xbb\xbf# Head\r\n\r\n```go\r\none\r\ntwo你\r\n新\r\n```\r\n\r\nTail\r\n")
	if state.mode != renderedMode || state.codeBlock.input.String() != "one\ntwo你\n新" {
		t.Fatalf("multiline code editor mode=%v body=%q", state.mode, state.codeBlock.input.String())
	}
	if err := state.session.Save(state.session.Active, ""); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, want) {
		t.Fatalf("multiline code save = %q, %v", saved, err)
	}
	clickID(t, driver, editModeID)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	if !state.codeBlock.active || state.codeBlock.input.String() != "one\ntwo你\n新" {
		t.Fatalf("code editor round-trip = %+v", state.codeBlock)
	}
}

func TestRenderedFencedCodeUndoRedoUsesDocumentHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "code.md")
	source := []byte("```go\nold\n```\n")
	if err := os.WriteFile(path, source, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != string(source) || state.session.Current().Dirty() || !state.codeBlock.active || state.codeBlock.input.String() != "old" {
		t.Fatalf("first code Undo = %q", state.session.Current().Text())
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != string(source) {
		t.Fatalf("second code Undo changed clean source: %q", state.session.Current().Text())
	}
	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != "```go\nold新\n```\n" || !state.codeBlock.active {
		t.Fatalf("code Redo = %q", state.session.Current().Text())
	}
}

func TestRenderedFencedCodeTargetsSecondDuplicateAndFallsBackForUnsafeFence(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("~~~go\nsame\n~~~\n\n~~~go\nsame\n~~~\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	if !state.codeBlock.active || state.codeBlock.renderedIndex != 1 {
		t.Fatal("second identical code block did not activate")
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "~~~go\nsame\n~~~\n\n~~~go\nsame新\n~~~\n" {
		t.Fatalf("duplicate code edit = %q", got)
	}
	state.session.Current().Replace("```go\nbody\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if state.mode != editMode || state.codeBlock.active || state.session.Current().Text() != "```go\nbody\n" {
		t.Fatal("unclosed code did not fall back to Source")
	}
}

func TestRenderedFencedCodeClosingLineKeepsInputInSource(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("```go\nbody\n```\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Type("\n```\nnext"); err != nil {
		t.Fatal(err)
	}
	if state.mode != editMode || state.codeBlock.active || state.session.Current().Text() != "```go\nbody\n```\nnext\n```\n" {
		t.Fatalf("unsafe closing fence lost text or stayed rendered: %q mode=%v", state.session.Current().Text(), state.mode)
	}
}

func TestRenderedFencedCodePointerPlacesCaretWithoutDuplicatePaint(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("```go\nfirst\nsecond\n```\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	var first ui.Rect
	count := 0
	for _, label := range driver.Snapshot().Texts {
		if label.Value == "first" {
			first = label.Bounds
			count++
		}
	}
	if count != 1 || first.Width <= 0 {
		t.Fatalf("active code line painted %d times, bounds=%+v", count, first)
	}
	if err := driver.Click(first.X+1, first.Y+first.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "```go\nXfirst\nsecond\n```\n" {
		t.Fatalf("code pointer edit = %q", got)
	}
}

func TestRenderedFencedCodeSelectionAndResponsiveLayout(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Title\n\n```go\nfirst\nsecond\n```\n\nTail\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	if err := driver.SelectText(0, 5); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "# Title\n\n```go\n新\nsecond\n```\n\nTail\n" {
		t.Fatalf("code selection edit = %q", got)
	}
	for _, viewport := range []struct {
		width, height int
		dark          bool
	}{{1200, 780, false}, {740, 520, false}, {1200, 780, true}, {740, 520, true}} {
		state.dark = viewport.dark
		if err := driver.Resize(viewport.width, viewport.height); err != nil {
			t.Fatal(err)
		}
		for _, problem := range headless.CheckLayout(driver.Snapshot()) {
			if problem.Kind == headless.LayoutOverlap || problem.Kind == headless.LayoutSpill {
				t.Fatalf("code viewport=%+v layout=%+v", viewport, problem)
			}
		}
	}
}

func TestRenderedFencedCodeExternalChangeKeepsDraftAndDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "code.md")
	original := []byte("```go\nold\n```\n")
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	external := []byte("```go\noutside\n```\n")
	if err := os.WriteFile(path, external, 0644); err != nil {
		t.Fatal(err)
	}
	if err := state.session.Save(state.session.Active, ""); err != workspace.ErrConflict || !state.session.Current().Dirty() || state.session.Current().Text() != "```go\nold新\n```\n" {
		t.Fatalf("external conflict lost code draft: %v, %q", err, state.session.Current().Text())
	}
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, external) {
		t.Fatalf("external bytes overwritten: %q, %v", saved, err)
	}
}

func TestRenderedFencedCodeEmptyBlockStartsNativeEditing(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("~~~go\n~~~\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if !state.codeBlock.active || state.codeBlock.input.String() != "" || driver.Snapshot().Focused != codeInputID {
		t.Fatal("empty fenced code did not focus native input")
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "~~~go\n新\n~~~\n" || state.mode != renderedMode {
		t.Fatalf("empty code input = %q, mode=%v", got, state.mode)
	}
}

func TestRenderedFencedCodeStructuralUndoFallsBackWithoutPanicking(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Title\n")
	current := state.session.Current()
	if err := current.EditRange(len(current.Text()), len(current.Text()), "\n```go\nold\n```\n"); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if state.mode != editMode || state.codeBlock.active || state.session.Current().Text() != "# Title\n" {
		t.Fatalf("structural Undo state=%v source=%q", state.mode, state.session.Current().Text())
	}
}

func TestRenderedFencedCodeReservesHeightBeforeFollowingParagraph(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("```go\nshort\n```\n\nTail\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if len(state.preview.blocks) != 2 || !state.codeBlock.active {
		t.Fatalf("code layout blocks=%d active=%v", len(state.preview.blocks), state.codeBlock.active)
	}
	code := state.preview.blocks[0]
	tail := state.preview.blocks[1]
	if tail.top < code.top+max(float32(64), code.height)+8 {
		t.Fatalf("following paragraph top=%v overlaps active code bottom=%v", tail.top, code.top+max(float32(64), code.height))
	}
	for _, problem := range headless.CheckLayout(driver.Snapshot()) {
		if problem.Kind == headless.LayoutOverlap || problem.Kind == headless.LayoutSpill {
			t.Fatalf("active code overlaps following text: %+v", problem)
		}
	}
}

func TestRenderedFencedCodeTabIndentsWithoutLeavingInput(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("```go\nline\n```\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Key(gfx.KeyTab, 0); err != nil {
		t.Fatal(err)
	}
	if state.mode != renderedMode || !state.codeBlock.active || driver.Snapshot().Focused != codeInputID || state.session.Current().Text() != "```go\nline\t\n```\n" {
		t.Fatalf("Tab left code input or missed indentation: %q, mode=%v", state.session.Current().Text(), state.mode)
	}
}

func TestRenderedFencedCodeShiftTabRemovesCurrentLineIndent(t *testing.T) {
	for _, test := range []struct{ indent string }{{"\t"}, {"    "}} {
		state, driver := testDriver(t)
		state.session.Current().Replace("```go\n" + test.indent + "line\n```\n")
		clickID(t, driver, renderedModeID)
		clickID(t, driver, renderedRows.At(0))
		if err := driver.Key(gfx.KeyTab, gfx.ModifierShift); err != nil {
			t.Fatal(err)
		}
		if got := state.session.Current().Text(); got != "```go\nline\n```\n" || state.mode != renderedMode || driver.Snapshot().Focused != codeInputID {
			t.Fatalf("Shift+Tab indent %q = %q mode=%v", test.indent, got, state.mode)
		}
	}
}

func TestRenderedFencedCodeSelectedLinesIndentAndOutdentWithoutLosingText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "code.md")
	source := []byte("```go\none\ntwo\n```\n")
	if err := os.WriteFile(path, source, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.SelectText(1, 7); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyTab, 0); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "```go\n\tone\n\ttwo\n```\n" {
		t.Fatalf("Tab replaced selected code: %q", got)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != string(source) || state.session.Current().Dirty() {
		t.Fatalf("selected Tab undo = %q dirty=%v", got, state.session.Current().Dirty())
	}
	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "```go\n\tone\n\ttwo\n```\n" {
		t.Fatalf("selected Tab redo = %q", got)
	}
	if err := driver.SelectText(2, 9); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyTab, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != string(source) || state.session.Current().Dirty() {
		t.Fatalf("Shift+Tab failed to restore selected lines: %q dirty=%v", got, state.session.Current().Dirty())
	}
}

func TestRenderedFencedCodeSelectionEndingAtNextLineSkipsThatLine(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("```go\n甲\n乙\n丙\n```\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.SelectText(0, 2); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyTab, 0); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "```go\n\t甲\n乙\n丙\n```\n" {
		t.Fatalf("selected newline indented next line: %q", got)
	}
	if err := driver.Key(gfx.KeyTab, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "```go\n甲\n乙\n丙\n```\n" {
		t.Fatalf("Unicode outdent = %q", got)
	}
}

func TestRenderedFencedCodeSelectedIndentPreservesBOMCRLF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "code.md")
	original := []byte("\xef\xbb\xbf```go\r\none\r\ntwo\r\n```\r\n")
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.SelectText(1, 7); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyTab, 0); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "```go\r\n\tone\r\n\ttwo\r\n```\r\n" {
		t.Fatalf("CRLF selection indentation = %q", got)
	}
	if err := driver.Key(gfx.KeyTab, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Dirty() {
		t.Fatal("CRLF indent round-trip remained dirty")
	}
	if err := state.session.Save(state.session.Active, ""); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, original) {
		t.Fatalf("CRLF indent round-trip changed disk: %q, %v", saved, err)
	}
}

func TestRenderedFencedCodeSwitchingToTableClearsItsInput(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("```go\nold\n```\n\n| A | B |\n| --- | --- |\n| one | two |\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if !state.codeBlock.active {
		t.Fatal("code block was not active before switching")
	}
	clickID(t, driver, renderedTableCells.At(0))
	if state.codeBlock.active || !state.table.active || driver.Snapshot().Focused != tableInputID {
		t.Fatalf("code and table input overlapped: code=%v table=%v", state.codeBlock.active, state.table.active)
	}
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != "```go\nold\n```\n\n| AX | B |\n| --- | --- |\n| one | two |\n" {
		t.Fatalf("table focus changed code source: %q", got)
	}
}

func TestRenderedFencedCodeCompositionConsumesTabWithoutWritingSource(t *testing.T) {
	state, driver := testDriver(t)
	source := "```go\nold\n```\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Compose("ni"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyTab, 0); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != source || driver.Snapshot().Focused != codeInputID {
		t.Fatal("Tab committed a code composition or moved focus")
	}
	if err := driver.Type("你"); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != "```go\nold你\n```\n" {
		t.Fatalf("code composition commit = %q", state.session.Current().Text())
	}
}

func TestRenderedFencedCodePendingIMEBlocksSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "code.md")
	original := []byte("```go\nold\n```\n")
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Compose("ni"); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if !state.documentComposing(state.session.Current()) || state.message != session.ErrCompositionPending.Error() || !state.session.Current().Dirty() {
		t.Fatalf("pending code IME was saved: message=%q dirty=%v", state.message, state.session.Current().Dirty())
	}
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, original) {
		t.Fatalf("pending code IME changed disk: %q, %v", saved, err)
	}
	if err := driver.Type("你"); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || string(saved) != "```go\noldX你\n```\n" {
		t.Fatalf("confirmed code IME did not save: %q, %v", saved, err)
	}
}

func TestRenderedFencedCodeActiveKeepsGoSyntaxColor(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("```go\npackage main\n```\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	snapshot := driver.Snapshot()
	for _, label := range snapshot.Texts {
		if label.Value != "package main" {
			continue
		}
		for _, run := range snapshot.TextRuns(label) {
			if run.Color == ui.RGB(166, 38, 164) {
				return
			}
		}
	}
	t.Fatal("active Go code lost its keyword color")
}

func TestRenderedFencedCodeGrowsForNewLineWithoutClippingFirstLine(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("```go\nfirst\nsecond\n```\n\nTail\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("third"); err != nil {
		t.Fatal(err)
	}
	if state.codeBlock.view.ScrollY() != 0 {
		t.Fatalf("active code scrolled first line out: scroll=%v", state.codeBlock.view.ScrollY())
	}
	firstVisible := false
	for _, label := range driver.Snapshot().Texts {
		firstVisible = firstVisible || label.Value == "first"
	}
	if !firstVisible || state.preview.blocks[1].top < state.preview.blocks[0].top+state.preview.blocks[0].height+8 {
		t.Fatalf("new code line clipped or covered Tail: visible=%v blocks=%+v", firstVisible, state.preview.blocks)
	}
}

func TestRenderedFencedCodeActiveSurfaceKeepsReadingPalette(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("```go\nfirst\n```\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	bounds := state.previewRect
	left, width := state.markdownColumn(bounds)
	top := bounds.Y + 46 - state.previewScroll + state.preview.blocks[0].top
	appearance := referenceMarkdownColors(state.dark)
	border, fill := false, false
	for _, primitive := range driver.Snapshot().Primitives {
		if primitive.Kind != app.RectanglePrimitive || primitive.Bounds.X < left-1 || primitive.Bounds.X > left+1 || primitive.Bounds.Y < top-1 || primitive.Bounds.Y > top+1 || primitive.Bounds.Width < width-2 || primitive.Bounds.Height < state.preview.blocks[0].height-2 {
			continue
		}
		border = border || primitive.Color == appearance.codeBorder
		fill = fill || primitive.Color == appearance.codeFill
	}
	if !border || !fill {
		t.Fatalf("active code surface uses reader border=%v fill=%v", border, fill)
	}
}

func TestRenderedFencedCodeInsertionThenBodyTypingStaysRendered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inserted-code.md")
	source := "# Title\n\nBody\n\nTail\n"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	clickID(t, driver, headingMenuID)
	clickID(t, driver, blockFormats.At(4))
	inserted := "# Title\n\n```\nBody\n```\n\nTail\n"
	if state.mode != renderedMode || state.session.Current().Text() != inserted {
		t.Fatalf("fence insertion mode=%v source=%q", state.mode, state.session.Current().Text())
	}
	if !state.codeBlock.active || driver.Snapshot().Focused != codeInputID {
		t.Fatalf("inserted fence did not focus native input: active=%v focus=%v", state.codeBlock.active, driver.Snapshot().Focused)
	}
	if err := driver.Key(gfx.KeyEnd, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyTab, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("next"); err != nil {
		t.Fatal(err)
	}
	want := "# Title\n\n```\nBody新\n\tnext\n```\n\nTail\n"
	if got := state.session.Current().Text(); got != want || state.mode != renderedMode {
		t.Fatalf("typing after fence insertion mode=%v source=%q, want %q", state.mode, got, want)
	}
	undo := func(target string) {
		t.Helper()
		for steps := 1; steps <= 8; steps++ {
			if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
				t.Fatal(err)
			}
			if state.session.Current().Text() == target {
				return
			}
		}
		t.Fatalf("Undo did not reach %q: %q", target, state.session.Current().Text())
	}
	redo := func(target string) {
		t.Helper()
		for steps := 1; steps <= 8; steps++ {
			if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
				t.Fatal(err)
			}
			if state.session.Current().Text() == target {
				return
			}
		}
		t.Fatalf("Redo did not reach %q: %q", target, state.session.Current().Text())
	}
	undo(inserted)
	if !state.codeBlock.active {
		t.Fatal("Undo dropped the inserted fence's native input")
	}
	redo(want)
	if got := state.session.Current().Text(); got != want || !state.codeBlock.active {
		t.Fatalf("inserted fence Redo=%q active=%v", got, state.codeBlock.active)
	}
}

func TestRenderedFencedCodeVisualNavigationUsesNativeRows(test *testing.T) {
	for _, scenario := range []struct {
		name      string
		key       gfx.Key
		modifiers gfx.Modifiers
		from      int
		start     int
		end       int
	}{
		{"home", gfx.KeyHome, 0, 7, 4, 4},
		{"end", gfx.KeyEnd, 0, 0, 3, 3},
		{"command-left", gfx.KeyLeft, gfx.ModifierMeta, 6, 4, 4},
		{"command-right", gfx.KeyRight, gfx.ModifierMeta, 1, 3, 3},
		{"up", gfx.KeyUp, 0, 6, 2, 2},
		{"down", gfx.KeyDown, 0, 2, 6, 6},
		{"control-home", gfx.KeyHome, gfx.ModifierControl, 6, 0, 0},
		{"control-end", gfx.KeyEnd, gfx.ModifierControl, 1, 7, 7},
		{"shift-home", gfx.KeyHome, gfx.ModifierShift, 7, 4, 7},
		{"shift-end", gfx.KeyEnd, gfx.ModifierShift, 0, 0, 3},
	} {
		test.Run(scenario.name, func(context *testing.T) {
			state, driver, filename, original := renderedSourceCollisionFixture(context, "```go\nabc\nxyz\n```\n\nTail\n", false, 1200)
			clickID(context, driver, renderedRows.At(0))
			state.codeBlock.input.MoveTo(scenario.from, false)
			if err := driver.Resize(1200, 780); err != nil {
				context.Fatal(err)
			}
			if err := driver.Key(scenario.key, scenario.modifiers); err != nil {
				context.Fatal(err)
			}
			selection := state.codeBlock.input.Selection()
			if selection.Start != scenario.start || selection.End != scenario.end || driver.Snapshot().Focused != codeInputID {
				context.Fatalf("code navigation selection=%+v focus=%v, want %d..%d", selection, driver.Snapshot().Focused, scenario.start, scenario.end)
			}
			expectRenderedCollisionSave(context, state, driver, filename, original)
		})
	}
}

func TestRenderedFencedCodeVisualNavigationLeavesCompositionToIME(test *testing.T) {
	state, driver := testDriver(test)
	state.session.Current().Replace("```go\nbody\n```\n")
	clickID(test, driver, renderedModeID)
	clickID(test, driver, renderedRows.At(0))
	if err := driver.Compose("ni"); err != nil {
		test.Fatal(err)
	}
	input := state.codeBlock.input
	before, selection, source := input.String(), input.Selection(), state.session.Current().Text()
	result := state.onKey(gfx.KeyEvent{Target: codeInputID, Key: gfx.KeyHome})
	if result.Handled || input.String() != before || input.Selection() != selection || !state.codeBlockComposing() || state.session.Current().Text() != source {
		test.Fatal("application code navigation intercepted an active native composition")
	}
}
