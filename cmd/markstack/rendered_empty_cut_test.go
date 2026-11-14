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

func TestRenderedCrossBlockCutAllKeepsEmptyDocumentEditable(test *testing.T) {
	for _, scenario := range []struct {
		name     string
		prefix   string
		suffix   string
		backward bool
	}{
		{"final-newline", "", "\n", false},
		{"no-final-newline", "", "", false},
		{"leading-newlines", "\n\n", "\n", false},
		{"trailing-newlines", "", "\n\n\n", false},
		{"reverse-final-newline", "", "\n", true},
		{"reverse-no-final-newline", "", "", true},
		{"reverse-leading-newlines", "\n\n", "\n", true},
		{"reverse-trailing-newlines", "", "\n\n\n", true},
	} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{740, 1200} {
				test.Run(fmt.Sprintf("%s/dark-%v/width-%d", scenario.name, dark, width), func(context *testing.T) {
					state, driver, filename, original := renderedSourceCollisionFixture(context, scenario.prefix+"Left\n\nRight"+scenario.suffix, dark, width)
					beforeValue, beforeCaret, beforeRow, key := "Left", 0, 0, gfx.KeyDown
					if scenario.backward {
						beforeValue, beforeCaret, beforeRow, key = "Right", 5, 1, gfx.KeyUp
					}
					clickID(context, driver, renderedRows.At(beforeRow))
					state.rich.input.MoveTo(beforeCaret, false)
					if err := driver.Key(key, gfx.ModifierShift); err != nil {
						context.Fatal(err)
					}
					if !state.selection.active || !state.selection.editable || state.selection.visible != "Left\nRight" {
						context.Fatal("full supported selection did not activate")
					}
					if _, err := driver.ShortcutChord(primaryModifier(), 'x'); err != nil {
						context.Fatal(err)
					}
					if driver.Clipboard() != "Left\nRight" {
						context.Fatalf("cut clipboard=%q", driver.Clipboard())
					}
					bytesFor := func(value string) []byte {
						return append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(scenario.prefix+value+scenario.suffix, "\n", "\r\n"))...)
					}
					assertInput := func(value string, caret, row int) {
						context.Helper()
						if state.mode != renderedMode || !state.rich.active || state.rich.index != row || state.rich.input.String() != value || state.rich.input.Selection() != (text.Range{Start: caret, End: caret}) || driver.Snapshot().Focused != richInputID {
							context.Fatalf("empty cut history lost rendered input: mode=%v active=%v focus=%v", state.mode, state.rich.active, driver.Snapshot().Focused)
						}
					}
					assertInput("", 0, 0)
					expectRenderedCollisionSave(context, state, driver, filename, bytesFor(""))
					before := state.session.Current().Text()
					if err := driver.Compose("ni"); err != nil {
						context.Fatal(err)
					}
					if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
						context.Fatal(err)
					}
					stored, err := os.ReadFile(filename)
					if err != nil || !bytes.Equal(stored, bytesFor("")) || state.session.Current().Text() != before || state.session.Current().Dirty() {
						context.Fatal("empty-document preedit entered committed source or disk")
					}
					if _, err := state.onRenderedTextEvent(app.TextEvent{Kind: app.TextCancelComposition}); err != nil {
						context.Fatal(err)
					}
					if err := driver.Pump(); err != nil {
						context.Fatal(err)
					}
					assertInput("", 0, 0)
					if err := driver.Type("New"); err != nil {
						context.Fatal(err)
					}
					assertInput("New", 3, 0)
					expectRenderedCollisionSave(context, state, driver, filename, bytesFor("New"))
					for cycle := 0; cycle < 2; cycle++ {
						for _, step := range []struct {
							command rune
							value   string
							caret   int
							row     int
							bytes   []byte
						}{{'z', "", 0, 0, bytesFor("")}, {'z', beforeValue, beforeCaret, beforeRow, original}, {'y', "", 0, 0, bytesFor("")}, {'y', "New", 3, 0, bytesFor("New")}} {
							if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
								context.Fatal(err)
							}
							assertInput(step.value, step.caret, step.row)
							expectRenderedCollisionSave(context, state, driver, filename, step.bytes)
						}
					}
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
					if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
						context.Fatal(err)
					}
					assertInput("", 0, 0)
					expectRenderedCollisionSave(context, state, driver, filename, bytesFor(""))
					if err := driver.Key(gfx.KeyEnter, 0); err != nil {
						context.Fatal(err)
					}
					expectRenderedCollisionSave(context, state, driver, filename, bytesFor("\n"))
					if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
						context.Fatal(err)
					}
					assertInput("", 0, 0)
					expectRenderedCollisionSave(context, state, driver, filename, bytesFor(""))
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
					assertInput(beforeValue, beforeCaret, beforeRow)
					expectRenderedCollisionSave(context, state, driver, filename, original)
				})
			}
		}
	}
}

