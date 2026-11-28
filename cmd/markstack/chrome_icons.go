package main

import (
	"runtime"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"markstack/internal/icons"
)

func shortcutLabel(key string) string {
	if runtime.GOOS == "darwin" {
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

func segmentButton(canvas *gfx.Canvas, id gfx.ID, icon gfx.Icon, caption, label string, bounds ui.Rect, active, disabled bool, palette colors, action func()) {
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
	if caption != "" {
		size := palette.textSize(12)
		weight := gfx.WeightMedium
		if active {
			weight = gfx.WeightSemibold
		}
		textW := min(bounds.Width-26, canvas.TextWidthWeight(caption, size, weight))
		totalW := iconSize + 5 + textW
		startX := bounds.X + max(6, (bounds.Width-totalW)/2)
		iconBounds.X = startX
		canvas.Label(ellipsizeWeight(canvas, caption, bounds.Width-(startX-bounds.X)-iconSize-8, size, weight), ui.Rect{X: startX + iconSize + 5, Y: bounds.Y, Width: bounds.Width - (startX - bounds.X) - iconSize - 8, Height: bounds.Height}, gfx.TextStyle{Size: size, Weight: weight, Color: foreground})
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
