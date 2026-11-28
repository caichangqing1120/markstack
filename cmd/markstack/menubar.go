package main

import (
	"fmt"
	"strconv"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"markstack/internal/markdownedit"
	"markstack/internal/session"
)

var (
	paragraphMenuID        = controlTree.ID(page, "paragraph-menu")
	formatMenuID           = controlTree.ID(page, "format-menu")
	viewMenuID             = controlTree.ID(page, "view-menu")
	themeMenuID            = controlTree.ID(page, "theme-menu")
	markdownMenuScope      = controlTree.ID(page, "markdown-menu-scope")
	markdownMenuBackdropID = controlTree.ID(page, "markdown-menu-backdrop")
	paragraphItems         = controlTree.Sub(page, controlTree.Name("paragraph-menu-item"))
	inlineFormatItems      = controlTree.Sub(page, controlTree.Name("format-menu-item"))
	viewMenuItems          = controlTree.Sub(page, controlTree.Name("view-menu-item"))
	themeMenuItems         = controlTree.Sub(page, controlTree.Name("theme-menu-item"))
	calloutItems           = controlTree.Sub(page, controlTree.Name("callout-item"))
	diagramItems           = controlTree.Sub(page, controlTree.Name("diagram-item"))
	tableGridCells         = controlTree.Sub(page, controlTree.Name("table-grid-cell"))
	tableMenuItems         = controlTree.Sub(page, controlTree.Name("table-menu-item"))
)

type markdownMenu struct {
	trigger   gfx.ID
	anchor    ui.Rect
	previous  gfx.ID
	scroll    components.MenuScroll
	gridIndex int
}

type markdownMenuItem struct {
	option components.DropdownMenuItem
	action func()
}

func (item markdownMenuItem) checked(active bool) markdownMenuItem {
	item.option.Kind = components.DropdownMenuCheckbox
	item.option.Checked = active
	return item
}

func (state *model) paintTopMenuTriggers(canvas *gfx.Canvas, palette colors, next func(float32) ui.Rect, width float32, compact bool) {
	for _, trigger := range []struct {
		id           gfx.ID
		zh, en, icon string
	}{
		{paragraphMenuID, "段落", "Paragraph", "¶"},
		{formatMenuID, "格式", "Format", "Aa"},
		{viewMenuID, "显示", "View", "▣"},
		{themeMenuID, "主题", "Themes", "◐"},
	} {
		bounds := next(width)
		label := state.local(trigger.zh, trigger.en)
		caption := label
		if compact {
			caption = trigger.icon
		}
		active := state.formatMenu.trigger == trigger.id
		if active || canvas.Hovered() == trigger.id {
			canvas.Rounded(bounds, 5, palette.selected)
		}
		canvas.LabelCenter(ellipsizeWeight(canvas, caption, width-4, palette.textSize(12), gfx.WeightMedium), bounds, gfx.TextStyle{Size: palette.textSize(12), Weight: gfx.WeightMedium, Color: palette.ink})
		if canvas.FocusVisible() == trigger.id {
			canvas.RoundedBorder(bounds, 5, 1, palette.accent, ui.Color{})
		}
		canvas.HitWithOptions(trigger.id, bounds, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: label, Selected: active})
		if canvas.Clicked(trigger.id) {
			canvas.AfterPaint(func() { state.toggleMarkdownMenu(trigger.id, bounds) })
		}
	}
}

func (state *model) toggleMarkdownMenu(trigger gfx.ID, anchor ui.Rect) {
	if state.refuseComposingFormat() {
		return
	}
	if state.formatMenu.trigger == trigger {
		state.dismissMarkdownMenu()
		return
	}
	state.fileMenuOpen, state.headingMenuOpen = false, false
	if state.linkOpen {
		state.dismissLinkEditor()
	}
	state.slash = slashMenu{}
	state.formatMenu = markdownMenu{trigger: trigger, anchor: anchor, previous: state.formatFocus()}
	state.invalidator.Invalidate()
}

