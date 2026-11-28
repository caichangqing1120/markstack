package main

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/base"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/ui"

	"markstack/internal/icons"
	"markstack/internal/markdownedit"
	"markstack/internal/session"
	"markstack/internal/workspace"
)

type colors struct {
	background ui.Color
	sidebar    ui.Color
	bar        ui.Color
	selected   ui.Color
	border     ui.Color
	ink        ui.Color
	muted      ui.Color
	accent     ui.Color
	uiScale    float32
}

func (palette colors) textSize(size float32) float32 {
	if palette.uiScale == 0 {
		return size
	}
	return size * palette.uiScale
}

func (state *model) look() (ui.Theme, colors) {
	theme := ui.LightTheme()
	palette := colors{
		background: ui.RGB(252, 253, 255), sidebar: ui.RGB(240, 244, 251), bar: ui.RGB(247, 249, 254),
		selected: ui.RGB(220, 232, 255), border: ui.RGB(218, 225, 237), ink: ui.RGB(31, 41, 59),
		muted: ui.RGB(105, 117, 139), accent: ui.RGB(18, 86, 218),
	}
	if state.dark {
		theme = ui.DarkTheme()
		palette = colors{
			background: ui.RGB(32, 37, 46), sidebar: ui.RGB(27, 32, 40), bar: ui.RGB(38, 44, 54),
			selected: ui.RGB(49, 69, 102), border: ui.RGB(58, 67, 82), ink: ui.RGB(228, 234, 245),
			muted: ui.RGB(158, 170, 192), accent: ui.RGB(154, 189, 255),
		}
	}
	theme.Background = palette.background
	theme.Foreground = palette.ink
	theme.Card = palette.background
	theme.CardForeground = palette.ink
	theme.CardElevated = palette.bar
	theme.Popover = palette.bar
	theme.PopoverForeground = palette.ink
	theme.Border = palette.border
	theme.Muted = palette.sidebar
	theme.MutedForeground = palette.muted
	theme.Hover = palette.selected
	theme.Input = palette.border
	theme.Primary = palette.accent
	theme.PrimaryForeground = palette.background
	theme.Accent = palette.selected
	theme.AccentForeground = palette.accent
	theme.Ring = palette.accent
	palette.uiScale = state.uiSize(1)
	theme.Metrics.FontSizeM = state.uiSize(15)
	return theme, palette
}

func (state *model) paint(canvas *gfx.Canvas, size ui.Size) {
	state.syncRenderedDocumentHistory()
	defer state.syncBacklinks()
	defer state.syncDiskWatch()
	defer state.scheduleSessionManifest()
	defer state.scheduleRecovery()
	defer state.scheduleAutoSave()
	state.paneViewport = size.Width
	state.dropHeight = size.Height
	if current := state.session.Current(); current != nil && current.Kind == workspace.RichHTMLKind && state.mode == splitMode {
		state.mode = renderedMode
	}
	if current := state.session.Current(); current != nil && (current.ReadOnly() || current.Kind == workspace.CodeKind || current.Kind == workspace.PlainKind) {
		state.mode = editMode
	}
	if current := state.session.Current(); current != nil && current.Kind == workspace.RichHTMLKind {
		state.html.sync(current)
	}
	if state.mode == renderedMode {
		if current := state.session.Current(); current != nil {
			state.syncRendered(current)
			state.syncCodeBlock(current)
			state.syncTableCell(current)
		}
	} else {
		state.codeBlock = codeBlockEditor{}
		state.table = tableEditor{}
	}
	state.syncDocumentFind()
	theme, palette := state.look()
	viewport := ui.Rect{Width: size.Width, Height: size.Height}
	canvas.Fill(viewport, palette.background)
	state.paintTopBar(canvas, size, palette)
	statusHeight := float32(26)
	if state.mode == readMode {
		statusHeight = 0
	}
	sidebarWidth, outlineWidth := state.paneWidths(size.Width)
	if state.sidebar {
		state.paintSidebar(canvas, theme, palette, ui.Rect{X: 0, Y: 32, Width: sidebarWidth, Height: size.Height - 32 - statusHeight})
	} else {
		state.treeRect = ui.Rect{}
		state.workspaceSearch.rect = ui.Rect{}
		state.workspaceSearch.resultsRect = ui.Rect{}
	}
	main := ui.Rect{X: sidebarWidth, Y: 32, Width: size.Width - sidebarWidth, Height: size.Height - 32 - statusHeight}
	state.paintTabs(canvas, palette, main)
	content := ui.Rect{X: main.X, Y: 64, Width: main.Width, Height: max(0, size.Height-64-statusHeight)}
	if outlineWidth > 0 {
		content.Width -= outlineWidth
	}
	editorBounds := state.paintImagePasteBanner(canvas, theme, palette, content)
	editorBounds = state.paintDocumentBanners(canvas, palette, editorBounds)
	state.paintDocument(canvas, theme, palette, editorBounds)
	state.outlinePanel.filterRect, state.outlinePanel.rowsRect = ui.Rect{}, ui.Rect{}
	state.backlinks.rows = ui.Rect{}
	if outlineWidth > 0 {
		state.paintRightPane(canvas, theme, palette, ui.Rect{X: content.X + content.Width, Y: content.Y, Width: outlineWidth, Height: content.Height})
	}
	state.paintSlashMenu(canvas, palette, editorBounds)
	state.paintHeadingToolbar(canvas, theme, palette, editorBounds)
	state.paintDocumentFind(canvas, theme, palette, editorBounds)
	state.paintPaneResize(canvas, size, sidebarWidth, outlineWidth, statusHeight)
	if !state.focusRequest.IsZero() && (state.sourceRect.Width > 0 || state.rich.rect.Width > 0 || state.html.inputRect.Width > 0 || state.findRect.Width > 0 || state.linkRect.Width > 0 || state.workspaceSearch.rect.Width > 0 || state.treeNameRect.Width > 0 || state.outlinePanel.filterRect.Width > 0 || state.codeBlock.active || state.table.active || state.imageInspector.open) {
		_, eventPass := canvas.ClickTarget()
		if !eventPass {
			if state.focusReset {
				state.focusReset = false
			} else {
				canvas.RequestFocus(state.focusRequest)
				state.focusRequest = gfx.ID{}
			}
			state.invalidator.Invalidate()
		}
	}
	if statusHeight > 0 {
		state.paintStatus(canvas, palette, size)
	}
	if state.fileMenuOpen {
		state.paintFileMenu(canvas, palette, viewport)
	}
	if state.recentOpen {
		state.paintRecentPanel(canvas, palette, viewport)
	}
	state.paintConfirmation(canvas, palette, viewport)
	if state.fileAction == fileActionTrashConfirm {
		state.paintTreeTrashConfirmation(canvas, palette, viewport)
	}
	state.paintImageInspector(canvas, theme, palette, size)
	state.paintSettings(canvas, theme, palette, viewport)
	state.paintMediaManager(canvas, palette, viewport)
	state.paintDiskReloadConfirmation(canvas, palette, viewport)
	if !state.fileMenuOpen && !state.settingsOpen && !state.recentOpen && state.session.Pending() == nil && state.fileAction != fileActionTrashConfirm && !state.imageInspector.open && !state.mediaManager.open && state.diskWatch.confirm == nil {
		state.chromeTooltip.ShowOnFocus()
		state.chromeTooltip.Paint(canvas, theme, viewport, components.TooltipOptions{Layer: 20}, 450*time.Millisecond)
	}
	state.rememberFileMenuEditingFocus(canvas.Focused())
}

