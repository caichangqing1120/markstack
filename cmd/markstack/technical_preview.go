package main

import (
	"crypto/rand"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/ui"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	mdtext "github.com/yuin/goldmark/text"
	"markstack/internal/diagramrender"
	"markstack/internal/markdownedit"
	"markstack/internal/mathrender"
	"markstack/internal/vectorrender"
)

var technicalParser = goldmark.New(goldmark.WithExtensions(extension.GFM, mathrender.Markdown{}))

type technicalProjection struct {
	start, end   int
	projectedEnd int
	display      *mathrender.Display
}

type inlineFormula struct {
	tail, length int
	body         string
}

func (state *model) parseMarkdownProjection(source string) {
	state.preview.formulas, state.preview.displayMath = parseTechnicalMarkdown(&state.parsed, source, true)
}

func parseTechnicalMarkdown(document *markdown.Document, source string, trackFormulas bool) (map[*markdown.Span][]inlineFormula, map[int]bool) {
	if !strings.Contains(source, "$") {
		document.Parse(source)
		return nil, nil
	}
	reader := mdtext.NewReader([]byte(source))
	if strings.HasPrefix(source, "\xef\xbb\xbf") {
		reader.Advance(3)
	}
	var ranges []technicalProjection
	_ = ast.Walk(technicalParser.Parser().Parse(reader), func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			switch typed := node.(type) {
			case *mathrender.Inline:
				if node.Parent().Kind() != ast.KindImage {
					ranges = append(ranges, technicalProjection{start: typed.Range.Start, end: typed.Range.Stop})
				}
			case *mathrender.Display:
				ranges = append(ranges, technicalProjection{start: typed.Start, end: typed.End, display: typed})
			}
		}
		return ast.WalkContinue, nil
	})
	if len(ranges) == 0 {
		document.Parse(source)
		return nil, nil
	}
	sort.Slice(ranges, func(a, b int) bool { return ranges[a].start < ranges[b].start })
	marker := "$\ue000" + rand.Text() + "\ue000"
	var projected strings.Builder
	var projectedStarts []int
	position := 0
	for index := range ranges {
		item := &ranges[index]
		projected.WriteString(source[position:item.start])
		projectedStarts = append(projectedStarts, projected.Len())
		if item.display != nil {
			projected.WriteString("~~~\n~~~")
		} else {
			projected.WriteString(marker + strconv.Itoa(index) + "$")
		}
		item.projectedEnd = projected.Len()
		position = item.end
	}
	projected.WriteString(source[position:])
	document.Parse(projected.String())
	var formulaSpans map[*markdown.Span][]inlineFormula
	var displayBlocks map[int]bool
	if trackFormulas {
		formulaSpans = make(map[*markdown.Span][]inlineFormula)
		displayBlocks = make(map[int]bool)
	}
	mapOffset := func(offset int) int {
		previous := sort.Search(len(ranges), func(index int) bool { return ranges[index].projectedEnd > offset }) - 1
		if previous < 0 {
			return offset
		}
		return offset + ranges[previous].end - ranges[previous].projectedEnd
	}
	restore := func(spans []markdown.Span) {
		for index := range spans {
			span := &spans[index]
			if !strings.Contains(span.Text, marker) {
				continue
			}
			var value strings.Builder
			var formulas []inlineFormula
			remaining := span.Text
			for {
				prefix, rest, found := strings.Cut(remaining, marker)
				value.WriteString(prefix)
				if !found {
					break
				}
				digits, tail, found := strings.Cut(rest, "$")
				itemIndex, err := strconv.Atoi(digits)
				if !found || err != nil || itemIndex < 0 || itemIndex >= len(ranges) || ranges[itemIndex].display != nil {
					value.WriteString(marker + rest)
					break
				}
				item := ranges[itemIndex]
				raw := source[item.start:item.end]
				if trackFormulas {
					formulas = append(formulas, inlineFormula{tail: value.Len(), length: len(raw), body: raw[1 : len(raw)-1]})
				}
				value.WriteString(raw)
				remaining = tail
			}
			span.Text = value.String()
			for index := range formulas {
				formulas[index].tail = len(span.Text) - formulas[index].tail
			}
			if len(formulas) > 0 {
				formulaSpans[span] = formulas
			}
		}
	}
	for index := range document.Blocks {
		block := &document.Blocks[index]
		start := sort.SearchInts(projectedStarts, block.Start)
		if start < len(ranges) && block.Start == projectedStarts[start] && ranges[start].display != nil && block.Kind == markdown.Code {
			item := ranges[start].display
			value := strings.ReplaceAll(source[item.BodyStart:item.BodyEnd], "\r\n", "\n")
			if !item.SingleLine {
				value = strings.TrimSuffix(value, "\n")
			}
			block.Text, block.Info = value, "LaTeX"
			if trackFormulas {
				displayBlocks[index] = true
			}
		}
		block.Start, block.End = mapOffset(block.Start), mapOffset(block.End)
		restore(document.Spans(block))
		for _, cell := range document.Cells(block) {
			restore(document.CellSpans(cell))
		}
	}
	return formulaSpans, displayBlocks
}