func TestRenderedEmptyCutHistorySurvivesInactiveTabs(test *testing.T) {
	for _, extension := range []string{".md", ".txt", ".go", ".mh"} {
		for _, reopen := range []bool{false, true} {
			for _, dark := range []bool{false, true} {
				for _, width := range []int{740, 1200} {
					test.Run(fmt.Sprintf("%s/reopen-%v/dark-%v/width-%d", extension, reopen, dark, width), func(context *testing.T) {
						state, driver, filename, original := renderedSourceCollisionFixture(context, "Left\n\nRight\n", dark, width)
						current, tab := state.session.Current(), state.session.Active
						clickID(context, driver, renderedRows.At(0))
						state.rich.input.MoveTo(0, false)
						if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
							context.Fatal(err)
						}
						if _, err := driver.ShortcutChord(primaryModifier(), 'x'); err != nil {
							context.Fatal(err)
						}
						empty := []byte{0xef, 0xbb, 0xbf, '\r', '\n'}
						expectRenderedCollisionSave(context, state, driver, filename, empty)
						other := filepath.Join(context.TempDir(), "Other"+extension)
						otherBytes := []byte("<p>Other</p>\n")
						if err := os.WriteFile(other, otherBytes, 0640); err != nil {
							context.Fatal(err)
						}
						for cycle := 0; cycle < 2; cycle++ {
							for _, command := range []rune{'z', 'y'} {
								if !state.openFile(other) {
									context.Fatal("other document did not open")
								}
								state.invalidator.Invalidate()
								if err := driver.Pump(); err != nil {
									context.Fatal(err)
								}
								if reopen {
									if !state.openFile(filename) {
										context.Fatal("original document did not reopen")
									}
									state.invalidator.Invalidate()
									if err := driver.Pump(); err != nil {
										context.Fatal(err)
									}
								} else {
									clickID(context, driver, tabRows.At(tab))
								}
								if state.mode != renderedMode {
									clickID(context, driver, renderedModeID)
								}
								if state.session.Current() != current {
									context.Fatal("returning from the other document changed document identity")
								}
								if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
									context.Fatal(err)
								}
								if state.mode != renderedMode || !state.rich.active || driver.Snapshot().Focused != richInputID {
									context.Fatal("empty cut history lost its inactive rendered target")
								}
								wanted := original
								if command == 'y' {
									wanted = empty
									if !state.rich.virtual || state.rich.index != 0 || state.rich.input.String() != "" {
										context.Fatal("empty cut Redo did not restore the first virtual paragraph")
									}
								}
								expectRenderedCollisionSave(context, state, driver, filename, wanted)
							}
						}
						if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
							context.Fatal(err)
						}
						if err := driver.Type("Q"); err != nil {
							context.Fatal(err)
						}
						branch := current.Text()
						if !strings.HasPrefix(branch, "QLeft") {
							context.Fatal("the restored paragraph did not accept a new source branch")
						}
						if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
							context.Fatal(err)
						}
						if current.Text() != branch || current.Editor.CanRedo() {
							context.Fatal("new input retained the obsolete empty-cut Redo")
						}
						stored, err := os.ReadFile(other)
						if err != nil || !bytes.Equal(stored, otherBytes) {
							context.Fatal("empty-cut traversal changed the other document")
						}
					})
				}
			}
		}
	}
}

