package main

import (
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"markstack/internal/icons"
	"markstack/internal/workspace"
)

var (
	paletteScope    = controlTree.ID(page, "command-palette")
	paletteInputID  = controlTree.ID(page, "command-palette-input")
	paletteBackdrop = controlTree.ID(page, "command-palette-backdrop")
	paletteRows     = controlTree.Sub(page, controlTree.Name("command-palette-rows"))
)

const (
	paletteVisibleRows = 8
	paletteRowHeight   = 40
)

type paletteEntry struct {
	zh, en, detail, shortcut string
	run                      func()
}

type commandPalette struct {
	query       *text.Editor
	input       app.TextInput
	files       bool
	returnFocus gfx.ID
	entries     []paletteEntry
	results     []int
	lastQuery   string
	selected    int
	first       int
	scroll      float32
	list        ui.Rect
}

func (state *model) paletteCommands() []paletteEntry {
	primary := "Ctrl+"
	if primaryModifier() == gfx.ModifierMeta {
		primary = "⌘"
	}
	commands := []paletteEntry{
		{zh: "新建文档", en: "New Document", shortcut: primary + "N", run: func() { state.newDocument(); state.deferEditingFocus() }},
		{zh: "打开文件夹", en: "Open Folder", run: state.openFolder},
		{zh: "保存", en: "Save", shortcut: primary + "S", run: func() { state.saveActive(false) }},
		{zh: "导出 PDF", en: "Export PDF", run: state.exportActivePDF},
		{zh: "导出 DOCX", en: "Export DOCX", run: state.exportActiveDOCX},
		{zh: "导出 HTML", en: "Export HTML", run: state.exportActiveHTML},
		{zh: "打开设置", en: "Open Settings", shortcut: primary + ",", run: state.openSettings},
		{zh: "打开图片上传设置", en: "Open Image Upload Settings", run: func() { state.openSettingsSection(settingsImageHost) }},
		{zh: "关于与更新", en: "Open About & Updates", run: func() { state.openSettingsSection(settingsAbout) }},
		{zh: "清理孤立图片", en: "Clean Orphan Images", run: func() { state.openMediaManager(); state.mediaManager.orphans = true }},
		{zh: "切换侧栏", en: "Toggle Sidebar", run: func() { state.sidebar = !state.sidebar }},
		{zh: "切换大纲", en: "Toggle Outline", run: func() { state.outline = !state.outline }},
		{zh: "切换明暗模式", en: "Toggle Light/Dark Mode", run: state.toggleTheme},
		{zh: "切换打字机模式", en: "Toggle Typewriter Mode", shortcut: primary + "Alt+T", run: func() { state.toggleWritingMode(true) }},
		{zh: "切换专注模式", en: "Toggle Focus Mode", shortcut: primary + "Alt+D", run: func() { state.toggleWritingMode(false) }},
		{zh: "视图：排版", en: "View: Edit", run: func() { state.switchPaletteView(renderedMode) }},
		{zh: "视图：源码", en: "View: Source", shortcut: primary + "Alt+1", run: func() { state.switchPaletteView(editMode) }},
		{zh: "视图：分栏", en: "View: Split", shortcut: primary + "Alt+2", run: func() { state.switchPaletteView(splitMode) }},
		{zh: "视图：阅读", en: "View: Read", shortcut: primary + "Alt+3", run: func() { state.switchPaletteView(readMode) }},
	}
	for _, theme := range []struct{ key, zh, en string }{
		{"standard", "标准", "Standard"}, {"soft", "柔和", "Soft"}, {"github", "GitHub", "GitHub"},
		{"parchment", "羊皮纸", "Parchment"}, {"academic", "典雅", "Classic"}, {"nord", "Nord", "Nord"},
	} {
		commands = append(commands, paletteEntry{zh: "阅读主题：" + theme.zh, en: "Reading Theme: " + theme.en, run: func() {
			next := state.preferences
			next.ReadingTheme = theme.key
			state.changeSettings(next)
		}})
	}
	return commands
}

func (state *model) switchPaletteView(mode viewMode) {
	current := state.session.Current()
	if current == nil || mode != editMode && (current.ReadOnly() || current.Kind != workspace.MarkdownKind && (current.Kind != workspace.RichHTMLKind || mode == splitMode)) {
		return
	}
	state.mode = mode
	if mode == editMode || mode == splitMode {
		state.deferEditingFocus()
	}
}

func (state *model) paletteFiles() []paletteEntry {
	entries := make([]paletteEntry, 0, len(state.session.Tabs)+len(state.session.Nodes))
	seen := make(map[string]bool)
	pathLabel := func(path string) string {
		if state.session.Root != "" {
			if relative, err := filepath.Rel(state.session.Root, path); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return filepath.ToSlash(relative)
			}
		}
		return filepath.ToSlash(path)
	}
	for _, tab := range state.session.Tabs {
		detail := state.local("打开的标签", "Open tab")
		if tab.Path != "" {
			seen[tab.Path] = true
			detail = pathLabel(tab.Path)
		}
		entries = append(entries, paletteEntry{zh: tab.Title, en: tab.Title, detail: detail, run: func() {
			if index := slices.Index(state.session.Tabs, tab); index >= 0 && index != state.session.Active && state.prepareDocumentNavigation() {
				state.session.Active = index
				state.resetDocumentView()
				state.deferEditingFocus()
			}
		}})
	}
	for _, node := range state.session.Nodes {
		if node.Dir || seen[node.Path] {
			continue
		}
		if kind := workspace.KindOf(node.Path); kind != workspace.MarkdownKind && kind != workspace.CodeKind {
			continue
		}
		entries = append(entries, paletteEntry{zh: node.Name, en: node.Name, detail: pathLabel(node.Path), run: func() {
			if state.openFile(node.Path) {
				state.deferEditingFocus()
			}
		}})
	}
	return entries
}