func button(canvas *gfx.Canvas, id gfx.ID, label string, bounds ui.Rect, active bool, palette colors, action func()) {
	labeledButton(canvas, id, label, label, bounds, active, palette, action)
}

func labeledButton(canvas *gfx.Canvas, id gfx.ID, caption, label string, bounds ui.Rect, active bool, palette colors, action func()) {
	if icon, ok := captionIcon(caption); ok {
		iconButton(canvas, id, icon, "", label, bounds, active, false, palette, action)
		return
	}
	if active {
		canvas.Rounded(bounds, 5, palette.selected)
	} else if canvas.Hovered() == id {
		canvas.Rounded(bounds, 5, palette.selected)
	}
	canvas.LabelCenter(ellipsizeWeight(canvas, caption, bounds.Width-4, palette.textSize(12), gfx.WeightMedium), bounds, gfx.TextStyle{Size: palette.textSize(12), Weight: gfx.WeightMedium, Color: palette.ink})
	canvas.HitWithOptions(id, bounds, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: label})
	if canvas.Clicked(id) && action != nil {
		action()
	}
}

func (state *model) paintTopBar(canvas *gfx.Canvas, size ui.Size, palette colors) {
	local := state.local
	leading, trailing := canvas.TitlebarInsets()
	left := leading + 8
	canvas.MoveRegion(ui.Rect{Width: size.Width, Height: 32})
	canvas.Fill(ui.Rect{Width: size.Width, Height: 32}, palette.bar)
	canvas.Fill(ui.Rect{Y: 31, Width: size.Width, Height: 1}, palette.border)
	canvas.DrawIcon(icons.MarkstackMark, ui.Rect{X: left, Y: 6, Width: 20, Height: 20}, palette.ink)
	canvas.Label("墨栈", ui.Rect{X: left + 27, Y: 3, Width: 41, Height: 26}, gfx.TextStyle{Size: state.uiSize(14), Weight: gfx.WeightSemibold, Color: palette.ink})
	iconButton(canvas, sidebarToggleID, icons.PanelLeft, "", local("切换侧栏", "Toggle sidebar"), ui.Rect{X: left + 75, Y: 4, Width: 28, Height: 24}, state.sidebar, false, palette, func() { state.sidebar = !state.sidebar })
	button(canvas, fileMenuID, local("文件", "File"), ui.Rect{X: left + 111, Y: 4, Width: 48, Height: 24}, state.fileMenuOpen, palette, func() {
		state.fileMenuOpen = !state.fileMenuOpen
		state.fileMenuScroll = components.MenuScroll{}
	})
	iconButton(canvas, newFileID, icons.FilePlus2, "", local("新建", "New"), ui.Rect{X: left + 171, Y: 4, Width: 28, Height: 24}, false, false, palette, func() {
		state.newDocument()
		state.deferEditingFocus()
	})
	iconButton(canvas, findDocumentID, icons.Search, "", local("文内查找", "Find in document"), ui.Rect{X: left + 203, Y: 4, Width: 28, Height: 24}, state.findOpen, false, palette, state.openDocumentFind)
	modeWidth := float32(68)
	if size.Width < 1000 {
		modeWidth = 32
	}
	right := size.Width - trailing - 4*modeWidth - 91
	canvas.Rounded(ui.Rect{X: right - 3, Y: 2, Width: 4*modeWidth + 4, Height: 28}, 7, palette.sidebar)
	current := state.session.Current()
	canRender := current != nil && !current.ReadOnly() && (current.Kind == workspace.MarkdownKind || current.Kind == workspace.RichHTMLKind)
	canSplit := canRender && current.Kind == workspace.MarkdownKind
	caption := func(value string) string {
		if modeWidth == 32 {
			return ""
		}
		return value
	}
	changeMode := func(apply func()) {
		if focus := state.pendingCompositionFocus(); focus != (gfx.ID{}) {
			state.focusRequest = focus
			return
		}
		apply()
	}
	rendered, source := local("排版", "Edit"), local("源码", "Source")
	split, reading := local("分栏", "Split"), local("阅读", "Read")
	iconButton(canvas, renderedModeID, icons.FileType2, caption(rendered), rendered, ui.Rect{X: right, Y: 4, Width: modeWidth - 2, Height: 24}, state.mode == renderedMode, !canRender, palette, func() { changeMode(func() { state.mode = renderedMode }) })
	iconButton(canvas, editModeID, icons.CodeXml, caption(source), source, ui.Rect{X: right + modeWidth, Y: 4, Width: modeWidth - 2, Height: 24}, state.mode == editMode, false, palette, func() {
		changeMode(func() {
			state.mode = editMode
			state.deferEditingFocus()
		})
	})
	iconButton(canvas, splitModeID, icons.Columns2, caption(split), split, ui.Rect{X: right + 2*modeWidth, Y: 4, Width: modeWidth - 2, Height: 24}, state.mode == splitMode, !canSplit, palette, func() { changeMode(func() { state.mode = splitMode }) })
	iconButton(canvas, readModeID, icons.BookOpen, caption(reading), reading, ui.Rect{X: right + 3*modeWidth, Y: 4, Width: modeWidth - 2, Height: 24}, state.mode == readMode, !canRender, palette, func() { changeMode(func() { state.mode = readMode }) })
	end := right + 4*modeWidth
	canvas.Fill(ui.Rect{X: end + 9, Y: 8, Width: 1, Height: 16}, palette.border)
	iconButton(canvas, outlineToggleID, icons.ListTree, "", local("大纲", "Outline"), ui.Rect{X: end + 19, Y: 4, Width: 28, Height: 24}, state.outline, false, palette, func() { state.outline = !state.outline })
	themeIcon := icons.Moon
	if state.dark {
		themeIcon = icons.Sun
	}
	iconButton(canvas, themeToggleID, themeIcon, "", local("明暗", "Toggle appearance"), ui.Rect{X: end + 53, Y: 4, Width: 28, Height: 24}, false, false, palette, state.toggleTheme)
}

