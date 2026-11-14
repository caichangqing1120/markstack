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
)

func inlineImageFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "mixed.md")
	source := "# 图片\n\n前文 ![红](assets/red.png) 中间 ![绿](assets/green.png) 后文\n\n尾段\n"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		name  string
		color color.NRGBA
	}{{"red.png", color.NRGBA{R: 255, A: 255}}, {"green.png", color.NRGBA{G: 255, A: 255}}} {
		if err := os.WriteFile(filepath.Join(root, "assets", item.name), imagePNGColor(t, 240, 100, item.color), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return path, source
}

func TestInlineMarkdownTwoImagesRenderAndOpenDistinctInspectorHits(t *testing.T) {
	path, source := inlineImageFixture(t)
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
	if images := canvas.Images(); len(images) != 2 || !bytes.Equal(images[0].Bitmap.Pix[:4], []byte{0, 0, 255, 255}) || !bytes.Equal(images[1].Bitmap.Pix[:4], []byte{0, 255, 0, 255}) {
		t.Fatalf("mixed native pixels=%+v", images)
	}
	for _, value := range []string{"前文", "中间", "后文", "尾段"} {
		if !hasText(driver, value) {
			t.Fatalf("mixed preview omitted %q", value)
		}
	}
	first, second := previewInlineImages.At(1*64+1), previewInlineImages.At(1*64+3)
	clickID(t, driver, first)
	if !state.imageInspector.open || !bytes.Equal(state.imageInspector.bitmap.Pix[:4], []byte{0, 0, 255, 255}) {
		t.Fatal("first inline image did not open its own bitmap")
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, second)
	if !state.imageInspector.open || !bytes.Equal(state.imageInspector.bitmap.Pix[:4], []byte{0, 255, 0, 255}) {
		t.Fatal("second inline image reused the first bitmap")
	}
	if state.session.Current().Text() != source || state.session.Current().Dirty() {
		t.Fatal("inline image inspection changed source")
	}
	if err := state.session.Save(state.session.Active, ""); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || string(saved) != source {
		t.Fatalf("inline preview changed disk: %q, %v", saved, err)
	}
	moved := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(filepath.Dir(path), moved); err != nil {
		t.Fatal(err)
	}
	reopened := newModel()
	var movedCanvas gfx.Canvas
	secondDriver, err := app.NewDriver(newConfig(reopened), &movedCanvas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(secondDriver.Close)
	reopened.openFile(filepath.Join(moved, "mixed.md"))
	if err := secondDriver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, secondDriver, readModeID)
	if len(movedCanvas.Images()) != 2 || reopened.session.Current().Text() != source {
		t.Fatal("moved mixed document lost local images or changed source")
	}
	clickID(t, secondDriver, previewInlineImages.At(1*64+1))
	if !reopened.imageInspector.open || !bytes.Equal(reopened.imageInspector.bitmap.Pix[:4], []byte{0, 0, 255, 255}) {
		t.Fatal("moved mixed image could not be inspected through its own hit")
	}
	if err := secondDriver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if reopened.session.Current().Text() != source || reopened.session.Current().Dirty() {
		t.Fatal("inspecting a moved mixed image changed source")
	}
}

func TestInlineMarkdownUnsafeImagesHaveVisibleFallbackWithoutHits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unsafe.md")
	source := "前文 ![远程](https://example.org/p.png) 后文\n\n丢失 ![本地](assets/missing.png) 仍可读\n"
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
	if len(canvas.Images()) != 0 || !hasText(driver, "图片不可用") || state.session.Current().Text() != source {
		t.Fatalf("unsafe mixed image route: images=%d text=%q", len(canvas.Images()), state.session.Current().Text())
	}
	for _, hit := range driver.Snapshot().Hits {
		if strings.Contains(hit.Label, "查看图片") {
			t.Fatalf("unsafe mixed image became actionable: %+v", hit)
		}
	}
}

