package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func TestReadingThemeTableAndQuoteSurfacesStayVisible(t *testing.T) {
	const source = "> A quote\n\n| Header | Second |\n| --- | --- |\n| First row | Cell |\n| Stripe | Cell |\n"
	for _, theme := range []string{"standard", "soft", "github", "parchment", "academic", "nord"} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dark=%v", theme, dark), func(t *testing.T) {
				state, driver := testDriver(t)
				state.preferences.ReadingTheme, state.dark = theme, dark
				state.session.Current().Replace(source)
				if err := driver.Resize(740, 520); err != nil {
					t.Fatal(err)
				}
				for _, mode := range []gfx.ID{readModeID, renderedModeID} {
					clickID(t, driver, mode)
					_, palette := state.look()
					appearance := state.markdownAppearance(palette)
					frame := driver.Snapshot()
					requireMarkdownTextColor(t, frame, "Header", appearance.palette.ink, state.previewRect)
					requireMarkdownTextColor(t, frame, "A quote", appearance.palette.muted, state.previewRect)
					x, _ := state.markdownColumn(state.previewRect)
					top := state.previewRect.Y + 46 - state.previewScroll
					quote, table := state.preview.blocks[0], state.preview.blocks[1]
					quoteFill := appearance.quoteFill
					if quoteFill.Alpha == 0 {
						quoteFill = appearance.palette.background
					}
					for _, sample := range []struct {
						name  string
						point ui.Point
						want  ui.Color
					}{
						{"quote bar", ui.Point{X: x + 3, Y: top + quote.top + 4}, appearance.quoteBar},
						{"quote surface", ui.Point{X: x + 7, Y: top + quote.top + 4}, quoteFill},
						{"table header", ui.Point{X: x + 4, Y: top + table.top + 4}, appearance.tableHeader},
						{"table edge", ui.Point{X: x, Y: top + table.top + 4}, appearance.tableBorder},
					} {
						if got := markdownPaintedColorAt(frame, sample.point); got != sample.want {
							t.Errorf("%s = %v, want %v", sample.name, got, sample.want)
						}
					}
				}
				if state.session.Current().Text() != source {
					t.Fatal("switching modes changed the source")
				}
			})
		}
	}
}

func TestRenderedTableCellRemainsVisibleWhenChromeTakesFocus(test *testing.T) {
	original := []byte("\xef\xbb\xbf# Title\r\n\r\n| Left | Right |\r\n| --- | ---: |\r\n| one | two |\r\n| three | four |\r\n")
	for _, dark := range []bool{false, true} {
		for _, width := range []int{740, 1200} {
			for _, control := range []struct {
				name string
				id   gfx.ID
			}{{"theme", themeToggleID}, {"outline", outlineToggleID}, {"sidebar", sidebarToggleID}} {
				test.Run(fmt.Sprintf("dark-%v/width-%d/%s", dark, width, control.name), func(context *testing.T) {
					filename := filepath.Join(context.TempDir(), "table.md")
					if err := os.WriteFile(filename, original, 0600); err != nil {
						context.Fatal(err)
					}
					state, driver := testDriver(context)
					state.dark = dark
					state.openFile(filename)
					if err := driver.Resize(width, 780); err != nil {
						context.Fatal(err)
					}
					clickID(context, driver, renderedModeID)
					clickID(context, driver, renderedTableCells.At(4))
					selection := state.table.input.Selection()
					clickID(context, driver, control.id)
					frame := driver.Snapshot()
					if frame.Focused != control.id || frame.Editor != nil {
						context.Fatalf("toolbar focus=%v editor=%+v", frame.Focused, frame.Editor)
					}
					if !state.table.active || state.table.input.Selection() != selection {
						context.Fatal("toolbar changed the active table selection")
					}
					requireMarkdownTextColor(context, frame, "three", referenceMarkdownColors(state.dark).palette.ink, state.previewRect)
					clickID(context, driver, tableInputID)
					if frame := driver.Snapshot(); frame.Focused != tableInputID || frame.Editor == nil || frame.Editor.Value != "three" {
						context.Fatalf("returning to table lost the editor: %+v", frame.Editor)
					}
					if state.session.Current().Dirty() || state.session.Current().Text() != string(original[3:]) {
						context.Fatal("toolbar transition changed the source")
					}
					if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
						context.Fatal(err)
					}
					if saved, err := os.ReadFile(filename); err != nil || !bytes.Equal(saved, original) {
						context.Fatalf("toolbar transition changed saved BOM/CRLF bytes: %q, %v", saved, err)
					}
				})
			}
		}
	}
}
