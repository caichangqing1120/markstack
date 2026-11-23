package main

import (
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"markstack/internal/markdownedit"
	"markstack/internal/session"
)

type tableEditor struct {
	active        bool
	documentID    int
	renderedIndex int
	sourceIndex   int
	row           int
	column        int
	source        string
	block         markdownedit.Block
	cell          markdownedit.Cell
	input         *text.Editor
	textInput     app.TextInput
	columns       []float32
	layoutWidth   float32

	textCanvas    *gfx.Canvas
	textBounds    ui.Rect
	textStyle     gfx.TextStyle
	textAlignment gfx.Alignment
	caretBlink    components.CaretBlink

	compositionBackward bool
}

func (state *model) tableComposing() bool {
	_, composing := state.table.input.Composition()
	return composing
}

func (table *tableEditor) OffsetAt(point ui.Point) (int, bool) {
	if table.textCanvas == nil || table.input == nil {
		return 0, false
	}
	value := table.input.String()
	origin := table.textCanvas.LabelOrigin(value, table.textBounds, table.textStyle, table.textAlignment)
	return table.textCanvas.TextOffsetAtStyle(value, table.textStyle, point.X-origin), true
}

func (table *tableEditor) CaretBounds() (ui.Rect, bool) {
	if table.textCanvas == nil || table.input == nil {
		return ui.Rect{}, false
	}
	value := table.input.String()
	origin := table.textCanvas.LabelOrigin(value, table.textBounds, table.textStyle, table.textAlignment)
	caret := table.textCanvas.TextCaretStyle(value, table.textStyle, byteOffsetOfRune(value, table.input.Caret()))
	band := table.textBand()
	band.X = max(band.X, min(band.X+band.Width-2, origin+caret))
	band.Width = 2
	return band, true
}

func (table *tableEditor) textBand() ui.Rect {
	band := table.textBounds
	height := min(band.Height, table.textStyle.Size*1.4)
	band.Y += (band.Height - height) / 2
	band.Height = height
	return band
}

func (table *tableEditor) paintText(canvas *gfx.Canvas, selectionColor gfx.Color) {
	value := table.input.String()
	style := table.textStyle
	band := table.textBand()
	origin := canvas.LabelOrigin(value, table.textBounds, style, table.textAlignment)
	selection := table.input.Selection()
	marked, composing := table.input.Composition()
	canvas.Clip(table.textBounds)
	defer canvas.Unclip()
	selectionColor.Alpha = 0.25
	for _, span := range canvas.TextSelectionStyle(nil, value, style, byteOffsetOfRune(value, selection.Start), byteOffsetOfRune(value, selection.End)) {
		canvas.Fill(ui.Rect{X: origin + span.X, Y: band.Y, Width: span.Width, Height: band.Height}, selectionColor)
	}
	canvas.LabelAligned(value, table.textBounds, style, table.textAlignment)
	if composing {
		for _, span := range canvas.TextSelectionStyle(nil, value, style, byteOffsetOfRune(value, marked.Start), byteOffsetOfRune(value, marked.End)) {
			canvas.Fill(ui.Rect{X: origin + span.X, Y: band.Y + band.Height - 1, Width: span.Width, Height: 1}, style.Color)
		}
	} else if selection.Start == selection.End && table.caretBlink.Lit(canvas, table.input.Caret()) {
		if caret, ok := table.CaretBounds(); ok {
			canvas.Fill(caret, style.Color)
		}
	}
}

func (state *model) navigateTableCell(backward bool) gfx.EventResult {
	current := state.session.Current()
	if current == nil || !state.table.active || state.table.input == nil || state.tableComposing() {
		return gfx.EventResult{Handled: true}
	}
	index := -1
	for position, cell := range state.table.block.Cells {
		if cell.Row == state.table.row && cell.Column == state.table.column {
			index = position
			break
		}
	}
	if backward {
		index--
	} else {
		index++
	}
	if index < 0 || index >= len(state.table.block.Cells) {
		return gfx.EventResult{Handled: true, Focus: tableInputID}
	}
	cellBase := 0
	for blockIndex := 0; blockIndex < state.table.renderedIndex; blockIndex++ {
		if state.parsed.Blocks[blockIndex].Kind == markdown.Table {
			cellBase += len(state.parsed.Cells(&state.parsed.Blocks[blockIndex]))
		}
	}
	return state.activateTableCell(cellBase + index)
}

