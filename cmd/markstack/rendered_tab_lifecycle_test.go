package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"

	"markstack/internal/session"
)

func TestRenderedTabHistoryKeepsNewOwnerImageInspector(test *testing.T) {
	for _, route := range []string{"new", "open", "activate", "close"} {
		for _, width := range []int{740, 1200} {
			test.Run(fmt.Sprintf("%s/%d", route, width), func(context *testing.T) {
				state, driver, filename, original := renderedSourceCollisionFixture(context, "abcd\n\n## Target\n", width == 740, width)
				first, firstTab := state.session.Current(), state.session.Active
				clickID(context, driver, renderedRows.At(0))
				state.rich.input.MoveTo(2, false)
				if err := driver.Key(gfx.KeyEnter, 0); err != nil {
					context.Fatal(err)
				}
				other := filepath.Join(filepath.Dir(filename), "other.md")
				if err := os.WriteFile(other, []byte("# Other\n"), 0640); err != nil {
					context.Fatal(err)
				}
				switch route {
				case "new":
					state.session.NewDocument()
				case "open":
					if _, err := state.session.Open(other); err != nil {
						context.Fatal(err)
					}
				case "activate", "close":
					if !state.openFile(other) {
						context.Fatal("image fixture did not open the other document")
					}
					state.invalidator.Invalidate()
					if err := driver.Pump(); err != nil {
						context.Fatal(err)
					}
					if route == "activate" {
						state.session.Active = firstTab
					} else if !state.session.RequestClose(state.session.Active) {
						context.Fatal("image fixture did not close the clean document")
					}
				}
				current := state.session.Current()
				before, selection := current.Text(), current.Editor.Selection()
				canUndo, canRedo := current.Editor.CanUndo(), current.Editor.CanRedo()
				bitmap := gfx.Bitmap{Width: 8, Height: 8, Stride: 32, Pix: make([]byte, 8*8*4)}
				state.openImageInspectorBitmap(current, markdownPreviewImage{bitmap: bitmap, alt: "test"})
				if err := driver.Pump(); err != nil {
					context.Fatal(err)
				}
				if !state.imageInspector.open || state.imageInspector.documentID != current.ID || state.imageInspector.geometry.Image.Width <= 0 {
					context.Fatal("owner synchronization discarded the new document's image inspector")
				}
				if err := driver.Key(gfx.KeyEscape, 0); err != nil {
					context.Fatal(err)
				}
				if state.imageInspector.open || current.Text() != before || current.Editor.Selection() != selection || current.Editor.CanUndo() != canUndo || current.Editor.CanRedo() != canRedo {
					context.Fatal("image inspection changed its document or history")
				}
				clickID(context, driver, tabRows.At(firstTab))
				activateSpecialHistoryTarget(context, state, driver)
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
				if state.session.Current() != first || state.mode != renderedMode || !state.rich.active || state.rich.index != 0 || state.rich.input.String() != "abcd" || state.rich.input.Caret() != 2 {
					context.Fatal("image inspection lost the original document's structural history")
				}
				expectRenderedCollisionSave(context, state, driver, filename, original)
			})
		}
	}
}

