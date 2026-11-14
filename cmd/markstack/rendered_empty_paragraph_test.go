package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

var renderedEmptyParagraphCases = []struct {
	name, before, after string
	row                 int
}{
	{"first", "", "\n\nLast\n", 0},
	{"middle", "First\n\n", "\n\nLast\n", 1},
	{"last", "First\n\n", "\n", 1},
	{"raw-before", "<div>opaque</div>\n\n", "\n", 1},
	{"raw-after", "", "\n\n<div>opaque</div>\n", 0},
}

func TestRenderedEmptyParagraphKeepsDocumentPosition(test *testing.T) {
	for _, scenario := range renderedEmptyParagraphCases {
		for _, cut := range []bool{false, true} {
			for _, dark := range []bool{false, true} {
				for _, width := range []int{740, 1200} {
					test.Run(fmt.Sprintf("%s/cut-%v/dark-%v/width-%d", scenario.name, cut, dark, width), func(context *testing.T) {
						state, driver, filename, original := renderedSourceCollisionFixture(context, scenario.before+"Solo"+scenario.after, dark, width)
						clickID(context, driver, renderedRows.At(scenario.row))
						if _, err := driver.ShortcutChord(primaryModifier(), 'a'); err != nil {
							context.Fatal(err)
						}
						if cut {
							if _, err := driver.ShortcutChord(primaryModifier(), 'x'); err != nil {
								context.Fatal(err)
							}
							if driver.Clipboard() != "Solo" {
								context.Fatal("cut copied text outside the ordinary paragraph")
							}
						} else if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
							context.Fatal(err)
						}
						if state.mode != renderedMode || !state.rich.active || state.rich.input.String() != "" {
							context.Fatalf("empty ordinary paragraph lost rendered input: mode=%v active=%v", state.mode, state.rich.active)
						}
						empty := hitBounds(context, driver, richInputID)
						if scenario.row > 0 {
							previous := hitBounds(context, driver, renderedRows.At(scenario.row-1))
							if empty.Y < previous.Y+previous.Height {
								context.Fatal("empty input overlaps the preceding paragraph")
							}
						}
						if strings.Trim(scenario.after, "\n") != "" {
							next := hitBounds(context, driver, renderedRows.At(scenario.row))
							if empty.Y+empty.Height > next.Y {
								context.Fatal("empty input overlaps or follows the next paragraph")
							}
						}
						bytesFor := func(value string) []byte {
							return append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(scenario.before+value+scenario.after, "\n", "\r\n"))...)
						}
						expectRenderedCollisionSave(context, state, driver, filename, bytesFor(""))
						if err := driver.Type("New"); err != nil {
							context.Fatal(err)
						}
						expectRenderedCollisionSave(context, state, driver, filename, bytesFor("New"))
						for _, step := range []struct {
							command rune
							bytes   []byte
						}{{'z', bytesFor("")}, {'z', original}, {'y', bytesFor("")}, {'y', bytesFor("New")}} {
							if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
								context.Fatal(err)
							}
							if state.mode != renderedMode || !state.rich.active {
								context.Fatal("paragraph empty history unexpectedly left rendered mode")
							}
							expectRenderedCollisionSave(context, state, driver, filename, step.bytes)
						}
					})
				}
			}
		}
	}
}

