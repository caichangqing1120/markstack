package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"markstack/internal/session"
	"markstack/internal/workspace"
)

func TestM11KeyboardZoomScalesRenderedMarkdown(t *testing.T) {
	state, driver := testDriver(t)
	const source = "# Title\r\n\r\nBody text\r\n\r\n```go\r\nprintln(1)\r\n```\r\n"
	current := state.session.Current()
	current.Replace(source)
	clickID(t, driver, renderedModeID)
	for size := 16; size <= 32; size += 2 {
		if got := state.readingSize(); got != float32(size) {
			t.Fatalf("rendered size = %v, want %d", got, size)
		}
		for _, sample := range []struct {
			block int
			text  string
			size  float32
		}{{0, "Title", 36 * float32(size) / 16}, {1, "Body text", float32(size)}, {2, "println", float32(size) - 1}} {
			found := false
			for _, run := range typographyBlockTexts(driver.Snapshot(), state, sample.block) {
				if strings.Contains(run.Value, sample.text) && typographyNear(run.Style.Size, sample.size) {
					found = true
				}
			}
			if !found {
				t.Fatalf("size %d: no %q run at %v", size, sample.text, sample.size)
			}
		}
		if handled, err := driver.ShortcutChord(primaryModifier(), '='); err != nil || !handled {
			t.Fatalf("zoom shortcut: handled=%v err=%v", handled, err)
		}
	}
	if state.preferences.ReadingSize != 32 || state.preferences.SourceSize != 16 || current.Text() != source {
		t.Fatalf("zoom changed source or crossed maximum: %+v, %q", state.preferences, current.Text())
	}
	for size := 30; size >= 10; size -= 2 {
		m1Shortcut(t, driver, primaryModifier(), '-')
		if got := state.readingSize(); got != float32(max(12, size)) {
			t.Fatalf("decreased size = %v, want %d", got, max(12, size))
		}
	}
	m1Shortcut(t, driver, primaryModifier()|gfx.ModifierShift, '+')
	if state.readingSize() != 14 {
		t.Fatal("shifted plus did not zoom")
	}
	m1Shortcut(t, driver, primaryModifier()|gfx.ModifierAlt, '0')
	if state.readingSize() != 16 {
		t.Fatal("reset did not restore default size")
	}
}

func TestM11StatusShowsCursorAndDirtyStateAlongsideMessage(t *testing.T) {
	state, driver := testDriver(t)
	state.preferences.Language = "en"
	current := state.session.Current()
	current.Replace("First\r\n中 ab\rLast")
	current.Editor.MoveTo(9, false)
	state.message = "Document warning"
	state.invalidator.Invalidate()
	if err := driver.Pump(); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{current.Title, "Unsaved", "Document warning", "Ln 3", "L2:C3", "UTF-8"} {
		if !hasText(driver, label) {
			t.Errorf("status missing %q", label)
		}
	}
	clickID(t, driver, statusModeID)
	if state.mode != readMode {
		t.Fatalf("status click did not advance to reading: %v", state.mode)
	}
}

