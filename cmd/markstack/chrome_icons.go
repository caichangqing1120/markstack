package main

import (
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"markstack/internal/icons"
	"markstack/internal/session"
	"markstack/internal/workspace"
)

func shortcutLabel(key string) string {
	if runtime.GOOS == "darwin" {
		if key, ok := strings.CutPrefix(key, "⌥"); ok {
			return "⌥⌘" + key
		}
		return "⌘" + key
	}
	if key, ok := strings.CutPrefix(key, "⇧"); ok {
		return "Ctrl+Shift+" + key
	}
	if key, ok := strings.CutPrefix(key, "⌥"); ok {
		return "Ctrl+Alt+" + key
	}
	return "Ctrl+" + key
}

func (state *model) usesSourceFont() bool {
	current := state.session.Current()
	return state.mode == editMode || current != nil && (current.ReadOnly() || current.Kind == workspace.CodeKind || current.Kind == workspace.PlainKind)
}

func (state *model) activeFontSize() int {
	if state.usesSourceFont() {
		return state.preferences.SourceSize
	}
	return state.preferences.ReadingSize
}

func (state *model) zoomFont(delta int) {
	if !state.pendingCompositionFocus().IsZero() {
		return
	}
	current := state.activeFontSize()
	size := session.DefaultFontSize
	if delta != 0 {
		size = min(session.MaxFontSize, max(session.MinFontSize, current+delta))
	}
	if size == current {
		return
	}
	next := state.preferences
	zh, english := "正文字号 ", "Editor & reading size "
	if state.usesSourceFont() {
		next.SourceSize = size
		zh, english = "源码字号 ", "Source text size "
	} else {
		next.ReadingSize = size
	}
	state.changeSettings(next)
	if state.preferences == next {
		value := strconv.Itoa(size) + "px"
		state.showToast(zh+value, english+value)
	}
}

func (state *model) scrollFontZoom(event gfx.ScrollEvent) gfx.EventResult {
	result := gfx.EventResult{Handled: true, Repaint: true}
	if !state.pendingCompositionFocus().IsZero() {
		state.fontWheelDelta = 0
		return result
	}
	if event.DeltaY == 0 || event.Phase == gfx.ScrollPhaseMomentum || event.Phase == gfx.ScrollPhaseEnd {
		return result
	}
	if event.Precise {
		const threshold = 36
		now := time.Now()
		if now.Sub(state.fontWheelLast) > 250*time.Millisecond || state.fontWheelDelta*event.DeltaY < 0 {
			state.fontWheelDelta = 0
		}
		state.fontWheelLast = now
		state.fontWheelDelta += event.DeltaY
		if state.fontWheelDelta > -threshold && state.fontWheelDelta < threshold {
			return result
		}
		state.fontWheelDelta = 0
		if now.Sub(state.fontWheelStep) < 120*time.Millisecond {
			return result
		}
		state.fontWheelStep = now
	}
	state.fontWheelDelta = 0
	if event.DeltaY < 0 {
		state.zoomFont(2)
	} else {
		state.zoomFont(-2)
	}
	return result
}

func captionIcon(caption string) (gfx.Icon, bool) {
	switch caption {
	case "×":
		return icons.X, true
	case "✓":
		return icons.Check, true
	case "+":
		return icons.Plus, true
	case "−", "-":
		return icons.Minus, true
	case "‹":
		return icons.ChevronLeft, true
	case "›":
		return icons.ChevronRight, true
	case "⌕":
		return icons.Search, true
	case "↑":
		return icons.ArrowUp, true
	case "↓":
		return icons.ArrowDown, true
	case "←":
		return icons.ArrowLeft, true
	}
	return gfx.Icon{}, false
}

func iconButton(canvas *gfx.Canvas, id gfx.ID, icon gfx.Icon, caption, label string, bounds ui.Rect, active, disabled bool, palette colors, action func()) {
	foreground := palette.ink
	if disabled {
		foreground = palette.muted.WithAlpha(0.45)
	} else if active {
		canvas.Rounded(bounds, 6, palette.selected)
		foreground = palette.accent
	} else if canvas.Hovered() == id {
		canvas.Rounded(bounds, 6, palette.selected.WithAlpha(0.65))
	}
	const iconSize = float32(15)
	iconBounds := ui.Rect{X: bounds.X + (bounds.Width-iconSize)/2, Y: bounds.Y + (bounds.Height-iconSize)/2, Width: iconSize, Height: iconSize}
	if caption != "" {
		iconBounds.X = bounds.X + 8
		size := palette.textSize(12)
		canvas.Label(ellipsizeWeight(canvas, caption, bounds.Width-31, size, gfx.WeightMedium), ui.Rect{X: bounds.X + 27, Y: bounds.Y, Width: bounds.Width - 31, Height: bounds.Height}, gfx.TextStyle{Size: size, Weight: gfx.WeightMedium, Color: foreground})
	}
	canvas.DrawIcon(icon, iconBounds, foreground)
	if canvas.FocusVisible() == id && !disabled {
		canvas.RoundedBorder(bounds, 6, 1, palette.accent, ui.Color{})
	}
	canvas.HitWithOptions(id, bounds, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: label, Disabled: disabled, Selected: active})
	if !disabled && canvas.Clicked(id) && action != nil {
		action()
	}
}

func segmentButton(canvas *gfx.Canvas, id gfx.ID, icon gfx.Icon, label, hint string, bounds ui.Rect, active, disabled bool, palette colors, action func()) {
	foreground := palette.muted
	if disabled {
		foreground = palette.muted.WithAlpha(0.42)
	} else if active {
		canvas.RoundedBorder(bounds, 6, 1, palette.border, palette.background)
		foreground = palette.accent
	} else if canvas.Hovered() == id {
		canvas.Rounded(bounds, 6, palette.selected.WithAlpha(0.55))
		foreground = palette.ink
	}
	const iconSize = float32(14)
	iconBounds := ui.Rect{X: bounds.X + (bounds.Width-iconSize)/2, Y: bounds.Y + (bounds.Height-iconSize)/2, Width: iconSize, Height: iconSize}
	canvas.DrawIcon(icon, iconBounds, foreground)
	if canvas.FocusVisible() == id && !disabled {
		canvas.RoundedBorder(bounds, 6, 1, palette.accent, ui.Color{})
	}
	canvas.HitWithOptions(id, bounds, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: hint, Disabled: disabled, Selected: active})
	if !disabled && canvas.Clicked(id) && action != nil {
		action()
	}
}
