package main

import (
	"crypto/sha256"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"markstack/internal/markdownedit"
	"markstack/internal/session"
)

func (state *model) pendingFormatCompositionFocus() gfx.ID {
	if focus := state.pendingCompositionFocus(); focus != (gfx.ID{}) {
		return focus
	}
	if state.linkOpen && state.linkEditor != nil {
		if _, composing := state.linkEditor.Composition(); composing {
			return linkInputID
		}
	}
	return gfx.ID{}
}

func (state *model) refuseComposingFormat() bool {
	if focus := state.pendingFormatCompositionFocus(); focus != (gfx.ID{}) {
		state.message = session.ErrCompositionPending.Error()
		state.focusRequest = focus
		state.invalidator.Invalidate()
		return true
	}
	return false
}

func (state *model) formatControlCompositionFocus(target gfx.ID) gfx.ID {
	switch target {
	case boldBlockID, italicBlockID, strikeBlockID, linkBlockID, headingMenuID, linkApplyID:
		return state.pendingFormatCompositionFocus()
	}
	_, heading := target.In(headingLevels)
	_, structure := target.In(blockFormats)
	if heading || structure {
		return state.pendingFormatCompositionFocus()
	}
	return gfx.ID{}
}

func (state *model) paintHeadingToolbar(canvas *gfx.Canvas, theme ui.Theme, palette colors, bounds ui.Rect) {
	state.linkRect = ui.Rect{}
	if state.mode != renderedMode || !state.rich.active || state.rich.virtual || state.findOpen || state.rich.block.Kind != markdownedit.Heading && state.rich.block.Kind != markdownedit.Paragraph && state.rich.block.Kind != markdownedit.Quote && state.rich.block.Kind != markdownedit.ListItem {
		state.headingMenuOpen = false
		state.linkOpen = false
		return
	}
	if source := state.session.Current().Text(); state.rich.block.Kind == markdownedit.Paragraph {
		block := state.rich.block
		value := source[block.Start:block.End]
		selection := state.rich.input.Selection()
		selected := selection.Start != selection.End
		if selected || value == block.Visible || value == "**"+block.Visible+"**" {
			label := "加粗整段"
			if selected {
				label = "加粗选区"
			}
			labeledButton(canvas, boldBlockID, "B", label, ui.Rect{X: bounds.X + bounds.Width - 72, Y: bounds.Y + 6, Width: 25, Height: 25}, !selected && value != block.Visible, palette, state.applyBoldBlock)
		}
		if selected || value == block.Visible || value == "*"+block.Visible+"*" || value == "_"+block.Visible+"_" {
			label := "斜体整段"
			if selected {
				label = "斜体选区"
			}
			labeledButton(canvas, italicBlockID, "I", label, ui.Rect{X: bounds.X + bounds.Width - 102, Y: bounds.Y + 6, Width: 25, Height: 25}, !selected && value != block.Visible, palette, state.applyItalicBlock)
		}
		if selected || value == block.Visible || value == "~~"+block.Visible+"~~" {
			label := "删除线整段"
			if selected {
				label = "删除线选区"
			}
			labeledButton(canvas, strikeBlockID, "S", label, ui.Rect{X: bounds.X + bounds.Width - 132, Y: bounds.Y + 6, Width: 25, Height: 25}, !selected && value != block.Visible, palette, state.applyStrikeBlock)
		}
		if selected || value == block.Visible {
			label := "链接整段"
			if selected {
				label = "链接选区"
			}
			labeledButton(canvas, linkBlockID, "链接", label, ui.Rect{X: bounds.X + bounds.Width - 190, Y: bounds.Y + 6, Width: 53, Height: 25}, state.linkOpen, palette, func() {
				if state.refuseComposingFormat() {
					return
				}
				if state.linkOpen {
					state.linkOpen = false
				} else {
					state.openLinkEditor()
				}
			})
		}
	}
	trigger := ui.Rect{X: bounds.X + bounds.Width - 28, Y: bounds.Y + 6, Width: 24, Height: 25}
	labeledButton(canvas, headingMenuID, "H", "标题级别", trigger, state.headingMenuOpen, palette, func() {
		if state.refuseComposingFormat() {
			return
		}
		state.headingMenuOpen = !state.headingMenuOpen
		state.linkOpen = false
	})
	if state.linkOpen {
		state.paintLinkInput(canvas, theme, palette, bounds)
	}
	if !state.headingMenuOpen {
		return
	}
	canvas.PushLayer(8)
	box := ui.Rect{X: trigger.X - 84, Y: trigger.Y + 29, Width: 110, Height: 373}
	canvas.RoundedBorder(box, 5, 1, palette.border, palette.background)
	for level := 0; level <= 6; level++ {
		caption := "正文"
		if level > 0 {
			caption = "H" + strconv.Itoa(level)
		}
		chosen := level
		button(canvas, headingLevels.At(level), caption, ui.Rect{X: box.X + 5, Y: box.Y + 5 + float32(level)*28, Width: box.Width - 10, Height: 27}, false, palette, func() {
			state.applyHeadingLevel(chosen)
		})
	}
	for index, caption := range []string{"引用", "无序列表", "有序列表", "任务列表", "代码块", "插入表格"} {
		chosen := index
		button(canvas, blockFormats.At(index), caption, ui.Rect{X: box.X + 5, Y: box.Y + 201 + float32(index)*28, Width: box.Width - 10, Height: 27}, false, palette, func() {
			state.applyBlockFormat(chosen)
		})
	}
	canvas.PopLayer()
}