func TestRenderedEmptyCutResumesTypingAfterNavigation(test *testing.T) {
	for _, transition := range []struct {
		name      string
		extension string
		reopen    bool
		mode      gfx.ID
	}{
		{"tab-markdown", ".md", false, gfx.ID{}},
		{"tab-plain", ".txt", false, gfx.ID{}},
		{"tab-code", ".go", false, gfx.ID{}},
		{"tab-html", ".mh", false, gfx.ID{}},
		{"reopen-markdown", ".md", true, gfx.ID{}},
		{"reopen-plain", ".txt", true, gfx.ID{}},
		{"reopen-code", ".go", true, gfx.ID{}},
		{"reopen-html", ".mh", true, gfx.ID{}},
		{"source", "", false, editModeID},
		{"split", "", false, splitModeID},
		{"read", "", false, readModeID},
	} {
		for _, remnant := range []struct {
			name, prefix, suffix string
		}{{"empty", "", ""}, {"leading-newlines", "\n\n", "\n"}} {
			for _, viewport := range []struct {
				dark  bool
				width int
			}{{false, 740}, {true, 1200}} {
				test.Run(fmt.Sprintf("%s/%s/dark-%v/width-%d", transition.name, remnant.name, viewport.dark, viewport.width), func(context *testing.T) {
					state, driver, filename, original := renderedSourceCollisionFixture(context, remnant.prefix+"Left\n\nRight"+remnant.suffix, viewport.dark, viewport.width)
					current, tab := state.session.Current(), state.session.Active
					clickID(context, driver, renderedRows.At(0))
					state.rich.input.MoveTo(0, false)
					if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
						context.Fatal(err)
					}
					if !state.selection.active || !state.selection.editable || state.selection.visible != "Left\nRight" {
						context.Fatal("ordinary paragraphs were not fully selected")
					}
					if _, err := driver.ShortcutChord(primaryModifier(), 'x'); err != nil {
						context.Fatal(err)
					}
					bytesFor := func(value string) []byte {
						return append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(remnant.prefix+value+remnant.suffix, "\n", "\r\n"))...)
					}
					expectRenderedCollisionSave(context, state, driver, filename, bytesFor(""))
					emptySource := current.Text()
					other, otherBytes := "", []byte("<p>Other</p>\n")
					if transition.extension != "" {
						other = filepath.Join(context.TempDir(), "Other"+transition.extension)
						if err := os.WriteFile(other, otherBytes, 0640); err != nil {
							context.Fatal(err)
						}
						if !state.openFile(other) {
							context.Fatal("other document did not open")
						}
						state.invalidator.Invalidate()
						if err := driver.Pump(); err != nil {
							context.Fatal(err)
						}
						if transition.reopen {
							if !state.openFile(filename) {
								context.Fatal("already-open document did not reopen")
							}
							state.invalidator.Invalidate()
							if err := driver.Pump(); err != nil {
								context.Fatal(err)
							}
						} else {
							clickID(context, driver, tabRows.At(tab))
						}
					} else {
						clickID(context, driver, transition.mode)
					}
					if state.mode != renderedMode {
						clickID(context, driver, renderedModeID)
					}
					if state.session.Current() != current || current.Text() != emptySource || current.Dirty() {
						context.Fatal("navigation changed the empty document identity or committed source")
					}
					if !state.rich.active || state.rich.input == nil || state.rich.input.String() != "" {
						context.Fatal("returning to the empty rendered document lost the input target before any Undo/Redo")
					}
					clickID(context, driver, richInputID)
					if err := driver.Type("New"); err != nil {
						context.Fatal(err)
					}
					if !state.rich.active || state.rich.input.String() != "New" || driver.Snapshot().Focused != richInputID {
						context.Fatal("returned empty document did not accept ordinary input")
					}
					expectRenderedCollisionSave(context, state, driver, filename, bytesFor("New"))
					for _, step := range []struct {
						command rune
						bytes   []byte
					}{{'z', bytesFor("")}, {'z', original}, {'y', bytesFor("")}, {'y', bytesFor("New")}} {
						if _, err := driver.ShortcutChord(primaryModifier(), step.command); err != nil {
							context.Fatal(err)
						}
						if state.mode != renderedMode || !state.rich.active || driver.Snapshot().Focused != richInputID {
							context.Fatal("input after navigation lost rendered history continuity")
						}
						expectRenderedCollisionSave(context, state, driver, filename, step.bytes)
					}
					if other != "" {
						stored, err := os.ReadFile(other)
						if err != nil || !bytes.Equal(stored, otherBytes) {
							context.Fatal("continued input changed the other document")
						}
					}
				})
			}
		}
	}
}

