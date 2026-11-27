package main

import (
	"crypto/sha256"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/text"

	"markstack/internal/markdownedit"
	"markstack/internal/session"
)

const maxRenderedHistory = 256

type renderedHistory struct {
	crossUndo     []renderedCrossEdit
	crossRedo     []renderedCrossEdit
	structureUndo []renderedStructureEdit
	structureRedo []renderedStructureEdit
}

func renderedHistoryInputState(input *text.Editor) app.TextState {
	if input == nil {
		return app.TextState{}
	}
	return app.TextStateOf(input)
}

func (state *model) renderedHistorySnapshot(current *session.Document) (renderedHistory, int) {
	if current == nil {
		return renderedHistory{}, 0
	}
	if state.rich.documentID == current.ID && state.rich.source == current.Text() {
		state.sampleRenderedNativeGroups(current)
		return state.rich.renderedHistory, state.rich.appliedEdits + state.rich.sourceHistoryOffset
	}
	if state.selection.active && state.selection.documentID == current.ID && state.selection.source == current.Text() {
		return state.selection.history, state.selection.historyPosition
	}
	return renderedHistory{}, 0
}

// sampleRenderedNativeGroups records the native Undo/Redo units of an active
// rendered-only input before that editor is discarded. Viva's HistorySince
// reports accepted primitive batches rather than the units its public Undo and
// Redo coalesce, so the input's own Undo/Redo cursor is sampled and then
// restored. Only ordered, contiguous virtual-text edits that match the exact
// visible fingerprints and source/virtual chain receive a group count. A unit
// that begins at a retained edit but cannot be matched in full, because the
// history bound already dropped its oldest edits, is recorded as unrecovered so
// that a recreated input refuses it instead of replaying a shorter prefix. The
// document editor is never touched.
func (state *model) sampleRenderedNativeGroups(current *session.Document) {
	rich := &state.rich
	if current == nil || !rich.active || rich.input == nil || rich.documentID != current.ID ||
		rich.source != current.Text() || len(rich.crossUndo) == 0 {
		return
	}
	if _, composing := rich.input.Composition(); composing {
		return
	}
	rich.sampleNativeGroups()
}

func (rich *renderedEditor) sampleNativeGroups() bool {
	input := rich.input
	selection := input.Selection()
	reversed := input.Caret() == selection.Start
	canUndo, canRedo := input.CanUndo(), input.CanRedo()
	if !canUndo || len(rich.crossUndo) == 0 {
		return false
	}
	position := rich.appliedEdits + rich.sourceHistoryOffset
	top := len(rich.crossUndo) - 1
	// Every candidate native unit contains the newest recorded edit, so a
	// nonvirtual or off-position top proves that no group can match. Reject it
	// before copying or hashing the visible text.
	if !renderedNativeTopOrdered(rich.crossUndo, top, position) {
		return false
	}
	original := input.String()
	beforeHash := sha256.Sum256([]byte(original))
	if rich.crossUndo[top].afterVisibleHash != beforeHash {
		return false
	}
	runs := make([]int, 0, min(len(rich.crossUndo), maxRenderedHistory))
	undone := 0
	unmatched := false
	for undone < len(rich.crossUndo) && top >= 0 {
		if !renderedNativeTopOrdered(rich.crossUndo, top, position) ||
			rich.crossUndo[top].afterVisibleHash != beforeHash {
			break
		}
		if !input.Undo() {
			break
		}
		undone++
		afterHash := sha256.Sum256([]byte(input.String()))
		count, matched := renderedNativeGroup(rich.crossUndo, top, position, beforeHash, afterHash)
		if !matched {
			// A native unit did begin here, but no retained run reproduces its
			// visible transition: the history bound dropped its oldest edits.
			unmatched = true
			break
		}
		runs = append(runs, count)
		top -= count
		position -= count
		// Adjacent groups share this boundary: the next unit starts exactly
		// where the one just matched ended, so its digest is already known.
		beforeHash = afterHash
	}
	restored := true
	for range undone {
		if !input.Redo() {
			restored = false
			break
		}
	}
	if restored && (input.String() != original || input.CanUndo() != canUndo || input.CanRedo() != canRedo) {
		restored = false
	}
	if !restored {
		// The view is about to be discarded, so keep its visible text and
		// orientation while dropping an input history that could not be
		// proved restored. Never publish groups from a failed sample.
		rich.input = text.NewEditor(original)
		restoreRenderedInputSelection(rich.input, selection, reversed)
		return false
	}
	restoreRenderedInputSelection(input, selection, reversed)
	top = len(rich.crossUndo) - 1
	for _, count := range runs {
		for offset := 0; offset < count; offset++ {
			rich.crossUndo[top-offset].group = count
		}
		top -= count
	}
	if unmatched && top >= 0 && top < len(rich.crossUndo) {
		// Keep the exact unit boundary sampled above but record that the unit
		// cannot be rebuilt from the retained edits, so a recreated input
		// refuses it as a whole instead of undoing only its visible suffix.
		rich.crossUndo[top].unrecovered = true
	}
	return len(runs) > 0
}