func paletteScore(query []rune, value string) int {
	matched, score, previous, index := 0, 0, -2, 0
	boundary := true
	for _, char := range value {
		if unicode.ToLower(char) == query[matched] {
			score += 10
			if boundary {
				score += 8
			}
			if index == previous+1 {
				score += 6
			}
			previous = index
			matched++
			if matched == len(query) {
				return max(1, score-min(index, 24))
			}
		}
		boundary = !unicode.IsLetter(char) && !unicode.IsDigit(char)
		index++
	}
	return 0
}

func (panel *commandPalette) filter() {
	if _, composing := panel.query.Composition(); composing {
		return
	}
	query := strings.Join(strings.Fields(strings.ToLower(panel.query.String())), "")
	if panel.results != nil && panel.lastQuery == query {
		return
	}
	panel.lastQuery, panel.selected, panel.first = query, 0, 0
	panel.scroll = 0
	panel.results = make([]int, 0, len(panel.entries))
	type match struct{ index, score int }
	matches := make([]match, 0, len(panel.entries))
	characters := []rune(query)
	for index, entry := range panel.entries {
		if query == "" {
			panel.results = append(panel.results, index)
		} else if score := max(paletteScore(characters, entry.zh), paletteScore(characters, entry.en), paletteScore(characters, entry.detail)/2); score > 0 {
			matches = append(matches, match{index, score})
		}
	}
	slices.SortStableFunc(matches, func(left, right match) int { return right.score - left.score })
	for _, item := range matches {
		panel.results = append(panel.results, item.index)
	}
}

func (state *model) openPalette(files bool, focus gfx.ID) gfx.EventResult {
	if composing := state.pendingFormatCompositionFocus(); !composing.IsZero() {
		return gfx.EventResult{Handled: true, Focus: composing}
	}
	if state.palette.query != nil {
		focus = state.palette.returnFocus
	}
	state.fileMenuOpen, state.headingMenuOpen = false, false
	state.slash = slashMenu{}
	panel := commandPalette{query: text.NewEditor(""), files: files, returnFocus: focus}
	if files {
		panel.entries = state.paletteFiles()
	} else {
		panel.entries = state.paletteCommands()
	}
	state.palette = panel
	state.focusRequest = gfx.ID{}
	return gfx.EventResult{Handled: true, Repaint: true, Focus: paletteInputID}
}

