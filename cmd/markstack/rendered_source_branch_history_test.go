package main

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestRenderedReadOutlineSourceBranchKeepsActualRedo(test *testing.T) {
	for _, virtual := range []bool{false, true} {
		for _, mode := range []gfx.ID{editModeID, splitModeID} {
			for _, dark := range []bool{false, true} {
				for _, width := range []int{740, 1200} {
					test.Run(fmt.Sprintf("virtual-%v/mode-%v/dark-%v/width-%d", virtual, mode, dark, width), func(context *testing.T) {
						const source = "<!-- untouched -->\n\nabcd\n\n## Target\n\nTail **keep** &amp; <!-- opaque -->\n"
						state, driver, filename, original := renderedSourceCollisionFixture(context, source, dark, width)
						clickID(context, driver, renderedRows.At(1))
						caret := 2
						if virtual {
							caret = 4
						}
						state.rich.input.MoveTo(caret, false)
						if err := driver.Key(gfx.KeyEnter, 0); err != nil {
							context.Fatal(err)
						}
						if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
							context.Fatal(err)
						}
						readOutlineHistoryRoundTrip(context, state, driver, readMode, width, 2)
						clickID(context, driver, mode)
						clickID(context, driver, sourceID)
						current := state.session.Current()
						before := current.Text()
						end := strings.Index(before, "Target") + len("Target")
						current.Editor.MoveTo(utf8.RuneCountInString(before[:end]), false)
						if err := driver.Type("P"); err != nil {
							context.Fatal(err)
						}
						wanted := strings.Replace(before, "Target", "TargetP", 1)
						if current.Text() != wanted {
							context.Fatal("Source branch fixture did not commit to the heading")
						}
						expectRenderedCollisionSave(context, state, driver, filename, append([]byte{0xef, 0xbb, 0xbf}, []byte(wanted)...))
						if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
							context.Fatal(err)
						}
						if current.Text() != before || !current.Editor.CanRedo() {
							context.Fatal("Source branch fixture did not return to identical bytes with redo")
						}
						expectRenderedCollisionSave(context, state, driver, filename, original)
						clickID(context, driver, renderedModeID)
						clickID(context, driver, renderedRows.At(1))
						clickID(context, driver, renderedRows.At(2))
						for range 2 {
							if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
								context.Fatal(err)
							}
							if current.Text() != wanted || state.mode != renderedMode || !state.rich.active || state.rich.index != 2 || state.rich.input.String() != "TargetP" || state.rich.input.Caret() != 7 || driver.Snapshot().Focused != richInputID {
								context.Fatal("stale structure intercepted actual Source redo or abandoned the supported heading")
							}
							expectRenderedCollisionSave(context, state, driver, filename, append([]byte{0xef, 0xbb, 0xbf}, []byte(wanted)...))
							if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
								context.Fatal(err)
							}
							expectRenderedCollisionSave(context, state, driver, filename, original)
						}
						if current.Editor.CanUndo() || len(state.rich.structureRedo) != 0 {
							context.Fatal("Source branch retained the discarded structural transaction")
						}
					})
				}
			}
		}
	}
}

func TestRenderedReadOutlineSourceNoopPreservesRedo(test *testing.T) {
	for _, operation := range []string{"cancel", "empty-cancel", "noop-delete", "image-rejected"} {
		for _, mode := range []gfx.ID{editModeID, splitModeID} {
			test.Run(fmt.Sprintf("%s/mode-%v", operation, mode), func(context *testing.T) {
				state, driver, filename, original := renderedSourceCollisionFixture(context, "abcd\n\n## Target\n", false, 1200)
				clickID(context, driver, renderedRows.At(0))
				state.rich.input.MoveTo(2, false)
				if err := driver.Key(gfx.KeyEnter, 0); err != nil {
					context.Fatal(err)
				}
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
				readOutlineHistoryRoundTrip(context, state, driver, readMode, 1200, 1)
				clickID(context, driver, mode)
				clickID(context, driver, sourceID)
				current := state.session.Current()
				before := current.Editor.CommittedString()
				if operation == "noop-delete" {
					current.Editor.MoveTo(0, false)
					if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
						context.Fatal(err)
					}
				} else if operation == "image-rejected" {
					if err := driver.SetClipboardImage(imagePNG(context, 2, 2)[:33]); err != nil {
						context.Fatal(err)
					}
					if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
						context.Fatal(err)
					}
					if state.message == "" {
						context.Fatal("invalid image fixture was not rejected")
					}
				} else {
					if err := driver.Compose("ni"); err != nil {
						context.Fatal(err)
					}
					if operation == "empty-cancel" {
						if _, err := state.onMarkdownSourceTextEvent(app.TextEvent{Kind: app.TextSetMarked, Text: ""}); err != nil {
							context.Fatal(err)
						}
					}
					if current.Editor.CommittedString() != before || len(state.rich.structureRedo) != 1 {
						context.Fatal("Source preedit invalidated committed history")
					}
					if _, err := state.onMarkdownSourceTextEvent(app.TextEvent{Kind: app.TextCancelComposition}); err != nil {
						context.Fatal(err)
					}
					if err := driver.Pump(); err != nil {
						context.Fatal(err)
					}
				}
				if current.Editor.CommittedString() != before || len(state.rich.structureRedo) != 1 || !current.Editor.CanRedo() {
					context.Fatal("Source cancellation/no-op discarded structural redo")
				}
				expectRenderedCollisionSave(context, state, driver, filename, original)
				clickID(context, driver, renderedModeID)
				clickID(context, driver, renderedRows.At(0))
				clickID(context, driver, renderedRows.At(1))
				if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
					context.Fatal(err)
				}
				if state.mode != renderedMode || !state.rich.active || state.rich.index != 1 || state.rich.input.String() != "cd" {
					context.Fatal("Source cancellation/no-op lost actual structural redo")
				}
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
				expectRenderedCollisionSave(context, state, driver, filename, original)
			})
		}
	}
}

