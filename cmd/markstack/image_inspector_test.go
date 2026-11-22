package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

func TestImageInspectorKeyboardToolbar(t *testing.T) {
	for _, fixture := range []struct {
		name, source string
		image        gfx.ID
	}{
		{"photo.md", "![Photo](photo.png)\r\n", previewImages.At(0)},
		{"photo.mh", "<p><img src=\"photo.png\" alt=\"Photo\"></p>\r\n", htmlImages.At(0)},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, fixture.name)
			source := []byte("\xef\xbb\xbf" + fixture.source)
			if err := os.WriteFile(path, source, 0640); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "photo.png"), imagePNG(t, 320, 180), 0640); err != nil {
				t.Fatal(err)
			}
			state, driver := testDriver(t)
			if !state.openFile(path) {
				t.Fatal(state.message)
			}
			if err := driver.Resize(1200, 780); err != nil {
				t.Fatal(err)
			}
			press := func(key gfx.Key, modifiers gfx.Modifiers) {
				t.Helper()
				if err := driver.Key(key, modifiers); err != nil {
					t.Fatal(err)
				}
			}
			focus := func(step string, want gfx.ID) {
				t.Helper()
				if got := driver.Snapshot().Focused; got != want {
					t.Errorf("%s focused %v, want %v", step, got, want)
				}
			}
			clickID(t, driver, readModeID)
			clickID(t, driver, fixture.image)
			if !state.imageInspector.open {
				t.Fatal("image viewer did not open")
			}
			focus("open", imageCloseID)
			press(gfx.KeyTab, 0)
			focus("Tab skips surfaces and disabled controls", imageZoomInID)
			press(gfx.KeyTab, gfx.ModifierShift)
			focus("Shift+Tab returns to Close", imageCloseID)
			press(gfx.KeyTab, 0)
			press(gfx.KeyEnter, 0)
			if state.imageInspector.zoom <= 0 {
				t.Error("Enter did not activate Zoom In")
			}
			press(gfx.KeyTab, 0)
			press(gfx.KeyTab, 0)
			focus("Tab reaches enabled Zoom Out", imageZoomOutID)
			press(gfx.KeyTab, 0)
			focus("Tab reaches enabled Fit", imageFitID)
			press(gfx.KeySpace, 0)
			if state.imageInspector.zoom != 0 {
				t.Error("Space did not activate Fit")
			}
			focus("Fit hands off focus when it becomes disabled", imageZoomInID)
			press(gfx.KeyTab, 0)
			focus("Tab reaches Close", imageCloseID)
			press(gfx.KeyEnter, 0)
			if state.imageInspector.open {
				t.Error("Enter did not close the viewer")
			}
			focus("close returns to the invoking image", fixture.image)
			clickID(t, driver, fixture.image)
			press(gfx.KeyTab, gfx.ModifierControl)
			if driver.Snapshot().Focused != imageCloseID {
				t.Error("Control+Tab traversed the toolbar")
				press(gfx.KeyTab, gfx.ModifierShift)
			}
			press(gfx.KeyEnter, gfx.ModifierAlt)
			if !state.imageInspector.open {
				t.Error("Alt+Enter unexpectedly closed the viewer")
			} else {
				press(gfx.KeyEscape, 0)
			}
			current := state.session.Current()
			if current == nil || current.Path != path || current.Dirty() || current.Editor.CanUndo() {
				t.Fatal("viewing changed document identity, draft or history")
			}
			if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, source) {
				t.Fatalf("viewing changed disk bytes: %q, %v", data, err)
			}
		})
	}
}