func (state *model) dismissMarkdownMenu() gfx.ID {
	focus := state.formatMenu.previous
	if focus.IsZero() {
		focus = state.formatMenu.trigger
	}
	state.formatMenu = markdownMenu{}
	state.focusRequest = focus
	state.invalidator.Invalidate()
	return focus
}

func (state *model) onMarkdownMenuKey(event gfx.KeyEvent) gfx.EventResult {
	if state.formatMenu.trigger.IsZero() || event.Release || !state.pendingFormatCompositionFocus().IsZero() {
		return gfx.EventResult{}
	}
	if event.Key == gfx.KeyEscape {
		return gfx.EventResult{Handled: true, Repaint: true, Focus: state.dismissMarkdownMenu()}
	}
	if state.formatMenu.trigger == formatTableTriggerID {
		index := state.formatMenu.gridIndex
		row, column := index/6, index%6
		switch event.Key {
		case gfx.KeyUp:
			row = max(0, row-1)
		case gfx.KeyDown:
			row = min(5, row+1)
		case gfx.KeyLeft:
			column = max(0, column-1)
		case gfx.KeyRight:
			column = min(5, column+1)
		default:
			return gfx.EventResult{}
		}
		state.formatMenu.gridIndex = row*6 + column
		return gfx.EventResult{Handled: true, Repaint: true, Focus: tableGridCells.At(state.formatMenu.gridIndex)}
	}
	if target, handled := state.formatMenu.scroll.Navigate(event.Key, event.Target, true); handled {
		return gfx.EventResult{Handled: true, Repaint: true, Focus: target}
	}
	return gfx.EventResult{}
}

func (state *model) formatMenuItem(id gfx.ID, zh, en, key string, command formatCommand) markdownMenuItem {
	if key != "" && key != "Tab" && key != "⇧Tab" {
		key = shortcutLabel(key)
	}
	return markdownMenuItem{components.DropdownMenuItem{ID: id, Label: state.local(zh, en), Shortcut: key, Disabled: !state.canFormatMarkdown()}, func() { state.runFormat(command) }}
}

func (state *model) headingMenuItems(legacy bool) []markdownMenuItem {
	var items []markdownMenuItem
	level := state.currentFormatHeading()
	for index, label := range []string{"正文", "一级标题", "二级标题", "三级标题", "四级标题", "五级标题", "六级标题"} {
		id := paragraphItems.At(index)
		if legacy {
			id = headingLevels.At(index)
		}
		en := "Paragraph"
		if index > 0 {
			label += fmt.Sprintf(" H%d", index)
			en = fmt.Sprintf("Heading %d", index)
		}
		items = append(items, state.formatMenuItem(id, label, en, strconv.Itoa(index), cmdParagraph+formatCommand(index)).checked(level == index))
	}
	return append(items, state.formatMenuItem(paragraphItems.At(7), "提升标题级别", "Promote heading", "⌥]", cmdPromote), state.formatMenuItem(paragraphItems.At(8), "降低标题级别", "Demote heading", "⌥[", cmdDemote))
}

func (state *model) calloutMenuItems() []markdownMenuItem {
	var items []markdownMenuItem
	for index, zh := range []string{"提示", "技巧", "重要", "警告", "注意"} {
		name := formatCallouts[index]
		items = append(items, state.formatMenuItem(calloutItems.At(index), zh+" [!"+name+"]", "Callout [!"+name+"]", "", cmdNote+formatCommand(index)))
	}
	return items
}

func (state *model) legacyHeadingItems() []markdownMenuItem {
	items := append(state.headingMenuItems(true), markdownMenuItem{})
	for index, item := range []struct {
		zh, en  string
		command formatCommand
	}{{"引用", "Block quote", cmdQuote}, {"无序列表", "Bullet list", cmdBullet}, {"有序列表", "Ordered list", cmdOrdered}, {"任务列表", "Task list", cmdTask}, {"代码块", "Code block", cmdCodeBlock}, {"插入表格", "Insert table", cmdLegacyTable}} {
		items = append(items, state.formatMenuItem(blockFormats.At(index), item.zh, item.en, "", item.command))
	}
	return items
}