func (state *model) applyBlockFormat(option int) {
	if state.refuseComposingFormat() {
		return
	}
	state.headingMenuOpen = false
	current := state.session.Current()
	if current == nil || !state.rich.active || state.rich.virtual || option < 0 || option > 5 {
		return
	}
	if option == 4 || option == 5 {
		state.insertStructuralBlock(option)
		return
	}
	block := state.rich.block
	source := current.Text()
	if source[block.Start:block.End] != block.Visible {
		state.message = "混合格式请在 Markdown 源码模式编辑块类型"
		return
	}
	lineStart := strings.LastIndexByte(source[:block.Start], '\n') + 1
	prefix := source[lineStart:block.Start]
	indent := prefix[:len(prefix)-len(strings.TrimLeft(prefix, " \t"))]
	if block.Kind == markdownedit.Paragraph && strings.TrimSpace(prefix) != "" {
		state.message = "此块请在 Markdown 源码模式编辑"
		return
	}
	markers := []string{"> ", "- ", "1. ", "- [ ] "}
	wants := []markdownedit.Kind{markdownedit.Quote, markdownedit.ListItem, markdownedit.ListItem, markdownedit.ListItem}
	nextPrefix := indent + markers[option]
	if prefix == nextPrefix {
		return
	}
	next := source[:lineStart] + nextPrefix + source[block.Start:]
	blocks := markdownedit.Parse([]byte(next))
	if state.rich.sourceIndex >= len(blocks) || blocks[state.rich.sourceIndex].Kind != wants[option] || !blocks[state.rich.sourceIndex].Editable || blocks[state.rich.sourceIndex].Visible != block.Visible || option == 3 && !blocks[state.rich.sourceIndex].Task {
		state.message = "块类型无法安全应用；请在 Markdown 源码模式编辑"
		return
	}
	if err := current.EditRange(lineStart, block.Start, nextPrefix); err != nil {
		state.message = err.Error()
		return
	}
	state.rich.source = current.Text()
	state.rich.block = blocks[state.rich.sourceIndex]
	state.resetRenderedFormatHistory(source)
	state.focusRequest = richInputID
	state.invalidator.Invalidate()
}

