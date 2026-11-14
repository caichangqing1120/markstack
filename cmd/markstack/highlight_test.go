package main

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func TestMarkdownSourceHighlightKeepsRuneBoundariesAndOrder(t *testing.T) {
	palette := colors{accent: ui.RGB(52, 120, 159), muted: ui.RGB(120, 128, 134)}
	for _, source := range []string{
		"# 中文标题", "> 引用", "- 项目", "```go", "前缀 `代码` 和 [链接](https://example.com)",
	} {
		runs := markdownSourceRuns(source, palette, nil)
		if len(runs) == 0 {
			t.Fatalf("no highlighting for %q", source)
		}
		previous := 0
		for _, run := range runs {
			if run.Start < previous || run.End <= run.Start || run.End > len(source) || !utf8.ValidString(source[:run.Start]) || !utf8.ValidString(source[:run.End]) {
				t.Fatalf("invalid ordered byte range for %q: %+v", source, runs)
			}
			previous = run.End
		}
	}
}

func TestNativeHighlightedLineCoversEveryVisiblePixel(t *testing.T) {
	state := newModel()
	source := "**Bold** and `code`"
	state.session.Current().Replace(source)
	canvas := &gfx.Canvas{}
	engine := app.PlatformTextEngine()
	if engine == nil {
		t.Skip("platform text engine unavailable")
	}
	canvas.SetTextEngine(engine)
	driver, err := app.NewDriver(newConfig(state), canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	labels := 0
	for _, drawn := range driver.Snapshot().Texts {
		if drawn.Value != source || drawn.RunCount == 0 {
			continue
		}
		labels++
		covered, column := 0, int32(0)
		for _, run := range driver.Snapshot().TextRuns(drawn) {
			if run.Column != column {
				t.Fatalf("highlight left a grid gap at column %d", column)
			}
			covered += int(run.Bytes)
			column += run.Cells
		}
		if covered != len(source) {
			t.Fatalf("highlight covers %d of %d bytes", covered, len(source))
		}
	}
	if labels != 1 {
		t.Fatalf("highlight painted %d grid labels for one source line", labels)
	}
}

func TestMarkdownSourceHighlightDoesNotChangeTextOrSelection(t *testing.T) {
	state, driver := testDriver(t)
	source := "# 标题\n\n**重点** 和 `行内代码`"
	state.session.Current().Replace(source)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != source {
		t.Fatalf("highlight modified source: %q", got)
	}
	var highlighted bool
	_, palette := state.look()
	for _, drawn := range driver.Snapshot().Texts {
		if strings.Contains(drawn.Value, "标题") {
			for _, run := range driver.Snapshot().TextRuns(drawn) {
				if run.Color == palette.accent {
					highlighted = true
				}
			}
		}
	}
	if !highlighted {
		t.Fatal("heading source did not use accent highlight")
	}
}
