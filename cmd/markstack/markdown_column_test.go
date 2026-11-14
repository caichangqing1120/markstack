package main

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

func TestMarkdownReferenceColumnUsesViewportBreakpoints(t *testing.T) {
	for _, test := range []struct {
		name          string
		viewport      int
		sidebar       float32
		dark          bool
		left, content float32
	}{
		{"narrow", 740, 240, false, 270, 440},
		{"normal", 1200, 200, false, 300, 800},
		{"before-wide", 1399, 270, false, 434.5, 800},
		{"wide-boundary", 1400, 270, false, 353, 964},
		{"reference", 1605, 270, false, 455.5, 964},
		{"before-large", 1799, 270, false, 552.5, 964},
		{"large-boundary", 1800, 270, false, 465, 1140},
		{"large", 1920, 270, false, 525, 1140},
		{"night-normal", 1200, 200, true, 273, 854},
		{"night-reference", 1605, 270, true, 455.5, 964},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, driver := testDriver(t)
			state.sidebarWidth, state.dark = test.sidebar, test.dark
			state.session.Current().Replace("# Head\n\nBody\n")
			if err := driver.Resize(test.viewport, 955); err != nil {
				t.Fatal(err)
			}
			clickID(t, driver, readModeID)
			found := false
			for _, label := range driver.Snapshot().Texts {
				if label.Value == "Head" {
					found = true
					if math.Abs(float64(label.Bounds.X-test.left)) > 0.01 {
						t.Errorf("reader left=%v, want %v", label.Bounds.X, test.left)
					}
				}
			}
			if !found {
				t.Fatal("heading not painted")
			}
			if got := state.preview.blocks[0].width; got != test.content {
				t.Errorf("reader content width=%v, want %v", got, test.content)
			}
			clickID(t, driver, renderedModeID)
			clickID(t, driver, renderedRows.At(0))
			if math.Abs(float64(state.rich.rect.X-test.left)) > 0.01 || state.rich.rect.Width != test.content {
				t.Errorf("active column=%+v, want left=%v width=%v", state.rich.rect, test.left, test.content)
			}
		})
	}
}

func TestMarkdownReferenceColumnRespectsPaneBounds(t *testing.T) {
	for _, mode := range []viewMode{readMode, splitMode} {
		state, driver := testDriver(t)
		state.sidebarWidth, state.outline, state.outlineWidth = 270, true, 184
		state.session.Current().Replace("# Head\n\nBody\n")
		state.mode = mode
		if err := driver.Resize(1605, 955); err != nil {
			t.Fatal(err)
		}
		left, width := float32(363.5), float32(964)
		if mode == splitMode {
			left, width = state.previewRect.X+30, state.previewRect.Width-60
		}
		if got := state.preview.blocks[0].width; got != width {
			t.Errorf("mode %v content width=%v, want %v", mode, got, width)
		}
		found := false
		for _, label := range driver.Snapshot().Texts {
			if label.Value == "Head" && label.Bounds.X >= state.previewRect.X && label.Bounds.X < state.previewRect.X+state.previewRect.Width {
				found = true
				if math.Abs(float64(label.Bounds.X-left)) > 0.01 {
					t.Errorf("mode %v reader left=%v, want %v", mode, label.Bounds.X, left)
				}
			}
		}
		if !found {
			t.Fatalf("mode %v preview heading missing", mode)
		}
		if left < state.previewRect.X || left+width > state.previewRect.X+state.previewRect.Width {
			t.Fatalf("column escapes preview pane: left=%v width=%v pane=%+v", left, width, state.previewRect)
		}
	}
}