func TestRenderedEmptyParagraphResumesAfterNavigation(test *testing.T) {
	for _, scenario := range renderedEmptyParagraphCases {
		for _, transition := range []struct {
			name string
			mode gfx.ID
		}{{"tab", gfx.ID{}}, {"source", editModeID}, {"split", splitModeID}, {"read", readModeID}} {
			for _, dark := range []bool{false, true} {
				test.Run(fmt.Sprintf("%s/%s/dark-%v", scenario.name, transition.name, dark), func(context *testing.T) {
					state, driver, filename, original := renderedSourceCollisionFixture(context, scenario.before+"Solo"+scenario.after, dark, 740)
					current, tab := state.session.Current(), state.session.Active
					clickID(context, driver, renderedRows.At(scenario.row))
					for _, command := range []rune{'a', 'x'} {
						if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
							context.Fatal(err)
						}
					}
					bytesFor := func(value string) []byte {
						return append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(scenario.before+value+scenario.after, "\n", "\r\n"))...)
					}
					expectRenderedCollisionSave(context, state, driver, filename, bytesFor(""))
					if err := driver.Compose("ni"); err != nil {
						context.Fatal(err)
					}
					expectRenderedCollisionSave(context, state, driver, filename, bytesFor(""))
					if _, err := state.onRenderedTextEvent(app.TextEvent{Kind: app.TextCancelComposition}); err != nil {
						context.Fatal(err)
					}
					if err := driver.Pump(); err != nil {
						context.Fatal(err)
					}
					other := ""
					if transition.mode == (gfx.ID{}) {
						other = filepath.Join(context.TempDir(), "Other.txt")
						if err := os.WriteFile(other, []byte("Other"), 0640); err != nil {
							context.Fatal(err)
						}
						if !state.openFile(other) {
							context.Fatal("other document did not open")
						}
						state.invalidator.Invalidate()
						if err := driver.Pump(); err != nil {
							context.Fatal(err)
						}
						clickID(context, driver, tabRows.At(tab))
					} else {
						clickID(context, driver, transition.mode)
						if state.rich.active || state.rich.input != nil {
							context.Fatal("inactive view retained a native editor")
						}
					}
					if state.mode != renderedMode {
						clickID(context, driver, renderedModeID)
					}
					if state.session.Current() != current || state.mode != renderedMode || !state.rich.active || state.rich.input == nil || state.rich.input.String() != "" {
						context.Fatal("returning to an empty paragraph lost its input before any Undo/Redo")
					}
					clickID(context, driver, richInputID)
					if err := driver.Type("New"); err != nil {
						context.Fatal(err)
					}
					expectRenderedCollisionSave(context, state, driver, filename, bytesFor("New"))
					for _, step := range []struct {
						command rune
						bytes   []byte
					}{{'z', bytesFor("")}, {'z', original}, {'y', bytesFor("")}, {'y', bytesFor("New")}} {
						if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
							context.Fatal(err)
						}
						if state.mode != renderedMode || !state.rich.active {
							context.Fatal("navigation lost the empty paragraph history")
						}
						expectRenderedCollisionSave(context, state, driver, filename, step.bytes)
					}
					if other != "" {
						stored, err := os.ReadFile(other)
						if err != nil || string(stored) != "Other" {
							context.Fatal("empty paragraph navigation edited the other document")
						}
					}
				})
			}
		}
	}
}

