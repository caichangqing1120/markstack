package main

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"markstack/internal/richedit"
	"markstack/internal/session"
)

type htmlLeafTarget struct {
	blockIndex, leafIndex int
	visibleStart          int
	value                 string
	segment               htmlTextSegment
}

type htmlCrossSelection struct {
	blockIndex int
	lastBlock  int
	firstLeaf  int
	start, end int
	visible    string
	source     string
	documentID int
}

func (view *richHTMLView) nativeTextState() app.TextState {
	native := app.TextStateOf(view.input)
	if native.Composing || view.crossSelection == nil || view.dragging || view.crossSelection.lastBlock >= len(view.parsed.Blocks) {
		return native
	}
	selection := view.crossSelection
	var value strings.Builder
	endByte := 0
	for index := selection.blockIndex; index <= selection.lastBlock; index++ {
		if index > selection.blockIndex {
			value.WriteByte('\n')
		}
		if index == selection.lastBlock {
			endByte = value.Len() + selection.end
		}
		value.WriteString(view.parsed.Blocks[index].Visible)
	}
	visible := value.String()
	return app.TextState{
		Value: visible,
		Selection: app.TextRange{
			Start: utf8.RuneCountInString(view.parsed.Blocks[selection.blockIndex].Visible[:selection.start]),
			End:   utf8.RuneCountInString(visible[:endByte]),
		},
	}
}

func (view *richHTMLView) visibleBeforeLeaf(blockIndex, leafIndex int) int {
	offset := 0
	for index := 0; index < leafIndex; index++ {
		offset += len(view.parsed.Blocks[blockIndex].Leaves[index].Visible)
	}
	return offset
}

func (view *richHTMLView) selectAcrossLeaf(point ui.Point) bool {
	if view.wrapCanvas == nil || view.blockIndex < 0 || view.blockIndex >= len(view.parsed.Blocks) {
		return false
	}
	for index := len(view.leafTargets) - 1; index >= 0; index-- {
		target := view.leafTargets[index]
		if !contains(target.segment.bounds, point) || target.blockIndex == view.blockIndex && target.leafIndex == view.leafIndex {
			continue
		}
		segment := target.segment
		value := target.value
		end := min(segment.end, len(value))
		extent := target.visibleStart + segment.start + htmlTextOffsetAt(view.wrapCanvas, value[segment.start:end], segment.style, point.X-segment.bounds.X)
		selection := &htmlCrossSelection{source: view.source, documentID: view.documentID}
		if target.blockIndex == view.blockIndex {
			block := view.parsed.Blocks[view.blockIndex]
			first, last := min(target.leafIndex, view.leafIndex), max(target.leafIndex, view.leafIndex)
			for leafIndex := first; leafIndex <= last; leafIndex++ {
				leaf := block.Leaves[leafIndex]
				if !leaf.Editable || leaf.ImageSource != "" {
					return false
				}
			}
			selection.blockIndex, selection.lastBlock = view.blockIndex, view.blockIndex
			selection.firstLeaf = first
			selection.start, selection.end = min(view.crossAnchor, extent), max(view.crossAnchor, extent)
			if selection.start == selection.end || selection.start < 0 || selection.end > len(block.Visible) || !utf8.ValidString(block.Visible[:selection.start]) || !utf8.ValidString(block.Visible[:selection.end]) {
				return false
			}
			selection.visible = block.Visible[selection.start:selection.end]
		} else {
			selection.blockIndex, selection.lastBlock = min(target.blockIndex, view.blockIndex), max(target.blockIndex, view.blockIndex)
			if !view.canSelectAcrossBlocks(selection.blockIndex, selection.lastBlock) {
				return false
			}
			selection.firstLeaf = 0
			if view.blockIndex < target.blockIndex {
				selection.start, selection.end = view.crossAnchor, extent
			} else {
				selection.start, selection.end = extent, view.crossAnchor
			}
			first := view.parsed.Blocks[selection.blockIndex]
			last := view.parsed.Blocks[selection.lastBlock]
			if selection.start < 0 || selection.start > len(first.Visible) || selection.end < 0 || selection.end > len(last.Visible) {
				return false
			}
			var visible strings.Builder
			visible.WriteString(first.Visible[selection.start:])
			for blockIndex := selection.blockIndex + 1; blockIndex < selection.lastBlock; blockIndex++ {
				visible.WriteByte('\n')
				visible.WriteString(view.parsed.Blocks[blockIndex].Visible)
			}
			visible.WriteByte('\n')
			visible.WriteString(last.Visible[:selection.end])
			selection.visible = visible.String()
		}
		view.crossSelection = selection
		return true
	}
	return false
}

