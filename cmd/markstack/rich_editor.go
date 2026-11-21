package main

import (
	"crypto/sha256"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"markstack/internal/markdownedit"
	"markstack/internal/session"
)

type renderedEditor struct {
	active              bool
	virtual             bool
	virtualStart        int
	virtualEnd          int
	virtualSuffix       string
	virtualInsert       string
	virtualDepth        int
	emptyTarget         *renderedEmptyTarget
	virtualBeforeSelect text.Range
	virtualEdit         bool
	index               int
	sourceIndex         int
	documentID          int
	source              string
	block               markdownedit.Block
	input               *text.Editor
	textInput           app.TextInput
	rect                ui.Rect
	clickX              float32
	clickY              float32
	placeCaret          bool
	dragAnchor          int
	dragAnchorX         float32
	dragAnchorY         float32
	placeDragAnchor     bool
	appliedEdits        int
	undoneEdits         int
	sourceHistoryOffset int
	wrapped             bool
	mixed               bool
	inlineSpans         []markdown.Span
	wrapRows            []gfx.TextRow
	wrapCanvas          *gfx.Canvas
	wrapX               float32
	wrapY               float32
	wrapSize            float32
	wrapWeight          float32
	wrapLineHeight      float32
	wrapWidth           float32
	wrapGoalX           float32
	wrapGoalCaret       int
	wrapGoalActive      bool
	caretByte           int
	caretUpstream       bool
	caretFromPointer    bool
	preview             *previewView
	pendingCross        bool
	crossStart          int
	crossEnd            int
	crossSelection      text.Range
	crossInputChange    bool
	compositionStart    text.Range
	compositionEdit     bool
	renderedHistory
}

type renderedCrossEdit struct {
	beforeSourceHash, afterSourceHash   [32]byte
	beforeVisibleHash, afterVisibleHash [32]byte
	beforeSelect, afterSelect           text.Range
	beforeVirtual, afterVirtual         *renderedVirtualState
	inputChanged                        bool
	virtualText                         bool
	index                               int
	position                            int
}

func (rich *renderedEditor) OffsetAt(point ui.Point) (int, bool) {
	offset, ok := rich.offsetAt(point)
	if ok {
		rich.caretByte, rich.caretUpstream, rich.caretFromPointer = offset, rich.upstreamAt(point, offset), true
	}
	return offset, ok
}

func (rich *renderedEditor) offsetAt(point ui.Point) (int, bool) {
	if rich.mixed && rich.preview != nil && rich.index >= 0 && rich.index < len(rich.preview.blocks) {
		if offset, ok := rich.preview.blocks[rich.index].view.OffsetAt(point); ok {
			return offset, true
		}
	}
	if rich.mixed && !rich.wrapped && rich.wrapCanvas != nil {
		position := point.X - rich.wrapX
		offset := 0
		for _, span := range rich.inlineSpans {
			style := renderedSpanStyle(span, rich.wrapSize)
			width := rich.wrapCanvas.TextWidthStyle(span.Text, style)
			if position <= width {
				return offset + rich.wrapCanvas.TextOffsetAtStyle(span.Text, style, position), true
			}
			position -= width
			offset += len(span.Text)
		}
		return offset, true
	}
	if rich.wrapCanvas == nil || len(rich.wrapRows) == 0 {
		return 0, false
	}
	rowIndex := max(0, min(int((point.Y-rich.wrapY)/rich.wrapLineHeight), len(rich.wrapRows)-1))
	row := rich.wrapRows[rowIndex]
	value := rich.input.String()
	return row.Start + rich.wrapCanvas.TextOffsetAt(value[row.Start:row.End], rich.wrapSize, rich.wrapWeight, point.X-rich.wrapX), true
}

func (rich *renderedEditor) CaretBounds() (ui.Rect, bool) {
	if rich.input == nil {
		return ui.Rect{}, false
	}
	offset := byteOffsetOfRune(rich.input.String(), rich.input.Caret())
	return rich.caretBoundsAt(offset, rich.caretUpstream && rich.caretByte == offset)
}

func (rich *renderedEditor) caretBoundsAt(offset int, upstream bool) (ui.Rect, bool) {
	height := rich.wrapSize * 1.4
	if rich.wrapLineHeight > 0 {
		height = min(height, rich.wrapLineHeight)
	}
	if rich.mixed && rich.preview != nil && rich.index >= 0 && rich.index < len(rich.preview.blocks) {
		if upstream {
			if caret, ok := rich.preview.CaretAtUpstream(rich.wrapCanvas, rich.index, rich.input.String(), offset); ok {
				return caret, true
			}
		}
		if caret, ok := rich.preview.blocks[rich.index].view.CaretAt(offset); ok {
			return caret, true
		}
	}
	if rich.mixed && !rich.wrapped && rich.wrapCanvas != nil {
		caret := offset
		x := rich.wrapX
		for _, span := range rich.inlineSpans {
			end := min(caret, len(span.Text))
			x += rich.wrapCanvas.TextWidthStyle(span.Text[:end], renderedSpanStyle(span, rich.wrapSize))
			caret -= end
		}
		return ui.Rect{X: x, Y: rich.wrapY, Width: 1, Height: height}, true
	}
	if rich.wrapCanvas == nil {
		return ui.Rect{}, false
	}
	if len(rich.wrapRows) == 0 {
		return ui.Rect{X: rich.wrapX, Y: rich.wrapY, Width: 1, Height: height}, true
	}
	value := rich.input.String()
	index := rich.caretRow(offset, upstream)
	row := rich.wrapRows[index]
	position := min(offset, row.End)
	x := rich.wrapX + rich.wrapCanvas.TextCaret(value[row.Start:row.End], rich.wrapSize, rich.wrapWeight, position-row.Start)
	return ui.Rect{X: x, Y: rich.wrapY + float32(index)*rich.wrapLineHeight, Width: 1, Height: height}, true
}

func (rich *renderedEditor) caretRow(offset int, upstream bool) int {
	for index := len(rich.wrapRows) - 1; index >= 0; index-- {
		row := rich.wrapRows[index]
		if offset < row.Start || upstream && index > 0 && offset == row.Start && rich.wrapRows[index-1].End == offset {
			continue
		}
		return index
	}
	return 0
}

func (rich *renderedEditor) upstreamAt(point ui.Point, offset int) bool {
	value := rich.input.String()
	if offset <= 0 || offset > len(value) {
		return false
	}
	caret, ok := rich.caretBoundsAt(offset, false)
	previous, previousOK := rich.caretBoundsAt(gfx.GraphemeStart(value, offset-1), false)
	return ok && previousOK && caret.Y > previous.Y && point.Y < caret.Y
}

func (rich *renderedEditor) paintTextRows(canvas *gfx.Canvas, bounds ui.Rect, ink, selectionColor ui.Color) {
	value := rich.input.String()
	selection := rich.input.Selection()
	start, end := byteOffsetOfRune(value, selection.Start), byteOffsetOfRune(value, selection.End)
	markedStart, markedEnd := 0, 0
	if marked, composing := rich.input.Composition(); composing {
		markedStart, markedEnd = byteOffsetOfRune(value, marked.Start), byteOffsetOfRune(value, marked.End)
	}
	selectionColor.Alpha = 0.25
	for rowIndex, line := range rich.wrapRows {
		lineBounds := ui.Rect{X: bounds.X, Y: bounds.Y + float32(rowIndex)*rich.wrapLineHeight, Width: bounds.Width, Height: rich.wrapLineHeight}
		if end > line.Start && start < line.End {
			for _, span := range canvas.TextSelection(nil, value[line.Start:line.End], rich.wrapSize, rich.wrapWeight, max(start, line.Start)-line.Start, min(end, line.End)-line.Start) {
				canvas.Fill(ui.Rect{X: lineBounds.X + span.X, Y: lineBounds.Y, Width: span.Width, Height: min(rich.wrapLineHeight, rich.wrapSize*1.4)}, selectionColor)
			}
		}
		canvas.Label(value[line.Start:line.End], lineBounds, gfx.TextStyle{Size: rich.wrapSize, Weight: rich.wrapWeight, Color: ink})
		if markedStart < line.End && markedEnd > line.Start {
			for _, span := range canvas.TextSelection(nil, value[line.Start:line.End], rich.wrapSize, rich.wrapWeight, max(markedStart, line.Start)-line.Start, min(markedEnd, line.End)-line.Start) {
				canvas.Fill(ui.Rect{X: lineBounds.X + span.X, Y: lineBounds.Y + rich.wrapLineHeight - 2, Width: span.Width, Height: 1}, ink)
			}
		}
	}
	if canvas.Focused() == richInputID {
		if caret, ok := rich.CaretBounds(); ok {
			canvas.Fill(caret, ink)
		}
	}
}

func renderedSpanStyle(span markdown.Span, size float32) gfx.TextStyle {
	style := gfx.TextStyle{Size: size, Weight: gfx.WeightRegular, Italic: span.Style&markdown.Italic != 0}
	if span.Style&markdown.Bold != 0 {
		style.Weight = gfx.WeightSemibold
	}
	return style
}

func (rich *renderedEditor) moveVisualEdge(event gfx.KeyEvent) bool {
	if rich.input == nil || event.Modifiers&^(gfx.ModifierShift|gfx.ModifierMeta) != 0 {
		return false
	}
	if _, composing := rich.input.Composition(); composing {
		return false
	}
	ending := false
	switch event.Key {
	case gfx.KeyHome:
	case gfx.KeyEnd:
		ending = true
	case gfx.KeyLeft, gfx.KeyRight:
		if event.Modifiers&gfx.ModifierMeta == 0 {
			return false
		}
		ending = event.Key == gfx.KeyRight
	default:
		return false
	}
	value := rich.input.String()
	offset := 0
	upstream := false
	if rich.mixed && rich.preview != nil && rich.index >= 0 && rich.index < len(rich.preview.blocks) {
		caret, ok := rich.CaretBounds()
		if !ok {
			return false
		}
		position := rich.rect.X
		if ending {
			position += rich.rect.Width
		}
		point := ui.Point{X: position, Y: caret.Y + caret.Height/2}
		offset, ok = rich.offsetAt(point)
		if !ok {
			return false
		}
		upstream = rich.upstreamAt(point, offset)
	} else {
		caret := byteOffsetOfRune(value, rich.input.Caret())
		if len(rich.wrapRows) > 0 {
			index := rich.caretRow(caret, rich.caretUpstream && rich.caretByte == caret)
			row := rich.wrapRows[index]
			offset = row.Start
			if ending {
				offset = row.End
				upstream = index+1 < len(rich.wrapRows) && rich.wrapRows[index+1].Start == offset
			}
		}
	}
	rich.input.MoveTo(utf8.RuneCountInString(value[:offset]), event.Modifiers&gfx.ModifierShift != 0)
	rich.caretByte, rich.caretUpstream, rich.caretFromPointer = offset, upstream, false
	rich.wrapGoalActive = false
	return true
}

