package main

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/vivaui/viva/base"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/ui"
	"markstack/internal/markdownedit"
	"markstack/internal/vectorrender"
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

func markdownHeadingStyle(level int, night bool, baseSize float32) (size, lineHeight, readerScale float32) {
	size, lineHeight, readerScale = markdownHeadingMetrics(level, night)
	scale := baseSize / 16
	return size / readerScale * scale, lineHeight / readerScale * scale, readerScale
}

func markdownHeadingGeometry(level int, night bool, baseSize float32) (size, lineHeight float32) {
	size, lineHeight, readerScale := markdownHeadingStyle(level, night, baseSize)
	return size * readerScale, float32(int(lineHeight*readerScale + 0.5))
}

func markdownListInset(block *markdown.Block, baseSize, width float32) (native, extra float32) {
	switch {
	case block.Kind == markdown.Item:
		native = 24 + float32(block.Level)*20
	case block.Kind == markdown.Paragraph && block.Level > 0:
		native = 24 + float32(block.Level-1)*20
	}
	available := max(0, width-float32(block.Quote*14)-max(2*baseSize, width/3))
	return native, min(native*baseSize/16, available) - native
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
	view                      components.MarkdownView
	table                     previewTable
	image                     gfx.Bitmap
	imageAlt                  string
	imageWidth, imageHeight   float32
	inline                    []inlineDrawRun
	inlineSize                float32
	lineHeight                float32
	technical                 []technicalRun
	vector                    vectorrender.Scene
	vectorScale, editorHeight float32
	mathFallback              bool
	kind                      markdown.Kind
	number                    int
	heading                   int
	quoted                    bool
	top, height, rule, width  float32
	inset                     float32
	base, tableCellBase       int
}

type previewView struct {
	layoutKey          previewLayoutKey
	sourceBlocks       []markdownedit.Block
	mapping            []int
	taskMarkers        []previewTaskMarker
	appearance         markdownAppearance
	night              bool
	blocks             []previewBlock
	links              []string
	height             float32
	clickedImage       int
	clickedInline      markdownPreviewImage
	clickedInlineValid bool
	markedCanvas       gfx.Canvas
	markedMeasure      previewMeasurements
	formulas           map[*markdown.Span][]inlineFormula
	displayMath        map[int]bool
	editingInline      int
}

type previewLayoutKey struct {
	documentID                             int
	path, source                           string
	width, size, scale                     float32
	readingTheme, lineSpacing, editorWidth string
	night                                  bool
	mode                                   viewMode
	editingInline                          int
}

type previewTaskMarker struct {
	label   string
	index   int
	checked bool
}

