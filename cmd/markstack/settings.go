package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"markstack/internal/session"
)

var (
	typewriterToggleID        = controlTree.ID(page, "typewriter-toggle")
	focusModeToggleID         = controlTree.ID(page, "focus-mode-toggle")
	menuTypewriterID          = controlTree.ID(page, "menu-typewriter")
	menuFocusModeID           = controlTree.ID(page, "menu-focus-mode")
	settingsGeneralID         = controlTree.ID(page, "settings-general")
	settingsWritingID         = controlTree.ID(page, "settings-writing")
	settingsAboutID           = controlTree.ID(page, "settings-about")
	languageSystemID          = controlTree.ID(page, "language-system")
	languageChineseID         = controlTree.ID(page, "language-chinese")
	languageEnglishID         = controlTree.ID(page, "language-english")
	textSmallID               = controlTree.ID(page, "ui-text-small")
	textMediumID              = controlTree.ID(page, "ui-text-medium")
	textLargeID               = controlTree.ID(page, "ui-text-large")
	tabNumbersID              = controlTree.ID(page, "tab-numbers")
	pdfHeaderID               = controlTree.ID(page, "pdf-header")
	pdfFooterID               = controlTree.ID(page, "pdf-footer")
	pdfStandardID             = controlTree.ID(page, "pdf-standard")
	pdfCompactID              = controlTree.ID(page, "pdf-compact")
	pdfAcademicID             = controlTree.ID(page, "pdf-academic")
	configDirectoryID         = controlTree.ID(page, "config-directory")
	resetPreferencesID        = controlTree.ID(page, "reset-preferences")
	cancelResetPreferencesID  = controlTree.ID(page, "reset-preferences-cancel")
	confirmResetPreferencesID = controlTree.ID(page, "reset-preferences-confirm")
	readingGitHubID           = controlTree.ID(page, "reading-github")
	readingParchmentID        = controlTree.ID(page, "reading-parchment")
	readingAcademicID         = controlTree.ID(page, "reading-academic")
	readingNordID             = controlTree.ID(page, "reading-nord")
)

type settingsSection uint8

const (
	settingsGeneral settingsSection = iota
	settingsWriting
	settingsImageHost
	settingsAbout
)

type settingsPanel struct {
	section  settingsSection
	reset    bool
	feedback string
}

func (state *model) local(zh, english string) string {
	if state.preferences.Language == "en" || state.preferences.Language == "system" && state.systemEnglish {
		return english
	}
	return zh
}

func prefersEnglish(languages []string) bool {
	for _, language := range languages {
		base, _, _ := strings.Cut(strings.ToLower(language), "-")
		switch base {
		case "en":
			return true
		case "zh":
			return false
		}
	}
	return len(languages) != 0
}

func (state *model) uiSize(size float32) float32 {
	switch state.preferences.UITextSize {
	case "small":
		return size * .9
	case "large":
		return size * 1.15
	default:
		return size
	}
}

func (state *model) toggleWritingMode(typewriter bool) {
	next := state.preferences
	if typewriter {
		next.TypewriterMode = !next.TypewriterMode
	} else {
		next.FocusMode = !next.FocusMode
	}
	state.changeSettings(next)
}

func (state *model) enablePreferences(store session.PreferencesStore) error {
	state.settingsStore = &store
	loaded, err := store.Load()
	if err != nil {
		return err
	}
	state.preferences = loaded
	state.dark = loaded.Dark
	state.applyNativeAppearance()
	return nil
}

func (state *model) applyPreferences(next session.Preferences) error {
	if !next.Valid() {
		return session.ErrCorruptPreferences
	}
	if state.settingsStore != nil {
		if err := state.settingsStore.Save(next); err != nil {
			return err
		}
	}
	state.preferences = next
	if !next.TypewriterMode {
		state.writing = writingMotion{}
		state.sourceShift = 0
	}
	if !next.AutoSave {
		state.stopAutoSave()
	}
	state.dark = next.Dark
	state.applyNativeAppearance()
	state.invalidator.Invalidate()
	return nil
}

