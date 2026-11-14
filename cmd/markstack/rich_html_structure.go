package main

import (
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"

	"markstack/internal/richedit"
)

type htmlStructureEdit struct {
	kind                    htmlStructureKind
	before, after           string
	beforeBlock, afterBlock int
	beforeLeaf, afterLeaf   int
	beforeCaret, afterCaret int
}

type htmlStructureKind uint8

const htmlListExit htmlStructureKind = 1

func (state *model) insertHTMLParagraph() gfx.EventResult {
	view := &state.html
	current := state.session.Current()
	if current == nil || !view.active || view.blockIndex >= len(view.parsed.Blocks) || view.input == nil {
		return gfx.EventResult{}
	}
	if _, composing := view.input.Composition(); composing {
		return gfx.EventResult{}
	}
	selection := view.input.Selection()
	block := view.parsed.Blocks[view.blockIndex]
	if block.Kind == richedit.ListItem && block.Visible == "" && selection.Start == selection.End && view.input.Caret() == 0 && view.crossSelection == nil {
		return state.exitHTMLListItem()
	}
	canSplit := richedit.CanSplitParagraph(block)
	if block.Kind == richedit.ListItem {
		canSplit = richedit.CanSplitListItem(block)
	}
	if view.crossSelection != nil || selection.Start != selection.End || !canSplit {
		state.message = "此 HTML 结构无法安全拆段；原文未改，请切源码模式处理"
		return gfx.EventResult{Handled: true, Repaint: true, Focus: htmlInputID}
	}
	before := current.Text()
	caret := view.visibleBeforeLeaf(view.blockIndex, view.leafIndex) + byteOffsetOfRune(view.input.String(), view.input.Caret())
	var next []byte
	var err error
	if block.Kind == richedit.ListItem {
		next, err = richedit.SplitListItem([]byte(before), block, caret, current.Newline())
	} else {
		next, err = richedit.SplitParagraph([]byte(before), block, caret, current.Newline())
	}
	if err == nil {
		err = applyHTMLSourceDifference(current, before, string(next))
	}
	if err != nil {
		state.message = err.Error()
		return gfx.EventResult{Handled: true, Repaint: true, Focus: htmlInputID}
	}
	view.structureUndo = append(view.structureUndo, htmlStructureEdit{
		before: before, after: current.Text(), beforeBlock: view.blockIndex, afterBlock: view.blockIndex + 1,
		beforeLeaf: view.leafIndex, beforeCaret: view.input.Caret(), afterCaret: 0,
	})
	view.structureRedo = nil
	view.source = current.Text()
	view.parsed = richedit.Parse([]byte(view.source))
	view.blockIndex++
	view.leafIndex = 0
	view.leaf = view.parsed.Blocks[view.blockIndex].Leaves[0]
	view.input = text.NewEditor(view.leaf.Visible)
	view.input.MoveTo(0, false)
	view.workspaceMatch = nil
	view.scrollPending = false
	view.crossSelection = nil
	view.blockedSelection = false
	state.message = ""
	state.invalidator.Invalidate()
	return gfx.EventResult{Handled: true, Repaint: true, Focus: htmlInputID}
}

