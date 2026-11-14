package main

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"

	"markstack/internal/markdownedit"
)

const findMappingBody = "\u524d \U0001f469\u200d\U0001f4bb Needle \u4e2d Needle \u540e e\u0301"

func TestRenderedFindPlainMatchSurvivesUnrelatedFallbackBlocks(test *testing.T) {
	for _, scenario := range []struct {
		name   string
		source string
	}{
		{"quote-before", "# \u6807\u9898\n\n> first\n>\n> second\n\n" + findMappingBody + "\n\n# Footer\n"},
		{"quote-after", "# \u6807\u9898\n\n" + findMappingBody + "\n\n> first\n>\n> second\n\n# Footer\n"},
		{"nested-list-before", "# \u6807\u9898\n\n- parent\n  - child\n\n" + findMappingBody + "\n\n# Footer\n"},
		{"nested-list-after", "# \u6807\u9898\n\n" + findMappingBody + "\n\n- parent\n  - child\n\n# Footer\n"},
		{"setext-before", "# \u6807\u9898\n\nsetext\n------\n\n" + findMappingBody + "\n\n# Footer\n"},
		{"reference-before", "# \u6807\u9898\n\n[ref]: https://example.invalid\n\n" + findMappingBody + "\n\n# Footer\n"},
	} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{740, 1200} {
				test.Run(fmt.Sprintf("%s/dark-%v/width-%d", scenario.name, dark, width), func(context *testing.T) {
					state, driver, filename, original := renderedSourceCollisionFixture(context, scenario.source, dark, width)
					target := -1
					for index := range state.parsed.Blocks {
						if renderedPlain(&state.parsed, index) == findMappingBody {
							target = index
							break
						}
					}
					if target < 0 {
						context.Fatal("the plain target is missing from the rendered fixture")
					}
					clickID(context, driver, renderedRows.At(target))
					if state.mode != renderedMode || !state.rich.active || state.rich.input.String() != findMappingBody {
						context.Fatal("the fixture target is not an existing supported editor")
					}
					source := state.session.Current().Text()
					clickID(context, driver, findDocumentID)
					if err := driver.Type("Needle"); err != nil {
						context.Fatal(err)
					}
					if state.mode != renderedMode || state.findMode != renderedMode || len(state.findVisual) != 2 {
						context.Errorf("unrelated fallback blocks moved a plain match to source: mode=%v find-mode=%v visual=%+v", state.mode, state.findMode, state.findVisual)
					}
					if err := driver.Key(gfx.KeyEnter, 0); err != nil {
						context.Fatal(err)
					}
					if err := driver.Key(gfx.KeyEscape, 0); err != nil {
						context.Fatal(err)
					}
					if state.mode != renderedMode || !state.rich.active || driver.Snapshot().Focused != richInputID {
						context.Errorf("find did not return to the supported rendered target: mode=%v active=%v focus=%v", state.mode, state.rich.active, driver.Snapshot().Focused)
					} else {
						selection := state.rich.input.Selection()
						wantedStart := utf8.RuneCountInString(findMappingBody[:strings.LastIndex(findMappingBody, "Needle")])
						if selection.Start != wantedStart || selection.End != wantedStart+len("Needle") {
							context.Errorf("find selected the wrong Unicode occurrence: %+v", selection)
						}
					}
					if err := driver.Type("\u65b0"); err != nil {
						context.Fatal(err)
					}
					position := strings.LastIndex(source, "Needle")
					wantedSource := source[:position] + "\u65b0" + source[position+len("Needle"):]
					wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(wantedSource)...)
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

func TestRenderedFindKeepsComplexMatchFallbackWithRepeatedText(test *testing.T) {
	for _, prefix := range []struct {
		name   string
		source string
	}{
		{"inline-code", "`Needle`"},
		{"bold", "**Needle**"},
		{"link", "[Needle](https://example.invalid)"},
		{"quoted-fence", "> ```md\n> Needle\n> ```"},
	} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{740, 1200} {
				test.Run(fmt.Sprintf("%s/dark-%v/width-%d", prefix.name, dark, width), func(context *testing.T) {
					source := "# \u6807\u9898\n\n" + prefix.source + "\n\n" + findMappingBody + "\n"
					state, driver, filename, original := renderedSourceCollisionFixture(context, source, dark, width)
					clickID(context, driver, findDocumentID)
					if err := driver.Type("Needle"); err != nil {
						context.Fatal(err)
					}
					if state.mode != editMode || len(state.findVisual) != 0 || len(state.findMatches) != 3 {
						context.Fatalf("complex match lost its explicit source fallback: mode=%v visual=%+v matches=%+v", state.mode, state.findVisual, state.findMatches)
					}
					currentSource := state.session.Current().Text()
					wantedStart := utf8.RuneCountInString(currentSource[:strings.Index(currentSource, "Needle")])
					selection := state.session.Current().Editor.Selection()
					if selection.Start != wantedStart || selection.End != wantedStart+len("Needle") {
						context.Errorf("fallback did not select the first source occurrence: %+v", selection)
					}
					if err := driver.Key(gfx.KeyEscape, 0); err != nil {
						context.Fatal(err)
					}
					if driver.Snapshot().Focused != sourceID {
						context.Fatal("closing the complex search did not retain source focus")
					}
					expectRenderedCollisionSave(context, state, driver, filename, original)
				})
			}
		}
	}
}

func TestRenderedFindManyUnmappableBlocks(test *testing.T) {
	const count = 4000
	source := strings.Repeat("```\nbody\n```\n\n", count) + "Needle\n"
	for _, scenario := range []struct {
		query   string
		mode    viewMode
		matches int
	}{
		{"absent", renderedMode, 0},
		{"Needle", renderedMode, 1},
		{"body", editMode, count},
	} {
		test.Run(scenario.query, func(context *testing.T) {
			state := newModel()
			state.session.Current().Replace(source)
			state.mode, state.findOpen = renderedMode, true
			state.findEditor.Insert(scenario.query)
			state.syncDocumentFind()
			if state.mode != scenario.mode || state.findMode != scenario.mode || len(state.findMatches) != scenario.matches {
				context.Fatalf("large-document find: mode=%v find-mode=%v matches=%d", state.mode, state.findMode, len(state.findMatches))
			}
			if scenario.query == "Needle" && (len(state.findVisual) != 1 || state.findVisual[0].block != count) {
				context.Fatalf("plain match mapped to an unsupported block: %+v", state.findVisual)
			}
			if state.session.Current().Text() != source {
				context.Fatal("large-document search changed the source")
			}
		})
	}
}

func BenchmarkRenderedMappingUnmappableBlocks(benchmark *testing.B) {
	for _, count := range []int{1000, 10000, 100000} {
		benchmark.Run(fmt.Sprintf("blocks-%d", count), func(context *testing.B) {
			source := strings.Repeat("```\nx\n```\n\n", count)
			var document markdown.Document
			document.Parse(source)
			blocks := markdownedit.Parse([]byte(source))
			if len(document.Blocks) != count || len(blocks) != count {
				context.Fatal("benchmark requires one unmappable source block per rendered block")
			}
			context.SetBytes(int64(len(source)))
			context.ReportAllocs()
			context.ResetTimer()
			var mapping []int
			for range context.N {
				mapping = renderedMapping(&document, blocks)
			}
			context.StopTimer()
			if len(mapping) != count || mapping[0] != -1 || mapping[count-1] != -1 {
				context.Fatal("unmappable blocks acquired an editable mapping")
			}
		})
	}
}
