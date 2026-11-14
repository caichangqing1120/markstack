package main

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
)

func TestRenderedSupportedClipboardModeHistoryMatrix(test *testing.T) {
	const body = "\u4e2d \U0001f469\u200d\U0001f4bb e\u0301Z"
	cases := []struct {
		name     string
		fragment string
		input    gfx.ID
	}{
		{"paragraph", body + "\n", richInputID},
		{"quote", "> " + body + "\n", richInputID},
		{"unordered", "- " + body + "\n", richInputID},
		{"ordered", "7. " + body + "\n", richInputID},
		{"task", "- [ ] " + body + "\n", richInputID},
		{"bold", "**" + body + "**\n", richInputID},
		{"italic", "*" + body + "*\n", richInputID},
		{"strike", "~~" + body + "~~\n", richInputID},
		{"link", "[" + body + "](https://example.invalid/raw?a=1&b=2)\n", richInputID},
		{"fence", "```go\n" + body + "\n```\n", codeInputID},
		{"table", "| A | B |\n| :--- | ---: |\n| old | " + body + " |\n", tableInputID},
	}
	for level := 1; level <= 6; level++ {
		cases = append(cases, struct {
			name     string
			fragment string
			input    gfx.ID
		}{fmt.Sprintf("heading-%d", level), strings.Repeat("#", level) + " " + body + "\n", richInputID})
	}
	for _, scenario := range cases {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{740, 1200} {
				test.Run(fmt.Sprintf("%s/dark-%v/width-%d", scenario.name, dark, width), func(context *testing.T) {
					source := "<!-- keep: untouched -->\n\n" + scenario.fragment + "\nTail **keep** &amp; <!-- opaque -->\n"
					state, driver, filename, original := renderedSourceCollisionFixture(context, source, dark, width)
					if scenario.input == tableInputID {
						clickID(context, driver, renderedTableCells.At(3))
					} else {
						clickID(context, driver, renderedRows.At(1))
					}
					current := state.session.Current()
					documentID, tabs := current.ID, len(state.session.Tabs)
					originalSource := current.Text()
					assertRendered := func(wanted string) {
						context.Helper()
						if state.mode != renderedMode || driver.Snapshot().Focused != scenario.input || current.Text() != wanted {
							context.Fatalf("supported workflow mode=%v focus=%v source=%q, want %q", state.mode, driver.Snapshot().Focused, current.Text(), wanted)
						}
					}
					assertRendered(originalSource)
					for _, key := range []struct {
						key       gfx.Key
						modifiers gfx.Modifiers
					}{{gfx.KeyEnd, 0}, {gfx.KeyLeft, 0}, {gfx.KeyLeft, gfx.ModifierShift}} {
						if err := driver.Key(key.key, key.modifiers); err != nil {
							context.Fatal(err)
						}
					}
					input := state.rich.input
					if scenario.input == codeInputID {
						input = state.codeBlock.input
					} else if scenario.input == tableInputID {
						input = state.table.input
					}
					if input == nil || input.String() != body {
						context.Fatal("supported target did not retain its visible input")
					}
					selection := input.Selection()
					end := utf8.RuneCountInString(body) - 1
					if selection.Start != end-2 || selection.End != end {
						context.Fatalf("Shift+Left split or skipped the decomposed grapheme: %+v", selection)
					}
					if _, err := driver.ShortcutChord(primaryModifier(), 'c'); err != nil {
						context.Fatal(err)
					}
					if driver.Clipboard() != "e\u0301" || current.Dirty() {
						context.Fatalf("visible copy=%q dirty=%v", driver.Clipboard(), current.Dirty())
					}
					if _, err := driver.ShortcutChord(primaryModifier(), 'x'); err != nil {
						context.Fatal(err)
					}
					cutSource := strings.Replace(originalSource, body, strings.Replace(body, "e\u0301", "", 1), 1)
					assertRendered(cutSource)
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
					assertRendered(originalSource)
					if current.Dirty() {
						context.Fatal("cut Undo did not restore the clean document")
					}
					if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
						context.Fatal(err)
					}
					assertRendered(cutSource)
					if scenario.input == tableInputID {
						selection := state.table.input.Selection()
						caret := utf8.RuneCountInString(body) - 3
						if selection.Start != caret || selection.End != caret {
							context.Fatalf("Redo moved an interior cut to the cell end: %+v, want %d", selection, caret)
						}
					}
					if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
						context.Fatal(err)
					}
					assertRendered(originalSource)
					expectRenderedCollisionSave(context, state, driver, filename, original)
					if err := driver.Key(gfx.KeyEnd, 0); err != nil {
						context.Fatal(err)
					}
					if err := driver.Type("\u65b0"); err != nil {
						context.Fatal(err)
					}
					wantedSource := strings.Replace(originalSource, body, body+"\u65b0", 1)
					assertRendered(wantedSource)
					for _, mode := range []struct {
						id   gfx.ID
						mode viewMode
					}{{editModeID, editMode}, {renderedModeID, renderedMode}, {splitModeID, splitMode}, {readModeID, readMode}, {editModeID, editMode}} {
						clickID(context, driver, mode.id)
						if state.mode != mode.mode || state.session.Current().ID != documentID || len(state.session.Tabs) != tabs || current.Text() != wantedSource || !current.Dirty() {
							context.Fatal("mode switch changed document identity, source or dirty history")
						}
					}
					if err := driver.Key(gfx.KeyEnd, 0); err != nil {
						context.Fatal(err)
					}
					if driver.Snapshot().Focused != sourceID {
						context.Fatal("source mode did not accept the next keyboard command")
					}
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
					expectRenderedCollisionSave(context, state, driver, filename, original)
					if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
						context.Fatal(err)
					}
					wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(wantedSource)...)
					expectRenderedCollisionSave(context, state, driver, filename, wanted)
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
					expectRenderedCollisionSave(context, state, driver, filename, original)
				})
			}
		}
	}
}
