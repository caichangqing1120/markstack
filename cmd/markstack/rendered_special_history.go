package main

import (
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/text"

	"markstack/internal/markdownedit"
	"markstack/internal/session"
)

func (state *model) moveInactiveRenderedHistory(current *session.Document, before string, delta int, newBranch bool) {
	if current == nil || newBranch && before == current.Text() {
		return
	}
	rich := &state.rich
	if rich.active || rich.input != nil || rich.documentID != current.ID || rich.source != before {
		state.rich = renderedEditor{}
		return
	}
	rich.source = current.Text()
	rich.sourceHistoryOffset += delta
	if newBranch {
		rich.crossRedo, rich.structureRedo = nil, nil
	}
	rich.trimHistory(current)
}

func (state *model) stepInactiveRenderedHistory(command app.TextCommand, input *text.Editor) (app.TextState, bool) {
	current := state.session.Current()
	if current == nil || state.rich.active || state.rich.input != nil || state.rich.documentID != current.ID || state.rich.source != current.Text() {
		return app.TextState{}, false
	}
	result, handled := state.stepCrossHistory(command == app.TextUndo, false)
	if !handled {
		result, handled = state.stepStructureHistory(command == app.TextUndo)
	}
	if handled {
		if state.rich.active {
			state.table, state.codeBlock = tableEditor{}, codeBlockEditor{}
		} else if state.focusRequest != codeInputID && state.focusRequest != tableInputID {
			result = app.TextStateOf(input)
		}
	}
	return result, handled
}

func (state *model) specialHistorySourceFallback() {
	state.table, state.codeBlock = tableEditor{}, codeBlockEditor{}
	state.mode = editMode
	state.message = "历史操作已切换到 Markdown 源码模式"
	state.focusRequest = sourceID
}

func renderedInputSourceEdit(before, after string, caret int) (int, int, string) {
	if caret < 0 || caret > len(after) || !utf8.ValidString(after[:caret]) || !strings.HasSuffix(before, after[caret:]) {
		return 0, len(before), after
	}
	end := len(before) - len(after[caret:])
	start := 0
	for start < end && start < caret {
		original, originalSize := utf8.DecodeRuneInString(before[start:end])
		replacement, replacementSize := utf8.DecodeRuneInString(after[start:caret])
		if original != replacement || originalSize != replacementSize {
			break
		}
		start += originalSize
	}
	return start, end, after[start:caret]
}

func codeSourceHistorySelection(current *session.Document, block markdownedit.Block) (text.Range, bool) {
	source := current.Text()
	selection := current.Editor.Selection()
	start, end := byteOffsetOfRune(source, selection.Start), byteOffsetOfRune(source, selection.End)
	if !block.Code.Editable || start < block.Code.Start || end > block.Code.End || block.Code.End > len(source) {
		return text.Range{}, false
	}
	limit := utf8.RuneCountInString(block.Code.Visible)
	visible := text.Range{
		Start: min(limit, utf8.RuneCountInString(strings.ReplaceAll(source[block.Code.Start:start], "\r\n", "\n"))),
		End:   min(limit, utf8.RuneCountInString(strings.ReplaceAll(source[block.Code.Start:end], "\r\n", "\n"))),
	}
	if current.Editor.Caret() == selection.Start {
		visible.Start, visible.End = visible.End, visible.Start
	}
	return visible, true
}

func codeBlockSourceChange(before, after string, previous, updated markdownedit.Block) bool {
	return previous.Kind == markdownedit.FencedCode && updated.Kind == markdownedit.FencedCode && previous.Code.Editable && updated.Code.Editable &&
		previous.Code.Info == updated.Code.Info && previous.Code.Start == updated.Code.Start && previous.Code.Start >= 0 &&
		previous.Code.End >= previous.Code.Start && previous.Code.End <= len(before) && updated.Code.End >= updated.Code.Start && updated.Code.End <= len(after) &&
		before[:previous.Code.Start] == after[:updated.Code.Start] && before[previous.Code.End:] == after[updated.Code.End:]
}

func fenceLanguageSourceChange(before, after string, previous, updated markdownedit.Block) bool {
	if updated.Kind != markdownedit.FencedCode || !updated.Code.Editable || previous.Code.Info == updated.Code.Info {
		return false
	}
	patch, err := markdownedit.PatchFenceLanguage([]byte(before), previous, markdownedit.FenceLanguage(updated.Code.Info))
	if err == nil && before[:patch.Start]+patch.Text+before[patch.End:] == after {
		return true
	}
	patch, err = markdownedit.PatchFenceLanguage([]byte(after), updated, markdownedit.FenceLanguage(previous.Code.Info))
	return err == nil && after[:patch.Start]+patch.Text+after[patch.End:] == before
}