func (state *model) activateTableCell(hitIndex int) gfx.EventResult {
	current := state.session.Current()
	if current == nil || state.mode != renderedMode {
		return gfx.EventResult{}
	}
	if focus := state.pendingCompositionFocus(); focus != (gfx.ID{}) {
		return gfx.EventResult{Handled: true, Repaint: true, Focus: focus}
	}
	cellBase, renderedIndex, cellIndex := 0, -1, -1
	for index := range state.parsed.Blocks {
		block := &state.parsed.Blocks[index]
		if block.Kind != markdown.Table {
			continue
		}
		count := len(state.parsed.Cells(block))
		if hitIndex >= cellBase && hitIndex < cellBase+count {
			renderedIndex, cellIndex = index, hitIndex-cellBase
			break
		}
		cellBase += count
	}
	if renderedIndex < 0 {
		return gfx.EventResult{}
	}
	blocks := markdownedit.Parse([]byte(current.Text()))
	rendered := &state.parsed.Blocks[renderedIndex]
	sourceIndex := specialSourceBlockIndex(current.Text(), &state.parsed, renderedIndex, blocks)
	if sourceIndex < 0 || renderedIndex >= len(state.preview.blocks) {
		return state.tableCellSourceFallback(current, rendered.Start)
	}
	block := blocks[sourceIndex]
	cell := block.Cells[cellIndex]
	if (cell.Start != 0 || cell.End != 0 || cell.Editable) && !renderedTableCellSourceMatches(current.Text(), rendered, cell) {
		return state.tableCellSourceFallback(current, rendered.Start)
	}
	var visible string
	for _, span := range state.parsed.CellSpans(state.parsed.Cells(rendered)[cellIndex]) {
		visible += span.Text
	}
	if !cell.Editable || cell.Row != cellIndex/rendered.Columns || cell.Column != cellIndex%rendered.Columns || cell.Visible != visible {
		offset := cell.Start
		if cell.Start == 0 && cell.End == 0 {
			offset = rendered.Start
			for previous := cellIndex - 1; previous >= 0 && block.Cells[previous].Row == cell.Row; previous-- {
				neighbor := block.Cells[previous]
				if neighbor.End > 0 {
					if renderedTableCellSourceMatches(current.Text(), rendered, neighbor) {
						offset = neighbor.End
					}
					break
				}
			}
		}
		return state.tableCellSourceFallback(current, offset)
	}
	state.deactivateRenderedView(current)
	state.codeBlock = codeBlockEditor{}
	state.selection = renderedSelection{}
	state.table = tableEditor{
		active: true, documentID: current.ID, renderedIndex: renderedIndex, sourceIndex: sourceIndex,
		row: cell.Row, column: cell.Column, source: current.Text(), block: block, cell: cell,
		input:       text.NewEditor(cell.Visible),
		columns:     append([]float32(nil), state.preview.blocks[renderedIndex].table.columns...),
		layoutWidth: state.preview.blocks[renderedIndex].width,
	}
	state.table.input.MoveTo(utf8.RuneCountInString(cell.Visible), false)
	return gfx.EventResult{Handled: true, Repaint: true, Focus: tableInputID}
}

func (state *model) tableCellSourceFallback(current *session.Document, offset int) gfx.EventResult {
	state.table = tableEditor{}
	state.mode = editMode
	state.message = "此表格单元格请在 Markdown 源码模式编辑"
	state.focusRequest = sourceID
	offset = max(0, min(offset, len(current.Text())))
	current.Editor.MoveTo(utf8.RuneCountInString(current.Text()[:offset]), false)
	return gfx.EventResult{Handled: true, Repaint: true, Focus: sourceID}
}

func (state *model) onTableCellTextEvent(event app.TextEvent) (app.TextState, error) {
	input := state.table.input
	if input == nil {
		return app.TextState{}, nil
	}
	if event.Kind == app.TextCommandEvent && (event.Command == app.TextUndo || event.Command == app.TextRedo) {
		if state.tableComposing() {
			return app.TextStateOf(input), nil
		}
		state.historyReveal = true
		return state.stepTableSourceHistory(event.Command), nil
	}
	marked, composing := input.Composition()
	if event.Kind == app.TextSetMarked && (!composing || event.HasReplacement && event.Replacement != (app.TextRange{Start: marked.Start, End: marked.End})) {
		selection := input.Selection()
		state.table.compositionBackward = selection.Start != selection.End && input.Caret() == selection.Start
		if event.HasReplacement && event.Replacement != (app.TextRange{Start: selection.Start, End: selection.End}) {
			state.table.compositionBackward = event.Replacement.Start > event.Replacement.End
		}
	}
	event.Apply(input)
	if event.Kind == app.TextUnmark && composing {
		state.syncTableCellChange(state.session.Current(), &marked)
	}
	return app.TextStateOf(input), nil
}