func TestRenderedEmptyParagraphRejectsUnsafeCachedTargets(test *testing.T) {
	for _, mutation := range []string{"valid", "missing", "document", "source", "changed", "negative-index", "wrong-index", "past-index", "negative-start", "past-end", "reversed", "negative-depth", "inside-text", "text-end", "inside-crlf", "inside-raw", "raw-span"} {
		test.Run(mutation, func(context *testing.T) {
			state, driver, _, _ := renderedSourceCollisionFixture(context, "First\n\nSolo\n\n<script>\n\nopaque\n</script>\n", false, 740)
			clickID(context, driver, renderedRows.At(1))
			for _, command := range []rune{'a', 'x'} {
				if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
					context.Fatal(err)
				}
			}
			current := state.session.Current()
			state.deactivateRenderedView(current)
			if state.rich.input != nil || state.rich.active || state.rich.emptyTarget == nil {
				context.Fatal("deactivation did not retain only the empty slot metadata")
			}
			target := state.rich.emptyTarget
			switch mutation {
			case "missing":
				state.rich.emptyTarget = nil
			case "document":
				state.rich.documentID++
			case "source":
				state.rich.source += "\r\n"
			case "changed":
				current.Replace("Different\r\n")
			case "negative-index":
				target.index = -1
			case "wrong-index":
				target.index = 0
			case "past-index":
				target.index = 100
			case "negative-start":
				target.start = -1
			case "past-end":
				target.end = len(current.Text()) + 1
			case "reversed":
				target.end = target.start - 1
			case "negative-depth":
				target.depth = -1
			case "inside-text":
				target.start, target.end, target.index = 1, 1, 0
			case "text-end":
				target.start, target.end = 5, 5
			case "inside-crlf":
				target.start, target.end = 6, 6
			case "inside-raw":
				position := strings.Index(current.Text(), "<script>") + len("<script>\r\n")
				target.start, target.end, target.index = position, position, 2
			case "raw-span":
				target.end = len(current.Text())
			}
			source, selection := current.Text(), current.Editor.Selection()
			undo, redo, dirty := current.Editor.CanUndo(), current.Editor.CanRedo(), current.Dirty()
			state.restoreRenderedEmptyTarget(current)
			if mutation == "valid" {
				if !state.rich.active || state.rich.input == nil || state.rich.input.String() != "" {
					context.Fatal("valid empty paragraph target did not restore")
				}
			} else if state.rich.active || state.rich.input != nil {
				context.Fatal("unsafe cached slot created a native editor")
			}
			if current.Text() != source || current.Editor.Selection() != selection || current.Editor.CanUndo() != undo || current.Editor.CanRedo() != redo || current.Dirty() != dirty {
				context.Fatal("empty slot validation mutated Source, selection or history")
			}
		})
	}
}

func TestRenderedEmptyParagraphEnterSaveAsAndSourceBranch(test *testing.T) {
	for _, scenario := range renderedEmptyParagraphCases {
		test.Run(scenario.name, func(context *testing.T) {
			state, driver, filename, _ := renderedSourceCollisionFixture(context, scenario.before+"Solo"+scenario.after, false, 740)
			clickID(context, driver, renderedRows.At(scenario.row))
			for _, command := range []rune{'a', 'x'} {
				if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
					context.Fatal(err)
				}
			}
			bytesFor := func(value string) []byte {
				return append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(scenario.before+value+scenario.after, "\n", "\r\n"))...)
			}
			expectRenderedCollisionSave(context, state, driver, filename, bytesFor(""))
			if err := driver.Key(gfx.KeyEnter, 0); err != nil {
				context.Fatal(err)
			}
			if err := driver.Type("Q"); err != nil {
				context.Fatal(err)
			}
			saved := filepath.Join(context.TempDir(), "Saved.md")
			if err := state.session.Save(state.session.Active, saved); err != nil {
				context.Fatal(err)
			}
			savedInfo, err := os.Stat(saved)
			if err != nil {
				context.Fatal(err)
			}
			expectSaved := func(expected []byte) {
				context.Helper()
				if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
					context.Fatal(err)
				}
				stored, err := os.ReadFile(saved)
				if err != nil || !bytes.Equal(stored, expected) || state.session.Current().Dirty() {
					context.Fatalf("Save As did not retain expected paragraph bytes: %q, %v", stored, err)
				}
				if info, err := os.Stat(saved); err != nil || info.Mode().Perm() != savedInfo.Mode().Perm() {
					context.Fatal("saving the new path changed its permissions")
				}
			}
			expectSaved(bytesFor("\nQ"))
			for range 2 {
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
			}
			if state.mode != renderedMode || !state.rich.active || state.rich.input.String() != "" {
				context.Fatal("Enter/typing Undo lost the original empty slot")
			}
			expectSaved(bytesFor(""))
			before := state.session.Current().Text()
			if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
				context.Fatal(err)
			}
			if state.session.Current().Text() != before {
				context.Fatal("empty slot Backspace reverted an earlier source edit")
			}
			clickID(context, driver, editModeID)
			if err := driver.Type("B"); err != nil {
				context.Fatal(err)
			}
			branch := state.session.Current().Text()
			if !strings.Contains(branch, "B") || state.session.Current().Editor.CanRedo() {
				context.Fatal("new Source branch kept obsolete empty paragraph Redo")
			}
			clickID(context, driver, renderedModeID)
			if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
				context.Fatal(err)
			}
			if state.session.Current().Text() != branch {
				context.Fatal("returning to rendered mode revived obsolete source history")
			}
			stored, err := os.ReadFile(filename)
			if err != nil || string(stored) != string(bytesFor("")) {
				context.Fatal("Save As or Source branching modified the original file")
			}
			if info, err := os.Stat(filename); err != nil || info.Mode().Perm() != 0640 {
				context.Fatal("Save As changed the original file permissions")
			}
		})
	}
}

