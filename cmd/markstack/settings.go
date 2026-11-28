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

	"markstack/internal/icons"
	"markstack/internal/session"
)

var (
	lineSpacingCompactID      = controlTree.ID(page, "line-spacing-compact")
	lineSpacingStandardID     = controlTree.ID(page, "line-spacing-standard")
	lineSpacingRelaxedID      = controlTree.ID(page, "line-spacing-relaxed")
	editorWidthStandardID     = controlTree.ID(page, "editor-width-standard")
	editorWidthWideID         = controlTree.ID(page, "editor-width-wide")
	editorWidthFullID         = controlTree.ID(page, "editor-width-full")
	typewriterToggleID        = controlTree.ID(page, "typewriter-toggle")
	focusModeToggleID         = controlTree.ID(page, "focus-mode-toggle")
	menuTypewriterID          = controlTree.ID(page, "menu-typewriter")
	menuFocusModeID           = controlTree.ID(page, "menu-focus-mode")
	settingsGeneralID         = controlTree.ID(page, "settings-general")
	settingsWritingID         = controlTree.ID(page, "settings-writing")
	settingsAboutID           = controlTree.ID(page, "settings-about")
	settingsScrollbarID       = controlTree.ID(page, "settings-scrollbar")
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
	section   settingsSection
	reset     bool
	feedback  string
	scroll    float32
	scrollbar components.ScrollAreaGeometry
	dragging  bool
	thumbGrab float32
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
	return float32(state.preferences.ReadingSize)
}

