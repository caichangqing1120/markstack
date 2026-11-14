package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func readOutlineHistoryRoundTrip(context *testing.T, state *model, driver *app.Driver, mode viewMode, width, target int) {
	context.Helper()
	current := state.session.Current()
	source, documentID, tabs, dirty := current.Text(), current.ID, len(state.session.Tabs), current.Dirty()
	selection := current.Editor.Selection()
	canUndo, canRedo := current.Editor.CanUndo(), current.Editor.CanRedo()
	modeID := readModeID
	if mode == splitMode {
		modeID = splitModeID
	}
	clickID(context, driver, modeID)
	state.outline = true
	if err := driver.Resize(max(1100, width), 780); err != nil {
		context.Fatal(err)
	}
	for range 2 {
		clickID(context, driver, outlineRows.At(0))
		if state.mode != mode || state.rich.active || state.rich.input != nil || state.selection.active || driver.Snapshot().Focused == richInputID {
			context.Fatal("non-rendered outline retained or activated a hidden rendered input")
		}
		if current.Text() != source || current.ID != documentID || len(state.session.Tabs) != tabs || current.Dirty() != dirty || current.Editor.Selection() != selection || current.Editor.CanUndo() != canUndo || current.Editor.CanRedo() != canRedo {
			context.Fatal("non-rendered outline changed Source identity, selection or history")
		}
	}
	clickID(context, driver, renderedModeID)
	if err := driver.Resize(width, 780); err != nil {
		context.Fatal(err)
	}
	clickID(context, driver, renderedRows.At(target))
	if state.mode != renderedMode || !state.rich.active || state.rich.input.String() != "Target" || current.Text() != source || driver.Snapshot().Focused != richInputID {
		context.Fatal("return from non-rendered outline did not activate the real heading")
	}
}

