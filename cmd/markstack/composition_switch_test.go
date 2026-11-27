package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

// uncommittedCompositionSource exercises every native editor the block and mode
// switches can clear: a paragraph, a second paragraph, a fenced code block and a
// simple table.
const uncommittedCompositionSource = "# First\n\nParagraph\n\n```go\ncode\n```\n\n| A | B |\n| --- | --- |\n| 1 | 2 |\n\nSecond\n"

// compositionDriver opens a source in rendered mode with the outline visible.
func compositionDriver(t *testing.T, source string) (*model, *app.Driver) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "composition-switch.md")
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	state.outline = true
	state.mode = renderedMode
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	return state, driver
}

// codeCompositionSource has an editable fenced block second, so rendered row 1
// activates the code editor.
const codeCompositionSource = "# First\n\n```go\ncode\n```\n\nSecond\n"

// compositionEditor names the editor that holds the marked text, and reports
// whether it survived a switch attempt.
func compositionEditor(state *model, owner string) *text.Editor {
	switch owner {
	case "rich":
		return state.rich.input
	case "code":
		return state.codeBlock.input
	case "table":
		return state.table.input
	}
	return nil
}

// requireMarked asserts the marked text is still in owner's editor and the
// document was not committed or dirtied by the refused switch.
func requireMarked(t *testing.T, state *model, driver *app.Driver, owner string, focus gfx.ID, want string) {
	t.Helper()
	editor := compositionEditor(state, owner)
	if editor == nil {
		t.Fatalf("%s editor was cleared while composing", owner)
	}
	if _, composing := editor.Composition(); !composing {
		t.Fatalf("%s composition was discarded by the switch", owner)
	}
	if got := state.session.Current().Text(); got != want {
		t.Fatalf("the refused switch committed preedit: %q", got)
	}
	if state.session.Current().Dirty() {
		t.Fatal("the refused switch dirtied the document")
	}
	if focus != (gfx.ID{}) {
		if got := driver.Snapshot().Focused; got != focus {
			t.Fatalf("focus moved to %v, want %v", got, focus)
		}
	}
}