func TestRenderedEmptyParagraphRejectsChangedSourceOwner(test *testing.T) {
	for _, route := range []string{"active", "source", "inactive-tab"} {
		test.Run(route, func(context *testing.T) {
			state, driver, _, _ := renderedSourceCollisionFixture(context, "First\n\nSolo\n\nLast\n", false, 740)
			current, tab := state.session.Current(), state.session.Active
			clickID(context, driver, renderedRows.At(1))
			for _, command := range []rune{'a', 'x'} {
				if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
					context.Fatal(err)
				}
			}
			if route == "source" {
				clickID(context, driver, editModeID)
			} else if route == "inactive-tab" {
				other := filepath.Join(context.TempDir(), "Other.txt")
				if err := os.WriteFile(other, []byte("Other"), 0640); err != nil {
					context.Fatal(err)
				}
				if !state.openFile(other) {
					context.Fatal("other document did not open")
				}
				state.invalidator.Invalidate()
				if err := driver.Pump(); err != nil {
					context.Fatal(err)
				}
			}
			source := current.Text()
			current.Editor = text.NewEditor(source)
			state.invalidator.Invalidate()
			if err := driver.Pump(); err != nil {
				context.Fatal(err)
			}
			if route == "inactive-tab" {
				clickID(context, driver, tabRows.At(tab))
			}
			if state.mode != renderedMode {
				clickID(context, driver, renderedModeID)
			}
			if state.rich.active || state.rich.input != nil {
				context.Fatal("replacing the Source editor revived another owner's empty slot")
			}
			for _, command := range []rune{'z', 'y'} {
				if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
					context.Fatal(err)
				}
			}
			if current.Text() != source || current.Editor.CanUndo() || current.Editor.CanRedo() {
				context.Fatal("stale empty paragraph metadata mutated the replacement owner")
			}
		})
	}
}

func TestRenderedEmptyParagraphExplicitDeletionRestoresSelection(test *testing.T) {
	for _, replacement := range []bool{false, true} {
		for _, navigate := range []bool{false, true} {
			test.Run(fmt.Sprintf("replacement-%v/navigate-%v", replacement, navigate), func(context *testing.T) {
				state, driver, filename, original := renderedSourceCollisionFixture(context, "First\n\nSolo\n\nLast\n", false, 740)
				clickID(context, driver, renderedRows.At(1))
				event := app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextDeleteForward}
				state.rich.input.SetSelection(text.Range{Start: 0, End: 4})
				if replacement {
					state.rich.input.MoveTo(1, false)
					event = app.TextEvent{Kind: app.TextInsert, HasReplacement: true, Replacement: app.TextRange{Start: 0, End: 4}}
				}
				if _, err := state.onRenderedTextEvent(event); err != nil {
					context.Fatal(err)
				}
				if err := driver.Pump(); err != nil {
					context.Fatal(err)
				}
				if navigate {
					clickID(context, driver, readModeID)
					clickID(context, driver, renderedModeID)
				}
				clickID(context, driver, richInputID)
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
				if state.mode != renderedMode || !state.rich.active || state.rich.input.String() != "Solo" || state.rich.input.Selection() != (text.Range{Start: 0, End: 4}) || state.rich.input.Caret() != 4 {
					context.Fatal("empty paragraph Undo did not restore the actual replaced selection")
				}
				expectRenderedCollisionSave(context, state, driver, filename, original)
			})
		}
	}
}

