package main

import (
	"crypto/sha256"
	"weak"

	"github.com/vivaui/viva/text"

	"markstack/internal/workspace"
)

type renderedDocumentHistory struct {
	renderedHistory
	editor      weak.Pointer[text.Editor]
	source      [32]byte
	position    int
	emptyTarget *renderedEmptyTarget
}

type renderedDocumentHistories struct {
	entries    map[int]renderedDocumentHistory
	documentID int
	editor     weak.Pointer[text.Editor]
	tabCount   int
}

func (state *model) storeRenderedDocumentHistory() {
	cache := &state.renderedDocuments
	delete(cache.entries, cache.documentID)
	owner := cache.editor.Value()
	if cache.documentID == 0 || owner == nil {
		return
	}
	for _, document := range state.session.Tabs {
		if document.ID != cache.documentID || document.Editor != owner {
			continue
		}
		if document.Kind != workspace.MarkdownKind || document.Composing() || !owner.CanUndo() && !owner.CanRedo() {
			return
		}
		source := owner.CommittedString()
		owned := state.rich.documentID == document.ID && state.rich.source == source ||
			state.selection.active && state.selection.documentID == document.ID && state.selection.source == source
		if !owned {
			return
		}
		history, position := state.renderedHistorySnapshot(document)
		if cache.entries == nil {
			cache.entries = make(map[int]renderedDocumentHistory)
		}
		cache.entries[document.ID] = renderedDocumentHistory{
			renderedHistory: history, position: position,
			editor: weak.Make(owner), source: sha256.Sum256([]byte(source)),
			emptyTarget: state.rich.emptyTargetSnapshot(document),
		}
		return
	}
}

func (state *model) pruneRenderedDocumentHistories() {
	for documentID, history := range state.renderedDocuments.entries {
		live := false
		for _, document := range state.session.Tabs {
			if document.ID == documentID && document.Kind == workspace.MarkdownKind && document.Editor == history.editor.Value() {
				live = true
				break
			}
		}
		if !live {
			delete(state.renderedDocuments.entries, documentID)
		}
	}
	state.renderedDocuments.tabCount = len(state.session.Tabs)
}

func (state *model) restoreRenderedDocumentHistory() {
	state.pruneRenderedDocumentHistories()
	cache := &state.renderedDocuments
	cache.documentID, cache.editor = 0, weak.Pointer[text.Editor]{}
	current := state.session.Current()
	if current == nil {
		return
	}
	cache.documentID, cache.editor = current.ID, weak.Make(current.Editor)
	history, exists := cache.entries[current.ID]
	delete(cache.entries, current.ID)
	if !exists || current.Kind != workspace.MarkdownKind || current.Composing() || history.editor.Value() != current.Editor {
		return
	}
	source := current.Editor.CommittedString()
	if history.source != sha256.Sum256([]byte(source)) {
		return
	}
	state.rich = renderedEditor{
		documentID: current.ID, source: source,
		renderedHistory: history.renderedHistory, sourceHistoryOffset: history.position,
		emptyTarget: history.emptyTarget,
	}
	state.rich.trimHistory(current)
}

func (state *model) syncRenderedDocumentHistory() {
	current := state.session.Current()
	cache := &state.renderedDocuments
	if current == nil {
		if cache.documentID != 0 {
			state.resetDocumentView()
		}
		return
	}
	if cache.documentID == 0 {
		state.restoreRenderedDocumentHistory()
	} else if cache.documentID != current.ID || cache.editor.Value() != current.Editor {
		state.resetDocumentView()
	} else if cache.tabCount != len(state.session.Tabs) {
		state.pruneRenderedDocumentHistories()
	}
}