func (state *model) paintFileMenu(canvas *gfx.Canvas, palette colors, viewport ui.Rect) {
	leading, _ := canvas.TitlebarInsets()
	theme, _ := state.look()
	theme.Metrics.FontSizeM = state.uiSize(14)
	local := state.local
	box := ui.Rect{X: leading + 119, Y: 36, Width: 248, Height: 535}
	compositionFocus := state.pendingCompositionFocus()
	options := components.DropdownMenuOptions{Layer: 9, DismissID: fileMenuBackdropID, Viewport: viewport, Scroll: &state.fileMenuScroll, RowHeight: 26}
	if compositionFocus == (gfx.ID{}) {
		options.Scope = fileMenuScope
	}
	menu := components.BeginDropdownMenu(canvas, theme, box, options)
	defer menu.End()
	if canvas.Clicked(fileMenuBackdropID) {
		state.fileMenuOpen = false
		state.focusRequest = fileMenuID
		if compositionFocus != (gfx.ID{}) {
			state.focusRequest = compositionFocus
		}
	}
	items := []struct {
		id       gfx.ID
		label    string
		icon     gfx.Icon
		shortcut string
		action   func()
	}{
		{menuNewID, local("新建文档", "New document"), icons.FilePlus2, shortcutLabel("N"), func() { state.newDocument(); state.deferEditingFocus() }},
		{noteTemplateID, local("笔记模板", "Note template"), icons.NotebookPen, "", func() { state.newFromTemplate(session.TemplateNote) }},
		{proposalTemplateID, local("方案模板", "Proposal template"), icons.PanelsTopLeft, "", func() { state.newFromTemplate(session.TemplateProposal) }},
		{newRichHTMLID, local("新建富文本", "New rich text"), icons.FileType2, "", state.newRichHTML},
		{openFolderID, local("打开文件夹", "Open folder"), icons.FolderOpen, "", state.openFolder},
		{openFileID, local("打开文件", "Open file"), icons.FileInput, shortcutLabel("O"), state.openFiles},
		{mediaMenuID, local("图片资源管理", "Manage images"), icons.FolderOpen, "", state.openMediaManager},
		{menuCheckUpdatesID, local("检查更新...", "Check for Updates..."), icons.FileDown, "", func() { state.openSettingsSection(settingsAbout) }},
		{saveID, local("保存", "Save"), icons.Save, shortcutLabel("S"), func() { state.saveFromFileMenu(false) }},
		{saveAsID, local("另存为", "Save as"), icons.SaveAll, shortcutLabel("⇧S"), func() { state.saveFromFileMenu(true) }},
		{menuExportHTMLID, local("导出 HTML", "Export HTML"), icons.FileCode2, "", func() { state.runFileMenuEditingAction(state.exportActiveHTML) }},
		{menuExportPDFID, local("导出 PDF", "Export PDF"), icons.FileDown, "", func() { state.runFileMenuEditingAction(state.exportActivePDF) }},
		{menuExportDOCXID, local("导出 DOCX", "Export DOCX"), icons.FileDown, "", func() { state.runFileMenuEditingAction(state.exportActiveDOCX) }},
		{menuTypewriterID, local("打字机模式", "Typewriter mode"), gfx.Icon{}, shortcutLabel("⌥T"), func() { state.toggleWritingMode(true) }},
		{menuFocusModeID, local("专注模式", "Focus mode"), gfx.Icon{}, shortcutLabel("⌥D"), func() { state.toggleWritingMode(false) }},
		{menuSettingsID, local("设置", "Settings"), icons.Settings2, "", state.openSettings},
		{recentMenuID, local("最近打开", "Open recent"), icons.History, "", state.openRecentPanel},
	}
	for _, item := range items {
		id := item.id
		enabled := true
		if id == menuExportHTMLID || id == menuExportPDFID || id == menuExportDOCXID {
			current := state.session.Current()
			enabled = current != nil && !current.ReadOnly() && (current.Kind == workspace.MarkdownKind || current.Kind == workspace.RichHTMLKind)
		}
		if id == menuTypewriterID || id == menuFocusModeID {
			current := state.session.Current()
			enabled = current != nil && !current.ReadOnly() && current.Kind == workspace.MarkdownKind
		}
		if id == saveID || id == saveAsID {
			current := state.session.Current()
			enabled = current != nil && !current.ReadOnly()
		}
		option := components.DropdownMenuItem{ID: id, Label: item.label, Icon: item.icon, Shortcut: item.shortcut, Disabled: !enabled}
		if id == menuTypewriterID || id == menuFocusModeID {
			option.Kind = components.DropdownMenuCheckbox
			option.Checked = id == menuTypewriterID && state.preferences.TypewriterMode || id == menuFocusModeID && state.preferences.FocusMode
		}
		menu.AddItem(option)
		if enabled && canvas.Clicked(id) {
			state.fileMenuOpen = false
			item.action()
		}
		switch id {
		case newRichHTMLID, menuCheckUpdatesID, saveAsID, menuExportDOCXID, menuFocusModeID, menuSettingsID:
			menu.AddSeparator()
		}
	}
	if compositionFocus != (gfx.ID{}) {
		canvas.RequestFocus(compositionFocus)
		if !state.fileMenuOpen && state.focusRequest == compositionFocus {
			state.focusRequest = gfx.ID{}
		}
		return
	}
	canvas.PushFocusScope(fileMenuScope)
	canvas.SetInitialFocus(openFolderID)
	canvas.PopFocusScope()
}

