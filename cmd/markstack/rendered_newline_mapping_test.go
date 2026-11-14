package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestRenderedStyledEnterSplitsVerifiedSourceIntervals(test *testing.T) {
	for _, scenario := range []struct {
		name      string
		fragment  string
		caret     int
		split     string
		continued string
	}{
		{"plain-control", "abcd", 2, "ab\n\ncd", "ab\n\nQcd"},
		{"bold", "**abcd**", 2, "**ab**\n\n**cd**", "**ab**\n\n**Qcd**"},
		{"italic", "_abcd_", 2, "_ab_\n\n_cd_", "_ab_\n\n_Qcd_"},
		{"strike", "~~abcd~~", 2, "~~ab~~\n\n~~cd~~", "~~ab~~\n\n~~Qcd~~"},
		{"plain-after-bold", "**ab** cdef", 5, "**ab** cd\n\nef", "**ab** cd\n\nQef"},
		{"mixed-bold", "left **abcd** right", 7, "left **ab**\n\n**cd** right", "left **ab**\n\n**Qcd** right"},
		{"adjacent-styles", "**ab**_cd_", 2, "**ab**\n\n_cd_", "**ab**\n\n_Qcd_"},
		{"unicode-bold", "**\u4e2d\u6587**", 1, "**\u4e2d**\n\n**\u6587**", "**\u4e2d**\n\n**Q\u6587**"},
		{"heading", "## \u524d **\u4e2d\u6587** \u540e", 3, "## \u524d **\u4e2d**\n\n**\u6587** \u540e", "## \u524d **\u4e2d**\n\n**Q\u6587** \u540e"},
	} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{740, 1200} {
				test.Run(fmt.Sprintf("%s/dark-%v/width-%d", scenario.name, dark, width), func(context *testing.T) {
					prefix, suffix := "<!-- untouched -->\n\n", "\n\nTail **keep** &amp; <!-- opaque -->\n"
					state, driver, filename, original := renderedSourceCollisionFixture(context, prefix+scenario.fragment+suffix, dark, width)
					clickID(context, driver, renderedRows.At(1))
					if !state.rich.active || state.mode != renderedMode {
						context.Fatal("supported styled paragraph did not activate")
					}
					state.rich.input.MoveTo(scenario.caret, false)
					if err := driver.Key(gfx.KeyEnter, 0); err != nil {
						context.Fatal(err)
					}
					wantedSource := strings.ReplaceAll(prefix+scenario.split+suffix, "\n", "\r\n")
					if current := state.session.Current().Text(); current != wantedSource {
						context.Fatalf("Enter modified the wrong source interval: %q, want %q", current, wantedSource)
					}
					if state.mode != renderedMode || !state.rich.active || state.rich.input.Caret() != 0 {
						context.Fatalf("verified styled split abandoned editing: mode=%v active=%v", state.mode, state.rich.active)
					}
					wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(wantedSource)...)
					expectRenderedCollisionSave(context, state, driver, filename, wanted)
					if err := driver.Type("Q"); err != nil {
						context.Fatal(err)
					}
					continued := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(prefix+scenario.continued+suffix, "\n", "\r\n"))...)
					expectRenderedCollisionSave(context, state, driver, filename, continued)
					for _, step := range []struct {
						command rune
						bytes   []byte
					}{{'z', wanted}, {'z', original}, {'y', wanted}, {'y', continued}, {'z', wanted}, {'z', original}} {
						if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
							context.Fatal(err)
						}
						expectRenderedCollisionSave(context, state, driver, filename, step.bytes)
					}
				})
			}
		}
	}
}

func TestRenderedEnterFallbackKeepsVerifiedSelectionBytes(test *testing.T) {
	for _, scenario := range []struct {
		name     string
		fragment string
		target   text.Range
		wanted   string
		refused  bool
	}{
		{"plain-control", "abcd", text.Range{Start: 1, End: 3}, "a\nd", false},
		{"bold-selection", "**abcd**", text.Range{Start: 1, End: 3}, "**a**\n**d**", false},
		{"cross-style-selection", "**abcd** efgh", text.Range{Start: 2, End: 7}, "**ab**\ngh", false},
		{"partial-link", "[abcd](https://example.invalid/raw?a=1&b=2)", text.Range{Start: 2, End: 2}, "[ab\ncd](https://example.invalid/raw?a=1&b=2)", false},
		{"partial-link-cross-refused", "[abcd](https://example.invalid/raw?a=1&b=2) tail", text.Range{Start: 2, End: 7}, "[abcd](https://example.invalid/raw?a=1&b=2) tail", true},
		{"nested-cross-refused", "**ab *cd* ef** tail", text.Range{Start: 1, End: 10}, "**ab *cd* ef** tail", true},
		{"bold-start", "**abcd**", text.Range{Start: 0, End: 0}, "\n**abcd**", false},
		{"bold-start-selection", "**abcd**", text.Range{Start: 0, End: 2}, "\n**cd**", false},
		{"bold-end-selection", "**abcd**", text.Range{Start: 2, End: 4}, "**ab**\n", false},
		{"bold-all", "**abcd**", text.Range{Start: 0, End: 4}, "\n", false},
	} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{740, 1200} {
				test.Run(fmt.Sprintf("%s/dark-%v/width-%d", scenario.name, dark, width), func(context *testing.T) {
					prefix, suffix := "<!-- untouched -->\n\n", "\n\nTail **keep** &amp; <!-- opaque -->\n"
					state, driver, filename, original := renderedSourceCollisionFixture(context, prefix+scenario.fragment+suffix, dark, width)
					clickID(context, driver, renderedRows.At(1))
					if !state.rich.active || state.mode != renderedMode {
						context.Fatal("supported fallback fixture did not activate")
					}
					state.rich.input.SetSelection(scenario.target)
					if err := driver.Key(gfx.KeyEnter, 0); err != nil {
						context.Fatal(err)
					}
					wantedSource := strings.ReplaceAll(prefix+scenario.wanted+suffix, "\n", "\r\n")
					if current := state.session.Current().Text(); current != wantedSource {
						context.Fatalf("fallback Enter changed unrelated source bytes: %q, want %q", current, wantedSource)
					}
					wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(wantedSource)...)
					if scenario.refused {
						if state.mode != renderedMode || !state.rich.active || state.rich.input.Selection() != scenario.target || state.session.Current().Editor.CanUndo() || state.message == "" {
							context.Fatal("unsafe Enter lost selection, created history, or failed to report refusal")
						}
						expectRenderedCollisionSave(context, state, driver, filename, original)
						return
					}
					expectRenderedCollisionSave(context, state, driver, filename, wanted)
					if state.mode != editMode {
						context.Fatal("unsafe multiline layout was not explicitly handed to Source")
					}
					for _, step := range []struct {
						command rune
						bytes   []byte
					}{{'z', original}, {'y', wanted}, {'z', original}} {
						if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
							context.Fatal(err)
						}
						expectRenderedCollisionSave(context, state, driver, filename, step.bytes)
					}
				})
			}
		}
	}
}
