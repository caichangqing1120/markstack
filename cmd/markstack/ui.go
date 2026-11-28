package main

import (
	"fmt"
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
	recoveryWarning := state.recoveryStatusWarning()
	if state.mode == readMode {
		statusHeight = 0
	} else if recoveryWarning != "" {
		statusHeight += 22
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
	state.paintSelectionFormat(canvas, palette)
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
		if recoveryWarning != "" {
			state.paintRecoveryStatusWarning(canvas, palette, size, recoveryWarning)
		}
	}
	if state.fileMenuOpen {
		if state.topExportOpen {
			state.paintTopExportMenu(canvas, viewport)
		} else {
			state.paintFileMenu(canvas, palette, viewport)
		}
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
	if state.palette.query == nil && !state.fileMenuOpen && !state.settingsOpen && !state.recentOpen && state.session.Pending() == nil && state.fileAction != fileActionTrashConfirm && !state.imageInspector.open && !state.mediaManager.open && state.diskWatch.confirm == nil {
		state.chromeTooltip.ShowOnFocus()
		state.chromeTooltip.Paint(canvas, theme, viewport, components.TooltipOptions{Layer: 20}, 450*time.Millisecond)
	}
	state.paintCommandPalette(canvas, theme, palette, viewport)
	state.paintToast(canvas, palette, viewport, statusHeight)
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
	left, right := leading+8, size.Width-trailing-8
	canvas.MoveRegion(ui.Rect{Width: size.Width, Height: 32})
	canvas.Fill(ui.Rect{Width: size.Width, Height: 32}, palette.bar)
	canvas.Fill(ui.Rect{Y: 31, Width: size.Width, Height: 1}, palette.border)
	wide := size.Width >= 980
	showCommandLabels := wide && right-left >= 820
	controlWidth, gap := float32(28), float32(3)
	if right-left < 600 {
		controlWidth, gap = 26, 2
	}
	if size.Width >= 1280 {
		canvas.DrawIcon(icons.MarkstackMark, ui.Rect{X: left, Y: 6, Width: 20, Height: 20}, palette.ink)
		canvas.Label("墨栈", ui.Rect{X: left + 26, Y: 3, Width: 38, Height: 26}, gfx.TextStyle{Size: state.uiSize(14), Weight: gfx.WeightSemibold, Color: palette.ink})
		left += 70
	}
	next := func(width float32) ui.Rect {
		bounds := ui.Rect{X: left, Y: 4, Width: width, Height: 24}
		left += width + gap
		return bounds
	}
	current := state.session.Current()
	canSave := current != nil && !current.ReadOnly()
	iconButton(canvas, sidebarToggleID, icons.PanelLeft, "", local("切换侧栏", "Toggle sidebar"), next(controlWidth), state.sidebar, false, palette, func() { state.sidebar = !state.sidebar })
	iconButton(canvas, outlineToggleID, icons.ListTree, "", local("大纲", "Outline"), next(controlWidth), state.outline, false, palette, func() { state.outline = !state.outline })
	file := next(44)
	if !state.topExportOpen {
		state.fileMenuAnchor = file
	}
	button(canvas, fileMenuID, local("文件", "File"), file, state.fileMenuOpen && !state.topExportOpen, palette, func() { state.toggleFilePopover(false, file) })
	iconButton(canvas, newFileID, icons.FilePlus2, "", local("新建", "New"), next(controlWidth), false, false, palette, func() {
		state.newDocument()
		state.deferEditingFocus()
	})
	if wide {
		label := local("自动保存", "Auto-save")
		labelSize := state.uiSize(12)
		bounds := next(max(92, canvas.TextWidth(label, labelSize)+45))
		canvas.Label(label, bounds.Inset(gfx.Insets{Left: 3, Right: 38}), gfx.TextStyle{Size: labelSize, Color: palette.ink})
		track := ui.Rect{X: bounds.X + bounds.Width - 34, Y: 7, Width: 32, Height: 18}
		theme, _ := state.look()
		theme.Input = palette.muted
		if components.BeginSwitch(canvas, theme, topAutoSaveID, track, state.preferences.AutoSave).HitBounds(bounds).Label(label).Paint().Clicked() {
			state.runFileMenuEditingAction(func() {
				next := state.preferences
				next.AutoSave = !next.AutoSave
				state.changeSettings(next)
			})
		}
	}
	for _, control := range []struct {
		id         gfx.ID
		icon       gfx.Icon
		label      string
		choosePath bool
	}{{topSaveID, icons.Save, local("保存", "Save"), false}, {topSaveAsID, icons.SaveAll, local("另存为", "Save as"), true}} {
		caption, width := "", controlWidth
		if showCommandLabels {
			caption = control.label
			width = max(64, canvas.TextWidthWeight(caption, state.uiSize(12), gfx.WeightMedium)+33)
		}
		iconButton(canvas, control.id, control.icon, caption, control.label, next(width), false, !canSave, palette, func() { state.saveFromFileMenu(control.choosePath) })
	}
	export := local("导出", "Export")
	exportCaption, exportWidth := "", controlWidth
	if showCommandLabels {
		exportCaption = export
		exportWidth = max(66, canvas.TextWidthWeight(export, state.uiSize(12), gfx.WeightMedium)+33)
	}
	const fontWidth = 105
	rightWidth := 3*(controlWidth+gap) + fontWidth + gap + exportWidth
	rightStart := right - rightWidth
	modeWidth := float32(32)
	if rightStart-left >= 4*76+12 {
		modeWidth = 76
	}
	modeStart := min(max(left+5, (leading+right-4*modeWidth)/2), rightStart-4*modeWidth-7)
	canvas.RoundedBorder(ui.Rect{X: modeStart - 3, Y: 2, Width: 4*modeWidth + 4, Height: 28}, 8, 1, palette.border.WithAlpha(0.75), palette.sidebar)
	for index, control := range []struct {
		id    gfx.ID
		mode  viewMode
		icon  gfx.Icon
		label string
	}{
		{renderedModeID, renderedMode, icons.FileType2, local("排版", "Edit")},
		{splitModeID, splitMode, icons.Columns2, local("分栏", "Split")},
		{editModeID, editMode, icons.CodeXml, local("源码", "Source")},
		{readModeID, readMode, icons.BookOpen, local("阅读", "Read")},
	} {
		caption := ""
		if modeWidth > 32 {
			caption = control.label
		}
		bounds := ui.Rect{X: modeStart + float32(index)*modeWidth, Y: 4, Width: modeWidth - 2, Height: 24}
		segmentButton(canvas, control.id, control.icon, caption, control.label, bounds, state.mode == control.mode, !state.canUseViewMode(control.mode), palette, func() { state.setViewMode(control.mode) })
	}
	left = rightStart
	iconButton(canvas, findDocumentID, icons.Search, "", local("文内查找", "Find in document"), next(controlWidth), state.findOpen, false, palette, state.openDocumentFind)
	state.paintFontStepper(canvas, palette, next(fontWidth))
	themeIcon := icons.Moon
	if state.dark {
		themeIcon = icons.Sun
	}
	iconButton(canvas, themeToggleID, themeIcon, "", local("明暗", "Toggle appearance"), next(controlWidth), false, false, palette, state.toggleTheme)
	iconButton(canvas, settingsToggleID, icons.Settings2, "", local("设置", "Settings"), next(controlWidth), state.settingsOpen, false, palette, func() {
		if state.settingsOpen {
			state.settingsOpen = false
			state.settings.reset = false
		} else {
			state.openSettings()
		}
	})
	exportBounds := next(exportWidth)
	if state.topExportOpen {
		state.fileMenuAnchor = exportBounds
	}
	iconButton(canvas, topExportMenuID, icons.FileDown, exportCaption, export, exportBounds, state.fileMenuOpen && state.topExportOpen, !state.canUseViewMode(renderedMode), palette, func() { state.toggleFilePopover(true, exportBounds) })
}

func (state *model) canUseViewMode(mode viewMode) bool {
	current := state.session.Current()
	return mode == editMode || current != nil && !current.ReadOnly() && (current.Kind == workspace.MarkdownKind || current.Kind == workspace.RichHTMLKind && mode != splitMode)
}

func (state *model) setViewMode(mode viewMode) {
	if focus := state.pendingCompositionFocus(); !focus.IsZero() {
		state.focusRequest = focus
		return
	}
	if !state.canUseViewMode(mode) {
		return
	}
	state.mode = mode
	state.fontWheelDelta = 0
	if mode == editMode || mode == splitMode {
		state.deferEditingFocus()
	}
}

func (state *model) toggleFilePopover(export bool, anchor ui.Rect) {
	state.fileMenuOpen = !state.fileMenuOpen || state.topExportOpen != export
	state.topExportOpen = export
	state.fileMenuAnchor = anchor
	state.fileMenuScroll = components.MenuScroll{}
}

func (state *model) fileMenuTrigger() gfx.ID {
	if state.topExportOpen {
		return topExportMenuID
	}
	return fileMenuID
}

func (state *model) paintFontStepper(canvas *gfx.Canvas, palette colors, bounds ui.Rect) {
	local := state.local
	size := state.activeFontSize()
	canvas.RoundedBorder(bounds, 7, 1, palette.border, palette.sidebar)
	step := func(delta int) func() {
		return func() { state.runFileMenuEditingAction(func() { state.zoomFont(delta) }) }
	}
	minus := local("减小字号", "Decrease font size") + " · " + shortcutLabel("-")
	plus := local("增大字号", "Increase font size") + " · " + shortcutLabel("=") + " / " + shortcutLabel(local("+滚轮", "+Wheel"))
	reset := local("重置默认字号 (16px)", "Reset font size (16px)") + " · " + shortcutLabel("⌥0")
	iconButton(canvas, topFontDecreaseID, icons.Minus, "", minus, ui.Rect{X: bounds.X + 2, Y: bounds.Y + 1, Width: 24, Height: 22}, false, size <= session.MinFontSize, palette, step(-2))
	center := ui.Rect{X: bounds.X + 27, Y: bounds.Y + 1, Width: bounds.Width - 54, Height: 22}
	caption := "Aa " + strconv.Itoa(size)
	if canvas.FocusVisible() == topFontResetID {
		canvas.RoundedBorder(center, 5, 1, palette.accent, ui.Color{})
	}
	labeledButton(canvas, topFontResetID, caption, caption+" · "+reset, center, false, palette, step(0))
	iconButton(canvas, topFontIncreaseID, icons.Plus, "", plus, ui.Rect{X: bounds.X + bounds.Width - 26, Y: bounds.Y + 1, Width: 24, Height: 22}, false, size >= session.MaxFontSize, palette, step(2))
}

func (state *model) paintTopExportMenu(canvas *gfx.Canvas, viewport ui.Rect) {
	const width = 172
	anchor := state.fileMenuAnchor
	box := ui.Rect{X: max(4, anchor.X+anchor.Width-width), Y: 32, Width: width, Height: 90}
	theme, _ := state.look()
	theme.Metrics.FontSizeM = state.uiSize(14)
	options := components.DropdownMenuOptions{Layer: 9, DismissID: fileMenuBackdropID, Viewport: viewport, Scroll: &state.fileMenuScroll, RowHeight: 26}
	if state.pendingCompositionFocus().IsZero() {
		options.Scope = fileMenuScope
	}
	menu := components.BeginDropdownMenu(canvas, theme, box, options)
	defer menu.End()
	if canvas.Clicked(fileMenuBackdropID) {
		state.fileMenuOpen = false
		state.focusRequest = topExportMenuID
		if focus := state.pendingCompositionFocus(); !focus.IsZero() {
			state.focusRequest = focus
		}
	}
	for _, item := range []struct {
		id     gfx.ID
		label  string
		icon   gfx.Icon
		action func()
	}{
		{topExportPDFID, state.local("导出 PDF", "Export PDF"), icons.FileDown, state.exportActivePDF},
		{topExportDOCXID, state.local("导出 DOCX", "Export DOCX"), icons.FileDown, state.exportActiveDOCX},
		{topExportHTMLID, state.local("导出 HTML", "Export HTML"), icons.FileCode2, state.exportActiveHTML},
	} {
		menu.AddItem(components.DropdownMenuItem{ID: item.id, Label: item.label, Icon: item.icon})
		if canvas.Clicked(item.id) {
			state.fileMenuOpen = false
			state.runFileMenuEditingAction(item.action)
		}
	}
}

func (state *model) paintFileMenu(canvas *gfx.Canvas, palette colors, viewport ui.Rect) {
	theme, _ := state.look()
	theme.Metrics.FontSizeM = state.uiSize(14)
	local := state.local
	box := ui.Rect{X: state.fileMenuAnchor.X, Y: 36, Width: 248, Height: 535}
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
		state.paintEmptySidebar(canvas, palette, state.treeRect)
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

var emptySidebarOpenID = controlTree.ID(page, "empty-sidebar-open-folder")

func (state *model) paintEmptySidebar(canvas *gfx.Canvas, palette colors, bounds ui.Rect) {
	local := state.local
	center := bounds.X + bounds.Width/2
	top := bounds.Y + max(8, (bounds.Height-122)/2)
	canvas.Rounded(ui.Rect{X: center - 20, Y: top, Width: 40, Height: 40}, 10, palette.selected)
	canvas.DrawIcon(icons.FolderOpen, ui.Rect{X: center - 11, Y: top + 9, Width: 22, Height: 22}, palette.accent)
	title, subtitle := local("未打开工作区", "No workspace open"), local("打开文件夹开始整理", "Keep your files together")
	if current := state.session.Current(); current != nil && current.Path != "" {
		title, subtitle = local("独立文件", "Standalone file"), local("未关联工作区", "No workspace open")
	}
	for _, line := range []struct {
		label   string
		y, size float32
		weight  float32
		color   ui.Color
	}{
		{title, top + 43, 13, gfx.WeightSemibold, palette.ink},
		{subtitle, top + 68, 11, gfx.WeightRegular, palette.muted},
	} {
		size := palette.textSize(line.size)
		canvas.LabelCenter(ellipsizeWeight(canvas, line.label, bounds.Width-8, size, line.weight), ui.Rect{X: bounds.X, Y: line.y, Width: bounds.Width, Height: 22}, gfx.TextStyle{Size: size, Weight: line.weight, Color: line.color})
	}
	label := local("打开文件夹", "Open Folder")
	font := palette.textSize(12)
	width := min(bounds.Width-10, canvas.TextWidthWeight(label, font, gfx.WeightSemibold)+28)
	button := ui.Rect{X: center - width/2, Y: top + 93, Width: width, Height: 30}
	fill := palette.accent
	if canvas.Hovered() == emptySidebarOpenID {
		fill = fill.WithAlpha(0.86)
	}
	canvas.Rounded(button, 7, fill)
	if canvas.Focused() == emptySidebarOpenID {
		canvas.RoundedBorder(button.Inset(gfx.All(-2)), 9, 1, palette.accent, ui.Color{})
	}
	canvas.LabelCenter(label, button, gfx.TextStyle{Size: font, Weight: gfx.WeightSemibold, Color: palette.background})
	canvas.HitWithOptions(emptySidebarOpenID, button, label, gfx.HitOptions{Role: gfx.RoleButton})
	if canvas.Clicked(emptySidebarOpenID) {
		canvas.AfterPaint(state.openFolder)
	}
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
			tabBox := ui.Rect{X: x, Y: 33, Width: width - 3, Height: 31}
			canvas.Rounded(tabBox, 6, palette.background)
			canvas.Fill(ui.Rect{X: x + 8, Y: 33, Width: max(0, width-19), Height: 2}, palette.accent)
			canvas.Fill(ui.Rect{X: x, Y: 36, Width: 1, Height: 27}, palette.border.WithAlpha(0.7))
			canvas.Fill(ui.Rect{X: x + width - 4, Y: 36, Width: 1, Height: 27}, palette.border.WithAlpha(0.7))
		} else if canvas.Hovered() == tabRows.At(index) {
			canvas.Rounded(ui.Rect{X: x, Y: 35, Width: width - 3, Height: 26}, 5, palette.selected.WithAlpha(0.45))
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
	x, width := centeredEditorColumn(bounds, 872, 26)
	switch state.preferences.EditorWidth {
	case "wide":
		x, width = centeredEditorColumn(bounds, 1112, 26)
	case "full":
		x, width = centeredEditorColumn(bounds, bounds.Width, 32)
	}
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
	readingSize := state.readingSize()
	bodyLineHeight := state.markdownBodyLineHeight(readingSize)
	if source == "" && !(state.mode == renderedMode && state.rich.active && state.rich.virtual) {
		canvas.LabelCenter("开始写作…", ui.Rect{X: bounds.X, Y: bounds.Y + bounds.Height/3, Width: bounds.Width, Height: max(36, bodyLineHeight)}, gfx.TextStyle{Size: readingSize, Color: palette.muted})
	}
	x, width := state.markdownColumn(bounds)
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
		lineSpacing: state.preferences.LineSpacing, editorWidth: state.preferences.EditorWidth,
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
		state.preview.Layout(canvas, components.MarkdownStyle{Size: readingSize, LineHeight: bodyLineHeight, MonoFace: markdownCodeFace, Highlight: markdownCodeRuns}, &state.parsed, width, images, state.markdownInlineBlocks, activeTable)
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
		textInset, size, weight := renderedTextGeometry(&state.parsed, index, ui.Rect{Width: width}, state.dark, readingSize)
		lineHeight := bodyLineHeight
		if block.Kind == markdown.Heading {
			_, lineHeight = markdownHeadingGeometry(block.Level, state.dark, readingSize)
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
	textHeight := bodyLineHeight
	if state.mode == renderedMode && state.rich.active && state.virtualParagraphNeedsPlaceholder() {
		virtualWidth := width
		if state.rich.block.Kind == markdownedit.Quote {
			virtualWidth -= 14
		}
		state.rich.wrapRows = canvas.WrapRows(state.rich.wrapRows[:0], state.rich.input.String(), readingSize, gfx.WeightRegular, virtualWidth)
		textHeight = float32(max(1, len(state.rich.wrapRows))) * bodyLineHeight
		virtualIndex, virtualDepth = state.rich.index, state.virtualParagraphDepth()
	} else if state.mode == renderedMode && state.selection.active && state.selection.anchorVirtual != nil && state.selection.documentID == current.ID && state.selection.source == current.Text() {
		virtualIndex, virtualDepth = state.selection.anchorIndex, state.selection.anchorVirtual.depth
	}
	if virtualIndex >= 0 && virtualIndex <= len(state.preview.blocks) {
		state.preview.layoutKey = previewLayoutKey{}
		paragraphGap := markdownParagraphGap(state.dark, 0)
		gap := float32(virtualDepth)*(bodyLineHeight+paragraphGap) + textHeight + paragraphGap
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
	current := state.session.Current()
	if current == nil {
		return
	}
	style := gfx.TextStyle{Size: state.uiSize(11), Color: palette.muted}
	local := state.local
	left, right := float32(14), size.Width-14
	row := func(x, width float32) ui.Rect { return ui.Rect{X: x, Y: bar.Y + 1, Width: width, Height: 24} }
	textWidth := func(label string) float32 { return canvas.TextWidth(label, style.Size) + 1 }
	labelRight := func(label string) {
		width := textWidth(label)
		right -= width
		canvas.Label(label, row(right, width), style)
	}
	separator := func() {
		right -= 9
		canvas.Fill(ui.Rect{X: right, Y: bar.Y + 7, Width: 1, Height: 12}, palette.border)
		right -= 9
	}
	message := state.message
	if current.ReadOnly() {
		labelRight("局部预览 · UTF-8")
		if message == "" {
			message = largePreviewText
		}
	} else {
		labelRight("UTF-8")
		separator()
		mode := local("排版模式", "Edit")
		switch state.mode {
		case editMode:
			mode = local("源码模式", "Source")
		case splitMode:
			mode = local("分栏模式", "Split")
		}
		modeWidth := textWidth(mode) + 10
		right -= modeWidth
		if state.canUseViewMode(renderedMode) {
			labeledButton(canvas, statusModeID, mode, mode+" · "+local("切换模式", "Switch view mode"), row(right, modeWidth), false, palette, func() {
				modes := [...]viewMode{renderedMode, splitMode, editMode, readMode}
				for index, candidate := range modes {
					if candidate == state.mode {
						for step := 1; step < len(modes); step++ {
							next := modes[(index+step)%len(modes)]
							if state.canUseViewMode(next) {
								state.setViewMode(next)
								return
							}
						}
					}
				}
			})
		} else {
			canvas.Label(mode, row(right, modeWidth), style)
		}
		separator()
		info := state.statusDocumentStats(message == "")
		line, column := state.statusCaret(canvas.Focused())
		labelRight(fmt.Sprintf(local("光标 L%d:C%d", "L%d:C%d"), line, column))
		separator()
		infoWidth := min(size.Width*.34, textWidth(info))
		right -= infoWidth
		canvas.Label(ellipsize(canvas, info, infoWidth, style.Size), row(right, infoWidth), style)
	}
	available := max(0, right-left-16)
	badge := ""
	badgeStyle := style
	if current.Dirty() {
		badge = local("未保存", "Unsaved")
		badgeStyle.Color = ui.RGB(145, 86, 19)
		if state.dark {
			badgeStyle.Color = ui.RGB(245, 181, 96)
		}
		if strings.HasPrefix(message, "已保存 ") || strings.HasPrefix(message, "Saved ") {
			message = ""
		}
	} else if current.Path != "" && !current.ReadOnly() {
		badge = local("已保存", "Saved")
	}
	badgeWidth := float32(0)
	if badge != "" {
		badgeWidth = textWidth(badge) + 10
	}
	titleWidth := max(0, available-badgeWidth)
	if message != "" {
		titleWidth = min(titleWidth, available*.3, max(48, available-badgeWidth-textWidth(message)-8))
	}
	titleWidth = min(titleWidth, textWidth(current.Title))
	canvas.Label(ellipsize(canvas, current.Title, titleWidth, style.Size), row(left, titleWidth), style)
	left += titleWidth + 8
	if badge != "" {
		canvas.Label(badge, row(left, badgeWidth), badgeStyle)
		left += badgeWidth
	}
	if message != "" {
		width := max(0, right-left-12)
		canvas.Label(ellipsize(canvas, message, width, style.Size), row(left, width), style)
	}
}

func (state *model) recoveryStatusWarning() string {
	if state.recoveryLoadErr != nil {
		return state.local("恢复快照不可用：", "Recovery snapshot unavailable: ") + state.recoveryLoadErr.Error()
	}
	if state.recoveryWriteErr != nil {
		return state.local("恢复快照失败：", "Recovery snapshot failed: ") + state.recoveryWriteErr.Error()
	}
	return ""
}

func (state *model) paintRecoveryStatusWarning(canvas *gfx.Canvas, palette colors, size ui.Size, warning string) {
	bounds := ui.Rect{Y: size.Height - 48, Width: size.Width, Height: 22}
	canvas.Fill(bounds, palette.bar)
	canvas.Fill(ui.Rect{Y: bounds.Y, Width: bounds.Width, Height: 1}, palette.border)
	textSize := state.uiSize(11)
	textBounds := bounds.Inset(gfx.Insets{Left: 14, Right: 14})
	canvas.Label(ellipsize(canvas, warning, textBounds.Width, textSize), textBounds, gfx.TextStyle{Size: textSize, Color: palette.ink})
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