func (state *model) markdownBodyLineHeight(size float32) float32 {
	switch state.preferences.LineSpacing {
	case "compact":
		return size * 1.45
	case "relaxed":
		return size * 1.85
	}
	if state.preferences.ReadingTheme == "academic" {
		return size * (29.0 / 16)
	}
	return size * (26.0 / 16)
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
	if !state.dark {
		palette.muted = ui.RGB(100, 112, 134)
		theme.MutedForeground = palette.muted
	}
	view := settingsView{state: state, canvas: canvas, theme: theme, palette: palette}
	canDismiss := !state.updates.installing
	closeLabel := state.local("关闭设置", "Close Settings")
	canvas.HitWithOptions(settingsBackdropID, viewport, closeLabel, gfx.HitOptions{Role: gfx.RoleButton, Backdrop: true, Disabled: !canDismiss})
	if canDismiss && canvas.Clicked(settingsBackdropID) {
		state.dismissSettings()
		return
	}
	const sidebarWidth = 184
	width, height := min(float32(760), viewport.Width-48), min(float32(520), viewport.Height-48)
	box := ui.Rect{X: viewport.X + (viewport.Width-width)/2, Y: viewport.Y + (viewport.Height-height)/2, Width: width, Height: height}
	canvas.Elevate(box, 12, gfx.ShadowLG)
	canvas.Rounded(box, 12, palette.background)
	canvas.Clip(ui.Rect{X: box.X, Y: box.Y, Width: sidebarWidth, Height: box.Height})
	canvas.Rounded(box, 12, palette.sidebar)
	canvas.Unclip()
	canvas.RoundedBorder(box, 12, 1, palette.border, ui.Color{})
	canvas.Fill(ui.Rect{X: box.X + sidebarWidth, Y: box.Y + 1, Width: 1, Height: height - 2}, palette.border)
	canvas.Block(box)
	panel := &state.settings
	local := state.local
	view.label(local("设置", "Settings"), ui.Rect{X: box.X + 20, Y: box.Y + 14, Width: sidebarWidth - 40, Height: 32}, 16, gfx.WeightSemibold, palette.ink)
	closeBounds := ui.Rect{X: box.X + width - 48, Y: box.Y + 16, Width: 30, Height: 30}
	iconButton(canvas, settingsCloseID, icons.X, "", closeLabel, closeBounds, false, !canDismiss, palette, state.dismissSettings)
	if canDismiss {
		canvas.SetInitialFocus(settingsCloseID)
		canvas.SetEscapeTarget(settingsCloseID)
	}
	tabs := [...]struct {
		id             gfx.ID
		icon           gfx.Icon
		name, subtitle string
	}{
		{settingsGeneralID, icons.Settings2, local("外观与通用", "General"), local("外观、语言与应用偏好", "Appearance, language, and app preferences")},
		{settingsWritingID, icons.Pencil, local("写作与导出", "Writing & Export"), local("编辑体验、阅读主题与 PDF 排版", "Editor behavior, reading themes, and PDF layout")},
		{settingsImageHostID, icons.FileInput, local("图片上传", "Image Upload"), local("本地保存与云端对象存储配置", "Local saving and cloud upload settings")},
		{settingsAboutID, icons.History, local("关于与更新", "About & Updates"), local("版本、诊断信息与软件更新", "Version, diagnostics, and software updates")},
	}
	navEnabled := canDismiss && !panel.reset
	if navEnabled {
		for index, item := range tabs {
			if section := settingsSection(index); section != panel.section && canvas.Clicked(item.id) {
				state.selectSettingsSection(section)
				break
			}
		}
	}
	for index, item := range tabs {
		bounds := ui.Rect{X: box.X + 10, Y: box.Y + 60 + float32(index)*38, Width: sidebarWidth - 20, Height: 34}
		view.navigation(item.id, item.icon, item.name, bounds, panel.section == settingsSection(index), navEnabled)
	}
	header := ui.Rect{X: box.X + sidebarWidth + 22, Y: box.Y + 13, Width: closeBounds.X - box.X - sidebarWidth - 32, Height: 28}
	active := tabs[panel.section]
	view.label(active.name, header, 17, gfx.WeightSemibold, palette.ink)
	header.Y += 27
	header.Height = 20
	view.label(active.subtitle, header, 12, gfx.WeightRegular, palette.muted)
	canvas.Fill(ui.Rect{X: box.X + sidebarWidth + 1, Y: box.Y + 68, Width: width - sidebarWidth - 2, Height: 1}, palette.border)
	feedback := panel.feedback
	if panel.section == settingsImageHost && state.imageHost.form != nil {
		feedback = state.imageHost.form.feedback
	} else if state.settingsError != "" {
		feedback = state.settingsError
	}
	body := ui.Rect{X: header.X, Y: box.Y + 77, Width: width - sidebarWidth - 34, Height: height - 91}
	if feedback != "" {
		body.Height -= 27
		view.label(feedback, ui.Rect{X: body.X, Y: body.Y + body.Height + 4, Width: body.Width - 9, Height: 24}, 12, gfx.WeightRegular, palette.ink)
	}
	const gutter = 10
	contentWidth := body.Width - gutter - 4
	if panel.reset {
		panel.scrollbar = components.ScrollAreaGeometry{}
		body.Width = contentWidth
		view.paintReset(body)
		return
	}
	scrollTheme := theme
	scrollTheme.Border = palette.muted.WithAlpha(.8)
	area := components.BeginScrollArea(canvas, scrollTheme, body, ui.Size{Width: contentWidth, Height: view.contentHeight()}).
		Offset(0, panel.scroll).ScrollbarSize(gutter).ReserveScrollbars().
		VerticalScrollbar(settingsScrollbarID, local("滚动设置", "Scroll settings"), "")
	geometry := area.BeginContent()
	panel.scrollbar, panel.scroll = geometry, geometry.OffsetY
	content := ui.Rect{X: body.X, Y: body.Y - panel.scroll, Width: contentWidth}
	switch panel.section {
	case settingsGeneral:
		view.paintGeneral(content)
	case settingsWriting:
		view.paintWriting(content)
	case settingsImageHost:
		view.paintImageHost(content)
	case settingsAbout:
		view.paintAbout(content)
	}
	area.EndContent()
}

func (panel *settingsPanel) scrollTo(offset float32) gfx.EventResult {
	next := max(0, min(offset, panel.scrollbar.MaxOffsetY))
	changed := next != panel.scroll
	panel.scroll = next
	return gfx.EventResult{Handled: true, Repaint: changed}
}