func (state *model) paintSidebar(canvas *gfx.Canvas, theme ui.Theme, palette colors, bounds ui.Rect) {
	if state.workspaceSearch.open {
		state.paintWorkspaceSearch(canvas, theme, palette, bounds)
		return
	}
	state.workspaceSearch.rect = ui.Rect{}
	state.workspaceSearch.resultsRect = ui.Rect{}
	canvas.Fill(bounds, palette.sidebar)
	canvas.Fill(ui.Rect{X: bounds.Width - 1, Y: bounds.Y, Width: 1, Height: bounds.Height}, palette.border)
	root := "文件"
	if state.session.Root != "" {
		root = filepath.Base(state.session.Root)
	}
	rootBounds := ui.Rect{X: 14, Y: 43, Width: bounds.Width - 100, Height: 27}
	canvas.Label(ellipsizeWeight(canvas, root, bounds.Width-102, 13, gfx.WeightSemibold), rootBounds, gfx.TextStyle{Size: 13, Weight: gfx.WeightSemibold, Color: palette.ink})
	canvas.HitWithOptions(rootSelectID, rootBounds, root, gfx.HitOptions{Role: gfx.RoleButton, Hint: "选择工作区根目录", Disabled: state.session.Root == ""})
	if canvas.Clicked(rootSelectID) {
		state.treeSelection = ""
	}
	iconButton(canvas, workspaceOpenID, icons.Search, "", "工作区全文搜索", ui.Rect{X: bounds.Width - 66, Y: 43, Width: 24, Height: 25}, false, false, palette, state.openWorkspaceSearch)
	iconButton(canvas, refreshID, icons.RefreshCw, "", "刷新工作区", ui.Rect{X: bounds.Width - 36, Y: 43, Width: 24, Height: 25}, false, false, palette, state.refresh)
	query := state.search.String()
	searchBounds := ui.Rect{X: 11, Y: 74, Width: bounds.Width - 22, Height: 28}
	searchTheme := theme
	searchTheme.Metrics.FontSizeM = 12
	components.PaintInput(canvas, searchTheme, searchBounds, components.InputSnapshot{
		ID: searchID, Label: "筛选文件名", Value: query, Placeholder: "搜索文件名", Focused: canvas.Focused() == searchID,
		PlatformEditing: canvas.Focused() == searchID && query != "", ShowCaret: canvas.Focused() == searchID && query == "",
	})
	state.paintTreeActions(canvas, theme, palette, bounds.Width)
	treeTop := float32(144)
	if state.treeNameRect.Width > 0 {
		treeTop = 174
	}
	state.treeRect = ui.Rect{X: 7, Y: treeTop, Width: bounds.Width - 14, Height: max(0, bounds.Y+bounds.Height-treeTop-8)}
	if state.session.Root == "" {
		if state.session.Current().Path == "" {
			canvas.Label("打开文件夹开始整理", ui.Rect{X: 16, Y: treeTop + 15, Width: bounds.Width - 26, Height: 30}, gfx.TextStyle{Size: 12, Color: palette.muted})
		}
		state.visible = nil
		return
	}
	state.visible = state.visibleNodes(query)
	state.treeScroll = min(state.treeScroll, max(0, float32(len(state.visible))*27-state.treeRect.Height))
	canvas.Clip(state.treeRect)
	tree := components.BeginTree(canvas, theme, ui.Rect{
		X: state.treeRect.X, Y: state.treeRect.Y - state.treeScroll,
		Width: state.treeRect.Width, Height: max(state.treeRect.Height, float32(len(state.visible))*27),
	}).RowHeight(27).Indent(14)
	fileIndex, folderIndex := 0, 0
	for _, node := range state.visible {
		if node.Dir {
			index := folderIndex
			folderIndex++
			row := tree.Item(components.TreeItem{
				ID: folderRows.At(index), DisclosureID: folderRows.At(index + 100000), Label: node.Name,
				Depth: uint8(max(node.Depth-1, 0)), Branch: true, Expanded: !state.collapsed[node.Path], Selected: state.treeSelection == node.Path,
			})
			canvas.Label(ellipsize(canvas, node.Name, row.Content.Width-10, 12), row.Content.Inset(gfx.Insets{Left: 3, Right: 7}), gfx.TextStyle{Size: 12, Color: palette.ink})
			if canvas.Clicked(folderRows.At(index)) || canvas.Clicked(folderRows.At(index+100000)) {
				state.treeSelection = node.Path
				state.collapsed[node.Path] = !state.collapsed[node.Path]
			}
			continue
		}
		index := fileIndex
		fileIndex++
		selected := state.session.Current().Path == node.Path || state.treeSelection == node.Path
		row := tree.Item(components.TreeItem{ID: fileRows.At(index), Label: node.Name, Depth: uint8(node.Depth), Selected: selected})
		canvas.Label(ellipsize(canvas, node.Name, row.Content.Width-18, 12), row.Content.Inset(gfx.Insets{Left: 6, Right: 10}), gfx.TextStyle{Size: 12, Color: palette.ink})
		if canvas.Clicked(fileRows.At(index)) {
			state.treeSelection = node.Path
			state.openFile(node.Path)
		}
	}
	canvas.Unclip()
}

func (state *model) visibleNodes(query string) []workspace.Node {
	query = strings.ToLower(strings.TrimSpace(query))
	var shown []workspace.Node
	if query != "" {
		ancestors := make(map[string]bool)
		for _, node := range state.session.Nodes {
			if node.Dir || !strings.Contains(strings.ToLower(node.Name), query) {
				continue
			}
			for dir := filepath.Dir(node.Path); dir != state.session.Root && dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
				ancestors[dir] = true
			}
		}
		for _, node := range state.session.Nodes {
			if node.Dir && ancestors[node.Path] || !node.Dir && strings.Contains(strings.ToLower(node.Name), query) {
				shown = append(shown, node)
			}
		}
		return shown
	}
	collapsedDepth := 0
	for _, node := range state.session.Nodes {
		if collapsedDepth != 0 {
			if node.Depth > collapsedDepth {
				continue
			}
			collapsedDepth = 0
		}
		shown = append(shown, node)
		if node.Dir && state.collapsed[node.Path] {
			collapsedDepth = node.Depth
		}
	}
	return shown
}

func (state *model) paintTabs(canvas *gfx.Canvas, palette colors, main ui.Rect) {
	changeDocument := func(action func()) {
		if !state.prepareDocumentNavigation() {
			return
		}
		action()
		state.resetDocumentView()
	}
	bar := ui.Rect{X: main.X, Y: 32, Width: main.Width, Height: 32}
	canvas.Fill(bar, palette.bar)
	canvas.Fill(ui.Rect{X: bar.X, Y: 63, Width: bar.Width, Height: 1}, palette.border)
	canvas.Clip(bar)
	tabsWidth := bar.Width
	if state.mode == readMode && state.session.Current() != nil && state.session.Current().Kind == workspace.MarkdownKind {
		reserve := min(float32(320), bar.Width*0.45)
		tabsWidth -= reserve
		info := state.statisticsLabel()
		if state.message != "" {
			info = state.message + "  ·  " + info
		}
		bounds := ui.Rect{X: bar.X + tabsWidth + 4, Y: bar.Y + 5, Width: reserve - 12, Height: 22}
		canvas.Rounded(bounds, 5, palette.sidebar)
		canvas.Label(ellipsize(canvas, info, bounds.Width-16, 11), bounds.Inset(gfx.Insets{Left: 8, Right: 8}), gfx.TextStyle{Size: 11, Color: palette.muted})
	}
	available := tabsWidth - 12
	capacity := max(1, int(available/108))
	if len(state.session.Tabs) > capacity {
		available -= 58
		capacity = max(1, int(available/136))
		state.tabStart = min(max(state.tabStart, 0), max(0, len(state.session.Tabs)-capacity))
		if state.session.Active < state.tabStart {
			state.tabStart = state.session.Active
		} else if state.session.Active >= state.tabStart+capacity {
			state.tabStart = state.session.Active - capacity + 1
		}
		labeledButton(canvas, tabPreviousID, "‹", "上一标签页", ui.Rect{X: bar.X + tabsWidth - 57, Y: 35, Width: 25, Height: 25}, false, palette, func() {
			changeDocument(func() { state.session.Active = max(0, state.session.Active-1) })
		})
		labeledButton(canvas, tabNextID, "›", "下一标签页", ui.Rect{X: bar.X + tabsWidth - 30, Y: 35, Width: 25, Height: 25}, false, palette, func() {
			changeDocument(func() { state.session.Active = min(len(state.session.Tabs)-1, state.session.Active+1) })
		})
	} else {
		state.tabStart = 0
	}
	width := min(float32(176), available/float32(min(capacity, len(state.session.Tabs))))
	for index := state.tabStart; index < min(len(state.session.Tabs), state.tabStart+capacity); index++ {
		document := state.session.Tabs[index]
		x := bar.X + 6 + float32(index-state.tabStart)*width
		tab := ui.Rect{X: x, Y: 33, Width: width - 3, Height: 30}
		if index == state.session.Active {
			canvas.Rounded(ui.Rect{X: x, Y: 32, Width: width - 3, Height: 32}, 5, palette.background)
		}
		caption := document.Title
		if document.Dirty() {
			caption = "• " + caption
		}
		if state.preferences.ShowTabNumbers && index < 9 {
			caption = strconv.Itoa(index+1) + ". " + caption
		}
		textSize := state.uiSize(12)
		canvas.Label(ellipsize(canvas, caption, tab.Width-44, textSize), ui.Rect{X: tab.X + 13, Y: tab.Y, Width: tab.Width - 40, Height: tab.Height}, gfx.TextStyle{Size: textSize, Color: palette.ink})
		canvas.HitWithOptions(tabRows.At(index), ui.Rect{X: tab.X, Y: tab.Y, Width: tab.Width - 28, Height: tab.Height}, caption, gfx.HitOptions{Role: gfx.RoleTab, Selected: index == state.session.Active})
		if canvas.Clicked(tabRows.At(index)) {
			changeDocument(func() { state.session.Active = index })
		}
		labeledButton(canvas, tabClose.At(index), "×", state.local("关闭 ", "Close ")+document.Title, ui.Rect{X: tab.X + tab.Width - 24, Y: tab.Y + 4, Width: 20, Height: 21}, false, palette, func() {
			changeDocument(func() { state.session.RequestClose(index) })
		})
	}
	canvas.Unclip()
}

