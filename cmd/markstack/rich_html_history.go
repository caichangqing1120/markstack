package main

import (
	"crypto/sha256"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"

	"markstack/internal/richedit"
	"markstack/internal/session"
)

// htmlLeafEdit records the view coordinates of one committed IME composition
// so that the authoritative source editor's Undo and Redo can also restore the
// leaf and the oriented visible selection. It stores digests and coordinates
// only: no source copy is retained per commit, and ordinary typing creates no
// entry.
type htmlLeafEdit struct {
	documentID                        int
	owner                             *text.Editor
	beforeSourceHash, afterSourceHash [32]byte
	beforeBlock, beforeLeaf           int
	afterBlock, afterLeaf             int
	beforeSelection, afterSelection   text.Range
	serial                            int
}

// orientedHTMLSelection returns the selection with the caret end last, so that
// SetSelection restores both the range and the direction the user selected.
func orientedHTMLSelection(input *text.Editor) text.Range {
	if input == nil {
		return text.Range{}
	}
	selection := input.Selection()
	if input.Caret() == selection.Start && selection.Start != selection.End {
		selection.Start, selection.End = selection.End, selection.Start
	}
	return selection
}

func (view *richHTMLView) rememberHTMLComposition(current *session.Document, beforeSource string, beforeSelection text.Range, beforeBlock, beforeLeaf int) {
	if current == nil || view.input == nil {
		return
	}
	view.leafUndo = appendRenderedHistory(view.leafUndo, htmlLeafEdit{
		documentID:       current.ID,
		owner:            current.Editor,
		beforeSourceHash: sha256.Sum256([]byte(beforeSource)),
		afterSourceHash:  sha256.Sum256([]byte(current.Text())),
		beforeBlock:      beforeBlock,
		beforeLeaf:       beforeLeaf,
		afterBlock:       view.blockIndex,
		afterLeaf:        view.leafIndex,
		beforeSelection:  beforeSelection,
		afterSelection:   orientedHTMLSelection(view.input),
		serial:           view.leafSerial,
	})
	view.leafRedo = nil
	view.trimHTMLLeafHistory(current)
}

func (view *richHTMLView) trimHTMLLeafHistory(current *session.Document) {
	if current == nil ||
		len(view.leafUndo) > 0 && view.leafUndo[len(view.leafUndo)-1].owner != current.Editor ||
		len(view.leafRedo) > 0 && view.leafRedo[len(view.leafRedo)-1].owner != current.Editor {
		view.leafUndo, view.leafRedo = nil, nil
		view.leafSerial = 0
		return
	}
	if !current.Editor.CanUndo() {
		view.leafUndo = nil
	}
	if !current.Editor.CanRedo() {
		view.leafRedo = nil
	}
}

// anchorHTMLLeafSerial re-anchors the logical edit counter when the source
// editor has stepped back onto the newest recorded composition's result. One
// native Undo may consume several view edits, so the counter cannot be trusted
// to land exactly; the matching digest proves the position instead.
func (view *richHTMLView) anchorHTMLLeafSerial(current *session.Document) {
	if current == nil || len(view.leafUndo) == 0 {
		return
	}
	top := view.leafUndo[len(view.leafUndo)-1]
	if sha256.Sum256([]byte(current.Text())) == top.afterSourceHash {
		view.leafSerial = top.serial
	}
}

func (view *richHTMLView) anchorHTMLLeafRedoSerial(current *session.Document) {
	if current == nil || len(view.leafRedo) == 0 {
		return
	}
	top := view.leafRedo[len(view.leafRedo)-1]
	if sha256.Sum256([]byte(current.Text())) == top.beforeSourceHash {
		view.leafSerial = top.serial - 1
	}
}

// refreshHTMLSource reflects the authoritative source editor after a history
// step that did not belong to a recorded composition. The active leaf is kept;
// only a changed visible value is replaced with the projection's text.
func (view *richHTMLView) refreshHTMLSource(current *session.Document) {
	view.source = current.Text()
	view.parsed = richedit.Parse([]byte(view.source))
	view.workspaceMatch = nil
	view.scrollPending = false
	view.refreshLeaf()
	if view.active && view.input != nil && view.input.String() != view.leaf.Visible {
		view.input = text.NewEditor(view.leaf.Visible)
		view.input.MoveTo(view.input.RuneCount(), false)
	}
}

