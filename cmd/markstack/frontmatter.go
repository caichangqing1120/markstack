package main

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

const metadataCardInfo = "markstack:frontmatter"

func (state *model) hasCardDraft() bool {
	rich := &state.rich
	current := state.session.Current()
	return current != nil && rich.active && rich.input != nil && rich.documentID == current.ID && rich.block.IsCard() && rich.input.CommittedString() != rich.block.Visible
}

func (state *model) flushCardDraft() bool {
	if !state.hasCardDraft() {
		return true
	}
	state.syncRendered(state.session.Current())
	if !state.hasCardDraft() {
		return true
	}
	state.focusRequest = richInputID
	state.invalidator.Invalidate()
	return false
}

func (state *model) discardCardDraft() {
	rich := &state.rich
	caret := rich.input.Caret()
	rich.input = text.NewEditor(rich.block.Visible)
	rich.input.MoveTo(caret, false)
	state.message = ""
}

func metadataCardStyle(size float32) gfx.TextStyle {
	return gfx.TextStyle{Size: size * .9, Mono: true, MonoFace: markdownCodeFace}
}

func (card *markdownCardLayout) layoutMetadata(canvas *gfx.Canvas, style components.MarkdownStyle, body string, width float32) float32 {
	card.label, card.glyph = "YAML Front Matter", "≡"
	card.metadataRows = canvas.WrapLinesStyle(card.metadataRows[:0], body, metadataCardStyle(style.Size), max(1, width-2*markdownCardInset), len(body)+1, nil)
	return markdownCardHeader + markdownCardBottom + float32(max(1, len(card.metadataRows)))*style.LineHeight
}

func (card *markdownCardLayout) paintMetadata(canvas *gfx.Canvas, block *previewBlock, paint components.MarkdownPaint) {
	style := metadataCardStyle(block.inlineSize)
	style.Color = paint.Look.Text
	for index, row := range card.metadataRows {
		bounds := ui.Rect{X: paint.X, Y: paint.Y + float32(index)*block.lineHeight, Width: block.width - 2*markdownCardInset, Height: block.lineHeight}
		canvas.Label(row, bounds, style)
	}
}
