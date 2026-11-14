package main

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"

	"markstack/internal/richedit"
)

func (state *model) navigateHTMLTable(reverse bool) gfx.EventResult {
	view := &state.html
	current := state.session.Current()
	if current == nil || !view.active || view.input == nil || view.blockIndex >= len(view.parsed.Blocks) {
		return gfx.EventResult{}
	}
	cell := view.parsed.Blocks[view.blockIndex]
	if cell.Kind != richedit.TableCell {
		return gfx.EventResult{}
	}
	result := gfx.EventResult{Handled: true, Repaint: true, Focus: htmlInputID}
	if _, composing := view.input.Composition(); composing {
		state.message = "请先完成输入法组合，再切换表格单元格"
		return result
	}
	if view.crossSelection != nil || view.blockedSelection || current.Text() != view.source {
		state.message = "当前 HTML 选区无法安全切换表格单元格"
		return result
	}
	direction := 1
	if reverse {
		direction = -1
	}
	for index := view.blockIndex + direction; index >= 0 && index < len(view.parsed.Blocks); index += direction {
		candidate := view.parsed.Blocks[index]
		if candidate.Kind != richedit.TableCell || candidate.TableStart != cell.TableStart {
			break
		}
		for leaf := range candidate.Leaves {
			leafIndex := leaf
			if reverse {
				leafIndex = len(candidate.Leaves) - 1 - leaf
			}
			if candidate.Leaves[leafIndex].Editable {
				state.message = ""
				return state.activateHTMLLeafAt(index, leafIndex)
			}
		}
	}
	if reverse {
		state.message = "已到表格起始单元格"
		return result
	}
	before := current.Text()
	next, index, err := richedit.InsertTableRow([]byte(before), view.parsed, view.blockIndex, current.Newline())
	if err == nil {
		err = applyHTMLSourceDifference(current, before, string(next))
	}
	if err != nil {
		state.message = "此表格无法安全新增行；原文未改，请切源码模式处理"
		return result
	}
	view.structureUndo = append(view.structureUndo, htmlStructureEdit{
		before: before, after: current.Text(), beforeBlock: view.blockIndex, afterBlock: index,
		beforeLeaf: view.leafIndex, afterLeaf: 0, beforeCaret: view.input.Caret(), afterCaret: 0,
	})
	view.structureRedo = nil
	view.source = current.Text()
	view.parsed = richedit.Parse([]byte(view.source))
	view.blockIndex, view.leafIndex = index, 0
	view.leaf = view.parsed.Blocks[index].Leaves[0]
	view.input = text.NewEditor(view.leaf.Visible)
	view.input.MoveTo(0, false)
	view.workspaceMatch = nil
	view.scrollPending = false
	view.crossSelection = nil
	view.blockedSelection = false
	state.message = ""
	state.invalidator.Invalidate()
	return result
}