func (state *model) insertStructuralBlock(option int) {
	current := state.session.Current()
	block := state.rich.block
	source := current.Text()
	if block.Kind != markdownedit.Paragraph || source[block.Start:block.End] != block.Visible {
		state.message = "此块请在 Markdown 源码模式编辑结构"
		return
	}
	newline := current.Newline()
	if option == 4 {
		lineStart := strings.LastIndexByte(source[:block.Start], '\n') + 1
		if source[lineStart:block.Start] != "" || strings.Contains(block.Visible, "```") {
			state.message = "此块无法安全转换为代码块"
			return
		}
		replacement := "```" + newline + block.Visible + newline + "```"
		next := source[:lineStart] + replacement + source[block.End:]
		blocks := markdownedit.Parse([]byte(next))
		if state.rich.sourceIndex >= len(blocks) || blocks[state.rich.sourceIndex].Kind != markdownedit.FencedCode {
			state.message = "代码块结构无效；原文未改变"
			return
		}
		if err := current.EditRange(lineStart, block.End, replacement); err != nil {
			state.message = err.Error()
			return
		}
		position := lineStart + len("```") + len(newline)
		current.Editor.MoveTo(utf8.RuneCountInString(current.Text()[:position]), false)
		state.message = "代码块已插入，请在 Markdown 源码模式编辑"
	} else {
		replacement := newline + newline + "| 列 1 | 列 2 |" + newline + "| --- | --- |" + newline + "|     |     |"
		next := source[:block.End] + replacement + source[block.End:]
		found := false
		for _, candidate := range markdownedit.Parse([]byte(next)) {
			if candidate.Kind == markdownedit.Table && candidate.Start > block.End {
				found = true
				break
			}
		}
		if !found {
			state.message = "表格结构无效；原文未改变"
			return
		}
		if err := current.EditRange(block.End, block.End, replacement); err != nil {
			state.message = err.Error()
			return
		}
		position := block.End + 2*len(newline) + 2
		current.Editor.MoveTo(utf8.RuneCountInString(current.Text()[:position]), false)
		state.message = "表格已插入，请在 Markdown 源码模式编辑单元格"
	}
	state.rich = renderedEditor{}
	state.mode = editMode
	state.focusRequest = sourceID
	state.invalidator.Invalidate()
}

func (state *model) openLinkEditor() {
	if state.refuseComposingFormat() {
		return
	}
	state.linkOpen = true
	state.headingMenuOpen = false
	state.linkEditor = text.NewEditor("")
	state.focusRequest = linkInputID
	state.invalidator.Invalidate()
}

func (state *model) paintLinkInput(canvas *gfx.Canvas, theme ui.Theme, palette colors, bounds ui.Rect) {
	width := min(float32(310), bounds.Width-16)
	box := ui.Rect{X: bounds.X + bounds.Width - width - 12, Y: bounds.Y + 36, Width: width, Height: 38}
	state.linkRect = box
	canvas.PushLayer(8)
	canvas.RoundedBorder(box, 5, 1, palette.border, palette.background)
	field := ui.Rect{X: box.X + 5, Y: box.Y + 4, Width: width - 77, Height: 29}
	inputTheme := theme
	inputTheme.Metrics.FontSizeM = 12
	value := state.linkEditor.String()
	focused := canvas.Focused() == linkInputID
	components.PaintInput(canvas, inputTheme, field, components.InputSnapshot{
		ID: linkInputID, Label: "链接地址", Value: value, Placeholder: "https://",
		Focused: focused, PlatformEditing: focused && value != "", ShowCaret: focused && value == "",
	})
	labeledButton(canvas, linkCancelID, "×", "取消链接", ui.Rect{X: field.X + field.Width + 4, Y: field.Y + 2, Width: 26, Height: 25}, false, palette, func() {
		state.linkOpen = false
		state.focusRequest = richInputID
		state.invalidator.Invalidate()
	})
	labeledButton(canvas, linkApplyID, "✓", "应用链接", ui.Rect{X: field.X + field.Width + 34, Y: field.Y + 2, Width: 27, Height: 25}, false, palette, state.applyLinkBlock)
	canvas.PopLayer()
}