func TestInlineMarkdownNarrowDarkRowsDoNotOverlapFollowingBlock(t *testing.T) {
	path, source := inlineImageFixture(t)
	state := newModel()
	var canvas gfx.Canvas
	session, err := headless.Open(newConfig(state), &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	state.openFile(path)
	state.mode = readMode
	for _, viewport := range []struct {
		width, height int
		dark          bool
		readingSize   int
	}{{1200, 780, false, 16}, {740, 520, false, 16}, {740, 520, true, 22}} {
		state.dark = viewport.dark
		state.preferences.ReadingSize = viewport.readingSize
		if err := session.Resize(viewport.width, viewport.height); err != nil {
			t.Fatal(err)
		}
		if len(canvas.Images()) != 2 {
			t.Fatalf("viewport=%+v image count=%d", viewport, len(canvas.Images()))
		}
		for _, problem := range headless.CheckLayout(session.Snapshot()) {
			if problem.Kind == headless.LayoutOverlap || problem.Kind == headless.LayoutSpill {
				t.Fatalf("viewport=%+v mixed layout=%+v", viewport, problem)
			}
		}
	}
	if state.session.Current().Text() != source || state.session.Current().Dirty() {
		t.Fatal("resizing mixed preview changed source")
	}
}

func TestMixedRenderedFindAndEditFallBackToSourceOffsets(t *testing.T) {
	path, source := inlineImageFixture(t)
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, findDocumentID)
	if err := driver.Type("后文"); err != nil {
		t.Fatal(err)
	}
	if state.mode != editMode || len(state.findMatches) != 1 || state.session.Current().Text() != source {
		t.Fatalf("mixed rendered find used stale visual offsets: mode=%v matches=%v", state.mode, state.findMatches)
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	if state.mode != editMode || state.session.Current().Dirty() {
		t.Fatalf("mixed image block became editable with wrong source mapping: mode=%v dirty=%v", state.mode, state.session.Current().Dirty())
	}
}

func TestMixedInlineLargeWrappedCJKLeavesFollowingBlockBelowImage(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "long.md")
	line := strings.Repeat("长中文", 32) + " ![图](assets/p.png) " + strings.Repeat("后续内容", 32)
	if err := os.WriteFile(path, []byte(line+"\n\n尾段\n"), 0644); err != nil {
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
	state.mode = readMode
	if err := session.Resize(740, 900); err != nil {
		t.Fatal(err)
	}
	if len(canvas.Images()) != 1 {
		t.Fatalf("large mixed image count=%d", len(canvas.Images()))
	}
	imageBottom := canvas.Images()[0].Bounds.Y + canvas.Images()[0].Bounds.Height
	for _, drawn := range session.Snapshot().Texts {
		if drawn.Value == "尾段" && drawn.Bounds.Y > imageBottom {
			for _, problem := range headless.CheckLayout(session.Snapshot()) {
				if problem.Kind == headless.LayoutOverlap || problem.Kind == headless.LayoutSpill {
					t.Fatalf("long mixed layout=%+v", problem)
				}
			}
			return
		}
	}
	t.Fatalf("following block overlaps image or is missing; bottom=%v", imageBottom)
}

func TestInlineUnavailableImageFallbackDoesNotOverlapFollowingText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fallback.md")
	source := "远程 ![不可用](https://example.org/p.png) 仍可读。\n\n丢失 ![缺失](assets/missing.png) 仍可读。\n"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
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
	state.mode = readMode
	for _, width := range []int{1200, 740} {
		if err := session.Resize(width, 780); err != nil {
			t.Fatal(err)
		}
		for _, problem := range headless.CheckLayout(session.Snapshot()) {
			if problem.Kind == headless.LayoutOverlap || problem.Kind == headless.LayoutSpill {
				t.Fatalf("width=%d unavailable image layout=%+v", width, problem)
			}
		}
	}
}

func TestOversizedMixedParagraphStaysLiteralAndEditable(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "long.md")
	source := strings.Repeat("中", 11<<10) + " ![图](assets/p.png)"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "p.png"), imagePNG(t, 1, 1), 0644); err != nil {
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
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, readModeID)
	if len(canvas.Images()) != 0 || state.session.Current().Text() != source || state.session.Current().Dirty() {
		t.Fatalf("oversized mixed source rendered unsafely: images=%d dirty=%v", len(canvas.Images()), state.session.Current().Dirty())
	}
	// Truthful fallback: the 32 KiB guard only skips the custom mixed-row
	// measurer; the plain literal preview and the source stay editable.
	clickID(t, driver, editModeID)
	clickID(t, driver, sourceID)
	current := state.session.Current()
	current.Editor.MoveTo(current.Editor.RuneCount(), false)
	if err := driver.Type("尾"); err != nil {
		t.Fatal(err)
	}
	if got := current.Text(); got != source+"尾" {
		t.Fatalf("oversized mixed paragraph was not editable: %d bytes", len(got))
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil || current.Text() != source {
		t.Fatalf("oversized mixed edit did not undo: %v", err)
	}
	if err := state.session.Save(state.session.Active, ""); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || string(saved) != source {
		t.Fatalf("oversized mixed no-op save = %d bytes, %v", len(saved), err)
	}
}