func TestMarkdownReferenceColumnClickPreservesSourceBytes(t *testing.T) {
	engine := app.PlatformTextEngine()
	if engine == nil {
		t.Skip("platform text engine unavailable")
	}
	path := filepath.Join(t.TempDir(), "Column.md")
	source := "before **bold** after\r\n\r\nTail\r\n<!--keep-->\r\n"
	original := []byte("\ufeff" + source)
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	state := newModel()
	state.sidebarWidth = 240
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
	if state.rich.rect.X != 270 || state.rich.rect.Width != 440 {
		t.Errorf("native active column=%+v", state.rich.rect)
	}
	caret, ok := state.preview.blocks[0].view.CaretAt(len("before bo"))
	if !ok {
		t.Fatal("bold leaf caret missing")
	}
	if err := driver.Click(caret.X, caret.Y+caret.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	want := []byte("\ufeffbefore **boXld** after\r\n\r\nTail\r\n<!--keep-->\r\n")
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, want) {
		t.Fatalf("column click changed wrong bytes: %q, %v", saved, err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, original) {
		t.Fatalf("column Undo/Save did not restore original: %q, %v", saved, err)
	}
}

func TestMarkdownReferenceColumnNativeHeadingBounds(t *testing.T) {
	engine := app.PlatformTextEngine()
	if engine == nil {
		if os.Getenv("MARKSTACK_REQUIRE_NATIVE_COLUMN") == "1" {
			t.Fatal("native column acceptance requires a platform text engine")
		}
		t.Skip("platform text engine unavailable")
	}
	source, err := os.ReadFile("testdata/Rendered-Heading.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name          string
		viewport      int
		sidebar       float32
		left, content float32
	}{{"narrow", 740, 190, 220, 490}, {"reference", 1605, 270, 455.5, 964}} {
		t.Run(test.name, func(t *testing.T) {
			state := newModel()
			state.sidebarWidth = test.sidebar
			state.session.Current().Replace(string(source))
			var canvas gfx.Canvas
			canvas.SetTextEngine(engine)
			driver, err := app.NewDriver(newConfig(state), &canvas)
			if err != nil {
				t.Fatal(err)
			}
			defer driver.Close()
			if err := driver.Resize(test.viewport, 520); err != nil {
				t.Fatal(err)
			}
			check := func(stage string) {
				t.Helper()
				top := state.previewRect.Y + 46 - state.previewScroll + state.preview.blocks[0].top
				bottom := top + state.preview.blocks[0].height
				checked := 0
				for _, label := range canvas.Texts() {
					if label.Value == "" || label.Style.Color.Alpha <= 0 || !label.HasClip || label.ClipOf != state.previewRect || label.Bounds.Y < top || label.Bounds.Y >= bottom {
						continue
					}
					advance := canvas.TextWidthStyle(label.Value, label.Style)
					if label.Bounds.X < test.left-0.01 || label.Bounds.X+advance > test.left+test.content+0.01 || label.Bounds.Y+label.Bounds.Height > bottom+0.01 {
						t.Errorf("%s native heading run exceeds column: %q bounds=%+v advance=%v column=%v,%v bottom=%v", stage, label.Value, label.Bounds, advance, test.left, test.content, bottom)
					}
					checked++
				}
				if checked == 0 {
					t.Fatalf("%s checked no native heading runs", stage)
				}
				for _, problem := range headless.CheckLayout(driver.Snapshot()) {
					if problem.Kind != headless.LayoutSpill {
						t.Errorf("%s unexpected layout problem: %+v", stage, problem)
					}
				}
			}
			clickID(t, driver, readModeID)
			check("reader")
			clickID(t, driver, renderedModeID)
			clickID(t, driver, renderedRows.At(0))
			if err := driver.SelectText(28, 28); err != nil {
				t.Fatal(err)
			}
			if err := driver.Type("X"); err != nil {
				t.Fatal(err)
			}
			check("active")
			if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
				t.Fatal(err)
			}
			if err := driver.Compose("中文预编辑中文预编辑中文预编辑中文预编辑"); err != nil {
				t.Fatal(err)
			}
			check("preedit")
			if state.session.Current().Text() != string(source) {
				t.Fatal("native geometry validation committed preedit")
			}
			if err := driver.Type("验"); err != nil {
				t.Fatal(err)
			}
			check("committed")
		})
	}
}