func TestM11LayoutPreferencesApplyToPreviewAndActiveEditor(t *testing.T) {
	state, driver := testDriver(t)
	store := session.PreferencesStore{Path: filepath.Join(t.TempDir(), "private", "settings.json")}
	if err := state.enablePreferences(store); err != nil {
		t.Fatal(err)
	}
	current := state.session.Current()
	const source = "Intro paragraph\n\n1. List text\n\n```go\nprintln(1)\n```"
	current.Replace(source)
	if err := driver.Resize(1800, 980); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	standard := state.preview.layoutKey.width
	clickID(t, driver, settingsToggleID)
	clickID(t, driver, settingsWritingID)
	clickID(t, driver, lineSpacingRelaxedID)
	scrollSettings(t, driver, 100)
	clickID(t, driver, editorWidthWideID)
	clickID(t, driver, settingsCloseID)
	if got, err := store.Load(); err != nil || got.LineSpacing != "relaxed" || got.EditorWidth != "wide" {
		t.Fatalf("layout settings not persisted: %+v, %v", got, err)
	}
	wide := state.preview.layoutKey.width
	if wide <= standard || state.preview.blocks[0].height != 16*float32(1.85) {
		t.Fatalf("relaxed, wide preview geometry: width %v (standard %v), height %v", wide, standard, state.preview.blocks[0].height)
	}
	for _, entry := range []struct {
		size           int
		spacing, theme string
		ratio          float32
	}{{12, "compact", "academic", 1.45}, {21, "standard", "academic", 29.0 / 16}, {32, "relaxed", "standard", 1.85}} {
		next := state.preferences
		next.ReadingSize, next.LineSpacing, next.ReadingTheme, next.EditorWidth = entry.size, entry.spacing, entry.theme, "full"
		state.changeSettings(next)
		if err := driver.Pump(); err != nil {
			t.Fatal(err)
		}
		wantHeight := float32(entry.size) * entry.ratio
		if state.preview.blocks[0].width != state.previewRect.Width-64 || !typographyNear(state.preview.blocks[0].height, wantHeight) {
			t.Fatalf("%+v: preview layout=%+v height=%v", entry, state.preview.layoutKey, state.preview.blocks[0].height)
		}
		listX := float32(0)
		for _, run := range typographyBlockTexts(driver.Snapshot(), state, 1) {
			if run.Value == "List text" {
				listX = run.Bounds.X
			}
		}
		clickID(t, driver, renderedRows.At(1))
		if !state.rich.active || state.rich.wrapSize != float32(entry.size) || !typographyNear(state.rich.wrapLineHeight, wantHeight) || !typographyNear(state.rich.rect.X, listX) {
			t.Fatalf("%+v: active list x=%v (preview %v) size=%v line=%v", entry, state.rich.rect.X, listX, state.rich.wrapSize, state.rich.wrapLineHeight)
		}
		marker := false
		for _, run := range typographyBlockTexts(driver.Snapshot(), state, 1) {
			marker = marker || run.Value == "1." && run.Style.Size == float32(entry.size)
		}
		if !marker {
			t.Fatalf("ordered marker did not scale to %d", entry.size)
		}
		clickID(t, driver, renderedRows.At(2))
		fencedSize := false
		for _, run := range typographyBlockTexts(driver.Snapshot(), state, 2) {
			fencedSize = fencedSize || run.Style.Mono && run.Style.Size == float32(entry.size-1)
		}
		if !state.codeBlock.active || !fencedSize {
			t.Fatalf("fenced editor did not scale at %d", entry.size)
		}
	}
	full := state.previewRect.Width - 64
	clickID(t, driver, editModeID)
	if state.sourceRect.Width != full || state.sourceRect.Width <= wide {
		t.Fatalf("full source width = %v, want %v (wide preview = %v)", state.sourceRect.Width, full, wide)
	}
	if current.Text() != source {
		t.Fatal("layout preferences rewrote Markdown")
	}
}

func TestM11RenderedStatusUsesDocumentCoordinates(t *testing.T) {
	state, driver := testDriver(t)
	const source = "First\r\n\r\n**中 ab**\r\n\r\n```go\r\nline one\r\n中 second\r\n```\r\n\r\n| A | B |\r\n| --- | --- |\r\n| 中 one | two |\r\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	if err := driver.SelectText(2, 2); err != nil || !hasText(driver, "L3:C5") {
		t.Fatalf("styled paragraph caret should include Markdown delimiters: %v", err)
	}
	clickID(t, driver, renderedRows.At(2))
	if err := driver.SelectText(11, 11); err != nil || !hasText(driver, "L7:C3") {
		t.Fatalf("fenced caret should count CRLF source lines: %v", err)
	}
	clickID(t, driver, renderedTableCells.At(2))
	if err := driver.SelectText(1, 1); err != nil || !hasText(driver, "L12:C4") {
		t.Fatalf("table caret should include the cell's document position: %v", err)
	}
	if state.session.Current().Text() != source {
		t.Fatal("caret mapping modified source")
	}
}

