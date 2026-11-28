package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestRenderedNavigationInterleavedHistoryKeepsTargets(test *testing.T) {
	for _, kind := range []string{"format", "cross", "structure", "virtual"} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{740, 1200} {
				test.Run(fmt.Sprintf("%s/dark-%v/width-%d", kind, dark, width), func(context *testing.T) {
					const prefix = "<!-- untouched -->\n\n"
					const suffix = "\n\n## Target\n\nTail **keep** &amp; <!-- opaque -->\n"
					body, beforeBody, afterBody := "abcd", "abcd", "a**bc**d"
					beforeRow, afterRow, targetRow := 1, 1, 2
					beforeSelection, afterSelection := text.Range{Start: 1, End: 3}, text.Range{Start: 1, End: 3}
					if kind == "cross" {
						body, beforeBody, afterBody = "ab **cd** efgh", "ab **cd** efgh", "aXgh"
						beforeSelection, afterSelection = text.Range{Start: 1, End: 8}, text.Range{Start: 2, End: 2}
					} else if kind == "structure" {
						afterBody = "ab\n\ncd"
						afterRow, targetRow = 2, 3
						beforeSelection, afterSelection = text.Range{Start: 2, End: 2}, text.Range{}
					} else if kind == "virtual" {
						beforeBody, afterBody = "abcd\n\n", "abcd\n\nQ"
						beforeRow, afterRow, targetRow = 2, 2, 3
						beforeSelection, afterSelection = text.Range{}, text.Range{Start: 1, End: 1}
					}
					state, driver, filename, original := renderedSourceCollisionFixture(context, prefix+body+suffix, dark, width)
					clickID(context, driver, renderedRows.At(1))
					switch kind {
					case "format":
						state.rich.input.SetSelection(beforeSelection)
						state.applyBoldBlock()
						if err := driver.Pump(); err != nil {
							context.Fatal(err)
						}
					case "cross":
						state.rich.input.SetSelection(beforeSelection)
						if err := driver.Type("X"); err != nil {
							context.Fatal(err)
						}
					case "structure", "virtual":
						caret := 2
						if kind == "virtual" {
							caret = 4
						}
						state.rich.input.MoveTo(caret, false)
						if err := driver.Key(gfx.KeyEnter, 0); err != nil {
							context.Fatal(err)
						}
						if kind == "virtual" {
							if err := driver.Type("Q"); err != nil {
								context.Fatal(err)
							}
						}
					}
					clickID(context, driver, renderedRows.At(targetRow))
					state.rich.input.MoveTo(6, false)
					if err := driver.Type("Z"); err != nil {
						context.Fatal(err)
					}
					for cycle := 0; cycle < 2; cycle++ {
						for _, step := range []struct {
							command   rune
							body      string
							suffix    string
							row       int
							selection text.Range
							virtual   bool
						}{
							{'z', afterBody, suffix, targetRow, text.Range{Start: 6, End: 6}, false},
							{'z', beforeBody, suffix, beforeRow, beforeSelection, kind == "virtual"},
							{'y', afterBody, suffix, afterRow, afterSelection, kind == "virtual"},
							{'y', afterBody, strings.Replace(suffix, "Target", "TargetZ", 1), targetRow, text.Range{Start: 7, End: 7}, false},
						} {
							if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
								context.Fatal(err)
							}
							if state.mode != renderedMode || !state.rich.active || state.rich.virtual != step.virtual || state.rich.index != step.row || state.rich.input.Selection() != step.selection || state.rich.input.Caret() != step.selection.End || driver.Snapshot().Focused != richInputID {
								context.Fatalf("interleaved navigation history lost target: command=%c mode=%v active=%v virtual=%v row=%d", step.command, state.mode, state.rich.active, state.rich.virtual, state.rich.index)
							}
							wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(prefix+step.body+step.suffix, "\n", "\r\n"))...)
							expectRenderedCollisionSave(context, state, driver, filename, wanted)
						}
					}
					undoCount := 2
					if kind == "virtual" {
						undoCount++
					}
					for range undoCount {
						if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
							context.Fatal(err)
						}
					}
					if state.session.Current().Editor.CanUndo() {
						context.Fatal("interleaved navigation invented Source undo units")
					}
					expectRenderedCollisionSave(context, state, driver, filename, original)
				})
			}
		}
	}
}

