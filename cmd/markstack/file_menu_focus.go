package main

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"

	"markstack/internal/session"
)

type fileMenuEditingFocus struct {
	document *session.Document
	mode     viewMode
	target   gfx.ID
	editor   *text.Editor
	source   *text.Editor
}

func (state *model) fileMenuEditor(target gfx.ID) *text.Editor {
	current := state.session.Current()
	if current == nil {
		return nil
	}
	if target == sourceID && (state.mode == editMode || state.mode == splitMode) {
		return current.Editor
	}
	if state.mode != renderedMode {
		return nil
	}
	switch target {
	case richInputID:
		if state.rich.active && state.rich.documentID == current.ID {
			return state.rich.input
		}
	case codeInputID:
		if state.codeBlock.active && state.codeBlock.documentID == current.ID {
			return state.codeBlock.input
		}
	case tableInputID:
		if state.table.active && state.table.documentID == current.ID {
			return state.table.input
		}
	case htmlInputID:
		if state.html.active && state.html.documentID == current.ID {
			return state.html.input
		}
	}
	return nil
}

func (state *model) rememberFileMenuEditingFocus(target gfx.ID) {
	if state.fileMenuOpen || keepsEditingFocus(target) || !state.focusRequest.IsZero() {
		return
	}
	state.fileMenuReturnFocus = fileMenuEditingFocus{}
	if editor := state.fileMenuEditor(target); editor != nil {
		state.fileMenuReturnFocus = fileMenuEditingFocus{state.session.Current(), state.mode, target, editor, state.session.Current().Editor}
	}
}

func keepsEditingFocus(target gfx.ID) bool {
	switch target {
	case fileMenuID, fileMenuBackdropID, topExportMenuID, topSaveID, topSaveAsID, topAutoSaveID, topFontDecreaseID, topFontIncreaseID, topFontResetID:
		return true
	}
	return false
}

func (state *model) saveFromFileMenu(choosePath bool) {
	state.runFileMenuEditingAction(func() { state.saveActive(choosePath) })
}

func (state *model) runFileMenuEditingAction(action func()) {
	previous := state.fileMenuReturnFocus
	action()
	if previous.editor != nil && state.session.Current() == previous.document && state.session.Current().Editor == previous.source && state.mode == previous.mode && state.fileMenuEditor(previous.target) == previous.editor {
		state.focusRequest = previous.target
		state.invalidator.Invalidate()
	}
}