func (state *model) paintDocument(canvas *gfx.Canvas, theme ui.Theme, palette colors, bounds ui.Rect) {
	state.sourceRect, state.previewRect = ui.Rect{}, ui.Rect{}
	state.previewLinkHits = state.previewLinkHits[:0]
	state.rich.rect = ui.Rect{}
	if state.mode != renderedMode {
		if state.selection.active || state.rich.active {
			state.deactivateRenderedView(state.session.Current())
		}
		state.headingMenuOpen = false
	}
	current := state.session.Current()
	if state.mode == editMode || current == nil || current.Kind != workspace.MarkdownKind {
		state.preview.layoutKey = previewLayoutKey{}
	}
	if current == nil || bounds.Width < 1 || bounds.Height < 1 {
		return
	}
	if current.ReadOnly() {
		top := float32(12)
		if state.findOpen {
			top = 78
		}
		state.paintSource(canvas, theme, palette, ui.Rect{X: bounds.X + 14, Y: bounds.Y + top, Width: bounds.Width - 28, Height: bounds.Height - top - 12})
		return
	}
	if current.Kind == workspace.RichHTMLKind {
		if state.mode == readMode && state.preferences.ReadingTheme == "soft" {
			palette = state.readingPalette(palette)
			canvas.Fill(bounds, palette.background)
		}
		state.paintRichHTML(canvas, theme, palette, bounds)
		return
	}
	if current.Kind == workspace.CodeKind || current.Kind == workspace.PlainKind {
		width := min(float32(1050), max(0, bounds.Width-36))
		x := bounds.X + (bounds.Width-width)/2
		topInset := float32(16)
		if state.findOpen {
			topInset = 80
		}
		state.paintSource(canvas, theme, palette, ui.Rect{X: x, Y: bounds.Y + topInset, Width: width, Height: bounds.Height - topInset - 14})
		return
	}
	if state.mode == splitMode {
		if bounds.Width >= 640 {
			half := bounds.Width / 2
			canvas.Fill(ui.Rect{X: bounds.X + half, Y: bounds.Y, Width: 1, Height: bounds.Height}, palette.border)
			state.paintSource(canvas, theme, palette, ui.Rect{X: bounds.X + 18, Y: bounds.Y + 40, Width: half - 36, Height: bounds.Height - 55})
			state.paintPreview(canvas, theme, palette, ui.Rect{X: bounds.X + half + 1, Y: bounds.Y, Width: half - 1, Height: bounds.Height})
			return
		}
		half := bounds.Height / 2
		canvas.Fill(ui.Rect{X: bounds.X, Y: bounds.Y + half, Width: bounds.Width, Height: 1}, palette.border)
		state.paintSource(canvas, theme, palette, ui.Rect{X: bounds.X + 18, Y: bounds.Y + 20, Width: bounds.Width - 36, Height: half - 30})
		state.paintPreview(canvas, theme, palette, ui.Rect{X: bounds.X, Y: bounds.Y + half + 1, Width: bounds.Width, Height: bounds.Height - half - 1})
		return
	}
	if state.mode == readMode {
		state.paintPreview(canvas, theme, palette, bounds)
		return
	}
	if state.mode == renderedMode {
		state.paintRendered(canvas, theme, palette, bounds)
		return
	}
	width := min(float32(820), max(0, bounds.Width-52))
	x := bounds.X + (bounds.Width-width)/2
	state.paintSource(canvas, theme, palette, ui.Rect{X: x, Y: bounds.Y + 45, Width: width, Height: bounds.Height - 69})
}