func (state *model) applyNativeAppearance() {
	appearance := app.AppearanceLight
	if state.dark {
		appearance = app.AppearanceDark
	}
	app.SetNativeAppearance(appearance)
}

func defaultPreferencesStore() (session.PreferencesStore, error) {
	if path := os.Getenv("MARKSTACK_SETTINGS_PATH"); path != "" {
		return session.PreferencesStore{Path: path}, nil
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return session.PreferencesStore{}, err
	}
	return session.PreferencesStore{Path: filepath.Join(root, "MarkStack", "settings.json")}, nil
}

func preferencesEnabled() bool {
	return os.Getenv("VIVA_HEADLESS") != "1" || os.Getenv("MARKSTACK_SETTINGS_PATH") != ""
}

func (state *model) sourceSize() float32 {
	return float32(state.preferences.SourceSize)
}

func (state *model) readingSize() float32 {
	if state.mode == readMode || state.mode == splitMode {
		return float32(state.preferences.ReadingSize)
	}
	return 16
}

func (state *model) readingPalette(palette colors) colors {
	if (state.mode != readMode && state.mode != splitMode) || state.preferences.ReadingTheme != "soft" {
		return palette
	}
	if state.dark {
		palette.background = ui.RGB(41, 47, 46)
		palette.ink = ui.RGB(233, 238, 235)
		palette.muted = ui.RGB(171, 184, 178)
	} else {
		palette.background = ui.RGB(241, 246, 244)
		palette.ink = ui.RGB(43, 55, 53)
		palette.muted = ui.RGB(107, 126, 120)
	}
	return palette
}

func (state *model) openSettings() {
	state.openSettingsSection(settingsGeneral)
}

func (state *model) openSettingsSection(section settingsSection) {
	state.fileMenuOpen = false
	state.recentOpen = false
	state.selectSettingsSection(section)
	state.settingsOpen = true
	state.invalidator.Invalidate()
}

func (state *model) selectSettingsSection(section settingsSection) {
	state.imageHost.closeForm()
	state.settings = settingsPanel{section: section}
	if section == settingsImageHost {
		state.initImageHostForm()
	}
}

func (state *model) changeSettings(next session.Preferences) {
	if err := state.applyPreferences(next); err != nil {
		state.settingsError = state.settingsErrorText(err)
		state.message = state.settingsError
		return
	}
	if state.message == state.settingsError {
		state.message = ""
	}
	state.settingsError = ""
}

func (state *model) settingsErrorText(err error) string {
	switch {
	case errors.Is(err, session.ErrUnsafePreferencesPath):
		return state.local("设置文件路径或权限不安全，未更改", "Settings path or permissions are unsafe; no changes made")
	case errors.Is(err, session.ErrCorruptPreferences):
		return state.local("设置文件已损坏，未覆盖原文件", "Settings file is corrupt; the original was not overwritten")
	default:
		return state.local("设置保存失败：", "Couldn't save settings: ") + err.Error()
	}
}

