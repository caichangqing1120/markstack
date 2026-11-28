package main

import (
	"fmt"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"markstack/internal/icons"
)

const formatBarHeight = 38

var (
	formatBoldID           = boldBlockID
	formatItalicID         = italicBlockID
	formatStrikeID         = strikeBlockID
	formatHeadingTriggerID = headingMenuID
	formatLinkID           = linkBlockID
	formatUnderlineID      = controlTree.ID(page, "format-underline")
	formatSubscriptID      = controlTree.ID(page, "format-subscript")
	formatSuperscriptID    = controlTree.ID(page, "format-superscript")
	formatHighlightID      = controlTree.ID(page, "format-highlight")
	formatQuoteID          = controlTree.ID(page, "format-quote")
	formatBulletListID     = controlTree.ID(page, "format-bullet-list")
	formatOrderedListID    = controlTree.ID(page, "format-ordered-list")
	formatTaskListID       = controlTree.ID(page, "format-task-list")
	formatCalloutTriggerID = controlTree.ID(page, "format-callout-menu")
	formatInlineCodeID     = controlTree.ID(page, "format-inline-code")
	formatCodeBlockID      = controlTree.ID(page, "format-code-block")
	formatImageID          = controlTree.ID(page, "format-image")
	formatTableTriggerID   = controlTree.ID(page, "format-table-grid")
	formatDiagramTriggerID = controlTree.ID(page, "format-diagram-menu")
	formatMathBlockID      = controlTree.ID(page, "format-math-block")
	formatRuleID           = controlTree.ID(page, "format-rule")
	formatTOCID            = controlTree.ID(page, "format-toc")
	formatUndoID           = controlTree.ID(page, "format-undo")
	formatRedoID           = controlTree.ID(page, "format-redo")
	formatSaveID           = controlTree.ID(page, "format-save")
	formatZenID            = controlTree.ID(page, "format-zen")
)

type zenView struct {
	active, sidebar, outline bool
}

func (state *model) toggleZen() {
	if state.zen.active {
		state.sidebar, state.outline = state.zen.sidebar, state.zen.outline
		state.zen = zenView{}
		return
	}
	state.zen = zenView{active: true, sidebar: state.sidebar, outline: state.outline}
	state.sidebar, state.outline = false, false
}

func (state *model) formatBarVisible() bool {
	return state.canFormatMarkdown() && !state.preferences.HideFormatBar
}

type formatTool struct {
	id              gfx.ID
	caption, zh, en string
	key             string
	command         formatCommand
	icon            gfx.Icon
}

