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
	if err := os.WriteFile(asset, imagePNGColor(t, 2, 1, color.NRGBA{G: 255, A: 255}), 0644); err != nil {
		t.Fatal(err)
	}
	state.refresh()
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	if images := canvas.Images(); len(images) != 1 || !bytes.Equal(images[0].Bitmap.Pix[:4], []byte{0, 255, 0, 255}) || state.session.Current().Dirty() {
		t.Fatalf("refreshed image=%+v dirty=%v", images, state.session.Current().Dirty())
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
		})
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