func TestM11TabShortcutsKeepPrimaryNumbersAvailable(t *testing.T) {
	state, driver := testDriver(t)
	clickID(t, driver, newFileID)
	clickID(t, driver, newFileID)
	if len(state.session.Tabs) != 3 {
		t.Fatal("fixture did not open three tabs")
	}
	m1Shortcut(t, driver, gfx.ModifierAlt, '1')
	if state.session.Active != 0 {
		t.Fatal("Alt+1 did not activate the first tab")
	}
	m1Shortcut(t, driver, primaryModifier()|gfx.ModifierShift, ']')
	if state.session.Active != 1 {
		t.Fatal("Primary+Shift+] did not activate the next tab")
	}
	if err := driver.Key(gfx.KeyTab, gfx.ModifierControl); err != nil || state.session.Active != 2 {
		t.Fatalf("Ctrl+Tab active=%d, err=%v", state.session.Active, err)
	}
	if err := driver.Key(gfx.KeyTab, gfx.ModifierControl|gfx.ModifierShift); err != nil || state.session.Active != 1 {
		t.Fatalf("Ctrl+Shift+Tab active=%d, err=%v", state.session.Active, err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), '1'); err != nil || state.session.Active != 1 {
		t.Fatalf("Primary+1 still switches tabs: active=%d err=%v", state.session.Active, err)
	}
}

func TestM11NarrowZoomKeepsDeepListTextInColumn(t *testing.T) {
	state, driver := testDriver(t)
	state.preferences.ReadingSize = 32
	var source strings.Builder
	for depth := range 12 {
		source.WriteString(strings.Repeat("  ", depth) + "- list item\n")
	}
	state.session.Current().Replace(source.String())
	if err := driver.Resize(740, 960); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	deepest := len(state.preview.blocks) - 1
	if deepest < 10 {
		t.Fatal("fixture did not produce the nested items")
	}
	clickID(t, driver, renderedRows.At(deepest))
	if !state.rich.active || state.rich.index != deepest || state.rich.rect.Width < 64 || state.rich.wrapSize != 32 {
		t.Fatalf("nested list lost its visible editor: %+v", state.rich.rect)
	}
}