// stepHTMLLeafHistory advances the source editor only when its next unit
// exactly crosses a recorded composition's source digests. A unit that turns
// out to be something else is kept as the authoritative step and the metadata
// is left for a later match. This lets ordinary edits between a composition and
// its Undo step first, without double-stepping the active input history.
func (state *model) stepHTMLLeafHistory(undo bool) (app.TextState, bool) {
	view := &state.html
	current := state.session.Current()
	if current == nil || view.input == nil || !view.active {
		return app.TextState{}, false
	}
	view.trimHTMLLeafHistory(current)
	stack := view.leafUndo
	if !undo {
		stack = view.leafRedo
	}
	if len(stack) == 0 {
		return app.TextState{}, false
	}
	edit := stack[len(stack)-1]
	if current.ID != view.documentID || current.ID != edit.documentID || view.source != current.Text() {
		return app.TextState{}, false
	}
	// The source editor may hold later units between this composition and its
	// next step. Only the exact logical position may claim the metadata, so a
	// later edit that happens to restore the same bytes cannot revive it.
	if undo && view.leafSerial != edit.serial || !undo && view.leafSerial != edit.serial-1 {
		return app.TextState{}, false
	}
	haveSource, wantSource := edit.afterSourceHash, edit.beforeSourceHash
	blockIndex, leafIndex, selection := edit.beforeBlock, edit.beforeLeaf, edit.beforeSelection
	if !undo {
		haveSource, wantSource = edit.beforeSourceHash, edit.afterSourceHash
		blockIndex, leafIndex, selection = edit.afterBlock, edit.afterLeaf, edit.afterSelection
	}
	if sha256.Sum256([]byte(current.Text())) != haveSource {
		return app.TextState{}, false
	}
	changed := false
	if undo {
		changed = current.Editor.Undo()
	} else {
		changed = current.Editor.Redo()
	}
	if !changed {
		return app.TextState{}, false
	}
	if sha256.Sum256([]byte(current.Text())) != wantSource {
		// The next unit was not the recorded composition. Keep that legitimate
		// source step, but do not consume or invent view metadata for it.
		if undo {
			if view.leafSerial > 0 {
				view.leafSerial--
			}
			view.anchorHTMLLeafSerial(current)
		} else {
			view.leafSerial++
			view.anchorHTMLLeafRedoSerial(current)
		}
		view.refreshHTMLSource(current)
		view.trimHTMLLeafHistory(current)
		return app.TextStateOf(view.input), true
	}
	view.source = current.Text()
	view.parsed = richedit.Parse([]byte(view.source))
	view.workspaceMatch = nil
	view.scrollPending = false
	view.crossSelection = nil
	view.blockedSelection = false
	if blockIndex < 0 || blockIndex >= len(view.parsed.Blocks) ||
		leafIndex < 0 || leafIndex >= len(view.parsed.Blocks[blockIndex].Leaves) ||
		!view.parsed.Blocks[blockIndex].Leaves[leafIndex].Editable {
		view.active = false
	} else {
		leaf := view.parsed.Blocks[blockIndex].Leaves[leafIndex]
		if view.input == nil || view.blockIndex != blockIndex || view.leafIndex != leafIndex || view.input.String() != leaf.Visible {
			// A different leaf, or a value the current input no longer shows,
			// needs its own editor. Staying in the same leaf keeps the input's
			// native history instead of replaying it through the metadata.
			view.input = text.NewEditor(leaf.Visible)
		}
		view.blockIndex, view.leafIndex = blockIndex, leafIndex
		view.leaf = leaf
		view.input.SetSelection(selection)
		view.active = true
	}
	if undo {
		view.leafUndo = view.leafUndo[:len(view.leafUndo)-1]
		view.leafRedo = appendRenderedHistory(view.leafRedo, edit)
		view.leafSerial = edit.serial - 1
	} else {
		view.leafRedo = view.leafRedo[:len(view.leafRedo)-1]
		view.leafUndo = appendRenderedHistory(view.leafUndo, edit)
		view.leafSerial = edit.serial
	}
	view.trimHTMLLeafHistory(current)
	state.message = ""
	if view.active {
		state.focusRequest = htmlInputID
	}
	state.invalidator.Invalidate()
	return app.TextStateOf(view.input), true
}