func nativeMeasure(canvas *gfx.Canvas) vectorrender.Measure {
	return func(value string, size float64) float64 { return float64(canvas.TextWidth(value, float32(size))) }
}

func technicalScene(canvas *gfx.Canvas, value string, math bool, size float32) (vectorrender.Scene, error) {
	if math {
		return mathrender.Layout(value, float64(size)*1.3, nativeMeasure(canvas))
	}
	return diagramrender.Layout(value, nativeMeasure(canvas))
}

func technicalBlock(block *markdown.Block, math bool) bool {
	return math || block.Kind == markdown.Code && strings.EqualFold(markdownedit.FenceLanguage(block.Info), "mermaid")
}

func vectorFit(scene vectorrender.Scene, width float32) float32 {
	return min(1, max(1, width)/float32(scene.Width))
}

func paintVector(canvas *gfx.Canvas, scene vectorrender.Scene, x, y, scale float32, appearance markdownAppearance, textColor ui.Color) {
	palette := appearance.palette
	stroke := palette.muted
	fill := appearance.codeFill
	width, height := float32(scene.Width), float32(scene.Height)
	for _, op := range scene.Ops {
		left, baseline := x+float32(op.At.X)*scale, y+float32(op.At.Y)*scale
		bounds := ui.Rect{X: left, Y: baseline, Width: float32(op.Width) * scale, Height: float32(op.Height) * scale}
		size := float32(op.Size) * scale
		switch op.Kind {
		case vectorrender.Text:
			if op.Label {
				canvas.Rounded(ui.Rect{X: left - 3*scale, Y: baseline - size, Width: bounds.Width + 6*scale, Height: size + 4*scale}, 2*scale, palette.background)
			}
			canvas.Label(op.Text, ui.Rect{X: left, Y: baseline - size*.89, Width: bounds.Width + 3, Height: size * 1.15}, gfx.TextStyle{Size: size, Color: textColor})
		case vectorrender.Rect:
			canvas.RoundedBorder(bounds, float32(op.Radius)*scale, size, stroke, fill)
		case vectorrender.Line, vectorrender.Polygon:
			builder := canvas.PathBuilder()
			builder.MoveTo(float32(op.Points[0].X), float32(op.Points[0].Y))
			for _, point := range op.Points[1:] {
				builder.LineTo(float32(point.X), float32(point.Y))
			}
			style := gfx.PathStyle{Stroke: stroke, StrokeWidth: float32(op.Size), Dashed: op.Dashed}
			if op.Kind == vectorrender.Polygon {
				builder.Close()
				style.Fill = fill
				if op.Size == 0 {
					style.Fill = stroke
					style.Stroke = ui.Color{}
				}
			}
			canvas.DrawPath(builder.Path(width, height), ui.Rect{X: x, Y: y, Width: width * scale, Height: height * scale}, style)
		}
	}
}

type technicalRun struct {
	text, link             string
	linkIndex              int
	style                  gfx.TextStyle
	bounds                 ui.Rect
	scene                  vectorrender.Scene
	scale                  float32
	strike, mono, fallback bool
}