func TestRenderedTabHistoryKeepsSameByteDocumentsIndependent(test *testing.T) {
	state, driver, filename, original := renderedSourceCollisionFixture(test, "abcd\n\n## Target\n", false, 1200)
	first, firstTab := state.session.Current(), state.session.Active
	clickID(test, driver, renderedRows.At(0))
	state.rich.input.SetSelection(text.Range{Start: 3, End: 1})
	state.applyBoldBlock()
	state.invalidator.Invalidate()
	if err := driver.Pump(); err != nil {
		test.Fatal(err)
	}
	changed := first.Text()
	other := filepath.Join(filepath.Dir(filename), "same.md")
	if err := os.WriteFile(other, original, 0640); err != nil {
		test.Fatal(err)
	}
	state.openFile(other)
	state.invalidator.Invalidate()
	if err := driver.Pump(); err != nil {
		test.Fatal(err)
	}
	second, secondTab := state.session.Current(), state.session.Active
	clickID(test, driver, renderedRows.At(0))
	state.rich.input.SetSelection(text.Range{Start: 1, End: 3})
	state.applyBoldBlock()
	state.invalidator.Invalidate()
	if err := driver.Pump(); err != nil {
		test.Fatal(err)
	}
	if first.ID == second.ID || first.Text() != second.Text() || second.Text() != changed {
		test.Fatal("same-byte document fixture is not independent")
	}
	for range 2 {
		for _, target := range []struct {
			document *session.Document
			other    *session.Document
			tab      int
			caret    int
		}{
			{first, second, firstTab, 1},
			{second, first, secondTab, 3},
		} {
			clickID(test, driver, tabRows.At(target.tab))
			activateSpecialHistoryTarget(test, state, driver)
			otherSource, otherSelection := target.other.Text(), target.other.Editor.Selection()
			otherUndo, otherRedo := target.other.Editor.CanUndo(), target.other.Editor.CanRedo()
			if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
				test.Fatal(err)
			}
			if state.session.Current() != target.document || state.mode != renderedMode || !state.rich.active || state.rich.index != 0 || state.rich.input.String() != "abcd" || state.rich.input.Selection() != (text.Range{Start: 1, End: 3}) || state.rich.input.Caret() != target.caret || driver.Snapshot().Focused != richInputID {
				test.Fatal("same-byte tab reused another document's directed history")
			}
			if target.other.Text() != otherSource || target.other.Editor.Selection() != otherSelection || target.other.Editor.CanUndo() != otherUndo || target.other.Editor.CanRedo() != otherRedo {
				test.Fatal("tab history traversal consumed the other document's history")
			}
			expectRenderedCollisionSave(test, state, driver, target.document.Path, original)
			if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
				test.Fatal(err)
			}
			if target.document.Text() != changed || state.rich.input.Caret() != target.caret {
				test.Fatal("same-byte tab redo lost its own selection")
			}
		}
	}
}

func TestRenderedTabHistorySourceBranchLifecycle(test *testing.T) {
	for _, operation := range []string{"branch", "cancel", "empty-cancel", "noop", "history-cycle"} {
		for _, otherKind := range []string{"markdown", "plain"} {
			test.Run(fmt.Sprintf("%s/%s", operation, otherKind), func(context *testing.T) {
				state, driver, filename, original := renderedSourceCollisionFixture(context, "abcd\n\n## Target\n", false, 1200)
				clickID(context, driver, renderedRows.At(0))
				state.rich.input.MoveTo(2, false)
				if err := driver.Key(gfx.KeyEnter, 0); err != nil {
					context.Fatal(err)
				}
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
				tabHistoryRoundTrip(context, state, driver, otherKind, false)
				current := state.session.Current()
				if _, cached := state.renderedDocuments.entries[current.ID]; cached {
					context.Fatal("active document retained an unused cached copy of its history")
				}
				clickID(context, driver, editModeID)
				clickID(context, driver, sourceID)
				position := strings.Index(current.Text(), "Target") + len("Target")
				current.Editor.MoveTo(utf8.RuneCountInString(current.Text()[:position]), false)
				if operation == "branch" {
					if err := driver.Type("P"); err != nil {
						context.Fatal(err)
					}
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
				} else if operation == "noop" {
					current.Editor.MoveTo(0, false)
					if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
						context.Fatal(err)
					}
				} else if operation == "history-cycle" {
					for _, command := range []rune{'y', 'z'} {
						if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
							context.Fatal(err)
						}
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
					if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
						context.Fatal(err)
					}
					if _, composing := current.Editor.Composition(); !composing || current.Dirty() || driver.Snapshot().Focused != sourceID {
						context.Fatal("tab-restored Source preedit lost its owner or became dirty")
					}
					if _, err := state.onMarkdownSourceTextEvent(app.TextEvent{Kind: app.TextCancelComposition}); err != nil {
						context.Fatal(err)
					}
					if err := driver.Pump(); err != nil {
						context.Fatal(err)
					}
				}
				expectRenderedCollisionSave(context, state, driver, filename, original)
				for cycle := range 2 {
					tabHistoryRoundTrip(context, state, driver, otherKind, cycle == 1)
					if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
						context.Fatal(err)
					}
					body, row, caret := "cd", 1, 0
					wanted := strings.Replace(string(original[3:]), "abcd", "ab\r\n\r\ncd", 1)
					if operation == "branch" {
						body, row, caret = "TargetP", 1, 7
						wanted = strings.Replace(string(original[3:]), "Target", "TargetP", 1)
					}
					if state.mode != renderedMode || !state.rich.active || state.rich.index != row || state.rich.input.String() != body || state.rich.input.Caret() != caret || driver.Snapshot().Focused != richInputID {
						context.Fatal("tab cache resurrected a stale branch or discarded actual redo")
					}
					expectRenderedCollisionSave(context, state, driver, filename, append([]byte{0xef, 0xbb, 0xbf}, []byte(wanted)...))
					if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
						context.Fatal(err)
					}
					expectRenderedCollisionSave(context, state, driver, filename, original)
				}
			})
		}
	}
}

