package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestDocumentFindSelectsAndCyclesMatchesWithoutEditing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "find.md")
	source := "# 标题\n\n正文一。正文二。\n"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, findDocumentID)
	if driver.Snapshot().Focused != findInputID {
		t.Fatalf("find field focus = %v", driver.Snapshot().Focused)
	}
	if err := driver.Type("正文"); err != nil {
		t.Fatal(err)
	}
	if state.mode != editMode || len(state.findMatches) != 2 || state.findIndex != 0 {
		t.Fatalf("find results = %v, index=%d mode=%v", state.findMatches, state.findIndex, state.mode)
	}
	first := strings.Index(source, "正文")
	if caret := state.session.Current().Editor.Selection().Start; caret != len([]rune(source[:first])) {
		t.Fatalf("first match at rune %d, want %d", caret, len([]rune(source[:first])))
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if state.findIndex != 1 {
		t.Fatalf("next match index = %d", state.findIndex)
	}
	if err := driver.Key(gfx.KeyEnter, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if state.findIndex != 0 {
		t.Fatalf("previous match index = %d", state.findIndex)
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if state.findOpen || state.session.Current().Text() != source || state.session.Current().Dirty() {
		t.Fatal("closing find changed document")
	}
}

func TestDocumentFindShortcutKeepsFilenameFindSeparate(t *testing.T) {
	state, driver := testDriver(t)
	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierAlt, 'f'); err != nil {
		t.Fatal(err)
	}
	if !state.findOpen || driver.Snapshot().Focused != findInputID {
		t.Fatalf("document find not focused: open=%v focus=%v", state.findOpen, driver.Snapshot().Focused)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'f'); err != nil {
		t.Fatal(err)
	}
	if driver.Snapshot().Focused != searchID {
		t.Fatal("existing filename filter shortcut changed")
	}
}

func TestDocumentFindFitsMinimumWindowAndResetsOnTabSwitch(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# First\n\nFirst body")
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, findDocumentID)
	if err := driver.Type("First"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []gfx.ID{findInputID, findPreviousID, findNextID, findCloseID} {
		found := false
		for _, hit := range driver.Snapshot().Hits {
			if hit.ID == id {
				found = true
				if hit.Bounds.X < 0 || hit.Bounds.X+hit.Bounds.Width > 740 {
					t.Fatalf("find control outside narrow window: %+v", hit)
				}
			}
		}
		if !found {
			t.Fatalf("find control %v missing", id)
		}
	}
	clickID(t, driver, newFileID)
	if state.findOpen || len(state.findMatches) != 0 {
		t.Fatal("new tab retained another document's matches")
	}
}

func TestRenderedDocumentFindHighlightsWithoutSwitchingViews(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rendered-find.md")
	if err := os.WriteFile(path, []byte("# 标题\n\n正文和正文\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, findDocumentID)
	if err := driver.Type("正文"); err != nil {
		t.Fatal(err)
	}
	if state.mode != renderedMode || len(state.findMatches) != 2 {
		t.Fatalf("rendered find mode=%v matches=%v", state.mode, state.findMatches)
	}
	highlights := 0
	for _, primitive := range driver.Snapshot().Primitives {
		if primitive.Bounds.Y > state.previewRect.Y+40 && primitive.Bounds.Y < state.previewRect.Y+180 && primitive.Color.Alpha > 0.1 && primitive.Color.Alpha < 0.5 {
			highlights++
		}
	}
	if highlights < 2 {
		t.Fatalf("rendered matches have %d visible highlights", highlights)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if state.findIndex != 1 || state.mode != renderedMode || state.session.Current().Dirty() {
		t.Fatalf("next match index=%d mode=%v dirty=%v", state.findIndex, state.mode, state.session.Current().Dirty())
	}
}

func TestRenderedFindFallsBackForFormattedMatch(t *testing.T) {
	state, driver := testDriver(t)
	source := "# Title\n\n**bold** text\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, findDocumentID)
	if err := driver.Type("bold"); err != nil {
		t.Fatal(err)
	}
	if state.mode != editMode || len(state.findMatches) != 1 {
		t.Fatalf("formatted match mode=%v matches=%v", state.mode, state.findMatches)
	}
	if caret := state.session.Current().Editor.Selection().Start; caret != strings.Index(source, "bold") {
		t.Fatalf("source fallback selection=%d", caret)
	}
}

func TestRenderedFindScrollsDistantMatchIntoView(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace(strings.Repeat("Paragraph\n\n", 45) + "Needle\n")
	clickID(t, driver, renderedModeID)
	clickID(t, driver, findDocumentID)
	if err := driver.Type("Needle"); err != nil {
		t.Fatal(err)
	}
	if state.mode != renderedMode || state.previewScroll <= 0 {
		t.Fatalf("distant rendered match not scrolled: mode=%v scroll=%v", state.mode, state.previewScroll)
	}
}

func TestRenderedExampleFindKeepsSimpleMatchesInPlace(t *testing.T) {
	source, err := os.ReadFile("../../examples/Welcome.md")
	if err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.session.Current().Replace(string(source))
	clickID(t, driver, renderedModeID)
	clickID(t, driver, findDocumentID)
	if err := driver.Type("Markdown"); err != nil {
		t.Fatal(err)
	}
	if state.mode != renderedMode || len(state.findMatches) != 2 {
		t.Fatalf("example rendered search mode=%v matches=%v", state.mode, state.findMatches)
	}
}

func TestRenderedFindJumpKeepsTypingAtMatchedBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "find-type.md")
	source := "# Title\r\n\r\n" + strings.Repeat("段落内容。\r\n\r\n", 30) + "Needle body\r\n"
	original := append([]byte{0xef, 0xbb, 0xbf}, []byte(source)...)
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, findDocumentID)
	if err := driver.Type("Needle"); err != nil {
		t.Fatal(err)
	}
	if len(state.findMatches) != 1 || state.mode != renderedMode || state.previewScroll <= 0 {
		t.Fatalf("rendered find mode=%v matches=%d scroll=%v", state.mode, len(state.findMatches), state.previewScroll)
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if state.findOpen {
		t.Fatal("Escape left find open")
	}
	if err := driver.Type("新"); err != nil {
		t.Fatalf("typing after a rendered find jump: %v", err)
	}
	want := strings.Replace(source, "Needle body", "新 body", 1)
	if got := state.session.Current().Text(); got != want || state.mode != renderedMode || !state.session.Current().Dirty() {
		t.Fatalf("find jump then typing mode=%v source=%q, want %q", state.mode, got, want)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, append([]byte{0xef, 0xbb, 0xbf}, []byte(want)...)) {
		t.Fatalf("find-jump typing changed BOM/CRLF bytes: %q, %v", saved, err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, original) {
		t.Fatalf("find-jump Undo/Save did not restore the original: %q, %v", saved, err)
	}
}

func TestRenderedFindCloseSelectsChosenUnicodeOccurrence(t *testing.T) {
	for _, closeButton := range []bool{false, true} {
		name := "escape"
		if closeButton {
			name = "close button"
		}
		t.Run(name, func(t *testing.T) {
			source := "# Heading\r\n\r\n前 Needle 中 Needle 后\r\n"
			state, driver := compositionDriver(t, source)
			clickID(t, driver, findDocumentID)
			if err := driver.Type("Needle"); err != nil {
				t.Fatal(err)
			}
			if err := driver.Key(gfx.KeyEnter, 0); err != nil {
				t.Fatal(err)
			}
			if state.findIndex != 1 {
				t.Fatalf("selected match=%d, want 1", state.findIndex)
			}
			if closeButton {
				clickID(t, driver, findCloseID)
			} else if err := driver.Key(gfx.KeyEscape, 0); err != nil {
				t.Fatal(err)
			}
			if !state.rich.active || driver.Snapshot().Focused != richInputID {
				t.Fatal("closing find did not focus rendered input")
			}
			if got := state.rich.input.Selection(); got != (text.Range{Start: 11, End: 17}) {
				t.Fatalf("selected occurrence=%+v, want [11,17)", got)
			}
			if err := driver.Type("新"); err != nil {
				t.Fatal(err)
			}
			if got := state.session.Current().Text(); got != "# Heading\r\n\r\n前 Needle 中 新 后\r\n" {
				t.Fatalf("typing changed the wrong occurrence: %q", got)
			}
		})
	}
}

func TestRenderedFindReplaysOrdinaryPositionAfterCompositionCancellation(t *testing.T) {
	source := "# First\n\n```go\ncode\n```\n\n" + strings.Repeat("filler\n\n", 45) + "Needle one\n\nNeedle two\n"
	state, driver := compositionDriver(t, source)
	clickID(t, driver, renderedRows.At(1))
	if err := driver.Compose("ni"); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, findDocumentID)
	if err := driver.Type("Needle"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if state.findIndex != 1 || state.previewScroll != 0 {
		t.Fatalf("pending composition moved search: index=%d scroll=%v", state.findIndex, state.previewScroll)
	}
	if !state.codeBlock.input.Undo() {
		t.Fatal("could not cancel the native editor composition")
	}
	state.invalidator.Invalidate()
	if err := driver.Pump(); err != nil {
		t.Fatal(err)
	}
	if state.pendingCompositionFocus() != (gfx.ID{}) || state.session.Current().Text() != source || state.session.Current().Dirty() {
		t.Fatal("composition cancellation changed the committed document")
	}
	if state.findIndex != 1 || state.previewScroll <= 0 {
		t.Fatalf("ordinary find did not replay selected position: index=%d scroll=%v", state.findIndex, state.previewScroll)
	}
	clickID(t, driver, findCloseID)
	if err := driver.Type("新"); err != nil {
		t.Fatalf("typing after canceled composition and find: %v", err)
	}
	if got := state.session.Current().Text(); got != strings.Replace(source, "Needle two", "新 two", 1) {
		t.Fatalf("search resumed at the wrong match: %q", got)
	}
}

func TestDocumentFindWithoutMatchRestoresActiveNativeEditor(t *testing.T) {
	filler := strings.Repeat("filler\n\n", 45)
	for _, testcase := range []struct {
		name, extension, source string
		target, focus           gfx.ID
	}{
		{"paragraph", ".md", "# First\n\nBody\n\n" + filler + "Needle\n", renderedRows.At(1), richInputID},
		{"fence", ".md", "# First\n\n```go\ncode\n```\n\n" + filler + "Needle\n", renderedRows.At(1), codeInputID},
		{"table", ".md", "| A | B |\n| --- | --- |\n| 1 | 2 |\n\n" + filler + "Needle\n", renderedTableCells.At(2), tableInputID},
		{"rich html", ".mh", "<p>Body</p>" + strings.Repeat("<p>filler</p>", 45) + "<p>Needle</p>", htmlLeafRows.At(0), htmlInputID},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "find"+testcase.extension)
			if err := os.WriteFile(path, []byte(testcase.source), 0644); err != nil {
				t.Fatal(err)
			}
			state, driver := testDriver(t)
			state.openFile(path)
			state.mode = renderedMode
			if err := driver.Resize(1201, 780); err != nil {
				t.Fatal(err)
			}
			clickID(t, driver, testcase.target)
			for _, query := range []string{"", "absent"} {
				clickID(t, driver, findDocumentID)
				if err := driver.Type(query); err != nil {
					t.Fatal(err)
				}
				clickID(t, driver, findCloseID)
				if got := driver.Snapshot().Focused; got != testcase.focus {
					t.Fatalf("query %q restored focus=%v, want %v", query, got, testcase.focus)
				}
			}
			clickID(t, driver, findDocumentID)
			if err := driver.SelectText(0, state.findEditor.RuneCount()); err != nil {
				t.Fatal(err)
			}
			if err := driver.Type("Needle"); err != nil {
				t.Fatal(err)
			}
			if state.previewScroll <= 0 && state.html.scroll <= 0 {
				t.Fatal("distant search did not move the viewport away from the active editor")
			}
			if err := driver.SelectText(0, state.findEditor.RuneCount()); err != nil {
				t.Fatal(err)
			}
			if err := driver.Type("absent"); err != nil {
				t.Fatal(err)
			}
			clickID(t, driver, findCloseID)
			if got := driver.Snapshot().Focused; got != testcase.focus {
				t.Fatalf("unmatched find left the original editor offscreen: focus=%v, want %v", got, testcase.focus)
			}
			if err := driver.Type("新"); err != nil {
				t.Fatalf("typing after unmatched find: %v", err)
			}
			if !strings.Contains(state.session.Current().Text(), "新") || !state.session.Current().Dirty() {
				t.Fatal("unmatched find dropped the next character")
			}
		})
	}
}
