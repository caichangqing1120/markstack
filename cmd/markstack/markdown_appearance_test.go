package main

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
)

type markdownReferenceColors struct {
	palette                          colors
	link, headingRule, codeFill      ui.Color
	codeBorder, inlineCodeBackground ui.Color
}

func referenceMarkdownColors(dark bool) markdownReferenceColors {
	if dark {
		return markdownReferenceColors{
			palette: colors{
				background: ui.RGB(54, 59, 64), ink: ui.RGB(184, 191, 198), muted: ui.RGB(157, 162, 166),
				border: ui.RGB(71, 77, 84), sidebar: ui.RGB(54, 59, 64),
			},
			link: ui.RGB(224, 224, 224), codeFill: ui.RGB(51, 51, 51), codeBorder: ui.RGB(51, 51, 51),
			inlineCodeBackground: ui.Color{Alpha: 0.05},
		}
	}
	return markdownReferenceColors{
		palette: colors{
			background: ui.RGB(255, 255, 255), ink: ui.RGB(51, 51, 51), muted: ui.RGB(119, 119, 119),
			border: ui.RGB(223, 226, 229), sidebar: ui.RGB(248, 248, 248),
		},
		link: ui.RGB(65, 131, 196), headingRule: ui.RGB(238, 238, 238),
		codeFill: ui.RGB(248, 248, 248), codeBorder: ui.RGB(231, 234, 237), inlineCodeBackground: ui.RGB(243, 244, 244),
	}
}

func markdownPaintedColorAt(frame app.Snapshot, point ui.Point) ui.Color {
	var result ui.Color
	for _, primitive := range frame.Primitives {
		if primitive.Kind != app.RectanglePrimitive || primitive.Color.Alpha == 0 {
			continue
		}
		box := primitive.Bounds
		if point.X < box.X || point.X >= box.X+box.Width || point.Y < box.Y || point.Y >= box.Y+box.Height {
			continue
		}
		if primitive.Clipped {
			clip := primitive.Clip
			if point.X < clip.X || point.X >= clip.X+clip.Width || point.Y < clip.Y || point.Y >= clip.Y+clip.Height {
				continue
			}
		}
		result = primitive.Color
	}
	return result
}

func requireMarkdownTextColor(context *testing.T, frame app.Snapshot, value string, expected ui.Color, clips ...ui.Rect) {
	context.Helper()
	for _, label := range frame.Texts {
		if len(clips) > 0 {
			clip := clips[0]
			if label.Bounds.X < clip.X || label.Bounds.X >= clip.X+clip.Width || label.Bounds.Y < clip.Y || label.Bounds.Y >= clip.Y+clip.Height {
				continue
			}
		}
		if strings.TrimSuffix(label.Value, "\r") == value {
			if label.Style.Color != expected {
				context.Errorf("%q ink=%v, want reference %v", value, label.Style.Color, expected)
			}
			return
		}
	}
	var painted []string
	for _, label := range frame.Texts {
		painted = append(painted, label.Value)
	}
	context.Fatalf("missing painted Markdown text %q; painted values=%q", value, painted)
}