func TestRenderedEmptyParagraphKeepsUTF8Intervals(test *testing.T) {
	for _, value := range []string{"\u4e2d\u6587", "A\U0001f469\u200d\U0001f4bbB", "e\u0301"} {
		for _, dark := range []bool{false, true} {
			test.Run(fmt.Sprintf("%q/dark-%v", value, dark), func(context *testing.T) {
				before, after := "\u9996\u6bb5\n\n", "\n\n\u5c3e\u6bb5\n"
				state, driver, filename, original := renderedSourceCollisionFixture(context, before+value+after, dark, 740)
				clickID(context, driver, renderedRows.At(1))
				for _, command := range []rune{'a', 'x'} {
					if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
						context.Fatal(err)
					}
				}
				if driver.Clipboard() != value || state.mode != renderedMode || !state.rich.active {
					context.Fatal("UTF-8 paragraph clearing lost text or rendered input")
				}
				clickID(context, driver, readModeID)
				clickID(context, driver, renderedModeID)
				clickID(context, driver, richInputID)
				if err := driver.Type("\u65b0\U0001f642"); err != nil {
					context.Fatal(err)
				}
				expected := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(before+"\u65b0\U0001f642"+after, "\n", "\r\n"))...)
				expectRenderedCollisionSave(context, state, driver, filename, expected)
				for range 2 {
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
				}
				expectRenderedCollisionSave(context, state, driver, filename, original)
			})
		}
	}
}

func TestRenderedEmptyParagraphKeepsNativeDeleteGroup(test *testing.T) {
	for _, scenario := range renderedEmptyParagraphCases {
		test.Run(scenario.name, func(context *testing.T) {
			state, driver, filename, original := renderedSourceCollisionFixture(context, scenario.before+"Solo"+scenario.after, false, 740)
			clickID(context, driver, renderedRows.At(scenario.row))
			state.rich.input.MoveTo(4, false)
			for range 4 {
				if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
					context.Fatal(err)
				}
			}
			for cycle := 0; cycle < 2; cycle++ {
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
				if state.mode != renderedMode || !state.rich.active || state.rich.input.String() != "Solo" || state.rich.input.Selection() != (text.Range{Start: 4, End: 4}) {
					context.Fatal("empty paragraph conversion split the native deletion Undo group")
				}
				expectRenderedCollisionSave(context, state, driver, filename, original)
				if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
					context.Fatal(err)
				}
				if state.mode != renderedMode || !state.rich.active || state.rich.input.String() != "" {
					context.Fatal("native deletion Redo lost the empty input")
				}
			}
		})
	}
}