// renderedNativeTopOrdered reports whether the newest remaining recorded edit
// can begin a native unit at this position. Every candidate unit in
// renderedNativeGroup covers that edit at offset zero, so failing this cheap
// metadata check proves no match without touching the input editor.
func renderedNativeTopOrdered(stack []renderedCrossEdit, top, position int) bool {
	if top < 0 || top >= len(stack) {
		return false
	}
	edit := stack[top]
	return edit.virtualText && edit.position == position
}

// renderedNativeGroup matches one public native Undo unit against the newest
// contiguous virtual-text edits. The unit must run exactly from before to
// after and every adjacent edit must agree on visible bytes, source bytes and
// virtual range, including their ordered positions. Callers pass the exact
// visible digests for both states so adjacent units can reuse the boundary.
func renderedNativeGroup(stack []renderedCrossEdit, top, position int, beforeHash, afterHash [32]byte) (int, bool) {
	if top < 0 || top >= len(stack) {
		return 0, false
	}
	for count := 1; count <= top+1; count++ {
		first := stack[top-count+1]
		if !first.virtualText || first.beforeVisibleHash != afterHash {
			continue
		}
		valid := true
		for offset := 0; offset < count; offset++ {
			edit := stack[top-offset]
			if !edit.virtualText || edit.position != position-offset {
				valid = false
				break
			}
			if offset == 0 {
				if edit.afterVisibleHash != beforeHash {
					valid = false
					break
				}
				continue
			}
			newer := stack[top-offset+1]
			if edit.afterVisibleHash != newer.beforeVisibleHash || edit.afterSourceHash != newer.beforeSourceHash ||
				!sameRenderedVirtual(edit.afterVirtual, newer.beforeVirtual) {
				valid = false
				break
			}
		}
		if valid {
			return count, true
		}
	}
	return 0, false
}

func restoreRenderedInputSelection(input *text.Editor, selection text.Range, reversed bool) {
	if reversed {
		selection.Start, selection.End = selection.End, selection.Start
	}
	input.SetSelection(selection)
}

func (state *model) deactivateRenderedView(current *session.Document) {
	history, position := state.renderedHistorySnapshot(current)
	emptyTarget := state.rich.emptyTargetSnapshot(current)
	state.rich = renderedEditor{renderedHistory: history, sourceHistoryOffset: position, emptyTarget: emptyTarget}
	state.selection = renderedSelection{}
	if current != nil {
		state.rich.documentID, state.rich.source = current.ID, current.Text()
		state.rich.trimHistory(current)
	}
}

type renderedStructureEdit struct {
	beforeSourceHash, afterSourceHash [32]byte
	beforeIndex, afterIndex           int
	beforeCaret, afterCaret           int
	beforeVirtual, afterVirtual       *renderedVirtualState
	position                          int
}

type renderedVirtualState struct {
	kind           markdownedit.Kind
	start, end     int
	suffix, insert string
	depth          int
}

type renderedEmptyTarget struct {
	index, start, end, depth int
	insert                   string
}

func (rich *renderedEditor) emptyTargetSnapshot(current *session.Document) *renderedEmptyTarget {
	if current == nil || rich.documentID != current.ID || rich.source != current.Text() {
		return nil
	}
	if !rich.active {
		return rich.emptyTarget
	}
	if !rich.virtual || rich.index < 0 || rich.block.Kind != markdownedit.Paragraph || rich.input == nil || rich.input.String() != "" ||
		rich.virtualStart < 0 || rich.virtualEnd < rich.virtualStart || rich.virtualEnd > len(rich.source) ||
		strings.Trim(rich.source[rich.virtualStart:rich.virtualEnd], "\r\n") != "" {
		return nil
	}
	if _, composing := rich.input.Composition(); composing {
		return nil
	}
	return &renderedEmptyTarget{
		index: rich.index, start: rich.virtualStart, end: rich.virtualEnd, depth: rich.virtualDepth,
		insert: strings.Clone(rich.virtualInsert),
	}
}