func (state *model) paragraphMenuItems() []markdownMenuItem {
	items := append(state.headingMenuItems(false), markdownMenuItem{}, state.formatMenuItem(paragraphItems.At(9), "插入表格 (3×3)", "Insert table (3×3)", "", cmdTable))
	var context formatContext
	if state.canFormatMarkdown() {
		context = state.formatContext()
	}
	_, _, canEdit := state.formatTableCell(context)
	for index, option := range []struct {
		zh, en string
		action markdownedit.TableAction
	}{
		{"在上方插入表格行", "Insert table row above", markdownedit.RowAbove}, {"在下方插入表格行", "Insert table row below", markdownedit.RowBelow},
		{"在左侧插入表格列", "Insert table column left", markdownedit.ColumnLeft}, {"在右侧插入表格列", "Insert table column right", markdownedit.ColumnRight},
		{"删除当前表格行", "Delete table row", markdownedit.RowDelete}, {"删除当前表格列", "Delete table column", markdownedit.ColumnDelete},
		{"表格列左对齐", "Align table column left", markdownedit.AlignLeft}, {"表格列居中", "Center table column", markdownedit.AlignCenter},
		{"表格列右对齐", "Align table column right", markdownedit.AlignRight}, {"删除整张表格", "Delete table", markdownedit.TableDelete},
		{"格式化整张表格", "Format table", markdownedit.TableFormat},
	} {
		items = append(items, markdownMenuItem{components.DropdownMenuItem{ID: tableMenuItems.At(index), Label: state.local(option.zh, option.en), Disabled: !canEdit}, func() { state.runFormatTableAction(option.action) }})
	}
	items = append(items, markdownMenuItem{}, state.formatMenuItem(paragraphItems.At(10), "公式块", "Math block", "⌥B", cmdMathBlock), state.formatMenuItem(paragraphItems.At(11), "代码块", "Code block", "⌥C", cmdCodeBlock))
	items = append(items, state.calloutMenuItems()...)
	items = append(items, markdownMenuItem{})
	for index, option := range []struct {
		zh, en, key string
		command     formatCommand
	}{
		{"引用", "Block quote", "⌥Q", cmdQuote}, {"有序列表", "Ordered list", "⌥O", cmdOrdered},
		{"无序列表", "Bullet list", "⌥U", cmdBullet}, {"任务列表", "Task list", "⌥X", cmdTask},
		{"切换任务完成状态", "Toggle completed task", "⇧T", cmdToggleTask}, {"增加列表缩进", "Indent list", "Tab", cmdIndent}, {"减少列表缩进", "Outdent list", "⇧Tab", cmdOutdent},
		{}, {"在上方插入段落", "Insert paragraph above", "", cmdAbove}, {"在下方插入段落", "Insert paragraph below", "", cmdBelow},
		{"水平分割线", "Horizontal rule", "⌥-", cmdRule}, {"内容目录 [TOC]", "Table of contents [TOC]", "", cmdTOC},
		{"YAML Front Matter", "YAML Front Matter", "", cmdFrontMatter}, {"插入脚注 [^1]", "Insert footnote [^1]", "", cmdFootnote},
	} {
		if option.zh == "" {
			items = append(items, markdownMenuItem{})
			continue
		}
		item := state.formatMenuItem(paragraphItems.At(12+index), option.zh, option.en, option.key, option.command)
		if option.command == cmdToggleTask {
			item.option.Disabled = !context.block.Task
		} else if option.command == cmdIndent || option.command == cmdOutdent {
			item.option.Disabled = context.block.List == nil
		}
		items = append(items, item)
	}
	return items
}