func (state *model) exitHTMLListItem() gfx.EventResult {
	view := &state.html
	current := state.session.Current()
	before := current.Text()
	next, paragraphIndex, err := richedit.ExitEmptyListItem([]byte(before), view.parsed, view.blockIndex, current.Newline())
	if err == nil {
		err = applyHTMLSourceDifference(current, before, string(next))
	}
	if err != nil {
		state.message = "空列表项无法安全退出；原文未改，请切源码模式处理"
		return gfx.EventResult{Handled: true, Repaint: true, Focus: htmlInputID}
	}
	view.structureUndo = append(view.structureUndo, htmlStructureEdit{
		kind:   htmlListExit,
		before: before, after: current.Text(), beforeBlock: view.blockIndex, afterBlock: paragraphIndex,
		beforeLeaf: view.leafIndex, afterLeaf: 0, beforeCaret: 0, afterCaret: 0,
	})
	view.structureRedo = nil
	view.source = current.Text()
	view.parsed = richedit.Parse([]byte(view.source))
	view.blockIndex, view.leafIndex = paragraphIndex, 0
	view.leaf = view.parsed.Blocks[paragraphIndex].Leaves[0]
	view.input = text.NewEditor(view.leaf.Visible)
	view.input.MoveTo(0, false)
	view.workspaceMatch = nil
	view.scrollPending = false
	view.crossSelection = nil
	view.blockedSelection = false
	state.message = ""
	state.invalidator.Invalidate()
	return gfx.EventResult{Handled: true, Repaint: true, Focus: htmlInputID}
}

func (state *model) reverseHTMLListExit() bool {
	view := &state.html
	if !view.active || view.input == nil || len(view.structureUndo) == 0 || view.input.Caret() != 0 || view.crossSelection != nil {
		return false
	}
	selection := view.input.Selection()
	if selection.Start != selection.End || view.input.String() != "" {
		return false
	}
	if _, composing := view.input.Composition(); composing {
		return false
	}
	edit := view.structureUndo[len(view.structureUndo)-1]
	current := state.session.Current()
	if edit.kind != htmlListExit || current == nil || current.Text() != edit.after || view.blockIndex != edit.afterBlock || view.leafIndex != edit.afterLeaf {
		return false
	}
	if err := applyHTMLSourceDifference(current, edit.after, edit.before); err != nil {
		state.message = err.Error()
		return true
	}
	view.structureUndo = append(view.structureUndo, htmlStructureEdit{
		before: edit.after, after: current.Text(), beforeBlock: edit.afterBlock, afterBlock: edit.beforeBlock,
		beforeLeaf: edit.afterLeaf, afterLeaf: edit.beforeLeaf, beforeCaret: edit.afterCaret, afterCaret: edit.beforeCaret,
	})
	view.structureRedo = nil
	view.source = current.Text()
	view.parsed = richedit.Parse([]byte(view.source))
	view.blockIndex, view.leafIndex = edit.beforeBlock, edit.beforeLeaf
	view.leaf = view.parsed.Blocks[view.blockIndex].Leaves[view.leafIndex]
	view.input = text.NewEditor(view.leaf.Visible)
	view.input.MoveTo(edit.beforeCaret, false)
	view.workspaceMatch = nil
	view.scrollPending = false
	view.crossSelection = nil
	view.blockedSelection = false
	state.message = ""
	state.invalidator.Invalidate()
	return true
}

func (state *model) mergeHTMLParagraph() bool {
	view := &state.html
	if !view.active || view.blockIndex <= 0 || view.input == nil || view.input.Caret() != 0 {
		return false
	}
	selection := view.input.Selection()
	if selection.Start != selection.End || view.crossSelection != nil {
		return false
	}
	if _, composing := view.input.Composition(); composing {
		return false
	}
	current := state.session.Current()
	if current == nil || current.Text() != view.source {
		state.message = "HTML 原文已变化；请重新定位"
		return true
	}
	previousIndex := view.blockIndex - 1
	previous := view.parsed.Blocks[previousIndex]
	before := current.Text()
	next, err := richedit.ReplaceBlockSelection([]byte(before), view.parsed, previousIndex, len(previous.Visible), view.blockIndex, 0, "")
	if err != nil {
		if previous.Kind == richedit.ListItem && view.parsed.Blocks[view.blockIndex].Kind == richedit.ListItem {
			next, err = richedit.MergeListItems([]byte(before), view.parsed, previousIndex, view.blockIndex)
		} else {
			next, err = richedit.MergeParagraphs([]byte(before), view.parsed, previousIndex, view.blockIndex)
		}
	}
	if err == nil {
		err = applyHTMLSourceDifference(current, before, string(next))
	}
	if err != nil {
		state.message = "无法安全合并 HTML 段落；原文未改，请切源码模式处理"
		return true
	}
	view.source = current.Text()
	view.parsed = richedit.Parse([]byte(view.source))
	leafIndex, caret := htmlLeafAtVisible(view.parsed.Blocks[previousIndex], len(previous.Visible))
	view.structureUndo = append(view.structureUndo, htmlStructureEdit{
		before: before, after: current.Text(), beforeBlock: view.blockIndex, afterBlock: previousIndex,
		beforeLeaf: view.leafIndex, afterLeaf: leafIndex, beforeCaret: 0, afterCaret: caret,
	})
	view.structureRedo = nil
	view.blockIndex, view.leafIndex = previousIndex, leafIndex
	view.leaf = view.parsed.Blocks[previousIndex].Leaves[leafIndex]
	view.input = text.NewEditor(view.leaf.Visible)
	view.input.MoveTo(caret, false)
	view.workspaceMatch = nil
	view.scrollPending = false
	view.blockedSelection = false
	state.message = ""
	state.invalidator.Invalidate()
	return true
}

