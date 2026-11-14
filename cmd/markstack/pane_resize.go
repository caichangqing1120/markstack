package main

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func (state *model) paneWidths(viewport float32) (sidebar, outline float32) {
	if state.sidebar {
		preferred := state.sidebarWidth
		if preferred == 0 {
			preferred = min(280, max(190, viewport*0.168))
		}
		sidebar = min(max(190, preferred), min(420, max(190, viewport-500)))
	}
	remaining := viewport - sidebar
	if state.outline && remaining > 770 {
		preferred := state.outlineWidth
		if preferred == 0 {
			preferred = 184
		}
		outline = min(max(160, preferred), min(360, remaining-500))
	}
	return sidebar, outline
}

func (state *model) onPanePointer(event gfx.PointerEvent) gfx.EventResult {
	if state.session.Pending() != nil || state.fileAction == fileActionTrashConfirm {
		state.paneDragging = gfx.ID{}
		return gfx.EventResult{}
	}
	if event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonPrimary && (event.Target == sidebarResizeID || event.Target == outlineResizeID) {
		state.paneDragging = event.Target
		return gfx.EventResult{Handled: true}
	}
	if state.paneDragging.IsZero() {
		return gfx.EventResult{}
	}
	if event.Kind == gfx.PointerUp {
		state.paneDragging = gfx.ID{}
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if event.Kind != gfx.PointerMove {
		return gfx.EventResult{}
	}
	sidebar, outline := state.paneWidths(event.Viewport.Width)
	if state.paneDragging == sidebarResizeID {
		state.sidebarWidth = min(max(190, event.X), min(420, max(190, event.Viewport.Width-500)))
	} else if outline > 0 {
		state.outlineWidth = min(max(160, event.Viewport.Width-event.X), min(360, event.Viewport.Width-sidebar-500))
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (state *model) onPaneKey(event gfx.KeyEvent) gfx.EventResult {
	if state.session.Pending() != nil || state.fileAction == fileActionTrashConfirm || event.Modifiers != 0 {
		return gfx.EventResult{}
	}
	if event.Target != sidebarResizeID && event.Target != outlineResizeID {
		return gfx.EventResult{}
	}
	direction := float32(0)
	switch event.Key {
	case gfx.KeyLeft:
		direction = -16
	case gfx.KeyRight:
		direction = 16
	default:
		return gfx.EventResult{}
	}
	sidebar, outline := state.paneWidths(state.paneViewport)
	if event.Target == sidebarResizeID && sidebar > 0 {
		state.sidebarWidth = min(max(190, sidebar+direction), min(420, max(190, state.paneViewport-500)))
	} else if event.Target == outlineResizeID && outline > 0 {
		state.outlineWidth = min(max(160, outline-direction), min(360, state.paneViewport-sidebar-500))
	} else {
		return gfx.EventResult{}
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (state *model) paintPaneResize(canvas *gfx.Canvas, size ui.Size, sidebar, outline, statusHeight float32) {
	height := max(0, size.Height-64-statusHeight)
	if sidebar > 0 {
		canvas.HitWithOptions(sidebarResizeID, ui.Rect{X: sidebar - 4, Y: 64, Width: 8, Height: height}, "调整文件侧栏宽度", gfx.HitOptions{
			Role: gfx.RoleSlider, Cursor: gfx.PointerCursorResizeHorizontal, Hint: "拖动调整文件侧栏宽度",
		})
	}
	if outline > 0 {
		canvas.HitWithOptions(outlineResizeID, ui.Rect{X: size.Width - outline - 4, Y: 64, Width: 8, Height: height}, "调整大纲宽度", gfx.HitOptions{
			Role: gfx.RoleSlider, Cursor: gfx.PointerCursorResizeHorizontal, Hint: "拖动调整大纲宽度",
		})
	}
}