func (rich *renderedEditor) moveVisualRow(key gfx.Key, extend bool) bool {
	if key != gfx.KeyUp && key != gfx.KeyDown {
		return false
	}
	value := rich.input.String()
	caret := byteOffsetOfRune(value, rich.input.Caret())
	if rich.mixed && rich.preview != nil && rich.index >= 0 && rich.index < len(rich.preview.blocks) {
		current, ok := rich.CaretBounds()
		if !ok {
			return false
		}
		goal := current.X
		if rich.wrapGoalActive && rich.wrapGoalCaret == caret {
			goal = rich.wrapGoalX
		}
		direction := float32(-1)
		if key == gfx.KeyDown {
			direction = 1
		}
		point := ui.Point{X: goal, Y: current.Y + direction*current.Height + current.Height/2}
		offset, ok := rich.offsetAt(point)
		if !ok {
			return false
		}
		upstream := rich.upstreamAt(point, offset)
		next, ok := rich.caretBoundsAt(offset, upstream)
		if !ok || next.Y == current.Y {
			rich.wrapGoalActive = false
			return false
		}
		rich.input.MoveTo(utf8.RuneCountInString(value[:offset]), extend)
		rich.caretByte, rich.caretUpstream, rich.caretFromPointer = offset, upstream, false
		rich.wrapGoalX, rich.wrapGoalCaret, rich.wrapGoalActive = goal, offset, true
		return true
	}
	if !rich.wrapped || rich.wrapCanvas == nil {
		return false
	}
	rowIndex := rich.caretRow(caret, rich.caretUpstream && rich.caretByte == caret)
	target := rowIndex - 1
	if key == gfx.KeyDown {
		target = rowIndex + 1
	}
	if target < 0 || target >= len(rich.wrapRows) {
		rich.wrapGoalActive = false
		return false
	}
	current, ok := rich.CaretBounds()
	if !ok {
		return false
	}
	goal := current.X - rich.wrapX
	if rich.wrapGoalActive && rich.wrapGoalCaret == caret {
		goal = rich.wrapGoalX
	}
	next := rich.wrapRows[target]
	offset := next.Start + rich.wrapCanvas.TextOffsetAt(value[next.Start:next.End], rich.wrapSize, rich.wrapWeight, goal)
	rich.input.MoveTo(utf8.RuneCountInString(value[:offset]), extend)
	upstream := target+1 < len(rich.wrapRows) && rich.wrapRows[target+1].Start == offset && offset == next.End
	rich.caretByte, rich.caretUpstream, rich.caretFromPointer = offset, upstream, false
	rich.wrapGoalX, rich.wrapGoalCaret, rich.wrapGoalActive = goal, offset, true
	return true
}

type renderedSelection struct {
	active          bool
	editable        bool
	documentID      int
	source          string
	start, end      int
	prefix          string
	suffix          string
	visible         string
	first, last     int
	anchorIndex     int
	anchorOffset    int
	anchorVirtual   *renderedVirtualState
	firstOffset     int
	lastOffset      int
	placeExtent     bool
	clickX          float32
	clickY          float32
	extentIndex     int
	history         renderedHistory
	historyPosition int
}

// pendingCompositionFocus names the native editor that owns uncommitted
// composition text, so a block or mode switch can keep the marked text and its
// focus instead of clearing the editor that holds it.
func (state *model) pendingCompositionFocus() gfx.ID {
	if state.rich.active && state.rich.input != nil && state.richComposing() {
		return richInputID
	}
	if state.codeBlock.active && state.codeBlockComposing() {
		return codeInputID
	}
	if state.table.active && state.table.input != nil && state.tableComposing() {
		return tableInputID
	}
	if state.html.active && state.html.input != nil && state.htmlComposing() {
		return htmlInputID
	}
	if current := state.session.Current(); current != nil && current.Composing() {
		return sourceID
	}
	return gfx.ID{}
}

func (state *model) activateRendered(index int) gfx.EventResult {
	current := state.session.Current()
	if current == nil || index < 0 || index >= len(state.parsed.Blocks) {
		return gfx.EventResult{}
	}
	if focus := state.pendingCompositionFocus(); focus != (gfx.ID{}) {
		return gfx.EventResult{Handled: true, Focus: focus}
	}
	if state.parsed.Blocks[index].Kind == markdown.Code {
		return state.activateCodeBlock(index)
	}
	state.codeBlock = codeBlockEditor{}
	state.table = tableEditor{}
	state.headingMenuOpen = false
	state.linkOpen = false
	blocks := markdownedit.Parse([]byte(current.Text()))
	mapping := renderedMapping(&state.parsed, blocks)
	if mapping[index] < 0 {
		if start := state.parsed.Blocks[index].Start; start >= 0 && start <= len(current.Text()) {
			current.Editor.MoveTo(utf8.RuneCountInString(current.Text()[:start]), false)
		}
		state.rich = renderedEditor{}
		state.mode = editMode
		state.message = "此块请在 Markdown 源码模式编辑"
		return gfx.EventResult{Handled: true, Repaint: true, Focus: sourceID}
	}
	block := blocks[mapping[index]]
	history, position := state.renderedHistorySnapshot(current)
	state.selection = renderedSelection{}
	state.rich = renderedEditor{
		active: true, index: index, sourceIndex: mapping[index], documentID: current.ID, source: current.Text(),
		block: block, input: text.NewEditor(block.Visible),
		renderedHistory: history, sourceHistoryOffset: position,
	}
	state.rich.trimHistory(current)
	state.rich.input.MoveTo(utf8.RuneCountInString(block.Visible), false)
	return gfx.EventResult{Handled: true, Repaint: true, Focus: richInputID}
}

func (state *model) activeMixedSpan() bool {
	rich := &state.rich
	if !rich.active || rich.virtual || rich.input == nil || rich.index < 0 || rich.index >= len(state.parsed.Blocks) || rich.index >= len(state.preview.blocks) {
		return false
	}
	if _, composing := rich.input.Composition(); composing && renderedPlain(&state.parsed, rich.index) != rich.input.String() {
		return false
	}
	spans := state.parsed.Spans(&state.parsed.Blocks[rich.index])
	return len(spans) > 1 || len(spans) == 1 && (spans[0].Style != 0 || spans[0].Link != "")
}

func (state *model) projectRenderedComposition() {
	rich := &state.rich
	current := state.session.Current()
	if state.mode != renderedMode || !rich.active || rich.virtual || rich.input == nil || current == nil ||
		rich.documentID != current.ID || rich.source != current.Text() || rich.index < 0 || rich.index >= len(state.parsed.Blocks) {
		return
	}
	marked, composing := rich.input.Composition()
	if !composing || renderedPlain(&state.parsed, rich.index) != rich.block.Visible {
		return
	}
	value := rich.input.String()
	start, end := byteOffsetOfRune(value, marked.Start), byteOffsetOfRune(value, marked.End)
	prefix, suffix := value[:start], value[end:]
	original := rich.block.Visible
	if len(prefix)+len(suffix) > len(original) || !strings.HasPrefix(original, prefix) || !strings.HasSuffix(original, suffix) {
		return
	}
	from, to := len(prefix), len(original)-len(suffix)
	if !markdownedit.EditableRange(rich.block, from, to) {
		state.projectCrossStyleComposition(from, to, value[start:end])
		return
	}
	spans := state.parsed.Spans(&state.parsed.Blocks[rich.index])
	offset := 0
	for index := range spans {
		span := &spans[index]
		if from >= offset && to <= offset+len(span.Text) {
			span.Text = span.Text[:from-offset] + value[start:end] + span.Text[to-offset:]
			return
		}
		offset += len(span.Text)
	}
}

func (state *model) projectCrossStyleComposition(from, to int, marked string) {
	rich := &state.rich
	next, err := markdownedit.ReplaceSelection([]byte(rich.source), rich.block, from, to, marked)
	if err != nil {
		return
	}
	projected := &state.compositionPreview
	projected.Parse(string(next))
	if len(projected.Blocks) != len(state.parsed.Blocks) || renderedPlain(projected, rich.index) != rich.input.String() {
		return
	}
	for index := range projected.Blocks {
		before, after := &state.parsed.Blocks[index], &projected.Blocks[index]
		if before.Kind != after.Kind || before.Level != after.Level || before.Quote != after.Quote ||
			before.Number != after.Number || before.Columns != after.Columns || before.Info != after.Info || before.Text != after.Text {
			return
		}
		if index != rich.index {
			original, spans := state.parsed.Spans(before), projected.Spans(after)
			if len(original) != len(spans) {
				return
			}
			copy(spans, original)
		}
		after.Start, after.End = before.Start, before.End
	}
	state.parsed, state.compositionPreview = state.compositionPreview, state.parsed
}

func (state *model) selectRendered(index int) gfx.EventResult {
	current := state.session.Current()
	if !state.rich.active || index < 0 || index >= len(state.parsed.Blocks) || current == nil {
		return gfx.EventResult{}
	}
	if _, composing := state.rich.input.Composition(); composing {
		return gfx.EventResult{Handled: true}
	}
	blocks := markdownedit.Parse([]byte(current.Text()))
	mapping := renderedMapping(&state.parsed, blocks)
	first, last := min(index, state.rich.index), max(index, state.rich.index)
	anchor := byteOffsetOfRune(state.rich.input.String(), state.rich.input.Caret())
	selection := renderedSelection{active: true, documentID: current.ID, source: current.Text(), first: first, last: last, anchorIndex: state.rich.index, anchorOffset: anchor, extentIndex: index}
	if state.virtualParagraphNeedsPlaceholder() && state.rich.block.Kind == markdownedit.Paragraph {
		selection.anchorVirtual = state.rich.virtualState()
	}
	if state.rich.documentID == current.ID && state.rich.source == current.Text() {
		selection.history = state.rich.renderedHistory
		selection.historyPosition = state.rich.appliedEdits + state.rich.sourceHistoryOffset
	}
	if selection.anchorVirtual != nil {
		selection.setVirtualExtent(index, &state.parsed)
	} else if index > state.rich.index {
		selection.firstOffset, selection.lastOffset = anchor, len(renderedPlain(&state.parsed, last))
	} else {
		selection.firstOffset, selection.lastOffset = 0, anchor
	}
	selection.refresh(&state.parsed, blocks, mapping)
	state.selection = selection
	state.rich = renderedEditor{}
	return gfx.EventResult{Handled: true, Repaint: true, Focus: renderedSelectionID}
}

func (state *model) selectRenderedAt(index int, x, y float32) gfx.EventResult {
	result := state.selectRendered(index)
	if state.selection.active {
		state.selection.placeExtent = true
		state.selection.clickX = x
		state.selection.clickY = y
		state.selection.extentIndex = index
	}
	return result
}

func (state *model) renderedRowAt(y float32) int {
	base := state.previewRect.Y + 46 - state.previewScroll
	for index, block := range state.preview.blocks {
		top := base + block.top
		if y >= top && y < top+max(26, block.height) {
			return index
		}
	}
	return -1
}