func (panel *settingsPanel) scrollPointer(event gfx.PointerEvent) gfx.EventResult {
	switch event.Kind {
	case gfx.PointerDown:
		if event.Target != settingsScrollbarID || event.Button != gfx.PointerButtonPrimary {
			return gfx.EventResult{}
		}
		thumb := panel.scrollbar.VerticalThumb
		panel.dragging = true
		panel.thumbGrab = thumb.Height / 2
		if event.Y >= thumb.Y && event.Y < thumb.Y+thumb.Height {
			panel.thumbGrab = event.Y - thumb.Y
		}
	case gfx.PointerMove:
		if !panel.dragging {
			return gfx.EventResult{}
		}
	default:
		panel.dragging = false
		return gfx.EventResult{}
	}
	return panel.scrollTo(panel.scrollbar.VerticalOffsetForThumb(event.Y - panel.thumbGrab))
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

func (view settingsView) navigation(id gfx.ID, icon gfx.Icon, title string, bounds ui.Rect, active, enabled bool) {
	color, weight := view.palette.ink, float32(gfx.WeightMedium)
	if active {
		view.canvas.Rounded(bounds, 8, view.palette.selected)
		view.canvas.Rounded(ui.Rect{X: bounds.X + 3, Y: bounds.Y + 7, Width: 3, Height: 20}, 1.5, view.palette.accent)
		color, weight = view.palette.accent, gfx.WeightSemibold
	} else if enabled && view.canvas.Hovered() == id {
		view.canvas.Rounded(bounds, 8, view.palette.selected.WithAlpha(.55))
	}
	if !enabled {
		color = view.palette.muted
	}
	view.canvas.DrawIcon(icon, ui.Rect{X: bounds.X + 13, Y: bounds.Y + 10, Width: 14, Height: 14}, color)
	view.label(title, bounds.Inset(gfx.Insets{Left: 35, Right: 7}), 13, weight, color)
	view.control(id, title, bounds, 8, active, enabled, nil)
}

func (view settingsView) control(id gfx.ID, label string, bounds ui.Rect, radius float32, selected, enabled bool, action func()) {
	if enabled && view.canvas.FocusVisible() == id {
		view.canvas.RoundedBorder(bounds, radius, 1, view.palette.accent, ui.Color{})
	}
	view.canvas.HitWithOptions(id, bounds, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: label, Disabled: !enabled, Selected: selected})
	if enabled && action != nil && view.canvas.Clicked(id) {
		action()
	}
}

func (view settingsView) center(text string, bounds ui.Rect, size, weight float32, color ui.Color) {
	size = view.state.uiSize(size)
	view.canvas.LabelCenter(ellipsizeWeight(view.canvas, text, bounds.Width-10, size, weight), bounds, gfx.TextStyle{Size: size, Weight: weight, Color: color})
}

func (view settingsView) value(bounds ui.Rect, value string) {
	size := view.state.uiSize(12)
	value = ellipsizeWeight(view.canvas, value, bounds.Width, size, gfx.WeightRegular)
	width := view.canvas.TextWidthWeight(value, size, gfx.WeightRegular)
	bounds.X += max(0, bounds.Width-width-1)
	bounds.Width = width + 1
	view.canvas.Label(value, bounds, gfx.TextStyle{Size: size, Weight: gfx.WeightRegular, Color: view.palette.muted})
}

func (view settingsView) button(id gfx.ID, caption string, bounds ui.Rect, primary, enabled bool, action func()) {
	fill, border, color := view.palette.background, view.palette.border, view.palette.ink
	weight := float32(gfx.WeightMedium)
	switch {
	case !enabled:
		fill, color = view.palette.bar, view.palette.muted
	case primary:
		fill, border, color, weight = view.palette.accent, view.palette.accent, view.palette.background, gfx.WeightSemibold
		if view.canvas.Hovered() == id {
			fill = fill.WithAlpha(.86)
		}
	case view.canvas.Hovered() == id:
		fill = view.palette.selected
	}
	view.canvas.RoundedBorder(bounds, 7, 1, border, fill)
	view.center(caption, bounds, 12, weight, color)
	if primary && enabled && view.canvas.FocusVisible() == id {
		view.canvas.RoundedBorder(bounds.Inset(gfx.Insets{Left: 2, Top: 2, Right: 2, Bottom: 2}), 5, 1, color, ui.Color{})
	}
	view.control(id, caption, bounds, 7, false, enabled, action)
}