func (state *model) stepTableSourceHistory(command app.TextCommand) app.TextState {
	table := &state.table
	input := table.input
	current := state.session.Current()
	if current == nil || input == nil {
		return app.TextState{}
	}
	if table.documentID != current.ID || table.source != current.Text() {
		return app.TextStateOf(input)
	}
	if result, handled := state.stepInactiveRenderedHistory(command, input); handled {
		return result
	}
	before := table.source
	changed := false
	if command == app.TextUndo {
		changed = current.Editor.Undo()
	} else {
		changed = current.Editor.Redo()
	}
	if !changed {
		return app.TextStateOf(input)
	}
	if state.restoreOtherRenderedHistory(current, before, command) {
		state.table, state.codeBlock = tableEditor{}, codeBlockEditor{}
		return app.TextStateOf(state.rich.input)
	}
	delta := 1
	if command == app.TextUndo {
		delta = -1
	}
	blocks := markdownedit.Parse([]byte(current.Text()))
	if table.sourceIndex >= 0 && table.sourceIndex < len(blocks) && blocks[table.sourceIndex].Kind == markdownedit.Table {
		block := blocks[table.sourceIndex]
		for _, cell := range block.Cells {
			if cell.Row != table.row || cell.Column != table.column || !cell.Editable {
				continue
			}
			next, err := markdownedit.ReplaceCell([]byte(before), table.block, table.row, table.column, cell.Visible)
			if err == nil && (string(next) == current.Text() || tableCellSourceChange(before, current.Text(), table.cell, cell)) {
				state.moveInactiveRenderedHistory(current, before, delta, false)
				start := utf8.RuneCountInString(current.Text()[:cell.Start])
				selection := current.Editor.Selection()
				if current.Editor.Caret() == selection.Start {
					selection.Start, selection.End = selection.End, selection.Start
				}
				limit := utf8.RuneCountInString(cell.Visible)
				table.input = text.NewEditor(cell.Visible)
				table.input.SetSelection(text.Range{
					Start: max(0, min(selection.Start-start, limit)),
					End:   max(0, min(selection.End-start, limit)),
				})
				table.block, table.cell, table.source = block, cell, current.Text()
				return app.TextStateOf(table.input)
			}
			break
		}
	}
	if result, restored := state.restoreSpecialSourceHistory(current, before, command); restored {
		return result
	}
	state.moveInactiveRenderedHistory(current, before, delta, false)
	state.specialHistorySourceFallback()
	return app.TextStateOf(input)
}

func tableCellSourceChange(before, after string, previous, updated markdownedit.Cell) bool {
	if previous.Row != updated.Row || previous.Column != updated.Column {
		return false
	}
	span := func(source string, cell markdownedit.Cell) (int, int, bool) {
		if !cell.Editable || cell.Start < 0 || cell.End < cell.Start || cell.End > len(source) || source[cell.Start:cell.End] != cell.Visible {
			return 0, 0, false
		}
		start, end := cell.Start, cell.End
		for start > 0 && (source[start-1] == ' ' || source[start-1] == '\t') {
			start--
		}
		for end < len(source) && (source[end] == ' ' || source[end] == '\t') {
			end++
		}
		return start, end, true
	}
	beforeStart, beforeEnd, beforeOK := span(before, previous)
	afterStart, afterEnd, afterOK := span(after, updated)
	return beforeOK && afterOK && before[:beforeStart] == after[:afterStart] && before[beforeEnd:] == after[afterEnd:]
}

func (state *model) syncTableCell(current *session.Document) {
	state.syncTableCellChange(current, nil)
}

func (state *model) syncTableCellChange(current *session.Document, composition *text.Range) {
	cell := &state.table
	if !cell.active {
		return
	}
	if current == nil || cell.documentID != current.ID || cell.source != current.Text() || cell.input == nil {
		state.table = tableEditor{}
		return
	}
	defer state.moveInactiveRenderedHistory(current, current.Text(), 1, true)
	if _, composing := cell.input.Composition(); composing || cell.input.String() == cell.cell.Visible {
		return
	}
	replacement := cell.input.String()
	next, err := markdownedit.ReplaceCell([]byte(cell.source), cell.block, cell.row, cell.column, replacement)
	if err != nil {
		if err := current.EditRange(cell.cell.Start, cell.cell.End, replacement); err != nil {
			state.message = err.Error()
			return
		}
		state.tableCellSourceFallback(current, cell.cell.Start+len(replacement))
		return
	}
	if composition == nil || !state.applyCommittedTableComposition(current, *composition, replacement) {
		caret := byteOffsetOfRune(replacement, cell.input.Caret())
		start, end, inserted := renderedInputSourceEdit(cell.cell.Visible, replacement, caret)
		current.Editor.BreakUndoGroup()
		err = current.EditRange(cell.cell.Start+start, cell.cell.Start+end, inserted)
		current.Editor.BreakUndoGroup()
		if err != nil {
			state.message = err.Error()
			return
		}
	}
	if current.Text() != string(next) {
		state.tableCellSourceFallback(current, cell.cell.Start+len(replacement))
		return
	}
	maxStart := cell.cell.Start
	if replacement == "" {
		for offset := cell.cell.End; offset < len(cell.source) && (cell.source[offset] == ' ' || cell.source[offset] == '\t'); offset++ {
			maxStart++
		}
	}
	for sourceIndex, block := range markdownedit.Parse(next) {
		if block.Kind != markdownedit.Table {
			continue
		}
		for _, updated := range block.Cells {
			if updated.Row == cell.row && updated.Column == cell.column && updated.Start >= cell.cell.Start && updated.Start <= maxStart && updated.Visible == replacement {
				cell.block, cell.cell, cell.source = block, updated, current.Text()
				cell.sourceIndex = sourceIndex
				state.message = ""
				return
			}
		}
	}
	state.tableCellSourceFallback(current, cell.cell.Start+len(replacement))
}