func TestMarkdownReferenceAppearanceKeepsChromeAndEditorsSeparate(test *testing.T) {
	body := "Body \u539f\u6587"
	original := []byte("\xef\xbb\xbf# Title\r\n\r\n" + body + "\r\n\r\n> Quote\r\n\r\n[Link](https://example.org)\r\n\r\n| Left | Right |\r\n| --- | --- |\r\n| one | two |\r\n| three | four |\r\n\r\n```text\r\nplain code\r\n```\r\n\r\n###### Small\r\n\r\nTail `inline`\r\n")
	for _, dark := range []bool{false, true} {
		for _, width := range []int{740, 1200} {
			for _, mode := range []viewMode{readMode, renderedMode, splitMode} {
				test.Run(fmt.Sprintf("dark-%v/width-%d/mode-%d", dark, width, mode), func(context *testing.T) {
					filename := filepath.Join(context.TempDir(), "appearance.md")
					if err := os.WriteFile(filename, original, 0600); err != nil {
						context.Fatal(err)
					}
					state, driver := testDriver(context)
					state.dark, state.mode = dark, mode
					state.openFile(filename)
					height := 955
					if mode == splitMode {
						height = 1400
					}
					if err := driver.Resize(width, height); err != nil {
						context.Fatal(err)
					}
					want := referenceMarkdownColors(dark)
					_, chrome := state.look()
					frame := driver.Snapshot()
					point := ui.Point{X: state.previewRect.X + state.previewRect.Width - 8, Y: state.previewRect.Y + state.previewRect.Height - 8}
					if got := markdownPaintedColorAt(frame, point); got != want.palette.background {
						context.Errorf("document background=%v, want reference %v", got, want.palette.background)
					}
					if got := markdownPaintedColorAt(frame, ui.Point{X: 8, Y: 700}); got != chrome.sidebar {
						context.Errorf("article colors changed sidebar: %v, want %v", got, chrome.sidebar)
					}
					requireMarkdownTextColor(context, frame, body, want.palette.ink, state.previewRect)
					requireMarkdownTextColor(context, frame, "Quote", want.palette.muted, state.previewRect)
					requireMarkdownTextColor(context, frame, "Link", want.link, state.previewRect)
					requireMarkdownTextColor(context, frame, "plain code", want.palette.ink, state.previewRect)
					heading, small := want.palette.ink, want.palette.muted
					if dark {
						heading, small = ui.RGB(222, 222, 222), ui.RGB(255, 255, 255)
					}
					requireMarkdownTextColor(context, frame, "Title", heading, state.previewRect)
					requireMarkdownTextColor(context, frame, "Small", small, state.previewRect)
					left, contentWidth := state.markdownColumn(state.previewRect)
					top := state.previewRect.Y + 46 - state.previewScroll
					code := state.preview.blocks[5]
					border, fill, inline, quote, rule := false, false, false, false, dark
					for _, primitive := range frame.Primitives {
						box := primitive.Bounds
						if box.X == left && box.Y == top+code.top && box.Width == contentWidth && box.Height == code.height && primitive.Color == want.codeBorder {
							border = true
						}
						if box.X == left+1 && box.Y == top+code.top+1 && box.Width == contentWidth-2 && box.Height == code.height-2 && primitive.Color == want.codeFill {
							fill = true
						}
						if box.X >= left && box.Y >= top+state.preview.blocks[7].top && primitive.Color == want.inlineCodeBackground {
							inline = true
						}
						if box.X == left+2 && box.Y == top+state.preview.blocks[2].top && box.Width == 3 && primitive.Color == want.palette.border {
							quote = true
						}
						if !dark && box.X == left && box.Y == top+state.preview.blocks[0].rule && box.Width == contentWidth && box.Height == 1 && primitive.Color == want.headingRule {
							rule = true
						}
					}
					if !border || !fill {
						context.Errorf("reference code surface border/fill=%v/%v", border, fill)
					}
					if !inline || !quote || !rule {
						context.Errorf("reference inline/quote/heading rules=%v/%v/%v", inline, quote, rule)
					}
					table := state.preview.blocks[4]
					for row, expected := range []ui.Color{want.palette.sidebar, want.palette.background, want.palette.sidebar} {
						point := ui.Point{X: left + 4, Y: top + table.top + float32(row+1)*38 - 4}
						if got := markdownPaintedColorAt(frame, point); got != expected {
							context.Errorf("reading table row %d fill=%v, want %v", row, got, expected)
						}
					}
					clickID(context, driver, renderedModeID)
					for _, active := range []struct {
						block int
						value string
						color ui.Color
					}{{1, body, want.palette.ink}, {2, "Quote", want.palette.muted}, {3, "Link", want.link}, {6, "Small", small}, {5, "plain code", want.palette.ink}} {
						clickID(context, driver, renderedRows.At(active.block))
						activeFrame := driver.Snapshot()
						if activeFrame.Editor == nil {
							context.Fatal("activating a block lost native text input")
						}
						requireMarkdownTextColor(context, activeFrame, active.value, active.color, state.previewRect)
					}
					clickID(context, driver, renderedTableCells.At(4))
					for _, hit := range driver.Snapshot().Hits {
						if hit.ID == tableInputID {
							point := ui.Point{X: hit.Bounds.X + 4, Y: hit.Bounds.Y + hit.Bounds.Height - 4}
							if got := markdownPaintedColorAt(driver.Snapshot(), point); got != want.palette.sidebar {
								context.Errorf("active table fill=%v, want %v", got, want.palette.sidebar)
							}
						}
					}
					clickID(context, driver, editModeID)
					clickID(context, driver, sourceID)
					sourceFrame := driver.Snapshot()
					if sourceFrame.Focused != sourceID || sourceFrame.Editor == nil {
						context.Fatal("returning to source lost its native input")
					}
					requireMarkdownTextColor(context, sourceFrame, body, chrome.ink, state.sourceRect)
					if current := state.session.Current(); current.Dirty() || current.Text() != string(original[3:]) {
						context.Fatal("theme or activation changed committed source")
					}
					if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
						context.Fatal(err)
					}
					if saved, err := os.ReadFile(filename); err != nil || !bytes.Equal(saved, original) {
						context.Fatalf("theme round trip changed BOM/CRLF or source bytes: %q, %v", saved, err)
					}
				})
			}
		}
	}
}