func (state *model) paintSource(canvas *gfx.Canvas, theme ui.Theme, palette colors, bounds ui.Rect) {
	if bounds.Width < 40 || bounds.Height < 64 {
		return
	}
	state.sourceRect = bounds
	sourceTheme := theme
	sourceTheme.Border = palette.background
	sourceTheme.Ring = palette.background
	sourceTheme.Input = ui.Color{}
	sourceTheme.Metrics.FontSizeM = state.sourceSize()
	current := state.session.Current()
	value := current.Text()
	focused := canvas.Focused() == sourceID
	selection := current.Editor.Selection()
	byteAt := func(offset int) int { return byteOffsetOfRune(value, offset) }
	if current.Kind == workspace.MarkdownKind && !current.ReadOnly() && !current.Composing() {
		state.stats.update(value)
		byteAt = state.stats.byteAt
	}
	label := "Markdown 源码编辑器"
	switch current.Kind {
	case workspace.RichHTMLKind:
		label = "HTML 原文编辑器"
	case workspace.CodeKind:
		label = "代码编辑器"
	case workspace.PlainKind:
		label = "纯文本编辑器"
	}
	snapshot := components.CodeEditorSnapshot{
		ID: sourceID, Label: label, Value: value,
		Placeholder: "开始写作…", Focused: focused, ShowCaret: focused,
		SelectionStart: byteAt(selection.Start),
		SelectionEnd:   byteAt(selection.End),
		Caret:          byteAt(current.Editor.Caret()),
	}
	if current.ReadOnly() {
		snapshot.Label = largePreviewText
		snapshot.ReadOnly = true
		snapshot.ShowCaret = false
		snapshot.Wrap = true
		snapshot.FontSize = state.sourceSize()
		state.source.Paint(canvas, sourceTheme, bounds, snapshot)
		return
	}
	if marked, composing := current.Editor.Composition(); composing {
		snapshot.CompositionStart = byteOffsetOfRune(value, marked.Start)
		snapshot.CompositionEnd = byteOffsetOfRune(value, marked.End)
	}
	canvas.Clip(bounds.Inset(gfx.All(4)))
	snapshot.FontSize, snapshot.Wrap, snapshot.Language = state.sourceSize(), true, "markdown"
	if current.Kind == workspace.RichHTMLKind {
		snapshot.Language, snapshot.LineNumbers = "html", true
	} else if current.Kind == workspace.CodeKind {
		language := codeLanguage(current.Path)
		if language == "" {
			language = codeLanguage(current.Title)
		}
		state.codeHighlight.Prepare(current.ID, value, language, palette, state.dark)
		snapshot.Language, snapshot.LineNumbers, snapshot.CurrentLine, snapshot.MatchBrackets, snapshot.Wrap = language, true, true, true, false
		if len(state.codeHighlight.lines) > 0 {
			snapshot.HighlightLine = func(_ string, lineIndex int, line string, runs []components.CodeEditorRun) []components.CodeEditorRun {
				return state.codeHighlight.Runs(lineIndex, line, runs)
			}
		}
	} else if current.Kind == workspace.PlainKind {
		snapshot.Language = ""
	} else if current.Kind == workspace.MarkdownKind {
		if state.preferences.FocusMode && !current.Composing() {
			first, last := state.sourceFocusLines(snapshot.Caret)
			snapshot.HighlightLine = func(_ string, index int, line string, runs []components.CodeEditorRun) []components.CodeEditorRun {
				return state.sourceFocusRuns(line, palette, runs, index < first || index > last)
			}
		} else {
			snapshot.Highlight = func(_ string, line string, runs []components.CodeEditorRun) []components.CodeEditorRun {
				return markdownSourceRuns(line, palette, runs)
			}
		}
	}
	composingConfirmation := state.confirmationCompositionFocus() == sourceID
	if composingConfirmation {
		canvas.PushFocusScope(confirmScope)
	}
	frame := bounds
	if current.Kind == workspace.MarkdownKind && state.preferences.TypewriterMode {
		frame.Y += state.sourceShift
	}
	state.source.Paint(canvas, sourceTheme, frame, snapshot)
	if current.Kind == workspace.MarkdownKind && !state.paintSourceAnchor(canvas, snapshot.Caret) && focused && state.preferences.TypewriterMode {
		key := writingCaret{document: current.ID, mode: state.mode, offset: snapshot.Caret, viewport: bounds, source: value}
		if caret, ok := state.source.CaretBounds(); ok {
			previous := state.sourceWritingScroll()
			target := previous + caret.Y + caret.Height/2 - (bounds.Y + bounds.Height/2)
			state.setSourceWritingScroll(state.followWritingCaret(canvas, key, previous, target))
			if previous != state.sourceWritingScroll() {
				canvas.RequestFrame()
			}
		}
	}
	if composingConfirmation {
		canvas.PopFocusScope()
	}
	canvas.Unclip()
}

