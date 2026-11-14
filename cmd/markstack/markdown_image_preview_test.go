package main

import (
	"bytes"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"

	"markstack/internal/workspace"
)

func TestMarkdownStandaloneImageRendersAfterRelocatingDocumentAndAssets(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	imageBytes := imagePNGColor(t, 240, 80, color.NRGBA{R: 255, A: 255})
	if err := os.WriteFile(path, []byte("# Gallery\n\n![Photo](assets/p.png)\n\nAfter\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "p.png"), imageBytes, 0644); err != nil {
		t.Fatal(err)
	}
	state := newModel()
	var canvas gfx.Canvas
	driver, err := app.NewDriver(newConfig(state), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, readModeID)
	images := canvas.Images()
	if len(images) != 1 || images[0].Bitmap.Width != 240 || !bytes.Equal(images[0].Bitmap.Pix[:4], []byte{0, 0, 255, 255}) {
		t.Fatalf("native Markdown images=%+v", images)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.session.Save(state.session.Active, ""); err != nil {
		t.Fatal(err)
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(before, after) {
		t.Fatalf("preview changed source: %q, %v", after, err)
	}
	moved := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	second := newModel()
	var movedCanvas gfx.Canvas
	movedDriver, err := app.NewDriver(newConfig(second), &movedCanvas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(movedDriver.Close)
	second.openFile(filepath.Join(moved, "note.md"))
	if err := movedDriver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, movedDriver, readModeID)
	if len(movedCanvas.Images()) != 1 || second.session.Current().Dirty() {
		t.Fatal("relocated Markdown image did not render without editing source")
	}
}

func TestMarkdownStandaloneImageRefusesRemoteMissingAndSymlinkAssets(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "private.png"), imagePNG(t, 1, 1), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "assets")); err != nil {
		t.Fatal(err)
	}
	source := "![Remote](https://example.org/p.png)\n\n![Missing](missing.png)\n\n![Private](assets/private.png)\n"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state := newModel()
	var canvas gfx.Canvas
	driver, err := app.NewDriver(newConfig(state), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, readModeID)
	if len(canvas.Images()) != 0 || state.session.Current().Text() != source || state.session.Current().Dirty() {
		t.Fatalf("unsafe image rendered or modified document: images=%d text=%q", len(canvas.Images()), state.session.Current().Text())
	}
	if !hasText(driver, "Remote") || !hasText(driver, "Private") {
		t.Fatal("unsupported image did not have a visible fallback")
	}
	if data, err := workspace.ReadAsset("", path, state.session.Current().SavedLocation(), "assets/private.png"); err == nil || len(data) != 0 {
		t.Fatal("symlink image escaped authorized directory")
	}
}

func TestMarkdownImageNarrowDarkLayoutAndCacheRefresh(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("![Photo](assets/p.png)\n\nBelow\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	asset := filepath.Join(root, "assets", "p.png")
	if err := os.WriteFile(asset, imagePNGColor(t, 200, 90, color.NRGBA{R: 255, A: 255}), 0644); err != nil {
		t.Fatal(err)
	}
	state := newModel()
	var canvas gfx.Canvas
	session, err := headless.Open(newConfig(state), &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	if changed, err := state.session.RequestWorkspace(root); err != nil || !changed {
		t.Fatalf("workspace=%v, %v", changed, err)
	}
	state.openFile(path)
	state.mode = readMode
	for _, viewport := range []struct {
		width, height int
		dark          bool
	}{{1200, 780, false}, {1200, 780, true}, {740, 520, false}, {740, 520, true}} {
		state.dark = viewport.dark
		if err := session.Resize(viewport.width, viewport.height); err != nil {
			t.Fatal(err)
		}
		if len(canvas.Images()) != 1 {
			t.Fatalf("viewport=%+v image count=%d", viewport, len(canvas.Images()))
		}
		for _, problem := range headless.CheckLayout(session.Snapshot()) {
			if problem.Kind == headless.LayoutOverlap || problem.Kind == headless.LayoutSpill {
				t.Fatalf("viewport=%+v layout=%+v", viewport, problem)
			}
		}
	}
	if err := os.WriteFile(asset, imagePNGColor(t, 200, 90, color.NRGBA{G: 255, A: 255}), 0644); err != nil {
		t.Fatal(err)
	}
	state.refresh()
	if err := session.Resize(741, 520); err != nil {
		t.Fatal(err)
	}
	images := canvas.Images()
	if len(images) != 1 || !bytes.Equal(images[0].Bitmap.Pix[:4], []byte{0, 255, 0, 255}) {
		t.Fatalf("refreshed Markdown image=%+v", images)
	}
	if strings.Contains(state.session.Current().Text(), "data:") {
		t.Fatal("preview embedded image data into source")
	}
}

func TestMarkdownPlainInlineRendersButInvalidLocalImagesStayLiteral(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.Mkdir(filepath.Join(root, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "valid.png"), imagePNG(t, 20, 10), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "broken.png"), []byte("not a PNG"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "huge.png"), imagePNG(t, 5000, 1), 0644); err != nil {
		t.Fatal(err)
	}
	source := "before ![Inline](assets/valid.png) after\n\n![Broken](assets/broken.png)\n\n![Huge](assets/huge.png)\n"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state := newModel()
	var canvas gfx.Canvas
	driver, err := app.NewDriver(newConfig(state), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, readModeID)
	if len(canvas.Images()) != 1 || canvas.Images()[0].Bitmap.Width != 20 || !hasText(driver, "before") || !hasText(driver, "after") || !hasText(driver, "Broken") || !hasText(driver, "Huge") || state.session.Current().Text() != source {
		t.Fatalf("inline/unsupported image routing: count=%d source=%q", len(canvas.Images()), state.session.Current().Text())
	}
}
