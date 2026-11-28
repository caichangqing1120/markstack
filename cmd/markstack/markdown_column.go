package main

import "github.com/vivaui/viva/ui"

func (state *model) markdownColumn(bounds ui.Rect) (left, width float32) {
	maximum := float32(860)
	if state.dark {
		maximum = 914
	}
	switch {
	case state.paneViewport >= 1800:
		maximum = 1200
	case state.paneViewport >= 1400:
		maximum = 1024
	}
	if state.preferences.EditorWidth == "wide" {
		maximum += 240
	} else if state.preferences.EditorWidth == "full" {
		return centeredEditorColumn(bounds, bounds.Width, 32)
	}
	return centeredEditorColumn(bounds, maximum, 30)
}

func centeredEditorColumn(bounds ui.Rect, maximum, inset float32) (left, width float32) {
	outer := min(max(float32(0), bounds.Width), maximum)
	padding := min(inset, max(float32(0), (outer-40)/2))
	return bounds.X + (bounds.Width-outer)/2 + padding, outer - 2*padding
}