func (state *model) restoreSpecialSourceHistory(current *session.Document, before string, command app.TextCommand) (app.TextState, bool) {
	if current == nil || state.rich.documentID != current.ID || state.rich.source != before || current.Text() == before {
		return app.TextState{}, false
	}
	source := current.Text()
	prior, blocks := markdownedit.Parse([]byte(before)), markdownedit.Parse([]byte(source))
	selection := current.Editor.Selection()
	start, end := byteOffsetOfRune(source, selection.Start), byteOffsetOfRune(source, selection.End)
	for index, block := range blocks {
		var previous markdownedit.Block
		if index < len(prior) {
			previous = prior[index]
		}
		if block.Kind == markdownedit.FencedCode {
			if start < block.Code.Start || end > block.Code.End || !codeBlockSourceChange(before, source, previous, block) && !fenceLanguageSourceChange(before, source, previous, block) {
				continue
			}
			visible, valid := codeSourceHistorySelection(current, block)
			if valid {
				return state.restoreSpecialHistoryView(current, command, block, -1, visible)
			}
		} else if block.Kind == markdownedit.Table && start >= block.Start && end <= block.End {
			structure := previous.Kind == markdownedit.Table && previous.Start == block.Start &&
				before[:previous.Start] == source[:block.Start] && before[previous.End:] == source[block.End:]
			if !structure && markdownedit.CanEditTable([]byte(source), block) {
				patch, err := markdownedit.PatchTable([]byte(source), block, 0, 0, markdownedit.TableDelete)
				structure = err == nil && source[:patch.Start]+source[patch.End:] == before
			}
			for cellIndex, cell := range block.Cells {
				if !cell.Editable || start < cell.Start || end > cell.End || !structure && (len(previous.Cells) != len(block.Cells) || !tableCellSourceChange(before, source, previous.Cells[cellIndex], cell)) {
					continue
				}
				visible := text.Range{Start: utf8.RuneCountInString(source[cell.Start:start]), End: utf8.RuneCountInString(source[cell.Start:end])}
				if current.Editor.Caret() == selection.Start {
					visible.Start, visible.End = visible.End, visible.Start
				}
				return state.restoreSpecialHistoryView(current, command, block, cellIndex, visible)
			}
		}
	}
	return app.TextState{}, false
}

func (state *model) restoreSpecialHistoryView(current *session.Document, command app.TextCommand, block markdownedit.Block, cell int, selection text.Range) (app.TextState, bool) {
	history, position := state.rich.renderedHistory, state.rich.appliedEdits+state.rich.sourceHistoryOffset
	sourceSelection := current.Editor.Selection()
	if current.Editor.Caret() == sourceSelection.Start {
		sourceSelection.Start, sourceSelection.End = sourceSelection.End, sourceSelection.Start
	}
	state.parseEditableMarkdown(current.Text())
	index := -1
	for renderedIndex := range state.parsed.Blocks {
		if specialBlockMatches(current.Text(), &state.parsed, renderedIndex, block) {
			if index >= 0 {
				return app.TextState{}, false
			}
			index = renderedIndex
		}
	}
	if index < 0 {
		return app.TextState{}, false
	}
	input := (*text.Editor)(nil)
	if cell < 0 {
		if state.parsed.Blocks[index].Kind != markdown.Code {
			return app.TextState{}, false
		}
		result := state.activateCodeBlock(index)
		if !state.codeBlock.active || state.codeBlock.input == nil {
			current.Editor.SetSelection(sourceSelection)
			return app.TextState{}, false
		}
		input, state.focusRequest = state.codeBlock.input, result.Focus
	} else {
		if state.parsed.Blocks[index].Kind != markdown.Table || cell >= len(state.parsed.Cells(&state.parsed.Blocks[index])) {
			return app.TextState{}, false
		}
		hit := cell
		for position := 0; position < index; position++ {
			if state.parsed.Blocks[position].Kind == markdown.Table {
				hit += len(state.parsed.Cells(&state.parsed.Blocks[position]))
			}
		}
		result := state.activateTableCell(hit)
		if !state.table.active || state.table.input == nil {
			current.Editor.SetSelection(sourceSelection)
			return app.TextState{}, false
		}
		input, state.focusRequest = state.table.input, result.Focus
	}
	input.SetSelection(selection)
	if command == app.TextUndo {
		position--
	} else {
		position++
	}
	state.rich = renderedEditor{documentID: current.ID, source: current.Text(), renderedHistory: history, sourceHistoryOffset: position}
	state.rich.trimHistory(current)
	return app.TextStateOf(input), true
}
