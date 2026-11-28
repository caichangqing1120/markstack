package main

import (
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

const toastDuration = 2400 * time.Millisecond

type toastState struct {
	label   string
	expires time.Duration
}

func (state *model) showToast(zh, english string) {
	state.toast = toastState{label: state.local(zh, english)}
	state.invalidator.Invalidate()
}

func (state *model) paintToast(canvas *gfx.Canvas, palette colors, viewport ui.Rect, statusHeight float32) {
	toast := &state.toast
	if toast.label == "" {
		return
	}
	now := canvas.Now()
	if toast.expires == 0 {
		toast.expires = now + toastDuration
	}
	if now >= toast.expires {
		*toast = toastState{}
		return
	}
	canvas.RequestFrameIn(toast.expires - now)
	canvas.PushLayer(22)
	defer canvas.PopLayer()
	font := palette.textSize(12)
	width := min(viewport.Width-32, canvas.TextWidthWeight(toast.label, font, gfx.WeightMedium)+44)
	pill := ui.Rect{X: viewport.Width - width - 16, Y: viewport.Height - statusHeight - 50, Width: width, Height: 34}
	canvas.Elevate(pill, 17, gfx.ShadowSM)
	canvas.RoundedBorder(pill, 17, 1, palette.border, palette.background)
	canvas.Rounded(ui.Rect{X: pill.X + 13, Y: pill.Y + 14, Width: 6, Height: 6}, 3, palette.accent)
	canvas.Label(ellipsize(canvas, toast.label, width-40, font), pill.Inset(gfx.Insets{Left: 27, Right: 13}), gfx.TextStyle{Size: font, Weight: gfx.WeightMedium, Color: palette.ink})
}
