package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"

	"markstack/internal/workspace"
)

type sourceIndentHistoryCase struct {
	name      string
	before    string
	after     string
	selection text.Range
	mapped    text.Range
	outdent   bool
}

var sourceIndentHistoryCases = []sourceIndentHistoryCase{
	{"partial-indent", "  \u732bab\n\t\u72d7cd\nkeep\n", "\t  \u732bab\n\t\t\u72d7cd\nkeep\n", text.Range{Start: 3, End: 9}, text.Range{Start: 4, End: 11}, false},
	{"middle-empty", "\u732bab\n\n\u72d7cd\nkeep\n", "\t\u732bab\n\t\n\t\u72d7cd\nkeep\n", text.Range{Start: 1, End: 7}, text.Range{Start: 2, End: 10}, false},
	{"leading-empty", "\n\u732bab\n\u72d7cd\nkeep\n", "\t\n\t\u732bab\n\t\u72d7cd\nkeep\n", text.Range{Start: 0, End: 7}, text.Range{Start: 0, End: 10}, false},
	{"only-empty", "\n\nkeep\n", "\t\n\t\nkeep\n", text.Range{Start: 0, End: 2}, text.Range{Start: 0, End: 4}, false},
	{"partial-outdent", "  \u732bab\n\t\u72d7cd\nkeep\n", "\u732bab\n\u72d7cd\nkeep\n", text.Range{Start: 3, End: 9}, text.Range{Start: 1, End: 6}, true},
	{"collapsed-outdent", "  \u732bab\nkeep\n", "\u732bab\nkeep\n", text.Range{Start: 2, End: 2}, text.Range{}, true},
	{"inside-removed-prefix", "    \u732bab\n\t\u72d7cd\nkeep\n", "\u732bab\n\u72d7cd\nkeep\n", text.Range{Start: 1, End: 10}, text.Range{Start: 0, End: 5}, true},
	{"whitespace-line-boundary", " \n\t \nkeep\n", "\t \n\t\t \nkeep\n", text.Range{Start: 0, End: 5}, text.Range{Start: 0, End: 7}, false},
	{"collapsed-literal-tab", "abc\nkeep\n", "ab\tc\nkeep\n", text.Range{Start: 2, End: 2}, text.Range{Start: 3, End: 3}, false},
	{"no-op-outdent", "alpha\nbeta\n", "alpha\nbeta\n", text.Range{Start: 1, End: 8}, text.Range{Start: 1, End: 8}, true},
}

func sourceIndentFixtureText(value string, crlf bool) string {
	if crlf {
		return strings.ReplaceAll(value, "\n", "\r\n")
	}
	return value
}

func sourceIndentFixtureRange(value string, selected text.Range, crlf bool) text.Range {
	position := func(offset int) int {
		return utf8.RuneCountInString(sourceIndentFixtureText(string([]rune(value)[:offset]), crlf))
	}
	return text.Range{Start: position(selected.Start), End: position(selected.End)}
}

func assertSourceIndentHistoryState(test *testing.T, editor *text.Editor, body string, selected text.Range, backward bool) {
	test.Helper()
	caret := selected.End
	if backward {
		caret = selected.Start
	}
	if editor.String() != body || editor.Selection() != selected || editor.Caret() != caret {
		test.Fatalf("source/history state = %q %v caret=%d, want %q %v caret=%d", editor.String(), editor.Selection(), editor.Caret(), body, selected, caret)
	}
}

func assertSourceIndentSavedBytes(test *testing.T, filename, body string, bom bool) {
	test.Helper()
	want := []byte(body)
	if bom {
		want = append([]byte{0xef, 0xbb, 0xbf}, want...)
	}
	got, err := os.ReadFile(filename)
	if err != nil || !bytes.Equal(got, want) {
		test.Fatalf("saved bytes = %q, want %q, err=%v", got, want, err)
	}
	info, err := os.Stat(filename)
	if err != nil || info.Mode().Perm() != 0640 {
		test.Fatalf("saved permissions = %v, err=%v", info, err)
	}
}