func (state *model) parseEditableMarkdown(source string) {
	state.preview.layoutKey = previewLayoutKey{}
	state.parseMarkdownProjection(source)
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
		Selection: base.TextRange{Anchor: previousOffset, Focus: offset},
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
	if extra == 0 {
		return
	}
	view.layoutKey = previewLayoutKey{}
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
	view.layoutKey = previewLayoutKey{}
	for len(view.blocks) < len(document.Blocks) {
		view.blocks = append(view.blocks, previewBlock{})
	}
	view.blocks = view.blocks[:len(document.Blocks)]
	view.links = view.links[:0]
	view.height = 0
	tableCells := 0
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
		_, layout.inset = markdownListInset(block, style.Size, width)
		layout.number = block.Number
		layout.heading = 0
		if block.Kind == markdown.Heading {
			layout.heading = block.Level
		}
		layout.quoted = block.Quote > 0
		layout.image, layout.imageAlt, layout.imageWidth, layout.imageHeight = gfx.Bitmap{}, "", 0, 0
		layout.inline = nil
		layout.technical = nil
		layout.vector, layout.vectorScale, layout.editorHeight, layout.mathFallback = vectorrender.Scene{}, 0, 0, false
		layout.inlineSize = style.Size
		layout.lineHeight = style.LineHeight
		layout.base = len(view.links)
		layout.tableCellBase = tableCells
		if technicalBlock(block, view.displayMath[index]) {
			layout.vector, _ = technicalScene(canvas, block.Text, view.displayMath[index], style.Size)
			layout.mathFallback = view.displayMath[index] && layout.vector.Width == 0
		}
		if layout.vector.Width > 0 {
			layout.vectorScale = vectorFit(layout.vector, width-20)
			layout.height = float32(layout.vector.Height)*layout.vectorScale + 16
		} else if parts, ok := mixed[index]; ok && len(parts) > 0 {
			layout.inline, layout.height = layoutInlineMedia(canvas, parts, style, width)
		} else if image, ok := images[index]; ok && image.bitmap.Width > 0 && image.bitmap.Height > 0 {
			bitmap := image.bitmap
			layout.image, layout.imageAlt = bitmap, image.alt
			scale := min(float32(1), width/float32(bitmap.Width), 320/float32(bitmap.Height)) * markdownImageScale(image.ref.Title)
			layout.imageWidth = float32(bitmap.Width) * scale
			layout.imageHeight = float32(bitmap.Height) * scale
			layout.height = layout.imageHeight + 12
		} else if block.Kind == markdown.Table {
			var pinned []float32
			if activeTable != nil && activeTable.renderedIndex == index && activeTable.layoutWidth == width {
				pinned = activeTable.columns
			}
			layout.height = layout.table.layout(view, canvas, style, document, block, width, pinned)
			tableCells += len(layout.table.cells)
			for cellIndex := range layout.table.cells {
				cell := &layout.table.cells[cellIndex]
				cell.base = len(view.links) - layout.base
				if len(cell.technical) > 0 {
					view.registerTechnicalLinks(cell.technical)
				} else {
					for link := 0; link < cell.view.LinkCount(); link++ {
						view.links = append(view.links, cell.view.Link(link))
					}
				}
			}
		} else {
			part := *document
			part.Blocks = document.Blocks[index : index+1]
			blockStyle := style
			if block.Kind == markdown.Heading {
				blockStyle.Size, blockStyle.LineHeight, _ = markdownHeadingStyle(block.Level, view.night, style.Size)
			}
			if index != view.editingInline && len(view.formulas) != 0 {
				nativeInset, extra := markdownListInset(block, style.Size, width)
				inset := float32(block.Quote*14) + nativeInset + extra
				layout.technical, layout.height = view.layoutTechnicalInline(canvas, document.Spans(block), blockStyle, width, inset)
			}
			rawHeight := layout.view.Layout(canvas, blockStyle, &part, width-layout.inset)
			if len(layout.technical) > 0 {
				view.registerTechnicalLinks(layout.technical)
			} else {
				layout.height = rawHeight
				for link := 0; link < layout.view.LinkCount(); link++ {
					view.links = append(view.links, layout.view.Link(link))
				}
			}
			if block.Kind == markdown.Code {
				layout.height += codeHeaderHeight
			}
			if !view.night && block.Kind == markdown.Heading && block.Level <= 2 {
				layout.rule = layout.height + 5
				layout.height += 6
			}
		}
		view.height += layout.height
	}
	return view.height
}

func (view *previewView) firstVisible(top float32) int {
	return sort.Search(len(view.blocks), func(index int) bool {
		block := &view.blocks[index]
		return block.top+block.height >= top
	})
}

func (view *previewView) Paint(canvas *gfx.Canvas, paint components.MarkdownPaint) {
	view.paint(canvas, paint, -1, -1, base.TextRange{}, nil, -1, -1)
}

func paintCodeCard(canvas *gfx.Canvas, bounds ui.Rect, appearance markdownAppearance, active bool) {
	if active {
		ring := ui.Rect{X: bounds.X - 2, Y: bounds.Y - 2, Width: bounds.Width + 4, Height: bounds.Height + 4}
		canvas.Elevate(ring, 10, gfx.ShadowSM)
		canvas.Rounded(ring, 10, appearance.palette.accent)
	}
	canvas.RoundedBorder(bounds, 8, 1, appearance.codeBorder, appearance.codeFill)
	strip := ui.Rect{X: bounds.X + 1, Y: bounds.Y + 1, Width: bounds.Width - 2, Height: codeHeaderHeight - 2}
	canvas.Clip(strip)
	canvas.Rounded(bounds.Inset(gfx.All(1)), 7, appearance.codeFill.Mix(appearance.palette.border, 0.23))
	canvas.Unclip()
	divider := appearance.codeBorder
	if divider == appearance.codeFill {
		divider = appearance.palette.border
	}
	canvas.Fill(ui.Rect{X: bounds.X + 1, Y: bounds.Y + codeHeaderHeight - 1, Width: bounds.Width - 2, Height: 1}, divider)
}

