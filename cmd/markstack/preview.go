package main

import (
	"math"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/ui"
)

var markdownCodeFace = gfx.MonoStack(gfx.MonoFamily("Courier"), "Courier New", "Liberation Mono")

func markdownHeadingMetrics(level int, night bool) (size, lineHeight, readerScale float32) {
	if night {
		switch level {
		case 1:
			return 40, 44, 1.4
		case 2:
			return 26.08, 30, 1.25
		case 3:
			return 18.72, 24, 1.1
		case 4:
			return 17.92, 22, 1
		case 5:
			return 15.52, 20, 1
		case 6:
			return 14.88, 16, 1
		}
	}
	switch level {
	case 1:
		return 36, 43, 1.4
	case 2:
		return 28, 34, 1.25
	case 3:
		return 24, 34, 1.1
	case 4:
		return 20, 28, 1
	case 5, 6:
		return 16, 22, 1
	default:
		return 16, 26, 1
	}
}

func markdownHeadingColor(level int, night bool, fallback ui.Color) ui.Color {
	if !night || level == 0 {
		if !night && level == 6 {
			return ui.RGB(119, 119, 119)
		}
		return fallback
	}
	if level == 4 || level == 6 {
		return ui.RGB(255, 255, 255)
	}
	return ui.RGB(222, 222, 222)
}

func markdownParagraphGap(night bool, previousHeading int) float32 {
	if !night {
		return 16
	}
	if previousHeading == 6 {
		return 12
	}
	return 24
}

type previewBlock struct {
	view                     components.MarkdownView
	table                    previewTable
	image                    gfx.Bitmap
	imageAlt                 string
	imageWidth, imageHeight  float32
	inline                   []inlineDrawRun
	inlineSize               float32
	kind                     markdown.Kind
	heading                  int
	quoted                   bool
	top, height, rule, width float32
	base                     int
}

type previewView struct {
	readLayout         readPreviewLayout
	taskMarkers        []previewTaskMarker
	night              bool
	blocks             []previewBlock
	links              []string
	height             float32
	clickedImage       int
	clickedInline      markdownPreviewImage
	clickedInlineValid bool
	markedCanvas       gfx.Canvas
	markedMeasure      previewMeasurements
}

type readPreviewLayout struct {
	documentID         int
	path, source       string
	width, size, scale float32
	readingTheme       string
	night              bool
}

type previewTaskMarker struct {
	index   int
	checked bool
}

type previewMeasurements struct {
	canvas *gfx.Canvas
}

func (measure *previewMeasurements) Width(value string, size, weight float32) float32 {
	return measure.canvas.TextWidthWeight(value, size, weight)
}

func (measure *previewMeasurements) Caret(value string, size, weight float32, offset int) float32 {
	return measure.canvas.TextCaret(value, size, weight, offset)
}

func (measure *previewMeasurements) Selection(dst []gfx.TextSpan, value string, size, weight float32, start, end int) []gfx.TextSpan {
	return measure.canvas.TextSelection(dst, value, size, weight, start, end)
}

func (measure *previewMeasurements) OffsetAt(value string, size, weight, position float32) int {
	return measure.canvas.TextOffsetAt(value, size, weight, position)
}

func (measure *previewMeasurements) WidthStyle(value string, size, weight float32, italic bool) float32 {
	return measure.canvas.TextWidthStyle(value, gfx.TextStyle{Size: size, Weight: weight, Italic: italic})
}

func (measure *previewMeasurements) CaretStyle(value string, size, weight float32, italic bool, offset int) float32 {
	return measure.canvas.TextCaretStyle(value, gfx.TextStyle{Size: size, Weight: weight, Italic: italic}, offset)
}

func (measure *previewMeasurements) SelectionStyle(dst []gfx.TextSpan, value string, size, weight float32, italic bool, start, end int) []gfx.TextSpan {
	return measure.canvas.TextSelectionStyle(dst, value, gfx.TextStyle{Size: size, Weight: weight, Italic: italic}, start, end)
}

func (measure *previewMeasurements) OffsetAtStyle(value string, size, weight float32, italic bool, position float32) int {
	return measure.canvas.TextOffsetAtStyle(value, gfx.TextStyle{Size: size, Weight: weight, Italic: italic}, position)
}

