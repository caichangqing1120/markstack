package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestImageInspectorZoomLimitStaysAnchoredToFit(test *testing.T) {
	fixtures := []struct {
		name, body    string
		image         gfx.ID
		width, height int
		fitBasedLimit bool
	}{
		{"photo.md", "![Photo](photo.png)\r\n", previewImages.At(0), 96, 96, true},
		{"photo.mh", "<p><img src=\"photo.png\" alt=\"Photo\"></p>\r\n", htmlImages.At(0), 640, 360, false},
	}
	for _, fixture := range fixtures {
		test.Run(fixture.name, func(subtest *testing.T) {
			root := subtest.TempDir()
			path := filepath.Join(root, fixture.name)
			source := []byte("\xef\xbb\xbf" + fixture.body)
			if err := os.WriteFile(path, source, 0o640); err != nil {
				subtest.Fatal(err)
			}
			imagePath := filepath.Join(root, "photo.png")
			image := imagePNG(subtest, fixture.width, fixture.height)
			if err := os.WriteFile(imagePath, image, 0o640); err != nil {
				subtest.Fatal(err)
			}
			state, driver := testDriver(subtest)
			if !state.openFile(path) {
				subtest.Fatal(state.message)
			}
			if err := driver.Resize(1200, 780); err != nil {
				subtest.Fatal(err)
			}
			clickID(subtest, driver, readModeID)
			clickID(subtest, driver, fixture.image)
			inspector := &state.imageInspector
			fit := inspector.geometry.Scale * inspector.deviceScale
			if !inspector.open || fit <= 0 || (4*fit > 16) != fixture.fitBasedLimit {
				subtest.Fatalf("fixture did not reach the expected Fit branch: open=%v fit=%g", inspector.open, fit)
			}
			limit := max(16, fit*4)
			clickID(subtest, driver, imageZoomInID)
			firstZoom := max(1, fit*1.25)
			if inspector.zoom != firstZoom {
				subtest.Fatalf("first zoom=%g, want %g", inspector.zoom, firstZoom)
			}
			for step := range 24 {
				want := min(inspector.zoom*1.5, limit)
				if step%2 == 0 {
					clickID(subtest, driver, imageZoomInID)
				} else if err := driver.Scroll(gfx.ScrollEvent{X: 600, Y: 400, DeltaY: -60}); err != nil {
					subtest.Fatal(err)
				}
				if inspector.zoom != want {
					subtest.Fatalf("zoom at step %d = %g, want %g (limit %g)", step+2, inspector.zoom, want, limit)
				}
			}
			if inspector.zoom != limit {
				subtest.Fatalf("zoom stopped at %g, want %g", inspector.zoom, limit)
			}
			clickID(subtest, driver, imageZoomOutID)
			if inspector.zoom != limit/1.5 {
				subtest.Fatalf("Zoom Out from limit = %g", inspector.zoom)
			}
			clickID(subtest, driver, imageFitID)
			if inspector.zoom != 0 || inspector.geometry.Scale*inspector.deviceScale != fit {
				subtest.Fatalf("Fit did not restore the original scale: zoom=%g geometry=%+v", inspector.zoom, inspector.geometry)
			}
			if err := driver.Key(gfx.KeyEscape, 0); err != nil || inspector.open {
				subtest.Fatalf("Escape did not close the viewer: %v", err)
			}
			if document := state.session.Current(); document.Path != path || document.Dirty() || document.Editor.CanUndo() {
				subtest.Fatal("zoom changed the active document, draft or history")
			}
			for _, file := range []struct {
				path string
				want []byte
			}{{path, source}, {imagePath, image}} {
				if stored, err := os.ReadFile(file.path); err != nil || !bytes.Equal(stored, file.want) {
					subtest.Fatalf("zoom changed disk bytes: %v", err)
				}
				if info, err := os.Stat(file.path); err != nil || info.Mode().Perm() != 0o640 {
					subtest.Fatalf("zoom changed file permissions: %v, %v", info, err)
				}
			}
		})
	}
}