func (state *model) paintSettings(canvas *gfx.Canvas, theme ui.Theme, palette colors, viewport ui.Rect) {
	if !state.settingsOpen {
		return
	}
	canvas.PushLayer(11)
	canvas.PushFocusScope(settingsScope)
	canvas.Fill(viewport, ui.RGBA(15, 19, 22, 0.45))
	defer canvas.PopLayer()
	defer canvas.PopFocusScope()
	canvas.TrapFocus(settingsScope)
	view := settingsView{state: state, canvas: canvas, theme: theme, palette: palette}
	canDismiss := !state.updates.installing
	closeLabel := state.local("关闭设置", "Close Settings")
	canvas.HitWithOptions(settingsBackdropID, viewport, closeLabel, gfx.HitOptions{Role: gfx.RoleButton, Backdrop: true, Disabled: !canDismiss})
	if canDismiss && canvas.Clicked(settingsBackdropID) {
		state.dismissSettings()
	}
	width, height := min(float32(862), viewport.Width-28), min(float32(480), viewport.Height-12)
	box := ui.Rect{X: (viewport.Width - width) / 2, Y: (viewport.Height - height) / 2, Width: width, Height: height}
	canvas.RoundedBorder(box, 8, 1, palette.border, palette.background)
	view.label(state.local("设置", "Settings"), ui.Rect{X: box.X + 18, Y: box.Y + 7, Width: 142, Height: 30}, 18, gfx.WeightSemibold, palette.ink)
	closeBounds := ui.Rect{X: box.X + width - 40, Y: box.Y + 8, Width: 26, Height: 26}
	if canDismiss {
		labeledButton(canvas, settingsCloseID, "×", closeLabel, closeBounds, false, palette, state.dismissSettings)
		canvas.SetInitialFocus(settingsCloseID)
		canvas.SetEscapeTarget(settingsCloseID)
	}
	content := ui.Rect{X: box.X + 196, Y: box.Y + 40, Width: width - 211, Height: height - 63}
	if state.settings.reset {
		view.paintReset(content)
		return
	}
	canvas.Rounded(ui.Rect{X: box.X + 10, Y: box.Y + 40, Width: 174, Height: height - 53}, 7, palette.sidebar)
	for index, item := range []struct {
		id   gfx.ID
		name string
	}{
		{settingsGeneralID, state.local("外观与通用", "Appearance & General")},
		{settingsWritingID, state.local("写作与导出", "Writing & Export")},
		{settingsImageHostID, state.local("图床上传", "Image Host")},
		{settingsAboutID, state.local("关于与更新", "About & Updates")},
	} {
		section := settingsSection(index)
		view.button(item.id, item.name, item.name, ui.Rect{X: box.X + 14, Y: box.Y + 46 + float32(index)*34, Width: 166, Height: 30}, state.settings.section == section, canDismiss, func() {
			if state.settings.section != section {
				state.selectSettingsSection(section)
			}
		})
	}
	switch state.settings.section {
	case settingsGeneral:
		view.paintGeneral(content)
		content.Y += 182
		view.paintWriting(content, 27)
	case settingsWriting:
		const spacing = 30
		view.paintWriting(content, spacing)
		exportBounds := content
		exportBounds.Y += writingCardHeight(spacing) + 8
		view.paintPDFExport(exportBounds)
	case settingsImageHost:
		view.paintImageHost(content)
		return
	case settingsAbout:
		view.paintAbout(content)
	}
	feedback := state.settings.feedback
	if state.settingsError != "" {
		feedback = state.settingsError
	}
	view.label(feedback, ui.Rect{X: content.X, Y: box.Y + height - 27, Width: content.Width, Height: 22}, 11, gfx.WeightRegular, palette.accent)
}

func (state *model) dismissSettings() {
	if state.updates.installing {
		return
	}
	if state.settings.reset {
		state.settings.reset = false
	} else {
		state.imageHost.closeForm()
		state.settingsOpen = false
	}
}

type settingsView struct {
	state   *model
	canvas  *gfx.Canvas
	theme   ui.Theme
	palette colors
}

func (view settingsView) label(text string, bounds ui.Rect, size, weight float32, color ui.Color) {
	size = view.state.uiSize(size)
	view.canvas.Label(ellipsizeWeight(view.canvas, text, bounds.Width, size, weight), bounds, gfx.TextStyle{Size: size, Weight: weight, Color: color})
}

func (view settingsView) button(id gfx.ID, caption, label string, bounds ui.Rect, active, enabled bool, action func()) {
	fill := view.palette.bar
	if active || enabled && view.canvas.Hovered() == id {
		fill = view.palette.selected
	}
	view.canvas.RoundedBorder(bounds, 5, 1, view.palette.border, fill)
	if enabled && view.canvas.FocusVisible() == id {
		view.canvas.RoundedBorder(bounds, 5, 1, view.palette.accent, ui.Color{})
	}
	color := view.palette.ink
	if !enabled {
		color = view.palette.muted
	}
	size := view.state.uiSize(12)
	view.canvas.LabelCenter(ellipsizeWeight(view.canvas, caption, bounds.Width-8, size, gfx.WeightMedium), bounds, gfx.TextStyle{Size: size, Weight: gfx.WeightMedium, Color: color})
	view.canvas.HitWithOptions(id, bounds, label, gfx.HitOptions{Role: gfx.RoleButton, Disabled: !enabled, Selected: active})
	if enabled && view.canvas.Clicked(id) && action != nil {
		action()
	}
}