func (view *previewView) Height() float32       { return view.height }
func (view *previewView) LinkCount() int        { return len(view.links) }
func (view *previewView) Link(index int) string { return view.links[index] }

func (view *previewView) CaretAtUpstream(canvas *gfx.Canvas, index int, value string, offset int) (ui.Rect, bool) {
	if canvas == nil || index < 0 || index >= len(view.blocks) || offset <= 0 || offset > len(value) {
		return ui.Rect{}, false
	}
	copyView := view.blocks[index].view
	previousOffset := gfx.GraphemeStart(value, offset-1)
	previous, previousOK := copyView.CaretAt(previousOffset)
	current, currentOK := copyView.CaretAt(offset)
	if !previousOK || !currentOK || previous.Y >= current.Y {
		return ui.Rect{}, false
	}
	view.markedCanvas.Reset()
	view.markedMeasure.canvas = canvas
	view.markedCanvas.SetTextEngine(&view.markedMeasure)
	selectionColor := ui.RGB(255, 255, 255)
	copyView.Paint(&view.markedCanvas, components.MarkdownPaint{
		Look:      components.MarkdownLook{Selection: selectionColor},
		Selection: components.MarkdownSelection{Anchor: previousOffset, Focus: offset},
	})
	local, ok := copyView.CaretAt(previousOffset)
	if !ok {
		return ui.Rect{}, false
	}
	for _, rectangle := range view.markedCanvas.Rectangles() {
		bounds := rectangle.Bounds
		if rectangle.Color != selectionColor || bounds.Y != local.Y || bounds.Width <= 0 {
			continue
		}
		ending := bounds.X
		if math.Abs(float64(local.X-bounds.X)) <= math.Abs(float64(local.X-bounds.X-bounds.Width)) {
			ending += bounds.Width
		}
		previous.X += ending - local.X
		return previous, true
	}
	return ui.Rect{}, false
}

func (view *previewView) reserveBlockHeight(index int, minimum float32) {
	block := &view.blocks[index]
	extra := max(float32(0), minimum-block.height)
	block.height += extra
	if block.rule > 0 {
		block.rule += extra
	}
	for following := index + 1; following < len(view.blocks); following++ {
		view.blocks[following].top += extra
	}
	view.height += extra
}

func (view *previewView) Layout(canvas *gfx.Canvas, style components.MarkdownStyle, document *markdown.Document, width float32, images map[int]markdownPreviewImage, mixed map[int][]inlinePreviewPart, activeTable *tableEditor) float32 {
	view.readLayout = readPreviewLayout{}
	for len(view.blocks) < len(document.Blocks) {
		view.blocks = append(view.blocks, previewBlock{})
	}
	view.blocks = view.blocks[:len(document.Blocks)]
	view.links = view.links[:0]
	view.height = 0
	for index := range document.Blocks {
		block := &document.Blocks[index]
		layout := &view.blocks[index]
		if view.night {
			gap := float32(0)
			if index > 0 {
				previous := &document.Blocks[index-1]
				heading := 0
				if previous.Kind == markdown.Heading {
					heading = previous.Level
				}
				gap = markdownParagraphGap(view.night, heading)
				if previous.Kind == markdown.Item && block.Kind == markdown.Item {
					gap = 8
				}
			}
			if block.Kind == markdown.Heading && block.Level == 1 {
				gap = 80
			}
			view.height += gap * style.Size / 16
		} else if index > 0 {
			previous := &document.Blocks[index-1]
			switch {
			case block.Kind == markdown.Heading && block.Level <= 2:
				view.height += 14
			case previous.Kind == markdown.Item && block.Kind == markdown.Item:
				view.height += 6
			default:
				view.height += markdownParagraphGap(view.night, 0)
			}
		}
		layout.kind, layout.top, layout.rule, layout.width = block.Kind, view.height, 0, width
		layout.heading = 0
		if block.Kind == markdown.Heading {
			layout.heading = block.Level
		}
		layout.quoted = block.Quote > 0
		layout.image, layout.imageAlt, layout.imageWidth, layout.imageHeight = gfx.Bitmap{}, "", 0, 0
		layout.inline = nil
		layout.inlineSize = style.Size
		layout.base = len(view.links)
		if parts, ok := mixed[index]; ok && len(parts) > 0 {
			layout.inline, layout.height = layoutInlineMedia(canvas, parts, style, width)
		} else if image, ok := images[index]; ok && image.bitmap.Width > 0 && image.bitmap.Height > 0 {
			bitmap := image.bitmap
			layout.image, layout.imageAlt = bitmap, image.alt
			scale := min(float32(1), width/float32(bitmap.Width), 320/float32(bitmap.Height))
			layout.imageWidth = float32(bitmap.Width) * scale
			layout.imageHeight = float32(bitmap.Height) * scale
			layout.height = layout.imageHeight + 12
		} else if block.Kind == markdown.Table {
			var pinned []float32
			if activeTable != nil && activeTable.renderedIndex == index && activeTable.layoutWidth == width {
				pinned = activeTable.columns
			}
			layout.height = layout.table.layout(canvas, style, document, block, width, pinned)
			for cellIndex := range layout.table.cells {
				cell := &layout.table.cells[cellIndex]
				cell.base = len(view.links) - layout.base
				for link := 0; link < cell.view.LinkCount(); link++ {
					view.links = append(view.links, cell.view.Link(link))
				}
			}
		} else {
			part := *document
			part.Blocks = document.Blocks[index : index+1]
			blockStyle := style
			scale := style.Size / 16
			if block.Kind == markdown.Heading {
				size, lineHeight, readerScale := markdownHeadingMetrics(block.Level, view.night)
				blockStyle.Size, blockStyle.LineHeight = size/readerScale*scale, lineHeight/readerScale*scale
			}
			layout.height = layout.view.Layout(canvas, blockStyle, &part, width)
			if !view.night && block.Kind == markdown.Heading && block.Level <= 2 {
				layout.rule = layout.height + 5
				layout.height += 6
			}
			for link := 0; link < layout.view.LinkCount(); link++ {
				view.links = append(view.links, layout.view.Link(link))
			}
		}
		view.height += layout.height
	}
	return view.height
}