func (state *model) inlineFormatMenuItems() []markdownMenuItem {
	var items []markdownMenuItem
	for index, option := range []struct {
		zh, en, key string
		command     formatCommand
	}{
		{"加粗", "Bold", "B", cmdBold}, {"斜体", "Italic", "I", cmdItalic}, {"下划线", "Underline", "U", cmdUnderline},
		{"删除线", "Strikethrough", "⇧X", cmdStrike}, {"行内代码", "Inline code", "`", cmdInlineCode}, {"行内公式 ($…$)", "Inline math ($…$)", "", cmdInlineMath},
		{"高亮 (==…==)", "Highlight (==…==)", "⇧H", cmdHighlight}, {"上标", "Superscript", "", cmdSuperscript}, {"下标", "Subscript", "", cmdSubscript},
		{"超链接", "Link", "K", cmdLink}, {"插入图片…", "Insert image…", "⇧I", cmdImage}, {}, {"清除格式", "Clear formatting", "\\", cmdClear},
	} {
		if option.zh == "" {
			items = append(items, markdownMenuItem{})
		} else {
			items = append(items, state.formatMenuItem(inlineFormatItems.At(index), option.zh, option.en, option.key, option.command))
		}
	}
	return items
}

func (state *model) viewMenuOptions() []markdownMenuItem {
	item := func(index int, zh, en, key string, action func()) markdownMenuItem {
		return markdownMenuItem{components.DropdownMenuItem{ID: viewMenuItems.At(index), Label: state.local(zh, en), Shortcut: key}, action}
	}
	var items []markdownMenuItem
	for index, option := range []struct {
		mode   viewMode
		zh, en string
	}{{renderedMode, "排版模式", "Rendered mode"}, {splitMode, "分栏模式", "Split mode"}, {editMode, "源码模式", "Source mode"}, {readMode, "阅读模式", "Read mode"}} {
		entry := item(index, option.zh, option.en, "", func() { state.setViewMode(option.mode) }).checked(state.mode == option.mode)
		entry.option.Disabled = !state.canUseViewMode(option.mode)
		items = append(items, entry)
	}
	return append(items, markdownMenuItem{},
		item(4, "显示侧边栏", "Show sidebar", "", func() { state.sidebar = !state.sidebar }).checked(state.sidebar),
		item(5, "显示格式工具栏", "Show formatting toolbar", "", func() {
			state.changeFormatPreference(func(next *session.Preferences) { next.HideFormatBar = !next.HideFormatBar })
		}).checked(!state.preferences.HideFormatBar),
		item(6, "显示大纲与反向链接", "Show outline and backlinks", "", func() { state.outline = !state.outline }).checked(state.outline),
		markdownMenuItem{},
		item(7, "打字机模式", "Typewriter mode", shortcutLabel("⌥T"), func() { state.toggleWritingMode(true) }).checked(state.preferences.TypewriterMode),
		item(8, "专注模式", "Focus mode", shortcutLabel("⌥D"), func() { state.toggleWritingMode(false) }).checked(state.preferences.FocusMode),
		markdownMenuItem{},
		item(9, "增大字号", "Increase font size", shortcutLabel("="), func() { state.zoomFont(2) }),
		item(10, "减小字号", "Decrease font size", shortcutLabel("-"), func() { state.zoomFont(-2) }),
		item(11, "重置默认字号", "Reset font size", shortcutLabel("⌥0"), func() { state.zoomFont(0) }),
	)
}

func (state *model) changeFormatPreference(update func(*session.Preferences)) {
	next := state.preferences
	update(&next)
	state.changeSettings(next)
}