func TestRenderedReadOutlineRetainsOwnedHistory(test *testing.T) {
	for _, kind := range []string{"split", "virtual", "format", "cross", "selection", "redo", "virtual-text"} {
		for _, mode := range []viewMode{readMode, splitMode} {
			for _, dark := range []bool{false, true} {
				for _, width := range []int{740, 1200} {
					test.Run(fmt.Sprintf("%s/mode-%d/dark-%v/width-%d", kind, mode, dark, width), func(context *testing.T) {
						const prefix = "<!-- untouched -->\n\n"
						const suffix = "\n\n## Target\n\nTail **keep** &amp; <!-- opaque -->\n"
						body, beforeBody, afterBody := "abcd", "abcd", "ab\n\ncd"
						beforeRow, afterRow, target := 1, 2, 3
						beforeSelection, afterSelection := text.Range{Start: 2, End: 2}, text.Range{}
						beforeVirtual, afterVirtual := false, false
						switch kind {
						case "virtual":
							afterBody, target, afterVirtual = "abcd\n\n", 2, true
							beforeSelection = text.Range{Start: 4, End: 4}
						case "virtual-text":
							beforeBody, afterBody = "abcd\n\n", "abcd\n\nQ"
							beforeRow, beforeVirtual, afterVirtual = 2, true, true
							beforeSelection, afterSelection = text.Range{}, text.Range{Start: 1, End: 1}
						case "format":
							afterBody, afterRow, target = "a**bc**d", 1, 2
							beforeSelection, afterSelection = text.Range{Start: 3, End: 1}, text.Range{Start: 3, End: 1}
						case "cross":
							body, beforeBody, afterBody = "ab **cd** efgh", "ab **cd** efgh", "aXgh"
							afterRow, target = 1, 2
							beforeSelection, afterSelection = text.Range{Start: 8, End: 1}, text.Range{Start: 2, End: 2}
						case "redo":
							target = 2
						}
						state, driver, filename, original := renderedSourceCollisionFixture(context, prefix+body+suffix, dark, width)
						clickID(context, driver, renderedRows.At(1))
						if kind == "format" {
							state.rich.input.SetSelection(beforeSelection)
							state.applyBoldBlock()
							if err := driver.Pump(); err != nil {
								context.Fatal(err)
							}
						} else if kind == "cross" {
							state.rich.input.SetSelection(beforeSelection)
							if err := driver.Type("X"); err != nil {
								context.Fatal(err)
							}
						} else {
							caret := 2
							if kind == "virtual" || kind == "virtual-text" {
								caret = 4
							}
							state.rich.input.MoveTo(caret, false)
							if err := driver.Key(gfx.KeyEnter, 0); err != nil {
								context.Fatal(err)
							}
							if kind == "virtual-text" {
								if err := driver.Type("Q"); err != nil {
									context.Fatal(err)
								}
							} else if kind == "redo" {
								if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
									context.Fatal(err)
								}
							}
						}
						for cycle := 0; cycle < 2; cycle++ {
							if kind == "selection" {
								if err := driver.Key(gfx.KeyUp, gfx.ModifierShift); err != nil {
									context.Fatal(err)
								}
								if !state.selection.active {
									context.Fatal("selection carrier fixture did not activate")
								}
							}
							readOutlineHistoryRoundTrip(context, state, driver, mode, width, target)
							commands := []rune{'z', 'y'}
							if kind == "redo" {
								commands = []rune{'y', 'z'}
							}
							for _, command := range commands {
								if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
									context.Fatal(err)
								}
								wantedBody, row, selection, virtual := beforeBody, beforeRow, beforeSelection, beforeVirtual
								if command == 'y' {
									wantedBody, row, selection, virtual = afterBody, afterRow, afterSelection, afterVirtual
								}
								ordered := text.Range{Start: min(selection.Start, selection.End), End: max(selection.Start, selection.End)}
								if state.mode != renderedMode || !state.rich.active || state.rich.virtual != virtual || state.rich.index != row || state.rich.input.Selection() != ordered || state.rich.input.Caret() != selection.End || driver.Snapshot().Focused != richInputID {
									context.Fatalf("non-rendered outline lost owned history: command=%c mode=%v active=%v row=%d", command, state.mode, state.rich.active, state.rich.index)
								}
								wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(prefix+wantedBody+suffix, "\n", "\r\n"))...)
								expectRenderedCollisionSave(context, state, driver, filename, wanted)
							}
						}
						if kind != "redo" {
							if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
								context.Fatal(err)
							}
						}
						if kind == "virtual-text" {
							if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
								context.Fatal(err)
							}
						}
						if state.session.Current().Editor.CanUndo() {
							context.Fatal("reading outline invented an extra Source history unit")
						}
						expectRenderedCollisionSave(context, state, driver, filename, original)
					})
				}
			}
		}
	}
}

func TestRenderedReadOutlineRejectsStaleCarrier(test *testing.T) {
	for _, selection := range []bool{false, true} {
		for _, stale := range []string{"document", "source", "actual-source"} {
			test.Run(fmt.Sprintf("selection-%v/%s", selection, stale), func(context *testing.T) {
				state, driver, _, _ := renderedSourceCollisionFixture(context, "abcd\n\n## Target\n", false, 1200)
				clickID(context, driver, renderedRows.At(0))
				state.rich.input.MoveTo(2, false)
				if err := driver.Key(gfx.KeyEnter, 0); err != nil {
					context.Fatal(err)
				}
				if selection {
					if err := driver.Key(gfx.KeyUp, gfx.ModifierShift); err != nil {
						context.Fatal(err)
					}
					if !state.selection.active {
						context.Fatal("stale selection fixture did not activate")
					}
				}
				current := state.session.Current()
				if stale == "actual-source" {
					start := strings.Index(current.Text(), "Target") + len("Target")
					if err := current.EditRange(start, start, "X"); err != nil {
						context.Fatal(err)
					}
				} else if selection {
					if stale == "document" {
						state.selection.documentID++
					} else {
						state.selection.source += "stale"
					}
				} else if stale == "document" {
					state.rich.documentID++
				} else {
					state.rich.source += "stale"
				}
				clickID(context, driver, readModeID)
				state.outline = true
				if err := driver.Resize(1200, 780); err != nil {
					context.Fatal(err)
				}
				before, sourceSelection := current.Text(), current.Editor.Selection()
				canUndo, canRedo := current.Editor.CanUndo(), current.Editor.CanRedo()
				clickID(context, driver, outlineRows.At(0))
				if state.mode != readMode || state.rich.active || state.rich.input != nil || len(state.rich.structureUndo) != 0 || len(state.rich.structureRedo) != 0 || len(state.rich.crossUndo) != 0 || len(state.rich.crossRedo) != 0 || state.rich.sourceHistoryOffset != 0 || current.Text() != before || current.Editor.Selection() != sourceSelection || current.Editor.CanUndo() != canUndo || current.Editor.CanRedo() != canRedo {
					context.Fatal("reading outline reused a stale carrier or changed Source history")
				}
			})
		}
	}
}