func TestRenderedTabHistoryRejectsReusedDocumentIdentity(test *testing.T) {
	state, driver, _, original := renderedSourceCollisionFixture(test, "abcd\n\n## Target\n", false, 1200)
	clickID(test, driver, renderedRows.At(0))
	state.rich.input.MoveTo(2, false)
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		test.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		test.Fatal(err)
	}
	previous := state.session.Current()
	other := filepath.Join(filepath.Dir(previous.Path), "other.md")
	if err := os.WriteFile(other, []byte("# Other\n"), 0640); err != nil {
		test.Fatal(err)
	}
	state.openFile(other)
	state.invalidator.Invalidate()
	if err := driver.Pump(); err != nil {
		test.Fatal(err)
	}
	if len(state.renderedDocuments.entries) != 1 {
		test.Fatal("reused identity fixture has no inactive history")
	}
	root := test.TempDir()
	filename := filepath.Join(root, "collision.md")
	if err := os.WriteFile(filename, original, 0640); err != nil {
		test.Fatal(err)
	}
	if changed, err := state.session.RequestWorkspace(root); err != nil || !changed {
		test.Fatal("reused identity fixture did not replace the workspace")
	}
	current, err := state.session.Open(filename)
	if err != nil || current.ID != previous.ID || current.Editor == previous.Editor {
		test.Fatal("reused identity fixture did not create a new Source editor with the same ID")
	}
	current.Editor.MoveTo(1, false)
	current.Editor.Insert("Q")
	if !current.Editor.Undo() {
		test.Fatal("reused identity fixture did not create its own redo")
	}
	state.invalidator.Invalidate()
	if err := driver.Pump(); err != nil {
		test.Fatal(err)
	}
	if len(state.renderedDocuments.entries) != 0 {
		test.Fatal("workspace replacement retained an old Source editor's cached history")
	}
	activateSpecialHistoryTarget(test, state, driver)
	if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
		test.Fatal(err)
	}
	if state.mode != renderedMode || !state.rich.active || state.rich.index != 0 || state.rich.input.String() != "aQbcd" || state.rich.input.Caret() != 2 {
		test.Fatal("same ID and source bytes resurrected another Source editor's structure redo")
	}
	wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.Replace(string(original[3:]), "abcd", "aQbcd", 1))...)
	expectRenderedCollisionSave(test, state, driver, filename, wanted)
}