func (state *model) themeMenuOptions() []markdownMenuItem {
	makeItem := func(index int, zh, en string, active bool, update func(*session.Preferences)) markdownMenuItem {
		return (markdownMenuItem{components.DropdownMenuItem{ID: themeMenuItems.At(index), Label: state.local(zh, en)}, func() { state.changeFormatPreference(update) }}).checked(active)
	}
	items := []markdownMenuItem{
		makeItem(0, "浅色外观", "Light appearance", !state.dark, func(next *session.Preferences) { next.Dark = false }),
		makeItem(1, "深色外观", "Dark appearance", state.dark, func(next *session.Preferences) { next.Dark = true }), {},
	}
	for index, theme := range []struct{ key, zh, en string }{
		{"standard", "标准", "Standard"}, {"soft", "柔和", "Soft"}, {"github", "GitHub", "GitHub"},
		{"parchment", "羊皮纸", "Parchment"}, {"academic", "典雅", "Academic"}, {"nord", "Nord", "Nord"},
	} {
		items = append(items, makeItem(index+2, theme.zh, theme.en, state.preferences.ReadingTheme == theme.key, func(next *session.Preferences) { next.ReadingTheme = theme.key }))
	}
	return items
}

func (state *model) paintMarkdownMenu(canvas *gfx.Canvas, theme ui.Theme, palette colors, viewport ui.Rect) {
	popup := &state.formatMenu
	var items []markdownMenuItem
	switch popup.trigger {
	case paragraphMenuID:
		items = state.paragraphMenuItems()
	case formatMenuID:
		items = state.inlineFormatMenuItems()
	case viewMenuID:
		items = state.viewMenuOptions()
	case themeMenuID:
		items = state.themeMenuOptions()
	case formatHeadingTriggerID:
		items = state.legacyHeadingItems()
	case formatCalloutTriggerID:
		items = state.calloutMenuItems()
	case formatDiagramTriggerID:
		items = []markdownMenuItem{state.formatMenuItem(diagramItems.At(0), "流程图 Flowchart", "Flowchart", "", cmdFlowchart), state.formatMenuItem(diagramItems.At(1), "时序图 Sequence", "Sequence diagram", "", cmdSequence)}
	case formatTableTriggerID:
		state.paintFormatTableGrid(canvas, palette, viewport)
		return
	default:
		return
	}
	const rowHeight = 25
	theme.Metrics.FontSizeM = state.uiSize(13)
	width, height := float32(172), 2*float32(components.DropdownMenuPadding)
	for _, item := range items {
		if item.action == nil {
			height += components.DropdownMenuSeparatorHeight
		} else {
			height += rowHeight
			width = max(width, components.DropdownMenuItemWidth(canvas, theme, item.option))
		}
	}
	anchor := popup.anchor
	y := anchor.Y + anchor.Height + 3
	box := ui.Rect{X: min(anchor.X, viewport.Width-width-4), Y: y, Width: width, Height: min(height, viewport.Height-y-5)}
	options := components.DropdownMenuOptions{ID: popup.trigger, Layer: 9, Scope: markdownMenuScope, DismissID: markdownMenuBackdropID, Viewport: viewport, Scroll: &popup.scroll, RowHeight: rowHeight}
	if popup.trigger == formatHeadingTriggerID {
		options.DismissID = gfx.ID{}
		options.Scope = gfx.ID{}
	}
	if !state.pendingFormatCompositionFocus().IsZero() {
		options.Scope = gfx.ID{}
	}
	menu := components.BeginDropdownMenu(canvas, theme, box, options)
	defer menu.End()
	if canvas.Clicked(markdownMenuBackdropID) {
		canvas.AfterPaint(func() { state.dismissMarkdownMenu() })
	}
	var first gfx.ID
	for _, item := range items {
		if item.action == nil {
			menu.AddSeparator()
			continue
		}
		menu.AddItem(item.option)
		if first.IsZero() && !item.option.Disabled {
			first = item.option.ID
		}
		if !item.option.Disabled && canvas.Clicked(item.option.ID) {
			canvas.AfterPaint(func() {
				if !state.refuseComposingFormat() {
					state.dismissMarkdownMenu()
					item.action()
				}
			})
		}
	}
	if !options.Scope.IsZero() && !first.IsZero() {
		canvas.PushFocusScope(options.Scope)
		canvas.SetInitialFocus(first)
		canvas.PopFocusScope()
	}
}

