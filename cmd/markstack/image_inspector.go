package main

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"markstack/internal/session"
)

type imageInspectorState struct {
	open        bool
	documentID  int
	path        string
	source      string
	alt         string
	bitmap      gfx.Bitmap
	zoom        float32
	fitZoom     float32
	deviceScale float32
	pan         ui.Point
	bounds      ui.Rect
	geometry    components.ImageViewGeometry
	dragging    bool
	lastPoint   ui.Point
}

func (state *model) openImageInspector(document *session.Document, index int) {
	image, ok := state.markdownMediaBlocks[index]
	if !ok {
		return
	}
	state.openImageInspectorBitmap(document, image)
}

func (state *model) openImageInspectorBitmap(document *session.Document, image markdownPreviewImage) {
	if image.bitmap.Width <= 0 || image.bitmap.Height <= 0 || document == nil {
		return
	}
	state.syncRenderedDocumentHistory()
	state.fileMenuOpen = false
	state.recentOpen = false
	state.imageInspector = imageInspectorState{
		open: true, documentID: document.ID, path: document.Path, source: document.Text(),
		alt: image.alt, bitmap: image.bitmap,
	}
	state.invalidator.Invalidate()
}

func (state *model) closeImageInspector() {
	state.imageInspector = imageInspectorState{}
	state.invalidator.Invalidate()
}

func (state *model) changeImageZoom(direction int) {
	inspector := &state.imageInspector
	if direction > 0 {
		if inspector.zoom == 0 {
			inspector.zoom = max(1, inspector.geometry.Scale*inspector.deviceScale*1.25)
		} else {
			inspector.zoom *= 1.5
		}
		fit := inspector.geometry.Scale * inspector.deviceScale
		inspector.zoom = min(inspector.zoom, max(16, fit*4))
	} else if inspector.zoom > 0 {
		inspector.zoom /= 1.5
		if inspector.zoom <= inspector.fitZoom*1.05 {
			inspector.zoom = 0
		}
	}
	inspector.pan = ui.Point{}
	state.invalidator.Invalidate()
}

func (state *model) onImageInspectorKey(event gfx.KeyEvent) gfx.EventResult {
	switch event.Key {
	case gfx.KeyEscape:
		state.closeImageInspector()
	case gfx.KeyLeft:
		state.panImage(40, 0)
	case gfx.KeyRight:
		state.panImage(-40, 0)
	case gfx.KeyUp:
		state.panImage(0, 40)
	case gfx.KeyDown:
		state.panImage(0, -40)
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (state *model) onImageInspectorPointer(event gfx.PointerEvent) gfx.EventResult {
	inspector := &state.imageInspector
	if event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonPrimary && event.Target == imageCanvasID {
		inspector.dragging = true
		inspector.lastPoint = ui.Point{X: event.X, Y: event.Y}
		return gfx.EventResult{Handled: true}
	}
	if !inspector.dragging {
		return gfx.EventResult{}
	}
	if event.Kind == gfx.PointerUp {
		inspector.dragging = false
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if event.Kind != gfx.PointerMove {
		return gfx.EventResult{}
	}
	state.panImage(event.X-inspector.lastPoint.X, event.Y-inspector.lastPoint.Y)
	inspector.lastPoint = ui.Point{X: event.X, Y: event.Y}
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (state *model) onImageInspectorScroll(event gfx.ScrollEvent) gfx.EventResult {
	if event.DeltaY < 0 {
		state.changeImageZoom(1)
	} else if event.DeltaY > 0 {
		state.changeImageZoom(-1)
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (state *model) panImage(deltaX, deltaY float32) {
	inspector := &state.imageInspector
	maximumX := max(0, (inspector.geometry.Image.Width-inspector.bounds.Width)/2)
	maximumY := max(0, (inspector.geometry.Image.Height-inspector.bounds.Height)/2)
	inspector.pan.X = min(max(inspector.pan.X+deltaX, -maximumX), maximumX)
	inspector.pan.Y = min(max(inspector.pan.Y+deltaY, -maximumY), maximumY)
	state.invalidator.Invalidate()
}

func (state *model) paintImageInspector(canvas *gfx.Canvas, theme ui.Theme, palette colors, size ui.Size) {
	inspector := &state.imageInspector
	if !inspector.open {
		return
	}
	current := state.session.Current()
	if current == nil || current.ID != inspector.documentID || current.Path != inspector.path || current.Text() != inspector.source {
		state.closeImageInspector()
		return
	}
	viewport := ui.Rect{X: 0, Y: 32, Width: size.Width, Height: size.Height - 32}
	inspector.bounds = ui.Rect{X: 16, Y: 80, Width: max(40, size.Width-32), Height: max(40, size.Height-96)}
	canvas.PushLayer(12)
	canvas.PushFocusScope(imageScope)
	canvas.Fill(viewport, palette.background)
	canvas.HitWithOptions(imageBackdropID, ui.Rect{Width: size.Width, Height: size.Height}, "关闭图片查看", gfx.HitOptions{Role: gfx.RoleButton})
	if canvas.Clicked(imageBackdropID) {
		state.closeImageInspector()
	}
	inspector.deviceScale = canvas.PixelScale()
	inspector.geometry = components.PaintImageView(canvas, theme, inspector.bounds, components.ImageViewOptions{
		Bitmap: inspector.bitmap, Zoom: inspector.zoom, Pan: inspector.pan,
	})
	if inspector.zoom == 0 {
		inspector.fitZoom = inspector.geometry.Scale * inspector.deviceScale
	}
	canvas.HitWithOptions(imageCanvasID, inspector.bounds, "拖动图片", gfx.HitOptions{Role: gfx.RoleButton, Hint: "拖动以平移图片"})
	left := size.Width - 178
	canvas.Label(ellipsize(canvas, inspector.alt, max(80, left-40), 13), ui.Rect{X: 20, Y: 42, Width: max(80, left-40), Height: 28}, gfx.TextStyle{Size: 13, Weight: gfx.WeightSemibold, Color: palette.ink})
	findCommand(canvas, imageZoomOutID, "−", "缩小图片", ui.Rect{X: left, Y: 42, Width: 28, Height: 28}, false, inspector.zoom > 0, palette, func() { state.changeImageZoom(-1) })
	findCommand(canvas, imageFitID, "适应", "适应窗口", ui.Rect{X: left + 32, Y: 42, Width: 50, Height: 28}, false, inspector.zoom > 0, palette, func() {
		inspector.zoom = 0
		inspector.pan = ui.Point{}
	})
	labeledButton(canvas, imageZoomInID, "+", "放大图片", ui.Rect{X: left + 86, Y: 42, Width: 28, Height: 28}, false, palette, func() { state.changeImageZoom(1) })
	labeledButton(canvas, imageCloseID, "×", "关闭图片", ui.Rect{X: left + 126, Y: 42, Width: 30, Height: 28}, false, palette, state.closeImageInspector)
	canvas.SetEscapeTarget(imageCloseID)
	canvas.TrapFocus(imageScope)
	canvas.PopFocusScope()
	canvas.PopLayer()
}