func TestRenderedTabHistoryPrunesClosedDocuments(test *testing.T) {
	state, driver, filename, original := renderedSourceCollisionFixture(test, "abcd\n\n## Target\n", false, 1200)
	first, tab := state.session.Current(), state.session.Active
	clickID(test, driver, renderedRows.At(0))
	state.rich.input.MoveTo(2, false)
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		test.Fatal(err)
	}
	other := filepath.Join(filepath.Dir(filename), "other.md")
	if err := os.WriteFile(other, []byte("# Other\n"), 0640); err != nil {
		test.Fatal(err)
	}
	state.openFile(other)
	state.invalidator.Invalidate()
	if err := driver.Pump(); err != nil {
		test.Fatal(err)
	}
	clickID(test, driver, tabClose.At(tab))
	if state.session.Pending() == nil || len(state.renderedDocuments.entries) != 1 {
		test.Fatal("pending close discarded an open dirty document's history")
	}
	state.resolve(session.Cancel)
	state.invalidator.Invalidate()
	if err := driver.Pump(); err != nil {
		test.Fatal(err)
	}
	clickID(test, driver, tabRows.At(tab))
	activateSpecialHistoryTarget(test, state, driver)
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		test.Fatal(err)
	}
	if state.mode != renderedMode || !state.rich.active || state.rich.index != 0 || state.rich.input.Caret() != 2 {
		test.Fatal("cancelled close lost the dirty document's actual history target")
	}
	expectRenderedCollisionSave(test, state, driver, filename, original)
	state.openFile(other)
	state.invalidator.Invalidate()
	if err := driver.Pump(); err != nil {
		test.Fatal(err)
	}
	clickID(test, driver, tabClose.At(tab))
	if state.session.Pending() != nil || len(state.renderedDocuments.entries) != 0 {
		test.Fatal("closed tab retained history metadata")
	}
	if !state.openFile(filename) {
		test.Fatal("closed-document fixture did not reopen")
	}
	state.invalidator.Invalidate()
	if err := driver.Pump(); err != nil {
		test.Fatal(err)
	}
	if current := state.session.Current(); current == first || current.ID == first.ID || current.Editor.CanUndo() || current.Editor.CanRedo() || len(state.renderedDocuments.entries) != 0 {
		test.Fatal("reopened tab reused closed Source history")
	}
	if actual, err := os.ReadFile(filename); err != nil || !bytes.Equal(actual, original) {
		test.Fatal("tab lifecycle changed the original file")
	}
}

func TestRenderedTabNavigationKeepsComposingOwner(test *testing.T) {
	for _, editor := range []string{"rich", "table", "fence", "source"} {
		for _, operation := range []string{"tab", "active-tab", "reopen", "close", "close-other", "next", "previous"} {
			test.Run(editor+"/"+operation, func(context *testing.T) {
				state, driver, filename, original := renderedSourceCollisionFixture(context, "abcd\n\n"+specialHistoryPrefix("table")+specialHistoryPrefix("fence")+"## Target\n", false, 740)
				current, tab := state.session.Current(), state.session.Active
				other := filepath.Join(filepath.Dir(filename), "other.md")
				if err := os.WriteFile(other, []byte("# Other\n\nkeep\n"), 0640); err != nil {
					context.Fatal(err)
				}
				state.openFile(other)
				otherTab := state.session.Active
				for range 4 {
					state.newDocument()
				}
				if !state.openFile(filename) {
					context.Fatal("composition tab fixture did not return to its document")
				}
				state.invalidator.Invalidate()
				if err := driver.Pump(); err != nil {
					context.Fatal(err)
				}
				clickID(context, driver, renderedModeID)
				clickID(context, driver, renderedRows.At(0))
				state.rich.input.MoveTo(2, false)
				if err := driver.Key(gfx.KeyEnter, 0); err != nil {
					context.Fatal(err)
				}
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
				input, focus := state.rich.input, richInputID
				if editor == "table" || editor == "fence" {
					activateHistorySpecial(context, state, driver, editor)
					input, focus = specialHistoryInput(state, editor)
				} else if editor == "source" {
					clickID(context, driver, editModeID)
					clickID(context, driver, sourceID)
					input, focus = current.Editor, sourceID
				}
				if err := driver.Compose("ni"); err != nil {
					context.Fatal(err)
				}
				switch operation {
				case "tab":
					clickID(context, driver, tabRows.At(otherTab))
				case "active-tab":
					clickID(context, driver, tabRows.At(tab))
				case "reopen":
					state.openFile(other)
					state.invalidator.Invalidate()
					if err := driver.Pump(); err != nil {
						context.Fatal(err)
					}
				case "close":
					clickID(context, driver, tabClose.At(tab))
				case "close-other":
					clickID(context, driver, tabClose.At(otherTab))
				case "next":
					clickID(context, driver, tabNextID)
				case "previous":
					clickID(context, driver, tabPreviousID)
				}
				actualInput := state.rich.input
				if editor == "source" {
					actualInput = state.session.Current().Editor
				} else if editor == "table" || editor == "fence" {
					actualInput, _ = specialHistoryInput(state, editor)
				}
				if state.session.Current() != current || actualInput != input || state.session.Pending() != nil || current.Dirty() || driver.Snapshot().Focused != focus {
					context.Fatal("tab transition abandoned the composing document or native owner")
				}
				if _, composing := input.Composition(); !composing {
					context.Fatal("tab transition committed or cancelled unconfirmed marked text")
				}
				if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
					context.Fatal(err)
				}
				if actual, err := os.ReadFile(filename); err != nil || !bytes.Equal(actual, original) {
					context.Fatal("composing tab navigation or Save wrote preedit to disk")
				}
				var err error
				if editor == "source" {
					_, err = state.onMarkdownSourceTextEvent(app.TextEvent{Kind: app.TextCancelComposition})
				} else if editor == "rich" {
					_, err = state.onRenderedTextEvent(app.TextEvent{Kind: app.TextCancelComposition})
				} else {
					_, err = specialHistoryEvent(state, editor, app.TextEvent{Kind: app.TextCancelComposition})
				}
				if err != nil {
					context.Fatal(err)
				}
				if err := driver.Pump(); err != nil {
					context.Fatal(err)
				}
				tabHistoryRoundTrip(context, state, driver, "markdown", false)
				if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
					context.Fatal(err)
				}
				if state.mode != renderedMode || !state.rich.active || state.rich.index != 1 || state.rich.input.String() != "cd" || state.rich.input.Caret() != 0 {
					context.Fatal("cancelled tab navigation lost the prior structural redo")
				}
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
				expectRenderedCollisionSave(context, state, driver, filename, original)
			})
		}
	}
}