func TestRenderedEmptyParagraphPreservesHorizontalWhitespace(test *testing.T) {
	for _, whitespace := range []struct{ name, before, after string }{
		{"control", "", ""}, {"leading-one", " ", ""}, {"leading-two", "  ", ""}, {"leading-three", "   ", ""},
		{"trailing-one", "", " "}, {"trailing-tab", "", "\t"}, {"both", "  ", " \t"},
	} {
		for _, route := range []gfx.ID{readModeID, editModeID, splitModeID} {
			test.Run(fmt.Sprintf("%s/%v", whitespace.name, route), func(context *testing.T) {
				before, after := "First\n\n"+whitespace.before, whitespace.after+"\n\nLast\n"
				state, driver, filename, original := renderedSourceCollisionFixture(context, before+"Solo"+after, false, 740)
				clickID(context, driver, renderedRows.At(1))
				for _, command := range []rune{'a', 'x'} {
					if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
						context.Fatal(err)
					}
				}
				bytesFor := func(value string) []byte {
					return append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(before+value+after, "\n", "\r\n"))...)
				}
				expectRenderedCollisionSave(context, state, driver, filename, bytesFor(""))
				clickID(context, driver, route)
				clickID(context, driver, renderedModeID)
				if !state.rich.active || state.rich.input == nil || state.rich.input.String() != "" {
					context.Fatal("retained horizontal whitespace prevented empty input restoration")
				}
				clickID(context, driver, richInputID)
				if err := driver.Type("New"); err != nil {
					context.Fatal(err)
				}
				expectRenderedCollisionSave(context, state, driver, filename, bytesFor("New"))
				for range 2 {
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
				}
				expectRenderedCollisionSave(context, state, driver, filename, original)
			})
		}
	}
}

func TestRenderedEmptyParagraphLeavesSpecialInputActive(test *testing.T) {
	for _, editor := range []string{"table", "fence"} {
		test.Run(editor, func(context *testing.T) {
			state, driver, _, _ := renderedSourceCollisionFixture(context, specialHistoryPrefix(editor)+"Solo\n\nLast\n", false, 740)
			clickID(context, driver, renderedRows.At(1))
			for _, command := range []rune{'a', 'x'} {
				if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
					context.Fatal(err)
				}
			}
			activateHistorySpecial(context, state, driver, editor)
		})
	}
}

func TestRenderedEmptyParagraphMovesToAdjacentBlock(test *testing.T) {
	for _, direction := range []gfx.Key{gfx.KeyUp, gfx.KeyDown} {
		for _, extend := range []bool{false, true} {
			test.Run(fmt.Sprintf("%v/extend-%v", direction, extend), func(context *testing.T) {
				state, driver, filename, original := renderedSourceCollisionFixture(context, "First\n\nSolo\n\nLast\n\nTail\n", false, 740)
				clickID(context, driver, renderedRows.At(1))
				for _, command := range []rune{'a', 'x'} {
					if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
						context.Fatal(err)
					}
				}
				modifiers := gfx.Modifiers(0)
				if extend {
					modifiers = gfx.ModifierShift
				}
				if err := driver.Key(direction, modifiers); err != nil {
					context.Fatal(err)
				}
				value := "Last"
				if direction == gfx.KeyUp {
					value = "First"
				}
				if !extend {
					if !state.rich.active || state.rich.input.String() != value {
						context.Fatalf("empty input navigation skipped its adjacent block: want %q", value)
					}
					return
				}
				if !state.selection.active {
					context.Fatal("empty input did not extend selection to its adjacent block")
				}
				if _, err := driver.ShortcutChord(primaryModifier(), 'c'); err != nil {
					context.Fatal(err)
				}
				copied := "\nLast"
				if direction == gfx.KeyUp {
					copied = "First\n"
				}
				if driver.Clipboard() != copied {
					context.Fatalf("empty anchor selected the wrong visible text: %q, want %q", driver.Clipboard(), copied)
				}
				driver.SetClipboard("New")
				if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
					context.Fatal(err)
				}
				expected := "First\n\nNew\n\nTail\n"
				if direction == gfx.KeyUp {
					expected = "New\n\nLast\n\nTail\n"
				}
				expectRenderedCollisionSave(context, state, driver, filename, append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(expected, "\n", "\r\n"))...))
				for _, command := range []rune{'z', 'y', 'z'} {
					if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
						context.Fatal(err)
					}
					if state.mode != renderedMode || !state.rich.active || command == 'z' && (!state.rich.virtual || state.rich.input.String() != "") {
						context.Fatal("selection history lost its actual empty anchor")
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

func TestRenderedEmptyParagraphSelectionReturnsToAnchor(test *testing.T) {
	for _, position := range []struct {
		name, before, after string
		row                 int
	}{
		{"first", "", "\n\nLast\n\nTail\n", 0},
		{"middle", "First\n\n", "\n\nLast\n\nTail\n", 1},
		{"last", "First\n\nPrevious\n\n", "\n", 2},
	} {
		for _, escape := range []bool{false, true} {
			test.Run(fmt.Sprintf("%s/escape-%v", position.name, escape), func(context *testing.T) {
				state, driver, filename, _ := renderedSourceCollisionFixture(context, position.before+"Solo"+position.after, false, 740)
				clickID(context, driver, renderedRows.At(position.row))
				for _, command := range []rune{'a', 'x'} {
					if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
						context.Fatal(err)
					}
				}
				key, reverse := gfx.KeyDown, gfx.KeyUp
				if position.name == "last" {
					key, reverse = gfx.KeyUp, gfx.KeyDown
				}
				if err := driver.Key(key, gfx.ModifierShift); err != nil || !state.selection.active {
					context.Fatal("empty paragraph selection did not activate")
				}
				if escape {
					reverse = gfx.KeyEscape
				}
				if err := driver.Key(reverse, gfx.ModifierShift); err != nil {
					context.Fatal(err)
				}
				if state.selection.active || !state.rich.active || !state.rich.virtual || state.rich.input.String() != "" || driver.Snapshot().Focused != richInputID {
					context.Fatal("selection did not collapse to the actual empty anchor")
				}
				if err := driver.Type("New"); err != nil {
					context.Fatal(err)
				}
				expected := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(position.before+"New"+position.after, "\n", "\r\n"))...)
				expectRenderedCollisionSave(context, state, driver, filename, expected)
			})
		}
	}
}

