package main

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
)

func TestRenderedOutlineCollisionUsesHeadingSourceInterval(test *testing.T) {
	for _, scenario := range []struct {
		name   string
		source string
	}{
		{"quoted-backticks", "# \u524d\u8a00\n\n> ```md\n> ## Repeat\n> ```\n\n## Repeat\n\n## Repeat\n\nTail\n"},
		{"quoted-tildes", "# \u524d\u8a00\n\n> ~~~md\n> ## Repeat\n> ~~~\n\n## Repeat\n\n## Repeat\n\nTail\n"},
		{"nested-quote", "# \u524d\u8a00\n\n> > ```md\n> > ## Repeat\n> > ```\n\n## Repeat\n\n## Repeat\n\nTail\n"},
		{"formatted-code-text", "# \u524d\u8a00\n\n> ```md\n> ## **Repeat**\n> ```\n\n## Repeat\n\n## Repeat\n\nTail\n"},
	} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{1100, 1605} {
				test.Run(fmt.Sprintf("%s/dark-%v/width-%d", scenario.name, dark, width), func(context *testing.T) {
					state, driver, filename, original := renderedSourceCollisionFixture(context, scenario.source, dark, width)
					state.outline = true
					if err := driver.Resize(width, 780); err != nil {
						context.Fatal(err)
					}
					currentSource := state.session.Current().Text()
					targetStart := strings.Index(currentSource, "\r\n## Repeat\r\n") + 2
					targetRune := utf8.RuneCountInString(currentSource[:targetStart])
					targetIndex := -1
					for index, entry := range state.outlines {
						if entry.Rune == targetRune {
							targetIndex = index
							break
						}
					}
					if targetIndex < 0 {
						context.Fatalf("real heading missing from outline: %+v", state.outlines)
					}
					clickID(context, driver, outlineRows.At(targetIndex))
					if state.mode != renderedMode || !state.rich.active || driver.Snapshot().Focused != richInputID {
						context.Errorf("outline collision lost its editable target: mode=%v active=%v focus=%v", state.mode, state.rich.active, driver.Snapshot().Focused)
					}
					if err := driver.Type("X"); err != nil {
						context.Fatal(err)
					}
					targetEnd := strings.Index(scenario.source, "\n## Repeat\n") + len("\n## Repeat")
					wantedSource := scenario.source[:targetEnd] + "X" + scenario.source[targetEnd:]
					wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(wantedSource, "\n", "\r\n"))...)
					expectRenderedCollisionSave(context, state, driver, filename, wanted)
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
					expectRenderedCollisionSave(context, state, driver, filename, original)
					if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil {
						context.Fatal(err)
					}
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

func TestOutlineSkipsHeadingsInsideQuotedFences(test *testing.T) {
	for _, source := range []string{
		"# Before\n\n> ```md\n> ## Code\n> ```\n\n## After\n",
		"# Before\r\n\r\n> ~~~md\r\n> ## Code\r\n> ~~~\r\n\r\n## After\r\n",
		"# Before\n\n> > ```md\n> > ## Code\n> > ```\n\n## After\n",
	} {
		entries := outlineOf(source)
		if len(entries) != 2 || entries[0].Title != "Before" || entries[1].Title != "After" {
			test.Errorf("quoted code became an outline heading: %+v", entries)
		}
	}
}

func TestReadOutlineCollisionScrollsToHeadingSourceInterval(test *testing.T) {
	source := "# \u524d\u8a00\n\n> ```md\n> ## Repeat\n> ```\n\n## Repeat\n\n" + strings.Repeat("Body\n\n", 40) + "## Repeat\n"
	state, driver, filename, original := renderedSourceCollisionFixture(test, source, false, 1200)
	state.outline = true
	state.mode = readMode
	if err := driver.Resize(1200, 780); err != nil {
		test.Fatal(err)
	}
	currentSource := state.session.Current().Text()
	targetStart := strings.Index(currentSource, "\r\n## Repeat\r\n") + 2
	targetRune := utf8.RuneCountInString(currentSource[:targetStart])
	targetIndex := -1
	for index, entry := range state.outlines {
		if entry.Rune == targetRune {
			targetIndex = index
			break
		}
	}
	if targetIndex < 0 {
		test.Fatal("real heading missing from outline")
	}
	wantedScroll := float32(-1)
	for index, block := range state.parsed.Blocks {
		if block.Start == targetStart {
			wantedScroll = max(0, state.preview.blocks[index].top-8)
			break
		}
	}
	if wantedScroll < 0 {
		test.Fatal("real heading missing from rendered blocks")
	}
	clickID(test, driver, outlineRows.At(targetIndex))
	if state.mode != readMode || state.previewScroll != wantedScroll {
		test.Errorf("read outline scrolled to another heading: mode=%v scroll=%v, want %v", state.mode, state.previewScroll, wantedScroll)
	}
	expectRenderedCollisionSave(test, state, driver, filename, original)
}