func TestRenderedTabHistoryKeepsSourceCapacity(test *testing.T) {
	for _, editor := range []string{"table", "fence"} {
		test.Run(editor, func(context *testing.T) {
			state, driver, filename, _ := renderedSourceCollisionFixture(context, "abcd\n\n"+specialHistoryPrefix(editor)+"## Target\n", false, 1200)
			clickID(context, driver, renderedRows.At(0))
			state.rich.input.MoveTo(2, false)
			if err := driver.Key(gfx.KeyEnter, 0); err != nil {
				context.Fatal(err)
			}
			current := state.session.Current()
			afterEnter := current.Text()
			activateHistorySpecial(context, state, driver, editor)
			input, focus := specialHistoryInput(state, editor)
			before := input.String()
			input.MoveTo(input.RuneCount(), false)
			for range 260 {
				if err := driver.Type("X"); err != nil {
					context.Fatal(err)
				}
			}
			afterTyping := current.Text()
			if state.rich.appliedEdits+state.rich.sourceHistoryOffset != 261 || len(state.rich.structureUndo) != 0 {
				context.Fatal("capacity fixture retained an out-of-range structural annotation")
			}
			for step := range 256 {
				if step%32 == 0 {
					tabHistoryRoundTrip(context, state, driver, "markdown", step%64 == 0)
				}
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
				input, _ = specialHistoryInput(state, editor)
				if state.mode != renderedMode || input == nil || state.rich.appliedEdits+state.rich.sourceHistoryOffset != 260-step || driver.Snapshot().Focused != focus || len(state.rich.structureUndo) != 0 {
					context.Fatal("tab traversal lost actual Source capacity or resurrected an old structure")
				}
			}
			if current.Editor.CanUndo() {
				context.Fatal("tab cache created extra Source Undo units")
			}
			remaining := strings.Replace(afterEnter, before, before+strings.Repeat("X", 4), 1)
			expectRenderedCollisionSave(context, state, driver, filename, append([]byte{0xef, 0xbb, 0xbf}, []byte(remaining)...))
			for step := range 256 {
				if step%32 == 0 {
					tabHistoryRoundTrip(context, state, driver, "markdown", step%64 == 0)
				}
				if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
					context.Fatal(err)
				}
				input, _ = specialHistoryInput(state, editor)
				if state.mode != renderedMode || input == nil || state.rich.appliedEdits+state.rich.sourceHistoryOffset != 6+step || driver.Snapshot().Focused != focus || len(state.rich.structureRedo) != 0 {
					context.Fatal("tab traversal lost actual Source Redo capacity")
				}
			}
			if current.Editor.CanRedo() || current.Text() != afterTyping {
				context.Fatal("tab cache did not replay exactly the retained Source units")
			}
			expectRenderedCollisionSave(context, state, driver, filename, append([]byte{0xef, 0xbb, 0xbf}, []byte(afterTyping)...))
		})
	}
}