func (view settingsView) card(bounds ui.Rect, height float32, title string) {
	bounds.Height = height
	view.canvas.RoundedBorder(bounds, 7, 1, view.palette.border, view.palette.background)
	view.label(title, ui.Rect{X: bounds.X + 12, Y: bounds.Y + 3, Width: bounds.Width - 24, Height: 27}, 14, gfx.WeightSemibold, view.palette.ink)
}

func (view settingsView) row(bounds ui.Rect, offset float32, label string) ui.Rect {
	view.label(label, ui.Rect{X: bounds.X + 12, Y: bounds.Y + offset, Width: 144, Height: 26}, 13, gfx.WeightRegular, view.palette.ink)
	return ui.Rect{X: bounds.X + 160, Y: bounds.Y + offset, Width: bounds.Width - 172, Height: 26}
}

type settingOption struct {
	id                    gfx.ID
	value, caption, label string
}

func (view settingsView) options(bounds ui.Rect, current string, options []settingOption, apply func(*session.Preferences, string)) {
	width := min(float32(96), (bounds.Width-5*float32(len(options)-1))/float32(len(options)))
	for index, option := range options {
		rect := ui.Rect{X: bounds.X + float32(index)*(width+5), Y: bounds.Y, Width: width, Height: bounds.Height}
		label := option.label
		if label == "" {
			label = option.caption
		}
		view.button(option.id, option.caption, label, rect, current == option.value, true, func() {
			view.change(func(next *session.Preferences) { apply(next, option.value) })
		})
	}
}

func (view settingsView) change(apply func(*session.Preferences)) {
	next := view.state.preferences
	apply(&next)
	view.state.changeSettings(next)
}

func (view settingsView) toggle(id gfx.ID, bounds ui.Rect, label string, checked bool, apply func(*session.Preferences)) {
	track := ui.Rect{X: bounds.X, Y: bounds.Y + 4, Width: 34, Height: 18}
	target := ui.Rect{X: bounds.X - 12, Y: bounds.Y, Width: 58, Height: bounds.Height}
	if components.BeginSwitch(view.canvas, view.theme, id, track, checked).HitBounds(target).Label(label).Paint().Clicked() {
		view.change(apply)
	}
}

func (view settingsView) paintGeneral(bounds ui.Rect) {
	state := view.state
	local := state.local
	prefs := state.preferences
	view.card(bounds, 174, local("外观与通用", "Appearance & General"))
	view.options(view.row(bounds, 31, local("外观", "Appearance")), strconv.FormatBool(prefs.Dark), []settingOption{
		{appearanceLightID, "false", "GitHub", local("使用浅色外观", "Use light appearance")},
		{appearanceDarkID, "true", "Night", local("使用深色外观", "Use dark appearance")},
	}, func(next *session.Preferences, value string) { next.Dark = value == "true" })
	view.options(view.row(bounds, 59, local("界面语言", "Language")), prefs.Language, []settingOption{
		{languageSystemID, "system", local("跟随系统", "System"), ""}, {languageChineseID, "zh-CN", "简体中文", ""}, {languageEnglishID, "en", "English", ""},
	}, func(next *session.Preferences, value string) { next.Language = value })
	view.options(view.row(bounds, 87, local("界面字号", "UI text size")), prefs.UITextSize, []settingOption{
		{textSmallID, "small", local("小", "Small"), ""}, {textMediumID, "medium", local("中", "Medium"), ""}, {textLargeID, "large", local("大", "Large"), ""},
	}, func(next *session.Preferences, value string) { next.UITextSize = value })
	label := local("显示标签编号", "Show tab numbers")
	view.toggle(tabNumbersID, view.row(bounds, 115, label), label, prefs.ShowTabNumbers, func(next *session.Preferences) { next.ShowTabNumbers = !next.ShowTabNumbers })
	openLabel, resetLabel := local("打开配置目录", "Open settings folder"), local("恢复默认设置", "Restore defaults")
	view.button(configDirectoryID, openLabel, openLabel, ui.Rect{X: bounds.X + 12, Y: bounds.Y + 143, Width: 158, Height: 24}, false, true, state.openConfigDirectory)
	view.button(resetPreferencesID, resetLabel, resetLabel, ui.Rect{X: bounds.X + 178, Y: bounds.Y + 143, Width: 140, Height: 24}, false, true, func() { state.settings.reset = true })
}

