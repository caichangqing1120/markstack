package main

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func (block *previewBlock) technicalOffsetAt(canvas *gfx.Canvas, point ui.Point) int {
	x, y := point.X-block.technicalOrigin.X, point.Y-block.technicalOrigin.Y
	runs := block.technical
	row := 0
	for row+1 < len(runs) && y >= runs[row].rowTop+runs[row].rowHeight {
		row++
	}
	for index := row; index < len(runs) && runs[index].rowTop == runs[row].rowTop; index++ {
		run := runs[index]
		if x <= run.bounds.X+run.bounds.Width {
			return run.start + canvas.TextOffsetAtStyle(run.text, run.style, x-run.bounds.X)
		}
		if index+1 == len(runs) || runs[index+1].rowTop != run.rowTop {
			return run.start + len(run.text)
		}
	}
	return runs[len(runs)-1].start + len(runs[len(runs)-1].text)
}

func (block *previewBlock) technicalCaret(canvas *gfx.Canvas, offset int, upstream bool) ui.Rect {
	runs := block.technical
	index := 0
	for index+1 < len(runs) && (offset > runs[index].start+len(runs[index].text) || !upstream && offset == runs[index+1].start) {
		index++
	}
	run := runs[index]
	position := max(0, min(len(run.text), offset-run.start))
	return ui.Rect{
		X: block.technicalOrigin.X + run.bounds.X + canvas.TextCaretStyle(run.text, run.style, position),
		Y: block.technicalOrigin.Y + run.rowTop, Width: 1, Height: run.rowHeight,
	}
}
