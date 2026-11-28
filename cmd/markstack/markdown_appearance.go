package main

import "github.com/vivaui/viva/ui"

type markdownAppearance struct {
	palette                          colors
	link, headingRule, codeFill      ui.Color
	codeBorder, inlineCodeBackground ui.Color
	quoteFill, quoteBar              ui.Color
	tableHeader, tableStripe         ui.Color
	tableBorder                      ui.Color
	curated                          bool
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
	appearance.quoteBar, appearance.tableBorder = appearance.palette.border, appearance.palette.border
	appearance.tableHeader, appearance.tableStripe = appearance.palette.sidebar, appearance.palette.sidebar
	return appearance
}

func (state *model) markdownAppearance(palette colors) markdownAppearance {
	theme := state.preferences.ReadingTheme
	if theme == "soft" && state.mode != readMode && state.mode != splitMode {
		theme = "standard"
	}
	return readingAppearance(theme, state.dark, palette)
}

type readingColors struct {
	paper, ink, muted, accent   uint32
	quote, quoteBar             uint32
	table, stripe, border, code uint32
}

var readingThemes = map[string][2]readingColors{
	"soft": {
		{0xf1f6f4, 0x2b3735, 0x4b6159, 0x215c52, 0xe6efeb, 0x657f74, 0xe2ebe7, 0xeaf2ee, 0x6f827a, 0xe8f0ec},
		{0x292f2e, 0xe9eeeb, 0xb0c1b9, 0x9adcce, 0x343e3b, 0x91a99f, 0x3a4641, 0x303936, 0x859c91, 0x303a37},
	},
	"github": {
		{0xfbfcfe, 0x1f2328, 0x505963, 0x0550ae, 0xf1f5fb, 0x637386, 0xe9eff7, 0xf4f7fb, 0x748294, 0xeff3f8},
		{0x0d1117, 0xe6edf3, 0xaebaca, 0x79b8ff, 0x151f2e, 0x849ab8, 0x202c3e, 0x161f2c, 0x778ca9, 0x161e2a},
	},
	"parchment": {
		{0xf8efdd, 0x382719, 0x5e4634, 0x7b381a, 0xf0e2c9, 0x8b6e47, 0xeddebf, 0xf3e9d4, 0x8c704d, 0xf2e6cf},
		{0x2a211a, 0xf4e6d0, 0xd0bda3, 0xffc284, 0x382b21, 0xb49571, 0x423126, 0x34281f, 0xa48768, 0x34291f},
	},
	"academic": {
		{0xfdfbf7, 0x202521, 0x505751, 0x365643, 0xf0f1eb, 0x667566, 0xe9ece4, 0xf3f3ed, 0x748173, 0xf0f1ec},
		{0x202420, 0xf2f2e9, 0xb7c4b6, 0xb4dfb5, 0x2b342c, 0x9cb29e, 0x343d34, 0x2a312a, 0x8ca38d, 0x2b322b},
	},
	"nord": {
		{0xeceff4, 0x2e3440, 0x495568, 0x294f69, 0xe2e8f0, 0x5d6f87, 0xdce3ed, 0xe6ebf2, 0x6b7a90, 0xe3e9f1},
		{0x2e3440, 0xeceff4, 0xc1ccda, 0x9ed1dd, 0x363f50, 0x96a8c0, 0x404b5e, 0x353e4e, 0x9aaac1, 0x363e4c},
	},
}

func readingAppearance(theme string, dark bool, palette colors) markdownAppearance {
	appearance := markdownAppearanceFor(dark, palette)
	pack, ok := readingThemes[theme]
	if !ok {
		return appearance
	}
	value := pack[0]
	if dark {
		value = pack[1]
	}
	color := func(hex uint32) ui.Color { return ui.RGB(uint8(hex>>16), uint8(hex>>8), uint8(hex)) }
	appearance.curated = true
	appearance.palette.background, appearance.palette.ink = color(value.paper), color(value.ink)
	appearance.palette.muted, appearance.palette.accent = color(value.muted), color(value.accent)
	appearance.palette.sidebar, appearance.palette.border = color(value.table), color(value.border)
	appearance.link, appearance.headingRule = appearance.palette.accent, appearance.palette.border
	appearance.quoteFill, appearance.quoteBar = color(value.quote), color(value.quoteBar)
	appearance.tableHeader, appearance.tableStripe = color(value.table), color(value.stripe)
	appearance.tableBorder = appearance.palette.border
	appearance.codeFill, appearance.codeBorder = color(value.code), appearance.palette.border
	appearance.inlineCodeBackground = appearance.codeFill
	return appearance
}

func (appearance markdownAppearance) headingColor(level int, dark bool) ui.Color {
	if appearance.curated {
		return appearance.palette.ink
	}
	return markdownHeadingColor(level, dark, appearance.palette.ink)
}