func TestRenderedEmptyParagraphSelectionCutKeepsSlot(test *testing.T) {
	for _, forward := range []bool{false, true} {
		test.Run(fmt.Sprintf("forward-%v", forward), func(context *testing.T) {
			state, driver, filename, original := renderedSourceCollisionFixture(context, "First\n\nSolo\n\nLast\n\nTail\n", false, 740)
			clickID(context, driver, renderedRows.At(1))
			for _, command := range []rune{'a', 'x'} {
				if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
					context.Fatal(err)
				}
			}
			key := gfx.KeyUp
			if forward {
				key = gfx.KeyDown
			}
			if err := driver.Key(key, gfx.ModifierShift); err != nil {
				context.Fatal(err)
			}
			if _, err := driver.ShortcutChord(primaryModifier(), 'x'); err != nil {
				context.Fatal(err)
			}
			if state.mode != renderedMode || !state.rich.active || !state.rich.virtual || state.rich.input.String() != "" {
				context.Fatal("cut from an empty anchor lost the surviving empty slot")
			}
			if err := driver.Type("New"); err != nil {
				context.Fatal(err)
			}
			expected := "New\n\nLast\n\nTail\n"
			if forward {
				expected = "First\n\nNew\n\nTail\n"
			}
			expectRenderedCollisionSave(context, state, driver, filename, append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(expected, "\n", "\r\n"))...))
			for range 3 {
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
			}
			expectRenderedCollisionSave(context, state, driver, filename, original)
		})
	}
}

func TestRenderedEmptyParagraphSelectionRejectsReadonlyNeighbor(test *testing.T) {
	for _, forward := range []bool{false, true} {
		test.Run(fmt.Sprintf("forward-%v", forward), func(context *testing.T) {
			source, row, key := "<div>opaque</div>\n\nSolo\n\nLast\n", 1, gfx.KeyUp
			if forward {
				source, row, key = "First\n\nSolo\n\n<div>opaque</div>\n", 1, gfx.KeyDown
			}
			state, driver, filename, _ := renderedSourceCollisionFixture(context, source, false, 740)
			clickID(context, driver, renderedRows.At(row))
			for _, command := range []rune{'a', 'x'} {
				if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
					context.Fatal(err)
				}
			}
			before := state.session.Current().Text()
			if err := driver.Key(key, gfx.ModifierShift); err != nil {
				context.Fatal(err)
			}
			if !state.selection.active || state.selection.editable {
				context.Fatal("empty anchor authorized editing its read-only neighbor")
			}
			driver.SetClipboard("New")
			for _, command := range []rune{'x', 'v'} {
				if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
					context.Fatal(err)
				}
			}
			expectRenderedCollisionSave(context, state, driver, filename, append([]byte{0xef, 0xbb, 0xbf}, []byte(before)...))
		})
	}
}