func writingCardHeight(spacing float32) float32 { return 63 + 6*spacing }

func (view settingsView) paintWriting(bounds ui.Rect, spacing float32) {
	local := view.state.local
	prefs := view.state.preferences
	view.card(bounds, writingCardHeight(spacing), local("写作与编辑", "Writing & Editing"))
	view.paintReadingThemeSample(bounds)
	stepper := func(offset float32, size int, label, minus, plus string, decrease, increase gfx.ID, apply func(*session.Preferences, int)) {
		row := view.row(bounds, offset, label)
		button := ui.Rect{X: row.X, Y: row.Y, Width: 28, Height: row.Height}
		view.button(decrease, "−", minus, button, false, size > 14, func() { view.change(func(next *session.Preferences) { apply(next, size-2) }) })
		button.X += 33
		button.Width = 37
		view.canvas.LabelCenter(strconv.Itoa(size), button, gfx.TextStyle{Size: view.state.uiSize(13), Color: view.palette.ink})
		button.X += 42
		button.Width = 28
		view.button(increase, "+", plus, button, false, size < 22, func() { view.change(func(next *session.Preferences) { apply(next, size+2) }) })
	}
	stepper(31, prefs.SourceSize, local("源码字号", "Source text size"), local("减小源码字号", "Decrease source text size"), local("增大源码字号", "Increase source text size"), sourceSizeDecreaseID, sourceSizeIncreaseID, func(next *session.Preferences, size int) { next.SourceSize = size })
	stepper(31+spacing, prefs.ReadingSize, local("阅读字号", "Reading text size"), local("减小阅读字号", "Decrease reading text size"), local("增大阅读字号", "Increase reading text size"), readingSizeDecreaseID, readingSizeIncreaseID, func(next *session.Preferences, size int) { next.ReadingSize = size })
	themeRow := view.row(bounds, 31+2*spacing, local("阅读主题", "Reading theme"))
	setTheme := func(next *session.Preferences, value string) { next.ReadingTheme = value }
	view.options(themeRow, prefs.ReadingTheme, []settingOption{
		{readingStandardID, "standard", local("标准", "Standard"), local("标准阅读主题", "Standard reading theme")},
		{readingSoftID, "soft", local("柔和", "Soft"), local("柔和阅读主题", "Soft reading theme")},
		{readingGitHubID, "github", "GitHub", local("GitHub 阅读主题", "GitHub reading theme")},
	}, setTheme)
	themeRow.Y += spacing
	view.options(themeRow, prefs.ReadingTheme, []settingOption{
		{readingParchmentID, "parchment", local("羊皮纸", "Parchment"), local("羊皮纸阅读主题", "Parchment reading theme")},
		{readingAcademicID, "academic", local("学术", "Academic"), local("学术阅读主题", "Academic reading theme")},
		{readingNordID, "nord", "Nord", local("Nord 阅读主题", "Nord reading theme")},
	}, setTheme)
	label := local("自动保存", "Auto-save")
	view.toggle(autoSaveToggleID, view.row(bounds, 31+4*spacing, label), label, prefs.AutoSave, func(next *session.Preferences) { next.AutoSave = !next.AutoSave })
	label = local("打字机模式", "Typewriter mode")
	view.toggle(typewriterToggleID, view.row(bounds, 31+5*spacing, label), label, prefs.TypewriterMode, func(next *session.Preferences) { next.TypewriterMode = !next.TypewriterMode })
	label = local("专注模式", "Focus mode")
	view.toggle(focusModeToggleID, view.row(bounds, 31+6*spacing, label), label, prefs.FocusMode, func(next *session.Preferences) { next.FocusMode = !next.FocusMode })
}