func TestRenderedNavigationDuplicateFormatTargets(test *testing.T) {
	for _, format := range []string{"bold", "heading", "quote", "list", "ordered", "task"} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{740, 1200} {
				test.Run(fmt.Sprintf("%s/dark-%v/width-%d", format, dark, width), func(context *testing.T) {
					state, driver, filename, original := renderedSourceCollisionFixture(context, "same\n\nsame\n\nsame\n", dark, width)
					clickID(context, driver, renderedRows.At(1))
					state.rich.input.SetSelection(text.Range{Start: 3, End: 1})
					middle := "s**am**e"
					switch format {
					case "bold":
						state.applyBoldBlock()
					case "heading":
						middle = "### same"
						state.applyHeadingLevel(3)
					default:
						option := map[string]int{"quote": 0, "list": 1, "ordered": 2, "task": 3}[format]
						middle = []string{"> same", "- same", "1. same", "- [ ] same"}[option]
						state.applyBlockFormat(option)
					}
					if err := driver.Pump(); err != nil {
						context.Fatal(err)
					}
					changed := append([]byte{0xef, 0xbb, 0xbf}, []byte("same\r\n\r\n"+middle+"\r\n\r\nsame\r\n")...)
					clickID(context, driver, renderedRows.At(2))
					for cycle := 0; cycle < 2; cycle++ {
						for _, command := range []rune{'z', 'y'} {
							if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
								context.Fatal(err)
							}
							if state.mode != renderedMode || !state.rich.active || state.rich.index != 1 || state.rich.input.String() != "same" || state.rich.input.Selection() != (text.Range{Start: 1, End: 3}) || state.rich.input.Caret() != 1 || driver.Snapshot().Focused != richInputID {
								context.Fatalf("duplicate %s target lost its Source identity or oriented selection: command=%c mode=%v row=%d", format, command, state.mode, state.rich.index)
							}
							wanted := original
							if command == 'y' {
								wanted = changed
							}
							expectRenderedCollisionSave(context, state, driver, filename, wanted)
						}
					}
				})
			}
		}
	}
}

func TestRenderedNavigationOtherHistoryRejectsUnsafeSource(test *testing.T) {
	for _, unsafe := range []string{"readonly", "block-count", "multiple-blocks", "document", "source", "selection"} {
		test.Run(unsafe, func(context *testing.T) {
			body := "abcd\n\nother\n\nlast\n"
			if unsafe == "readonly" {
				body = "abcd\n\n` other `\n\nlast\n"
			}
			state, driver, _, _ := renderedSourceCollisionFixture(context, body, false, 1200)
			clickID(context, driver, renderedRows.At(0))
			current := state.session.Current()
			beforeSource := current.Text()
			start := strings.Index(beforeSource, "other")
			end, inserted := start+len("other"), "OTHER"
			if unsafe == "block-count" {
				inserted += "\r\n\r\nextra"
			} else if unsafe == "multiple-blocks" {
				start, inserted = 0, "ABCD\r\n\r\nOTHER"
			}
			if err := current.EditRange(start, end, inserted); err != nil {
				context.Fatal(err)
			}
			switch unsafe {
			case "document":
				state.rich.documentID++
			case "source":
				state.rich.source += "stale"
			case "selection":
				current.Editor.SetSelection(text.Range{Start: 0, End: 1})
			}
			input, source := state.rich.input, current.Text()
			selection := current.Editor.Selection()
			canUndo, canRedo := current.Editor.CanUndo(), current.Editor.CanRedo()
			if state.restoreOtherRenderedHistory(current, beforeSource, app.TextRedo) || state.rich.input != input || current.Text() != source || current.Editor.Selection() != selection || current.Editor.CanUndo() != canUndo || current.Editor.CanRedo() != canRedo {
				context.Fatal("unsafe Source interval changed history or activated a rendered target")
			}
		})
	}
}