func TestRenderedEmptyParagraphPointerSelectionKeepsAnchor(test *testing.T) {
	for _, forward := range []bool{false, true} {
		test.Run(fmt.Sprintf("forward-%v", forward), func(context *testing.T) {
			state, driver, filename, original := renderedSourceCollisionFixture(context, "First\n\nSolo\n\nLast\n\nTail\n", false, 740)
			clickID(context, driver, renderedRows.At(1))
			for _, command := range []rune{'a', 'x'} {
				if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
					context.Fatal(err)
				}
			}
			index, value := 0, "First"
			if forward {
				index, value = 1, "Last"
			}
			anchor, target := hitBounds(context, driver, richInputID), hitBounds(context, driver, renderedRows.At(index))
			pointerX := target.X + state.rich.wrapCanvas.TextCaret(value, 16, gfx.WeightRegular, 2)
			pointerY := target.Y + 13
			if err := driver.Press(anchor.X+1, anchor.Y+13, gfx.PointerButtonPrimary); err != nil {
				context.Fatal(err)
			}
			if err := driver.Move(pointerX, pointerY); err != nil {
				context.Fatal(err)
			}
			if err := driver.Release(pointerX, pointerY, gfx.PointerButtonPrimary); err != nil {
				context.Fatal(err)
			}
			copied, expected := "rst\n", "FiNew\n\nLast\n\nTail\n"
			if forward {
				copied, expected = "\nLa", "First\n\nNewst\n\nTail\n"
			}
			if !state.selection.active || !state.selection.editable || state.selection.visible != copied {
				context.Fatalf("empty-anchor drag selected the wrong text: %q, want %q", state.selection.visible, copied)
			}
			driver.SetClipboard("New")
			if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
				context.Fatal(err)
			}
			expectRenderedCollisionSave(context, state, driver, filename, append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(expected, "\n", "\r\n"))...))
			for range 2 {
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
			}
			expectRenderedCollisionSave(context, state, driver, filename, original)
		})
	}
}

func TestRenderedEmptyParagraphSelectionKeepsLayout(test *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, width := range []int{740, 1200} {
			for _, key := range []gfx.Key{gfx.KeyUp, gfx.KeyDown} {
				test.Run(fmt.Sprintf("dark-%v/width-%d/key-%v", dark, width, key), func(context *testing.T) {
					state, driver, _, _ := renderedSourceCollisionFixture(context, "First\n\nSolo\n\nLast\n\nTail\n", dark, width)
					clickID(context, driver, renderedRows.At(1))
					for _, command := range []rune{'a', 'x'} {
						if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
							context.Fatal(err)
						}
					}
					before := make([]float32, len(state.preview.blocks))
					for index, block := range state.preview.blocks {
						before[index] = block.top
					}
					if err := driver.Key(key, gfx.ModifierShift); err != nil || !state.selection.active {
						context.Fatal("layout fixture did not select from its empty anchor")
					}
					if state.rich.active || state.rich.input != nil {
						context.Fatal("selection kept a hidden native editor for geometry")
					}
					for index, block := range state.preview.blocks {
						if block.top != before[index] {
							context.Fatalf("selection collapsed its empty anchor: block=%d top=%v want=%v", index, block.top, before[index])
						}
					}
				})
			}
		}
	}
}