func (state *model) applyLinkBlock() {
	if state.refuseComposingFormat() {
		return
	}
	current := state.session.Current()
	if current == nil || !state.rich.active || state.rich.virtual || state.rich.block.Kind != markdownedit.Paragraph {
		return
	}
	destination := strings.TrimSpace(state.linkEditor.String())
	if !markdownedit.SafeLinkURL(destination) {
		state.message = "仅支持安全的 http(s) 或 mailto 链接"
		return
	}
	block := state.rich.block
	source := current.Text()
	selection := state.rich.input.Selection()
	if selection.Start != selection.End {
		visible := state.rich.input.String()
		start := byteOffsetOfRune(visible, selection.Start)
		end := byteOffsetOfRune(visible, selection.End)
		next, err := markdownedit.LinkSelection([]byte(source), block, start, end, destination)
		if err != nil {
			state.message = "选区跨越格式边界；请在 Markdown 源码模式编辑"
			return
		}
		if err := applyRenderedSource(current, source, block, string(next)); err != nil {
			state.message = err.Error()
			return
		}
		blocks := markdownedit.Parse([]byte(current.Text()))
		if state.rich.sourceIndex >= len(blocks) || !blocks[state.rich.sourceIndex].Editable || blocks[state.rich.sourceIndex].Visible != visible {
			state.rich = renderedEditor{}
			state.mode = editMode
			state.message = "链接已应用，继续在 Markdown 源码模式编辑"
			state.linkOpen = false
			state.focusRequest = sourceID
			return
		}
		state.rich.source = current.Text()
		state.rich.block = blocks[state.rich.sourceIndex]
		state.resetRenderedFormatHistory(source)
		state.linkOpen = false
		state.focusRequest = richInputID
		state.invalidator.Invalidate()
		return
	}
	if source[block.Start:block.End] != block.Visible {
		state.message = "此段请在 Markdown 源码模式编辑链接"
		return
	}
	replacement := "[" + block.Visible + "](" + destination + ")"
	next := source[:block.Start] + replacement + source[block.End:]
	blocks := markdownedit.Parse([]byte(next))
	if state.rich.sourceIndex >= len(blocks) || !blocks[state.rich.sourceIndex].Editable || blocks[state.rich.sourceIndex].Kind != markdownedit.Paragraph || blocks[state.rich.sourceIndex].Visible != block.Visible {
		state.message = "此段无法安全创建链接；请在 Markdown 源码模式编辑"
		return
	}
	if err := current.EditRange(block.Start, block.End, replacement); err != nil {
		state.message = err.Error()
		return
	}
	state.rich.source = current.Text()
	state.rich.block = blocks[state.rich.sourceIndex]
	state.resetRenderedFormatHistory(source)
	state.linkOpen = false
	state.focusRequest = richInputID
	state.invalidator.Invalidate()
}

func (state *model) applyBoldBlock() {
	state.applyWholeBlockStyle("**")
}

func (state *model) applyItalicBlock() {
	state.applyWholeBlockStyle("*")
}

func (state *model) applyStrikeBlock() {
	state.applyWholeBlockStyle("~~")
}

func (state *model) applyWholeBlockStyle(marker string) {
	if state.refuseComposingFormat() {
		return
	}
	current := state.session.Current()
	if current == nil || !state.rich.active || state.rich.virtual || state.rich.block.Kind != markdownedit.Paragraph {
		return
	}
	selection := state.rich.input.Selection()
	if selection.Start != selection.End {
		state.applySelectedInlineStyle(marker)
		return
	}
	source := current.Text()
	block := state.rich.block
	value := source[block.Start:block.End]
	replacement := ""
	switch value {
	case block.Visible:
		replacement = marker + block.Visible + marker
	case marker + block.Visible + marker:
		replacement = block.Visible
	case "_" + block.Visible + "_":
		if marker != "*" {
			return
		}
		replacement = block.Visible
	default:
		return
	}
	next := source[:block.Start] + replacement + source[block.End:]
	blocks := markdownedit.Parse([]byte(next))
	if state.rich.sourceIndex >= len(blocks) || blocks[state.rich.sourceIndex].Kind != markdownedit.Paragraph || !blocks[state.rich.sourceIndex].Editable || blocks[state.rich.sourceIndex].Visible != block.Visible {
		state.message = "此段无法安全设置格式；请在 Markdown 源码模式编辑"
		return
	}
	if err := current.EditRange(block.Start, block.End, replacement); err != nil {
		state.message = err.Error()
		return
	}
	state.rich.source = current.Text()
	state.rich.block = blocks[state.rich.sourceIndex]
	state.resetRenderedFormatHistory(source)
	state.focusRequest = richInputID
	state.invalidator.Invalidate()
}

