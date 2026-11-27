package main

import (
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"markstack/internal/markdownedit"
	"markstack/internal/session"
	"markstack/internal/workspace"
)

func (state *model) indentRenderedList(outdent bool) gfx.EventResult {
	rich, current := &state.rich, state.session.Current()
	if current == nil || !rich.active || rich.block.Kind != markdownedit.ListItem {
		return gfx.EventResult{}
	}
	result := gfx.EventResult{Handled: true, Repaint: true, Focus: richInputID}
	if state.refuseComposingFormat() || rich.documentID != current.ID || rich.source != current.Text() || rich.input.String() != rich.block.Visible {
		return result
	}
	prior := *rich
	block := prior.block
	if prior.virtual {
		for _, candidate := range markdownedit.Parse([]byte(prior.source)) {
			if candidate.Kind == markdownedit.ListItem && candidate.Start == prior.virtualStart && candidate.Visible == prior.input.String() {
				block = candidate
				break
			}
		}
	}
	patch, position, err := markdownedit.PatchListIndent([]byte(prior.source), block, outdent)
	if err != nil {
		return result
	}
	value := prior.source[:patch.Start] + patch.Text + prior.source[patch.End:]
	selection := directedCodeSelection(prior.input)
	before := shiftedSelection(selection, utf8.RuneCountInString(prior.source[:block.Start]))
	after := shiftedSelection(selection, utf8.RuneCountInString(value[:position]))
	if err := applyMarkdownPatch(current, patch, before, after); err != nil {
		state.message = err.Error()
		return result
	}
	state.parseEditableMarkdown(value)
	blocks := markdownedit.Parse([]byte(value))
	normalizeRenderedTasks(&state.parsed, blocks)
	for index, mapped := range renderedMapping(&state.parsed, blocks) {
		if mapped >= 0 && blocks[mapped].Kind == markdownedit.ListItem && blocks[mapped].Start == position {
			result = state.activateRendered(index)
			state.rich.input.SetSelection(selection)
			state.rememberRenderedStructure(prior.renderedHistory, prior.appliedEdits+prior.sourceHistoryOffset, prior.source, prior.index, prior.input.Caret(), prior.virtualState())
			state.historyReveal = true
			state.message = ""
			return result
		}
	}
	state.deactivateRenderedView(current)
	state.mode = editMode
	return gfx.EventResult{Handled: true, Repaint: true, Focus: sourceID}
}

func shiftedSelection(selection text.Range, offset int) text.Range {
	return text.Range{Start: selection.Start + offset, End: selection.End + offset}
}

func (state *model) exitRenderedEmptyList(current *session.Document) gfx.EventResult {
	result := gfx.EventResult{Handled: true, Repaint: true, Focus: richInputID}
	prior := state.rich
	if current == nil || prior.source != current.Text() || prior.documentID != current.ID || state.refuseComposingFormat() {
		return result
	}
	patch, err := markdownedit.ExitEmptyList([]byte(prior.source), prior.block)
	if err != nil {
		return result
	}
	before := utf8.RuneCountInString(prior.source[:prior.block.Start])
	after := utf8.RuneCountInString(prior.source[:patch.Start] + patch.Text)
	if err := applyMarkdownPatch(current, patch, text.Range{Start: before, End: before}, text.Range{Start: after, End: after}); err != nil {
		state.message = err.Error()
		return result
	}
	state.rich = renderedEditor{}
	result = state.focusMarkdownGap(current, patch.Start+len(patch.Text))
	if state.rich.active {
		state.rememberRenderedStructure(prior.renderedHistory, prior.appliedEdits+prior.sourceHistoryOffset, prior.source, prior.index, 0, prior.virtualState())
	}
	return result
}

func (state *model) paintTaskMarkers(canvas *gfx.Canvas, palette colors, clip ui.Rect, x, y float32) {
	for _, task := range state.preview.taskMarkers {
		layout := state.preview.blocks[task.index]
		indent := float32(state.parsed.Blocks[task.index].Level) * 20
		marker := ui.Rect{X: x + indent, Y: y + layout.top, Width: 24, Height: 26}
		if marker.Y+marker.Height <= clip.Y || marker.Y >= clip.Y+clip.Height {
			continue
		}
		canvas.Fill(marker, palette.background)
		glyph := "☐"
		if task.checked {
			glyph = "☑"
		}
		canvas.LabelCenter(glyph, marker, gfx.TextStyle{Size: 17, Color: palette.ink})
		canvas.HitWithOptions(taskChecks.At(task.index), marker, task.label, gfx.HitOptions{Role: gfx.RoleCheckbox, Checked: task.checked})
	}
}

func (state *model) toggleTaskAt(index int) gfx.EventResult {
	current := state.session.Current()
	if current == nil || current.Kind != workspace.MarkdownKind || state.mode != renderedMode && state.mode != readMode {
		return gfx.EventResult{}
	}
	if focus := state.pendingFormatCompositionFocus(); !focus.IsZero() {
		return gfx.EventResult{Handled: true, Focus: focus}
	}
	source := current.Text()
	result := gfx.EventResult{Handled: true, Repaint: true, Focus: taskChecks.At(index)}
	var block markdownedit.Block
	renderedTasks(&state.parsed, markdownedit.Parse([]byte(source)), func(rendered int, candidate markdownedit.Block) {
		if rendered == index {
			block = candidate
		}
	})
	if !block.Task {
		return gfx.EventResult{}
	}
	next, err := markdownedit.ToggleTask([]byte(source), block)
	if err != nil {
		state.message = err.Error()
		return result
	}
	var selection text.Range
	keepSelection := state.rich.active && !state.rich.virtual && state.rich.index == index && state.rich.source == source
	if keepSelection {
		selection = directedCodeSelection(state.rich.input)
	}
	caret := utf8.RuneCountInString(source[:block.Start])
	target := shiftedSelection(selection, caret)
	position := block.TaskStart + 1
	patch := markdownedit.SourcePatch{Start: position, End: position + 1, Text: string(next[position : position+1])}
	if err := applyMarkdownPatch(current, patch, target, target); err != nil {
		state.message = err.Error()
		return result
	}
	state.message = ""
	if keepSelection {
		state.rich.source = current.Text()
		state.rich.block = markdownedit.Parse(next)[state.rich.sourceIndex]
		state.resetRenderedFormatHistory(source)
		result.Focus = richInputID
	} else {
		state.table, state.codeBlock = tableEditor{}, codeBlockEditor{}
		state.selection = renderedSelection{}
		history, offset := state.rich.renderedHistory, state.rich.appliedEdits+state.rich.sourceHistoryOffset
		if state.rich.documentID != current.ID || state.rich.source != source {
			history, offset = renderedHistory{}, 0
		}
		state.rich = renderedEditor{documentID: current.ID, source: current.Text(), renderedHistory: history, sourceHistoryOffset: offset + 1}
		state.rich.crossRedo, state.rich.structureRedo = nil, nil
		state.rich.trimHistory(current)
	}
	state.focusRequest = result.Focus
	return result
}