func (state *model) restoreRenderedEmptyTarget(current *session.Document) {
	if state.rich.active || state.table.active || state.codeBlock.active || state.selection.active {
		return
	}
	target := state.rich.emptyTargetSnapshot(current)
	if target == nil || target.index < 0 || target.depth < 0 || target.start < 0 || target.end > len(current.Text()) || target.start > target.end {
		return
	}
	source := current.Text()
	lineStart := strings.LastIndexAny(source[:target.start], "\r\n") + 1
	lineEnd := len(source)
	if offset := strings.IndexAny(source[target.end:], "\r\n"); offset >= 0 {
		lineEnd = target.end + offset
	}
	if strings.Trim(source[target.start:target.end], "\r\n") != "" ||
		strings.Trim(source[lineStart:target.start], " \t") != "" ||
		strings.Trim(source[target.end:lineEnd], " \t") != "" ||
		target.start > 0 && target.start < len(source) && source[target.start-1] == '\r' && source[target.start] == '\n' ||
		target.end > 0 && target.end < len(source) && source[target.end-1] == '\r' && source[target.end] == '\n' {
		return
	}
	for _, block := range markdownedit.Parse([]byte(source)) {
		if block.Start <= target.start && target.start < block.End || block.Start < target.end && block.End > target.start {
			return
		}
	}
	var parsed markdown.Document
	parsed.Parse(source)
	index := 0
	for _, block := range parsed.Blocks {
		if block.Start < target.start {
			index++
		}
	}
	if index != target.index {
		return
	}
	history, position := state.renderedHistorySnapshot(current)
	virtual := &renderedVirtualState{
		kind: markdownedit.Paragraph, start: target.start, end: target.end,
		suffix: source[target.start:target.end], insert: target.insert, depth: target.depth,
	}
	state.parsed = parsed
	if state.restoreRenderedStructureView(target.index, 0, virtual) {
		state.rich.renderedHistory = history
		state.rich.sourceHistoryOffset = position
		state.rich.trimHistory(current)
	}
}

func (rich *renderedEditor) virtualState() *renderedVirtualState {
	if !rich.virtual {
		return nil
	}
	return &renderedVirtualState{
		kind: rich.block.Kind, start: rich.virtualStart, end: rich.virtualEnd,
		suffix: strings.Clone(rich.virtualSuffix), insert: strings.Clone(rich.virtualInsert), depth: rich.virtualDepth,
	}
}

func appendRenderedHistory[Edit any](history []Edit, edit Edit) []Edit {
	if len(history) >= maxRenderedHistory {
		keep := maxRenderedHistory - 1
		copy(history, history[len(history)-keep:])
		clear(history[keep:])
		history = history[:keep]
	}
	return append(history, edit)
}

func (rich *renderedEditor) trimHistory(current *session.Document) {
	oldest := rich.appliedEdits + rich.sourceHistoryOffset - maxRenderedHistory
	for len(rich.crossUndo) > 0 && rich.crossUndo[0].position <= oldest {
		rich.crossUndo = rich.crossUndo[1:]
	}
	for len(rich.structureUndo) > 0 && rich.structureUndo[0].position <= oldest {
		rich.structureUndo = rich.structureUndo[1:]
	}
	if !current.Editor.CanUndo() {
		rich.crossUndo, rich.structureUndo = nil, nil
	}
	if !current.Editor.CanRedo() {
		rich.crossRedo, rich.structureRedo = nil, nil
	}
}

