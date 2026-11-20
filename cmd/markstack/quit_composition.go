package main

import (
	"github.com/vivaui/viva/text"

	"markstack/internal/session"
)

func (state *model) requestSessionQuit() bool {
	projected := make(map[*session.Document][]*text.Editor)
	for _, owner := range []struct {
		active     bool
		documentID int
		input      *text.Editor
	}{
		{state.rich.active, state.rich.documentID, state.rich.input},
		{state.codeBlock.active, state.codeBlock.documentID, state.codeBlock.input},
		{state.table.active, state.table.documentID, state.table.input},
		{state.html.active, state.html.documentID, state.html.input},
	} {
		if !owner.active || owner.input == nil {
			continue
		}
		if _, composing := owner.input.Composition(); !composing {
			continue
		}
		for _, document := range state.session.Tabs {
			if document.ID == owner.documentID {
				projected[document] = append(projected[document], owner.input)
				break
			}
		}
	}
	if len(projected) == 0 {
		return state.session.RequestQuit()
	}
	return state.session.RequestQuitWithPending(func(document *session.Document) bool {
		for _, input := range projected[document] {
			if _, composing := input.Composition(); composing {
				return true
			}
		}
		return false
	})
}
