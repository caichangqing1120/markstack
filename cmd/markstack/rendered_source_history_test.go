package main

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestRenderedSourceHistoryRestoresVisibleSelectionAndDirection(test *testing.T) {
	const body = "a\u4e2d\u6587Z"
	const unicodePrefix = "\U0001f469\u200d\U0001f4bb e\u0301 "
	for _, scenario := range []struct {
		name     string
		fragment string
		visible  string
		start    int
	}{
		{"plain", body, body, 1},
		{"bold", "**" + body + "**", body, 1},
		{"italic", "_" + body + "_", body, 1},
		{"strike", "~~" + body + "~~", body, 1},
		{"link", "[" + body + "](https://example.invalid/raw?a=1&b=2)", body, 1},
		{"mixed-bold", "keep **" + body + "** tail", "keep " + body + " tail", 6},
		{"mixed-link", "keep [" + body + "](https://example.invalid/raw?a=1&b=2) tail", "keep " + body + " tail", 6},
		{"heading", "### " + body, body, 1},
		{"mixed-heading", "### keep **" + body + "** tail", "keep " + body + " tail", 6},
		{"quote", "> " + body, body, 1},
		{"unordered", "- " + body, body, 1},
		{"ordered", "7. " + body, body, 1},
		{"task", "- [ ] " + body, body, 1},
		{"unicode-prefix", unicodePrefix + "**" + body + "**", unicodePrefix + body, 8},
	} {
		for _, backward := range []bool{false, true} {
			for _, dark := range []bool{false, true} {
				for _, width := range []int{740, 1200} {
					test.Run(fmt.Sprintf("%s/backward-%v/dark-%v/width-%d", scenario.name, backward, dark, width), func(context *testing.T) {
						source := "<!-- untouched -->\n\n" + scenario.fragment + "\n\nTail **keep** &amp; <!-- opaque -->\n"
						state, driver, filename, original := renderedSourceCollisionFixture(context, source, dark, width)
						clickID(context, driver, editModeID)
						current := state.session.Current()
						before := current.Text()
						bodyStart := strings.Index(before, body)
						from := utf8.RuneCountInString(before[:bodyStart+len("a")])
						to := utf8.RuneCountInString(before[:bodyStart+len("a\u4e2d\u6587")])
						target := text.Range{Start: from, End: to}
						if backward {
							target.Start, target.End = target.End, target.Start
						}
						current.Editor.SetSelection(target)
						if err := driver.Type("X"); err != nil {
							context.Fatal(err)
						}
						edited := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.Replace(before, body, "aXZ", 1))...)
						expectRenderedCollisionSave(context, state, driver, filename, edited)
						clickID(context, driver, renderedModeID)
						clickID(context, driver, renderedRows.At(1))
						for _, command := range []rune{'z', 'y', 'z'} {
							if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
								context.Fatal(err)
							}
							if state.mode != renderedMode || !state.rich.active || state.rich.input == nil {
								context.Fatal("same-block Source history abandoned rendered editing")
							}
							if command == 'y' {
								caret := scenario.start + 1
								if selection := state.rich.input.Selection(); selection != (text.Range{Start: caret, End: caret}) {
									context.Fatalf("Redo used source markup as a visible offset: %+v, want caret %d", selection, caret)
								}
								expectRenderedCollisionSave(context, state, driver, filename, edited)
								continue
							}
							wanted := text.Range{Start: scenario.start, End: scenario.start + 2}
							caret := wanted.End
							if backward {
								caret = wanted.Start
							}
							if state.rich.input.String() != scenario.visible || state.rich.input.Selection() != wanted || state.rich.input.Caret() != caret {
								context.Fatalf("Undo lost visible selection/direction: selection=%+v caret=%d, want %+v caret=%d", state.rich.input.Selection(), state.rich.input.Caret(), wanted, caret)
							}
							if current.Editor.Selection() != (text.Range{Start: from, End: to}) || current.Editor.Caret() != target.End {
								context.Fatal("rendered restoration altered the authoritative Source selection")
							}
							expectRenderedCollisionSave(context, state, driver, filename, original)
						}
						key, changedBody := gfx.KeyLeft, "aQ\u6587Z"
						if backward {
							key, changedBody = gfx.KeyRight, "a\u4e2dQZ"
						}
						if err := driver.Key(key, gfx.ModifierShift); err != nil {
							context.Fatal(err)
						}
						if err := driver.Type("Q"); err != nil {
							context.Fatal(err)
						}
						continued := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.Replace(before, body, changedBody, 1))...)
						expectRenderedCollisionSave(context, state, driver, filename, continued)
						for _, step := range []struct {
							command rune
							bytes   []byte
						}{{'z', original}, {'y', continued}, {'z', original}} {
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
}

func TestRenderedSourceHistoryKeepsHiddenSyntaxSelectionsInSource(test *testing.T) {
	for _, scenario := range []struct {
		name        string
		fragment    string
		selected    string
		replacement string
	}{
		{"whole-style", "**abcd**", "**abcd**", "__abcd__"},
		{"link-destination", "[abcd](https://example.invalid/before?x=1)", "before", "after"},
	} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{740, 1200} {
				test.Run(fmt.Sprintf("%s/dark-%v/width-%d", scenario.name, dark, width), func(context *testing.T) {
					source := "<!-- untouched -->\n\n" + scenario.fragment + "\n\nTail **keep** &amp; <!-- opaque -->\n"
					state, driver, filename, original := renderedSourceCollisionFixture(context, source, dark, width)
					clickID(context, driver, editModeID)
					current := state.session.Current()
					before := current.Text()
					start := strings.Index(before, scenario.selected)
					target := text.Range{Start: utf8.RuneCountInString(before[:start]), End: utf8.RuneCountInString(before[:start+len(scenario.selected)])}
					current.Editor.SetSelection(target)
					if err := driver.Type(scenario.replacement); err != nil {
						context.Fatal(err)
					}
					clickID(context, driver, renderedModeID)
					clickID(context, driver, renderedRows.At(1))
					if !state.rich.active || state.mode != renderedMode {
						context.Fatal("safe updated style/link did not activate")
					}
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
					if state.mode != editMode || state.rich.active || current.Editor.Selection() != target {
						context.Fatalf("hidden Source syntax selection was silently projected: mode=%v selection=%+v", state.mode, current.Editor.Selection())
					}
					expectRenderedCollisionSave(context, state, driver, filename, original)
					if err := driver.Type("Q"); err != nil {
						context.Fatal(err)
					}
					wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.Replace(before, scenario.selected, "Q", 1))...)
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

func TestRenderedFormatHistoryKeepsItsOwnVisibleSelection(test *testing.T) {
	const body = "a\u4e2d\u6587Z"
	const destination = "https://example.invalid/raw?a=1&b=2"
	for _, scenario := range []struct {
		name      string
		button    gfx.ID
		formatted string
		whole     bool
		link      bool
	}{
		{"selected-bold", boldBlockID, "a**\u4e2d\u6587**Z", false, false},
		{"selected-italic", italicBlockID, "a*\u4e2d\u6587*Z", false, false},
		{"selected-strike", strikeBlockID, "a~~\u4e2d\u6587~~Z", false, false},
		{"selected-link", linkBlockID, "a[\u4e2d\u6587](" + destination + ")Z", false, true},
		{"whole-bold", boldBlockID, "**" + body + "**", true, false},
		{"whole-link", linkBlockID, "[" + body + "](" + destination + ")", true, true},
	} {
		directions := []bool{false, true}
		if scenario.whole {
			directions = []bool{false}
		}
		for _, backward := range directions {
			for _, dark := range []bool{false, true} {
				for _, width := range []int{740, 1200} {
					test.Run(fmt.Sprintf("%s/backward-%v/dark-%v/width-%d", scenario.name, backward, dark, width), func(context *testing.T) {
						source := "<!-- untouched -->\n\n" + body + "\n\nTail **keep** &amp; <!-- opaque -->\n"
						state, driver, filename, original := renderedSourceCollisionFixture(context, source, dark, width)
						clickID(context, driver, renderedRows.At(1))
						target := text.Range{Start: 1, End: 3}
						if scenario.whole {
							target.End = target.Start
						} else if backward {
							target.Start, target.End = target.End, target.Start
						}
						state.rich.input.SetSelection(target)
						if err := driver.Resize(width, 780); err != nil {
							context.Fatal(err)
						}
						clickID(context, driver, scenario.button)
						if scenario.link {
							if err := driver.Type(destination); err != nil {
								context.Fatal(err)
							}
							clickID(context, driver, linkApplyID)
						}
						formatted := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(strings.Replace(source, body, scenario.formatted, 1), "\n", "\r\n"))...)
						for _, command := range []rune{0, 'z', 'y', 'z'} {
							if command != 0 {
								if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
									context.Fatal(err)
								}
							}
							selection := text.Range{Start: 1, End: 3}
							if scenario.whole {
								selection.End = selection.Start
							}
							if state.mode != renderedMode || !state.rich.active || state.rich.input.Selection() != selection || state.rich.input.Caret() != target.End {
								context.Fatal("format transaction lost its visible selection, direction or rendered view")
							}
							wanted := formatted
							if command == 'z' {
								wanted = original
							}
							expectRenderedCollisionSave(context, state, driver, filename, wanted)
						}
					})
				}
			}
		}
	}
}

func TestRenderedFormatHistoryRetainsRepeatedTransactionsAndEarlierTyping(test *testing.T) {
	const body = "a\u4e2d\u6587Z"
	for _, backward := range []bool{false, true} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{740, 1200} {
				test.Run(fmt.Sprintf("backward-%v/dark-%v/width-%d", backward, dark, width), func(context *testing.T) {
					source := "<!-- untouched -->\n\n" + body + "\n\nTail **keep** &amp; <!-- opaque -->\n"
					state, driver, filename, original := renderedSourceCollisionFixture(context, source, dark, width)
					clickID(context, driver, renderedRows.At(1))
					if err := driver.Type("P"); err != nil {
						context.Fatal(err)
					}
					target := text.Range{Start: 1, End: 3}
					if backward {
						target.Start, target.End = target.End, target.Start
					}
					state.rich.input.SetSelection(target)
					if err := driver.Resize(width, 780); err != nil {
						context.Fatal(err)
					}
					plain := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(strings.Replace(source, body, body+"P", 1), "\n", "\r\n"))...)
					bold := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(strings.Replace(source, body, "a**\u4e2d\u6587**ZP", 1), "\n", "\r\n"))...)
					for _, wanted := range [][]byte{bold, plain, bold} {
						clickID(context, driver, boldBlockID)
						if state.rich.input.Selection() != (text.Range{Start: 1, End: 3}) || state.rich.input.Caret() != target.End {
							context.Fatal("repeated formatting flipped the selected active endpoint")
						}
						expectRenderedCollisionSave(context, state, driver, filename, wanted)
					}
					for _, step := range []struct {
						command rune
						bytes   []byte
					}{{'z', plain}, {'z', bold}, {'z', plain}, {'z', original}, {'y', plain}, {'y', bold}, {'y', plain}, {'y', bold}, {'z', plain}, {'z', bold}, {'z', plain}, {'z', original}} {
						if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
							context.Fatal(err)
						}
						if state.mode != renderedMode || !state.rich.active {
							context.Fatal("repeated format/source history abandoned rendered editing")
						}
						expectRenderedCollisionSave(context, state, driver, filename, step.bytes)
					}
					if state.session.Current().Editor.CanUndo() {
						context.Fatal("format annotations invented extra source undo units")
					}
				})
			}
		}
	}
}