func TestSourceIndentOrientedHistory(test *testing.T) {
	for _, extension := range []string{".go", ".txt"} {
		for _, crlf := range []bool{false, true} {
			for _, fixture := range sourceIndentHistoryCases {
				for _, backward := range []bool{false, true} {
					test.Run(fmt.Sprintf("%s/crlf-%t/%s/backward-%t", extension, crlf, fixture.name, backward), func(test *testing.T) {
						filename := filepath.Join(test.TempDir(), "source"+extension)
						before := sourceIndentFixtureText(fixture.before, crlf)
						after := sourceIndentFixtureText(fixture.after, crlf)
						data := []byte(before)
						if crlf {
							data = append([]byte{0xef, 0xbb, 0xbf}, data...)
						}
						if err := os.WriteFile(filename, data, 0640); err != nil {
							test.Fatal(err)
						}
						state, driver := testDriver(test)
						state.openFile(filename)
						if err := driver.Resize(1200, 780); err != nil {
							test.Fatal(err)
						}
						clickID(test, driver, sourceID)
						current := state.session.Current()
						selected := sourceIndentFixtureRange(fixture.before, fixture.selection, crlf)
						mapped := sourceIndentFixtureRange(fixture.after, fixture.mapped, crlf)
						oriented := selected
						if backward {
							oriented = text.Range{Start: selected.End, End: selected.Start}
						}
						current.Editor.SetSelection(oriented)
						modifiers := gfx.Modifiers(0)
						if fixture.outdent {
							modifiers = gfx.ModifierShift
						}
						if err := driver.Key(gfx.KeyTab, modifiers); err != nil {
							test.Fatal(err)
						}
						assertSourceIndentHistoryState(test, current.Editor, after, mapped, backward)
						if before == after {
							if current.Editor.CanUndo() || current.Editor.CanRedo() || current.Dirty() {
								test.Fatal("no-op outdent changed history or dirty state")
							}
							return
						}
						for range 2 {
							if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
								test.Fatal(err)
							}
							assertSourceIndentSavedBytes(test, filename, after, crlf)
							if handled, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil || !handled {
								test.Fatalf("Undo handled=%v, err=%v", handled, err)
							}
							assertSourceIndentHistoryState(test, current.Editor, before, selected, backward)
							if current.Editor.CanUndo() {
								test.Fatal("indent was not a single Undo unit")
							}
							if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
								test.Fatal(err)
							}
							assertSourceIndentSavedBytes(test, filename, before, crlf)
							if handled, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil || !handled {
								test.Fatalf("Redo handled=%v, err=%v", handled, err)
							}
							assertSourceIndentHistoryState(test, current.Editor, after, mapped, backward)
						}
					})
				}
			}
		}
	}
}

func TestSourceIndentCompositionAndExternalConflict(test *testing.T) {
	for _, extension := range []string{".go", ".txt"} {
		for _, crlf := range []bool{false, true} {
			test.Run(fmt.Sprintf("%s/crlf-%t", extension, crlf), func(test *testing.T) {
				filename := filepath.Join(test.TempDir(), "source"+extension)
				before := sourceIndentFixtureText("  alpha\n\tbeta\nkeep\n", crlf)
				data := []byte(before)
				if crlf {
					data = append([]byte{0xef, 0xbb, 0xbf}, data...)
				}
				if err := os.WriteFile(filename, data, 0640); err != nil {
					test.Fatal(err)
				}
				state, driver := testDriver(test)
				state.openFile(filename)
				if err := driver.Resize(1200, 780); err != nil {
					test.Fatal(err)
				}
				clickID(test, driver, sourceID)
				current := state.session.Current()
				selected := text.Range{Start: 3, End: 5}
				current.Editor.SetSelection(selected)
				if err := driver.Compose("ni"); err != nil {
					test.Fatal(err)
				}
				preedit := current.Text()
				for _, modifiers := range []gfx.Modifiers{0, gfx.ModifierShift} {
					if err := driver.Key(gfx.KeyTab, modifiers); err != nil {
						test.Fatal(err)
					}
					if _, composing := current.Editor.Composition(); !composing || current.Text() != preedit {
						test.Fatal("indent changed or ended the active composition")
					}
					assertSourceIndentSavedBytes(test, filename, before, crlf)
				}
				if !current.Editor.CancelComposition() {
					test.Fatal("composition cancellation refused")
				}
				assertSourceIndentHistoryState(test, current.Editor, before, selected, false)
				if current.Editor.CanUndo() || current.Dirty() {
					test.Fatal("refused composition indentation changed committed history")
				}
				external := []byte("external owner\r\n")
				if err := os.WriteFile(filename, external, 0640); err != nil {
					test.Fatal(err)
				}
				if err := driver.Key(gfx.KeyTab, 0); err != nil {
					test.Fatal(err)
				}
				if current.Text() == before || !current.Dirty() {
					test.Fatal("post-composition indentation did not run")
				}
				if err := state.session.Save(state.session.Active, ""); !errors.Is(err, workspace.ErrConflict) {
					test.Fatalf("external save guard = %v", err)
				}
				got, err := os.ReadFile(filename)
				if err != nil || !bytes.Equal(got, external) {
					test.Fatalf("external owner bytes overwritten: %q, %v", got, err)
				}
			})
		}
	}
}

