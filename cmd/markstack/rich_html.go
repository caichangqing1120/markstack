package main

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"markstack/internal/richedit"
	"markstack/internal/session"
)

type richHTMLView struct {
	parsed           richedit.Document
	documentID       int
	source           string
	rect             ui.Rect
	height           float32
	scroll           float32
	blockY           []float32
	active           bool
	blockIndex       int
	leafIndex        int
	leaf             richedit.Leaf
	input            *text.Editor
	textInput        app.TextInput
	inputRect        ui.Rect
	segments         []htmlTextSegment
	wrapCanvas       *gfx.Canvas
	lastPointerUp    ui.Point
	clickPoint       ui.Point
	placeCaret       bool
	goalX            float32
	goalCaret        int
	goalActive       bool
	dragging         bool
	blockedSelection bool
	links            []string
	images           map[string]gfx.Bitmap
	leafTargets      []htmlLeafTarget
	crossSelection   *htmlCrossSelection
	crossAnchor      int
	structureUndo    []htmlStructureEdit
	structureRedo    []htmlStructureEdit
	workspaceMatch   *renderedMatch
	scrollPending    bool
	scrollBlock      int
}

func (view *richHTMLView) sync(current *session.Document) {
	if view.documentID == current.ID && view.source == current.Text() {
		return
	}
	view.documentID, view.source = current.ID, current.Text()
	view.parsed = richedit.Parse([]byte(view.source))
	view.scroll = 0
	view.active = false
	view.images = nil
	view.workspaceMatch = nil
	view.scrollPending = false
	view.crossSelection = nil
}

func (state *model) paintRichHTML(canvas *gfx.Canvas, theme ui.Theme, palette colors, bounds ui.Rect) {
	current := state.session.Current()
	if state.mode == editMode {
		state.html.rect = ui.Rect{}
		state.html.active = false
		state.html.crossSelection = nil
		state.paintSource(canvas, theme, palette, bounds)
		return
	}
	state.html.sync(current)
	if state.html.workspaceMatch != nil && (state.html.rect.Width != bounds.Width || state.html.rect.Height != bounds.Height) {
		state.html.scrollBlock, state.html.scrollPending = state.html.workspaceMatch.block, true
	}
	state.html.rect = bounds
	state.html.inputRect = ui.Rect{}
	state.html.segments = state.html.segments[:0]
	state.html.leafTargets = state.html.leafTargets[:0]
	state.html.links = state.html.links[:0]
	width := min(float32(900), max(80, bounds.Width-72))
	left := bounds.X + (bounds.Width-width)/2
	y := bounds.Y + 44 - state.html.scroll
	if cap(state.html.blockY) < len(state.html.parsed.Blocks) {
		state.html.blockY = make([]float32, len(state.html.parsed.Blocks))
	} else {
		state.html.blockY = state.html.blockY[:len(state.html.parsed.Blocks)]
	}
	canvas.Clip(bounds)
	defer canvas.Unclip()
	leafBase := 0
	for index := 0; index < len(state.html.parsed.Blocks); {
		block := &state.html.parsed.Blocks[index]
		if block.Kind == richedit.TableCell {
			end := index
			for end < len(state.html.parsed.Blocks) && state.html.parsed.Blocks[end].Kind == richedit.TableCell {
				end++
			}
			y += state.paintRichHTMLTable(canvas, palette, index, end, leafBase, left, y, width) + 18
			for cell := index; cell < end; cell++ {
				leafBase += len(state.html.parsed.Blocks[cell].Leaves)
			}
			index = end
			continue
		}
		state.html.blockY[index] = y - (bounds.Y - state.html.scroll)
		y += state.paintRichHTMLBlock(canvas, palette, block, index, leafBase, left, y, width) + 17
		leafBase += len(block.Leaves)
		index++
	}
	state.html.height = max(0, y-(bounds.Y-state.html.scroll)+36)
	if state.html.scrollPending && state.html.scrollBlock >= 0 && state.html.scrollBlock < len(state.html.blockY) {
		state.html.scroll = max(0, min(state.html.blockY[state.html.scrollBlock]-80, max(0, state.html.height-bounds.Height)))
		state.html.scrollPending = false
		state.invalidator.Invalidate()
	}
}