func TestRenderedReadOutlineRedoLifecycle(test *testing.T) {
	for _, operation := range []string{"cancel", "noop", "branch"} {
		for _, mode := range []viewMode{readMode, splitMode} {
			for _, dark := range []bool{false, true} {
				for _, width := range []int{740, 1200} {
					test.Run(fmt.Sprintf("%s/mode-%d/dark-%v/width-%d", operation, mode, dark, width), func(context *testing.T) {
						const source = "<!-- untouched -->\n\nabcd\n\n## Target\n\nTail **keep** &amp; <!-- opaque -->\n"
						state, driver, filename, original := renderedSourceCollisionFixture(context, source, dark, width)
						clickID(context, driver, renderedRows.At(1))
						state.rich.input.MoveTo(2, false)
						if err := driver.Key(gfx.KeyEnter, 0); err != nil {
							context.Fatal(err)
						}
						if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
							context.Fatal(err)
						}
						readOutlineHistoryRoundTrip(context, state, driver, mode, width, 2)
						switch operation {
						case "cancel":
							if err := driver.Compose("ni"); err != nil {
								context.Fatal(err)
							}
							if _, err := state.onRenderedTextEvent(app.TextEvent{Kind: app.TextCancelComposition}); err != nil {
								context.Fatal(err)
							}
							if err := driver.Pump(); err != nil {
								context.Fatal(err)
							}
						case "noop":
							state.rich.input.SetSelection(text.Range{Start: 0, End: 6})
							if err := driver.Type("Target"); err != nil {
								context.Fatal(err)
							}
						case "branch":
							state.rich.input.MoveTo(6, false)
							if err := driver.Type("Z"); err != nil {
								context.Fatal(err)
							}
						}
						current := state.session.Current()
						if operation == "branch" {
							if len(state.rich.structureRedo) != 0 || current.Editor.CanRedo() {
								context.Fatal("real Source edit retained the inactive structural redo")
							}
							changed := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(strings.Replace(source, "Target", "TargetZ", 1), "\n", "\r\n"))...)
							if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
								context.Fatal(err)
							}
							expectRenderedCollisionSave(context, state, driver, filename, changed)
						} else {
							if len(state.rich.structureRedo) != 1 || !current.Editor.CanRedo() || state.rich.appliedEdits+state.rich.sourceHistoryOffset != 0 {
								context.Fatal("cancelled/no-op edit discarded inactive redo or advanced Source position")
							}
							expectRenderedCollisionSave(context, state, driver, filename, original)
							if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
								context.Fatal(err)
							}
							if state.mode != renderedMode || !state.rich.active || state.rich.index != 2 || state.rich.input.Selection() != (text.Range{}) {
								context.Fatal("cancelled/no-op edit lost the restored structural redo target")
							}
						}
						if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
							context.Fatal(err)
						}
						expectRenderedCollisionSave(context, state, driver, filename, original)
					})
				}
			}
		}
	}
}
