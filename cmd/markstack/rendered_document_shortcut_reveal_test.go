package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestRenderedDocumentShortcutRevealsHistoryTarget(test *testing.T) {
	for _, kind := range []string{"typing", "structure", "virtual", "virtual-text", "wrapped", "mixed", "table", "fence"} {
		for _, width := range []int{740, 1200} {
			test.Run(fmt.Sprintf("%s/width-%d", kind, width), func(context *testing.T) {
				body := "TARGET\n\nTail\n"
				if kind == "virtual" || kind == "virtual-text" {
					body = "TARGET"
				} else if kind == "wrapped" {
					body = strings.Repeat("long target text ", 250) + "\n\nTail\n"
				} else if kind == "mixed" {
					body = "**bold** " + strings.Repeat("long target text ", 250) + "\n\nTail\n"
				} else if kind == "table" || kind == "fence" {
					body = specialHistoryPrefix(kind) + "Tail\n"
				}
				state, driver, filename, original := renderedSourceCollisionFixture(context, strings.Repeat("spacer\n\n", 40)+body, false, width)
				if err := driver.Scroll(gfx.ScrollEvent{X: state.previewRect.X + 20, Y: state.previewRect.Y + 20, DeltaY: state.preview.blocks[40].top - 40}); err != nil {
					context.Fatal(err)
				}
				focus := richInputID
				if kind == "table" || kind == "fence" {
					activateHistorySpecial(context, state, driver, kind)
					input, id := specialHistoryInput(state, kind)
					focus = id
					input.MoveTo(input.RuneCount(), false)
				} else {
					clickID(context, driver, renderedRows.At(40))
					state.rich.input.MoveTo(state.rich.input.RuneCount(), false)
				}
				if kind == "structure" || kind == "virtual" || kind == "virtual-text" {
					if kind == "structure" {
						state.rich.input.MoveTo(3, false)
					}
					if err := driver.Key(gfx.KeyEnter, 0); err != nil {
						context.Fatal(err)
					}
					if kind == "virtual-text" {
						if err := driver.Scroll(gfx.ScrollEvent{X: state.previewRect.X + 20, Y: state.previewRect.Y + 20, DeltaY: 100}); err != nil {
							context.Fatal(err)
						}
						clickID(context, driver, richInputID)
					}
				}
				before := original
				if kind == "virtual-text" {
					before = append([]byte{0xef, 0xbb, 0xbf}, []byte(state.session.Current().Text())...)
				}
				if kind != "structure" && kind != "virtual" {
					if err := driver.Type("Q"); err != nil {
						context.Fatal(err)
					}
				}
				changed := append([]byte{0xef, 0xbb, 0xbf}, []byte(state.session.Current().Text())...)
				navigateDocumentWithoutInput(context, state, driver, false)
				if state.previewScroll != 0 {
					context.Fatal("fixture did not return to the top of the document")
				}
				for range 2 {
					for _, command := range []rune{'z', 'y'} {
						if handled, err := driver.ShortcutChord(primaryModifier(), command); err != nil || !handled {
							context.Fatalf("history shortcut stopped: command=%c handled=%v error=%v", command, handled, err)
						}
						wanted := before
						if command == 'y' {
							wanted = changed
						}
						if state.mode != renderedMode || driver.Snapshot().Focused != focus || state.previewScroll == 0 {
							context.Fatalf("offscreen history target did not acquire visible focus: command=%c focus=%v scroll=%v", command, driver.Snapshot().Focused, state.previewScroll)
						}
						if state.rich.active {
							caret, ok := state.rich.CaretBounds()
							if !ok || caret.Y < state.previewRect.Y || caret.Y+caret.Height > state.previewRect.Y+state.previewRect.Height {
								context.Fatalf("restored caret is outside viewport: command=%c caret=%v viewport=%v", command, caret, state.previewRect)
							}
						}
						expectRenderedCollisionSave(context, state, driver, filename, wanted)
					}
				}
				if err := driver.Type("R"); err != nil {
					context.Fatal(err)
				}
				if state.session.Current().Text() == string(changed[3:]) || driver.Snapshot().Focused != focus {
					context.Fatal("typing did not continue at the restored history target")
				}
				for state.session.Current().Editor.CanUndo() {
					previous := state.session.Current().Text()
					if handled, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil || !handled || previous == state.session.Current().Text() {
						context.Fatal("continuous Undo stalled before original source")
					}
				}
				expectRenderedCollisionSave(context, state, driver, filename, original)
			})
		}
	}
}