const (
	settingsRowHeight = 42
	settingsHeading   = 24
	settingsGroupGap  = 12
	settingsSwatches  = 84
	settingsInset     = 14
)

func settingsGroupHeight(rows int) float32 {
	return settingsHeading + float32(rows*settingsRowHeight)
}

func (view settingsView) contentHeight() float32 {
	switch view.state.settings.section {
	case settingsWriting:
		return settingsGroupHeight(5) + settingsHeading + settingsSwatches + settingsGroupHeight(2) + settingsGroupHeight(3) + 3*settingsGroupGap
	case settingsImageHost:
		return settingsGroupHeight(view.imageHostRows())
	case settingsAbout:
		return settingsGroupHeight(3) + settingsGroupHeight(3) + settingsGroupHeight(1) + 2*settingsGroupGap
	default:
		return settingsGroupHeight(4) + settingsGroupHeight(2) + settingsGroupGap
	}
}

func (view settingsView) heading(bounds ui.Rect, title string) {
	view.label(title, ui.Rect{X: bounds.X + 2, Y: bounds.Y, Width: bounds.Width - 4, Height: 20}, 12, gfx.WeightSemibold, view.palette.ink)
}

func (view settingsView) group(bounds ui.Rect, title string, rows int) ui.Rect {
	view.heading(bounds, title)
	bounds.Y += settingsHeading
	bounds.Height = float32(rows * settingsRowHeight)
	view.canvas.RoundedBorder(bounds, 8, 1, view.palette.border, view.palette.bar)
	return bounds
}

func (view settingsView) cardRow(card ui.Rect, index int) ui.Rect {
	row := ui.Rect{X: card.X + settingsInset, Y: card.Y + float32(index*settingsRowHeight), Width: card.Width - 2*settingsInset, Height: settingsRowHeight}
	if index > 0 {
		view.canvas.Fill(ui.Rect{X: row.X, Y: row.Y, Width: row.Width, Height: 1}, view.palette.border)
	}
	return row
}

func (view settingsView) row(card ui.Rect, index int, label string, controlWidth float32) ui.Rect {
	row := view.cardRow(card, index)
	controlWidth = min(controlWidth, row.Width)
	if label != "" {
		view.label(label, ui.Rect{X: row.X, Y: row.Y, Width: row.Width - controlWidth - 12, Height: row.Height}, 13, gfx.WeightMedium, view.palette.ink)
	}
	return ui.Rect{X: row.X + row.Width - controlWidth, Y: row.Y + 7, Width: controlWidth, Height: 28}
}

type settingOption struct {
	id                    gfx.ID
	value, caption, label string
}

func (view settingsView) options(bounds ui.Rect, current string, options []settingOption, apply func(*session.Preferences, string)) {
	view.segments(bounds, current, options, true, func(value string) {
		view.change(func(next *session.Preferences) { apply(next, value) })
	})
}

func (view settingsView) segments(bounds ui.Rect, current string, options []settingOption, enabled bool, apply func(string)) {
	view.canvas.RoundedBorder(bounds, 8, 1, view.palette.border, view.palette.sidebar)
	width := (bounds.Width - 4) / float32(len(options))
	for index, option := range options {
		rect := ui.Rect{X: bounds.X + 2 + float32(index)*width, Y: bounds.Y + 2, Width: width - 1, Height: bounds.Height - 4}
		label := option.label
		if label == "" {
			label = option.caption
		}
		active := current == option.value
		color, weight := view.palette.ink, float32(gfx.WeightMedium)
		if active {
			view.canvas.Elevate(rect, 6, gfx.ShadowXS)
			view.canvas.RoundedBorder(rect, 6, 1, view.palette.border, view.palette.background)
			color, weight = view.palette.accent, gfx.WeightSemibold
		} else if enabled && view.canvas.Hovered() == option.id {
			view.canvas.Rounded(rect, 6, view.palette.selected.WithAlpha(.55))
		}
		if !enabled {
			color = view.palette.muted
		}
		view.center(option.caption, rect, 12, weight, color)
		view.control(option.id, label, rect, 6, active, enabled, func() { apply(option.value) })
	}
}