func TestM11ToolbarAndWheelZoomUseActiveMode(t *testing.T) {
	state, driver := testDriver(t)
	store := session.PreferencesStore{Path: filepath.Join(t.TempDir(), "private", "settings.json")}
	if err := state.enablePreferences(store); err != nil {
		t.Fatal(err)
	}
	state.session.Current().Replace("Body text")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	clickID(t, driver, topFontIncreaseID)
	if state.readingSize() != 18 || state.rich.wrapSize != 18 || driver.Snapshot().Focused != richInputID || !hasText(driver, "Aa 18") {
		t.Fatal("top bar did not resize the active editor while preserving focus")
	}
	clickID(t, driver, topFontDecreaseID)
	clickID(t, driver, topFontDecreaseID)
	clickID(t, driver, topFontResetID)
	if state.readingSize() != 16 {
		t.Fatal("the reset button did not reset reading size")
	}
	viewport := state.previewRect
	wheel := gfx.ScrollEvent{X: viewport.X + 5, Y: viewport.Y + 5, Modifiers: primaryModifier(), DeltaY: -12, Precise: true}
	for _, want := range []float32{16, 16, 18} {
		if err := driver.Scroll(wheel); err != nil || state.readingSize() != want {
			t.Fatalf("precise zoom reading=%v want=%v, err=%v", state.readingSize(), want, err)
		}
	}
	wheel.DeltaY = -200
	if result := state.onScroll(wheel); !result.Handled || state.readingSize() != 18 {
		t.Fatalf("a burst of precise events skipped a font step: %v, %+v", state.readingSize(), result)
	}
	wheel.Phase, wheel.DeltaY = gfx.ScrollPhaseMomentum, -200
	if result := state.onScroll(wheel); !result.Handled || state.readingSize() != 18 {
		t.Fatalf("momentum zoomed: %v, %+v", state.readingSize(), result)
	}
	clickID(t, driver, topExportMenuID)
	wheel.Phase, wheel.Precise = gfx.ScrollPhaseWheel, false
	if err := driver.Scroll(wheel); err != nil || state.readingSize() != 18 {
		t.Fatalf("wheel zoomed behind menu: %v, %v", state.readingSize(), err)
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil || state.fileMenuOpen || driver.Snapshot().Focused != topExportMenuID {
		t.Fatalf("export dismissal did not restore trigger focus: %v", err)
	}
	clickID(t, driver, editModeID)
	viewport = state.sourceRect
	wheel.X, wheel.Y, wheel.DeltaY = viewport.X+10, viewport.Y+10, 45
	if err := driver.Scroll(wheel); err != nil || state.sourceSize() != 14 || state.readingSize() != 18 {
		t.Fatalf("wheel did not isolate source zoom: prefs=%+v err=%v", state.preferences, err)
	}
	if got, err := store.Load(); err != nil || got != state.preferences {
		t.Fatalf("zoom not persisted: %+v, %v", got, err)
	}
	clickID(t, driver, splitModeID)
	clickID(t, driver, topFontIncreaseID)
	clickID(t, driver, readModeID)
	clickID(t, driver, topFontIncreaseID)
	if state.sourceSize() != 14 || state.readingSize() != 22 {
		t.Fatalf("split/read zoom changed the wrong size: %+v", state.preferences)
	}
}

func TestM11QuickActionsProtectMarkedText(t *testing.T) {
	state, driver := compositionDriver(t, uncommittedCompositionSource)
	clickID(t, driver, renderedRows.At(1))
	if err := driver.Compose("ni"); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, topFontIncreaseID)
	preview := state.previewRect
	if err := driver.Scroll(gfx.ScrollEvent{X: preview.X + 10, Y: preview.Y + 10, DeltaY: -120, Modifiers: primaryModifier()}); err != nil {
		t.Fatal(err)
	}
	if state.readingSize() != 16 {
		t.Fatal("zoom changed text geometry during composition")
	}
	clickID(t, driver, topSaveID)
	requireMarked(t, state, driver, "rich", richInputID, uncommittedCompositionSource)
	clickID(t, driver, topExportMenuID)
	clickID(t, driver, topExportHTMLID)
	if state.fileMenuOpen {
		t.Fatal("refused export did not dismiss its menu")
	}
	if _, requested := driver.LastSaveFileRequest(); requested {
		t.Fatal("marked text reached a picker")
	}
	requireMarked(t, state, driver, "rich", richInputID, uncommittedCompositionSource)
}

func TestM11ReadOnlyPreviewDisablesWritingActions(t *testing.T) {
	state, driver := testDriver(t)
	path := m5File(t, t.TempDir(), "large.md", strings.Repeat("Partial document\n", 5000))
	if err := os.Truncate(path, workspace.MaxDocumentSize+128); err != nil {
		t.Fatal(err)
	}
	m5OpenFile(t, driver, path)
	for _, id := range []gfx.ID{topSaveID, topSaveAsID, topExportMenuID} {
		found := false
		for _, hit := range driver.Snapshot().Hits {
			if hit.ID == id {
				found = hit.Disabled
			}
		}
		if !found {
			t.Errorf("readonly file has an enabled writing action: %v", id)
		}
	}
	if !state.session.Current().ReadOnly() || !hasText(driver, "局部预览 · UTF-8") || m3HasHit(driver, statusModeID) {
		t.Fatal("readonly status does not reflect the partial preview")
	}
}

