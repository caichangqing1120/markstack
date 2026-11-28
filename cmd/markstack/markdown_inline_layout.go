package main

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

type inlineDrawRun struct {
	partIndex int
	row       int
	text      string
	markdownPreviewImage
	bounds ui.Rect
}

func layoutInlineMedia(canvas *gfx.Canvas, parts []inlinePreviewPart, style components.MarkdownStyle, width float32) ([]inlineDrawRun, float32) {
	lineHeight := max(style.LineHeight, style.Size*1.4)
	rowHeights := []float32{lineHeight}
	var runs []inlineDrawRun
	row, x := 0, float32(0)
	nextRow := func() {
		row++
		rowHeights = append(rowHeights, lineHeight)
		x = 0
	}
	textStyle := gfx.TextStyle{Size: style.Size, Weight: gfx.WeightRegular}
	for partIndex, part := range parts {
		if part.bitmap.Width > 0 && part.bitmap.Height > 0 {
			scale := min(float32(1), width/float32(part.bitmap.Width), 260/float32(part.bitmap.Height)) * markdownImageScale(part.ref.Title)
			imageWidth, imageHeight := float32(part.bitmap.Width)*scale, float32(part.bitmap.Height)*scale
			if x > 0 && x+imageWidth > width {
				nextRow()
			}
			runs = append(runs, inlineDrawRun{partIndex: partIndex, row: row, markdownPreviewImage: part.markdownPreviewImage,
				bounds: ui.Rect{X: x, Width: imageWidth, Height: imageHeight}})
			rowHeights[row] = max(rowHeights[row], imageHeight+8)
			x += imageWidth + 8
			continue
		}
		for from := 0; from < len(part.text); {
			segment, next := canvas.WrapRow(part.text, from, style.Size, gfx.WeightRegular, max(1, width-x))
			if segment.End == segment.Start {
				if next < 0 {
					break
				}
				from = next
				nextRow()
				continue
			}
			value := part.text[segment.Start:segment.End]
			measured := htmlTextAdvance(canvas, value, textStyle)
			if measured > width-x+0.5 && x > 0 {
				nextRow()
				continue
			}
			runs = append(runs, inlineDrawRun{partIndex: partIndex, row: row, text: value,
				bounds: ui.Rect{X: x, Width: min(max(1, width-x), measured+2), Height: lineHeight}})
			x += measured + 2
			if next < 0 {
				break
			}
			from = next
			nextRow()
		}
	}
	y := float32(0)
	rowY := make([]float32, len(rowHeights))
	for index, height := range rowHeights {
		rowY[index] = y
		y += height
	}
	for index := range runs {
		row := runs[index].row
		runs[index].bounds.Y = rowY[row] + rowHeights[row] - runs[index].bounds.Height
	}
	return runs, max(lineHeight, y)
}