func (state *model) finishPalette(index int) gfx.EventResult {
	panel := state.palette
	state.palette = commandPalette{}
	state.focusRequest, state.focusReset = panel.returnFocus, false
	if index >= 0 && index < len(panel.results) {
		panel.entries[panel.results[index]].run()
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (state *model) onPaletteKey(event gfx.KeyEvent) gfx.EventResult {
	panel := &state.palette
	if _, composing := panel.query.Composition(); composing {
		return gfx.EventResult{}
	}
	panel.filter()
	switch event.Key {
	case gfx.KeyEscape:
		return state.finishPalette(-1)
	case gfx.KeyEnter:
		if len(panel.results) == 0 {
			return gfx.EventResult{Handled: true}
		}
		return state.finishPalette(panel.selected)
	case gfx.KeyDown, gfx.KeyUp:
		if count := len(panel.results); count != 0 {
			delta := 1
			if event.Key == gfx.KeyUp {
				delta = -1
			}
			panel.selected = (panel.selected + delta + count) % count
			panel.first = min(panel.selected, max(panel.first, panel.selected-paletteVisibleRows+1))
			panel.scroll = float32(panel.first * paletteRowHeight)
		}
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	return gfx.EventResult{}
}

func (panel *commandPalette) scrollResults(event gfx.ScrollEvent) gfx.EventResult {
	if !contains(panel.list, ui.Point{X: event.X, Y: event.Y}) || len(panel.results) <= paletteVisibleRows {
		return gfx.EventResult{Handled: true}
	}
	limit := float32((len(panel.results) - paletteVisibleRows) * paletteRowHeight)
	panel.scroll = max(0, min(limit, panel.scroll+event.StepY(paletteRowHeight)))
	first := int(panel.scroll / paletteRowHeight)
	changed := first != panel.first
	panel.first = first
	panel.selected = max(first, min(panel.selected, first+paletteVisibleRows-1))
	return gfx.EventResult{Handled: true, Repaint: changed}
}

func (state *model) paintCommandPalette(canvas *gfx.Canvas, theme ui.Theme, palette colors, viewport ui.Rect) {
	panel := &state.palette
	if panel.query == nil {
		return
	}
	panel.filter()
	local := state.local
	title, placeholder := local("命令面板", "Command Palette"), local("搜索命令…", "Search commands…")
	if panel.files {
		title, placeholder = local("快速打开文件", "Quick Open File"), local("搜索标签页和工作区文件…", "Search tabs and workspace files…")
	}
	rows := min(paletteVisibleRows, max(1, len(panel.entries)))
	width, height := min(float32(540), viewport.Width-32), float32(92+rows*paletteRowHeight)
	box := ui.Rect{X: (viewport.Width - width) / 2, Y: (viewport.Height - height) / 2, Width: width, Height: height}
	canvas.PushLayer(21)
	defer canvas.PopLayer()
	canvas.PushFocusScope(paletteScope)
	defer canvas.PopFocusScope()
	canvas.TrapFocus(paletteScope)
	canvas.Fill(viewport, ui.RGBA(15, 22, 34, 0.34))
	canvas.HitWithOptions(paletteBackdrop, viewport, local("关闭面板", "Close palette"), gfx.HitOptions{Role: gfx.RoleButton, Backdrop: true, SkipFocus: true})
	_, composing := panel.query.Composition()
	if !composing && canvas.Clicked(paletteBackdrop) {
		canvas.AfterPaint(func() { state.finishPalette(-1) })
	}
	canvas.Elevate(box, 12, gfx.ShadowLG)
	canvas.RoundedBorder(box, 12, 1, palette.border, palette.background)
	canvas.Block(box)
	canvas.DrawIcon(icons.Search, ui.Rect{X: box.X + 16, Y: box.Y + 15, Width: 16, Height: 16}, palette.accent)
	canvas.Label(title, ui.Rect{X: box.X + 40, Y: box.Y + 8, Width: width - 64, Height: 30}, gfx.TextStyle{Size: palette.textSize(13), Color: palette.ink, Weight: gfx.WeightSemibold})
	field := ui.Rect{X: box.X + 12, Y: box.Y + 40, Width: width - 24, Height: 35}
	focused := canvas.Focused() == paletteInputID
	components.PaintInput(canvas, theme, field, components.InputSnapshot{
		ID: paletteInputID, Label: placeholder, Placeholder: placeholder, Value: panel.query.String(),
		Focused: focused, PlatformEditing: focused && panel.query.String() != "", ShowCaret: focused, NavigationKeys: true,
	})
	canvas.SetInitialFocus(paletteInputID)
	canvas.Fill(ui.Rect{X: box.X + 1, Y: box.Y + 82, Width: width - 2, Height: 1}, palette.border)
	panel.list = ui.Rect{X: box.X + 8, Y: box.Y + 86, Width: width - 16, Height: float32(rows * paletteRowHeight)}
	if len(panel.results) == 0 {
		canvas.LabelCenter(local("无匹配结果", "No matches"), ui.Rect{X: box.X + 12, Y: box.Y + 84, Width: width - 24, Height: paletteRowHeight}, gfx.TextStyle{Size: palette.textSize(13), Color: palette.muted})
	}
	for index := panel.first; index < min(len(panel.results), panel.first+rows); index++ {
		entry := panel.entries[panel.results[index]]
		id := paletteRows.At(index)
		row := ui.Rect{X: panel.list.X, Y: panel.list.Y + float32(index-panel.first)*paletteRowHeight, Width: panel.list.Width, Height: paletteRowHeight - 2}
		if index == panel.selected || canvas.Hovered() == id {
			canvas.Rounded(row, 6, palette.selected)
		}
		label := local(entry.zh, entry.en)
		caption := row.Inset(gfx.Insets{Left: 10, Right: 10})
		if entry.shortcut != "" {
			badgeWidth := canvas.TextWidthWeight(entry.shortcut, 11, gfx.WeightMedium) + 14
			badge := ui.Rect{X: row.X + row.Width - badgeWidth - 10, Y: row.Y + 9, Width: badgeWidth, Height: 20}
			canvas.RoundedBorder(badge, 4, 1, palette.border, palette.bar)
			canvas.LabelCenter(entry.shortcut, badge, gfx.TextStyle{Size: 11, Weight: gfx.WeightMedium, Color: palette.muted})
			caption.Width -= badgeWidth + 8
		}
		if panel.files {
			caption.Height = 22
			canvas.Label(ellipsize(canvas, entry.detail, caption.Width, 10), ui.Rect{X: caption.X, Y: row.Y + 20, Width: caption.Width, Height: 14}, gfx.TextStyle{Size: 10, Color: palette.muted})
		}
		font := palette.textSize(13)
		canvas.Label(ellipsize(canvas, label, caption.Width, font), caption, gfx.TextStyle{Size: font, Color: palette.ink})
		canvas.HitWithOptions(id, row, label, gfx.HitOptions{Role: gfx.RoleButton, SkipFocus: true, Selected: index == panel.selected, Hint: entry.detail})
		if !composing && canvas.Clicked(id) {
			canvas.AfterPaint(func() { state.finishPalette(index) })
		}
	}
}