var (
	quoteToolIcon = gfx.Icon{ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(10, 8), gfx.LineTo(6, 8), gfx.LineTo(6, 13), gfx.LineTo(10, 13), gfx.LineTo(10, 16), gfx.LineTo(7, 18),
		gfx.MoveTo(18, 8), gfx.LineTo(14, 8), gfx.LineTo(14, 13), gfx.LineTo(18, 13), gfx.LineTo(18, 16), gfx.LineTo(15, 18),
	}}
	bulletToolIcon = gfx.Icon{ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(4, 6), gfx.LineTo(4.2, 6), gfx.MoveTo(9, 6), gfx.LineTo(20, 6),
		gfx.MoveTo(4, 12), gfx.LineTo(4.2, 12), gfx.MoveTo(9, 12), gfx.LineTo(20, 12),
		gfx.MoveTo(4, 18), gfx.LineTo(4.2, 18), gfx.MoveTo(9, 18), gfx.LineTo(20, 18),
	}}
	orderedToolIcon = gfx.Icon{ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(4, 5), gfx.LineTo(5, 5), gfx.LineTo(5, 9), gfx.MoveTo(4, 9), gfx.LineTo(6, 9),
		gfx.MoveTo(4, 15), gfx.LineTo(6, 15), gfx.MoveTo(4, 19), gfx.LineTo(6, 19),
		gfx.MoveTo(10, 6), gfx.LineTo(20, 6), gfx.MoveTo(10, 12), gfx.LineTo(20, 12), gfx.MoveTo(10, 18), gfx.LineTo(20, 18),
	}}
	taskToolIcon = gfx.Icon{ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(3.5, 6.5), gfx.LineTo(5.5, 8.5), gfx.LineTo(8.5, 5),
		gfx.MoveTo(12, 6), gfx.LineTo(20, 6),
		gfx.MoveTo(4, 14), gfx.LineTo(8, 14), gfx.LineTo(8, 18), gfx.LineTo(4, 18), gfx.ClosePath(),
		gfx.MoveTo(12, 16), gfx.LineTo(20, 16),
	}}
	calloutToolIcon = gfx.Icon{ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(5, 4), gfx.LineTo(19, 4), gfx.LineTo(19, 20), gfx.LineTo(5, 20), gfx.ClosePath(),
		gfx.MoveTo(12, 8), gfx.LineTo(12, 13), gfx.MoveTo(12, 16.5), gfx.LineTo(12.1, 16.5),
	}}
	codeBlockToolIcon = gfx.Icon{ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(4, 4), gfx.LineTo(20, 4), gfx.LineTo(20, 20), gfx.LineTo(4, 20), gfx.ClosePath(),
		gfx.MoveTo(10, 9), gfx.LineTo(7, 12), gfx.LineTo(10, 15),
		gfx.MoveTo(14, 9), gfx.LineTo(17, 12), gfx.LineTo(14, 15),
	}}
	linkToolIcon = gfx.Icon{ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(10, 13), gfx.CubicTo(11.5, 14.8, 14.2, 14.8, 16, 13), gfx.LineTo(19, 10), gfx.CubicTo(21, 8, 21, 5, 19, 3.5), gfx.CubicTo(17, 2, 14.5, 2.5, 13, 4), gfx.LineTo(11.5, 5.5),
		gfx.MoveTo(14, 11), gfx.CubicTo(12.5, 9.2, 9.8, 9.2, 8, 11), gfx.LineTo(5, 14), gfx.CubicTo(3, 16, 3, 19, 5, 20.5), gfx.CubicTo(7, 22, 9.5, 21.5, 11, 20), gfx.LineTo(12.5, 18.5),
	}}
	imageToolIcon = gfx.Icon{ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(4, 4), gfx.LineTo(20, 4), gfx.LineTo(20, 20), gfx.LineTo(4, 20), gfx.ClosePath(),
		gfx.MoveTo(8.5, 8.5), gfx.LineTo(8.7, 8.5),
		gfx.MoveTo(20, 15), gfx.LineTo(15, 10), gfx.LineTo(6, 19),
	}}
	tableToolIcon = gfx.Icon{ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(3, 4), gfx.LineTo(21, 4), gfx.LineTo(21, 20), gfx.LineTo(3, 20), gfx.ClosePath(),
		gfx.MoveTo(3, 10), gfx.LineTo(21, 10), gfx.MoveTo(3, 15), gfx.LineTo(21, 15),
		gfx.MoveTo(9, 4), gfx.LineTo(9, 20), gfx.MoveTo(15, 4), gfx.LineTo(15, 20),
	}}
	diagramToolIcon = gfx.Icon{ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(4, 4), gfx.LineTo(4, 20), gfx.LineTo(20, 20),
		gfx.MoveTo(7, 15), gfx.LineTo(11, 10), gfx.LineTo(14.5, 13.5), gfx.LineTo(19, 8),
	}}
	undoToolIcon = gfx.Icon{ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(9, 14), gfx.LineTo(4, 9), gfx.LineTo(9, 4),
		gfx.MoveTo(4, 9), gfx.LineTo(15, 9), gfx.CubicTo(18.5, 9, 20, 11, 20, 14), gfx.CubicTo(20, 17, 18.5, 19, 15, 19), gfx.LineTo(12, 19),
	}}
	redoToolIcon = gfx.Icon{ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(15, 14), gfx.LineTo(20, 9), gfx.LineTo(15, 4),
		gfx.MoveTo(20, 9), gfx.LineTo(9, 9), gfx.CubicTo(5.5, 9, 4, 11, 4, 14), gfx.CubicTo(4, 17, 5.5, 19, 9, 19), gfx.LineTo(12, 19),
	}}
	zenToolIcon = gfx.Icon{ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(15, 4), gfx.LineTo(20, 4), gfx.LineTo(20, 9), gfx.MoveTo(20, 4), gfx.LineTo(14, 10),
		gfx.MoveTo(9, 20), gfx.LineTo(4, 20), gfx.LineTo(4, 15), gfx.MoveTo(4, 20), gfx.LineTo(10, 14),
	}}
)