func TestRenderedEmptyCutKeepsCurrentEmptyPositionOnReturn(test *testing.T) {
	for _, action := range []string{"enter", "type-undo", "type-delete"} {
		for _, dark := range []bool{false, true} {
			test.Run(fmt.Sprintf("%s/dark-%v", action, dark), func(context *testing.T) {
				state, driver, filename, _ := renderedSourceCollisionFixture(context, "\n\nLeft\n\nRight\n", dark, 740)
				clickID(context, driver, renderedRows.At(0))
				state.rich.input.MoveTo(0, false)
				if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
					context.Fatal(err)
				}
				if _, err := driver.ShortcutChord(primaryModifier(), 'x'); err != nil {
					context.Fatal(err)
				}
				prefix := "\r\n\r\n"
				if action == "enter" {
					if err := driver.Key(gfx.KeyEnter, 0); err != nil {
						context.Fatal(err)
					}
					prefix += "\r\n"
				} else {
					if err := driver.Type("P"); err != nil {
						context.Fatal(err)
					}
					if action == "type-undo" {
						if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
							context.Fatal(err)
						}
					} else if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
						context.Fatal(err)
					}
				}
				other := filepath.Join(context.TempDir(), "Other.txt")
				if err := os.WriteFile(other, []byte("Other\n"), 0640); err != nil {
					context.Fatal(err)
				}
				for _, path := range []string{other, filename} {
					if !state.openFile(path) {
						context.Fatal("document did not open")
					}
					state.invalidator.Invalidate()
					if err := driver.Pump(); err != nil {
						context.Fatal(err)
					}
				}
				clickID(context, driver, renderedModeID)
				clickID(context, driver, richInputID)
				if err := driver.Type("Q"); err != nil {
					context.Fatal(err)
				}
				expected := append([]byte{0xef, 0xbb, 0xbf}, []byte(prefix+"Q\r\n")...)
				expectRenderedCollisionSave(context, state, driver, filename, expected)
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
				if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
					context.Fatal(err)
				}
				if state.mode != renderedMode || !state.rich.active || state.rich.input.String() != "" {
					context.Fatal("empty root Backspace lost its editing target after navigation")
				}
				expectRenderedCollisionSave(context, state, driver, filename, []byte{0xef, 0xbb, 0xbf, '\r', '\n', '\r', '\n', '\r', '\n'})
			})
		}
	}
}