func (view *previewView) Paint(canvas *gfx.Canvas, paint components.MarkdownPaint) {
	view.paint(canvas, paint, -1, -1, components.MarkdownSelection{}, nil, -1, -1)
}

func (view *previewView) PaintExcept(canvas *gfx.Canvas, paint components.MarkdownPaint, excluded int) {
	view.paint(canvas, paint, excluded, -1, components.MarkdownSelection{}, nil, -1, -1)
}

func (view *previewView) PaintSelected(canvas *gfx.Canvas, paint components.MarkdownPaint, index int, selection components.MarkdownSelection) {
	view.paint(canvas, paint, -1, index, selection, nil, -1, -1)
}

func (view *previewView) PaintMarked(canvas *gfx.Canvas, paint components.MarkdownPaint, index int, marked components.MarkdownSelection, color ui.Color) {
	if index < 0 || index >= len(view.blocks) || color.Alpha <= 0 {
		return
	}
	block := &view.blocks[index]
	paint.Y += block.top
	paint.Look = components.MarkdownLook{Selection: color}
	paint.Links = 0
	paint.Selection = marked
	view.markedCanvas.Reset()
	view.markedMeasure.canvas = canvas
	view.markedCanvas.SetTextEngine(&view.markedMeasure)
	block.view.Paint(&view.markedCanvas, paint)
	for _, rectangle := range view.markedCanvas.Rectangles() {
		if rectangle.Color == color {
			rectangle.Bounds.Y += rectangle.Bounds.Height - 2
			rectangle.Bounds.Height = 1
			canvas.Fill(rectangle.Bounds, color)
		}
	}
}

func (view *previewView) PaintSelectionRange(canvas *gfx.Canvas, paint components.MarkdownPaint, selection *renderedSelection) {
	view.paint(canvas, paint, -1, -1, components.MarkdownSelection{}, selection, -1, -1)
}

func (view *previewView) PaintExceptTableCell(canvas *gfx.Canvas, paint components.MarkdownPaint, blockIndex, cellIndex int) {
	view.paint(canvas, paint, -1, -1, components.MarkdownSelection{}, nil, blockIndex, cellIndex)
}