var formatGroups = [][]formatTool{
	{
		{id: formatBoldID, caption: "B", zh: "加粗", en: "Bold", key: "B", command: cmdBold},
		{id: formatUnderlineID, caption: "U", zh: "下划线", en: "Underline", key: "U", command: cmdUnderline},
		{id: formatItalicID, caption: "I", zh: "斜体", en: "Italic", key: "I", command: cmdItalic},
		{id: formatStrikeID, caption: "S", zh: "删除线", en: "Strikethrough", key: "⇧X", command: cmdStrike},
		{id: formatSubscriptID, caption: "X₂", zh: "下标", en: "Subscript", command: cmdSubscript},
		{id: formatSuperscriptID, caption: "X²", zh: "上标", en: "Superscript", command: cmdSuperscript},
		{id: formatHighlightID, caption: "==", zh: "高亮", en: "Highlight", key: "⇧H", command: cmdHighlight},
	},
	{
		{id: formatHeadingTriggerID, caption: "H▾", zh: "标题级别", en: "Heading level"},
		{id: formatQuoteID, zh: "引用", en: "Block quote", key: "⌥Q", command: cmdQuote, icon: quoteToolIcon},
		{id: formatBulletListID, zh: "无序列表", en: "Bullet list", key: "⌥U", command: cmdBullet, icon: bulletToolIcon},
		{id: formatOrderedListID, zh: "有序列表", en: "Ordered list", key: "⌥O", command: cmdOrdered, icon: orderedToolIcon},
		{id: formatTaskListID, zh: "任务列表", en: "Task list", key: "⌥X", command: cmdTask, icon: taskToolIcon},
		{id: formatCalloutTriggerID, zh: "警告框", en: "Callout", icon: calloutToolIcon},
	},
	{
		{id: formatInlineCodeID, zh: "行内代码", en: "Inline code", key: "`", command: cmdInlineCode, icon: icons.CodeXml},
		{id: formatCodeBlockID, zh: "代码块", en: "Code block", key: "⌥C", command: cmdCodeBlock, icon: codeBlockToolIcon},
		{id: formatLinkID, zh: "超链接", en: "Link", key: "K", command: cmdLink, icon: linkToolIcon},
		{id: formatImageID, zh: "插入本地图片", en: "Insert local image", key: "⇧I", command: cmdImage, icon: imageToolIcon},
		{id: formatTableTriggerID, zh: "插入表格", en: "Insert table", icon: tableToolIcon},
		{id: formatDiagramTriggerID, zh: "Mermaid 图表", en: "Mermaid diagram", icon: diagramToolIcon},
		{id: formatMathBlockID, caption: "Σ", zh: "公式块", en: "Math block", key: "⌥B", command: cmdMathBlock},
		{id: formatRuleID, caption: "—", zh: "水平分割线", en: "Horizontal rule", key: "⌥-", command: cmdRule},
		{id: formatTOCID, zh: "插入内容目录", en: "Insert table of contents", command: cmdTOC, icon: icons.ListTree},
	},
	{
		{id: formatUndoID, zh: "撤销", en: "Undo", key: "Z", command: cmdUndo, icon: undoToolIcon},
		{id: formatRedoID, zh: "重做", en: "Redo", key: "⇧Z", command: cmdRedo, icon: redoToolIcon},
		{id: formatSaveID, zh: "保存", en: "Save", key: "S", command: cmdSave, icon: icons.Save},
	},
}