func TestMarkdownImageInspectorZoomPanAndEscapeKeepSource(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "photo.md")
	source := "# Photo\n\n![Preview](assets/p.png)\n\nAfter\n"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "p.png"), imagePNG(t, 320, 180), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, readModeID)
	clickID(t, driver, previewImages.At(1))
	if !state.imageInspector.open || state.imageInspector.zoom != 0 || state.session.Current().Dirty() {
		t.Fatalf("inspector open=%v zoom=%v dirty=%v", state.imageInspector.open, state.imageInspector.zoom, state.session.Current().Dirty())
	}
	for _, text := range driver.Snapshot().Texts {
		if text.Value == "−" {
			t.Fatal("disabled image zoom-out paints a font glyph")
		}
	}
	for range 3 {
		clickID(t, driver, imageZoomInID)
	}
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == imageZoomInID && hit.Label != "放大图片" {
			t.Fatalf("image zoom control has unclear name: %+v", hit)
		}
	}
	if state.imageInspector.zoom <= 1 || state.imageInspector.geometry.Image.Width <= state.imageInspector.bounds.Width {
		t.Fatalf("inspector zoom=%v geometry=%+v bounds=%+v", state.imageInspector.zoom, state.imageInspector.geometry, state.imageInspector.bounds)
	}
	point := state.imageInspector.bounds
	centerX, centerY := point.X+point.Width/2, point.Y+point.Height/2
	if err := driver.Press(centerX, centerY, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := driver.Move(centerX+200, centerY+20); err != nil {
		t.Fatal(err)
	}
	if err := driver.Release(centerX+200, centerY+20, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if state.imageInspector.pan.X <= 0 || state.imageInspector.pan.X > (state.imageInspector.geometry.Image.Width-point.Width)/2+1 {
		t.Fatalf("inspector pan=%+v geometry=%+v", state.imageInspector.pan, state.imageInspector.geometry)
	}
	clickID(t, driver, imageFitID)
	if state.imageInspector.zoom != 0 || state.imageInspector.pan != (ui.Point{}) {
		t.Fatalf("fit did not reset zoom/pan: %+v", state.imageInspector)
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil || state.imageInspector.open {
		t.Fatalf("escape did not close image inspector: %v", err)
	}
	if state.session.Current().Text() != source || state.session.Current().Dirty() {
		t.Fatal("image inspection changed Markdown")
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.session.Save(state.session.Active, ""); err != nil {
		t.Fatal(err)
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(before, after) {
		t.Fatalf("inspector changed disk bytes: %q, %v", after, err)
	}
}

func TestMarkdownImageInspectorDoesNotOpenUnsafeOrMissingImages(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "unsafe.md")
	if err := os.WriteFile(path, []byte("![Remote](https://example.org/x.png)\n\n![Missing](missing.png)\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, readModeID)
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == previewImages.At(0) || hit.ID == previewImages.At(1) || hit.Label == "查看图片 Remote" || hit.Label == "查看图片 Missing" {
			t.Fatalf("unsafe image became actionable: %+v", hit)
		}
	}
	if state.imageInspector.open {
		t.Fatal("unsafe image opened inspector")
	}
}

func TestMarkdownImageInspectorNarrowDarkLayoutAndModalIsolation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("![Photo](assets/p.png)\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "p.png"), imagePNG(t, 320, 180), 0644); err != nil {
		t.Fatal(err)
	}
	state := newModel()
	var canvas gfx.Canvas
	session, err := headless.Open(newConfig(state), &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	state.openFile(path)
	if err := session.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, session.Driver(), readModeID)
	clickID(t, session.Driver(), previewImages.At(0))
	for _, viewport := range []struct {
		width, height int
		dark          bool
	}{{1200, 780, false}, {740, 520, false}, {740, 520, true}} {
		state.dark = viewport.dark
		if err := session.Resize(viewport.width, viewport.height); err != nil {
			t.Fatal(err)
		}
		if len(canvas.Images()) != 2 || !state.imageInspector.open {
			t.Fatalf("viewport=%+v image layers=%d open=%v", viewport, len(canvas.Images()), state.imageInspector.open)
		}
		for _, problem := range headless.CheckLayout(session.Snapshot()) {
			if problem.Kind == headless.LayoutOverlap || problem.Kind == headless.LayoutSpill {
				t.Fatalf("viewport=%+v layout=%+v", viewport, problem)
			}
		}
		for _, hit := range session.Snapshot().Hits {
			if hit.ID == imageCloseID || hit.ID == imageZoomInID || hit.ID == imageZoomOutID || hit.ID == imageFitID {
				if hit.Bounds.X < 0 || hit.Bounds.X+hit.Bounds.Width > float32(viewport.width) {
					t.Fatalf("viewport=%+v control outside window: %+v", viewport, hit)
				}
			}
		}
	}
	clickID(t, session.Driver(), fileMenuID)
	if state.fileMenuOpen || state.imageInspector.open {
		t.Fatalf("backdrop did not isolate background action: menu=%v inspector=%v", state.fileMenuOpen, state.imageInspector.open)
	}
}
