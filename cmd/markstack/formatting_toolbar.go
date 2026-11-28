package main

import (
	"fmt"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"markstack/internal/icons"
)

const formatBarHeight float32 = 34

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
		{id: formatQuoteID, caption: "❝", zh: "引用", en: "Block quote", key: "⌥Q", command: cmdQuote},
		{id: formatBulletListID, caption: "•≡", zh: "无序列表", en: "Bullet list", key: "⌥U", command: cmdBullet},
		{id: formatOrderedListID, caption: "1≡", zh: "有序列表", en: "Ordered list", key: "⌥O", command: cmdOrdered},
		{id: formatTaskListID, caption: "☑", zh: "任务列表", en: "Task list", key: "⌥X", command: cmdTask},
		{id: formatCalloutTriggerID, caption: "[!]", zh: "警告框", en: "Callout"},
	},
	{
		{id: formatInlineCodeID, zh: "行内代码", en: "Inline code", key: "`", command: cmdInlineCode, icon: icons.CodeXml},
		{id: formatCodeBlockID, caption: "[<>]", zh: "代码块", en: "Code block", key: "⌥C", command: cmdCodeBlock},
		{id: formatLinkID, caption: "↗", zh: "超链接", en: "Link", key: "K", command: cmdLink},
		{id: formatImageID, caption: "▧", zh: "插入本地图片", en: "Insert local image", key: "⇧I", command: cmdImage},
		{id: formatTableTriggerID, caption: "▦", zh: "插入表格", en: "Insert table"},
		{id: formatDiagramTriggerID, caption: "◇", zh: "Mermaid 图表", en: "Mermaid diagram"},
		{id: formatMathBlockID, caption: "Σ", zh: "公式块", en: "Math block", key: "⌥B", command: cmdMathBlock},
		{id: formatRuleID, caption: "—", zh: "水平分割线", en: "Horizontal rule", key: "⌥-", command: cmdRule},
		{id: formatTOCID, caption: "TOC", zh: "插入内容目录", en: "Insert table of contents", command: cmdTOC},
	},
	{
		{id: formatUndoID, caption: "↶", zh: "撤销", en: "Undo", key: "Z", command: cmdUndo},
		{id: formatRedoID, caption: "↷", zh: "重做", en: "Redo", key: "⇧Z", command: cmdRedo},
		{id: formatSaveID, zh: "保存", en: "Save", key: "S", command: cmdSave, icon: icons.Save},
	},
}

func (state *model) paintFormattingToolbar(canvas *gfx.Canvas, palette colors, bounds ui.Rect) {
	state.formatBarRect = bounds
	canvas.Fill(bounds, palette.bar)
	canvas.Fill(ui.Rect{X: bounds.X, Y: bounds.Y + bounds.Height - 1, Width: bounds.Width, Height: 1}, palette.border)
	const padding, separator = float32(7), float32(7)
	unit := min(float32(29), (bounds.Width-2*padding-3*separator-32)/27)
	left := bounds.X + padding
	for group, tools := range formatGroups {
		if group > 0 {
			canvas.Fill(ui.Rect{X: left + 3, Y: bounds.Y + 8, Width: 1, Height: 18}, palette.border)
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
			rect := ui.Rect{X: left, Y: bounds.Y + 4, Width: width, Height: 26}
			left += width
			active := state.formatMenu.trigger == tool.id || tool.id == formatLinkID && state.linkOpen
			if tool.id == formatLinkID {
				state.formatLinkAnchor = rect
			}
			state.paintFormatTool(canvas, palette, tool, rect, active)
		}
	}
	state.paintFormatTool(canvas, palette, formatTool{id: formatZenID, caption: "⤢", zh: "沉浸写作", en: "Distraction-free writing"}, ui.Rect{
		X: bounds.X + bounds.Width - padding - 26, Y: bounds.Y + 4, Width: 26, Height: 26,
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
		canvas.DrawIcon(tool.icon, ui.Rect{X: bounds.X + (bounds.Width-14)/2, Y: bounds.Y + 6, Width: 14, Height: 14}, ink)
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