func TestRenderedTabNavigationFlushesConfirmedInput(test *testing.T) {
	for _, editor := range []string{"rich", "table", "fence"} {
		for _, route := range []string{"tab", "reopen"} {
			test.Run(editor+"/"+route, func(context *testing.T) {
				state, driver, filename, original := renderedSourceCollisionFixture(context, "abcd\n\n"+specialHistoryPrefix("table")+specialHistoryPrefix("fence")+"## Target\n", false, 1200)
				current, tab := state.session.Current(), state.session.Active
				other := filepath.Join(filepath.Dir(filename), "other.md")
				if err := os.WriteFile(other, []byte("# Other\n\nkeep\n"), 0640); err != nil {
					context.Fatal(err)
				}
				state.openFile(other)
				otherTab := state.session.Active
				state.openFile(filename)
				state.invalidator.Invalidate()
				if err := driver.Pump(); err != nil {
					context.Fatal(err)
				}
				clickID(context, driver, renderedRows.At(0))
				input := state.rich.input
				if editor == "table" || editor == "fence" {
					activateHistorySpecial(context, state, driver, editor)
					input, _ = specialHistoryInput(state, editor)
				}
				before := input.String()
				input.MoveTo(input.RuneCount(), false)
				var err error
				if editor == "rich" {
					_, err = state.onRenderedTextEvent(app.TextEvent{Kind: app.TextInsert, Text: "X"})
				} else {
					_, err = specialHistoryEvent(state, editor, app.TextEvent{Kind: app.TextInsert, Text: "X"})
				}
				if err != nil || input.String() != before+"X" || current.Text() != string(original[3:]) {
					context.Fatal("late-input fixture did not leave a confirmed native edit awaiting paint")
				}
				wanted := strings.Replace(string(original[3:]), before, before+"X", 1)
				if route == "reopen" {
					state.openFile(other)
					state.invalidator.Invalidate()
					if err := driver.Pump(); err != nil {
						context.Fatal(err)
					}
				} else {
					clickID(context, driver, tabRows.At(otherTab))
				}
				if state.session.Current() == current || current.Text() != wanted || !current.Dirty() {
					context.Fatal("document navigation dropped a confirmed native edit before Source synchronization")
				}
				if actual, err := os.ReadFile(filename); err != nil || !bytes.Equal(actual, original) {
					context.Fatal("navigation implicitly saved a confirmed native edit")
				}
				clickID(context, driver, tabRows.At(tab))
				activateSpecialHistoryTarget(context, state, driver)
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
				expectRenderedCollisionSave(context, state, driver, filename, original)
				if current.Editor.CanUndo() {
					context.Fatal("navigation duplicated the synchronized native Source unit")
				}
			})
		}
	}
}