func (state *model) extendRenderedSelection(index int) gfx.EventResult {
	selection := &state.selection
	if index < 0 || index >= len(state.parsed.Blocks) {
		return gfx.EventResult{Handled: true}
	}
	if index == selection.anchorIndex && selection.anchorVirtual == nil {
		return state.collapseRenderedSelection()
	}
	current := state.session.Current()
	blocks := markdownedit.Parse([]byte(current.Text()))
	mapping := renderedMapping(&state.parsed, blocks)
	if selection.anchorVirtual != nil {
		selection.setVirtualExtent(index, &state.parsed)
	} else {
		first, last := min(selection.anchorIndex, index), max(selection.anchorIndex, index)
		selection.first, selection.last, selection.extentIndex = first, last, index
		if selection.anchorIndex == first {
			selection.firstOffset, selection.lastOffset = selection.anchorOffset, len(renderedPlain(&state.parsed, last))
		} else {
			selection.firstOffset, selection.lastOffset = 0, selection.anchorOffset
		}
	}
	selection.refresh(&state.parsed, blocks, mapping)
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (state *model) collapseRenderedSelection() gfx.EventResult {
	selection := state.selection
	current := state.session.Current()
	if !selection.active || current == nil || current.ID != selection.documentID || current.Text() != selection.source {
		return gfx.EventResult{Handled: true}
	}
	if selection.anchorVirtual != nil {
		if state.restoreRenderedStructureView(selection.anchorIndex, 0, selection.anchorVirtual) {
			state.selection = renderedSelection{}
			state.rich.renderedHistory, state.rich.sourceHistoryOffset = selection.history, selection.historyPosition
			state.rich.trimHistory(current)
			return gfx.EventResult{Handled: true, Repaint: true, Focus: richInputID}
		}
		return gfx.EventResult{Handled: true}
	}
	if selection.anchorIndex < 0 || selection.anchorIndex >= len(state.parsed.Blocks) {
		return gfx.EventResult{Handled: true}
	}
	visible := renderedPlain(&state.parsed, selection.anchorIndex)
	if selection.anchorOffset < 0 || selection.anchorOffset > len(visible) || !utf8.ValidString(visible[:selection.anchorOffset]) {
		return gfx.EventResult{Handled: true}
	}
	result := state.activateRendered(selection.anchorIndex)
	if state.rich.active {
		state.rich.input.MoveTo(utf8.RuneCountInString(visible[:selection.anchorOffset]), false)
		state.rich.renderedHistory = selection.history
		state.rich.sourceHistoryOffset = selection.historyPosition
		state.rich.trimHistory(current)
	}
	return result
}

func renderedPlain(document *markdown.Document, index int) string {
	block := &document.Blocks[index]
	if block.Kind == markdown.Code {
		return block.Text
	}
	var visible strings.Builder
	if block.Kind == markdown.Table {
		for cellIndex, cell := range document.Cells(block) {
			if cellIndex > 0 {
				if cellIndex%block.Columns == 0 {
					visible.WriteByte('\n')
				} else {
					visible.WriteByte('\t')
				}
			}
			for _, span := range document.CellSpans(cell) {
				visible.WriteString(span.Text)
			}
		}
		return visible.String()
	}
	for _, span := range document.Spans(block) {
		visible.WriteString(span.Text)
	}
	return visible.String()
}

func (selection *renderedSelection) setVirtualExtent(index int, document *markdown.Document) {
	selection.first, selection.last, selection.extentIndex = min(index, selection.anchorIndex), max(index, selection.anchorIndex-1), index
	selection.firstOffset, selection.lastOffset = 0, len(renderedPlain(document, selection.last))
}

func (selection *renderedSelection) refresh(document *markdown.Document, blocks []markdownedit.Block, mapping []int) {
	selection.start, selection.end, selection.prefix, selection.suffix, selection.editable = renderedSelectionSourceRange([]byte(selection.source), blocks, mapping, selection.first, selection.last, selection.firstOffset, selection.lastOffset)
	if anchor := selection.anchorVirtual; anchor != nil && selection.editable {
		if selection.extentIndex >= selection.anchorIndex {
			if anchor.start < 0 || anchor.start > selection.start || strings.Trim(selection.source[anchor.start:selection.start], " \t\r\n") != "" {
				selection.editable = false
			} else {
				selection.start, selection.prefix = anchor.start, ""
			}
		} else if anchor.start < selection.end || anchor.start > len(selection.source) || strings.Trim(selection.source[selection.end:anchor.start], " \t\r\n") != "" {
			selection.editable = false
		} else {
			selection.end, selection.suffix = anchor.start, ""
		}
	}
	var visible strings.Builder
	if selection.anchorVirtual != nil && selection.extentIndex >= selection.anchorIndex {
		visible.WriteByte('\n')
	}
	for row := selection.first; row <= selection.last; row++ {
		if row > selection.first {
			visible.WriteByte('\n')
		}
		value := renderedPlain(document, row)
		start, end := 0, len(value)
		if row == selection.first {
			start = selection.firstOffset
		}
		if row == selection.last {
			end = selection.lastOffset
		}
		visible.WriteString(value[start:end])
	}
	if selection.anchorVirtual != nil && selection.extentIndex < selection.anchorIndex {
		visible.WriteByte('\n')
	}
	selection.visible = visible.String()
}

func (state *model) finishRenderedSelection(selection renderedSelection, inserted string) gfx.EventResult {
	state.selection = renderedSelection{}
	current := state.session.Current()
	if !strings.ContainsAny(inserted, "\r\n") {
		anchor := ""
		if selection.anchorVirtual == nil {
			anchor = renderedPlain(&state.parsed, selection.anchorIndex)
		}
		beforeCaret := utf8.RuneCountInString(anchor[:selection.anchorOffset])
		first := renderedPlain(&state.parsed, selection.first)
		visiblePrefix := first[:selection.firstOffset] + inserted
		position := selection.start + len(selection.prefix) + len(inserted)
		source := current.Text()
		state.parsed.Parse(source)
		if inserted == "" && selection.prefix == "" && selection.suffix == "" && len(state.parsed.Blocks) == 0 && strings.Trim(source, "\r\n") == "" && position >= 0 && position <= len(source) {
			state.rich = renderedEditor{
				active: true, virtual: true, documentID: current.ID, source: source,
				input: text.NewEditor(""), block: markdownedit.Block{Kind: markdownedit.Paragraph},
				virtualStart: position, virtualEnd: len(source), virtualSuffix: source[position:],
			}
			state.rememberRenderedStructure(selection.history, selection.historyPosition, selection.source, selection.anchorIndex, beforeCaret, selection.anchorVirtual)
			return gfx.EventResult{Handled: true, Repaint: true, Focus: richInputID}
		}
		if inserted == "" && selection.prefix == "" && selection.suffix == "" && selection.anchorVirtual != nil {
			index := 0
			for _, block := range state.parsed.Blocks {
				if block.Start < position {
					index++
				}
			}
			state.rich = renderedEditor{
				documentID: current.ID, source: source,
				emptyTarget: &renderedEmptyTarget{index: index, start: position, end: position},
			}
			state.restoreRenderedEmptyTarget(current)
			if state.rich.active {
				state.rememberRenderedStructure(selection.history, selection.historyPosition, selection.source, selection.anchorIndex, beforeCaret, selection.anchorVirtual)
				return gfx.EventResult{Handled: true, Repaint: true, Focus: richInputID}
			}
		}
		blocks := markdownedit.Parse([]byte(source))
		for index, sourceIndex := range renderedMapping(&state.parsed, blocks) {
			if sourceIndex < 0 {
				continue
			}
			block := blocks[sourceIndex]
			if position < block.Start || position > block.End || !strings.HasPrefix(block.Visible, visiblePrefix) {
				continue
			}
			result := state.activateRendered(index)
			if state.rich.active {
				caret := utf8.RuneCountInString(visiblePrefix)
				state.rich.input.MoveTo(caret, false)
				state.rememberRenderedStructure(selection.history, selection.historyPosition, selection.source, selection.anchorIndex, beforeCaret, selection.anchorVirtual)
				return result
			}
		}
	}
	state.mode = editMode
	return gfx.EventResult{Handled: true, Repaint: true, Focus: sourceID}
}

func normalizeRenderedTasks(document *markdown.Document, blocks []markdownedit.Block) {
	if len(blocks) != len(document.Blocks) {
		return
	}
	for index, block := range blocks {
		if !block.Task || document.Blocks[index].Kind != markdown.Item {
			continue
		}
		spans := document.Spans(&document.Blocks[index])
		if len(spans) > 0 && len(spans[0].Text) >= 4 && spans[0].Text[0] == '[' && spans[0].Text[2] == ']' && spans[0].Text[3] == ' ' {
			spans[0].Text = spans[0].Text[4:]
		}
	}
}

func renderedMapping(document *markdown.Document, blocks []markdownedit.Block) []int {
	mapping := make([]int, len(document.Blocks))
	cursor := 0
	for index := range mapping {
		mapping[index] = -1
		for sourceIndex := cursor; sourceIndex < len(blocks); sourceIndex++ {
			if blocks[sourceIndex].Editable && blocks[sourceIndex].Start > document.Blocks[index].End {
				break
			}
			cursor = sourceIndex + 1
			if blocks[sourceIndex].Editable && renderedMatches(document, index, blocks[sourceIndex]) {
				mapping[index] = sourceIndex
				break
			}
		}
	}
	return mapping
}

func renderedSelectionSourceRange(source []byte, blocks []markdownedit.Block, mapping []int, first, last, firstOffset, lastOffset int) (int, int, string, string, bool) {
	if first < 0 || last >= len(mapping) || mapping[first] < 0 {
		return 0, 0, "", "", false
	}
	for row := first; row <= last; row++ {
		index := mapping[row]
		if index < 0 || index != mapping[first]+row-first {
			return 0, 0, "", "", false
		}
	}
	start, startErr := markdownedit.CrossBlockEdgeAt(source, blocks[mapping[first]], firstOffset, false)
	end, endErr := markdownedit.CrossBlockEdgeAt(source, blocks[mapping[last]], lastOffset, true)
	if startErr != nil || endErr != nil || start.Offset > end.Offset {
		return 0, 0, "", "", false
	}
	return start.Offset, end.Offset, start.Retain, end.Retain, true
}

func renderedMatches(document *markdown.Document, index int, block markdownedit.Block) bool {
	rendered := &document.Blocks[index]
	if block.Start < rendered.Start || block.End > rendered.End {
		return false
	}
	switch block.Kind {
	case markdownedit.Heading:
		if rendered.Kind != markdown.Heading {
			return false
		}
	case markdownedit.Paragraph, markdownedit.Quote:
		if rendered.Kind != markdown.Paragraph || (block.Kind == markdownedit.Quote) != (rendered.Quote > 0) {
			return false
		}
	case markdownedit.ListItem:
		if rendered.Kind != markdown.Item {
			return false
		}
	default:
		return false
	}
	var visible strings.Builder
	for _, span := range document.Spans(rendered) {
		visible.WriteString(span.Text)
	}
	return visible.String() == block.Visible
}

func (state *model) syncRendered(current *session.Document) {
	rich := &state.rich
	if !rich.active || rich.documentID != current.ID || rich.input == nil {
		return
	}
	if current.Text() != rich.source {
		state.rich = renderedEditor{}
		return
	}
	if _, composing := rich.input.Composition(); composing || rich.input.String() == rich.block.Visible && !rich.pendingCross {
		return
	}
	replacement := rich.input.String()
	if rich.virtual {
		next, err := virtualBlockSource(rich, replacement)
		if err != nil {
			if err := current.EditRange(rich.virtualStart, rich.virtualEnd, replacement+rich.virtualSuffix); err != nil {
				state.message = err.Error()
				state.rich = renderedEditor{}
				return
			}
			state.rich = renderedEditor{}
			state.mode = editMode
			state.message = "已切到 Markdown 源码模式继续编辑"
			state.focusRequest = sourceID
			return
		}
		before := rich.source
		if err := applyRenderedSource(current, before, rich.block, string(next)); err != nil {
			state.message = err.Error()
			return
		}
		if current.Text() != string(next) {
			state.message = "无法同步新段落；原文未改变"
			return
		}
		if before == current.Text() {
			rich.pendingCross = false
			return
		}
		if rich.virtualEdit {
			rich.pendingCross, rich.crossInputChange = true, true
			rich.crossSelection = rich.virtualBeforeSelect
			rich.rememberCrossHistory(before, current.Text(), rich.block.Visible, replacement, true)
		}
		rich.virtualEdit = false
		rich.virtualEnd = rich.virtualStart + len(replacement) + len(rich.virtualSuffix)
		rich.appliedEdits++
		rich.undoneEdits = 0
		rich.source = current.Text()
		if before != rich.source {
			rich.crossRedo, rich.structureRedo = nil, nil
		}
		rich.refreshVirtualBlock()
		rich.trimHistory(current)
		state.message = ""
		return
	}
	previous := rich.block
	var next []byte
	var err error
	if rich.pendingCross {
		end := len(replacement) - (len(previous.Visible) - rich.crossEnd)
		if end < rich.crossStart || !strings.HasPrefix(replacement, previous.Visible[:rich.crossStart]) || !strings.HasSuffix(replacement, previous.Visible[rich.crossEnd:]) {
			err = markdownedit.ErrReadOnlyBlock
		} else {
			next, err = markdownedit.ReplaceSelection([]byte(rich.source), previous, rich.crossStart, rich.crossEnd, replacement[rich.crossStart:end])
		}
	} else {
		next, err = markdownedit.Replace([]byte(rich.source), previous, replacement)
	}
	if err != nil {
		rich.pendingCross = false
		next, err = markdownedit.PatchRaw([]byte(rich.source), previous, replacement)
		if err != nil {
			caret := min(rich.input.Caret(), utf8.RuneCountInString(previous.Visible))
			rich.input = text.NewEditor(previous.Visible)
			rich.input.MoveTo(caret, false)
			rich.appliedEdits, rich.undoneEdits = 0, 0
			state.message = "跨格式边界无法安全编辑；原文未改，请切源码模式处理"
			state.invalidator.Invalidate()
			return
		}
		if err := applyRenderedSource(current, rich.source, previous, string(next)); err != nil {
			state.message = err.Error()
			state.rich = renderedEditor{}
			return
		}
		state.rich = renderedEditor{}
		state.mode = editMode
		state.message = "已切到 Markdown 源码模式继续编辑"
		state.focusRequest = sourceID
		return
	}
	beforeSource := rich.source
	if err := applyRenderedSource(current, rich.source, previous, string(next)); err != nil {
		state.message = err.Error()
		return
	}
	if beforeSource != current.Text() {
		rich.structureRedo = nil
	}
	rich.rememberCrossHistory(beforeSource, current.Text(), previous.Visible, replacement, false)
	rich.crossRedo = rich.crossRedo[:0]
	rich.appliedEdits++
	rich.undoneEdits = 0
	rich.trimHistory(current)
	rich.source = current.Text()
	blocks := markdownedit.Parse([]byte(rich.source))
	if rich.sourceIndex >= len(blocks) || blocks[rich.sourceIndex].Visible != replacement || !blocks[rich.sourceIndex].Editable {
		state.rich = renderedEditor{}
		return
	}
	rich.block = blocks[rich.sourceIndex]
	state.message = ""
}

func applyRenderedSource(current *session.Document, before string, block markdownedit.Block, after string) error {
	current.Editor.BreakUndoGroup()
	defer current.Editor.BreakUndoGroup()
	start := 0
	for start < len(before) && start < len(after) {
		original, originalSize := utf8.DecodeRuneInString(before[start:])
		replacement, replacementSize := utf8.DecodeRuneInString(after[start:])
		if original != replacement || originalSize != replacementSize {
			break
		}
		start += originalSize
	}
	endBefore, endAfter := len(before), len(after)
	for endBefore > start && endAfter > start {
		original, originalSize := utf8.DecodeLastRuneInString(before[:endBefore])
		replacement, replacementSize := utf8.DecodeLastRuneInString(after[:endAfter])
		if original != replacement || originalSize != replacementSize {
			break
		}
		endBefore -= originalSize
		endAfter -= replacementSize
	}
	if start == endBefore && start == endAfter {
		return nil
	}
	return current.EditRange(start, endBefore, after[start:endAfter])
}

func (state *model) onRenderedTextEvent(event app.TextEvent) (app.TextState, error) {
	rich := &state.rich
	if rich.input == nil {
		return app.TextState{}, nil
	}
	if current := state.session.Current(); current != nil && rich.active && rich.documentID == current.ID && rich.source == current.Text() && !rich.pendingCross && rich.input.String() != rich.block.Visible {
		if _, composing := rich.input.Composition(); !composing {
			var err error
			if rich.virtual {
				_, err = virtualBlockSource(rich, rich.input.String())
			} else {
				_, err = markdownedit.Replace([]byte(rich.source), rich.block, rich.input.String())
			}
			if err == nil {
				state.syncRendered(current)
				rich = &state.rich
			}
		}
	}
	if event.Kind == app.TextCancelComposition {
		// The input method cancelled: CancelComposition restores the text and
		// selection the marked string replaced, so this is not an edit and must
		// leave no composition-only state behind.
		event.Apply(rich.input)
		rich.compositionEdit = false
		rich.compositionStart = text.Range{}
		rich.pendingCross = false
		rich.virtualEdit = false
		state.invalidator.Invalidate()
		return app.TextStateOf(rich.input), nil
	}
	value := rich.input.String()
	if event.Kind != app.TextSetSelection || !rich.caretFromPointer || rich.caretByte < 0 || rich.caretByte > len(value) ||
		event.Selection.End != utf8.RuneCountInString(value[:rich.caretByte]) {
		rich.caretUpstream = false
	}
	rich.caretFromPointer, rich.wrapGoalActive = false, false
	if event.Kind == app.TextPasteNoText {
		current := state.session.Current()
		if current == nil || !rich.active || rich.virtual || rich.source != current.Text() || rich.block.Kind != markdownedit.Paragraph ||
			rich.block.Start < 0 || rich.block.End > len(rich.source) || rich.source[rich.block.Start:rich.block.End] != rich.block.Visible {
			state.message = "此排版位置无法安全粘贴图片；请切源码模式"
			return app.TextStateOf(rich.input), nil
		}
		selection := rich.input.Selection()
		if selection.Start != selection.End {
			state.message = "请将光标定位到单个文字位置再粘贴图片"
			return app.TextStateOf(rich.input), nil
		}
		position := rich.block.Start + byteOffsetOfRune(rich.input.String(), selection.Start)
		if err := state.pasteMarkdownClipboardImage(current, position, position); err != nil {
			state.message = err.Error()
			return app.TextStateOf(rich.input), nil
		}
		result := app.TextStateOf(rich.input)
		result.Redraw = true
		state.rich = renderedEditor{}
		state.mode = editMode
		state.focusRequest = sourceID
		state.invalidator.Invalidate()
		return result, nil
	}
	if event.Kind == app.TextCommandEvent && event.Command == app.TextDeleteBackward {
		if result := state.removeVirtualParagraph(); result.Handled {
			state.focusRequest = result.Focus
			state.invalidator.Invalidate()
			return app.TextStateOf(state.rich.input), nil
		}
		if result := state.mergeRenderedParagraph(); result.Handled {
			state.focusRequest = result.Focus
			state.invalidator.Invalidate()
			return app.TextStateOf(state.rich.input), nil
		}
	}
	marked, composing := rich.input.Composition()
	if rich.virtual && !composing && (event.Kind != app.TextCommandEvent || event.Command != app.TextUndo && event.Command != app.TextRedo) {
		rich.virtualBeforeSelect = rich.input.Selection()
		if rich.input.Caret() == rich.virtualBeforeSelect.Start {
			rich.virtualBeforeSelect.Start, rich.virtualBeforeSelect.End = rich.virtualBeforeSelect.End, rich.virtualBeforeSelect.Start
		}
		if event.HasReplacement {
			rich.virtualBeforeSelect = text.Range{Start: event.Replacement.Start, End: event.Replacement.End}
		}
		rich.virtualEdit = true
	}
	if composing {
		if event.HasReplacement && event.Replacement.Start == marked.Start && event.Replacement.End == marked.End &&
			(event.Kind == app.TextSetMarked || event.Kind == app.TextInsert || event.Kind == app.TextPaste) {
			event.HasReplacement = false
		}
		if event.Kind == app.TextPaste {
			event.Kind = app.TextInsert
		}
	}
	if event.Kind == app.TextSetMarked && !composing {
		rich.compositionStart = rich.input.Selection()
		if event.HasReplacement {
			rich.compositionStart = text.Range{Start: event.Replacement.Start, End: event.Replacement.End}
		}
		rich.compositionEdit = false
	}
	if event.Kind == app.TextCommandEvent && event.Command == app.TextUndo && composing {
		if rich.compositionEdit {
			event.Apply(rich.input)
		} else {
			rich.input.SetSelection(rich.compositionStart)
		}
		rich.pendingCross = false
		rich.virtualEdit = false
		return app.TextStateOf(rich.input), nil
	}
	if event.Kind == app.TextCommandEvent && !composing && (event.Command == app.TextUndo || event.Command == app.TextRedo) {
		state.historyReveal = true
		current := state.session.Current()
		if current == nil || current.ID != rich.documentID {
			return app.TextStateOf(rich.input), nil
		}
		stack := rich.crossUndo
		hasInputHistory := rich.input.CanUndo()
		if event.Command == app.TextRedo {
			stack = rich.crossRedo
			hasInputHistory = rich.input.CanRedo()
		}
		if len(stack) > 0 && (!stack[len(stack)-1].inputChanged || !hasInputHistory) {
			if result, handled := state.stepCrossHistory(event.Command == app.TextUndo, false); handled {
				return result, nil
			}
		}
	}
	if !rich.virtual && !composing && (event.Kind == app.TextInsert || event.Kind == app.TextPaste || event.Kind == app.TextSetMarked || event.Kind == app.TextCommandEvent && (event.Command == app.TextDeleteBackward || event.Command == app.TextDeleteForward)) {
		selection := rich.input.Selection()
		start, end := selection.Start, selection.End
		if event.HasReplacement {
			start, end = event.Replacement.Start, event.Replacement.End
		}
		value := rich.input.String()
		startByte, endByte := byteOffsetOfRune(value, start), byteOffsetOfRune(value, end)
		if !markdownedit.EditableRange(rich.block, startByte, endByte) {
			replacement := event.Text
			if event.Kind == app.TextCommandEvent {
				replacement = ""
			}
			if _, err := markdownedit.ReplaceSelection([]byte(rich.source), rich.block, startByte, endByte, replacement); err != nil {
				state.message = "跨格式选区无法安全替换；原文未改，请切源码模式处理"
				state.invalidator.Invalidate()
				result := app.TextStateOf(rich.input)
				result.Redraw = true
				return result, nil
			}
			rich.input.BreakUndoGroup()
			rich.pendingCross = true
			rich.crossStart, rich.crossEnd = startByte, endByte
			rich.crossSelection = text.Range{Start: start, End: end}
			caret := rich.input.Caret()
			if event.HasReplacement {
				caret = event.Replacement.End
			}
			if caret == start {
				rich.crossSelection.Start, rich.crossSelection.End = rich.crossSelection.End, rich.crossSelection.Start
			}
			rich.crossInputChange = false
		}
	}
	previous := rich.input.String()
	beforeSelection := rich.input.Selection()
	if rich.input.Caret() == beforeSelection.Start {
		beforeSelection.Start, beforeSelection.End = beforeSelection.End, beforeSelection.Start
	}
	if event.HasReplacement {
		beforeSelection = text.Range{Start: event.Replacement.Start, End: event.Replacement.End}
	}
	if composing {
		beforeSelection = rich.compositionStart
	}
	hadInputHistory := false
	if event.Kind == app.TextCommandEvent {
		switch event.Command {
		case app.TextUndo:
			hadInputHistory = rich.input.CanUndo()
		case app.TextRedo:
			hadInputHistory = rich.input.CanRedo()
		}
	}
	event.Apply(rich.input)
	if previous != rich.input.String() {
		if rich.pendingCross {
			rich.crossInputChange = true
		}
		if composing || event.Kind == app.TextSetMarked {
			rich.compositionEdit = true
		}
	}
	if event.Kind != app.TextCommandEvent || event.Command != app.TextUndo && event.Command != app.TextRedo {
		if previous != "" && rich.input.String() == "" {
			if _, pending := rich.input.Composition(); !pending && state.finishRenderedEmptyParagraph(beforeSelection) {
				return app.TextStateOf(state.rich.input), nil
			}
		}
		return app.TextStateOf(rich.input), nil
	}
	undo := event.Command == app.TextUndo
	if result, handled := state.stepCrossHistory(undo, hadInputHistory); handled {
		return result, nil
	}
	if previous == rich.input.String() {
		return state.stepSourceHistory(event.Command, hadInputHistory), nil
	}
	limit := rich.appliedEdits
	if !undo {
		limit = rich.undoneEdits
	}
	var next []byte
	var err error
	if rich.virtual {
		next, err = virtualBlockSource(rich, rich.input.String())
	} else {
		next, err = markdownedit.Replace([]byte(rich.source), rich.block, rich.input.String())
	}
	if err != nil {
		state.restoreRenderedHistory(undo, 0)
		return app.TextStateOf(rich.input), nil
	}
	current := state.session.Current()
	want := string(next)
	steps := 0
	for current.Text() != want && steps < limit {
		var changed bool
		if undo {
			changed = current.Editor.Undo()
		} else {
			changed = current.Editor.Redo()
		}
		if !changed {
			break
		}
		steps++
	}
	if current.Text() != want {
		state.restoreRenderedHistory(undo, steps)
		state.message = "无法同步撤销历史；原文未改变"
		return app.TextStateOf(rich.input), nil
	}
	if rich.virtual {
		rich.moveVirtualTextHistory(undo, steps)
	}
	if undo {
		rich.appliedEdits -= steps
		rich.undoneEdits += steps
	} else {
		rich.appliedEdits += steps
		rich.undoneEdits -= steps
	}
	rich.source = current.Text()
	rich.trimHistory(current)
	if rich.virtual {
		rich.virtualEnd = rich.virtualStart + len(rich.input.String()) + len(rich.virtualSuffix)
		rich.refreshVirtualBlock()
		return app.TextStateOf(rich.input), nil
	}
	blocks := markdownedit.Parse([]byte(rich.source))
	if rich.sourceIndex < len(blocks) && blocks[rich.sourceIndex].Editable && blocks[rich.sourceIndex].Visible == rich.input.String() {
		rich.block = blocks[rich.sourceIndex]
	}
	return app.TextStateOf(rich.input), nil
}

func (state *model) stepCrossHistory(undo, nativeApplied bool) (app.TextState, bool) {
	rich := &state.rich
	stack := rich.crossUndo
	if !undo {
		stack = rich.crossRedo
	}
	if len(stack) == 0 {
		return app.TextState{}, false
	}
	edit := stack[len(stack)-1]
	if edit.virtualText && nativeApplied {
		return app.TextState{}, false
	}
	position := edit.position
	if !undo {
		position--
	}
	if rich.appliedEdits+rich.sourceHistoryOffset != position {
		return app.TextState{}, false
	}
	current := state.session.Current()
	if current == nil || current.ID != rich.documentID {
		return app.TextState{}, false
	}
	wantVisible, haveSource, wantSource := edit.beforeVisibleHash, edit.afterSourceHash, edit.beforeSourceHash
	haveVisible := edit.afterVisibleHash
	haveVirtual := edit.afterVirtual
	if !undo {
		wantVisible, haveSource, wantSource = edit.afterVisibleHash, edit.beforeSourceHash, edit.afterSourceHash
		haveVisible = edit.beforeVisibleHash
		haveVirtual = edit.beforeVirtual
	}
	if sha256.Sum256([]byte(current.Text())) != haveSource || nativeApplied && rich.index != edit.index {
		return app.TextState{}, false
	}
	relocated := !nativeApplied && (!rich.active || rich.index != edit.index || haveVirtual != nil && !rich.virtual)
	if !nativeApplied && haveVirtual != nil {
		virtual := rich.virtualState()
		relocated = relocated || virtual == nil || *virtual != *haveVirtual
	}
	if relocated {
		target, valid := renderedHistoryTarget(current, edit.index, haveVirtual)
		if !valid {
			return app.TextState{}, false
		}
		target.renderedHistory = rich.renderedHistory
		target.sourceHistoryOffset = rich.appliedEdits + rich.sourceHistoryOffset
		rich = &target
	}
	expectedVisible := wantVisible
	if !nativeApplied {
		expectedVisible = haveVisible
	}
	if sha256.Sum256([]byte(rich.input.String())) != expectedVisible {
		return app.TextState{}, false
	}
	var changed bool
	if undo {
		changed = current.Editor.Undo()
	} else {
		changed = current.Editor.Redo()
	}
	block, sourceIndex, virtualEnd := markdownedit.Block{}, rich.sourceIndex, rich.virtualEnd
	valid := false
	if rich.virtual {
		virtual := rich.virtualState()
		virtual.end += len(current.Text()) - len(rich.source)
		candidate, ok := renderedVirtualHistoryEditor(current, rich.index, 0, virtual)
		if ok {
			block, sourceIndex, virtualEnd, valid = candidate.block, candidate.sourceIndex, candidate.virtualEnd, true
		}
	} else {
		blocks := markdownedit.Parse([]byte(current.Text()))
		if sourceIndex >= 0 && sourceIndex < len(blocks) && blocks[sourceIndex].Editable {
			block, valid = blocks[sourceIndex], true
		}
	}
	if !changed || sha256.Sum256([]byte(current.Text())) != wantSource || !valid || sha256.Sum256([]byte(block.Visible)) != wantVisible {
		steps := 0
		if changed {
			steps = 1
		}
		if nativeApplied {
			state.restoreRenderedHistory(undo, steps)
		} else if changed {
			if undo {
				current.Editor.Redo()
			} else {
				current.Editor.Undo()
			}
		}
		state.message = "无法同步跨格式撤销历史；原文未改变"
		return renderedHistoryInputState(state.rich.input), true
	}
	if relocated {
		state.rich = *rich
		rich = &state.rich
		state.focusRequest = richInputID
	}
	if undo {
		rich.crossUndo = rich.crossUndo[:len(rich.crossUndo)-1]
		rich.crossRedo = appendRenderedHistory(rich.crossRedo, edit)
		if nativeApplied {
			rich.appliedEdits--
			rich.undoneEdits++
		} else {
			rich.sourceHistoryOffset--
		}
	} else {
		rich.crossRedo = rich.crossRedo[:len(rich.crossRedo)-1]
		rich.crossUndo = appendRenderedHistory(rich.crossUndo, edit)
		if nativeApplied {
			rich.appliedEdits++
			rich.undoneEdits--
		} else {
			rich.sourceHistoryOffset++
		}
	}
	rich.source = current.Text()
	rich.block, rich.sourceIndex, rich.virtualEnd = block, sourceIndex, virtualEnd
	if !nativeApplied {
		if rich.input.String() != rich.block.Visible {
			rich.input = text.NewEditor(rich.block.Visible)
		}
		selection := edit.beforeSelect
		if !undo {
			selection = edit.afterSelect
		}
		rich.input.SetSelection(selection)
	}
	rich.trimHistory(current)
	state.message = ""
	return app.TextStateOf(rich.input), true
}

func (state *model) stepSourceHistory(command app.TextCommand, hadInputHistory bool) app.TextState {
	rich := &state.rich
	input := rich.input
	if hadInputHistory {
		return app.TextStateOf(input)
	}
	current := state.session.Current()
	if result, handled := state.stepStructureHistory(command == app.TextUndo); handled {
		return result
	}
	beforeSource := current.Text()
	changed := false
	switch command {
	case app.TextUndo:
		changed = current.Editor.Undo()
	case app.TextRedo:
		changed = current.Editor.Redo()
	}
	if !changed {
		return renderedHistoryInputState(input)
	}
	if result, restored := state.restoreSpecialSourceHistory(current, beforeSource, command); restored {
		return result
	}
	if state.restoreOtherRenderedHistory(current, beforeSource, command) {
		return app.TextStateOf(state.rich.input)
	}
	if input == nil {
		state.rich = renderedEditor{}
		state.mode = editMode
		state.message = "历史操作已切换到 Markdown 源码模式"
		state.focusRequest = sourceID
		return app.TextState{}
	}
	if rich.virtual {
		if state.restoreVirtualSourceHistory(current, command) {
			return app.TextStateOf(state.rich.input)
		}
		state.rich = renderedEditor{}
		state.mode = editMode
		state.message = "历史操作已切换到 Markdown 源码模式"
		state.focusRequest = sourceID
		return app.TextStateOf(input)
	}
	blocks := markdownedit.Parse([]byte(current.Text()))
	if rich.sourceIndex >= len(blocks) || !blocks[rich.sourceIndex].Editable || blocks[rich.sourceIndex].Kind != rich.block.Kind {
		state.rich = renderedEditor{}
		state.mode = editMode
		state.message = "历史操作已切换到 Markdown 源码模式"
		state.focusRequest = sourceID
		return app.TextStateOf(input)
	}
	block := blocks[rich.sourceIndex]
	if block.Start != rich.block.Start || current.Text()[:block.Start] != rich.source[:rich.block.Start] || current.Text()[block.End:] != rich.source[rich.block.End:] {
		state.rich = renderedEditor{}
		state.mode = editMode
		state.message = "历史操作已切换到 Markdown 源码模式"
		state.focusRequest = sourceID
		return app.TextStateOf(input)
	}
	selection := current.Editor.Selection()
	source := current.Text()
	from, to, err := markdownedit.VisibleRangeAtSource([]byte(source), block, byteOffsetOfRune(source, selection.Start), byteOffsetOfRune(source, selection.End))
	if err != nil {
		state.rich = renderedEditor{}
		state.mode = editMode
		state.message = "历史操作已切换到 Markdown 源码模式"
		state.focusRequest = sourceID
		return app.TextStateOf(input)
	}
	visible := text.Range{Start: utf8.RuneCountInString(block.Visible[:from]), End: utf8.RuneCountInString(block.Visible[:to])}
	if current.Editor.Caret() == selection.Start {
		visible.Start, visible.End = visible.End, visible.Start
	}
	rich.input = text.NewEditor(block.Visible)
	rich.input.SetSelection(visible)
	if command == app.TextUndo {
		rich.sourceHistoryOffset--
	} else {
		rich.sourceHistoryOffset++
	}
	rich.block = block
	rich.source = current.Text()
	rich.trimHistory(current)
	return app.TextStateOf(rich.input)
}

func (state *model) restoreRenderedHistory(undo bool, steps int) {
	current := state.session.Current()
	for range steps {
		if undo {
			current.Editor.Redo()
		} else {
			current.Editor.Undo()
		}
	}
	if undo {
		state.rich.input.Redo()
	} else {
		state.rich.input.Undo()
	}
}

func (state *model) insertRenderedNewline() gfx.EventResult {
	if !state.rich.active {
		return gfx.EventResult{}
	}
	if _, composing := state.rich.input.Composition(); composing {
		return gfx.EventResult{}
	}
	current := state.session.Current()
	value := state.rich.input.String()
	selection := state.rich.input.Selection()
	newline := current.Newline()
	if state.rich.virtual && value == "" && selection == (text.Range{}) {
		prior := state.rich
		switch prior.block.Kind {
		case markdownedit.Paragraph:
			if err := current.EditRange(prior.virtualStart, prior.virtualStart, newline); err != nil {
				state.message = err.Error()
				return gfx.EventResult{Handled: true, Repaint: true}
			}
			state.rich = renderedEditor{
				active: true, virtual: true, index: prior.index, documentID: current.ID,
				source: current.Text(), input: text.NewEditor(""), block: markdownedit.Block{Kind: markdownedit.Paragraph},
				virtualStart: prior.virtualStart + len(newline), virtualEnd: prior.virtualEnd + len(newline),
				virtualSuffix: prior.virtualSuffix, virtualInsert: newline, virtualDepth: prior.virtualDepth + 1,
			}
			state.rememberRenderedStructure(prior.renderedHistory, prior.appliedEdits+prior.sourceHistoryOffset, prior.source, prior.index, prior.input.Caret(), prior.virtualState())
			return gfx.EventResult{Handled: true, Repaint: true, Focus: richInputID}
		case markdownedit.ListItem:
			marker := strings.TrimPrefix(prior.virtualInsert, newline)
			start := prior.virtualStart - len(marker)
			if start < 0 || marker == prior.virtualInsert {
				break
			}
			if err := current.EditRange(start, prior.virtualStart, newline); err != nil {
				state.message = err.Error()
				return gfx.EventResult{Handled: true, Repaint: true}
			}
			state.rich = renderedEditor{
				active: true, virtual: true, index: prior.index, documentID: current.ID,
				source: current.Text(), input: text.NewEditor(""), block: markdownedit.Block{Kind: markdownedit.Paragraph},
				virtualStart: start + len(newline), virtualEnd: start + 2*len(newline),
				virtualSuffix: newline, virtualInsert: newline,
			}
			state.rememberRenderedStructure(prior.renderedHistory, prior.appliedEdits+prior.sourceHistoryOffset, prior.source, prior.index, prior.input.Caret(), prior.virtualState())
			return gfx.EventResult{Handled: true, Repaint: true, Focus: richInputID}
		}
	}
	blockEnd := state.rich.block.End
	prose := state.rich.block.Kind == markdownedit.Paragraph || state.rich.block.Kind == markdownedit.Heading
	terminal := blockEnd == len(current.Text()) || blockEnd+len(newline) == len(current.Text()) && strings.HasSuffix(current.Text(), newline)
	between := state.rich.index+1 < len(state.parsed.Blocks) && strings.HasPrefix(current.Text()[blockEnd:], newline+newline)
	if prose && (!state.rich.virtual || value != "") && selection.Start == selection.End && selection.End == state.rich.input.RuneCount() && (terminal || between) {
		prior := state.rich
		suffix := ""
		if terminal {
			suffix = current.Text()[blockEnd:]
		}
		if err := current.EditRange(blockEnd, blockEnd, newline+newline); err != nil {
			state.message = err.Error()
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		state.rich = renderedEditor{
			active: true, virtual: true, index: state.rich.index + 1, documentID: current.ID,
			source: current.Text(), input: text.NewEditor(""), block: markdownedit.Block{Kind: markdownedit.Paragraph},
			virtualStart: blockEnd + 2*len(newline), virtualEnd: blockEnd + 2*len(newline) + len(suffix), virtualSuffix: suffix, virtualInsert: newline + newline,
		}
		state.rememberRenderedStructure(prior.renderedHistory, prior.appliedEdits+prior.sourceHistoryOffset, prior.source, prior.index, prior.input.Caret(), prior.virtualState())
		return gfx.EventResult{Handled: true, Repaint: true, Focus: richInputID}
	}
	if state.rich.block.Kind == markdownedit.ListItem && (!state.rich.virtual || value != "") && selection.Start == selection.End && selection.End == state.rich.input.RuneCount() && (terminal || state.rich.index+1 < len(state.parsed.Blocks) && state.parsed.Blocks[state.rich.index+1].Kind == markdown.Item && strings.HasPrefix(current.Text()[blockEnd:], newline)) {
		lineStart := strings.LastIndexByte(current.Text()[:state.rich.block.Start], '\n') + 1
		marker := nextListMarker(current.Text()[lineStart:state.rich.block.Start])
		if marker != "" {
			prior := state.rich
			insert := newline + marker
			suffix := ""
			if terminal {
				suffix = current.Text()[blockEnd:]
			}
			if err := current.EditRange(blockEnd, blockEnd, insert); err != nil {
				state.message = err.Error()
				return gfx.EventResult{Handled: true, Repaint: true}
			}
			state.rich = renderedEditor{
				active: true, virtual: true, index: state.rich.index + 1, documentID: current.ID,
				source: current.Text(), input: text.NewEditor(""), block: markdownedit.Block{Kind: markdownedit.ListItem},
				virtualStart: blockEnd + len(insert), virtualEnd: blockEnd + len(insert) + len(suffix), virtualSuffix: suffix, virtualInsert: insert,
			}
			state.rememberRenderedStructure(prior.renderedHistory, prior.appliedEdits+prior.sourceHistoryOffset, prior.source, prior.index, prior.input.Caret(), prior.virtualState())
			return gfx.EventResult{Handled: true, Repaint: true, Focus: richInputID}
		}
	}
	source := []byte(current.Text())
	startByte, endByte := byteOffsetOfRune(value, selection.Start), byteOffsetOfRune(value, selection.End)
	begin, beginErr := markdownedit.CrossBlockEdgeAt(source, state.rich.block, startByte, false)
	finish, finishErr := markdownedit.CrossBlockEdgeAt(source, state.rich.block, endByte, true)
	mapped := beginErr == nil && finishErr == nil && begin.Offset <= finish.Offset
	if prose && mapped && selection.Start == selection.End && selection.Start > 0 && selection.End < state.rich.input.RuneCount() {
		insert := begin.Retain + newline + newline + finish.Retain
		candidate := string(source[:begin.Offset]) + insert + string(source[finish.Offset:])
		blocks := markdownedit.Parse([]byte(candidate))
		left, right, rightIndex := value[:startByte], value[endByte:], -1
		leftEnd := begin.Offset + len(begin.Retain)
		rightStart := leftEnd + 2*len(newline)
		rightEnd := state.rich.block.End + len(insert) - (finish.Offset - begin.Offset)
		validLeft := false
		for index, block := range blocks {
			if !block.Editable {
				continue
			}
			if block.Kind == state.rich.block.Kind && block.Start == state.rich.block.Start && block.End == leftEnd && block.Visible == left {
				validLeft = true
			}
			if block.Kind == markdownedit.Paragraph && block.Start == rightStart && block.End == rightEnd && block.Visible == right {
				rightIndex = index
			}
		}
		if validLeft && rightIndex >= 0 {
			index := state.rich.index + 1
			if err := current.EditRange(begin.Offset, finish.Offset, insert); err != nil {
				state.message = err.Error()
				return gfx.EventResult{Handled: true, Repaint: true}
			}
			state.parsed.Parse(current.Text())
			mapping := renderedMapping(&state.parsed, blocks)
			if current.Text() == candidate && index < len(mapping) && mapping[index] == rightIndex {
				input := text.NewEditor(right)
				input.MoveTo(0, false)
				history, position := state.rich.renderedHistory, state.rich.appliedEdits+state.rich.sourceHistoryOffset
				beforeVirtual := state.rich.virtualState()
				state.rich = renderedEditor{
					active: true, index: index, sourceIndex: rightIndex, documentID: current.ID,
					source: current.Text(), block: blocks[rightIndex], input: input,
				}
				state.rememberRenderedStructure(history, position, string(source), index-1, selection.End, beforeVirtual)
				return gfx.EventResult{Handled: true, Repaint: true, Focus: richInputID}
			}
			state.rich = renderedEditor{}
			state.mode = editMode
			state.message = "已切到 Markdown 源码模式继续编辑"
			return gfx.EventResult{Handled: true, Repaint: true, Focus: sourceID}
		}
	}
	var next []byte
	var err error
	if mapped {
		next = append([]byte(nil), source[:begin.Offset]...)
		next = append(next, begin.Retain...)
		next = append(next, newline...)
		next = append(next, finish.Retain...)
		next = append(next, source[finish.Offset:]...)
	} else {
		next, err = markdownedit.PatchRaw(source, state.rich.block, value[:startByte]+newline+value[endByte:])
	}
	if err != nil {
		state.message = "无法安全换行；原文未改，请切源码模式处理"
		return gfx.EventResult{Handled: true, Repaint: true, Focus: richInputID}
	}
	if err := applyRenderedSource(current, string(source), state.rich.block, string(next)); err != nil {
		state.message = err.Error()
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	state.rich = renderedEditor{}
	state.mode = editMode
	state.message = "已切到 Markdown 源码模式继续编辑"
	return gfx.EventResult{Handled: true, Repaint: true, Focus: sourceID}
}

func nextListMarker(marker string) string {
	if marker == "- " || marker == "* " || marker == "+ " {
		return marker
	}
	if len(marker) < 3 || marker[len(marker)-1] != ' ' || marker[len(marker)-2] != '.' && marker[len(marker)-2] != ')' {
		return ""
	}
	for _, digit := range marker[:len(marker)-2] {
		if digit < '0' || digit > '9' {
			return ""
		}
	}
	number, err := strconv.Atoi(marker[:len(marker)-2])
	if err != nil || number == int(^uint(0)>>1) {
		return ""
	}
	return strconv.Itoa(number+1) + marker[len(marker)-2:]
}

func (state *model) removeVirtualParagraph() gfx.EventResult {
	rich := &state.rich
	if !rich.active || !rich.virtual || rich.index == 0 && rich.virtualDepth == 0 || rich.appliedEdits != 0 || rich.input.String() != "" || rich.input.Selection() != (text.Range{}) {
		return gfx.EventResult{}
	}
	current := state.session.Current()
	if current == nil || current.ID != rich.documentID || current.Text() != rich.source {
		return gfx.EventResult{}
	}
	if _, handled := state.stepStructureHistory(true); handled {
		return gfx.EventResult{Handled: true, Repaint: true, Focus: state.focusRequest}
	}
	return gfx.EventResult{}
}

func (state *model) mergeRenderedParagraph() gfx.EventResult {
	rich := &state.rich
	if !rich.active || rich.virtual || rich.block.Kind != markdownedit.Paragraph || rich.index < 1 {
		return gfx.EventResult{}
	}
	if _, composing := rich.input.Composition(); composing {
		return gfx.EventResult{}
	}
	selection := rich.input.Selection()
	if selection.Start != 0 || selection.End != 0 {
		return gfx.EventResult{}
	}
	current := state.session.Current()
	if current == nil || current.Text() != rich.source {
		return gfx.EventResult{}
	}
	blocks := markdownedit.Parse([]byte(rich.source))
	mapping := renderedMapping(&state.parsed, blocks)
	previousIndex := rich.index - 1
	if previousIndex >= len(mapping) || mapping[previousIndex] < 0 {
		return gfx.EventResult{}
	}
	previous := blocks[mapping[previousIndex]]
	if (previous.Kind != markdownedit.Paragraph && previous.Kind != markdownedit.Heading) || previous.End > rich.block.Start || rich.block.Start > len(rich.source) || rich.source[previous.End:rich.block.Start] != current.Newline()+current.Newline() {
		return gfx.EventResult{}
	}
	merged := previous.Visible + rich.block.Visible
	candidate := rich.source[:previous.End] + rich.source[rich.block.Start:]
	parsed := markdownedit.Parse([]byte(candidate))
	mergedIndex := -1
	for index, block := range parsed {
		if block.Kind == previous.Kind && block.Editable && block.Start == previous.Start && block.End == previous.Start+len(merged) && block.Visible == merged {
			mergedIndex = index
			break
		}
	}
	if mergedIndex < 0 {
		return gfx.EventResult{}
	}
	if err := current.EditRange(previous.End, rich.block.Start, ""); err != nil {
		state.message = err.Error()
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	state.parsed.Parse(current.Text())
	nextMapping := renderedMapping(&state.parsed, parsed)
	if current.Text() != candidate || previousIndex >= len(nextMapping) || nextMapping[previousIndex] != mergedIndex {
		state.rich = renderedEditor{}
		state.mode = editMode
		state.message = "已切到 Markdown 源码模式继续编辑"
		return gfx.EventResult{Handled: true, Repaint: true, Focus: sourceID}
	}
	input := text.NewEditor(merged)
	caret := utf8.RuneCountInString(previous.Visible)
	input.MoveTo(caret, false)
	history, position, beforeSource, beforeIndex := rich.renderedHistory, rich.appliedEdits+rich.sourceHistoryOffset, rich.source, rich.index
	state.rich = renderedEditor{
		active: true, index: previousIndex, sourceIndex: mergedIndex, documentID: current.ID,
		source: current.Text(), block: parsed[mergedIndex], input: input,
	}
	state.rememberRenderedStructure(history, position, beforeSource, beforeIndex, 0, nil)
	return gfx.EventResult{Handled: true, Repaint: true, Focus: richInputID}
}

func (rich *renderedEditor) refreshVirtualBlock() {
	kind := rich.block.Kind
	rich.block = markdownedit.Block{Kind: kind, Visible: rich.input.String()}
	if rich.block.Visible == "" {
		return
	}
	for index, block := range markdownedit.Parse([]byte(rich.source)) {
		if block.Kind == kind && block.Editable && block.Start == rich.virtualStart && block.Visible == rich.block.Visible {
			rich.block = block
			rich.sourceIndex = index
			return
		}
	}
}

func (state *model) finishRenderedEmptyParagraph(beforeSelection text.Range) bool {
	rich := &state.rich
	current := state.session.Current()
	if current == nil || !rich.active || rich.virtual || rich.documentID != current.ID || rich.source != current.Text() ||
		rich.block.Kind != markdownedit.Paragraph || rich.block.Start < 0 || rich.block.End > len(rich.source) ||
		rich.source[rich.block.Start:rich.block.End] != rich.block.Visible {
		return false
	}
	next, err := markdownedit.PatchRaw([]byte(rich.source), rich.block, "")
	if err != nil {
		return false
	}
	beforeBlocks, afterBlocks := markdownedit.Parse([]byte(rich.source)), markdownedit.Parse(next)
	if rich.sourceIndex < 0 || rich.sourceIndex >= len(beforeBlocks) || len(afterBlocks)+1 != len(beforeBlocks) ||
		beforeBlocks[rich.sourceIndex].Start != rich.block.Start || beforeBlocks[rich.sourceIndex].End != rich.block.End {
		return false
	}
	for index, before := range beforeBlocks {
		if index == rich.sourceIndex {
			continue
		}
		afterIndex := index
		if index > rich.sourceIndex {
			afterIndex--
			before.Start -= rich.block.End - rich.block.Start
			before.End -= rich.block.End - rich.block.Start
		}
		after := afterBlocks[afterIndex]
		if before.Kind != after.Kind || before.Start != after.Start || before.End != after.End || before.Visible != after.Visible ||
			before.Editable != after.Editable || before.Task != after.Task || before.Checked != after.Checked {
			return false
		}
	}
	if err := applyRenderedSource(current, rich.source, rich.block, string(next)); err != nil {
		state.message = err.Error()
		return false
	}
	rich.virtual = true
	rich.virtualStart, rich.virtualEnd = rich.block.Start, rich.block.End
	rich.virtualSuffix = ""
	rich.pendingCross, rich.crossInputChange, rich.crossSelection = true, true, beforeSelection
	rich.rememberCrossHistory(rich.source, current.Text(), rich.block.Visible, "", true)
	rich.appliedEdits++
	rich.undoneEdits = 0
	rich.source, rich.virtualEnd = current.Text(), rich.virtualStart
	rich.block = markdownedit.Block{Kind: markdownedit.Paragraph}
	rich.crossRedo, rich.structureRedo = nil, nil
	rich.trimHistory(current)
	state.focusRequest = richInputID
	state.invalidator.Invalidate()
	return true
}

func virtualBlockSource(rich *renderedEditor, value string) ([]byte, error) {
	if rich.virtualStart < 0 || rich.virtualEnd > len(rich.source) || rich.virtualStart > rich.virtualEnd || rich.virtualEnd-rich.virtualStart < len(rich.virtualSuffix) || !strings.HasSuffix(rich.source[rich.virtualStart:rich.virtualEnd], rich.virtualSuffix) {
		return nil, markdownedit.ErrStaleBlock
	}
	next := []byte(rich.source[:rich.virtualStart] + value + rich.virtualSuffix + rich.source[rich.virtualEnd:])
	if value == "" {
		return next, nil
	}
	for _, block := range markdownedit.Parse(next) {
		if block.Kind == rich.block.Kind && block.Editable && block.Start == rich.virtualStart && block.End == rich.virtualStart+len(value) && block.Visible == value {
			return next, nil
		}
	}
	return nil, markdownedit.ErrReadOnlyBlock
}

func (state *model) virtualListRow() bool {
	rich := &state.rich
	return rich.active && rich.virtual && rich.block.Kind == markdownedit.ListItem && rich.input.String() == "" && rich.index < len(state.parsed.Blocks) && state.parsed.Blocks[rich.index].Kind == markdown.Item && renderedPlain(&state.parsed, rich.index) == ""
}

func (state *model) virtualParagraphNeedsPlaceholder() bool {
	return state.rich.virtual && state.rich.block.Visible == "" && !state.virtualListRow()
}

func (state *model) virtualParagraphDepth() int {
	return state.rich.virtualDepth
}

func (state *model) virtualParagraphTop() float32 {
	height := float32(max(1, len(state.rich.wrapRows))) * 26
	paragraphGap := markdownParagraphGap(state.dark, 0)
	top := state.preview.Height() - height - paragraphGap
	if state.rich.index == 0 {
		top = float32(state.virtualParagraphDepth()) * (26 + paragraphGap)
	}
	if state.rich.index > 0 && state.rich.index-1 < len(state.preview.blocks) {
		previous := state.preview.blocks[state.rich.index-1]
		top = previous.top + previous.height + markdownParagraphGap(state.dark, previous.heading) + float32(state.virtualParagraphDepth())*(26+paragraphGap)
	}
	return top
}

func (state *model) paintRendered(canvas *gfx.Canvas, theme ui.Theme, palette colors, bounds ui.Rect) {
	appearance := state.markdownAppearance(palette)
	palette = appearance.palette
	current := state.session.Current()
	if state.selection.active && (state.selection.documentID != current.ID || state.selection.source != current.Text()) {
		state.selection = renderedSelection{}
	}
	if !state.rich.active && state.rich.emptyTarget != nil && byteOffsetOfRune(current.Text(), current.Editor.Caret()) != state.rich.emptyTarget.start {
		state.rich.emptyTarget = nil
	}
	state.restoreRenderedEmptyTarget(current)
	if !state.rich.active && !current.Composing() && strings.Trim(current.Text(), "\r\n") == "" {
		source := current.Text()
		position := byteOffsetOfRune(source, current.Editor.Caret())
		if position > 0 && position < len(source) && source[position-1] == '\r' && source[position] == '\n' {
			position--
		}
		history, offset := state.renderedHistorySnapshot(current)
		virtual := &renderedVirtualState{kind: markdownedit.Paragraph, start: position, end: len(source), suffix: source[position:]}
		if state.restoreRenderedStructureView(0, 0, virtual) {
			state.rich.renderedHistory, state.rich.sourceHistoryOffset = history, offset
			state.rich.trimHistory(current)
		}
	}
	state.paintPreview(canvas, theme, palette, bounds)
	state.rich.rect = ui.Rect{}
	blocks := markdownedit.Parse([]byte(current.Text()))
	mapping := renderedMapping(&state.parsed, blocks)
	x, width := state.markdownColumn(bounds)
	y := bounds.Y + 46 - state.previewScroll
	canvas.Clip(bounds)
	defer canvas.Unclip()
	if state.selection.active {
		selectionBounds := ui.Rect{X: bounds.X + 5, Y: bounds.Y, Width: max(0, bounds.Width-10), Height: bounds.Height}
		canvas.HitWithOptions(renderedSelectionID, selectionBounds, "渲染选区", gfx.HitOptions{Role: gfx.RoleButton, Selected: true})
	}
	findCursor := 0
	tableCellBase := 0
	for index := range state.preview.blocks {
		layout := &state.preview.blocks[index]
		row := ui.Rect{X: x, Y: y + layout.top, Width: width, Height: max(26, layout.height)}
		lineHeight := float32(26)
		if layout.kind == markdown.Heading {
			_, lineHeight, _ = markdownHeadingMetrics(state.parsed.Blocks[index].Level, state.dark)
		}
		cellBase := tableCellBase
		if layout.kind == markdown.Table {
			tableCellBase += len(layout.table.cells)
		}
		if state.selection.placeExtent && index == state.selection.extentIndex {
			if mapping[index] >= 0 {
				value := blocks[mapping[index]].Visible
				textX, fontSize, weight := renderedTextGeometry(&state.parsed, index, row, state.dark)
				offset := canvas.TextOffsetAt(value, fontSize, weight, state.selection.clickX-textX)
				if placed, ok := layout.view.OffsetAt(ui.Point{X: state.selection.clickX, Y: state.selection.clickY}); ok {
					offset = placed
				}
				if state.selection.anchorVirtual != nil && state.selection.extentIndex >= state.selection.anchorIndex || state.selection.anchorVirtual == nil && state.selection.anchorIndex == state.selection.first {
					state.selection.lastOffset = offset
				} else {
					state.selection.firstOffset = offset
				}
				state.selection.refresh(&state.parsed, blocks, mapping)
			}
			state.selection.placeExtent = false
		}
		if row.Y+row.Height < bounds.Y || row.Y >= bounds.Y+bounds.Height {
			continue
		}
		if state.codeBlock.active && state.codeBlock.renderedIndex == index {
			state.paintCodeBlock(canvas, theme, palette, row)
			continue
		}
		if layout.kind == markdown.Table {
			state.paintTableCellHits(canvas, palette, index, cellBase, x, y, layout)
			continue
		}
		if state.selection.active && index >= state.selection.first && index <= state.selection.last && mapping[index] < 0 {
			selectionColor := palette.accent
			selectionColor.Alpha = 0.18
			canvas.Fill(row, selectionColor)
		}
		if state.findOpen && state.findMode == renderedMode {
			value := renderedPlain(&state.parsed, index)
			textX, fontSize, weight := renderedTextGeometry(&state.parsed, index, row, state.dark)
			for findCursor < len(state.findVisual) && state.findVisual[findCursor].block < index {
				findCursor++
			}
			for findCursor < len(state.findVisual) && state.findVisual[findCursor].block == index {
				match := state.findVisual[findCursor]
				color := palette.accent
				color.Alpha = 0.18
				if findCursor == state.findIndex {
					color.Alpha = 0.36
				}
				for _, span := range canvas.TextSelection(nil, value, fontSize, weight, match.start, match.end) {
					canvas.Fill(ui.Rect{X: textX + span.X, Y: row.Y, Width: span.Width, Height: min(lineHeight, fontSize*1.4)}, color)
				}
				findCursor++
			}
		}
		if state.rich.active && state.rich.index == index && !state.virtualParagraphNeedsPlaceholder() {
			block := state.rich.block
			inputBounds := ui.Rect{X: row.X, Y: row.Y, Width: row.Width, Height: max(26, row.Height)}
			textX, fontSize, weight := renderedTextGeometry(&state.parsed, index, row, state.dark)
			textColor := markdownHeadingColor(layout.heading, state.dark, palette.ink)
			if layout.quoted {
				textColor = palette.muted
			}
			spans := state.parsed.Spans(&state.parsed.Blocks[index])
			if len(spans) == 1 && spans[0].Link != "" {
				textColor = appearance.link
			}
			if block.Kind == markdownedit.ListItem && !block.Task && !state.activeMixedSpan() {
				marker := "•"
				if number := state.parsed.Blocks[index].Number; number > 0 {
					marker = strconv.Itoa(number) + "."
				}
				markerWidth := canvas.TextWidthWeight(marker, 16, gfx.WeightRegular)
				markerColor := palette.ink
				if layout.quoted {
					markerColor = palette.muted
				}
				canvas.Label(marker, ui.Rect{X: textX - 6 - markerWidth, Y: row.Y, Width: markerWidth + 2, Height: lineHeight}, gfx.TextStyle{Size: 16, Color: markerColor})
			}
			if block.Kind == markdownedit.Quote && !state.activeMixedSpan() {
				canvas.Fill(ui.Rect{X: row.X + 2, Y: row.Y, Width: 3, Height: inputBounds.Height}, palette.border)
			}
			inputBounds.X, inputBounds.Width = textX, inputBounds.Width-(textX-row.X)
			hitBounds := inputBounds
			value := state.rich.input.String()
			state.rich.mixed = state.activeMixedSpan()
			state.rich.preview = &state.preview
			state.rich.inlineSpans = append(state.rich.inlineSpans[:0], spans...)
			state.rich.wrapped = false
			if !state.rich.mixed {
				state.rich.wrapRows = canvas.WrapRows(state.rich.wrapRows[:0], value, fontSize, weight, inputBounds.Width)
				state.rich.wrapped = len(state.rich.wrapRows) > 1
				if layout.kind == markdown.Heading {
					inputBounds.Height = float32(max(1, len(state.rich.wrapRows))) * lineHeight
				}
			}
			state.rich.wrapCanvas = canvas
			if state.rich.wrapWidth != inputBounds.Width || state.rich.wrapSize != fontSize || state.rich.wrapWeight != weight || state.rich.wrapLineHeight != lineHeight {
				state.rich.caretUpstream, state.rich.caretFromPointer, state.rich.wrapGoalActive = false, false, false
			}
			state.rich.wrapWidth = inputBounds.Width
			state.rich.wrapX, state.rich.wrapY = inputBounds.X, inputBounds.Y
			state.rich.wrapSize, state.rich.wrapWeight, state.rich.wrapLineHeight = fontSize, weight, lineHeight
			if state.rich.placeDragAnchor {
				offset := canvas.TextOffsetAt(value, fontSize, weight, state.rich.dragAnchorX-inputBounds.X)
				if state.rich.wrapped || state.rich.mixed {
					offset, _ = state.rich.OffsetAt(ui.Point{X: state.rich.dragAnchorX, Y: state.rich.dragAnchorY})
				}
				state.rich.dragAnchor = utf8.RuneCountInString(value[:offset])
				state.rich.placeDragAnchor = false
			}
			if state.rich.placeCaret {
				offset := canvas.TextOffsetAt(value, fontSize, weight, state.rich.clickX-inputBounds.X)
				if state.rich.wrapped || state.rich.mixed {
					offset, _ = state.rich.OffsetAt(ui.Point{X: state.rich.clickX, Y: state.rich.clickY})
				}
				state.rich.input.MoveTo(utf8.RuneCountInString(value[:offset]), false)
				state.rich.placeCaret = false
			}
			if state.rich.mixed {
				if canvas.Focused() == richInputID {
					if caret, ok := state.rich.CaretBounds(); ok {
						canvas.Fill(caret, palette.ink)
					}
				}
			} else {
				state.rich.paintTextRows(canvas, inputBounds, textColor, palette.accent)
			}
			composingConfirmation := state.confirmationCompositionFocus() == richInputID
			if composingConfirmation {
				canvas.PushFocusScope(confirmScope)
			}
			canvas.HitWithOptions(richInputID, hitBounds, "渲染块编辑器", gfx.HitOptions{
				Role: gfx.RoleTextField, Value: value, TextBounds: bounds,
				TextSize: fontSize, TextWeight: weight, TextColor: ui.Color{}, NavigationKeys: true,
				HideNativeOverlays: true, Placement: &state.rich,
			})
			if composingConfirmation {
				canvas.PopFocusScope()
			}
			state.rich.rect = inputBounds
			continue
		}
		label := "源码编辑 Markdown 块"
		if mapping[index] >= 0 {
			label = "编辑 " + blocks[mapping[index]].Visible
		}
		id := renderedRows.At(index)
		canvas.HitWithOptions(id, row, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: "复杂块切换到源码"})
		if canvas.Clicked(id) {
			state.focusRequest = state.activateRendered(index).Focus
			state.invalidator.Invalidate()
		}
	}
	if state.rich.active && state.virtualParagraphNeedsPlaceholder() {
		height := float32(max(1, len(state.rich.wrapRows))) * 26
		top := state.virtualParagraphTop()
		row := ui.Rect{X: x, Y: y + top, Width: width, Height: height}
		if row.Y+row.Height >= bounds.Y && row.Y < bounds.Y+bounds.Height {
			value := state.rich.input.String()
			state.rich.mixed, state.rich.wrapped = false, len(state.rich.wrapRows) > 1
			state.rich.wrapCanvas = canvas
			state.rich.wrapX, state.rich.wrapY = row.X, row.Y
			state.rich.wrapSize, state.rich.wrapWeight, state.rich.wrapLineHeight = 16, gfx.WeightRegular, 26
			state.rich.paintTextRows(canvas, row, palette.ink, palette.accent)
			composingConfirmation := state.confirmationCompositionFocus() == richInputID
			if composingConfirmation {
				canvas.PushFocusScope(confirmScope)
			}
			canvas.HitWithOptions(richInputID, row, "新段落", gfx.HitOptions{
				Role: gfx.RoleTextField, Value: value, TextBounds: bounds,
				TextSize: 16, TextWeight: gfx.WeightRegular, TextColor: ui.Color{}, NavigationKeys: true,
				HideNativeOverlays: true, Placement: &state.rich,
			})
			if composingConfirmation {
				canvas.PopFocusScope()
			}
			state.rich.rect = row
		}
	}
	blocks = markdownedit.Parse([]byte(current.Text()))
	if len(blocks) == len(state.preview.blocks) {
		for index, block := range blocks {
			if !block.Task {
				continue
			}
			marker := ui.Rect{X: x + 2, Y: y + state.preview.blocks[index].top, Width: 20, Height: 26}
			if marker.Y+marker.Height < bounds.Y || marker.Y >= bounds.Y+bounds.Height {
				continue
			}
			id := taskChecks.At(index)
			canvas.HitWithOptions(id, marker, "任务 "+block.Visible, gfx.HitOptions{Role: gfx.RoleCheckbox, Checked: block.Checked})
			if canvas.Clicked(id) {
				state.toggleTaskAt(index)
			}
		}
	}
	if state.rich.active && (state.rich.wrapped || state.rich.mixed) && canvas.Focused() == richInputID {
		if caret, ok := state.rich.CaretBounds(); ok {
			previous := state.previewScroll
			limit := max(0, state.preview.Height()-bounds.Height+60)
			if caret.Y+caret.Height > bounds.Y+bounds.Height-12 {
				state.previewScroll = min(limit, previous+caret.Y+caret.Height-(bounds.Y+bounds.Height-12))
			} else if caret.Y < bounds.Y+8 {
				state.previewScroll = max(0, previous-(bounds.Y+8-caret.Y))
			}
			if state.previewScroll != previous {
				state.invalidator.Invalidate()
			}
		}
	}
}

func (state *model) toggleTaskAt(index int) {
	current := state.session.Current()
	if current == nil {
		return
	}
	source := current.Text()
	blocks := markdownedit.Parse([]byte(source))
	if len(blocks) != len(state.parsed.Blocks) || index < 0 || index >= len(blocks) || !blocks[index].Task {
		return
	}
	block := blocks[index]
	next, err := markdownedit.ToggleTask([]byte(current.Text()), block)
	if err != nil {
		state.message = err.Error()
		return
	}
	marker := string(next[block.TaskStart+1 : block.TaskStart+2])
	if err := current.EditRange(block.TaskStart+1, block.TaskStart+2, marker); err != nil {
		state.message = err.Error()
		return
	}
	if state.rich.active && state.rich.index == index {
		state.rich.source = current.Text()
		state.rich.block = markdownedit.Parse([]byte(current.Text()))[index]
		state.resetRenderedFormatHistory(source)
		state.focusRequest = richInputID
	}
	state.invalidator.Invalidate()
}

func renderedTextGeometry(document *markdown.Document, index int, row ui.Rect, night bool) (float32, float32, float32) {
	block := &document.Blocks[index]
	x, size, weight := row.X, float32(16), gfx.WeightRegular
	if block.Kind == markdown.Heading {
		size, _, _ = markdownHeadingMetrics(block.Level, night)
		weight = gfx.WeightSemibold
		if block.Level == 1 {
			weight = gfx.WeightBold
		}
	}
	if block.Kind == markdown.Item {
		x += 24
	}
	if block.Quote > 0 {
		x += 14
	}
	if block.Kind == markdown.Paragraph {
		spans := document.Spans(block)
		if len(spans) == 1 {
			switch {
			case spans[0].Style&markdown.Bold != 0:
				weight = gfx.WeightBold
			case spans[0].Style&markdown.Italic != 0:
				weight = gfx.WeightMedium
			}
		}
	}
	return x, size, weight
}
