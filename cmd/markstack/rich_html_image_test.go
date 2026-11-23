package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

func imagePNG(t *testing.T, width, height int) []byte {
	return imagePNGColor(t, width, height, color.NRGBA{R: 255, A: 255})
}

func TestRichHTMLMissingImagePlaceholderDoesNotOverlapTrailingCaret(t *testing.T) {
	state := newModel()
	var canvas gfx.Canvas
	session, err := headless.Open(newConfig(state), &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	state.openFile(filepath.Join("..", "..", "internal", "richedit", "testdata", "Rich-HTML.mh"))
	if err := session.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	for _, problem := range headless.CheckLayout(session.Snapshot()) {
		if problem.Kind == headless.LayoutOverlap && strings.Contains(problem.Text, "本地图") {
			t.Fatalf("missing image overlaps next caret: %+v", problem)
		}
	}
}

func imagePNGColor(t *testing.T, width, height int, colorValue color.NRGBA) []byte {
	t.Helper()
	bitmap := image.NewNRGBA(image.Rect(0, 0, width, height))
	bitmap.Set(0, 0, colorValue)
	var output bytes.Buffer
	if err := png.Encode(&output, bitmap); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestRichHTMLRefreshReloadsChangedLocalImage(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "note.mh")
	asset := filepath.Join(root, "assets", "p.png")
	if err := os.WriteFile(path, []byte(`<p><img src="assets/p.png" alt="local"></p>`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(asset, imagePNG(t, 2, 1), 0644); err != nil {
		t.Fatal(err)
	}
	state := newModel()
	var canvas gfx.Canvas
	driver, err := app.NewDriver(newConfig(state), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	if changed, err := state.session.RequestWorkspace(root); err != nil || !changed {
		t.Fatalf("workspace=%v, %v", changed, err)
	}
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	if images := canvas.Images(); len(images) != 1 || !bytes.Equal(images[0].Bitmap.Pix[:4], []byte{0, 0, 255, 255}) {
		t.Fatalf("initial image=%+v", images)
	}
	bounds := canvas.Images()[0].Bounds
	x, y := bounds.X+bounds.Width/2, bounds.Y+bounds.Height/2
	if err := driver.Press(x, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(asset, imagePNGColor(t, 2, 1, color.NRGBA{G: 255, A: 255}), 0644); err != nil {
		t.Fatal(err)
	}
	activateRichHTMLImageTestControl(t, driver, refreshID)
	if images := canvas.Images(); len(images) != 1 || !bytes.Equal(images[0].Bitmap.Pix[:4], []byte{0, 255, 0, 255}) || state.session.Current().Dirty() {
		t.Fatalf("refreshed image=%+v dirty=%v", images, state.session.Current().Dirty())
	}
	if err := driver.Release(x, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if state.imageInspector.open {
		t.Fatal("release opened the refreshed bitmap without a new press")
	}
	if err := driver.Click(x, y); err != nil {
		t.Fatal(err)
	}
	if !state.imageInspector.open || !bytes.Equal(state.imageInspector.bitmap.Pix[:4], []byte{0, 255, 0, 255}) {
		t.Fatal("a new click did not open the refreshed bitmap")
	}
}

func TestRichHTMLImageDecoderBoundsPixelsAndConvertsBGRA(t *testing.T) {
	bitmap, err := decodeRichImage(imagePNG(t, 2, 1))
	if err != nil || bitmap.Width != 2 || bitmap.Height != 1 || bitmap.Stride != 8 || !bytes.Equal(bitmap.Pix[:4], []byte{0, 0, 255, 255}) {
		t.Fatalf("decoded bitmap=%+v, %v", bitmap, err)
	}
	if _, err := decodeRichImage([]byte("not an image")); err == nil {
		t.Fatal("malformed image was accepted")
	}
	if _, err := decodeRichImage(imagePNG(t, 5000, 1)); !errors.Is(err, errRichImageTooLarge) {
		t.Fatalf("oversized dimensions=%v", err)
	}
}

func TestRichHTMLDrawsOnlyConfinedLocalImage(t *testing.T) {
	root := t.TempDir()
	for _, directory := range []string{"notes", "assets"} {
		if err := os.Mkdir(filepath.Join(root, directory), 0755); err != nil {
			t.Fatal(err)
		}
	}
	asset := filepath.Join(root, "assets", "p.png")
	if err := os.WriteFile(asset, imagePNG(t, 2, 1), 0644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outside, imagePNG(t, 1, 1), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "broken.png"), []byte("not a PNG"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "assets", "link.png")); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, source string
		wantImages   int
	}{
		{"local", `<p><img src="../assets/p.png" alt="本地图"></p>`, 1},
		{"symlink", `<p><img src="../assets/link.png" alt="链接图"></p>`, 0},
		{"remote", `<p><img src="https://example.org/p.png" alt="远程图"></p>`, 0},
		{"corrupt", `<p><img src="../assets/broken.png" alt="损坏图"></p>`, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(root, "notes", test.name+".mh")
			if err := os.WriteFile(path, []byte(test.source), 0644); err != nil {
				t.Fatal(err)
			}
			state := newModel()
			var canvas gfx.Canvas
			driver, err := app.NewDriver(newConfig(state), &canvas)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(driver.Close)
			if changed, err := state.session.RequestWorkspace(root); err != nil || !changed {
				t.Fatalf("workspace=%v, %v", changed, err)
			}
			state.openFile(path)
			if err := driver.Resize(1200, 780); err != nil {
				t.Fatal(err)
			}
			images := canvas.Images()
			if len(images) != test.wantImages {
				t.Fatalf("drawn images=%d, want %d", len(images), test.wantImages)
			}
			if test.wantImages == 1 && (images[0].Bitmap.Width != 2 || images[0].Bounds.Width <= 0 || images[0].Bounds.Height <= 0) {
				t.Fatalf("local image geometry=%+v", images[0])
			}
			if test.wantImages == 0 && !hasText(driver, "图") {
				t.Fatal("blocked image has no visible fallback")
			}
			for _, hit := range driver.Snapshot().Hits {
				if _, image := hit.ID.In(htmlImages); image && test.wantImages == 0 {
					t.Fatalf("blocked image became actionable: %q", hit.Label)
				}
			}
		})
	}
}

func TestRichHTMLImageInspectorOpensExactLocalImage(t *testing.T) {
	for _, read := range []bool{false, true} {
		mode := "layout"
		if read {
			mode = "read"
		}
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "images.mh")
			source := []byte("\xef\xbb\xbf<p>Before</p>\r\n<p><img src=\"first.png\" alt=\"First\"></p>\r\n<p><img src=\"second.png\" alt=\"Second\"></p>\r\n")
			if err := os.WriteFile(path, source, 0640); err != nil {
				t.Fatal(err)
			}
			for index, name := range []string{"first.png", "second.png"} {
				if err := os.WriteFile(filepath.Join(root, name), imagePNG(t, 60+index*10, 45), 0640); err != nil {
					t.Fatal(err)
				}
			}
			state := newModel()
			var canvas gfx.Canvas
			driver, err := app.NewDriver(newConfig(state), &canvas)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(driver.Close)
			if !state.openFile(path) {
				t.Fatal(state.message)
			}
			if err := driver.Resize(1100, 760); err != nil {
				t.Fatal(err)
			}
			if read {
				clickID(t, driver, readModeID)
			} else {
				clickID(t, driver, htmlLeafRows.At(0))
				if err := driver.SelectText(1, 3); err != nil {
					t.Fatal(err)
				}
				if err := driver.Type("X"); err != nil {
					t.Fatal(err)
				}
				if err := driver.SelectText(3, 1); err != nil {
					t.Fatal(err)
				}
			}
			modeBefore, current := state.mode, state.session.Current()
			draft := current.Text()
			if current.Dirty() == read || current.Editor.CanUndo() == read {
				t.Fatal("fixture has the wrong draft or history state")
			}
			input := state.html.input
			var selection app.TextState
			if !read {
				selection = app.TextStateOf(input)
			}
			first := canvas.Images()[0].Bounds
			dragX, dragY := first.X+4, first.Y+first.Height/2
			if err := driver.Press(dragX, dragY, gfx.PointerButtonPrimary); err != nil {
				t.Fatal(err)
			}
			if err := driver.Move(dragX+30, dragY); err != nil {
				t.Fatal(err)
			}
			if err := driver.Release(dragX+30, dragY, gfx.PointerButtonPrimary); err != nil {
				t.Fatal(err)
			}
			if state.imageInspector.open {
				t.Error("dragging inside an image opened the viewer")
				if err := driver.Key(gfx.KeyEscape, 0); err != nil {
					t.Fatal(err)
				}
			}
			if !read && driver.Snapshot().Focused != htmlInputID {
				t.Errorf("image press moved editor focus: %v", driver.Snapshot().Focused)
			}
			for index, name := range []string{"First", "Second"} {
				images := canvas.Images()
				if len(images) != 2 {
					t.Fatalf("drawn image count=%d", len(images))
				}
				image := images[index]
				if err := driver.Click(image.Bounds.X+image.Bounds.Width/2, image.Bounds.Y+image.Bounds.Height/2); err != nil {
					t.Fatal(err)
				}
				if !state.imageInspector.open || state.imageInspector.alt != name || state.imageInspector.bitmap.ID != image.Bitmap.ID {
					t.Fatalf("click did not open exact bitmap %s: open=%v alt=%q", name, state.imageInspector.open, state.imageInspector.alt)
				}
				clickID(t, driver, imageZoomInID)
				if state.imageInspector.zoom <= 0 {
					t.Fatal("shared zoom did not apply")
				}
				clickID(t, driver, imageFitID)
				if state.imageInspector.zoom != 0 {
					t.Fatal("shared Fit did not reset zoom")
				}
				if index == 0 {
					if err := driver.Key(gfx.KeyEscape, 0); err != nil {
						t.Fatal(err)
					}
				} else {
					clickID(t, driver, imageCloseID)
				}
				if state.imageInspector.open {
					t.Fatal("viewer did not close")
				}
				if !read && driver.Snapshot().Focused != htmlInputID {
					t.Errorf("closing image %s did not restore the focused editor", name)
				}
			}
			if state.mode != modeBefore || state.session.Current() != current || current.Text() != draft || current.Dirty() == read || current.Editor.CanUndo() == read || current.Editor.CanRedo() {
				t.Fatal("viewing changed the document, mode or history")
			}
			if !read && (state.html.input != input || app.TextStateOf(input) != selection) {
				t.Fatal("viewing changed the editor or directed selection")
			}
			if !read {
				if err := driver.Type("Z"); err != nil {
					t.Fatalf("typing immediately after closing the viewer: %v", err)
				}
				if want := strings.Replace(draft, "<p>BXore</p>", "<p>BZre</p>", 1); want == draft || current.Text() != want {
					t.Fatalf("typing changed the wrong source interval: %q, want %q", current.Text(), want)
				}
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					t.Fatal(err)
				}
				if current.Text() != draft || !current.Editor.CanUndo() || !current.Editor.CanRedo() {
					t.Fatal("undo did not return to the draft and its previous history")
				}
			}
			if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, source) {
				t.Fatalf("viewing changed the source: %q, %v", data, err)
			}
		})
	}
}