func TestM11TopBarActionsSaveAndExportWithoutChangingSource(t *testing.T) {
	requirePDFFont(t)
	state, driver := testDriver(t)
	root := t.TempDir()
	store := session.PreferencesStore{Path: filepath.Join(root, "private", "settings.json")}
	if err := state.enablePreferences(store); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "note.md")
	original := []byte("\xef\xbb\xbf# Heading\r\n\r\nBody text\r\n")
	if err := os.WriteFile(path, original, 0600); err != nil || !state.openFile(path) {
		t.Fatalf("opening fixture: %v %s", err, state.message)
	}
	clickID(t, driver, topAutoSaveID)
	if loaded, err := store.Load(); err != nil || !loaded.AutoSave {
		t.Fatalf("quick auto-save switch didn't persist: %+v %v", loaded, err)
	}
	clickID(t, driver, topAutoSaveID)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	if err := driver.Type(" X"); err != nil {
		t.Fatal(err)
	}
	current := state.session.Current()
	expected := []byte("\xef\xbb\xbf" + current.Text())
	if !current.Dirty() || !hasText(driver, "未保存") {
		t.Fatal("dirty status missing before quick Save")
	}
	clickID(t, driver, topSaveID)
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, expected) || current.Dirty() || !hasText(driver, "已保存") || driver.Snapshot().Focused != richInputID {
		t.Fatalf("quick Save: got=%q, want=%q, err=%v, focused=%v", got, expected, err, driver.Snapshot().Focused)
	}
	copyPath := filepath.Join(root, "copy.md")
	if err := driver.QueueSaveFile(copyPath, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, topSaveAsID)
	if got, err := os.ReadFile(copyPath); err != nil || !bytes.Equal(got, expected) {
		t.Fatalf("quick Save as: %q, %v", got, err)
	}
	for _, format := range []struct {
		id                gfx.ID
		extension, prefix string
	}{{topExportPDFID, ".pdf", "%PDF-"}, {topExportDOCXID, ".docx", "PK\x03\x04"}, {topExportHTMLID, ".html", "<!doctype html>"}} {
		target := filepath.Join(root, "output"+format.extension)
		if err := driver.QueueSaveFile(target, nil); err != nil {
			t.Fatal(err)
		}
		clickID(t, driver, topExportMenuID)
		clickID(t, driver, format.id)
		if got, err := os.ReadFile(target); err != nil || !bytes.HasPrefix(got, []byte(format.prefix)) || state.fileMenuOpen {
			t.Fatalf("quick export %s failed: %v (%s)", format.extension, err, state.message)
		}
	}
	if got, err := os.ReadFile(copyPath); err != nil || !bytes.Equal(got, expected) {
		t.Fatalf("export modified the source: %q, %v", got, err)
	}
}

func TestM11TopBarFitsResponsiveWindow(t *testing.T) {
	state := newModel()
	state.preferences.Language, state.preferences.UITextSize = "en", "large"
	var canvas gfx.Canvas
	config := newConfig(state)
	paint := config.Paint
	config.Paint = func(canvas *gfx.Canvas, size gfx.Size) {
		canvas.SetTitlebarInsets(80, 90)
		paint(canvas, size)
	}
	driver, err := app.NewDriver(config, &canvas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	for _, width := range []int{740, 979, 980, 1040, 1200, 1280, 1600} {
		if err := driver.Resize(width, 720); err != nil {
			t.Fatal(err)
		}
		leading, trailing := canvas.TitlebarInsets()
		var hits []gfx.Hit
		for _, hit := range driver.Snapshot().Hits {
			if hit.ID.IsZero() || hit.Bounds.Y >= 32 || hit.Bounds.Height == 0 {
				continue
			}
			if hit.Bounds.X < leading || hit.Bounds.X+hit.Bounds.Width > float32(width)-trailing+.01 {
				t.Fatalf("width %d: control outside available titlebar: %+v", width, hit)
			}
			for _, other := range hits {
				if hit.Bounds.X < other.Bounds.X+other.Bounds.Width && other.Bounds.X < hit.Bounds.X+hit.Bounds.Width {
					t.Fatalf("width %d: overlapping top controls %q and %q", width, hit.Label, other.Label)
				}
			}
			hits = append(hits, hit)
		}
		if m3HasHit(driver, topAutoSaveID) != (width >= 980) || !m3HasHit(driver, topFontResetID) || !m3HasHit(driver, topExportMenuID) {
			t.Fatalf("unexpected quick-control availability at %dpx", width)
		}
	}
}