func htmlLeafAtVisible(block richedit.Block, offset int) (int, int) {
	visible := 0
	for index, leaf := range block.Leaves {
		end := visible + len(leaf.Visible)
		if offset < end || index == len(block.Leaves)-1 {
			local := max(0, min(offset-visible, len(leaf.Visible)))
			return index, utf8.RuneCountInString(leaf.Visible[:local])
		}
		visible = end
	}
	return 0, 0
}

func (state *model) stepHTMLStructure(undo bool) (app.TextState, bool) {
	view := &state.html
	stack := view.structureUndo
	if !undo {
		stack = view.structureRedo
	}
	if len(stack) == 0 {
		return app.TextState{}, false
	}
	edit := stack[len(stack)-1]
	current := state.session.Current()
	if current == nil || undo && current.Text() != edit.after || !undo && current.Text() != edit.before {
		return app.TextState{}, false
	}
	changed := false
	if undo {
		changed = current.Editor.Undo()
	} else {
		changed = current.Editor.Redo()
	}
	want := edit.before
	blockIndex, leafIndex, caret := edit.beforeBlock, edit.beforeLeaf, edit.beforeCaret
	if !undo {
		want, blockIndex, leafIndex, caret = edit.after, edit.afterBlock, edit.afterLeaf, edit.afterCaret
	}
	if !changed || current.Text() != want {
		if changed {
			if undo {
				current.Editor.Redo()
			} else {
				current.Editor.Undo()
			}
		}
		state.message = "无法同步 HTML 结构历史；原文未改"
		return app.TextStateOf(view.input), true
	}
	view.source = current.Text()
	view.parsed = richedit.Parse([]byte(view.source))
	if blockIndex >= len(view.parsed.Blocks) || leafIndex >= len(view.parsed.Blocks[blockIndex].Leaves) || !view.parsed.Blocks[blockIndex].Leaves[leafIndex].Editable {
		view.active = false
		state.mode = editMode
		return app.TextStateOf(view.input), true
	}
	view.blockIndex, view.leafIndex = blockIndex, leafIndex
	view.leaf = view.parsed.Blocks[blockIndex].Leaves[leafIndex]
	view.input = text.NewEditor(view.leaf.Visible)
	view.input.MoveTo(min(caret, utf8.RuneCountInString(view.leaf.Visible)), false)
	view.workspaceMatch = nil
	view.crossSelection = nil
	if undo {
		view.structureUndo = view.structureUndo[:len(view.structureUndo)-1]
		view.structureRedo = append(view.structureRedo, edit)
	} else {
		view.structureRedo = view.structureRedo[:len(view.structureRedo)-1]
		view.structureUndo = append(view.structureUndo, edit)
	}
	return app.TextStateOf(view.input), true
}