func (view settingsView) paintReadingThemeSample(bounds ui.Rect) {
	appearance := readingAppearance(view.state.preferences.ReadingTheme, view.state.dark, view.palette)
	box := ui.Rect{X: bounds.X + bounds.Width - 76, Y: bounds.Y + 5, Width: 64, Height: 22}
	view.canvas.RoundedBorder(box, 4, 1, appearance.tableBorder, appearance.palette.background)
	view.canvas.LabelCenter("Aa", ui.Rect{X: box.X + 3, Y: box.Y, Width: 27, Height: box.Height}, gfx.TextStyle{Size: 13, Color: appearance.palette.ink})
	view.canvas.Rounded(ui.Rect{X: box.X + 33, Y: box.Y + 4, Width: 25, Height: 14}, 3, appearance.codeFill)
	view.canvas.Fill(ui.Rect{X: box.X + 37, Y: box.Y + 10, Width: 17, Height: 2}, appearance.link)
}

func (view settingsView) paintPDFExport(bounds ui.Rect) {
	local := view.state.local
	prefs := view.state.preferences
	view.card(bounds, 116, local("PDF 导出", "PDF Export"))
	label := local("文档页眉", "Running header")
	view.toggle(pdfHeaderID, view.row(bounds, 31, label), label, prefs.PDFHeader, func(next *session.Preferences) { next.PDFHeader = !next.PDFHeader })
	label = local("页脚页码", "Page numbers")
	view.toggle(pdfFooterID, view.row(bounds, 59, label), label, prefs.PDFFooter, func(next *session.Preferences) { next.PDFFooter = !next.PDFFooter })
	view.options(view.row(bounds, 87, local("排版样式", "Layout preset")), prefs.PDFPreset, []settingOption{
		{pdfStandardID, "standard", local("标准", "Standard"), local("标准 PDF 排版", "Standard PDF layout")},
		{pdfCompactID, "compact", local("紧凑", "Compact"), local("紧凑 PDF 排版", "Compact PDF layout")},
		{pdfAcademicID, "academic", local("学术", "Academic"), local("学术 PDF 排版", "Academic PDF layout")},
	}, func(next *session.Preferences, value string) { next.PDFPreset = value })
}

func (view settingsView) paintReset(bounds ui.Rect) {
	state := view.state
	local := state.local
	view.card(bounds, 116, local("恢复默认设置？", "Restore default settings?"))
	view.label(local("只重置偏好设置，不修改文档或草稿。", "Only preferences are reset. Documents and drafts are unchanged."), ui.Rect{X: bounds.X + 12, Y: bounds.Y + 34, Width: bounds.Width - 24, Height: 24}, 12, gfx.WeightRegular, view.palette.muted)
	cancel, confirm := local("取消", "Cancel"), local("恢复默认设置", "Restore defaults")
	view.button(cancelResetPreferencesID, cancel, cancel, ui.Rect{X: bounds.X + 12, Y: bounds.Y + 74, Width: 90, Height: 28}, false, true, state.dismissSettings)
	view.button(confirmResetPreferencesID, confirm, confirm, ui.Rect{X: bounds.X + 112, Y: bounds.Y + 74, Width: 146, Height: 28}, true, true, func() {
		state.settings.reset = false
		state.changeSettings(session.DefaultPreferences())
	})
	view.canvas.SetInitialFocus(cancelResetPreferencesID)
	view.canvas.SetEscapeTarget(cancelResetPreferencesID)
}

func (state *model) openConfigDirectory() {
	var store session.PreferencesStore
	var err error
	if state.settingsStore == nil {
		store, err = defaultPreferencesStore()
	} else {
		store = *state.settingsStore
	}
	if err == nil {
		var directory string
		directory, err = store.Directory()
		if err == nil {
			target := directory
			if info, statErr := os.Lstat(store.Path); statErr == nil && info.Mode().IsRegular() {
				target = filepath.Join(directory, filepath.Base(store.Path))
			}
			err = app.RevealPath(target)
		}
	}
	state.settings.feedback = ""
	if err != nil {
		state.settings.feedback = state.local("无法打开配置目录：", "Couldn't open settings folder: ") + err.Error()
	}
}