func TestRenderedTabHistoryPrunesConfirmedClose(test *testing.T) {
	for _, decision := range []session.Decision{session.Save, session.Discard} {
		for _, active := range []bool{false, true} {
			test.Run(fmt.Sprintf("decision-%d/active-%v", decision, active), func(context *testing.T) {
				state, driver, filename, original := renderedSourceCollisionFixture(context, "abcd\n\n## Target\n", false, 1200)
				current, tab := state.session.Current(), state.session.Active
				clickID(context, driver, renderedRows.At(0))
				state.rich.input.MoveTo(2, false)
				if err := driver.Key(gfx.KeyEnter, 0); err != nil {
					context.Fatal(err)
				}
				changed := append([]byte{0xef, 0xbb, 0xbf}, []byte(current.Text())...)
				other := filepath.Join(filepath.Dir(filename), "other.md")
				if err := os.WriteFile(other, []byte("# Other\n"), 0640); err != nil {
					context.Fatal(err)
				}
				state.openFile(other)
				state.invalidator.Invalidate()
				if err := driver.Pump(); err != nil {
					context.Fatal(err)
				}
				if active {
					clickID(context, driver, tabRows.At(tab))
				}
				clickID(context, driver, tabClose.At(tab))
				if state.session.Pending() == nil {
					context.Fatal("confirmed-close fixture has no unsaved confirmation")
				}
				state.resolve(decision)
				state.invalidator.Invalidate()
				if err := driver.Pump(); err != nil {
					context.Fatal(err)
				}
				if state.session.Pending() != nil || len(state.renderedDocuments.entries) != 0 {
					context.Fatal("confirmed close retained a closed document's annotations")
				}
				for _, document := range state.session.Tabs {
					if document == current {
						context.Fatal("confirmed close left its document open")
					}
				}
				wanted := original
				if decision == session.Save {
					wanted = changed
				}
				if actual, err := os.ReadFile(filename); err != nil || !bytes.Equal(actual, wanted) {
					context.Fatal("confirmed close changed its save/discard contract")
				}
			})
		}
	}
}

func TestRenderedTabHistoryRejectsChangedInactiveSource(test *testing.T) {
	for _, mutation := range []string{"content", "editor"} {
		test.Run(mutation, func(context *testing.T) {
			state, driver, filename, original := renderedSourceCollisionFixture(context, "abcd\n\n## Target\n", false, 1200)
			current, tab := state.session.Current(), state.session.Active
			clickID(context, driver, renderedRows.At(0))
			state.rich.input.MoveTo(2, false)
			if err := driver.Key(gfx.KeyEnter, 0); err != nil {
				context.Fatal(err)
			}
			if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
				context.Fatal(err)
			}
			other := filepath.Join(filepath.Dir(filename), "other.md")
			if err := os.WriteFile(other, []byte("# Other\n"), 0640); err != nil {
				context.Fatal(err)
			}
			state.openFile(other)
			state.invalidator.Invalidate()
			if err := driver.Pump(); err != nil {
				context.Fatal(err)
			}
			if mutation == "editor" {
				current.Editor = text.NewEditor(current.Text())
			}
			position := strings.Index(current.Text(), "Target") + len("Target")
			current.Editor.MoveTo(utf8.RuneCountInString(current.Text()[:position]), false)
			current.Editor.Insert("P")
			if mutation == "editor" && !current.Editor.Undo() {
				context.Fatal("new Source editor fixture has no actual redo")
			}
			clickID(context, driver, tabRows.At(tab))
			if len(state.rich.structureUndo) != 0 || len(state.rich.structureRedo) != 0 || len(state.renderedDocuments.entries) != 0 {
				context.Fatal("changed inactive Source retained old view history")
			}
			if mutation == "content" {
				clickID(context, driver, editModeID)
				clickID(context, driver, sourceID)
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					context.Fatal(err)
				}
				clickID(context, driver, renderedModeID)
			}
			activateSpecialHistoryTarget(context, state, driver)
			if _, err := driver.ShortcutChord(primaryModifier(), 'y'); err != nil {
				context.Fatal(err)
			}
			if state.mode != renderedMode || !state.rich.active || state.rich.index != 1 || state.rich.input.String() != "TargetP" || state.rich.input.Caret() != 7 {
				context.Fatal("stale cached structure intercepted the changed Source editor's actual redo")
			}
			wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.Replace(string(original[3:]), "Target", "TargetP", 1))...)
			expectRenderedCollisionSave(context, state, driver, filename, wanted)
		})
	}
}
