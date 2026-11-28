package main

import (
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/ui"

	"markstack/internal/workspace"
)

func (state *model) onRenderedDocumentHistoryShortcut(event gfx.KeyEvent) gfx.EventResult {
	primary := primaryModifier()
	command := app.TextUndo
	switch strings.ToLower(event.Text) {
	case "z":
		if event.Modifiers == primary|gfx.ModifierShift {
			command = app.TextRedo
		} else if event.Modifiers != primary {
			return gfx.EventResult{}
		}
	case "y":
		if event.Modifiers != primary {
			return gfx.EventResult{}
		}
		command = app.TextRedo
	default:
		return gfx.EventResult{}
	}
	current := state.session.Current()
	if current == nil || current.Kind != workspace.MarkdownKind || state.mode != renderedMode && state.mode != readMode || state.linkOpen || state.fileAction != fileActionNone {
		return gfx.EventResult{}
	}
	switch event.Target {
	case sourceID, richInputID, tableInputID, codeInputID, htmlInputID, searchID, workspaceQueryID, treeNameID, findInputID, replaceInputID, linkInputID, codeLanguageInputID:
		return gfx.EventResult{}
	}
	if focus := state.pendingCompositionFocus(); focus != (gfx.ID{}) {
		return gfx.EventResult{Handled: true, Focus: focus}
	}
	if state.mode == readMode {
		before := current.Text()
		if command == app.TextUndo {
			if state.stepMarkdownEdit(current, true) {
				state.moveInactiveRenderedHistory(current, before, -1, false)
			}
		} else if state.stepMarkdownEdit(current, false) {
			state.moveInactiveRenderedHistory(current, before, 1, false)
		}
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if state.html.input != nil {
		return gfx.EventResult{}
	}
	state.fileMenuOpen = false
	state.syncRendered(current)
	state.syncCodeBlock(current)
	state.syncTableCell(current)
	eventCommand := app.TextEvent{Kind: app.TextCommandEvent, Command: command}
	var err error
	switch {
	case state.rich.active && state.rich.input != nil && state.rich.documentID == current.ID:
		_, err = state.onRenderedTextEvent(eventCommand)
	case state.table.active && state.table.input != nil && state.table.documentID == current.ID:
		_, err = state.onTableCellTextEvent(eventCommand)
	case state.codeBlock.active && state.codeBlock.input != nil && state.codeBlock.documentID == current.ID:
		_, err = state.onCodeBlockTextEvent(eventCommand)
	case state.rich.input != nil || state.table.input != nil || state.codeBlock.input != nil:
		return gfx.EventResult{}
	default:
		if command == app.TextUndo && !current.Editor.CanUndo() || command == app.TextRedo && !current.Editor.CanRedo() {
			return gfx.EventResult{Handled: true}
		}
		state.deactivateRenderedView(current)
		if _, handled := state.stepCrossHistory(command == app.TextUndo, false); !handled {
			if _, handled := state.stepStructureHistory(command == app.TextUndo); !handled {
				state.stepSourceHistory(command, false)
			}
		}
	}
	if err != nil {
		state.message = err.Error()
	}
	focus := gfx.ID{}
	switch {
	case state.mode == editMode:
		focus = sourceID
	case state.rich.active:
		focus = richInputID
	case state.table.active:
		focus = tableInputID
	case state.codeBlock.active:
		focus = codeInputID
	}
	state.focusRequest = focus
	state.historyReveal = focus == richInputID || focus == tableInputID || focus == codeInputID
	return gfx.EventResult{Handled: true, Repaint: true, Focus: focus}
}

func (state *model) revealRenderedHistoryTarget(canvas *gfx.Canvas, theme ui.Theme, bounds ui.Rect) {
	if !state.historyReveal {
		return
	}
	state.historyReveal = false
	current := state.session.Current()
	if current == nil || state.mode != renderedMode || bounds.Height <= 0 {
		return
	}
	index := -1
	switch {
	case state.rich.active && state.rich.documentID == current.ID && state.rich.source == current.Text():
		index = state.rich.index
	case state.table.active && state.table.documentID == current.ID && state.table.source == current.Text():
		index = state.table.renderedIndex
	case state.codeBlock.active && state.codeBlock.documentID == current.ID && state.codeBlock.source == current.Text():
		index = state.codeBlock.renderedIndex
	}
	if index < 0 || index >= len(state.preview.blocks) && !(state.rich.active && state.virtualParagraphNeedsPlaceholder()) {
		return
	}
	top, height := float32(0), float32(26)
	if index < len(state.preview.blocks) {
		top = state.preview.blocks[index].top
	}
	var measure gfx.Canvas
	measure.SetTextEngine(&previewMeasurements{canvas: canvas})
	switch {
	case state.rich.active:
		if state.virtualParagraphNeedsPlaceholder() {
			top = state.virtualParagraphTop()
		}
		offset := byteOffsetOfRune(state.rich.input.String(), state.rich.input.Caret())
		if state.activeMixedSpan() {
			view := state.preview.blocks[index].view
			view.Paint(&measure, components.MarkdownPaint{})
			if caret, ok := view.CaretAt(offset); ok {
				top, height = top+caret.Y, caret.Height
			}
		} else {
			if index < len(state.parsed.Blocks) {
				if block := state.parsed.Blocks[index]; block.Kind == markdown.Heading {
					_, height, _ = markdownHeadingMetrics(block.Level, state.dark)
				}
			}
			top += float32(state.rich.caretRow(offset, false)) * height
		}
	case state.table.active:
		rows := state.preview.blocks[index].table.rows
		if state.table.row < 0 || state.table.row >= len(rows) {
			return
		}
		for _, rowHeight := range rows[:state.table.row] {
			top += rowHeight
		}
		height = rows[state.table.row]
	case state.codeBlock.active:
		_, width := state.markdownColumn(bounds)
		var view components.CodeEditor
		view.Paint(&measure, theme, ui.Rect{Y: codeHeaderHeight, Width: width, Height: state.preview.blocks[index].height - codeHeaderHeight}, components.CodeEditorSnapshot{
			Value: state.codeBlock.input.String(), Caret: byteOffsetOfRune(state.codeBlock.input.String(), state.codeBlock.input.Caret()),
			Focused: true, ShowCaret: true, Wrap: true, FontSize: state.readingSize() - 1, MonoFace: markdownCodeFace,
		})
		if caret, ok := view.CaretBounds(); ok {
			top, height = top+caret.Y, caret.Height
		}
	}
	top += 46
	if top-state.previewScroll < 8 {
		state.previewScroll = max(0, top-8)
	} else if top+height-state.previewScroll > bounds.Height-12 {
		state.previewScroll = max(0, top+height-bounds.Height+12)
	}
}
