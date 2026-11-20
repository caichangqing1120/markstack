package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"markstack/internal/session"
)

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
	state.fileMenuOpen = false
	state.recentOpen = false
	state.settingsOpen = true
	state.invalidator.Invalidate()
}

func (state *model) changeSettings(next session.Preferences) {
	if err := state.applyPreferences(next); err != nil {
		state.settingsError = settingsErrorText(err)
		state.message = state.settingsError
		return
	}
	if state.message == state.settingsError {
		state.message = ""
	}
	state.settingsError = ""
}

func settingsErrorText(err error) string {
	switch {
	case errors.Is(err, session.ErrUnsafePreferencesPath):
		return "设置文件路径或权限不安全，未更改"
	case errors.Is(err, session.ErrCorruptPreferences):
		return "设置文件已损坏，未覆盖原文件"
	default:
		return "设置保存失败：" + err.Error()
	}
}

func (state *model) paintSettings(canvas *gfx.Canvas, theme ui.Theme, palette colors, viewport ui.Rect) {
	if !state.settingsOpen {
		return
	}
	canvas.PushLayer(11)
	canvas.PushFocusScope(settingsScope)
	canvas.Fill(viewport, ui.RGBA(15, 19, 22, 0.45))
	canvas.HitWithOptions(settingsBackdropID, viewport, "关闭设置", gfx.HitOptions{Role: gfx.RoleButton, Backdrop: true})
	if canvas.Clicked(settingsBackdropID) {
		state.settingsOpen = false
	}
	width := min(float32(440), viewport.Width-32)
	box := ui.Rect{X: (viewport.Width - width) / 2, Y: (viewport.Height - 360) / 2, Width: width, Height: 360}
	canvas.RoundedBorder(box, 6, 1, palette.border, palette.background)
	canvas.Label("设置", ui.Rect{X: box.X + 20, Y: box.Y + 15, Width: 160, Height: 30}, gfx.TextStyle{Size: 16, Weight: gfx.WeightSemibold, Color: palette.ink})
	labeledButton(canvas, settingsCloseID, "×", "关闭设置", ui.Rect{X: box.X + width - 44, Y: box.Y + 16, Width: 24, Height: 24}, false, palette, func() { state.settingsOpen = false })
	label := func(value string, y float32) {
		canvas.Label(value, ui.Rect{X: box.X + 22, Y: y, Width: 120, Height: 26}, gfx.TextStyle{Size: 13, Color: palette.ink})
	}
	label("外观", box.Y+64)
	labeledButton(canvas, appearanceLightID, "浅色", "使用浅色外观", ui.Rect{X: box.X + 150, Y: box.Y + 64, Width: 86, Height: 27}, !state.dark, palette, func() {
		next := state.preferences
		next.Dark = false
		state.changeSettings(next)
	})
	labeledButton(canvas, appearanceDarkID, "深色", "使用深色外观", ui.Rect{X: box.X + 240, Y: box.Y + 64, Width: 86, Height: 27}, state.dark, palette, func() {
		next := state.preferences
		next.Dark = true
		state.changeSettings(next)
	})
	fontStepper := func(y float32, size int, kind string, decrease, increase gfx.ID, update func(int)) {
		findCommand(canvas, decrease, "−", "减小"+kind+"字号", ui.Rect{X: box.X + 150, Y: y, Width: 30, Height: 27}, false, size > 14, palette, func() { update(size - 2) })
		canvas.LabelCenter(strconv.Itoa(size), ui.Rect{X: box.X + 185, Y: y, Width: 48, Height: 27}, gfx.TextStyle{Size: 13, Color: palette.ink})
		findCommand(canvas, increase, "+", "增大"+kind+"字号", ui.Rect{X: box.X + 238, Y: y, Width: 30, Height: 27}, false, size < 22, palette, func() { update(size + 2) })
	}
	label("源码字号", box.Y+112)
	fontStepper(box.Y+112, state.preferences.SourceSize, "源码", sourceSizeDecreaseID, sourceSizeIncreaseID, func(size int) {
		next := state.preferences
		next.SourceSize = size
		state.changeSettings(next)
	})
	label("阅读字号", box.Y+159)
	fontStepper(box.Y+159, state.preferences.ReadingSize, "阅读", readingSizeDecreaseID, readingSizeIncreaseID, func(size int) {
		next := state.preferences
		next.ReadingSize = size
		state.changeSettings(next)
	})
	label("阅读主题", box.Y+206)
	labeledButton(canvas, readingStandardID, "标准", "标准阅读主题", ui.Rect{X: box.X + 150, Y: box.Y + 206, Width: 86, Height: 27}, state.preferences.ReadingTheme == "standard", palette, func() {
		next := state.preferences
		next.ReadingTheme = "standard"
		state.changeSettings(next)
	})
	labeledButton(canvas, readingSoftID, "柔和", "柔和阅读主题", ui.Rect{X: box.X + 240, Y: box.Y + 206, Width: 86, Height: 27}, state.preferences.ReadingTheme == "soft", palette, func() {
		next := state.preferences
		next.ReadingTheme = "soft"
		state.changeSettings(next)
	})
	label("自动保存", box.Y+253)
	if components.BeginSwitch(canvas, theme, autoSaveToggleID, ui.Rect{X: box.X + 166, Y: box.Y + 255, Width: 32, Height: 18}, state.preferences.AutoSave).Label("自动保存").Paint().Clicked() {
		next := state.preferences
		next.AutoSave = !next.AutoSave
		state.changeSettings(next)
	}
	if state.settingsError != "" {
		canvas.Label(ellipsize(canvas, state.settingsError, width-44, 11), ui.Rect{X: box.X + 22, Y: box.Y + 313, Width: width - 44, Height: 24}, gfx.TextStyle{Size: 11, Color: palette.accent})
	}
	canvas.SetInitialFocus(settingsCloseID)
	canvas.SetEscapeTarget(settingsCloseID)
	canvas.TrapFocus(settingsScope)
	canvas.PopFocusScope()
	canvas.PopLayer()
}