func TestRenderedDocumentShortcutKeepsActiveInputHistory(test *testing.T) {
	for _, kind := range []string{"typing", "structure", "virtual", "table", "fence"} {
		for _, toolbar := range []gfx.ID{sidebarToggleID, outlineToggleID} {
			test.Run(fmt.Sprintf("%s/%v", kind, toolbar), func(context *testing.T) {
				var expected []string
				for _, nonText := range []bool{false, true} {
					body := "TARGET\n\nTail\n"
					if kind == "table" || kind == "fence" {
						body = specialHistoryPrefix(kind) + "Tail\n"
					}
					state, driver, filename, original := renderedSourceCollisionFixture(context, body, false, 1200)
					focus := richInputID
					if kind == "table" || kind == "fence" {
						activateHistorySpecial(context, state, driver, kind)
						input, id := specialHistoryInput(state, kind)
						focus = id
						input.MoveTo(input.RuneCount(), false)
					} else {
						clickID(context, driver, renderedRows.At(0))
						state.rich.input.MoveTo(state.rich.input.RuneCount(), false)
						if kind == "structure" || kind == "virtual" {
							if kind == "structure" {
								state.rich.input.MoveTo(3, false)
							}
							if err := driver.Key(gfx.KeyEnter, 0); err != nil {
								context.Fatal(err)
							}
						}
					}
					for _, value := range []string{"A", "B", "C"} {
						if err := driver.Type(value); err != nil {
							context.Fatal(err)
						}
					}
					for index, command := range []rune{'z', 'y', 'z', 'y'} {
						if nonText {
							clickID(context, driver, toolbar)
							if driver.Snapshot().Focused == focus {
								context.Fatal("toolbar fixture retained native input focus")
							}
						}
						if handled, err := driver.ShortcutChord(primaryModifier(), command); err != nil || !handled {
							context.Fatalf("active input history stopped after toolbar focus: kind=%s command=%c handled=%v error=%v", kind, command, handled, err)
						}
						current := state.session.Current()
						if !nonText {
							expected = append(expected, current.Text())
						} else if current.Text() != expected[index] {
							context.Fatalf("toolbar changed native Undo granularity: command=%c got=%q want=%q", command, current.Text(), expected[index])
						}
						if state.mode != renderedMode || driver.Snapshot().Focused != focus {
							context.Fatal("active input history did not restore native focus")
						}
						expectRenderedCollisionSave(context, state, driver, filename, append([]byte{0xef, 0xbb, 0xbf}, []byte(current.Text())...))
					}
					for state.session.Current().Editor.CanUndo() {
						if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
							context.Fatal(err)
						}
					}
					expectRenderedCollisionSave(context, state, driver, filename, original)
				}
			})
		}
	}
}