func (view settingsView) change(apply func(*session.Preferences)) {
	next := view.state.preferences
	apply(&next)
	view.state.changeSettings(next)
}

func (view settingsView) toggle(id gfx.ID, bounds ui.Rect, label string, checked bool, apply func(*session.Preferences)) {
	if view.switchControl(id, bounds, label, checked, true) {
		view.change(apply)
	}
}

func (view settingsView) switchControl(id gfx.ID, bounds ui.Rect, label string, checked, enabled bool) bool {
	track := ui.Rect{X: bounds.X + bounds.Width - 34, Y: bounds.Y + 5, Width: 34, Height: 18}
	theme := view.theme
	theme.Input = view.palette.muted
	toggle := components.BeginSwitch(view.canvas, theme, id, track, checked).HitBounds(bounds).Label(label)
	if !enabled {
		toggle = toggle.Disabled()
	}
	return toggle.Paint().Clicked()
}

func (view settingsView) paintGeneral(bounds ui.Rect) {
	state := view.state
	local := state.local
	prefs := state.preferences
	card := view.group(bounds, local("界面", "Interface"), 4)
	view.options(view.row(card, 0, local("外观", "Appearance"), 164), strconv.FormatBool(prefs.Dark), []settingOption{
		{appearanceLightID, "false", local("浅色", "Light"), local("使用浅色外观", "Use light appearance")},
		{appearanceDarkID, "true", local("深色", "Dark"), local("使用深色外观", "Use dark appearance")},
	}, func(next *session.Preferences, value string) { next.Dark = value == "true" })
	view.options(view.row(card, 1, local("界面语言", "Language"), 246), prefs.Language, []settingOption{
		{languageSystemID, "system", local("跟随系统", "System"), ""}, {languageChineseID, "zh-CN", "简体中文", ""}, {languageEnglishID, "en", "English", ""},
	}, func(next *session.Preferences, value string) { next.Language = value })
	view.options(view.row(card, 2, local("界面字号", "UI text size"), 246), prefs.UITextSize, []settingOption{
		{textSmallID, "small", local("小", "Small"), ""}, {textMediumID, "medium", local("中", "Medium"), ""}, {textLargeID, "large", local("大", "Large"), ""},
	}, func(next *session.Preferences, value string) { next.UITextSize = value })
	label := local("显示标签编号", "Show tab numbers")
	view.toggle(tabNumbersID, view.row(card, 3, label, 44), label, prefs.ShowTabNumbers, func(next *session.Preferences) { next.ShowTabNumbers = !next.ShowTabNumbers })
	bounds.Y += settingsGroupHeight(4) + settingsGroupGap
	card = view.group(bounds, local("偏好设置", "Preferences"), 2)
	openLabel, resetLabel := local("打开配置目录", "Open settings folder"), local("恢复默认设置", "Restore defaults")
	view.button(configDirectoryID, openLabel, view.row(card, 0, local("配置文件", "Configuration files"), 168), false, true, state.openConfigDirectory)
	view.button(resetPreferencesID, resetLabel, view.row(card, 1, local("默认偏好", "Default preferences"), 168), false, true, func() { state.settings.reset = true })
}

