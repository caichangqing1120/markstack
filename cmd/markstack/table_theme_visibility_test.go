package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
)

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