func (state *model) paintRichHTMLBlock(canvas *gfx.Canvas, palette colors, block *richedit.Block, blockIndex, leafBase int, left, top, width float32) float32 {
	scale := float32(1)
	if state.mode == readMode {
		scale = state.readingSize() / 16
	}
	size, lineHeight := 16*scale, 27*scale
	indent := float32(0)
	switch block.Kind {
	case richedit.Heading:
		switch block.Level {
		case 1:
			size, lineHeight = 32*scale, 44*scale
		case 2:
			size, lineHeight = 24*scale, 35*scale
		case 3:
			size, lineHeight = 20*scale, 30*scale
		default:
			size, lineHeight = 18*scale, 27*scale
		}
	case richedit.ListItem:
		indent = 24
		marker := "•"
		if block.Number > 0 {
			marker = strconv.Itoa(block.Number) + "."
			indent = 36
		}
		markerStyle := gfx.TextStyle{Size: 16 * scale, Color: palette.muted}
		markerWidth := canvas.TextWidthStyle(marker, markerStyle)
		indent = max(indent, markerWidth+8)
		canvas.Label(marker, ui.Rect{X: left + indent - markerWidth - 6, Y: top, Width: markerWidth + 3, Height: lineHeight}, markerStyle)
	case richedit.Quote:
		indent = 18
		canvas.Fill(ui.Rect{X: left + 2, Y: top, Width: 3, Height: lineHeight}, palette.border)
	case richedit.Code, richedit.Unknown:
		size, lineHeight = 13*scale, 21*scale
	}
	textLeft := left + indent
	if block.Kind == richedit.Unknown {
		height := paintRichHTMLText(canvas, block.Visible, gfx.TextStyle{Size: size, Color: palette.muted}, textLeft, top, width-indent, lineHeight)
		state.paintHTMLSourceHit(canvas, blockIndex, ui.Rect{X: left, Y: top, Width: width, Height: height})
		return height
	}
	if block.Kind == richedit.Code {
		canvas.Fill(ui.Rect{X: left, Y: top - 6, Width: width, Height: lineHeight + 12}, palette.sidebar)
	}
	if len(block.Leaves) == 0 {
		return lineHeight
	}
	x, y := textLeft, top
	visibleOffset := 0
	for leafIndex, leaf := range block.Leaves {
		style := gfx.TextStyle{Size: size, Weight: gfx.WeightRegular, Color: palette.ink, Italic: leaf.Italic, Mono: leaf.Mono}
		if block.Kind == richedit.Heading || leaf.Bold {
			style.Weight = gfx.WeightSemibold
		}
		if leaf.SafeLink {
			style.Color = palette.accent
		}
		if block.Kind == richedit.Code || block.ReadOnly {
			style.Color = palette.muted
		}
		value := leaf.Visible
		active := state.html.active && state.mode == renderedMode && state.html.blockIndex == blockIndex && state.html.leafIndex == leafIndex
		if active && state.html.input != nil {
			value = state.html.input.String()
		}
		missingImage := false
		if leaf.ImageSource != "" {
			if leaf.SafeAsset {
				if bitmap, ok := state.richHTMLBitmap(leaf.ImageSource); ok {
					if x != textLeft {
						x, y = textLeft, y+lineHeight
					}
					scale := min(float32(1), (left+width-textLeft)/float32(bitmap.Width), 260/float32(bitmap.Height))
					imageBounds := ui.Rect{X: textLeft, Y: y, Width: float32(bitmap.Width) * scale, Height: float32(bitmap.Height) * scale}
					canvas.DrawBitmap(bitmap, imageBounds)
					x, y = textLeft, y+imageBounds.Height+8
					visibleOffset += len(leaf.Visible)
					continue
				}
			}
			value = "▧ " + value
			missingImage = true
		}
		if value == "" {
			value = " "
		}
		segments := make([]htmlTextSegment, 0, 2)
		paintRichHTMLRun(canvas, value, style, textLeft, left+width, &x, &y, lineHeight, leaf.Underline, leaf.Strike, func(start, end int, bounds ui.Rect) {
			segment := htmlTextSegment{start: min(start, len(leaf.Visible)), end: min(end, len(leaf.Visible)), bounds: bounds, style: style}
			if active && state.html.input != nil {
				segment.start, segment.end = min(start, len(value)), min(end, len(value))
			}
			segments = append(segments, segment)
			if state.html.crossSelection != nil && blockIndex >= state.html.crossSelection.blockIndex && blockIndex <= state.html.crossSelection.lastBlock && value == leaf.Visible {
				selection := state.html.crossSelection
				first, last := 0, len(block.Visible)
				if blockIndex == selection.blockIndex {
					first = selection.start
				}
				if blockIndex == selection.lastBlock {
					last = selection.end
				}
				from := max(segment.start, first-visibleOffset)
				to := min(segment.end, last-visibleOffset)
				if to > from {
					color := palette.accent
					color.Alpha = 0.25
					for _, span := range canvas.TextSelectionStyle(nil, value[start:end], style, from-start, to-start) {
						canvas.Fill(ui.Rect{X: bounds.X + span.X, Y: bounds.Y, Width: span.Width, Height: min(lineHeight, style.Size*1.4)}, color)
					}
				}
			}
			if state.findOpen && state.findMode == state.mode && value == leaf.Visible {
				for matchIndex, match := range state.findVisual {
					if match.block != blockIndex {
						continue
					}
					from := max(segment.start, match.start-visibleOffset)
					to := min(segment.end, match.end-visibleOffset)
					if to <= from {
						continue
					}
					color := palette.accent
					color.Alpha = 0.18
					if matchIndex == state.findIndex {
						color.Alpha = 0.36
					}
					for _, span := range canvas.TextSelectionStyle(nil, value[start:end], style, from-start, to-start) {
						canvas.Fill(ui.Rect{X: bounds.X + span.X, Y: bounds.Y, Width: span.Width, Height: min(lineHeight, style.Size*1.4)}, color)
					}
				}
			}
			if match := state.html.workspaceMatch; match != nil && match.block == blockIndex && value == leaf.Visible {
				from := max(segment.start, match.start-visibleOffset)
				to := min(segment.end, match.end-visibleOffset)
				if to > from {
					color := palette.accent
					color.Alpha = 0.36
					for _, span := range canvas.TextSelectionStyle(nil, value[start:end], style, from-start, to-start) {
						canvas.Fill(ui.Rect{X: bounds.X + span.X, Y: bounds.Y, Width: span.Width, Height: min(lineHeight, style.Size*1.4)}, color)
					}
				}
			}
			if active && state.html.input != nil {
				selection := state.html.input.Selection()
				from := max(segment.start, byteOffsetOfRune(state.html.input.String(), selection.Start))
				to := min(segment.end, byteOffsetOfRune(state.html.input.String(), selection.End))
				if to > from {
					color := palette.accent
					color.Alpha = 0.25
					for _, span := range canvas.TextSelectionStyle(nil, value[start:end], style, from-start, to-start) {
						canvas.Fill(ui.Rect{X: bounds.X + span.X, Y: bounds.Y, Width: span.Width, Height: min(lineHeight, style.Size*1.4)}, color)
					}
				}
			}
		})
		if missingImage {
			x += 4
		}
		visibleOffset += len(leaf.Visible)
		if len(segments) == 0 {
			segments = append(segments, htmlTextSegment{bounds: ui.Rect{X: x, Y: y, Width: 18, Height: lineHeight}, style: style})
		}
		for segmentIndex := range segments {
			segment := &segments[segmentIndex]
			segment.bounds.Width = max(1, htmlTextAdvance(canvas, value[segment.start:segment.end], style))
			if leaf.Visible == "" {
				segment.bounds.Width = max(18, segment.bounds.Width)
			} else {
				segment.bounds.Width += 3
			}
		}
		if leafIndex+1 < len(block.Leaves) && leaf.ImageSource == "" && leaf.Visible != "" {
			x += 3
		}
		for _, segment := range segments {
			state.html.leafTargets = append(state.html.leafTargets, htmlLeafTarget{
				blockIndex: blockIndex, leafIndex: leafIndex, visibleStart: visibleOffset - len(leaf.Visible),
				value: value, segment: segment,
			})
		}
		if state.mode == readMode && leaf.SafeLink && !block.ReadOnly {
			id := htmlLinks.At(len(state.html.links))
			state.html.links = append(state.html.links, leaf.Link)
			for _, segment := range segments {
				canvas.HitWithOptions(id, segment.bounds, "打开 HTML 链接 "+leaf.Link, gfx.HitOptions{Role: gfx.RoleLink, Hint: leaf.Link})
			}
			if canvas.Clicked(id) {
				if err := app.OpenURL(leaf.Link); err != nil {
					state.message = err.Error()
				}
			}
		}
		if state.mode != renderedMode {
			continue
		}
		if active {
			state.html.segments = append(state.html.segments[:0], segments...)
			state.html.wrapCanvas = canvas
			state.html.inputRect = segments[0].bounds
			if state.html.placeCaret {
				if offset, ok := state.html.OffsetAt(state.html.clickPoint); ok {
					state.html.input.MoveTo(utf8.RuneCountInString(state.html.input.String()[:offset]), false)
				}
				state.html.placeCaret = false
			}
			composingConfirmation := state.confirmationCompositionFocus() == htmlInputID
			if composingConfirmation {
				canvas.PushFocusScope(confirmScope)
			}
			for _, segment := range segments {
				canvas.HitWithOptions(htmlInputID, segment.bounds, "编辑 HTML 文本", gfx.HitOptions{
					Role: gfx.RoleTextField, Value: state.html.input.String(), TextBounds: segment.bounds,
					TextSize: style.Size, TextWeight: style.Weight, TextColor: ui.Color{},
					HideNativeOverlays: true, NavigationKeys: true, Placement: &state.html,
				})
			}
			if composingConfirmation {
				canvas.PopFocusScope()
			}
			if canvas.Focused() == htmlInputID {
				if caret, ok := state.html.CaretBounds(); ok {
					canvas.Fill(caret, palette.ink)
				}
			}
		} else if leaf.Editable {
			id := htmlLeafRows.At(leafBase + leafIndex)
			for _, segment := range segments {
				canvas.HitWithOptions(id, segment.bounds, "编辑 HTML 文本 "+leaf.Visible, gfx.HitOptions{Role: gfx.RoleButton, Hint: "编辑文字"})
			}
			if canvas.Clicked(id) {
				state.focusRequest = state.activateHTMLLeafAt(blockIndex, leafIndex).Focus
				state.html.clickPoint, state.html.placeCaret = state.html.lastPointerUp, true
				state.invalidator.Invalidate()
			}
		}
	}
	height := max(lineHeight, y-top+lineHeight)
	if block.Kind == richedit.Code {
		state.paintHTMLSourceHit(canvas, blockIndex, ui.Rect{X: left, Y: top, Width: width, Height: height})
	}
	return height
}