func (view *previewView) PaintExcept(canvas *gfx.Canvas, paint components.MarkdownPaint, excluded int) {
	view.paint(canvas, paint, excluded, -1, base.TextRange{}, nil, -1, -1)
}

func (view *previewView) PaintSelected(canvas *gfx.Canvas, paint components.MarkdownPaint, index int, selection base.TextRange) {
	view.paint(canvas, paint, -1, index, selection, nil, -1, -1)
}

func (view *previewView) PaintMarked(canvas *gfx.Canvas, paint components.MarkdownPaint, index int, marked base.TextRange, color ui.Color) {
	if index < 0 || index >= len(view.blocks) || color.Alpha <= 0 {
		return
	}
	block := &view.blocks[index]
	paint.X += block.inset
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
	view.paint(canvas, paint, -1, -1, base.TextRange{}, selection, -1, -1)
}

func (view *previewView) PaintExceptTableCell(canvas *gfx.Canvas, paint components.MarkdownPaint, blockIndex, cellIndex int) {
	view.paint(canvas, paint, -1, -1, base.TextRange{}, nil, blockIndex, cellIndex)
}

func (view *previewView) paint(canvas *gfx.Canvas, paint components.MarkdownPaint, excluded, selected int, selection base.TextRange, selectionRange *renderedSelection, tableIndex, cellIndex int) {
	view.clickedImage = -1
	view.clickedInlineValid = false
	appearance := view.appearance
	canvas.Clip(paint.Clip)
	defer canvas.Unclip()
	for index := view.firstVisible(paint.Clip.Y - paint.Y); index < len(view.blocks); index++ {
		block := &view.blocks[index]
		top := paint.Y + block.top
		if top > paint.Clip.Y+paint.Clip.Height {
			break
		}
		if top+block.height < paint.Clip.Y {
			continue
		}
		if block.rule > 0 {
			canvas.Fill(ui.Rect{X: paint.X, Y: top + block.rule, Width: block.width, Height: 1}, appearance.headingRule)
		}
		if block.quoted && appearance.quoteFill.Alpha > 0 {
			canvas.Fill(ui.Rect{X: paint.X, Y: top, Width: block.width, Height: block.height}, appearance.quoteFill)
		}
		if index == excluded {
			continue
		}
		part := paint
		part.Y, part.Base = top, paint.Base+block.base
		if block.kind == markdown.Code {
			part.Y += codeHeaderHeight
		}
		part.Look.Text = appearance.headingColor(block.heading, view.night)
		if block.quoted {
			part.Look.Line = appearance.quoteBar
		}
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
			part.Selection = base.TextRange{Anchor: start, Focus: end}
		}
		if block.vector.Width > 0 {
			left := paint.X + (block.width-float32(block.vector.Width)*block.vectorScale)/2
			paintVector(canvas, block.vector, left, top+8, block.vectorScale, appearance, part.Look.Text)
		} else if len(block.technical) > 0 {
			if block.quoted {
				canvas.Fill(ui.Rect{X: paint.X + 2, Y: top, Width: 3, Height: block.height}, part.Look.Line)
			}
			if block.kind == markdown.Item {
				paintMarkdownListMarker(canvas, block.number, part.X+block.technical[0].bounds.X, part.Y, block.inlineSize, block.lineHeight, part.Look.Muted)
			}
			paintTechnicalInline(canvas, block.technical, part, appearance)
		} else if block.image.Width > 0 {
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
						view.clickedInline = run.markdownPreviewImage
						view.clickedInlineValid = true
					}
				} else {
					canvas.Label(run.text, bounds, gfx.TextStyle{Size: block.inlineSize, Color: paint.Look.Text})
				}
			}
		} else if block.kind == markdown.Table {
			part.Look.Line = appearance.tableBorder
			skip := -1
			if index == tableIndex {
				skip = cellIndex
			}
			block.table.paint(canvas, part, skip, appearance)
		} else {
			part.X += block.inset
			if block.kind == markdown.Code {
				paintCodeCard(canvas, ui.Rect{X: paint.X, Y: top, Width: block.width, Height: block.height}, appearance, false)
				part.Look.Line, part.Look.CodeFill = gfx.Color{}, gfx.Color{}
				if block.mathFallback {
					canvas.Label("不支持的公式 · LaTeX", ui.Rect{X: part.X + 10, Y: top, Width: block.width - 20, Height: codeHeaderHeight}, gfx.TextStyle{Size: 12, Color: part.Look.Muted})
				}
			}
			block.view.Paint(canvas, part)
		}
	}
}