func (view *previewView) paint(canvas *gfx.Canvas, paint components.MarkdownPaint, excluded, selected int, selection components.MarkdownSelection, selectionRange *renderedSelection, tableIndex, cellIndex int) {
	view.clickedImage = -1
	view.clickedInlineValid = false
	appearance := markdownAppearanceFor(view.night, colors{})
	canvas.Clip(paint.Clip)
	defer canvas.Unclip()
	for index := range view.blocks {
		block := &view.blocks[index]
		top := paint.Y + block.top
		if top+block.height < paint.Clip.Y || top > paint.Clip.Y+paint.Clip.Height {
			continue
		}
		if block.rule > 0 {
			canvas.Fill(ui.Rect{X: paint.X, Y: top + block.rule, Width: block.width, Height: 1}, appearance.headingRule)
		}
		if index == excluded {
			continue
		}
		part := paint
		part.Y, part.Base = top, paint.Base+block.base
		part.Look.Text = markdownHeadingColor(block.heading, view.night, paint.Look.Text)
		if block.kind == markdown.Item && !block.quoted {
			part.Look.Muted = paint.Look.Text
		}
		if index == selected {
			part.Selection = selection
		} else if selectionRange != nil && block.kind != markdown.Table && block.kind != markdown.Code && index >= selectionRange.first && index <= selectionRange.last {
			start, end := 0, block.view.TextLen()
			if index == selectionRange.first {
				start = selectionRange.firstOffset
			}
			if index == selectionRange.last {
				end = selectionRange.lastOffset
			}
			part.Selection = components.MarkdownSelection{Anchor: start, Focus: end}
		}
		if block.image.Width > 0 {
			imageBounds := ui.Rect{X: paint.X, Y: top, Width: block.imageWidth, Height: block.imageHeight}
			canvas.DrawBitmap(block.image, imageBounds)
			canvas.HitWithOptions(previewImages.At(index), imageBounds, "查看图片 "+block.imageAlt, gfx.HitOptions{Role: gfx.RoleButton, Hint: "查看本地图片"})
			if canvas.Clicked(previewImages.At(index)) {
				view.clickedImage = index
			}
		} else if len(block.inline) > 0 {
			for _, run := range block.inline {
				bounds := run.bounds
				bounds.X += paint.X
				bounds.Y += top
				if run.bitmap.Width > 0 {
					canvas.DrawBitmap(run.bitmap, bounds)
					id := previewInlineImages.At(index*64 + run.partIndex)
					canvas.HitWithOptions(id, bounds, "查看图片 "+run.alt, gfx.HitOptions{Role: gfx.RoleButton, Hint: "查看本地图片"})
					if canvas.Clicked(id) {
						view.clickedInline = markdownPreviewImage{bitmap: run.bitmap, alt: run.alt}
						view.clickedInlineValid = true
					}
				} else {
					canvas.Label(run.text, bounds, gfx.TextStyle{Size: block.inlineSize, Color: paint.Look.Text})
				}
			}
		} else if block.kind == markdown.Table {
			part.Look.CodeFill = appearance.palette.sidebar
			skip := -1
			if index == tableIndex {
				skip = cellIndex
			}
			block.table.paint(canvas, part, skip)
		} else {
			if block.kind == markdown.Code {
				part.Look.Line = appearance.codeBorder
			}
			block.view.Paint(canvas, part)
		}
	}
}

type previewCell struct {
	document markdown.Document
	view     components.MarkdownView
	align    markdown.Align
	base     int
}

type previewTable struct {
	cells   []previewCell
	columns []float32
	rows    []float32
	width   float32
}