func TestSourceIndentEarlierLaterEditsAndTypingAfterUndo(test *testing.T) {
	for _, extension := range []string{".go", ".txt"} {
		for _, crlf := range []bool{false, true} {
			for _, backward := range []bool{false, true} {
				test.Run(fmt.Sprintf("%s/crlf-%t/backward-%t", extension, crlf, backward), func(test *testing.T) {
					fixture := sourceIndentHistoryCases[0]
					before := sourceIndentFixtureText(fixture.before, crlf)
					after := sourceIndentFixtureText(fixture.after, crlf)
					filename := filepath.Join(test.TempDir(), "source"+extension)
					data := []byte(before)
					if crlf {
						data = append([]byte{0xef, 0xbb, 0xbf}, data...)
					}
					if err := os.WriteFile(filename, data, 0640); err != nil {
						test.Fatal(err)
					}
					state, driver := testDriver(test)
					state.openFile(filename)
					if err := driver.Resize(1200, 780); err != nil {
						test.Fatal(err)
					}
					clickID(test, driver, sourceID)
					current := state.session.Current()
					current.Editor.MoveTo(current.Editor.RuneCount(), false)
					if err := driver.Type("!"); err != nil {
						test.Fatal(err)
					}
					selected := sourceIndentFixtureRange(fixture.before, fixture.selection, crlf)
					mapped := sourceIndentFixtureRange(fixture.after, fixture.mapped, crlf)
					oriented := selected
					if backward {
						oriented = text.Range{Start: selected.End, End: selected.Start}
					}
					current.Editor.SetSelection(oriented)
					if err := driver.Key(gfx.KeyTab, 0); err != nil {
						test.Fatal(err)
					}
					assertSourceIndentHistoryState(test, current.Editor, after+"!", mapped, backward)
					if err := driver.Type("L"); err != nil {
						test.Fatal(err)
					}
					later := current.Text()
					undo := func() {
						test.Helper()
						if handled, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil || !handled {
							test.Fatalf("Undo handled=%v err=%v", handled, err)
						}
					}
					redo := func() {
						test.Helper()
						if handled, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil || !handled {
							test.Fatalf("Redo handled=%v err=%v", handled, err)
						}
					}
					undo()
					assertSourceIndentHistoryState(test, current.Editor, after+"!", mapped, backward)
					undo()
					assertSourceIndentHistoryState(test, current.Editor, before+"!", selected, backward)
					undo()
					if current.Text() != before || current.Dirty() || current.Editor.CanUndo() {
						test.Fatalf("earlier edit Undo = %q, dirty=%v", current.Text(), current.Dirty())
					}
					redo()
					if current.Text() != before+"!" {
						test.Fatal("earlier edit did not redo")
					}
					redo()
					assertSourceIndentHistoryState(test, current.Editor, after+"!", mapped, backward)
					redo()
					if current.Text() != later {
						test.Fatal("later edit did not redo")
					}
					undo()
					undo()
					assertSourceIndentHistoryState(test, current.Editor, before+"!", selected, backward)
					if err := driver.Type("Z"); err != nil {
						test.Fatal(err)
					}
					original := []rune(before + "!")
					want := string(original[:selected.Start]) + "Z" + string(original[selected.End:])
					if current.Text() != want || current.Editor.CanRedo() {
						test.Fatalf("typing after Undo = %q, want %q; stale Redo=%v", current.Text(), want, current.Editor.CanRedo())
					}
					if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
						test.Fatal(err)
					}
					assertSourceIndentSavedBytes(test, filename, want, crlf)
				})
			}
		}
	}
}