func TestRenderedFormatHistoryMatchesSourceHistoryCapacity(test *testing.T) {
	const body = "a\u4e2d\u6587Z"
	source := "<!-- untouched -->\n\n" + body + "\n\nTail **keep** &amp; <!-- opaque -->\n"
	state, driver, filename, original := renderedSourceCollisionFixture(test, source, false, 1200)
	clickID(test, driver, renderedRows.At(1))
	before := state.session.Current().Text()
	bold := strings.Replace(before, body, "a**\u4e2d\u6587**Z", 1)
	state.rich.input.SetSelection(text.Range{Start: 1, End: 3})
	for operation := 0; operation < 260; operation++ {
		state.applyBoldBlock()
	}
	if err := driver.Resize(1200, 780); err != nil {
		test.Fatal(err)
	}
	for _, command := range []rune{'z', 'y'} {
		for operation := 1; operation <= 256; operation++ {
			if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
				test.Fatal(err)
			}
			wanted := before
			if operation%2 != 0 {
				wanted = bold
			}
			if state.mode != renderedMode || !state.rich.active || state.session.Current().Text() != wanted || state.rich.input.Selection() != (text.Range{Start: 1, End: 3}) {
				test.Fatalf("format history capacity lost transaction %c/%d: mode=%v source=%q", command, operation, state.mode, state.session.Current().Text())
			}
		}
		if command == 'z' && (state.session.Current().Editor.CanUndo() || len(state.rich.crossUndo) != 0) {
			test.Fatal("view metadata retained transactions beyond the source history capacity")
		}
	}
	if state.session.Current().Editor.CanRedo() {
		test.Fatal("format annotations invented redo units")
	}
	expectRenderedCollisionSave(test, state, driver, filename, original)
}