func TestReadingThemeAppearanceContrast(t *testing.T) {
	for _, dark := range []bool{false, true} {
		unique := make(map[ui.Color]string)
		for _, theme := range []string{"standard", "soft", "github", "parchment", "academic", "nord"} {
			t.Run(fmt.Sprintf("%s/dark=%v", theme, dark), func(t *testing.T) {
				state := newModel()
				state.mode, state.dark, state.preferences.ReadingTheme = readMode, dark, theme
				_, palette := state.look()
				appearance := state.markdownAppearance(palette)
				ink, paper := appearance.palette.ink, appearance.palette.background
				if previous := unique[paper]; previous != "" {
					t.Fatalf("%q has the same paper as %q", theme, previous)
				}
				unique[paper] = theme
				check := func(element string, front, back ui.Color, minimum float64) {
					t.Helper()
					if ratio := appearanceContrast(front, back); ratio < minimum {
						t.Errorf("%s contrast = %.2f, want at least %.1f", element, ratio, minimum)
					}
				}
				check("body", ink, paper, 4.5)
				check("table header", ink, appearance.tableHeader, 4.5)
				check("code text", ink, appearance.codeFill, 4.5)
				if !appearance.curated {
					return
				}
				check("muted", appearance.palette.muted, paper, 4.5)
				check("blockquote text", appearance.palette.muted, appearance.quoteFill, 4.5)
				check("blockquote bar", appearance.quoteBar, appearance.quoteFill, 3)
				check("table border", appearance.tableBorder, appearance.tableHeader, 3)
				for _, surface := range []ui.Color{paper, appearance.quoteFill, appearance.tableHeader, appearance.tableStripe} {
					check("link", appearance.link, surface, 4.5)
				}
				var look components.MarkdownLook
				appearance.applyCodeLook(&look, dark)
				for _, class := range []components.MarkdownCodeClass{components.CodeComment, components.CodeKeyword, components.CodeString, components.CodeNumber, components.CodeFunction} {
					check(fmt.Sprintf("syntax class %d", class), look.Code[class], appearance.codeFill, 4.5)
				}
			})
		}
	}
}

func appearanceContrast(front, back ui.Color) float64 {
	linear := func(channel float32) float64 {
		value := float64(channel)
		if value <= 0.04045 {
			return value / 12.92
		}
		return math.Pow((value+0.055)/1.055, 2.4)
	}
	luminance := func(color ui.Color) float64 {
		return 0.2126*linear(color.Red) + 0.7152*linear(color.Green) + 0.0722*linear(color.Blue)
	}
	first, second := luminance(front), luminance(back)
	return (max(first, second) + 0.05) / (min(first, second) + 0.05)
}

func TestMarkdownSoftAppearanceStaysReadingOnly(test *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, mode := range []viewMode{readMode, renderedMode} {
			test.Run(fmt.Sprintf("dark-%v/mode-%d", dark, mode), func(context *testing.T) {
				state, driver := testDriver(context)
				state.dark, state.mode = dark, mode
				state.preferences.ReadingTheme = "soft"
				state.preferences.ReadingSize = 22
				state.session.Current().Replace("Body text\n")
				before, dirty := state.session.Current().Text(), state.session.Current().Dirty()
				if err := driver.Resize(1200, 780); err != nil {
					context.Fatal(err)
				}
				want := referenceMarkdownColors(dark).palette
				if mode == readMode {
					want.background, want.ink = ui.RGB(241, 246, 244), ui.RGB(43, 55, 53)
					if dark {
						want.background, want.ink = ui.RGB(41, 47, 46), ui.RGB(233, 238, 235)
					}
				}
				requireMarkdownTextColor(context, driver.Snapshot(), "Body text", want.ink, state.previewRect)
				point := ui.Point{X: state.previewRect.X + 4, Y: state.previewRect.Y + state.previewRect.Height - 4}
				if got := markdownPaintedColorAt(driver.Snapshot(), point); got != want.background {
					context.Errorf("soft reading surface=%v, want %v", got, want.background)
				}
				if state.session.Current().Text() != before || state.session.Current().Dirty() != dirty {
					context.Fatal("reading appearance changed the draft")
				}
			})
		}
	}
}

func TestMarkdownAppearanceDoesNotChangeOtherDocumentKinds(test *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, extension := range []string{"txt", "go", "mh"} {
			test.Run(fmt.Sprintf("dark-%v/%s", dark, extension), func(context *testing.T) {
				filename := filepath.Join(context.TempDir(), "other."+extension)
				source := []byte("Other body\r\n")
				if extension == "mh" {
					source = []byte("<p>Other body</p>\r\n")
				}
				if err := os.WriteFile(filename, source, 0600); err != nil {
					context.Fatal(err)
				}
				state, driver := testDriver(context)
				state.dark = dark
				state.openFile(filename)
				if err := driver.Resize(1200, 780); err != nil {
					context.Fatal(err)
				}
				_, chrome := state.look()
				requireMarkdownTextColor(context, driver.Snapshot(), "Other body", chrome.ink)
				if state.session.Current().Dirty() {
					context.Fatal("opening another document kind made it dirty")
				}
			})
		}
	}
}