func (view settingsView) paintWriting(bounds ui.Rect) {
	local := view.state.local
	prefs := view.state.preferences
	card := view.group(bounds, local("写作与编辑", "Writing & Editing"), 5)
	stepper := func(index int, size int, label, minus, plus string, decrease, increase gfx.ID, apply func(*session.Preferences, int)) {
		row := view.row(card, index, label, 104)
		view.canvas.RoundedBorder(row, 8, 1, view.palette.border, view.palette.sidebar)
		button := ui.Rect{X: row.X + 2, Y: row.Y + 2, Width: 28, Height: row.Height - 4}
		iconButton(view.canvas, decrease, icons.Minus, "", minus, button, false, size <= session.MinFontSize, view.palette, func() {
			view.change(func(next *session.Preferences) { apply(next, max(session.MinFontSize, size-2)) })
		})
		value := ui.Rect{X: row.X + 30, Y: row.Y, Width: row.Width - 60, Height: row.Height}
		view.center(strconv.Itoa(size), value, 13, gfx.WeightSemibold, view.palette.ink)
		for _, edge := range []float32{value.X, value.X + value.Width - 1} {
			view.canvas.Fill(ui.Rect{X: edge, Y: row.Y + 6, Width: 1, Height: row.Height - 12}, view.palette.border)
		}
		button.X = row.X + row.Width - 30
		iconButton(view.canvas, increase, icons.Plus, "", plus, button, false, size >= session.MaxFontSize, view.palette, func() {
			view.change(func(next *session.Preferences) { apply(next, min(session.MaxFontSize, size+2)) })
		})
	}
	stepper(0, prefs.SourceSize, local("源码字号", "Source text size"), local("减小源码字号", "Decrease source text size"), local("增大源码字号", "Increase source text size"), sourceSizeDecreaseID, sourceSizeIncreaseID, func(next *session.Preferences, size int) { next.SourceSize = size })
	stepper(1, prefs.ReadingSize, local("正文字号", "Editor & reading size"), local("减小正文字号", "Decrease editor & reading size"), local("增大正文字号", "Increase editor & reading size"), readingSizeDecreaseID, readingSizeIncreaseID, func(next *session.Preferences, size int) { next.ReadingSize = size })
	label := local("自动保存", "Auto-save")
	view.toggle(autoSaveToggleID, view.row(card, 2, label, 44), label, prefs.AutoSave, func(next *session.Preferences) { next.AutoSave = !next.AutoSave })
	label = local("打字机模式", "Typewriter mode")
	view.toggle(typewriterToggleID, view.row(card, 3, label, 44), label, prefs.TypewriterMode, func(next *session.Preferences) { next.TypewriterMode = !next.TypewriterMode })
	label = local("专注模式", "Focus mode")
	view.toggle(focusModeToggleID, view.row(card, 4, label, 44), label, prefs.FocusMode, func(next *session.Preferences) { next.FocusMode = !next.FocusMode })
	bounds.Y += settingsGroupHeight(5) + settingsGroupGap
	view.paintReadingThemes(bounds)
	bounds.Y += settingsHeading + settingsSwatches + settingsGroupGap
	card = view.group(bounds, local("Markdown 排版", "Markdown layout"), 2)
	view.options(view.row(card, 0, local("行距", "Line spacing"), 246), prefs.LineSpacing, []settingOption{
		{lineSpacingCompactID, "compact", local("紧凑", "Compact"), ""},
		{lineSpacingStandardID, "standard", local("标准", "Standard"), ""},
		{lineSpacingRelaxedID, "relaxed", local("宽松", "Relaxed"), ""},
	}, func(next *session.Preferences, value string) { next.LineSpacing = value })
	view.options(view.row(card, 1, local("页面宽度", "Page width"), 246), prefs.EditorWidth, []settingOption{
		{editorWidthStandardID, "standard", local("标准", "Standard"), ""},
		{editorWidthWideID, "wide", local("宽屏", "Wide"), ""},
		{editorWidthFullID, "full", local("全宽", "Full"), ""},
	}, func(next *session.Preferences, value string) { next.EditorWidth = value })
	bounds.Y += settingsGroupHeight(2) + settingsGroupGap
	view.paintPDFExport(bounds)
}