func TestRichHTMLImageInspectorRejectsPressAcrossDocuments(t *testing.T) {
	root := t.TempDir()
	for _, asset := range []struct {
		name string
		fill color.NRGBA
	}{
		{"first", color.NRGBA{R: 255, A: 255}},
		{"second", color.NRGBA{B: 255, A: 255}},
	} {
		if err := os.WriteFile(filepath.Join(root, asset.name+".png"), imagePNGColor(t, 80, 60, asset.fill), 0640); err != nil {
			t.Fatal(err)
		}
		source := `<p><img src="` + asset.name + `.png" alt="` + asset.name + `"></p>`
		if err := os.WriteFile(filepath.Join(root, asset.name+".mh"), []byte(source), 0640); err != nil {
			t.Fatal(err)
		}
	}
	state := newModel()
	var canvas gfx.Canvas
	driver, err := app.NewDriver(newConfig(state), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	first, second := filepath.Join(root, "first.mh"), filepath.Join(root, "second.mh")
	if !state.openFile(first) || !state.openFile(second) {
		t.Fatal(state.message)
	}
	secondTab := tabRows.At(state.session.Active)
	if !state.openFile(first) {
		t.Fatal(state.message)
	}
	if err := driver.Resize(1100, 760); err != nil {
		t.Fatal(err)
	}
	image := canvas.Images()[0]
	x, y := image.Bounds.X+image.Bounds.Width/2, image.Bounds.Y+image.Bounds.Height/2
	if err := driver.Press(x, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	activateRichHTMLImageTestControl(t, driver, secondTab)
	next := canvas.Images()[0]
	if state.session.Current().Path != second || next.Bounds != image.Bounds || next.Bitmap.ID == image.Bitmap.ID {
		t.Fatal("fixture did not switch to a different bitmap at the same position")
	}
	if err := driver.Release(x, y, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if state.imageInspector.open {
		t.Fatalf("release opened the other document's image %q", state.imageInspector.alt)
	}
	if err := driver.Click(x, y); err != nil {
		t.Fatal(err)
	}
	if !state.imageInspector.open || state.imageInspector.bitmap.ID != next.Bitmap.ID {
		t.Fatal("a new click did not open the current bitmap")
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyTab, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyTab, 0); err != nil {
		t.Fatal(err)
	}
	if _, isImage := driver.Snapshot().Focused.In(htmlImages); !isImage {
		t.Fatal("Tab did not reach the image")
	}
	_, palette := state.look()
	visibleRing := false
	for _, triangle := range canvas.Triangles() {
		point := triangle.Points[0]
		if triangle.Color == palette.accent && point.X >= next.Bounds.X-3 && point.X <= next.Bounds.X+next.Bounds.Width+3 &&
			point.Y >= next.Bounds.Y-3 && point.Y <= next.Bounds.Y+next.Bounds.Height+3 {
			visibleRing = true
			break
		}
	}
	if !visibleRing {
		t.Error("image keyboard focus has no visible outline")
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if !state.imageInspector.open || state.imageInspector.bitmap.ID != next.Bitmap.ID {
		t.Fatal("the focused image did not open from the keyboard")
	}
}

func activateRichHTMLImageTestControl(t *testing.T, driver *app.Driver, id gfx.ID) {
	t.Helper()
	for range len(driver.Snapshot().Hits) {
		if driver.Snapshot().Focused == id {
			if err := driver.Key(gfx.KeyEnter, 0); err != nil {
				t.Fatal(err)
			}
			return
		}
		if err := driver.Key(gfx.KeyTab, gfx.ModifierShift); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatalf("could not reach %v by keyboard", id)
}

func TestRichHTMLImageInspectorDoesNotInterruptPreedit(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "composing.mh")
	source := []byte("\xef\xbb\xbf<p>Before</p>\r\n<p><img src=\"photo.png\" alt=\"Photo\"></p>\r\n")
	if err := os.WriteFile(path, source, 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "photo.png"), imagePNG(t, 80, 60), 0640); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MARKSTACK_RECOVERY_PATH", filepath.Join(t.TempDir(), "recovery.json"))
	state := newModel()
	var canvas gfx.Canvas
	driver, err := app.NewDriver(newConfig(state), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	if !state.openFile(path) {
		t.Fatal(state.message)
	}
	if err := driver.Resize(1100, 760); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, htmlLeafRows.At(0))
	if err := driver.SelectText(1, 3); err != nil {
		t.Fatal(err)
	}
	if err := driver.Compose("ni"); err != nil {
		t.Fatal(err)
	}
	input := state.html.input
	before := app.TextStateOf(input)
	for _, hit := range driver.Snapshot().Hits {
		if _, isImage := hit.ID.In(htmlImages); isImage && (hit.Role != gfx.RoleGroup || hit.Focusable) {
			t.Error("preedit image still advertises an actionable control")
		}
	}
	image := canvas.Images()[0]
	if err := driver.Click(image.Bounds.X+image.Bounds.Width/2, image.Bounds.Y+image.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if state.imageInspector.open || driver.Snapshot().Focused != htmlInputID || state.html.input != input || app.TextStateOf(input) != before {
		t.Fatal("image click interrupted the active preedit")
	}
	if err := driver.Compose("nihao"); err != nil {
		t.Fatal(err)
	}
	current := state.session.Current()
	if current.Dirty() || current.Editor.CanUndo() || state.html.input != input || !state.htmlComposing() {
		t.Fatal("continuing preedit changed the committed state")
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, source) {
		t.Fatalf("preedit changed the source: %q, %v", data, err)
	}
}

func TestRichHTMLLocalImageSurvivesDirectoryMoveAndSave(t *testing.T) {
	root := t.TempDir()
	for _, directory := range []string{"notes", "assets"} {
		if err := os.Mkdir(filepath.Join(root, directory), 0755); err != nil {
			t.Fatal(err)
		}
	}
	source := `<p><img src="../assets/p.png" alt="图"></p>`
	path := filepath.Join(root, "notes", "note.mh")
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	imageBytes := imagePNG(t, 2, 1)
	assetPath := filepath.Join(root, "assets", "p.png")
	if err := os.WriteFile(assetPath, imageBytes, 0644); err != nil {
		t.Fatal(err)
	}
	originalHash := sha256.Sum256(imageBytes)
	render := func(t *testing.T, documentPath string) []byte {
		t.Helper()
		state := newModel()
		var canvas gfx.Canvas
		driver, err := app.NewDriver(newConfig(state), &canvas)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(driver.Close)
		if changed, err := state.session.RequestWorkspace(filepath.Dir(filepath.Dir(documentPath))); err != nil || !changed {
			t.Fatalf("workspace=%v, %v", changed, err)
		}
		state.openFile(documentPath)
		if err := driver.Resize(1200, 780); err != nil {
			t.Fatal(err)
		}
		if images := canvas.Images(); len(images) != 1 || images[0].Bitmap.Width != 2 {
			t.Fatalf("local rich image=%+v", images)
		}
		image := canvas.Images()[0]
		if err := driver.Click(image.Bounds.X+image.Bounds.Width/2, image.Bounds.Y+image.Bounds.Height/2); err != nil {
			t.Fatal(err)
		}
		if !state.imageInspector.open || state.imageInspector.bitmap.ID != image.Bitmap.ID {
			t.Fatal("local image did not open after resolving its relative path")
		}
		if err := driver.Key(gfx.KeyEscape, 0); err != nil {
			t.Fatal(err)
		}
		if err := state.session.Save(state.session.Active, ""); err != nil {
			t.Fatal(err)
		}
		saved, err := os.ReadFile(documentPath)
		if err != nil || string(saved) != source || state.session.Current().Dirty() {
			t.Fatalf("rich image save=%q, %v", saved, err)
		}
		return saved
	}
	render(t, path)
	moved := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	movedAsset, err := os.ReadFile(filepath.Join(moved, "assets", "p.png"))
	if err != nil || sha256.Sum256(movedAsset) != originalHash {
		t.Fatalf("moved rich image hash changed: %v", err)
	}
	render(t, filepath.Join(moved, "notes", "note.mh"))
}