func (state *model) applySelectedInlineStyle(marker string) {
	current := state.session.Current()
	source := current.Text()
	block := state.rich.block
	visible := state.rich.input.String()
	selection := state.rich.input.Selection()
	start := byteOffsetOfRune(visible, selection.Start)
	end := byteOffsetOfRune(visible, selection.End)
	next, err := markdownedit.ToggleInline([]byte(current.Text()), block, start, end, marker)
	if err != nil {
		state.message = "选区跨越格式边界；请在 Markdown 源码模式编辑"
		return
	}
	if err := applyRenderedSource(current, state.rich.source, block, string(next)); err != nil {
		state.message = err.Error()
		return
	}
	blocks := markdownedit.Parse([]byte(current.Text()))
	if state.rich.sourceIndex >= len(blocks) || !blocks[state.rich.sourceIndex].Editable || blocks[state.rich.sourceIndex].Visible != visible {
		state.rich = renderedEditor{}
		state.mode = editMode
		state.message = "格式已应用，继续在 Markdown 源码模式编辑"
		state.focusRequest = sourceID
		return
	}
	state.rich.source = current.Text()
	state.rich.block = blocks[state.rich.sourceIndex]
	state.resetRenderedFormatHistory(source)
	state.focusRequest = richInputID
	state.invalidator.Invalidate()
}

func (state *model) resetRenderedInputHistory() {
	selection := state.rich.input.Selection()
	if state.rich.input.Caret() == selection.Start {
		selection.Start, selection.End = selection.End, selection.Start
	}
	state.rich.input = text.NewEditor(state.rich.block.Visible)
	state.rich.input.SetSelection(selection)
	state.rich.sourceHistoryOffset += state.rich.appliedEdits
	state.rich.appliedEdits = 0
	state.rich.undoneEdits = 0
	state.rich.pendingCross = false
	state.rich.crossUndo = nil
	state.rich.crossRedo = nil
}

func (state *model) resetRenderedFormatHistory(before string) {
	rich := &state.rich
	visible := rich.input.String()
	selection := rich.input.Selection()
	if rich.input.Caret() == selection.Start {
		selection.Start, selection.End = selection.End, selection.Start
	}
	history, redo := rich.crossUndo, rich.crossRedo
	state.resetRenderedInputHistory()
	rich.crossUndo, rich.crossRedo = history, redo
	if before == rich.source {
		return
	}
	rich.crossRedo, rich.structureRedo = nil, nil
	rich.sourceHistoryOffset++
	rich.crossUndo = appendRenderedHistory(history, renderedCrossEdit{
		beforeSourceHash: sha256.Sum256([]byte(before)), afterSourceHash: sha256.Sum256([]byte(rich.source)),
		beforeVisibleHash: sha256.Sum256([]byte(visible)), afterVisibleHash: sha256.Sum256([]byte(rich.block.Visible)),
		beforeSelect: selection, afterSelect: selection, index: rich.index, position: rich.sourceHistoryOffset,
	})
	rich.trimHistory(state.session.Current())
}

func (state *model) applyHeadingLevel(level int) {
	if state.refuseComposingFormat() {
		return
	}
	state.headingMenuOpen = false
	if !state.rich.active || level < 0 || level > 6 {
		return
	}
	current := state.session.Current()
	source := current.Text()
	block := state.rich.block
	lineStart := strings.LastIndexByte(source[:block.Start], '\n') + 1
	prefix := source[lineStart:block.Start]
	indent := prefix[:len(prefix)-len(strings.TrimLeft(prefix, " \t"))]
	if block.Kind == markdownedit.Paragraph && strings.TrimSpace(prefix) != "" {
		state.message = "此块请在 Markdown 源码模式编辑"
		return
	}
	nextPrefix := indent
	if level > 0 {
		nextPrefix += strings.Repeat("#", level) + " "
	}
	if prefix == nextPrefix {
		return
	}
	if err := current.EditRange(lineStart, block.Start, nextPrefix); err != nil {
		state.message = err.Error()
		return
	}
	blocks := markdownedit.Parse([]byte(current.Text()))
	want := markdownedit.Paragraph
	if level > 0 {
		want = markdownedit.Heading
	}
	if state.rich.sourceIndex >= len(blocks) || blocks[state.rich.sourceIndex].Kind != want || !blocks[state.rich.sourceIndex].Editable || blocks[state.rich.sourceIndex].Visible != state.rich.input.String() {
		state.rich = renderedEditor{}
		state.mode = editMode
		state.message = "标题格式已应用，继续在 Markdown 源码模式编辑"
		return
	}
	state.rich.source = current.Text()
	state.rich.block = blocks[state.rich.sourceIndex]
	state.resetRenderedFormatHistory(source)
}