func (view settingsView) paintReadingThemes(bounds ui.Rect) {
	local := view.state.local
	view.heading(bounds, local("阅读主题", "Reading Themes"))
	const gap = 8
	const height = (settingsSwatches - gap) / 2
	width := (bounds.Width - 2*gap) / 3
	for index, option := range []settingOption{
		{readingStandardID, "standard", local("标准", "Standard"), local("标准阅读主题", "Standard reading theme")},
		{readingSoftID, "soft", local("柔和", "Soft"), local("柔和阅读主题", "Soft reading theme")},
		{readingGitHubID, "github", "GitHub", local("GitHub 阅读主题", "GitHub reading theme")},
		{readingParchmentID, "parchment", local("羊皮纸", "Parchment"), local("羊皮纸阅读主题", "Parchment reading theme")},
		{readingAcademicID, "academic", local("典雅", "Classic"), local("典雅阅读主题", "Classic reading theme")},
		{readingNordID, "nord", "Nord", local("Nord 阅读主题", "Nord reading theme")},
	} {
		box := ui.Rect{X: bounds.X + float32(index%3)*(width+gap), Y: bounds.Y + settingsHeading + float32(index/3)*(height+gap), Width: width, Height: height}
		appearance := readingAppearance(option.value, view.state.dark, view.palette)
		active := view.state.preferences.ReadingTheme == option.value
		border, fill, color, weight := view.palette.border, view.palette.bar, view.palette.ink, float32(gfx.WeightMedium)
		if active {
			border, fill, color, weight = view.palette.accent, view.palette.selected, view.palette.accent, gfx.WeightSemibold
		} else if view.canvas.Hovered() == option.id {
			fill = view.palette.selected.WithAlpha(.55)
		}
		view.canvas.RoundedBorder(box, 8, 1, border, fill)
		swatch := ui.Rect{X: box.X + 9, Y: box.Y + 8, Width: 26, Height: 22}
		view.canvas.RoundedBorder(swatch, 5, 1, appearance.tableBorder, appearance.palette.background)
		view.canvas.LabelCenter("Aa", swatch, gfx.TextStyle{Size: 11, Weight: gfx.WeightSemibold, Color: appearance.palette.ink})
		view.label(option.caption, box.Inset(gfx.Insets{Left: 43, Right: 6}), 12, weight, color)
		view.control(option.id, option.label, box, 8, active, true, func() {
			view.change(func(next *session.Preferences) { next.ReadingTheme = option.value })
		})
	}
}

func (view settingsView) paintPDFExport(bounds ui.Rect) {
	local := view.state.local
	prefs := view.state.preferences
	card := view.group(bounds, local("PDF 导出", "PDF Export"), 3)
	label := local("文档页眉", "Running header")
	view.toggle(pdfHeaderID, view.row(card, 0, label, 44), label, prefs.PDFHeader, func(next *session.Preferences) { next.PDFHeader = !next.PDFHeader })
	label = local("页脚页码", "Page numbers")
	view.toggle(pdfFooterID, view.row(card, 1, label, 44), label, prefs.PDFFooter, func(next *session.Preferences) { next.PDFFooter = !next.PDFFooter })
	view.options(view.row(card, 2, local("排版样式", "Layout preset"), 246), prefs.PDFPreset, []settingOption{
		{pdfStandardID, "standard", local("标准", "Standard"), local("标准 PDF 排版", "Standard PDF layout")},
		{pdfCompactID, "compact", local("紧凑", "Compact"), local("紧凑 PDF 排版", "Compact PDF layout")},
		{pdfAcademicID, "academic", local("正式", "Formal"), local("正式 PDF 排版", "Formal PDF layout")},
	}, func(next *session.Preferences, value string) { next.PDFPreset = value })
}

func (view settingsView) paintReset(bounds ui.Rect) {
	state := view.state
	local := state.local
	card := view.group(bounds, local("恢复默认设置？", "Restore default settings?"), 2)
	view.label(local("只重置偏好设置，不修改文档或草稿。", "Only preferences are reset. Documents and drafts are unchanged."), view.cardRow(card, 0), 12, gfx.WeightRegular, view.palette.muted)
	cancel, confirm := local("取消", "Cancel"), local("恢复默认设置", "Restore defaults")
	actions := view.row(card, 1, "", 244)
	view.button(cancelResetPreferencesID, cancel, ui.Rect{X: actions.X, Y: actions.Y, Width: 90, Height: actions.Height}, false, true, state.dismissSettings)
	actions.X, actions.Width = actions.X+98, 146
	view.button(confirmResetPreferencesID, confirm, actions, true, true, func() {
		state.settings.reset = false
		state.changeSettings(session.DefaultPreferences())
		if state.settingsError == "" {
			state.showToast("已恢复默认设置", "Settings restored")
		}
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
