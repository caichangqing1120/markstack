package main

import (
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"

	"markstack/internal/richedit"
	"markstack/internal/workspace"
)

func (state *model) pasteHTMLClipboardImage() (app.TextState, error) {
	view := &state.html
	current := state.session.Current()
	if current == nil || view.input == nil {
		return app.TextState{}, nil
	}
	result := func() (app.TextState, error) { return app.TextStateOf(view.input), nil }
	if !view.active || current.ID != view.documentID || current.Text() != view.source || view.crossSelection != nil || view.blockedSelection {
		state.message = "当前 HTML 选区无法安全插入图片；原文未改"
		return result()
	}
	selection := view.input.Selection()
	if selection.Start != selection.End {
		state.message = "请先将光标定位到单个文字位置再粘贴图片"
		return result()
	}
	if _, composing := view.input.Composition(); composing {
		return result()
	}
	if current.Path == "" {
		state.message = "请先保存富文本文件，再粘贴图片"
		return result()
	}
	if err := current.VerifyDisk(); err != nil {
		state.message = err.Error()
		return result()
	}
	image, err := app.ClipboardImagePNG()
	if err != nil {
		state.message = err.Error()
		return result()
	}
	if _, err := decodeRichImage(image); err != nil {
		state.message = err.Error()
		return result()
	}
	relative := workspace.PNGAssetPath(image)
	before := current.Text()
	caret := byteOffsetOfRune(view.input.String(), view.input.Caret())
	next, err := richedit.InsertImage([]byte(before), view.leaf, caret, relative)
	if err != nil || !current.FitsSavedSize(len(next)) {
		state.message = "无法安全插入图片；原文未改"
		return result()
	}
	if err := writeClipboardPNGAsset(current, image); err != nil {
		state.message = err.Error()
		return result()
	}
	if err := current.VerifyDisk(); err != nil {
		state.message = err.Error()
		return result()
	}
	if err := applyHTMLSourceDifference(current, before, string(next)); err != nil {
		state.message = err.Error()
		return result()
	}
	imageTag := `<img src="` + relative + `" alt="">`
	position := view.leaf.Start + strings.Index(string(next[view.leaf.Start:]), imageTag)
	beforeBlock, beforeLeaf, beforeCaret := view.blockIndex, view.leafIndex, view.input.Caret()
	view.source = current.Text()
	view.parsed = richedit.Parse([]byte(view.source))
	view.leafSerial++
	view.workspaceMatch = nil
	view.scrollPending = false
	view.crossSelection = nil
	view.blockedSelection = false
	view.structureRedo = nil
	view.leafRedo = nil
	imageIndex := -1
	for index, leaf := range view.parsed.Blocks[view.blockIndex].Leaves {
		if leaf.Start == position && leaf.ImageSource == relative {
			imageIndex = index
			break
		}
	}
	if imageIndex < 0 {
		view.active = false
		state.message = "图片已插入；请重新定位光标"
		return result()
	}
	block := view.parsed.Blocks[view.blockIndex]
	newLeaf, newCaret := -1, 0
	if imageIndex+1 < len(block.Leaves) && block.Leaves[imageIndex+1].Editable {
		newLeaf = imageIndex + 1
	} else if imageIndex > 0 && block.Leaves[imageIndex-1].Editable {
		newLeaf = imageIndex - 1
		newCaret = len([]rune(block.Leaves[newLeaf].Visible))
	}
	if newLeaf < 0 {
		view.active = false
		state.message = "图片已插入；请切源码模式继续编辑"
		return result()
	}
	view.leafIndex = newLeaf
	view.leaf = block.Leaves[newLeaf]
	view.input = text.NewEditor(view.leaf.Visible)
	view.input.MoveTo(newCaret, false)
	view.structureUndo = append(view.structureUndo, htmlStructureEdit{
		before: before, after: view.source, beforeBlock: beforeBlock, afterBlock: view.blockIndex,
		beforeLeaf: beforeLeaf, afterLeaf: newLeaf, beforeCaret: beforeCaret, afterCaret: newCaret,
	})
	state.message = ""
	state.invalidator.Invalidate()
	updated := app.TextStateOf(view.input)
	updated.Redraw = true
	return updated, nil
}
