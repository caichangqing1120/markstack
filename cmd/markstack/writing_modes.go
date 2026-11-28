package main

import (
	"sort"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

type writingMotion struct {
	key      writingCaret
	tracking bool
	scroll   scrollMotion
}

type writingCaret struct {
	document, block, offset int
	mode                    viewMode
	source                  string
	viewport                ui.Rect
}

type scrollMotion struct {
	start        time.Duration
	from, target float32
	active       bool
}

func (motion *scrollMotion) step(canvas *gfx.Canvas, current, target float32) float32 {
	if !motion.active || motion.target != target {
		motion.start, motion.from, motion.target = canvas.Now(), current, target
		motion.active = true
	}
	progress := min(1, float32(canvas.Now()-motion.start)/float32(160*time.Millisecond))
	if progress >= 1 || max(target-current, current-target) < 0.5 {
		motion.active = false
		return target
	}
	canvas.RequestFrameIn(time.Second / 60)
	remaining := 1 - progress
	return target + (motion.from-target)*remaining*remaining*remaining
}

func (state *model) followWritingCaret(canvas *gfx.Canvas, key writingCaret, current, target float32) float32 {
	motion := &state.writing
	if motion.key != key {
		motion.key, motion.tracking = key, true
	}
	if !motion.tracking {
		return current
	}
	return motion.scroll.step(canvas, current, target)
}

func (state *model) sourceWritingScroll() float32 {
	return state.source.ScrollY() - state.sourceShift
}

func (state *model) setSourceWritingScroll(position float32) {
	state.source.SetScrollY(position)
	state.sourceShift = state.source.ScrollY() - position
}

func (state *model) sourceFocusLines(offset int) (int, int) {
	current := state.session.Current()
	state.outlinePanel.index(current.ID, current.Editor.CommittedString())
	blocks := state.outlinePanel.parsed.Blocks
	index := sort.Search(len(blocks), func(index int) bool { return blocks[index].End >= offset })
	if index < len(blocks) && blocks[index].Start <= offset {
		return state.stats.lineAt(blocks[index].Start), state.stats.lineAt(blocks[index].End)
	}
	line := state.stats.lineAt(offset)
	return line, line
}

func dimmed(color, background ui.Color) ui.Color {
	const strength = 0.42
	color.Red += (background.Red - color.Red) * strength
	color.Green += (background.Green - color.Green) * strength
	color.Blue += (background.Blue - color.Blue) * strength
	return color
}

func (state *model) sourceFocusRuns(line string, palette colors, runs []components.CodeEditorRun, dim bool) []components.CodeEditorRun {
	if !dim {
		return markdownSourceRuns(line, palette, runs)
	}
	state.focusRuns = markdownSourceRuns(line, palette, state.focusRuns)
	runs = runs[:0]
	position := 0
	for _, run := range state.focusRuns {
		if position < run.Start {
			runs = append(runs, components.CodeEditorRun{Start: position, End: run.Start, Color: dimmed(palette.ink, palette.background)})
		}
		run.Color = dimmed(run.Color, palette.background)
		runs, position = append(runs, run), run.End
	}
	if position < len(line) {
		runs = append(runs, components.CodeEditorRun{Start: position, End: len(line), Color: dimmed(palette.ink, palette.background)})
	}
	return runs
}

func (state *model) renderedWritingCaret(canvas *gfx.Canvas, bounds ui.Rect) (writingCaret, ui.Rect, bool) {
	current := state.session.Current()
	key := writingCaret{document: current.ID, mode: renderedMode, viewport: bounds, block: -1, source: current.Text()}
	var caret ui.Rect
	var visible bool
	switch {
	case state.rich.active:
		key.block, key.offset = state.rich.index, state.rich.input.Caret()
		if canvas.Focused() == richInputID && state.rich.rect.Height > 0 {
			caret, visible = state.rich.CaretBounds()
		}
	case state.codeBlock.active:
		key.block, key.offset = state.codeBlock.renderedIndex, state.codeBlock.input.Caret()
		if canvas.Focused() == codeInputID {
			caret, visible = state.codeBlock.view.CaretBounds()
		}
	case state.table.active:
		key.block, key.offset = state.table.renderedIndex, state.table.input.Caret()
		if canvas.Focused() == tableInputID {
			caret, visible = state.table.CaretBounds()
		}
	case state.selection.active:
		key.block, key.offset = state.selection.extentIndex, state.selection.lastOffset
	default:
		state.stats.update(key.source)
		offset := state.stats.byteAt(current.Editor.Caret())
		key.block = sort.Search(len(state.parsed.Blocks), func(index int) bool { return state.parsed.Blocks[index].End >= offset })
	}
	return key, caret, visible
}

func (state *model) paintRenderedWritingModes(canvas *gfx.Canvas, palette colors, bounds ui.Rect, origin float32) {
	if !state.preferences.FocusMode && !state.preferences.TypewriterMode {
		return
	}
	key, caret, visible := state.renderedWritingCaret(canvas, bounds)
	if state.preferences.FocusMode {
		color := palette.background
		color.Alpha = 0.42
		first := sort.Search(len(state.preview.blocks), func(index int) bool {
			block := state.preview.blocks[index]
			return origin+block.top+block.height > bounds.Y
		})
		for index := first; index < len(state.preview.blocks); index++ {
			block := state.preview.blocks[index]
			y := origin + block.top
			if y >= bounds.Y+bounds.Height {
				break
			}
			if index == key.block && !state.virtualParagraphNeedsPlaceholder() {
				continue
			}
			canvas.Fill(ui.Rect{X: bounds.X, Y: y, Width: bounds.Width, Height: max(26, block.height)}, color)
		}
	}
	if !state.preferences.TypewriterMode || state.anchor.active || state.pendingAnchor >= 0 {
		return
	}
	if !visible {
		if key.block < 0 || key.block >= len(state.preview.blocks) || state.writing.key == key && !state.writing.tracking {
			return
		}
		caret = ui.Rect{Y: origin + state.preview.blocks[key.block].top, Height: 26}
	}
	current := state.previewScroll
	target := current + caret.Y + caret.Height/2 - (bounds.Y + bounds.Height/2)
	minimum, maximum := state.previewLimits(bounds)
	state.previewScroll = state.followWritingCaret(canvas, key, current, min(maximum, max(minimum, target)))
	if current != state.previewScroll {
		canvas.RequestFrame()
	}
}

func (state *model) previewLimits(bounds ui.Rect) (float32, float32) {
	if state.mode == renderedMode && state.preferences.TypewriterMode {
		return -bounds.Height / 2, max(0, state.preview.Height()-bounds.Height/2+46)
	}
	return 0, max(0, state.preview.Height()-bounds.Height+60)
}
