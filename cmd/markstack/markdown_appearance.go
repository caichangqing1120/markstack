package main

import "github.com/vivaui/viva/ui"

type markdownAppearance struct {
	palette                          colors
	link, headingRule, codeFill      ui.Color
	codeBorder, inlineCodeBackground ui.Color
}

func markdownAppearanceFor(dark bool, palette colors) markdownAppearance {
	palette.background = ui.RGB(255, 255, 255)
	palette.ink = ui.RGB(51, 51, 51)
	palette.muted = ui.RGB(119, 119, 119)
	palette.border = ui.RGB(223, 226, 229)
	palette.sidebar = ui.RGB(248, 248, 248)
	appearance := markdownAppearance{
		palette: palette, link: ui.RGB(65, 131, 196), headingRule: ui.RGB(238, 238, 238),
		codeFill: ui.RGB(248, 248, 248), codeBorder: ui.RGB(231, 234, 237), inlineCodeBackground: ui.RGB(243, 244, 244),
	}
	if dark {
		appearance.palette.background = ui.RGB(54, 59, 64)
		appearance.palette.ink = ui.RGB(184, 191, 198)
		appearance.palette.muted = ui.RGB(157, 162, 166)
		appearance.palette.border = ui.RGB(71, 77, 84)
		appearance.palette.sidebar = appearance.palette.background
		appearance.link = ui.RGB(224, 224, 224)
		appearance.headingRule = ui.Color{}
		appearance.codeFill, appearance.codeBorder = ui.RGB(51, 51, 51), ui.RGB(51, 51, 51)
		appearance.inlineCodeBackground = ui.Color{Alpha: 0.05}
	}
	return appearance
}

func (state *model) markdownAppearance(palette colors) markdownAppearance {
	appearance := markdownAppearanceFor(state.dark, palette)
	appearance.palette = state.readingPalette(appearance.palette)
	return appearance
}