func TestRenderedReadOutlineSourceCommitInvalidatesCarrier(test *testing.T) {
	for _, operation := range []string{"commit", "paste", "image"} {
		test.Run(operation, func(context *testing.T) {
			state, driver, filename, original := renderedSourceCollisionFixture(context, "abcd\n\n## Target\n", false, 1200)
			clickID(context, driver, renderedRows.At(0))
			state.rich.input.MoveTo(2, false)
			if err := driver.Key(gfx.KeyEnter, 0); err != nil {
				context.Fatal(err)
			}
			if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
				context.Fatal(err)
			}
			readOutlineHistoryRoundTrip(context, state, driver, readMode, 1200, 1)
			clickID(context, driver, editModeID)
			clickID(context, driver, sourceID)
			current := state.session.Current()
			before := current.Editor.CommittedString()
			switch operation {
			case "commit":
				if err := driver.Compose("ni"); err != nil {
					context.Fatal(err)
				}
				if _, err := state.onMarkdownSourceTextEvent(app.TextEvent{Kind: app.TextInsert, Text: "X"}); err != nil {
					context.Fatal(err)
				}
				if err := driver.Pump(); err != nil {
					context.Fatal(err)
				}
			case "paste":
				driver.SetClipboard("X")
				if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
					context.Fatal(err)
				}
			case "image":
				if err := driver.SetClipboardImage(imagePNG(context, 1, 1)); err != nil {
					context.Fatal(err)
				}
				if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
					context.Fatal(err)
				}
			}
			if current.Editor.CommittedString() == before {
				context.Fatal("Source commit fixture made no committed change")
			}
			if state.rich.input != nil || state.rich.active || state.selection.active || len(state.rich.structureRedo) != 0 || len(state.rich.crossRedo) != 0 || state.rich.sourceHistoryOffset != 0 {
				context.Fatal("committed Source mutation retained the inactive rendered carrier")
			}
			expectRenderedCollisionSave(context, state, driver, filename, append([]byte{0xef, 0xbb, 0xbf}, []byte(current.Text())...))
			if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
				context.Fatal(err)
			}
			expectRenderedCollisionSave(context, state, driver, filename, original)
		})
	}
}

func TestRenderedReadOutlineSourceHistoryTraversalPreservesCarrier(test *testing.T) {
	for _, virtual := range []bool{false, true} {
		for _, mode := range []gfx.ID{editModeID, splitModeID} {
			for _, outline := range []bool{false, true} {
				test.Run(fmt.Sprintf("virtual-%v/mode-%v/outline-%v", virtual, mode, outline), func(context *testing.T) {
					state, driver, filename, original := renderedSourceCollisionFixture(context, "<!-- untouched -->\n\nabcd\n\n## Target\n", false, 1200)
					clickID(context, driver, renderedRows.At(1))
					caret, target := 2, 3
					if virtual {
						caret, target = 4, 2
					}
					state.rich.input.MoveTo(caret, false)
					if err := driver.Key(gfx.KeyEnter, 0); err != nil {
						context.Fatal(err)
					}
					current := state.session.Current()
					after := current.Text()
					for range 2 {
						if outline {
							readOutlineHistoryRoundTrip(context, state, driver, readMode, 1200, target)
						}
						clickID(context, driver, mode)
						clickID(context, driver, sourceID)
						for _, command := range []rune{'z', 'y'} {
							if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
								context.Fatal(err)
							}
						}
						if current.Text() != after {
							context.Fatal("Source history traversal fixture did not return to identical bytes")
						}
						expectRenderedCollisionSave(context, state, driver, filename, append([]byte{0xef, 0xbb, 0xbf}, []byte(after)...))
						clickID(context, driver, renderedModeID)
						clickID(context, driver, renderedRows.At(1))
						clickID(context, driver, renderedRows.At(target))
						if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
							context.Fatal(err)
						}
						if state.mode != renderedMode || !state.rich.active || state.rich.virtual || state.rich.index != 1 || state.rich.input.String() != "abcd" || state.rich.input.Caret() != caret || driver.Snapshot().Focused != richInputID {
							context.Fatal("Source history traversal discarded the valid structural carrier")
						}
						expectRenderedCollisionSave(context, state, driver, filename, original)
						if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
							context.Fatal(err)
						}
						if current.Text() != after || state.mode != renderedMode || !state.rich.active || state.rich.virtual != virtual || state.rich.index != 2 || state.rich.input.Caret() != 0 || driver.Snapshot().Focused != richInputID {
							context.Fatal("Source history traversal lost the original Redo target")
						}
					}
				})
			}
		}
	}
}