func TestRenderedDocumentShortcutFlushesOnlyCommittedInput(test *testing.T) {
	for _, composing := range []bool{false, true} {
		test.Run(fmt.Sprintf("composing-%v", composing), func(context *testing.T) {
			state, driver, filename, original := renderedSourceCollisionFixture(context, "target\n\nTail\n", false, 1200)
			clickID(context, driver, renderedRows.At(0))
			state.rich.input.MoveTo(state.rich.input.RuneCount(), false)
			if err := driver.Type("A"); err != nil {
				context.Fatal(err)
			}
			clickID(context, driver, sidebarToggleID)
			before := state.session.Current().Text()
			kind := app.TextInsert
			if composing {
				kind = app.TextSetMarked
			}
			if _, err := state.onRenderedTextEvent(app.TextEvent{Kind: kind, Text: "B"}); err != nil {
				context.Fatal(err)
			}
			result := state.onShortcut(gfx.KeyEvent{Target: sidebarToggleID, Text: "z", Modifiers: primaryModifier()})
			if !result.Handled || result.Focus != richInputID {
				context.Fatal("pre-paint document history was not routed to its native owner")
			}
			state.invalidator.Invalidate()
			if err := driver.Pump(); err != nil {
				context.Fatal(err)
			}
			if composing {
				if _, pending := state.rich.input.Composition(); !pending || state.session.Current().Text() != before {
					context.Fatal("document history committed or traversed pending native composition")
				}
				state.rich.input.CancelComposition()
				state.rich.input.SetSelection(text.Range{Start: 6, End: 6})
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
			}
			expectRenderedCollisionSave(context, state, driver, filename, original)
		})
	}
}

func TestRenderedDocumentShortcutRevealsTallSpecialTarget(test *testing.T) {
	for _, kind := range []string{"table", "fence"} {
		for _, width := range []int{740, 1200} {
			test.Run(fmt.Sprintf("%s/width-%d", kind, width), func(context *testing.T) {
				body := "```go\n" + strings.Repeat("long code line\n", 80) + "last\n```\n\nTail\n"
				if kind == "table" {
					body = "| A | B |\n| --- | --- |\n" + strings.Repeat("| cell | value |\n", 60) + "\nTail\n"
				}
				state, driver, filename, original := renderedSourceCollisionFixture(context, strings.Repeat("spacer\n\n", 40)+body, false, width)
				top := state.preview.blocks[40].top
				if kind == "table" {
					for _, height := range state.preview.blocks[40].table.rows[:60] {
						top += height
					}
				}
				if err := driver.Scroll(gfx.ScrollEvent{X: state.previewRect.X + 20, Y: state.previewRect.Y + 20, DeltaY: top - 40}); err != nil {
					context.Fatal(err)
				}
				if kind == "table" {
					clickID(context, driver, renderedTableCells.At(120))
				} else {
					activateHistorySpecial(context, state, driver, kind)
				}
				input, focus := specialHistoryInput(state, kind)
				input.MoveTo(input.RuneCount(), false)
				if err := driver.Type("Q"); err != nil {
					context.Fatal(err)
				}
				changed := append([]byte{0xef, 0xbb, 0xbf}, []byte(state.session.Current().Text())...)
				navigateDocumentWithoutInput(context, state, driver, false)
				for _, command := range []rune{'z', 'y', 'z'} {
					if handled, err := driver.ShortcutChord(primaryModifier(), command); err != nil || !handled || driver.Snapshot().Focused != focus {
						context.Fatal("tall special history lost its actual native input")
					}
					if kind == "table" {
						if state.table.row != 60 || state.table.column != 0 {
							context.Fatal("tall table history selected another cell")
						}
						for _, hit := range driver.Snapshot().Hits {
							if hit.ID == tableInputID && (hit.Bounds.Y < state.previewRect.Y || hit.Bounds.Y+hit.Bounds.Height > state.previewRect.Y+state.previewRect.Height) {
								context.Fatalf("restored table cell is not visible: %v", hit.Bounds)
							}
						}
					} else if caret, ok := state.codeBlock.view.CaretBounds(); !ok || caret.Y < state.previewRect.Y || caret.Y+caret.Height > state.previewRect.Y+state.previewRect.Height {
						context.Fatalf("restored fence caret is not visible: %v", caret)
					}
					wanted := original
					if command == 'y' {
						wanted = changed
					}
					expectRenderedCollisionSave(context, state, driver, filename, wanted)
				}
			})
		}
	}
}