func (state *model) paintHTMLSourceHit(canvas *gfx.Canvas, blockIndex int, bounds ui.Rect) {
	if state.mode != renderedMode {
		return
	}
	id := htmlBlocks.At(blockIndex)
	canvas.HitWithOptions(id, bounds, "查看只读 HTML 原文", gfx.HitOptions{Role: gfx.RoleButton, Hint: "查看原始 HTML"})
	if canvas.Clicked(id) {
		state.focusRequest = state.inspectHTMLBlock(blockIndex).Focus
		state.invalidator.Invalidate()
	}
}

func (state *model) inspectHTMLBlock(index int) gfx.EventResult {
	if index < 0 || index >= len(state.html.parsed.Blocks) {
		return gfx.EventResult{}
	}
	current := state.session.Current()
	start := state.html.parsed.Blocks[index].Start
	current.Editor.MoveTo(utf8.RuneCountInString(current.Text()[:start]), false)
	state.html.active = false
	state.mode = editMode
	return gfx.EventResult{Handled: true, Repaint: true, Focus: sourceID}
}

func (state *model) onHTMLPointer(event gfx.PointerEvent) gfx.EventResult {
	view := &state.html
	if event.Kind == gfx.PointerUp && event.Button == gfx.PointerButtonPrimary {
		view.lastPointerUp = ui.Point{X: event.X, Y: event.Y}
	}
	if !view.active || state.mode != renderedMode || event.Target != htmlInputID {
		return gfx.EventResult{}
	}
	switch event.Kind {
	case gfx.PointerDown:
		if event.Button == gfx.PointerButtonPrimary {
			view.dragging, view.blockedSelection = true, false
			view.crossSelection = nil
			if offset, ok := view.OffsetAt(ui.Point{X: event.X, Y: event.Y}); ok {
				view.crossAnchor = view.visibleBeforeLeaf(view.blockIndex, view.leafIndex) + offset
			}
		}
	case gfx.PointerMove, gfx.PointerUp:
		if view.dragging && !view.containsActivePoint(ui.Point{X: event.X, Y: event.Y}) {
			if view.selectAcrossLeaf(ui.Point{X: event.X, Y: event.Y}) {
				view.blockedSelection = false
				state.message = ""
				if event.Kind == gfx.PointerUp {
					view.dragging = false
				}
				return gfx.EventResult{Handled: true, Repaint: true}
			}
			view.crossSelection = nil
			view.blockedSelection = true
			state.message = "选区跨越 HTML 叶子；原文未改，请切源码模式处理"
			if event.Kind == gfx.PointerUp {
				view.dragging = false
			}
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		if event.Kind == gfx.PointerUp {
			view.dragging = false
		}
		if view.containsActivePoint(ui.Point{X: event.X, Y: event.Y}) {
			view.crossSelection = nil
		}
	}
	return gfx.EventResult{}
}

func (state *model) activateHTMLLeaf(flatIndex int) gfx.EventResult {
	for blockIndex, block := range state.html.parsed.Blocks {
		if flatIndex < len(block.Leaves) {
			return state.activateHTMLLeafAt(blockIndex, flatIndex)
		}
		flatIndex -= len(block.Leaves)
	}
	return gfx.EventResult{}
}

func (state *model) activateHTMLLeafAt(blockIndex, leafIndex int) gfx.EventResult {
	if state.mode != renderedMode || blockIndex < 0 || blockIndex >= len(state.html.parsed.Blocks) {
		return gfx.EventResult{}
	}
	block := &state.html.parsed.Blocks[blockIndex]
	if leafIndex < 0 || leafIndex >= len(block.Leaves) || !block.Leaves[leafIndex].Editable {
		return gfx.EventResult{}
	}
	leaf := block.Leaves[leafIndex]
	state.html.active = true
	state.html.blockIndex, state.html.leafIndex = blockIndex, leafIndex
	state.html.leaf = leaf
	state.html.blockedSelection = false
	state.html.crossSelection = nil
	state.html.placeCaret = false
	state.html.input = text.NewEditor(leaf.Visible)
	state.html.input.MoveTo(utf8.RuneCountInString(leaf.Visible), false)
	return gfx.EventResult{Handled: true, Repaint: true, Focus: htmlInputID}
}

func (state *model) onHTMLTextEvent(event app.TextEvent) (app.TextState, error) {
	view := &state.html
	current := state.session.Current()
	if event.Kind == app.TextPasteNoText {
		return state.pasteHTMLClipboardImage()
	}
	if event.Kind == app.TextCancelComposition {
		// A cancelled composition restores the leaf's own text; the source was
		// never changed for it, so no attribution patch may run.
		if view.input == nil {
			return app.TextState{}, nil
		}
		event.Apply(view.input)
		return app.TextStateOf(view.input), nil
	}
	if view.crossSelection != nil {
		if event.Kind == app.TextSetMarked || event.Kind == app.TextUnmark {
			event.Apply(view.input)
			return app.TextStateOf(view.input), nil
		}
		if event.Kind == app.TextInsert || event.Kind == app.TextPaste || event.Kind == app.TextCommandEvent && (event.Command == app.TextDeleteBackward || event.Command == app.TextDeleteForward) {
			replacement := event.Text
			if event.Kind == app.TextCommandEvent {
				replacement = ""
			}
			if err := state.applyHTMLCrossSelection(replacement); err != nil {
				state.message = err.Error()
			}
			return app.TextStateOf(view.input), nil
		}
	}
	if view.blockedSelection && htmlMutation(event) {
		state.message = "选区跨越 HTML 叶子；原文未改，请切源码模式处理"
		return app.TextStateOf(view.input), nil
	}
	if !view.active || current == nil || current.ID != view.documentID || current.Text() != view.source {
		return app.TextStateOf(view.input), richedit.ErrStaleLeaf
	}
	if _, composing := view.input.Composition(); composing && htmlSelectionEvent(event) {
		return app.TextStateOf(view.input), nil
	}
	if event.Kind == app.TextCommandEvent && event.Command == app.TextDeleteBackward && (state.reverseHTMLListExit() || state.mergeHTMLParagraph()) {
		return app.TextStateOf(view.input), nil
	}
	previous := view.input.CommittedString()
	event.Apply(view.input)
	if event.Kind == app.TextCommandEvent && (event.Command == app.TextUndo || event.Command == app.TextRedo) {
		if result, handled := state.stepHTMLStructure(event.Command == app.TextUndo); handled {
			return result, nil
		}
		changed := false
		if event.Command == app.TextUndo {
			changed = current.Editor.Undo()
		} else {
			changed = current.Editor.Redo()
		}
		if changed {
			view.source = current.Text()
			view.parsed = richedit.Parse([]byte(view.source))
			view.workspaceMatch = nil
			view.scrollPending = false
			view.refreshLeaf()
			if view.active && view.input.String() != view.leaf.Visible {
				view.input = text.NewEditor(view.leaf.Visible)
				view.input.MoveTo(view.input.RuneCount(), false)
			}
		}
		return app.TextStateOf(view.input), nil
	}
	if _, composing := view.input.Composition(); composing || view.input.String() == previous {
		return app.TextStateOf(view.input), nil
	}
	patched, err := richedit.ReplaceLeaf([]byte(view.source), view.leaf, view.input.String())
	if err != nil {
		view.input = text.NewEditor(previous)
		state.message = err.Error()
		return app.TextStateOf(view.input), nil
	}
	start, end := view.leaf.Start, view.leaf.End
	replacement := string(patched[start : len(patched)-(len(view.source)-end)])
	if err := current.EditRange(start, end, replacement); err != nil {
		view.input = text.NewEditor(previous)
		state.message = err.Error()
		return app.TextStateOf(view.input), nil
	}
	view.source = current.Text()
	view.parsed = richedit.Parse([]byte(view.source))
	view.structureRedo = nil
	view.workspaceMatch = nil
	view.scrollPending = false
	view.refreshLeaf()
	return app.TextStateOf(view.input), nil
}

func htmlSelectionEvent(event app.TextEvent) bool {
	if event.Kind == app.TextSetSelection {
		return true
	}
	if event.Kind != app.TextCommandEvent {
		return false
	}
	switch event.Command {
	case app.TextSelectAll,
		app.TextMoveLeft, app.TextMoveRight, app.TextMoveLeftAndModifySelection, app.TextMoveRightAndModifySelection,
		app.TextMoveLineStart, app.TextMoveLineEnd, app.TextMoveLineStartAndModifySelection, app.TextMoveLineEndAndModifySelection,
		app.TextMoveDocumentStart, app.TextMoveDocumentEnd, app.TextMoveDocumentStartAndModifySelection, app.TextMoveDocumentEndAndModifySelection,
		app.TextMoveWordLeft, app.TextMoveWordRight, app.TextMoveWordLeftAndModifySelection, app.TextMoveWordRightAndModifySelection:
		return true
	}
	return false
}

func htmlMutation(event app.TextEvent) bool {
	switch event.Kind {
	case app.TextInsert, app.TextSetMarked, app.TextPaste:
		return true
	case app.TextCommandEvent:
		return event.Command == app.TextDeleteBackward || event.Command == app.TextDeleteForward || event.Command == app.TextInsertNewline || event.Command == app.TextInsertLineBreak || event.Command == app.TextInsertTab
	default:
		return false
	}
}

func (view *richHTMLView) refreshLeaf() {
	if view.blockIndex >= len(view.parsed.Blocks) || view.leafIndex >= len(view.parsed.Blocks[view.blockIndex].Leaves) {
		view.active = false
		return
	}
	view.leaf = view.parsed.Blocks[view.blockIndex].Leaves[view.leafIndex]
}

func paintRichHTMLRun(canvas *gfx.Canvas, value string, style gfx.TextStyle, left, right float32, x, y *float32, lineHeight float32, underline, strike bool, visit func(start, end int, bounds ui.Rect)) {
	position := 0
	for len(value) > 0 {
		available := max(8, right-*x-4)
		row, next := canvas.WrapRow(value, 0, style.Size, style.Weight, available)
		segment := value[row.Start:row.End]
		measured := htmlTextAdvance(canvas, segment, style)
		if measured > right-*x && *x > left {
			*x, *y = left, *y+lineHeight
			continue
		}
		if segment != "" {
			bounds := ui.Rect{X: *x, Y: *y, Width: min(measured+2, right-*x), Height: lineHeight}
			if visit != nil {
				visit(position+row.Start, position+row.End, bounds)
			}
			canvas.Label(segment, bounds, style)
			if underline || strike {
				lineY := *y + lineHeight - 5
				if strike {
					lineY = *y + lineHeight/2
				}
				canvas.Fill(ui.Rect{X: *x, Y: lineY, Width: min(measured, right-*x), Height: 1}, style.Color)
			}
			*x += measured
		}
		if next < 0 {
			break
		}
		position += next
		value = value[next:]
		*x, *y = left, *y+lineHeight
	}
}

func paintRichHTMLText(canvas *gfx.Canvas, value string, style gfx.TextStyle, left, top, width, lineHeight float32) float32 {
	x, y := left, top
	paintRichHTMLRun(canvas, value, style, left, left+width, &x, &y, lineHeight, false, false, nil)
	return max(lineHeight, y-top+lineHeight)
}

func (state *model) paintRichHTMLTable(canvas *gfx.Canvas, palette colors, first, end, leafBase int, left, top, width float32) float32 {
	columns := 1
	for _, cell := range state.html.parsed.Blocks[first:end] {
		columns = max(columns, cell.Column+1)
	}
	cellWidth := width / float32(columns)
	y := top
	for rowStart := first; rowStart < end; {
		rowEnd := rowStart + 1
		for rowEnd < end && state.html.parsed.Blocks[rowEnd].Row == state.html.parsed.Blocks[rowStart].Row {
			rowEnd++
		}
		rowHeight := float32(34)
		for index := rowStart; index < rowEnd; index++ {
			cell := &state.html.parsed.Blocks[index]
			state.html.blockY[index] = y - (state.html.rect.Y - state.html.scroll)
			x := left + float32(cell.Column)*cellWidth
			contentHeight := state.paintRichHTMLBlock(canvas, palette, cell, index, leafBase, x+9, y+4, cellWidth-18)
			rowHeight = max(rowHeight, contentHeight+8)
			leafBase += len(cell.Leaves)
		}
		canvas.Fill(ui.Rect{X: left, Y: y, Width: width, Height: 1}, palette.border)
		for column := 0; column <= columns; column++ {
			canvas.Fill(ui.Rect{X: left + float32(column)*cellWidth, Y: y, Width: 1, Height: rowHeight}, palette.border)
		}
		y += rowHeight
		rowStart = rowEnd
	}
	canvas.Fill(ui.Rect{X: left, Y: y - 1, Width: width, Height: 1}, palette.border)
	return y - top
}

func (state *model) richHTMLHeadingEntries() []outlineEntry {
	var entries []outlineEntry
	for _, block := range state.html.parsed.Blocks {
		if block.Kind == richedit.Heading {
			entries = append(entries, outlineEntry{Title: strings.TrimSpace(block.Visible), Level: block.Level, Rune: utf8.RuneCountInString(state.html.source[:block.Start])})
		}
	}
	return entries
}