func (state *model) paintPreview(canvas *gfx.Canvas, theme ui.Theme, palette colors, bounds ui.Rect) (string, []markdownedit.Block) {
	state.previewRect = bounds
	appearance := state.markdownAppearance(palette)
	state.preview.appearance = appearance
	palette = appearance.palette
	canvas.Fill(bounds, palette.background)
	current := state.session.Current()
	source := current.Text()
	if source == "" && !(state.mode == renderedMode && state.rich.active && state.rich.virtual) {
		canvas.LabelCenter("开始写作…", ui.Rect{X: bounds.X, Y: bounds.Y + bounds.Height/3, Width: bounds.Width, Height: 36}, gfx.TextStyle{Size: 16, Color: palette.muted})
	}
	x, width := state.markdownColumn(bounds)
	readingSize := state.readingSize()
	var activeTable *tableEditor
	if state.table.active && state.table.documentID == current.ID {
		activeTable = &state.table
	}
	composing := state.rich.input != nil && state.richComposing()
	editingInline := -1
	if state.mode == renderedMode && state.rich.active && state.rich.documentID == current.ID {
		editingInline = state.rich.index
	}
	cacheable := activeTable == nil && !state.codeBlock.active && !composing && !state.virtualParagraphNeedsPlaceholder() && state.selection.anchorVirtual == nil
	key := previewLayoutKey{
		documentID: current.ID, path: current.Path, source: source,
		width: width, size: readingSize, scale: canvas.PixelScale(), readingTheme: state.preferences.ReadingTheme, night: state.dark, mode: state.mode, editingInline: editingInline,
	}
	if !cacheable || state.preview.layoutKey != key || state.markdownMediaBlocks == nil {
		state.parseMarkdownProjection(source)
		state.preview.editingInline = editingInline
		state.localLinks.update(source)
		state.localLinks.project(&state.parsed)
		sourceBlocks := markdownedit.Parse([]byte(source))
		state.preview.sourceBlocks = sourceBlocks
		normalizeRenderedTasks(&state.parsed, sourceBlocks)
		state.projectRenderedComposition()
		images := state.markdownPreviewImages(current)
		state.preview.night = state.dark
		lineHeight := readingSize * 26 / 16
		if state.preferences.ReadingTheme == "academic" && (state.mode == readMode || state.mode == splitMode) {
			lineHeight = readingSize * 29 / 16
		}
		state.preview.Layout(canvas, components.MarkdownStyle{Size: readingSize, LineHeight: lineHeight, MonoFace: markdownCodeFace, Highlight: markdownCodeRuns}, &state.parsed, width, images, state.markdownInlineBlocks, activeTable)
		if state.mode == renderedMode {
			state.preview.mapping = renderedMapping(&state.parsed, sourceBlocks)
		}
		state.preview.taskMarkers = state.preview.taskMarkers[:0]
		renderedTasks(&state.parsed, sourceBlocks, func(index int, block markdownedit.Block) {
			state.preview.taskMarkers = append(state.preview.taskMarkers, previewTaskMarker{index: index, checked: block.Checked, label: "任务 " + renderedPlain(&state.parsed, index)})
		})
		if cacheable {
			state.preview.layoutKey = key
		}
	}
	sourceBlocks := state.preview.sourceBlocks
	if activeTable != nil && activeTable.renderedIndex < len(state.preview.blocks) && state.preview.blocks[activeTable.renderedIndex].kind == markdown.Table {
		layout := &state.preview.blocks[activeTable.renderedIndex]
		activeTable.columns = append(activeTable.columns[:0], layout.table.columns...)
		activeTable.layoutWidth = width
	}
	if state.mode == renderedMode && state.codeBlock.active && state.codeBlock.renderedIndex < len(state.preview.blocks) {
		index := state.codeBlock.renderedIndex
		fontSize := state.readingSize() - 1
		_, cellHeight := canvas.MonoCellSize(fontSize, markdownCodeFace)
		lineHeight := max(cellHeight, float32(int(fontSize*1.5+0.5)))
		minimum := max(float32(64), float32(strings.Count(state.codeBlock.input.String(), "\n")+1)*lineHeight+16)
		layout := &state.preview.blocks[index]
		layout.editorHeight = minimum + codeHeaderHeight
		if technicalBlock(&state.parsed.Blocks[index], state.preview.displayMath[index]) {
			minimum = layout.editorHeight + 30
			if layout.vector.Width > 0 {
				minimum = layout.editorHeight + float32(layout.vector.Height)*layout.vectorScale + 16
			}
			state.preview.reserveBlockHeight(index, minimum)
		} else {
			state.preview.reserveBlockHeight(index, layout.editorHeight)
		}
	}
	if state.mode == renderedMode && state.rich.active && !state.virtualParagraphNeedsPlaceholder() && state.rich.documentID == current.ID && state.rich.index >= 0 && state.rich.index < len(state.preview.blocks) {
		index := state.rich.index
		block := &state.parsed.Blocks[index]
		textInset, size, weight := renderedTextGeometry(&state.parsed, index, ui.Rect{}, state.dark)
		lineHeight := float32(26)
		if block.Kind == markdown.Heading {
			_, lineHeight, _ = markdownHeadingMetrics(block.Level, state.dark)
		}
		minimum := lineHeight
		if !state.activeMixedSpan() {
			state.rich.wrapRows = canvas.WrapRows(state.rich.wrapRows[:0], state.rich.input.String(), size, weight, width-textInset)
			minimum = float32(max(1, len(state.rich.wrapRows))) * lineHeight
		}
		if state.preview.blocks[index].rule > 0 {
			minimum += 6
		}
		state.preview.reserveBlockHeight(index, minimum)
	}
	virtualIndex, virtualDepth := -1, 0
	textHeight := float32(26)
	if state.mode == renderedMode && state.rich.active && state.virtualParagraphNeedsPlaceholder() {
		state.rich.wrapRows = canvas.WrapRows(state.rich.wrapRows[:0], state.rich.input.String(), 16, gfx.WeightRegular, width)
		textHeight = float32(max(1, len(state.rich.wrapRows))) * 26
		virtualIndex, virtualDepth = state.rich.index, state.virtualParagraphDepth()
	} else if state.mode == renderedMode && state.selection.active && state.selection.anchorVirtual != nil && state.selection.documentID == current.ID && state.selection.source == current.Text() {
		virtualIndex, virtualDepth = state.selection.anchorIndex, state.selection.anchorVirtual.depth
	}
	if virtualIndex >= 0 && virtualIndex <= len(state.preview.blocks) {
		state.preview.layoutKey = previewLayoutKey{}
		paragraphGap := markdownParagraphGap(state.dark, 0)
		gap := float32(virtualDepth)*(26+paragraphGap) + textHeight + paragraphGap
		for index := virtualIndex; index < len(state.preview.blocks); index++ {
			state.preview.blocks[index].top += gap
		}
		state.preview.height += gap
	}
	state.revealRenderedHistoryTarget(canvas, theme, bounds)
	minimum, maximum := state.previewLimits(bounds)
	state.previewScroll = max(minimum, min(state.previewScroll, maximum))
	state.previewAnchor(canvas, bounds)
	markdownLook := components.MarkdownLookFor(theme)
	markdownLook.Text = palette.ink
	markdownLook.Muted = palette.muted
	markdownLook.Link = appearance.link
	if state.mode == renderedMode && state.selection.active {
		markdownLook.Selection = palette.accent
		markdownLook.Selection.Alpha = 0.25
	}
	markdownLook.CodeFill = appearance.codeFill
	markdownLook.InlineCodeFill = appearance.inlineCodeBackground
	markdownLook.Line = palette.border
	appearance.applyCodeLook(&markdownLook, state.dark)
	paint := components.MarkdownPaint{
		Look: markdownLook, X: x, Y: bounds.Y + 46 - state.previewScroll, Clip: bounds, Links: previewLinks,
	}
	firstHit := len(canvas.Hits())
	if state.mode == renderedMode && state.codeBlock.active && state.codeBlock.documentID == current.ID {
		state.preview.PaintExcept(canvas, paint, state.codeBlock.renderedIndex)
	} else if state.mode == renderedMode && state.rich.active && !state.virtualParagraphNeedsPlaceholder() {
		if state.activeMixedSpan() {
			selection := state.rich.input.Selection()
			value := state.rich.input.String()
			state.preview.PaintSelected(canvas, paint, state.rich.index, base.TextRange{
				Anchor: byteOffsetOfRune(value, selection.Start), Focus: byteOffsetOfRune(value, selection.End),
			})
			if marked, composing := state.rich.input.Composition(); composing {
				start, end := byteOffsetOfRune(value, marked.Start), byteOffsetOfRune(value, marked.End)
				block := &state.parsed.Blocks[state.rich.index]
				level := 0
				if block.Kind == markdown.Heading {
					level = block.Level
				}
				color := appearance.headingColor(level, state.dark)
				if block.Quote > 0 {
					color = palette.muted
				}
				offset := 0
				for _, span := range state.parsed.Spans(block) {
					if start >= offset && end <= offset+len(span.Text) {
						if span.Link != "" {
							color = appearance.link
						}
						break
					}
					offset += len(span.Text)
				}
				state.preview.PaintMarked(canvas, paint, state.rich.index, base.TextRange{Anchor: start, Focus: end}, color)
			}
		} else {
			state.preview.PaintExcept(canvas, paint, state.rich.index)
		}
	} else if state.mode == renderedMode && state.table.active && canvas.Focused() == tableInputID && state.table.documentID == current.ID && state.table.renderedIndex < len(state.preview.blocks) {
		columns := len(state.preview.blocks[state.table.renderedIndex].table.columns)
		state.preview.PaintExceptTableCell(canvas, paint, state.table.renderedIndex, state.table.row*columns+state.table.column)
	} else if state.mode == renderedMode && state.selection.active {
		state.preview.PaintSelectionRange(canvas, paint, &state.selection)
	} else {
		state.preview.Paint(canvas, paint)
	}
	state.captureMarkdownLinkHits(canvas, firstHit)
	if state.preview.clickedImage >= 0 {
		state.openImageInspector(current, state.preview.clickedImage)
	}
	if state.preview.clickedInlineValid {
		state.openImageInspectorBitmap(current, state.preview.clickedInline)
	}
	if state.mode == readMode {
		canvas.Clip(paint.Clip)
		state.paintTaskMarkers(canvas, palette, paint.Clip, x, paint.Y)
		canvas.Unclip()
	}
	if state.mode != renderedMode {
		canvas.Clip(paint.Clip)
		state.paintCodeControls(canvas, theme, palette, paint.Clip, x, paint.Y, sourceBlocks)
		for index := state.preview.firstVisible(bounds.Y - paint.Y); index < len(state.preview.blocks); index++ {
			block := state.preview.blocks[index]
			top := paint.Y + block.top
			if top >= bounds.Y+bounds.Height {
				break
			}
			if block.vector.Width > 0 || block.mathFallback {
				canvas.HitWithOptions(technicalPreviewRows.At(index), ui.Rect{X: x, Y: top, Width: width, Height: block.height}, "编辑公式或图表源码", gfx.HitOptions{Role: gfx.RoleButton})
			}
		}
		canvas.Unclip()
	}
	return source, sourceBlocks
}