func (view *previewView) layoutTechnicalInline(canvas *gfx.Canvas, spans []markdown.Span, style components.MarkdownStyle, width, inset float32) ([]technicalRun, float32) {
	hasMath := false
	for index := range spans {
		hasMath = hasMath || len(view.formulas[&spans[index]]) > 0
	}
	if !hasMath {
		return nil, 0
	}
	width = max(inset+1, width)
	var runs []technicalRun
	x, y := inset, float32(0)
	start := 0
	baseline, descent := style.LineHeight*.69, style.LineHeight*.31
	flush := func() {
		for index := start; index < len(runs); index++ {
			runs[index].bounds.Y += y + baseline
		}
		start, x, y = len(runs), inset, y+baseline+descent
		baseline, descent = style.LineHeight*.69, style.LineHeight*.31
	}
	push := func(run technicalRun, up, down float32) {
		if x > inset && x+run.bounds.Width > width {
			flush()
		}
		if x == inset && run.text != "" && strings.TrimSpace(run.text) == "" {
			return
		}
		run.bounds.X, run.bounds.Y = x, -up
		baseline, descent = max(baseline, up), max(descent, down)
		x += run.bounds.Width
		runs = append(runs, run)
	}
	for index := range spans {
		span := &spans[index]
		font := renderedSpanStyle(*span, style.Size)
		if span.Style&markdown.Mono != 0 {
			font = gfx.TextStyle{Size: style.Size, Mono: true, MonoFace: style.MonoFace}
		}
		base := technicalRun{style: font, link: span.Link, strike: span.Style&markdown.Strike != 0, mono: span.Style&markdown.Mono != 0}
		addText := func(value string, fallback bool) {
			for value != "" {
				character, length := utf8.DecodeRuneInString(value)
				if character == '\n' {
					flush()
					value = value[length:]
					continue
				}
				if !unicode.IsSpace(character) && !unicode.Is(unicode.Han, character) {
					for length < len(value) {
						next, bytes := utf8.DecodeRuneInString(value[length:])
						if unicode.IsSpace(next) || unicode.Is(unicode.Han, next) {
							break
						}
						length += bytes
					}
				}
				piece := value[:length]
				run := base
				run.fallback = fallback
				if fallback {
					run.style = gfx.TextStyle{Size: style.Size, Mono: true, MonoFace: style.MonoFace}
					run.mono = true
				}
				for _, row := range canvas.WrapRows(nil, piece, run.style.Size, run.style.Weight, width-inset) {
					run.text = piece[row.Start:row.End]
					run.bounds = ui.Rect{Width: canvas.TextWidthStyle(run.text, run.style), Height: style.LineHeight}
					push(run, style.LineHeight*.69, style.LineHeight*.31)
				}
				value = value[length:]
			}
		}
		position := 0
		for _, formula := range view.formulas[span] {
			from := len(span.Text) - formula.tail
			if from < position || from+formula.length > len(span.Text) {
				continue
			}
			addText(span.Text[position:from], false)
			position = from + formula.length
			scene, err := mathrender.Layout(formula.body, float64(style.Size), nativeMeasure(canvas))
			if err != nil {
				addText(span.Text[from:position]+" [不支持的公式]", true)
				continue
			}
			scale := vectorFit(scene, width-inset)
			run := technicalRun{scene: scene, scale: scale, link: span.Link,
				bounds: ui.Rect{Width: float32(scene.Width) * scale, Height: float32(scene.Height) * scale}}
			push(run, float32(scene.Baseline)*scale, float32(scene.Height-scene.Baseline)*scale)
		}
		addText(span.Text[position:], false)
	}
	flush()
	return runs, y
}

func (view *previewView) registerTechnicalLinks(runs []technicalRun) {
	for index := range runs {
		if runs[index].link != "" {
			runs[index].linkIndex = len(view.links)
			view.links = append(view.links, runs[index].link)
		}
	}
}

func paintTechnicalInline(canvas *gfx.Canvas, runs []technicalRun, paint components.MarkdownPaint, appearance markdownAppearance) {
	for _, run := range runs {
		bounds := run.bounds
		bounds.X, bounds.Y = bounds.X+paint.X, bounds.Y+paint.Y
		color := paint.Look.Text
		if run.link != "" {
			color = paint.Look.Link
			if !paint.Links.IsZero() {
				canvas.HitWithOptions(paint.Links.At(run.linkIndex), bounds, run.link, gfx.HitOptions{Role: gfx.RoleLink})
			}
		}
		if run.scene.Width > 0 {
			paintVector(canvas, run.scene, bounds.X, bounds.Y, run.scale, appearance, color)
			continue
		}
		if run.mono {
			canvas.Rounded(bounds, 2, paint.Look.InlineCodeFill)
		}
		if run.fallback {
			color = paint.Look.Muted
		}
		run.style.Color = color
		bounds.Width += 2
		canvas.Label(run.text, bounds, run.style)
		if run.strike {
			canvas.Fill(ui.Rect{X: bounds.X, Y: bounds.Y + bounds.Height*.5, Width: run.bounds.Width, Height: 1}, color)
		}
	}
}

var technicalPreviewRows = controlTree.Sub(page, controlTree.Name("technical-preview-rows"))

func (state *model) activateTechnicalPreview(index int) gfx.EventResult {
	if state.mode != readMode && state.mode != splitMode || index < 0 || index >= len(state.preview.blocks) || state.preview.blocks[index].vector.Width == 0 && !state.preview.displayMath[index] {
		return gfx.EventResult{}
	}
	if focus := state.pendingCompositionFocus(); !focus.IsZero() {
		return gfx.EventResult{Handled: true, Focus: focus}
	}
	state.mode = renderedMode
	return state.activateCodeBlock(index)
}