func TestRenderedFormatHistoryOrdersRepeatedStatesAndLaterTyping(test *testing.T) {
	for _, scenario := range []struct {
		name       string
		delete     bool
		laterInput bool
	}{
		{"repeated-source-state", true, false},
		{"later-input", false, true},
		{"repeated-state-and-later-input", true, true},
	} {
		for _, backward := range []bool{false, true} {
			for _, dark := range []bool{false, true} {
				for _, width := range []int{740, 1200} {
					test.Run(fmt.Sprintf("%s/backward-%v/dark-%v/width-%d", scenario.name, backward, dark, width), func(context *testing.T) {
						const body = "abcd"
						source := "<!-- untouched -->\n\n" + body + "\n\nTail **keep** &amp; <!-- opaque -->\n"
						state, driver, filename, original := renderedSourceCollisionFixture(context, source, dark, width)
						clickID(context, driver, renderedRows.At(1))
						if err := driver.Type("P"); err != nil {
							context.Fatal(err)
						}
						bodies := []string{body, body + "P"}
						if scenario.delete {
							if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
								context.Fatal(err)
							}
							bodies = append(bodies, body)
						}
						target := text.Range{Start: 1, End: 3}
						if backward {
							target.Start, target.End = target.End, target.Start
						}
						state.rich.input.SetSelection(target)
						if err := driver.Resize(width, 780); err != nil {
							context.Fatal(err)
						}
						clickID(context, driver, boldBlockID)
						formatIndex := len(bodies)
						bodies = append(bodies, strings.Replace(bodies[len(bodies)-1], "bc", "**bc**", 1))
						if scenario.laterInput {
							state.rich.input.MoveTo(4, false)
							if err := driver.Type("Q"); err != nil {
								context.Fatal(err)
							}
							bodies = append(bodies, strings.Replace(bodies[len(bodies)-1], "d", "dQ", 1))
						}
						for cycle := 0; cycle < 2; cycle++ {
							for _, command := range []rune{'z', 'y'} {
								for operation := 1; operation < len(bodies); operation++ {
									if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
										context.Fatal(err)
									}
									position := operation
									if command == 'z' {
										position = len(bodies) - operation - 1
									}
									if state.mode != renderedMode || !state.rich.active || state.session.Current().Text() != strings.ReplaceAll(strings.Replace(source, body, bodies[position], 1), "\n", "\r\n") {
										context.Fatalf("history cycle %d/%c/%d lost the source transaction order: mode=%v source=%q message=%q", cycle, command, operation, state.mode, state.session.Current().Text(), state.message)
									}
									if command == 'z' && position == formatIndex-1 || command == 'y' && position == formatIndex {
										if state.rich.input.Selection() != (text.Range{Start: 1, End: 3}) || state.rich.input.Caret() != target.End {
											context.Fatal("format history lost its visible selection or direction")
										}
									}
									wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(strings.Replace(source, body, bodies[position], 1), "\n", "\r\n"))...)
									expectRenderedCollisionSave(context, state, driver, filename, wanted)
								}
								current := state.session.Current()
								if command == 'z' && current.Editor.CanUndo() || command == 'y' && current.Editor.CanRedo() {
									context.Fatal("rendered history left unconsumed source transactions")
								}
							}
						}
						for operation := 1; operation < len(bodies); operation++ {
							if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
								context.Fatal(err)
							}
						}
						expectRenderedCollisionSave(context, state, driver, filename, original)
						state.rich.input.MoveTo(4, false)
						if err := driver.Type("R"); err != nil {
							context.Fatal(err)
						}
						continued := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(strings.Replace(source, body, body+"R", 1), "\n", "\r\n"))...)
						expectRenderedCollisionSave(context, state, driver, filename, continued)
						for _, step := range []struct {
							command rune
							bytes   []byte
						}{{'z', original}, {'y', continued}, {'z', original}} {
							if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
								context.Fatal(err)
							}
							if state.mode != renderedMode || !state.rich.active {
								context.Fatal("continued typing lost rendered history after a source history cycle")
							}
							expectRenderedCollisionSave(context, state, driver, filename, step.bytes)
						}
					})
				}
			}
		}
	}
}