func (state *model) rememberRenderedStructure(history renderedHistory, position int, beforeSource string, beforeIndex, beforeCaret int, beforeVirtual *renderedVirtualState) {
	rich := &state.rich
	rich.renderedHistory = history
	rich.sourceHistoryOffset = position + 1
	rich.crossRedo, rich.structureRedo = nil, nil
	rich.structureUndo = appendRenderedHistory(rich.structureUndo, renderedStructureEdit{
		beforeSourceHash: sha256.Sum256([]byte(beforeSource)), afterSourceHash: sha256.Sum256([]byte(rich.source)),
		beforeIndex: beforeIndex, afterIndex: rich.index,
		beforeCaret: beforeCaret, afterCaret: rich.input.Caret(), position: position + 1,
		beforeVirtual: beforeVirtual, afterVirtual: rich.virtualState(),
	})
	rich.trimHistory(state.session.Current())
}

func (state *model) stepStructureHistory(undo bool) (app.TextState, bool) {
	rich := &state.rich
	stack := rich.structureUndo
	if !undo {
		stack = rich.structureRedo
	}
	if len(stack) == 0 {
		return app.TextState{}, false
	}
	edit := stack[len(stack)-1]
	position := edit.position
	if !undo {
		position--
	}
	current := state.session.Current()
	if current == nil || current.ID != rich.documentID || rich.appliedEdits+rich.sourceHistoryOffset != position {
		return app.TextState{}, false
	}
	haveSource, wantSource := edit.afterSourceHash, edit.beforeSourceHash
	index, caret := edit.beforeIndex, edit.beforeCaret
	haveVirtual, wantVirtual := edit.afterVirtual, edit.beforeVirtual
	haveIndex := edit.afterIndex
	if !undo {
		haveSource, wantSource = edit.beforeSourceHash, edit.afterSourceHash
		index, caret = edit.afterIndex, edit.afterCaret
		haveVirtual, wantVirtual = edit.beforeVirtual, edit.afterVirtual
		haveIndex = edit.beforeIndex
	}
	virtual := rich.virtualState()
	if sha256.Sum256([]byte(current.Text())) != haveSource || virtual != nil && (haveVirtual == nil || *virtual != *haveVirtual) {
		return app.TextState{}, false
	}
	if virtual == nil && haveVirtual != nil {
		if _, valid := renderedHistoryTarget(current, haveIndex, haveVirtual); !valid {
			return app.TextState{}, false
		}
	}
	changed := false
	if undo {
		changed = current.Editor.Undo()
	} else {
		changed = current.Editor.Redo()
	}
	if !changed {
		rich.trimHistory(current)
		return renderedHistoryInputState(rich.input), true
	}
	input, history := rich.input, rich.renderedHistory
	if undo {
		history.structureUndo = history.structureUndo[:len(history.structureUndo)-1]
		history.structureRedo = appendRenderedHistory(history.structureRedo, edit)
		position--
	} else {
		history.structureRedo = history.structureRedo[:len(history.structureRedo)-1]
		history.structureUndo = appendRenderedHistory(history.structureUndo, edit)
		position++
	}
	if sha256.Sum256([]byte(current.Text())) == wantSource {
		state.parsed.Parse(current.Text())
		if state.restoreRenderedStructureView(index, caret, wantVirtual) {
			state.rich.renderedHistory = history
			state.rich.sourceHistoryOffset = position
			state.rich.trimHistory(current)
			return app.TextStateOf(state.rich.input), true
		}
	}
	state.rich = renderedEditor{}
	state.mode = editMode
	state.focusRequest = sourceID
	return renderedHistoryInputState(input), true
}

func (state *model) restoreRenderedStructureView(index, caret int, virtual *renderedVirtualState) bool {
	if virtual == nil {
		result := state.activateRendered(index)
		if !state.rich.active || caret < 0 || caret > state.rich.input.RuneCount() {
			return false
		}
		state.rich.input.MoveTo(caret, false)
		state.focusRequest = result.Focus
		return true
	}
	rich, valid := renderedVirtualHistoryEditor(state.session.Current(), index, caret, virtual)
	if !valid || index > len(state.parsed.Blocks) {
		return false
	}
	state.rich = rich
	state.focusRequest = richInputID
	return true
}

