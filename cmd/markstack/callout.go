package main

import (
	"strings"

	"github.com/vivaui/viva/base"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/ui"
	"markstack/internal/markdownsyntax"
)

const (
	calloutCardInfo    = "markstack:callout:"
	markdownCardInset  = 15
	markdownCardHeader = 34
	markdownCardBottom = 10
)

func markdownCard(block *markdown.Block) bool {
	return block.Kind == markdown.Paragraph && (block.Info == metadataCardInfo || strings.HasPrefix(block.Info, calloutCardInfo))
}

type markdownCardLayout struct {
	label, glyph string
	accent       ui.Color
	metadata     bool
	metadataRows []string
	content      *previewView
}

func (view *previewView) layoutCard(canvas *gfx.Canvas, layout *previewBlock, style components.MarkdownStyle, block *markdown.Block, width float32) float32 {
	card := &layout.card
	card.metadata = block.Info == metadataCardInfo
	card.accent = view.appearance.palette.muted
	if card.metadata {
		return card.layoutMetadata(canvas, style, block.Text, width)
	}
	kind, title, _ := strings.Cut(strings.TrimPrefix(block.Info, calloutCardInfo), ":")
	alert, _ := markdownsyntax.AlertType(kind)
	card.label, card.glyph = alert.Label(), alert.Glyph
	if title != "" {
		card.label += " · " + title
	}
	accent := alert.RGB
	if view.night {
		accent = alert.DarkRGB
	}
	card.accent = gfx.Color{Red: float32(accent[0]) / 255, Green: float32(accent[1]) / 255, Blue: float32(accent[2]) / 255, Alpha: 1}
	if card.content == nil {
		card.content = &previewView{}
	}
	var body markdown.Document
	card.content.formulas, card.content.displayMath = parseTechnicalProjection(&body, block.Text, true, false)
	card.content.appearance, card.content.night, card.content.editingInline = view.appearance, view.night, -1
	contentHeight := card.content.Layout(canvas, style, &body, max(1, width-2*markdownCardInset), nil, nil, nil)
	view.links = append(view.links, card.content.links...)
	return markdownCardHeader + markdownCardBottom + contentHeight
}

func (card *markdownCardLayout) paint(canvas *gfx.Canvas, block *previewBlock, paint components.MarkdownPaint, editing bool, appearance markdownAppearance) {
	box := ui.Rect{X: paint.X, Y: paint.Y, Width: block.width, Height: block.height}
	fill := appearance.palette.background.Mix(card.accent, .09)
	canvas.RoundedBorder(box, 8, 1, fill.Mix(card.accent, .21), fill)
	canvas.Clip(ui.Rect{X: box.X, Y: box.Y, Width: 4, Height: box.Height})
	canvas.Rounded(box, 8, card.accent)
	canvas.Unclip()
	badge := ui.Rect{X: box.X + markdownCardInset, Y: box.Y + 8, Width: 19, Height: 19}
	canvas.Rounded(badge, 5, fill.Mix(card.accent, .16))
	canvas.LabelCenter(card.glyph, badge, gfx.TextStyle{Size: 12, Weight: gfx.WeightBold, Color: card.accent})
	label := ui.Rect{X: badge.X + 25, Y: box.Y + 7, Width: max(0, box.Width-markdownCardInset-32), Height: 22}
	canvas.Clip(label)
	canvas.Label(card.label, label, gfx.TextStyle{Size: 13, Weight: gfx.WeightBold, Color: card.accent})
	canvas.Unclip()
	if editing {
		return
	}
	paint.X += markdownCardInset
	paint.Y += markdownCardHeader
	if card.metadata {
		card.paintMetadata(canvas, block, paint)
	} else {
		part := paint
		part.Selection = base.TextRange{}
		card.content.Paint(canvas, part)
	}
}