func (state *model) paintFormattingToolbar(canvas *gfx.Canvas, palette colors, bounds ui.Rect) {
	state.formatBarRect = bounds
	canvas.Fill(bounds, palette.bar)
	canvas.Fill(ui.Rect{X: bounds.X, Y: bounds.Y + bounds.Height - 1, Width: bounds.Width, Height: 1}, palette.border)
	const padding, separator, buttonHeight = 10, 10, 28
	unit := min(float32(32), (bounds.Width-2*padding-3*separator-buttonHeight-6)/27)
	left := bounds.X + padding
	for group, tools := range formatGroups {
		if group > 0 {
			canvas.Fill(ui.Rect{X: left + (separator-1)/2.0, Y: bounds.Y + 9, Width: 1, Height: 20}, palette.border)
			left += separator
		}
		for _, tool := range tools {
			width := unit
			switch tool.id {
			case formatHeadingTriggerID:
				width += unit * .9
				if level := state.currentFormatHeading(); level > 0 {
					tool.caption = fmt.Sprintf("H%d▾", level)
				} else if unit >= 23 {
					tool.caption = state.local("正文▾", "Text▾")
				}
			case formatCodeBlockID, formatTOCID:
				width += unit * .4
			}
			rect := ui.Rect{X: left, Y: bounds.Y + 5, Width: width, Height: buttonHeight}
			left += width
			active := state.formatMenu.trigger == tool.id || tool.id == formatLinkID && state.linkOpen
			if tool.id == formatLinkID {
				state.formatLinkAnchor = rect
			}
			state.paintFormatTool(canvas, palette, tool, rect, active)
		}
	}
	state.paintFormatTool(canvas, palette, formatTool{id: formatZenID, icon: zenToolIcon, zh: "沉浸写作", en: "Distraction-free writing"}, ui.Rect{
		X: bounds.X + bounds.Width - padding - buttonHeight, Y: bounds.Y + 5, Width: buttonHeight, Height: buttonHeight,
	}, state.zen.active)
}

func (state *model) paintFormatTool(canvas *gfx.Canvas, palette colors, tool formatTool, bounds ui.Rect, active bool) {
	label := state.local(tool.zh, tool.en)
	if tool.key != "" {
		label += " · " + shortcutLabel(tool.key)
	}
	ink := palette.ink
	if active || canvas.Hovered() == tool.id {
		canvas.Rounded(bounds, 5, palette.selected)
		ink = palette.accent
	}
	if tool.caption == "" {
		canvas.DrawIcon(tool.icon, ui.Rect{X: bounds.X + (bounds.Width-14)/2, Y: bounds.Y + (bounds.Height-14)/2, Width: 14, Height: 14}, ink)
	} else {
		style := gfx.TextStyle{Size: 12, Weight: gfx.WeightMedium, Color: ink, Italic: tool.id == formatItalicID}
		if tool.id == formatBoldID {
			style.Weight = gfx.WeightBold
		}
		canvas.LabelCenter(tool.caption, bounds, style)
		if tool.id == formatUnderlineID || tool.id == formatStrikeID {
			y := bounds.Y + 20
			if tool.id == formatStrikeID {
				y = bounds.Y + 13
			}
			canvas.Fill(ui.Rect{X: bounds.X + (bounds.Width-9)/2, Y: y, Width: 9, Height: 1}, ink)
		}
	}
	canvas.HitWithOptions(tool.id, bounds, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: label, SkipFocus: true, Selected: active})
	if canvas.Clicked(tool.id) {
		canvas.AfterPaint(func() {
			if state.refuseComposingFormat() {
				return
			}
			switch tool.id {
			case formatHeadingTriggerID, formatCalloutTriggerID, formatTableTriggerID, formatDiagramTriggerID:
				state.toggleMarkdownMenu(tool.id, bounds)
			case formatZenID:
				state.toggleZen()
			default:
				state.runFormat(tool.command)
			}
		})
	}
}