func TestSourceIndentNoOpOutdentPreservesRedo(test *testing.T) {
	for _, extension := range []string{".go", ".txt"} {
		test.Run(extension, func(test *testing.T) {
			filename := filepath.Join(test.TempDir(), "source"+extension)
			before := "alpha\nbeta\n"
			if err := os.WriteFile(filename, []byte(before), 0640); err != nil {
				test.Fatal(err)
			}
			state, driver := testDriver(test)
			state.openFile(filename)
			if err := driver.Resize(1200, 780); err != nil {
				test.Fatal(err)
			}
			clickID(test, driver, sourceID)
			current := state.session.Current()
			current.Editor.MoveTo(current.Editor.RuneCount(), false)
			if err := driver.Type("Z"); err != nil {
				test.Fatal(err)
			}
			if handled, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil || !handled || !current.Editor.CanRedo() {
				test.Fatalf("prepare Redo handled=%v err=%v", handled, err)
			}
			current.Editor.SetSelection(text.Range{Start: 8, End: 1})
			if err := driver.Key(gfx.KeyTab, gfx.ModifierShift); err != nil {
				test.Fatal(err)
			}
			assertSourceIndentHistoryState(test, current.Editor, before, text.Range{Start: 1, End: 8}, true)
			if !current.Editor.CanRedo() || current.Editor.CanUndo() || current.Dirty() {
				test.Fatal("no-op outdent invalidated history or changed dirty state")
			}
			if handled, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil || !handled || current.Text() != before+"Z" {
				test.Fatalf("preserved Redo = %q, handled=%v err=%v", current.Text(), handled, err)
			}
		})
	}
}

func TestSourceIndentPrefixReplacements(test *testing.T) {
	for _, crlf := range []bool{false, true} {
		for _, fixture := range sourceIndentHistoryCases {
			if fixture.name == "collapsed-literal-tab" {
				continue
			}
			for _, backward := range []bool{false, true} {
				test.Run(fmt.Sprintf("crlf-%t/%s/backward-%t", crlf, fixture.name, backward), func(test *testing.T) {
					before := sourceIndentFixtureText(fixture.before, crlf)
					selected := sourceIndentFixtureRange(fixture.before, fixture.selection, crlf)
					mapped := sourceIndentFixtureRange(fixture.after, fixture.mapped, crlf)
					caret := selected.End
					if backward {
						caret = selected.Start
						mapped.Start, mapped.End = mapped.End, mapped.Start
					}
					edits, after := sourceIndentReplacements(before, selected, caret, fixture.outdent)
					if after != mapped {
						test.Fatalf("oriented mapping = %v, want %v", after, mapped)
					}
					original := []rune(before)
					previousEnd := -1
					for _, edit := range edits {
						if edit.Range.Start <= previousEnd || edit.Range.End < edit.Range.Start || edit.Range.End > len(original) {
							test.Fatalf("invalid original-prefix batch: %v", edits)
						}
						if edit.Range.Start != 0 && original[edit.Range.Start-1] != '\n' {
							test.Fatalf("replacement is not at an original line prefix: %v", edit)
						}
						removed := string(original[edit.Range.Start:edit.Range.End])
						if fixture.outdent {
							if edit.Text != "" || removed != "\t" && (len(removed) == 0 || len(removed) > 4 || strings.Trim(removed, " ") != "") {
								test.Fatalf("outdent changed non-prefix bytes: %v removed=%q", edit, removed)
							}
						} else if removed != "" || edit.Text != "\t" {
							test.Fatalf("indent is not one literal prefix tab: %v", edit)
						}
						previousEnd = edit.Range.End
					}
					actual := append([]rune(nil), original...)
					for index := len(edits) - 1; index >= 0; index-- {
						edit := edits[index]
						actual = append(append(append([]rune(nil), actual[:edit.Range.Start]...), []rune(edit.Text)...), actual[edit.Range.End:]...)
					}
					if want := sourceIndentFixtureText(fixture.after, crlf); string(actual) != want {
						test.Fatalf("prefix-only bytes = %q, want %q", string(actual), want)
					}
				})
			}
		}
	}
}
