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
		foreground = palette.muted.WithAlpha(0.5)
	} else if active {
		canvas.Rounded(bounds, 5, palette.selected)
		foreground = palette.accent
	} else if canvas.Hovered() == id {
		canvas.Rounded(bounds, 5, palette.selected.WithAlpha(0.65))
	}
	iconBounds := ui.Rect{X: bounds.X + (bounds.Width-16)/2, Y: bounds.Y + (bounds.Height-16)/2, Width: 16, Height: 16}
	if caption != "" {
		iconBounds.X = bounds.X + 8
		size := palette.textSize(12)
		canvas.Label(ellipsizeWeight(canvas, caption, bounds.Width-33, size, gfx.WeightMedium), ui.Rect{X: bounds.X + 29, Y: bounds.Y, Width: bounds.Width - 33, Height: bounds.Height}, gfx.TextStyle{Size: size, Weight: gfx.WeightMedium, Color: foreground})
	}
	canvas.DrawIcon(icon, iconBounds, foreground)
	if canvas.FocusVisible() == id && !disabled {
		canvas.RoundedBorder(bounds, 5, 1, palette.accent, ui.Color{})
	}
	canvas.HitWithOptions(id, bounds, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: label, Disabled: disabled, Selected: active})
	if !disabled && canvas.Clicked(id) && action != nil {
		action()
	}
}