func (state *model) paintStatus(canvas *gfx.Canvas, palette colors, size ui.Size) {
	bar := ui.Rect{Y: size.Height - 26, Width: size.Width, Height: 26}
	canvas.Fill(bar, palette.bar)
	canvas.Fill(ui.Rect{Y: bar.Y, Width: size.Width, Height: 1}, palette.border)
	left := state.message
	if current := state.session.Current(); current != nil && current.ReadOnly() {
		left = largePreviewText
		if state.message != "" && state.message != largePreviewText {
			left += " · " + state.message
		}
	}
	if current := state.session.Current(); current != nil && current.Dirty() && (strings.HasPrefix(left, "已保存 ") || strings.HasPrefix(left, "Saved ")) {
		left = state.local("未保存的更改", "Unsaved changes")
	}
	info := state.statisticsLabel()
	textSize := state.uiSize(11)
	infoWidth := min(size.Width/2, canvas.TextWidth(info, textSize)+12)
	if info == "" {
		infoWidth = 210
	}
	leftBounds := ui.Rect{X: 15, Y: bar.Y + 1, Width: size.Width - infoWidth - 30, Height: 24}
	style := gfx.TextStyle{Size: textSize, Color: palette.muted}
	warning := ""
	if state.recoveryLoadErr != nil {
		warning = "恢复快照不可用：" + state.recoveryLoadErr.Error()
	} else if state.recoveryWriteErr != nil {
		warning = "恢复快照失败：" + state.recoveryWriteErr.Error()
	}
	if warning != "" {
		if left == "" {
			left = warning
		} else {
			bounds := leftBounds
			bounds.Width = min(bounds.Width/2, canvas.TextWidth(warning, style.Size))
			canvas.Label(ellipsize(canvas, warning, bounds.Width, style.Size), bounds, style)
			leftBounds.X += bounds.Width + 12
			leftBounds.Width -= bounds.Width + 12
		}
	}
	if left == "" && state.session.Root != "" {
		left = state.session.Root
	}
	canvas.Label(ellipsize(canvas, left, leftBounds.Width, style.Size), leftBounds, style)
	current := state.session.Current()
	if current == nil {
		return
	}
	if current.ReadOnly() {
		info = "局部预览 · UTF-8"
	} else if current.Kind != workspace.MarkdownKind {
		value := current.Text()
		line := strings.Count(value[:byteOffsetOfRune(value, current.Editor.Caret())], "\n") + 1
		info = "字数 " + strconv.Itoa(utf8.RuneCountInString(value)) + "    行 " + strconv.Itoa(line) + "    UTF-8"
		infoWidth = 210
	}
	canvas.Label(ellipsize(canvas, info, infoWidth, textSize), ui.Rect{X: size.Width - infoWidth - 10, Y: bar.Y + 1, Width: infoWidth, Height: 24}, style)
}

func (state *model) paintConfirmation(canvas *gfx.Canvas, palette colors, viewport ui.Rect) {
	confirmation := state.session.Pending()
	if _, clickPass := canvas.ClickTarget(); !clickPass {
		state.confirmationShown = confirmation
	}
	if confirmation == nil {
		return
	}
	compositionFocus := state.confirmationCompositionFocus()
	canvas.PushLayer(10)
	canvas.PushFocusScope(confirmScope)
	if compositionFocus != (gfx.ID{}) {
		canvas.HitWithOptions(confirmBackdropID, viewport, "退出确认背景", gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true, Backdrop: true})
	}
	canvas.Fill(viewport, ui.RGBA(15, 19, 22, 0.45))
	width := min(float32(440), viewport.Width-40)
	box := ui.Rect{X: (viewport.Width - width) / 2, Y: (viewport.Height - 172) / 2, Width: width, Height: 172}
	canvas.RoundedBorder(box, 7, 1, palette.border, palette.background)
	canvas.Label(state.local("保存对文档的更改？", "Save changes to this document?"), ui.Rect{X: box.X + 22, Y: box.Y + 19, Width: width - 44, Height: 30}, gfx.TextStyle{Size: state.uiSize(17), Weight: gfx.WeightSemibold, Color: palette.ink})
	canvas.Label(ellipsize(canvas, confirmation.Document.Title, width-48, 13), ui.Rect{X: box.X + 22, Y: box.Y + 55, Width: width - 44, Height: 28}, gfx.TextStyle{Size: 13, Color: palette.muted})
	resolve := func(id gfx.ID, decision session.Decision) func() {
		return func() {
			press := state.confirmationPress
			state.confirmationPress = nil
			byKey := canvas.ClickedByKey(id)
			if byKey {
				press = state.confirmationShown
			}
			if press != nil && *press == *confirmation && (byKey || state.confirmationButton == id) {
				state.resolve(decision)
			}
		}
	}
	button(canvas, cancelConfirmID, state.local("取消", "Cancel"), ui.Rect{X: box.X + width - 268, Y: box.Y + 117, Width: 68, Height: 32}, false, palette, resolve(cancelConfirmID, session.Cancel))
	button(canvas, discardConfirmID, state.local("不保存", "Discard"), ui.Rect{X: box.X + width - 191, Y: box.Y + 117, Width: 76, Height: 32}, false, palette, resolve(discardConfirmID, session.Discard))
	button(canvas, saveConfirmID, state.local("保存", "Save"), ui.Rect{X: box.X + width - 106, Y: box.Y + 117, Width: 84, Height: 32}, true, palette, resolve(saveConfirmID, session.Save))
	if compositionFocus == (gfx.ID{}) {
		canvas.SetInitialFocus(cancelConfirmID)
		canvas.SetEscapeTarget(cancelConfirmID)
	} else {
		canvas.RequestFocus(compositionFocus)
	}
	canvas.TrapFocus(confirmScope)
	canvas.PopFocusScope()
	canvas.PopLayer()
}

func ellipsize(canvas *gfx.Canvas, value string, width, size float32) string {
	return ellipsizeWeight(canvas, value, width, size, gfx.WeightRegular)
}

func ellipsizeWeight(canvas *gfx.Canvas, value string, width, size, weight float32) string {
	if width <= 0 {
		return ""
	}
	if canvas.TextWidthWeight(value, size, weight) <= width {
		return value
	}
	for len(value) > 0 && canvas.TextWidthWeight(value+"…", size, weight) > width {
		_, runeSize := utf8.DecodeLastRuneInString(value)
		value = value[:len(value)-runeSize]
	}
	return value + "…"
}
