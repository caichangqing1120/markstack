package main

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

type fixedLayoutEngine struct{ gfx.TextEngine }

func (fixedLayoutEngine) Width(value string, size, weight float32) float32 {
	if value == "" {
		return 0
	}
	return 40
}

func TestNativeOverlapCheckUsesDrawingMetricsAndStillRejectsRealOverlap(t *testing.T) {
	var canvas gfx.Canvas
	canvas.SetTextEngine(fixedLayoutEngine{gfx.ReferenceTextEngine()})
	for _, scenario := range []struct {
		name    string
		secondX float32
		layer   uint16
		clipped bool
		want    int
	}{
		{"touching native spans", 60, 0, false, 0},
		{"real overlap", 55, 0, false, 1},
		{"different layers", 55, 1, false, 0},
		{"clipped span", 45, 0, true, 0},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			snapshot := app.Snapshot{Width: 400, Height: 100, Texts: []gfx.Text{
				{Value: "Title续", Bounds: ui.Rect{X: 20, Y: 20, Width: 40, Height: 30}, Style: gfx.TextStyle{Size: 36}, HasClip: scenario.clipped, ClipOf: ui.Rect{X: 20, Y: 20, Width: 20, Height: 30}},
				{Value: "粗", Bounds: ui.Rect{X: scenario.secondX, Y: 20, Width: 40, Height: 30}, Style: gfx.TextStyle{Size: 36}, Layer: scenario.layer},
			}}
			if problems := nativeTextOverlapProblems(&canvas, snapshot); len(problems) != scenario.want {
				t.Fatalf("native overlaps=%+v, want %d", problems, scenario.want)
			}
		})
	}
}

func nativeTextOverlapProblems(canvas *gfx.Canvas, snapshot app.Snapshot) []headless.LayoutProblem {
	type measuredText struct {
		label   gfx.Text
		visible ui.Rect
	}
	intersection := func(first, second ui.Rect) ui.Rect {
		left, top := max(first.X, second.X), max(first.Y, second.Y)
		return ui.Rect{
			X: left, Y: top,
			Width:  max(float32(0), min(first.X+first.Width, second.X+second.Width)-left),
			Height: max(float32(0), min(first.Y+first.Height, second.Y+second.Height)-top),
		}
	}
	var measured []measuredText
	for _, label := range snapshot.Texts {
		if label.Value == "" || label.CellWidth > 0 || label.RunCount > 0 || label.Style.Mono {
			continue
		}
		visible := label.Bounds
		visible.Width = canvas.TextWidthStyle(label.Value, label.Style)
		if label.HasClip {
			visible = intersection(visible, label.ClipOf)
		}
		if visible.Width > 0 && visible.Height > 0 {
			measured = append(measured, measuredText{label: label, visible: visible})
		}
	}
	var result []headless.LayoutProblem
	for first, previous := range measured {
		for _, next := range measured[first+1:] {
			if previous.label.Layer != next.label.Layer {
				continue
			}
			common := intersection(previous.visible, next.visible)
			if common.Width > 1 && common.Height > 1 {
				result = append(result, headless.LayoutProblem{
					Kind: headless.LayoutOverlap, Bounds: common, Text: previous.label.Value,
					Detail: fmt.Sprintf("native glyphs overlap %q", next.label.Value),
				})
			}
		}
	}
	return result
}