func paintMarkdownListMarker(canvas *gfx.Canvas, number int, textX, y, size, lineHeight float32, color gfx.Color) {
	marker := "•"
	if number > 0 {
		marker = strconv.Itoa(number) + "."
	}
	width := canvas.TextWidthWeight(marker, size, gfx.WeightRegular)
	canvas.Label(marker, ui.Rect{X: textX - 6 - width, Y: y, Width: width + 2, Height: lineHeight}, gfx.TextStyle{Size: size, Color: color})
}

type previewCell struct {
	document       markdown.Document
	view           components.MarkdownView
	align          markdown.Align
	base           int
	technical      []technicalRun
	technicalWidth float32
}

type previewTable struct {
	cells       []previewCell
	columns     []float32
	rows        []float32
	width, size float32
}

func (table *previewTable) layout(view *previewView, canvas *gfx.Canvas, style components.MarkdownStyle, document *markdown.Document, block *markdown.Block, width float32, pinned []float32) float32 {
	table.size = style.Size
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
		cellWidth := max(40, table.columns[index%block.Columns]-26)
		height := cell.view.Layout(canvas, components.MarkdownStyle{Size: style.Size, LineHeight: style.LineHeight, MonoFace: style.MonoFace}, &cell.document, cellWidth)
		spans := document.CellSpans(cells[index])
		var technicalHeight float32
		cell.technical, technicalHeight = view.layoutTechnicalInline(canvas, spans, style, cellWidth, 0)
		cell.technicalWidth = 0
		for _, run := range cell.technical {
			cell.technicalWidth = max(cell.technicalWidth, run.bounds.X+run.bounds.Width)
		}
		if len(cell.technical) > 0 {
			height = technicalHeight
		}
		row := index / block.Columns
		table.rows[row] = max(table.rows[row], max(38, height+12))
	}
	var height float32
	for _, row := range table.rows {
		height += row
	}
	return height
}

func (table *previewTable) paint(canvas *gfx.Canvas, paint components.MarkdownPaint, skip int, appearance markdownAppearance) {
	var offsetY float32
	for row, height := range table.rows {
		if row%2 == 0 {
			fill := appearance.tableStripe
			if row == 0 {
				fill = appearance.tableHeader
			}
			canvas.Fill(ui.Rect{X: paint.X, Y: paint.Y + offsetY, Width: table.width, Height: height}, fill)
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
			if len(cell.technical) > 0 {
				part.X += tableTextInset(cell.align, width, cell.technicalWidth)
				paintTechnicalInline(canvas, cell.technical, part, appearance)
				offsetX += width
				continue
			}
			if cell.align != markdown.AlignLeft {
				cellWidth := float32(0)
				weight := gfx.WeightRegular
				if row == 0 {
					weight = gfx.WeightSemibold
				}
				for _, block := range cell.document.Blocks {
					for _, span := range cell.document.Spans(&block) {
						cellWidth += canvas.TextWidthWeight(span.Text, table.size, weight)
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