func (state *model) paintFormatTableGrid(canvas *gfx.Canvas, palette colors, viewport ui.Rect) {
	const pitch, pad = 25, 9
	const width, height = 6*pitch + 2*pad, 6*pitch + 2*pad + 20
	popup := &state.formatMenu
	box := ui.Rect{X: min(popup.anchor.X, viewport.Width-width-5), Y: popup.anchor.Y + popup.anchor.Height + 3, Width: width, Height: height}
	canvas.PushLayer(9)
	defer canvas.PopLayer()
	canvas.HitWithOptions(markdownMenuBackdropID, viewport, state.local("关闭菜单", "Close menu"), gfx.HitOptions{SkipFocus: true})
	canvas.Block(box)
	canvas.Elevate(box, 7, gfx.ShadowSM)
	canvas.RoundedBorder(box, 7, 1, palette.border, palette.background)
	if canvas.Clicked(markdownMenuBackdropID) {
		canvas.AfterPaint(func() { state.dismissMarkdownMenu() })
	}
	canvas.PushFocusScope(markdownMenuScope)
	defer canvas.PopFocusScope()
	canvas.SetInitialFocus(tableGridCells.At(0))
	index := popup.gridIndex
	if hovered, ok := canvas.Hovered().In(tableGridCells); ok && hovered < 36 {
		index = hovered
	}
	canvas.LabelCenter(fmt.Sprintf("%d × %d", index/6+1, index%6+1), ui.Rect{X: box.X, Y: box.Y + 4, Width: width, Height: 20}, gfx.TextStyle{Size: 12, Weight: gfx.WeightMedium, Color: palette.ink})
	for cell := range 36 {
		row, column := cell/6, cell%6
		bounds := ui.Rect{X: box.X + pad + float32(column*pitch), Y: box.Y + pad + 20 + float32(row*pitch), Width: pitch - 3, Height: pitch - 3}
		ink, fill := palette.border, palette.bar
		active := row <= index/6 && column <= index%6
		if active {
			ink, fill = palette.accent, palette.selected
		}
		canvas.RoundedBorder(bounds, 3, 1, ink, fill)
		id := tableGridCells.At(cell)
		label := fmt.Sprintf("%s %d × %d", state.local("插入表格", "Insert table"), row+1, column+1)
		canvas.HitWithOptions(id, bounds, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: label, Selected: active})
		if canvas.Clicked(id) {
			canvas.AfterPaint(func() {
				state.dismissMarkdownMenu()
				state.insertFormatTable(row+1, column+1)
			})
		}
	}
}

func (state *model) formatTableCell(context formatContext) (row, column int, ok bool) {
	if context.block.Kind != markdownedit.Table || !markdownedit.CanEditTable([]byte(context.source), context.block) {
		return 0, 0, false
	}
	if state.mode == renderedMode && state.table.active {
		return state.table.row, state.table.column, state.table.structured
	}
	start := formatLineStart(context.source, context.caret)
	for _, cell := range context.block.Cells {
		if cell.Start < start || cell.Start > formatLineEnd(context.source, start) {
			continue
		}
		if cell.Start > context.caret {
			if cell.Column == 0 {
				return cell.Row, 0, true
			}
			break
		}
		row, column = cell.Row, cell.Column
	}
	return row, column, true
}

func (state *model) runFormatTableAction(action markdownedit.TableAction) {
	if !state.canFormatMarkdown() || state.refuseComposingFormat() {
		return
	}
	if state.mode == renderedMode && state.table.active {
		state.focusRequest = state.applyTableAction(action, false).Focus
		return
	}
	context := state.formatContext()
	row, column, ok := state.formatTableCell(context)
	if !ok {
		return
	}
	patch, err := markdownedit.PatchTable([]byte(context.source), context.block, row, column, action)
	if err != nil {
		state.message = err.Error()
		return
	}
	caret := patch.Cell.Start
	if patch.Text == "" {
		caret = patch.Start
	}
	state.commitFormat(context, patch.Start, patch.End, patch.Text, caret, patch.Start, "", -1)
}