func TestMarkdownReferenceColumnWelcomeNativeBounds(t *testing.T) {
	engine := app.PlatformTextEngine()
	if engine == nil {
		if os.Getenv("MARKSTACK_REQUIRE_NATIVE_COLUMN") == "1" {
			t.Fatal("native column acceptance requires a platform text engine")
		}
		t.Skip("platform text engine unavailable")
	}
	source, err := os.ReadFile("../../examples/Welcome.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, viewport := range []int{740, 1200, 1605} {
		state := newModel()
		state.session.Current().Replace(string(source))
		var canvas gfx.Canvas
		canvas.SetTextEngine(engine)
		driver, err := app.NewDriver(newConfig(state), &canvas)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(driver.Close)
		if err := driver.Resize(viewport, 955); err != nil {
			t.Fatal(err)
		}
		for _, mode := range []gfx.ID{readModeID, renderedModeID} {
			clickID(t, driver, mode)
			left, content := state.markdownColumn(state.previewRect)
			checked := 0
			for _, label := range canvas.Texts() {
				if label.Value == "" || label.Style.Mono || label.Style.Color.Alpha <= 0 || !label.HasClip || label.ClipOf != state.previewRect {
					continue
				}
				advance := canvas.TextWidthStyle(label.Value, label.Style)
				if label.Bounds.X < left-0.01 || label.Bounds.X+advance > left+content+0.01 {
					t.Errorf("viewport %d native Welcome run exceeds column: %q bounds=%+v advance=%v column=%v,%v", viewport, label.Value, label.Bounds, advance, left, content)
				}
				checked++
			}
			if checked < 10 {
				t.Fatalf("viewport %d checked only %d native Welcome runs", viewport, checked)
			}
		}
		if state.session.Current().Text() != string(source) {
			t.Fatal("native Welcome validation changed source")
		}
	}
}

// TestMarkdownReferenceColumnThemeRoundTripKeepsMeasuredGeometry checks that the fixed
// reading column, block rhythm, table and code surfaces survive a light -> dark
// -> light round trip without drift, and that Night still uses its own measured
// 914px-based column at the reference viewport.
func TestMarkdownReferenceColumnThemeRoundTripKeepsMeasuredGeometry(t *testing.T) {
	state, driver := testDriver(t)
	state.sidebarWidth = 200
	state.session.Current().Replace("# Title\n\n- one\n- two\n\n> Quoted line\n\n| A | B |\n| --- | --- |\n| one | two |\n| three | four |\n\n```go\nfirst line\nsecond line\n```\n")
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, readModeID)
	capture := func() string {
		t.Helper()
		var output strings.Builder
		left, width := state.markdownColumn(state.previewRect)
		fmt.Fprintf(&output, "column=%v,%v preview=%+v\n", left, width, state.previewRect)
		for index, block := range state.preview.blocks {
			fmt.Fprintf(&output, "block %d kind=%v top=%v height=%v width=%v rule=%v heading=%d quoted=%v table=%v/%v/%v\n",
				index, block.kind, block.top, block.height, block.width, block.rule, block.heading, block.quoted,
				block.table.width, block.table.columns, block.table.rows)
		}
		snapshot := driver.Snapshot()
		for _, label := range snapshot.Texts {
			if label.Value == "" || !label.HasClip || label.ClipOf != state.previewRect {
				continue
			}
			fmt.Fprintf(&output, "text %q %+v %+v mask=%v\n", label.Value, label.Bounds, label.Style, label.CellWidth)
		}
		for _, primitive := range snapshot.Primitives {
			box := primitive.Bounds
			if box.Width <= 0 || box.Height <= 0 || box.X < state.previewRect.X || box.X+box.Width > state.previewRect.X+state.previewRect.Width || box.Y < state.previewRect.Y || box.Y+box.Height > state.previewRect.Y+state.previewRect.Height {
				continue
			}
			fmt.Fprintf(&output, "primitive kind=%v %+v color=%v radius=%v\n", primitive.Kind, box, primitive.Color, primitive.Radius)
		}
		return output.String()
	}
	light := capture()
	clickID(t, driver, themeToggleID)
	dark := capture()
	if dark == light {
		t.Fatal("theme toggle did not change the measured reading layout")
	}
	if left, width := state.markdownColumn(state.previewRect); left != 273 || width != 854 {
		t.Fatalf("Night reference column=%v,%v want 273,854", left, width)
	}
	for _, label := range driver.Snapshot().Texts {
		if label.Value == "Title" && (label.Style.Size != 40 || label.Bounds.Height != 44) {
			t.Errorf("Night heading size/line=%v/%v want 40/44", label.Style.Size, label.Bounds.Height)
		}
	}
	clickID(t, driver, themeToggleID)
	if got := capture(); got != light {
		t.Fatalf("light -> dark -> light round trip changed measured geometry:\n%s", got)
	}
}
