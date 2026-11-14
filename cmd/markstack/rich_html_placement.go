package main

import (
	"unicode"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

type htmlTextSegment struct {
	start, end int
	bounds     ui.Rect
	style      gfx.TextStyle
}

func htmlTextAdvance(canvas *gfx.Canvas, value string, style gfx.TextStyle) float32 {
	total := float32(0)
	previous := 0
	for offset, character := range value {
		if !unicode.In(character, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
			continue
		}
		total += canvas.TextWidthStyle(value[previous:offset], style)
		end := offset + utf8.RuneLen(character)
		total += max(canvas.TextWidthStyle(value[offset:end], style), style.Size)
		previous = end
	}
	total += canvas.TextWidthStyle(value[previous:], style)
	return max(canvas.TextWidthStyle(value, style), total)
}

func htmlTextOffsetAt(canvas *gfx.Canvas, value string, style gfx.TextStyle, x float32) int {
	previousWidth, previousOffset := float32(0), 0
	for offset, character := range value {
		end := offset + utf8.RuneLen(character)
		width := htmlTextAdvance(canvas, value[:end], style)
		if x < (previousWidth+width)/2 {
			return previousOffset
		}
		previousWidth, previousOffset = width, end
	}
	return len(value)
}

func (view *richHTMLView) OffsetAt(point ui.Point) (int, bool) {
	if view.input == nil || view.wrapCanvas == nil || len(view.segments) == 0 {
		return 0, false
	}
	value := view.input.String()
	for _, segment := range view.segments {
		if point.Y >= segment.bounds.Y && point.Y < segment.bounds.Y+segment.bounds.Height {
			end := min(segment.end, len(value))
			return segment.start + htmlTextOffsetAt(view.wrapCanvas, value[segment.start:end], segment.style, point.X-segment.bounds.X), true
		}
	}
	if point.Y < view.segments[0].bounds.Y {
		return 0, true
	}
	return len(value), true
}

func (view *richHTMLView) CaretBounds() (ui.Rect, bool) {
	if view.input == nil || view.wrapCanvas == nil || len(view.segments) == 0 {
		return ui.Rect{}, false
	}
	value := view.input.String()
	caret := byteOffsetOfRune(value, view.input.Caret())
	for index := len(view.segments) - 1; index >= 0; index-- {
		segment := view.segments[index]
		if caret < segment.start {
			continue
		}
		end := min(caret, segment.end, len(value))
		x := segment.bounds.X + htmlTextAdvance(view.wrapCanvas, value[segment.start:end], segment.style)
		return ui.Rect{X: x, Y: segment.bounds.Y, Width: 1, Height: segment.style.Size * 1.4}, true
	}
	return ui.Rect{}, false
}

func (view *richHTMLView) containsActivePoint(point ui.Point) bool {
	for _, segment := range view.segments {
		if contains(segment.bounds, point) {
			return true
		}
	}
	return false
}

func (view *richHTMLView) moveKey(event gfx.KeyEvent) bool {
	if view.input == nil || len(view.segments) == 0 || view.wrapCanvas == nil {
		return false
	}
	value := view.input.String()
	caret := byteOffsetOfRune(value, view.input.Caret())
	index := 0
	for segmentIndex, segment := range view.segments {
		if caret >= segment.start {
			index = segmentIndex
		}
	}
	target := caret
	switch event.Key {
	case gfx.KeyHome:
		target = view.segments[index].start
		view.goalActive = false
	case gfx.KeyEnd:
		target = view.segments[index].end
		view.goalActive = false
	case gfx.KeyUp, gfx.KeyDown:
		next := index - 1
		if event.Key == gfx.KeyDown {
			next = index + 1
		}
		if next < 0 || next >= len(view.segments) {
			view.goalActive = false
			return false
		}
		caretBounds, ok := view.CaretBounds()
		if !ok {
			return false
		}
		goal := caretBounds.X
		if view.goalActive && view.goalCaret == caret {
			goal = view.goalX
		}
		segment := view.segments[next]
		if offset, ok := view.OffsetAt(ui.Point{X: goal, Y: segment.bounds.Y + segment.bounds.Height/2}); ok {
			target = offset
		}
		view.goalX, view.goalCaret, view.goalActive = goal, target, true
	default:
		view.goalActive = false
		return false
	}
	target = max(0, min(target, len(value)))
	view.input.MoveTo(utf8.RuneCountInString(value[:target]), event.Modifiers&gfx.ModifierShift != 0)
	return true
}