func (state *model) applyCommittedTableComposition(current *session.Document, marked text.Range, replacement string) bool {
	cell := &state.table
	selection := cell.input.Selection()
	if current.Composing() || marked.Start < 0 || marked.End < marked.Start || marked.End > utf8.RuneCountInString(replacement) || selection.Start < marked.Start || selection.End > marked.End {
		return false
	}
	from, to := byteOffsetOfRune(replacement, marked.Start), byteOffsetOfRune(replacement, marked.End)
	prefix, suffix := replacement[:from], replacement[to:]
	if len(prefix)+len(suffix) > len(cell.cell.Visible) || !strings.HasPrefix(cell.cell.Visible, prefix) || !strings.HasSuffix(cell.cell.Visible, suffix) {
		return false
	}
	value := current.Text()
	start, end := cell.cell.Start+len(prefix), cell.cell.End-len(suffix)
	current.Editor.BreakUndoGroup()
	target := text.Range{Start: utf8.RuneCountInString(value[:start]), End: utf8.RuneCountInString(value[:end])}
	if cell.compositionBackward {
		target.Start, target.End = target.End, target.Start
	}
	current.Editor.SetSelection(target)
	current.Editor.SetMarkedText(replacement[from:to], text.Range{Start: selection.Start - marked.Start, End: selection.End - marked.Start})
	current.Editor.Unmark()
	return true
}

func (state *model) paintTableCellHits(canvas *gfx.Canvas, palette colors, blockIndex, cellBase int, x, y float32, layout *previewBlock) {
	table := &layout.table
	var offsetY float32
	for row, height := range table.rows {
		var offsetX float32
		for column, width := range table.columns {
			cellIndex := row*len(table.columns) + column
			bounds := ui.Rect{X: x + offsetX, Y: y + layout.top + offsetY, Width: width, Height: height}
			if state.table.active && state.table.renderedIndex == blockIndex && state.table.row == row && state.table.column == column {
				active := &state.table
				color := palette.background
				if row%2 == 0 {
					color = palette.sidebar
				}
				active.textCanvas = canvas
				active.textBounds = ui.Rect{X: bounds.X + 13, Y: bounds.Y + 6, Width: max(10, bounds.Width-26), Height: max(20, bounds.Height-12)}
				active.textAlignment = gfx.AlignLeft
				switch table.cells[cellIndex].align {
				case markdown.AlignCenter:
					active.textAlignment = gfx.AlignCenter
				case markdown.AlignRight:
					active.textAlignment = gfx.AlignRight
				}
				weight := gfx.WeightRegular
				if row == 0 {
					weight = gfx.WeightSemibold
				}
				active.textStyle = gfx.TextStyle{Size: 16, Weight: weight, Color: palette.ink}
				if canvas.Focused() == tableInputID {
					canvas.Fill(bounds.Inset(gfx.All(2)), color)
					active.paintText(canvas, palette.accent)
				}
				composingConfirmation := state.confirmationCompositionFocus() == tableInputID
				if composingConfirmation {
					canvas.PushFocusScope(confirmScope)
				}
				canvas.HitWithOptions(tableInputID, bounds, "表格单元格编辑器", gfx.HitOptions{
					Role: gfx.RoleTextField, Cursor: gfx.PointerCursorText, Value: active.input.String(),
					TextBounds: active.textBounds, TextSize: active.textStyle.Size, TextWeight: weight,
					HideNativeOverlays: true, Placement: active,
				})
				if composingConfirmation {
					canvas.PopFocusScope()
				}
			} else {
				canvas.HitWithOptions(renderedTableCells.At(cellBase+cellIndex), bounds, "编辑表格单元格", gfx.HitOptions{Role: gfx.RoleButton})
			}
			offsetX += width
		}
		offsetY += height
	}
}
