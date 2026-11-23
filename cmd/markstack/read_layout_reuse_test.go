package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func TestMarkdownReadLayoutRemeasuresOnlyWhenInputsChange(test *testing.T) {
	root := test.TempDir()
	path := filepath.Join(root, "read.md")
	const tail = "TailMarker keeps the layout measurable."
	const prefix = "# Start\r\n\r\n- [x] complete\r\n\r\n[Link](https://example.com)\r\n\r\n"
	source := prefix + strings.Repeat("Middle paragraph.\r\n\r\n", 24) + "## " + tail + "\r\n"
	raw := []byte("\xef\xbb\xbf" + source)
	if err := os.WriteFile(path, raw, 0o640); err != nil {
		test.Fatal(err)
	}
	state := newModel()
	if changed, err := state.session.RequestWorkspace(root); err != nil || !changed {
		test.Fatalf("workspace: changed=%t, err=%v", changed, err)
	}
	if !state.openFile(path) {
		test.Fatal(state.message)
	}
	state.mode = readMode
	var canvas gfx.Canvas
	canvas.SetTree(&controlTree)
	bounds := ui.Rect{Width: 740, Height: 260}
	scale := float32(1)
	measurements := 0
	measure := func(value string, size, weight float32) float32 {
		if strings.Contains(value, "TailMarker") {
			measurements++
		}
		return float32(utf8.RuneCountInString(value)) * size * .5
	}
	paint := func() int {
		measurements = 0
		canvas.Reset()
		canvas.SetPixelScale(scale)
		canvas.SetMeasurer(measure)
		theme, palette := state.look()
		state.paintDocument(&canvas, theme, palette, bounds)
		return measurements
	}
	assertMeasured := func(step string) {
		test.Helper()
		if paint() == 0 {
			test.Errorf("%s did not rebuild the layout", step)
		}
	}
	assertMeasured("first frame")
	state.previewScroll = 30
	if count := paint(); count != 0 {
		test.Errorf("unchanged Read scroll remeasured offscreen text %d times", count)
	}
	bounds.Height += 20
	if count := paint(); count != 0 {
		test.Errorf("height-only resize remeasured offscreen text %d times", count)
	}
	if state.preview.LinkCount() != 1 || state.preview.Link(0) != "https://example.com" {
		test.Fatal("repaint did not retain the link target")
	}
	bounds.Width = 500
	assertMeasured("column width change")
	state.dark = true
	assertMeasured("theme change")
	state.preferences.ReadingTheme = "soft"
	assertMeasured("reading theme change")
	state.preferences.ReadingSize += 2
	assertMeasured("reading size change")
	scale = 2
	assertMeasured("pixel scale change")
	state.refresh()
	assertMeasured("workspace refresh")
	current := state.session.Current()
	current.Replace(strings.Replace(source, "# Start", "# Updated", 1))
	assertMeasured("source change")
	if renderedPlain(&state.parsed, 0) != "Updated" {
		test.Fatal("Read kept stale parsed heading")
	}
	for _, mode := range []viewMode{editMode, renderedMode, splitMode} {
		state.mode = mode
		paint()
		state.mode = readMode
		assertMeasured("return to Read")
	}
	other := state.session.NewDocument()
	other.Replace(current.Text())
	assertMeasured("switch to identical text in another tab")
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, data) {
		test.Fatalf("Read changes on disk: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o640 {
		test.Fatalf("Read changes file mode: %v %v", info, err)
	}
}