func renderedVirtualHistoryEditor(current *session.Document, index, caret int, virtual *renderedVirtualState) (renderedEditor, bool) {
	source := current.Text()
	if index < 0 || virtual.start < 0 || virtual.end > len(source) ||
		virtual.end-virtual.start < len(virtual.suffix) || virtual.depth < 0 ||
		virtual.kind != markdownedit.Paragraph && virtual.kind != markdownedit.ListItem ||
		!strings.HasSuffix(source[virtual.start:virtual.end], virtual.suffix) {
		return renderedEditor{}, false
	}
	value := source[virtual.start : virtual.end-len(virtual.suffix)]
	if !utf8.ValidString(source[:virtual.start]) || !utf8.ValidString(value) || caret < 0 || caret > utf8.RuneCountInString(value) {
		return renderedEditor{}, false
	}
	rich := renderedEditor{
		active: true, virtual: true, index: index, documentID: current.ID, source: source,
		input: text.NewEditor(value), block: markdownedit.Block{Kind: virtual.kind},
		virtualStart: virtual.start, virtualEnd: virtual.end, virtualSuffix: virtual.suffix,
		virtualInsert: virtual.insert, virtualDepth: virtual.depth,
	}
	if next, err := virtualBlockSource(&rich, value); err != nil || string(next) != source {
		return renderedEditor{}, false
	}
	rich.input.MoveTo(caret, false)
	rich.refreshVirtualBlock()
	return rich, true
}

func (rich *renderedEditor) rememberCrossHistory(beforeSource, afterSource, beforeVisible, afterVisible string, virtualText bool) {
	if !rich.pendingCross {
		return
	}
	selection := rich.input.Selection()
	if rich.input.Caret() == selection.Start {
		selection.Start, selection.End = selection.End, selection.Start
	}
	beforeVirtual := rich.virtualState()
	var afterVirtual *renderedVirtualState
	if beforeVirtual != nil {
		after := *beforeVirtual
		after.end += len(afterSource) - len(beforeSource)
		afterVirtual = &after
	}
	rich.crossUndo = appendRenderedHistory(rich.crossUndo, renderedCrossEdit{
		beforeSourceHash: sha256.Sum256([]byte(beforeSource)), afterSourceHash: sha256.Sum256([]byte(afterSource)),
		beforeVisibleHash: sha256.Sum256([]byte(beforeVisible)), afterVisibleHash: sha256.Sum256([]byte(afterVisible)),
		beforeSelect: rich.crossSelection, afterSelect: selection, inputChanged: rich.crossInputChange, virtualText: virtualText,
		index: rich.index, beforeVirtual: beforeVirtual, afterVirtual: afterVirtual,
		position: rich.appliedEdits + rich.sourceHistoryOffset + 1,
	})
	if !virtualText {
		rich.input.BreakUndoGroup()
	}
	rich.pendingCross = false
}

func renderedHistoryTarget(current *session.Document, index int, virtual *renderedVirtualState) (renderedEditor, bool) {
	source := current.Text()
	var parsed markdown.Document
	parsed.Parse(source)
	if index < 0 || index > len(parsed.Blocks) {
		return renderedEditor{}, false
	}
	if virtual != nil {
		return renderedVirtualHistoryEditor(current, index, 0, virtual)
	}
	blocks := markdownedit.Parse([]byte(source))
	normalizeRenderedTasks(&parsed, blocks)
	mapping := renderedMapping(&parsed, blocks)
	if index >= len(mapping) || mapping[index] < 0 {
		return renderedEditor{}, false
	}
	block := blocks[mapping[index]]
	return renderedEditor{
		active: true, index: index, sourceIndex: mapping[index], documentID: current.ID,
		source: source, block: block, input: text.NewEditor(block.Visible),
	}, true
}