func (view *richHTMLView) canSelectAcrossBlocks(first, last int) bool {
	for index := first; index <= last; index++ {
		block := view.parsed.Blocks[index]
		if block.Kind != richedit.Paragraph || block.ReadOnly || len(block.Leaves) != 1 || !block.Leaves[0].Editable || block.Leaves[0].ImageSource != "" || block.Leaves[0].Start != block.OpenEnd || block.Leaves[0].End != block.CloseStart {
			return false
		}
		if index > first {
			previous := view.parsed.Blocks[index-1]
			if previous.End > block.Start || strings.TrimSpace(view.source[previous.End:block.Start]) != "" {
				return false
			}
		}
	}
	return true
}

func (state *model) applyHTMLCrossSelection(replacement string) error {
	view := &state.html
	selection := view.crossSelection
	current := state.session.Current()
	if selection == nil || current == nil || current.ID != selection.documentID || current.Text() != selection.source || selection.blockIndex >= len(view.parsed.Blocks) {
		return richedit.ErrStaleLeaf
	}
	before := current.Text()
	block := view.parsed.Blocks[selection.blockIndex]
	var patched []byte
	var err error
	if selection.lastBlock > selection.blockIndex {
		patched, err = richedit.ReplaceBlockSelection([]byte(before), view.parsed, selection.blockIndex, selection.start, selection.lastBlock, selection.end, replacement)
	} else {
		patched, err = richedit.ReplaceSelection([]byte(before), block, selection.start, selection.end, replacement)
	}
	if err != nil {
		return err
	}
	if err := applyHTMLSourceDifference(current, before, string(patched)); err != nil {
		return err
	}
	view.source = current.Text()
	view.parsed = richedit.Parse([]byte(view.source))
	view.leafSerial++
	view.workspaceMatch = nil
	view.scrollPending = false
	view.blockIndex, view.leafIndex = selection.blockIndex, selection.firstLeaf
	view.refreshLeaf()
	if view.active {
		visibleStart := 0
		for index := 0; index < selection.firstLeaf; index++ {
			visibleStart += len(block.Leaves[index].Visible)
		}
		prefix := max(0, min(selection.start-visibleStart, len(block.Leaves[selection.firstLeaf].Visible)))
		caret := utf8.RuneCountInString(block.Leaves[selection.firstLeaf].Visible[:prefix] + replacement)
		view.input = text.NewEditor(view.leaf.Visible)
		view.input.MoveTo(min(caret, view.input.RuneCount()), false)
	}
	view.crossSelection = nil
	view.blockedSelection = false
	view.leafRedo = nil
	state.message = ""
	return nil
}

func applyHTMLSourceDifference(document *session.Document, before, after string) error {
	if before == after {
		return nil
	}
	start := 0
	for start < len(before) && start < len(after) {
		original, originalSize := utf8.DecodeRuneInString(before[start:])
		next, nextSize := utf8.DecodeRuneInString(after[start:])
		if original != next || originalSize != nextSize {
			break
		}
		start += originalSize
	}
	endBefore, endAfter := len(before), len(after)
	for endBefore > start && endAfter > start {
		original, originalSize := utf8.DecodeLastRuneInString(before[:endBefore])
		next, nextSize := utf8.DecodeLastRuneInString(after[:endAfter])
		if original != next || originalSize != nextSize {
			break
		}
		endBefore -= originalSize
		endAfter -= nextSize
	}
	document.Editor.BreakUndoGroup()
	defer document.Editor.BreakUndoGroup()
	if err := document.EditRange(start, endBefore, after[start:endAfter]); err != nil {
		return err
	}
	if document.Text() != after {
		if !document.Editor.Undo() || document.Text() != before {
			return errors.New("HTML source transaction could not be restored")
		}
		return richedit.ErrReadOnlyLeaf
	}
	return nil
}