// TestPendingCompositionSurvivesBlockAndModeSwitches pins the cross-editor
// contract from AGENTS.md: uncommitted composition text owns the keyboard, so a
// block or mode switch must keep its editor and focus instead of clearing it.
func TestPendingCompositionSurvivesBlockAndModeSwitches(t *testing.T) {
	t.Run("rendered row click", func(t *testing.T) {
		state, driver := compositionDriver(t, uncommittedCompositionSource)
		clickID(t, driver, renderedRows.At(1))
		if err := driver.Compose("ni"); err != nil {
			t.Fatal(err)
		}
		clickID(t, driver, renderedRows.At(2))
		requireMarked(t, state, driver, "rich", richInputID, uncommittedCompositionSource)
	})
	t.Run("table cell click", func(t *testing.T) {
		state, driver := compositionDriver(t, uncommittedCompositionSource)
		clickID(t, driver, renderedRows.At(1))
		if err := driver.Compose("ni"); err != nil {
			t.Fatal(err)
		}
		clickID(t, driver, renderedTableCells.At(2))
		if state.table.active {
			t.Fatal("a table cell took over while a composition was pending")
		}
		requireMarked(t, state, driver, "rich", richInputID, uncommittedCompositionSource)
	})
	t.Run("block row click", func(t *testing.T) {
		state, driver := compositionDriver(t, uncommittedCompositionSource)
		clickID(t, driver, renderedRows.At(1))
		if err := driver.Compose("ni"); err != nil {
			t.Fatal(err)
		}
		clickID(t, driver, renderedRows.At(2))
		requireMarked(t, state, driver, "rich", richInputID, uncommittedCompositionSource)
	})
	t.Run("source mode shortcut", func(t *testing.T) {
		state, driver := compositionDriver(t, codeCompositionSource)
		clickID(t, driver, renderedRows.At(1))
		if !state.codeBlock.active {
			t.Fatalf("code block did not activate: %+v", state.codeBlock)
		}
		if err := driver.Compose("ni"); err != nil {
			t.Fatal(err)
		}
		if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierAlt, '1'); err != nil {
			t.Fatal(err)
		}
		if state.mode != renderedMode {
			t.Fatalf("source-view shortcut left rendered mode while composing: %v", state.mode)
		}
		requireMarked(t, state, driver, "code", codeInputID, codeCompositionSource)
	})
	t.Run("mode button", func(t *testing.T) {
		state, driver := compositionDriver(t, uncommittedCompositionSource)
		clickID(t, driver, renderedTableCells.At(2))
		if !state.table.active {
			t.Fatalf("table cell did not activate: %+v", state.table)
		}
		if err := driver.Compose("ni"); err != nil {
			t.Fatal(err)
		}
		clickID(t, driver, editModeID)
		if state.mode != renderedMode {
			t.Fatalf("the source button left rendered mode while composing: %v", state.mode)
		}
		requireMarked(t, state, driver, "table", tableInputID, uncommittedCompositionSource)
		if err := driver.Type("你"); err != nil {
			t.Fatalf("typing after the refused mode button: %v", err)
		}
	})
	t.Run("table cell enter commits composition", func(t *testing.T) {
		state, driver := compositionDriver(t, uncommittedCompositionSource)
		clickID(t, driver, renderedTableCells.At(2))
		if !state.table.active {
			t.Fatalf("table cell did not activate: %+v", state.table)
		}
		if err := driver.Compose("ni"); err != nil {
			t.Fatal(err)
		}
		// Latest Viva hands Return to the input method while it composes, as a
		// native window does: the candidate is committed instead of inserting a
		// newline or switching view. The commit must reach the source interval
		// and stay undoable rather than being discarded with the preedit.
		if err := driver.Key(gfx.KeyEnter, 0); err != nil {
			t.Fatal(err)
		}
		if state.mode != renderedMode || !state.table.active {
			t.Fatalf("cell Enter switched away after the commit: mode=%v table=%v", state.mode, state.table.active)
		}
		if _, composing := state.table.input.Composition(); composing {
			t.Fatal("cell Enter left the composition marked")
		}
		want := strings.Replace(uncommittedCompositionSource, "| 1 | 2 |", "| 1ni | 2 |", 1)
		if got := state.session.Current().Text(); got != want {
			t.Fatalf("cell Enter did not commit the candidate in place: %q", got)
		}
		if !state.session.Current().Dirty() {
			t.Fatal("committed composition did not mark the document dirty")
		}
		if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
			t.Fatal(err)
		}
		if got := state.session.Current().Text(); got != uncommittedCompositionSource {
			t.Fatalf("undo after the commit did not restore the cell: %q", got)
		}
		if state.session.Current().Dirty() {
			t.Fatal("undo after the commit left the document dirty")
		}
	})
	t.Run("find escape without match", func(t *testing.T) {
		state, driver := compositionDriver(t, uncommittedCompositionSource)
		clickID(t, driver, renderedRows.At(1))
		if err := driver.Compose("ni"); err != nil {
			t.Fatal(err)
		}
		clickID(t, driver, findDocumentID)
		if err := driver.Type("zzz"); err != nil {
			t.Fatal(err)
		}
		if err := driver.Key(gfx.KeyEscape, 0); err != nil {
			t.Fatal(err)
		}
		requireMarked(t, state, driver, "rich", richInputID, uncommittedCompositionSource)
	})
	t.Run("find complex match fallback", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "composition-styled.md")
		source := "**Styled** paragraph\n\n```go\ncode\n```\n"
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
		state, driver := testDriver(t)
		state.openFile(path)
		state.mode = renderedMode
		if err := driver.Resize(1201, 780); err != nil {
			t.Fatal(err)
		}
		clickID(t, driver, renderedRows.At(1))
		if !state.codeBlock.active {
			t.Fatalf("code block did not activate: %+v", state.codeBlock)
		}
		if err := driver.Compose("ni"); err != nil {
			t.Fatal(err)
		}
		clickID(t, driver, findDocumentID)
		if err := driver.Type("Styled"); err != nil {
			t.Fatal(err)
		}
		if err := driver.Key(gfx.KeyEnter, 0); err != nil {
			t.Fatal(err)
		}
		if state.mode != renderedMode {
			t.Fatalf("a complex find match switched to source mode while composing: %v", state.mode)
		}
		if state.codeBlock.input == nil {
			t.Fatal("the complex find match cleared the composing code editor")
		}
		if _, composing := state.codeBlock.input.Composition(); !composing {
			t.Fatal("the complex find match discarded the pending composition")
		}
	})
	t.Run("find close button", func(t *testing.T) {
		state, driver := compositionDriver(t, codeCompositionSource)
		clickID(t, driver, renderedRows.At(1))
		if !state.codeBlock.active {
			t.Fatalf("code block did not activate: %+v", state.codeBlock)
		}
		if err := driver.Compose("ni"); err != nil {
			t.Fatal(err)
		}
		clickID(t, driver, findDocumentID)
		clickID(t, driver, findCloseID)
		if state.findOpen {
			t.Fatal("the find close button left the bar open")
		}
		requireMarked(t, state, driver, "code", codeInputID, codeCompositionSource)
	})
	t.Run("outline distant heading", func(t *testing.T) {
		source := "# First\n\n```go\ncode\n```\n\n" + strings.Repeat("段落内容。\n\n", 45) + "# Last\n"
		state, driver := compositionDriver(t, source)
		clickID(t, driver, renderedRows.At(1))
		if !state.codeBlock.active {
			t.Fatalf("code block did not activate: %+v", state.codeBlock)
		}
		if err := driver.Compose("ni"); err != nil {
			t.Fatal(err)
		}
		before := state.previewScroll
		clickID(t, driver, outlineRows.At(1))
		if state.previewScroll != before {
			t.Fatalf("the refused outline jump scrolled away from the composing editor: %v -> %v", before, state.previewScroll)
		}
		requireMarked(t, state, driver, "code", codeInputID, source)
	})
	t.Run("deferred complex find refreshes", func(t *testing.T) {
		source := "**Styled** paragraph\n\n```go\ncode\n```\n"
		state, driver := compositionDriver(t, source)
		clickID(t, driver, renderedRows.At(1))
		if !state.codeBlock.active {
			t.Fatalf("code block did not activate: %+v", state.codeBlock)
		}
		if err := driver.Compose("ni"); err != nil {
			t.Fatal(err)
		}
		clickID(t, driver, findDocumentID)
		if err := driver.Type("Styled"); err != nil {
			t.Fatal(err)
		}
		if err := driver.Key(gfx.KeyEnter, 0); err != nil {
			t.Fatal(err)
		}
		if !state.findDeferred {
			t.Fatal("the complex match was not deferred while composing")
		}
		// The headless driver can commit a composition but cannot cancel one, so
		// end it by dropping the editor that owned it, as an IME cancellation
		// does: the deferred answer must then be recomputed instead of staying
		// frozen at its incomplete result set.
		clickID(t, driver, findCloseID)
		state.codeBlock = codeBlockEditor{}
		if state.pendingCompositionFocus() != (gfx.ID{}) {
			t.Fatalf("the composition did not end: %v", state.pendingCompositionFocus())
		}
		if got := state.session.Current().Text(); got != source {
			t.Fatalf("ending the composition changed the source: %q", got)
		}
		clickID(t, driver, findDocumentID)
		state.syncDocumentFind()
		driver.Snapshot()
		if state.findDeferred {
			t.Fatal("the deferred flag survived a finished composition")
		}
		if len(state.findMatches) == 0 {
			t.Fatal("the deferred complex match never produced a full answer")
		}
	})
	t.Run("deferred find keeps the viewport", func(t *testing.T) {
		// A plain match far below is collected before the complex match later in
		// the document defers the source fallback, so positioning would scroll the
		// composing editor out of the viewport without the guard.
		source := "# First\n\n```go\ncode\n```\n\n" + strings.Repeat("段落内容。\n\n", 45) +
			"plain target\n\n" + strings.Repeat("其余段落。\n\n", 5) + "**styled** target\n"
		state, driver := compositionDriver(t, source)
		clickID(t, driver, renderedRows.At(1))
		if !state.codeBlock.active {
			t.Fatalf("code block did not activate: %+v", state.codeBlock)
		}
		if err := driver.Compose("ni"); err != nil {
			t.Fatal(err)
		}
		before := state.previewScroll
		clickID(t, driver, findDocumentID)
		if err := driver.Type("target"); err != nil {
			t.Fatal(err)
		}
		if err := driver.Key(gfx.KeyEnter, 0); err != nil {
			t.Fatal(err)
		}
		if !state.findDeferred || len(state.findVisual) == 0 {
			t.Fatalf("the fixture did not reach the deferred positioning branch: deferred=%v visual=%d", state.findDeferred, len(state.findVisual))
		}
		if state.previewScroll != before {
			t.Fatalf("the deferred find scrolled the composing editor away: %v -> %v", before, state.previewScroll)
		}
		clickID(t, driver, findCloseID)
		requireMarked(t, state, driver, "code", codeInputID, source)
	})
	t.Run("source mode composition survives find", func(t *testing.T) {
		for _, name := range []string{"note.md", "main.go"} {
			t.Run(name, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), name)
				source := "package main\n\n// Needle\n"
				if name == "note.md" {
					source = "# 标题\n\nNeedle 段落\n"
				}
				if err := os.WriteFile(path, []byte(source), 0644); err != nil {
					t.Fatal(err)
				}
				state, driver := testDriver(t)
				state.openFile(path)
				state.mode = editMode
				if err := driver.Resize(1201, 780); err != nil {
					t.Fatal(err)
				}
				document := state.session.Current()
				document.Editor.SetSelection(text.Range{Start: 0, End: 0})
				if err := driver.Compose("ni"); err != nil {
					t.Fatal(err)
				}
				if !document.Composing() {
					t.Fatal("the fixture did not start a source composition")
				}
				clickID(t, driver, findDocumentID)
				if err := driver.Type("Needle"); err != nil {
					t.Fatal(err)
				}
				if err := driver.Key(gfx.KeyEnter, 0); err != nil {
					t.Fatal(err)
				}
				if !document.Composing() {
					t.Fatal("find positioning cancelled the source composition")
				}
				if document.Dirty() {
					t.Fatal("find positioning committed the source composition")
				}
				// The replace bar shows its own close button; it must restore the
				// composing editor's focus exactly like Escape does.
				clickID(t, driver, findCloseID)
				if state.findOpen {
					t.Fatal("the close button left the find bar open")
				}
				if got := driver.Snapshot().Focused; got != sourceID {
					t.Fatalf("focus after the close button = %v, want the source editor", got)
				}
				if !document.Composing() || document.Dirty() {
					t.Fatalf("the close button committed the source composition: composing=%v dirty=%v", document.Composing(), document.Dirty())
				}
			})
		}
	})
	t.Run("outline unmatched heading fallback", func(t *testing.T) {
		state, driver := compositionDriver(t, uncommittedCompositionSource)
		clickID(t, driver, renderedRows.At(1))
		if err := driver.Compose("ni"); err != nil {
			t.Fatal(err)
		}
		// A heading that only exists in the rendered projection cannot be
		// matched back to source, so the outline normally falls back to source.
		state.outlines[0].Title = "No such heading"
		clickID(t, driver, outlineRows.At(0))
		if state.mode != renderedMode {
			t.Fatalf("the outline fallback switched to source mode while composing: %v", state.mode)
		}
		requireMarked(t, state, driver, "rich", gfx.ID{}, uncommittedCompositionSource)
	})
}
