package main

import (
	"crypto/sha256"
	"strconv"
	"strings"

	"github.com/vivaui/viva/app"
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
	if target == paragraphMenuID || target == formatMenuID || target == viewMenuID || target == themeMenuID || target == markdownMenuBackdropID {
		return state.pendingFormatCompositionFocus()
	}
	switch target {
	case boldBlockID, italicBlockID, strikeBlockID, linkBlockID, headingMenuID, linkApplyID, codeLanguageApplyID:
		return state.pendingFormatCompositionFocus()
	}
	_, heading := target.In(headingLevels)
	_, structure := target.In(blockFormats)
	_, table := target.In(tableActions)
	_, task := target.In(taskChecks)
	_, codeLanguage := target.In(codeLanguageButtons)
	_, codeSuggestion := target.In(codeLanguageSuggestions)
	_, codeCopy := target.In(codeCopyButtons)
	if heading || structure || table || task || codeLanguage || codeSuggestion || codeCopy {
		return state.pendingFormatCompositionFocus()
	}
	return gfx.ID{}
}

func applyMarkdownPatch(current *session.Document, patch markdownedit.SourcePatch, before, after text.Range) error {
	return current.EditRange(patch.Start, patch.End, patch.Text, session.RangeEditSelection{Before: before, After: after})
}

func (state *model) paintHeadingToolbar(canvas *gfx.Canvas, theme ui.Theme, palette colors, bounds ui.Rect) {
	state.linkRect = ui.Rect{}
	if state.formatBarVisible() {
		if state.linkOpen {
			anchor := state.formatLinkAnchor
			width := min(float32(310), state.formatBarRect.Width-16)
			x := min(anchor.X, state.formatBarRect.X+state.formatBarRect.Width-width-12)
			state.paintLinkInput(canvas, theme, palette, ui.Rect{X: x, Y: state.formatBarRect.Y - 2, Width: width + 12})
		}
		return
	}
	if state.linkOpen && state.sourceLink.context != nil {
		state.paintLinkInput(canvas, theme, palette, bounds)
		return
	}
	if state.linkOpen && state.sourceLink.document != nil {
		if state.sourceLink.document == state.session.Current() && state.sourceRect.Width > 0 {
			state.paintLinkInput(canvas, theme, palette, state.sourceRect)
		} else {
			state.linkOpen, state.sourceLink = false, sourceLinkSelection{}
		}
		return
	}
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
	commands := [...]formatCommand{cmdQuote, cmdBullet, cmdOrdered, cmdTask, cmdCodeBlock, cmdLegacyTable}
	if option >= 0 && option < len(commands) {
		state.runFormat(commands[option])
	}
}

func (state *model) openLinkEditor() {
	if state.refuseComposingFormat() {
		return
	}
	state.linkOpen = true
	state.sourceLink = sourceLinkSelection{}
	state.headingMenuOpen = false
	state.linkEditor = text.NewEditor("")
	state.focusRequest = linkInputID
	state.invalidator.Invalidate()
}

func (state *model) openLinkShortcut() gfx.ID {
	value, _ := app.ClipboardText()
	destination := strings.TrimSpace(value)
	lower := strings.ToLower(destination)
	if (strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")) && markdownedit.SafeLinkURL(destination) {
		state.linkEditor = text.NewEditor(destination)
		before := state.session.Current().Text()
		state.applyLinkBlock()
		if before != state.session.Current().Text() {
			return state.focusRequest
		}
	}
	state.openLinkEditor()
	return state.focusRequest
}

func (state *model) prepareInlineShortcut() {
	rich := &state.rich
	if !rich.virtual || rich.virtualLiteral || rich.block.Kind != markdownedit.Paragraph || rich.block.Visible == "" || rich.input.String() != rich.block.Visible {
		return
	}
	current := state.session.Current()
	candidate, ok := renderedHistoryTarget(current, rich.index, nil)
	if !ok || candidate.block.Visible != rich.block.Visible || candidate.block.Start != rich.virtualStart {
		return
	}
	selection := rich.input.Selection()
	restoreRenderedInputSelection(candidate.input, selection, rich.input.Caret() == selection.Start)
	candidate.renderedHistory, candidate.sourceHistoryOffset = state.renderedHistorySnapshot(current)
	state.rich = candidate
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
		state.dismissLinkEditor()
	})
	labeledButton(canvas, linkApplyID, "✓", "应用链接", ui.Rect{X: field.X + field.Width + 34, Y: field.Y + 2, Width: 27, Height: 25}, false, palette, state.applyLinkBlock)
	canvas.PopLayer()
}

func (state *model) linkReturnFocus() gfx.ID {
	if state.sourceLink.context != nil {
		return state.formatFocus()
	}
	if state.sourceLink.document != nil {
		return sourceID
	}
	return richInputID
}

func (state *model) dismissLinkEditor() {
	state.focusRequest = state.linkReturnFocus()
	state.linkOpen, state.sourceLink = false, sourceLinkSelection{}
	state.invalidator.Invalidate()
}

func (state *model) applyLinkBlock() {
	if state.refuseComposingFormat() {
		return
	}
	current := state.session.Current()
	destination := strings.TrimSpace(state.linkEditor.String())
	if !markdownedit.SafeLinkURL(destination) {
		state.message = "仅支持安全的 http(s) 或 mailto 链接"
		return
	}
	if link := state.sourceLink; link.document != nil {
		if link.document == current && current.Text() == link.source {
			if link.context != nil {
				start, end := byteOffsetOfRune(link.source, link.selection.Start), byteOffsetOfRune(link.source, link.selection.End)
				value := link.source[start:end]
				if strings.ContainsAny(value, "\r\n") {
					return
				}
				if value == "" {
					value = state.local("链接", "Link")
				}
				value = "[" + strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`).Replace(value) + "](" + destination + ")"
				state.dismissLinkEditor()
				state.commitFormat(*link.context, start, end, value, start+len(value), link.context.start, link.context.block.Kind, -1)
				return
			}
			state.wrapSourceSelection(current, link.selection, "", destination)
		}
		state.dismissLinkEditor()
		return
	}
	if current == nil || !state.rich.active || state.rich.block.Kind != markdownedit.Paragraph {
		return
	}
	if state.rich.virtual {
		if start, end, ok := state.m2ParagraphRange(); ok && state.rich.input.String() == "" {
			if state.applyM2Patch(start, end, "[链接]("+destination+")", state.rich.index, 0, 0, nil) {
				state.rich.input.SetSelection(text.Range{End: state.rich.input.RuneCount()})
				state.linkOpen = false
			}
		}
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
		blocks := markdownedit.Parse(next)
		if state.rich.sourceIndex >= len(blocks) || !blocks[state.rich.sourceIndex].Editable || blocks[state.rich.sourceIndex].Visible != visible {
			state.message = "无法安全应用链接；原文未修改"
			return
		}
		if err := applyRenderedSource(current, source, block, string(next)); err != nil {
			state.message = err.Error()
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
	blocks := markdownedit.Parse(next)
	if state.rich.sourceIndex >= len(blocks) || !blocks[state.rich.sourceIndex].Editable || blocks[state.rich.sourceIndex].Visible != visible {
		state.message = "无法安全应用格式；原文未修改"
		return
	}
	if err := applyRenderedSource(current, state.rich.source, block, string(next)); err != nil {
		state.message = err.Error()
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
	if level >= 0 && level <= 6 {
		state.runFormat(cmdParagraph + formatCommand(level))
	}
}