func (table *previewTable) layout(canvas *gfx.Canvas, style components.MarkdownStyle, document *markdown.Document, block *markdown.Block, width float32, pinned []float32) float32 {
	cells := document.Cells(block)
	for len(table.cells) < len(cells) {
		table.cells = append(table.cells, previewCell{})
	}
	table.cells = table.cells[:len(cells)]
	table.columns = make([]float32, block.Columns)
	table.rows = make([]float32, len(cells)/block.Columns)
	for index, source := range cells {
		cell := &table.cells[index]
		spans := document.CellSpans(source)
		setInlineDocument(&cell.document, spans, index < block.Columns)
		cell.align = source.Align
		var plain strings.Builder
		for _, span := range spans {
			plain.WriteString(span.Text)
		}
		weight := gfx.WeightRegular
		if index < block.Columns {
			weight = gfx.WeightSemibold
		}
		column := index % block.Columns
		table.columns[column] = max(table.columns[column], max(64, canvas.TextWidthWeight(plain.String(), style.Size, weight)+30))
	}
	var total float32
	for _, column := range table.columns {
		total += column
	}
	if len(pinned) == len(table.columns) && len(pinned) > 0 {
		copy(table.columns, pinned)
	} else if total > width {
		remaining, free := width, block.Columns
		fixed := make([]bool, block.Columns)
		for free > 0 {
			share, changed := remaining/float32(free), false
			for index, column := range table.columns {
				if !fixed[index] && column <= share {
					fixed[index], changed = true, true
					remaining -= column
					free--
				}
			}
			if !changed {
				break
			}
		}
		for index := range table.columns {
			if !fixed[index] {
				table.columns[index] = max(66, remaining/float32(free))
			}
		}
	} else if total < width {
		scale := width / total
		for index := range table.columns {
			table.columns[index] *= scale
		}
	}
	table.width = 0
	for _, column := range table.columns {
		table.width += column
	}
	for index := range table.cells {
		cell := &table.cells[index]
		height := cell.view.Layout(canvas, components.MarkdownStyle{Size: style.Size, LineHeight: style.LineHeight, MonoFace: style.MonoFace}, &cell.document, max(40, table.columns[index%block.Columns]-26))
		row := index / block.Columns
		table.rows[row] = max(table.rows[row], max(38, height+12))
	}
	var height float32
	for _, row := range table.rows {
		height += row
	}
	return height
}

func (table *previewTable) paint(canvas *gfx.Canvas, paint components.MarkdownPaint, skip int) {
	var offsetY float32
	for row, height := range table.rows {
		if row%2 == 0 {
			canvas.Fill(ui.Rect{X: paint.X, Y: paint.Y + offsetY, Width: table.width, Height: height}, paint.Look.CodeFill)
		}
		var offsetX float32
		for column, width := range table.columns {
			cellIndex := row*len(table.columns) + column
			if cellIndex == skip {
				offsetX += width
				continue
			}
			cell := &table.cells[cellIndex]
			part := paint
			part.X, part.Y, part.Base = paint.X+offsetX+13, paint.Y+offsetY+6, paint.Base+cell.base
			if cell.align != markdown.AlignLeft {
				cellWidth := float32(0)
				for _, block := range cell.document.Blocks {
					for _, span := range cell.document.Spans(&block) {
						cellWidth += canvas.TextWidthWeight(span.Text, 16, gfx.WeightRegular)
					}
				}
				part.X += tableTextInset(cell.align, width, cellWidth)
			}
			cell.view.Paint(canvas, part)
			offsetX += width
		}
		offsetY += height
		canvas.Fill(ui.Rect{X: paint.X, Y: paint.Y + offsetY - 1, Width: table.width, Height: 1}, paint.Look.Line)
	}
	canvas.Fill(ui.Rect{X: paint.X, Y: paint.Y, Width: table.width, Height: 1}, paint.Look.Line)
	canvas.Fill(ui.Rect{X: paint.X, Y: paint.Y, Width: 1, Height: offsetY}, paint.Look.Line)
	var right float32
	for _, column := range table.columns {
		right += column
		canvas.Fill(ui.Rect{X: paint.X + right - 1, Y: paint.Y, Width: 1, Height: offsetY}, paint.Look.Line)
	}
}

func tableTextInset(align markdown.Align, width, textWidth float32) float32 {
	if align == markdown.AlignLeft {
		return 0
	}
	free := max(0, width-26-textWidth)
	if align == markdown.AlignCenter {
		free /= 2
	}
	return free
}

func setInlineDocument(document *markdown.Document, spans []markdown.Span, header bool) {
	var seed strings.Builder
	for index := range spans {
		if index%2 == 0 {
			seed.WriteString("x")
		} else {
			seed.WriteString("**x**")
		}
	}
	document.Parse(seed.String())
	if len(document.Blocks) == 0 {
		return
	}
	output := document.Spans(&document.Blocks[0])
	copy(output, spans)
	if header {
		for index := range output {
			output[index].Style |= markdown.Bold
		}
	}
}