func TestRenderedEmptyTargetRejectsStaleMetadata(test *testing.T) {
	for _, mutation := range []string{"valid", "missing", "document", "source", "changed-whitespace", "nonempty", "negative-start", "past-end", "reversed", "negative-depth"} {
		test.Run(mutation, func(context *testing.T) {
			state, driver, _, _ := renderedSourceCollisionFixture(context, "\n\nLeft\n\nRight\n", false, 740)
			clickID(context, driver, renderedRows.At(0))
			state.rich.input.MoveTo(0, false)
			if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
				context.Fatal(err)
			}
			if _, err := driver.ShortcutChord(primaryModifier(), 'x'); err != nil {
				context.Fatal(err)
			}
			current := state.session.Current()
			state.deactivateRenderedView(current)
			if state.rich.input != nil || state.rich.active || state.rich.emptyTarget == nil {
				context.Fatal("deactivation did not keep only the bounded empty-target metadata")
			}
			switch mutation {
			case "missing":
				state.rich.emptyTarget = nil
			case "document":
				state.rich.documentID++
			case "source":
				state.rich.source += "\r\n"
			case "changed-whitespace":
				current.Replace("\r\n")
			case "nonempty":
				current.Replace("<div>opaque</div>")
				state.rich.source = current.Text()
			case "negative-start":
				state.rich.emptyTarget.start = -1
			case "past-end":
				state.rich.emptyTarget.end = len(current.Text()) + 1
			case "reversed":
				state.rich.emptyTarget.end = state.rich.emptyTarget.start - 1
			case "negative-depth":
				state.rich.emptyTarget.depth = -1
			}
			source, selection, undo, redo := current.Text(), current.Editor.Selection(), current.Editor.CanUndo(), current.Editor.CanRedo()
			state.restoreRenderedEmptyTarget(current)
			if mutation == "valid" {
				if !state.rich.active || state.rich.input == nil || state.rich.input.String() != "" {
					context.Fatal("valid empty-target metadata did not restore the editor")
				}
			} else if state.rich.active || state.rich.input != nil {
				context.Fatal("invalid empty-target metadata created an editor")
			}
			if current.Text() != source || current.Editor.Selection() != selection || current.Editor.CanUndo() != undo || current.Editor.CanRedo() != redo {
				context.Fatal("empty-target validation changed committed source or history")
			}
		})
	}
}

func TestRenderedEmptyCutDoesNotBypassReadOnlyBlocks(test *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, width := range []int{740, 1200} {
			test.Run(fmt.Sprintf("dark-%v/width-%d", dark, width), func(context *testing.T) {
				state, driver, filename, original := renderedSourceCollisionFixture(context, "Left\n\nRight\n\n<div>opaque</div>\n", dark, width)
				clickID(context, driver, renderedRows.At(0))
				state.rich.input.MoveTo(0, false)
				for range 2 {
					if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
						context.Fatal(err)
					}
				}
				if !state.selection.active || state.selection.editable {
					context.Fatal("read-only fixture did not select its unsafe block")
				}
				if _, err := driver.ShortcutChord(primaryModifier(), 'x'); err != nil {
					context.Fatal(err)
				}
				if state.rich.virtual || state.session.Current().Dirty() {
					context.Fatal("read-only refusal created an empty virtual editor or source edit")
				}
				expectRenderedCollisionSave(context, state, driver, filename, original)
			})
		}
	}
}

func TestRenderedEmptyDocumentClearsPreviousPreviewBlocks(test *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, width := range []int{740, 1200} {
			test.Run(fmt.Sprintf("dark-%v/width-%d", dark, width), func(context *testing.T) {
				state, driver, filename, _ := renderedSourceCollisionFixture(context, "Left\n\nRight\n", dark, width)
				clickID(context, driver, editModeID)
				state.session.Current().Replace("")
				for _, mode := range []gfx.ID{renderedModeID, splitModeID, readModeID} {
					clickID(context, driver, mode)
					if state.session.Current().Text() != "" || len(state.parsed.Blocks) != 0 || len(state.preview.blocks) != 0 {
						context.Fatal("empty document retained a stale Markdown preview")
					}
				}
				expectRenderedCollisionSave(context, state, driver, filename, []byte{0xef, 0xbb, 0xbf})
			})
		}
	}
}