func (state *model) restoreOtherRenderedHistory(current *session.Document, beforeSource string, command app.TextCommand) bool {
	rich := &state.rich
	source := current.Text()
	if rich.documentID != current.ID || rich.source != beforeSource || source == beforeSource {
		return false
	}
	beforeBlocks, blocks := markdownedit.Parse([]byte(beforeSource)), markdownedit.Parse([]byte(source))
	if len(beforeBlocks) != len(blocks) {
		return false
	}
	selection := current.Editor.Selection()
	start, end := byteOffsetOfRune(source, selection.Start), byteOffsetOfRune(source, selection.End)
	for sourceIndex, block := range blocks {
		prior := beforeBlocks[sourceIndex]
		if !block.Editable || !prior.Editable || block.Kind != prior.Kind || block.Start != prior.Start ||
			rich.active && !rich.virtual && sourceIndex == rich.sourceIndex || start < block.Start || end > block.End ||
			beforeSource[:prior.Start] != source[:block.Start] || beforeSource[prior.End:] != source[block.End:] {
			continue
		}
		from, to, err := markdownedit.VisibleRangeAtSource([]byte(source), block, start, end)
		if err != nil {
			continue
		}
		visible := text.Range{Start: utf8.RuneCountInString(block.Visible[:from]), End: utf8.RuneCountInString(block.Visible[:to])}
		if current.Editor.Caret() == selection.Start {
			visible.Start, visible.End = visible.End, visible.Start
		}
		var parsed markdown.Document
		parsed.Parse(source)
		normalizeRenderedTasks(&parsed, blocks)
		for index, mapped := range renderedMapping(&parsed, blocks) {
			if mapped != sourceIndex {
				continue
			}
			position := rich.appliedEdits + rich.sourceHistoryOffset
			if command == app.TextUndo {
				position--
			} else {
				position++
			}
			history := rich.renderedHistory
			input := text.NewEditor(block.Visible)
			input.SetSelection(visible)
			state.rich = renderedEditor{
				active: true, index: index, sourceIndex: sourceIndex, documentID: current.ID,
				source: source, block: block, input: input,
				renderedHistory: history, sourceHistoryOffset: position,
			}
			state.rich.trimHistory(current)
			state.focusRequest = richInputID
			return true
		}
		return false
	}
	return false
}

func (rich *renderedEditor) moveVirtualTextHistory(undo bool, steps int) {
	cursor := rich.appliedEdits + rich.sourceHistoryOffset
	moved := 0
	contiguous := true
	for step := 0; step < steps; step++ {
		stack, position := rich.crossUndo, cursor-step
		if !undo {
			stack, position = rich.crossRedo, cursor+step+1
		}
		if len(stack) == 0 || !stack[len(stack)-1].virtualText || stack[len(stack)-1].position != position {
			contiguous = false
			continue
		}
		edit := stack[len(stack)-1]
		if undo {
			rich.crossUndo = stack[:len(stack)-1]
			rich.crossRedo = appendRenderedHistory(rich.crossRedo, edit)
		} else {
			rich.crossRedo = stack[:len(stack)-1]
			rich.crossUndo = appendRenderedHistory(rich.crossUndo, edit)
		}
		moved++
	}
	// A native unit that coalesced several rendered edits is replayed as one
	// step when the input editor no longer holds that native history. Mark it
	// only when every native source step had a contiguous virtual-text edit;
	// a separate native group keeps its own metadata.
	destination := rich.crossUndo
	if undo {
		destination = rich.crossRedo
	}
	if moved == steps && contiguous && moved > 0 {
		for index := len(destination) - moved; index < len(destination); index++ {
			destination[index].group = moved
		}
	}
}

func (state *model) restoreVirtualSourceHistory(current *session.Document, command app.TextCommand) bool {
	rich := &state.rich
	virtual := rich.virtualState()
	source, before := current.Text(), rich.source
	if virtual == nil || virtual.start < 0 || virtual.start > len(before) || virtual.end < virtual.start || virtual.end > len(before) ||
		!strings.HasPrefix(source, before[:virtual.start]) || !strings.HasSuffix(source, before[virtual.end:]) {
		return false
	}
	virtual.end += len(source) - len(before)
	selection := current.Editor.Selection()
	start, end := byteOffsetOfRune(source, selection.Start), byteOffsetOfRune(source, selection.End)
	if start < virtual.start || end > virtual.end-len(virtual.suffix) {
		return false
	}
	visibleSelection := text.Range{
		Start: utf8.RuneCountInString(source[virtual.start:start]),
		End:   utf8.RuneCountInString(source[virtual.start:end]),
	}
	if current.Editor.Caret() == selection.Start {
		visibleSelection.Start, visibleSelection.End = visibleSelection.End, visibleSelection.Start
	}
	history, position := rich.renderedHistory, rich.appliedEdits+rich.sourceHistoryOffset
	state.parsed.Parse(source)
	if !state.restoreRenderedStructureView(rich.index, visibleSelection.End, virtual) {
		return false
	}
	state.rich.input.SetSelection(visibleSelection)
	state.rich.renderedHistory = history
	if command == app.TextUndo {
		position--
	} else {
		position++
	}
	state.rich.sourceHistoryOffset = position
	state.rich.trimHistory(current)
	return true
}
