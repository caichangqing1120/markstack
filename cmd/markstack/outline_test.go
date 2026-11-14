package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
)

func TestRenderedOutlineScrollsWithoutLeavingCurrentView(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# First\n\n" + strings.Repeat("Paragraph\n\n", 45) + "## Target\n")
	state.outline = true
	state.mode = renderedMode
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, outlineRows.At(1))
	if state.mode != renderedMode || state.previewScroll <= 0 {
		t.Fatalf("outline left rendered view: mode=%v scroll=%v", state.mode, state.previewScroll)
	}
}

func TestReadOutlineScrollsWithoutOpeningSourceEditor(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# First\n\n" + strings.Repeat("Paragraph\n\n", 45) + "## Target\n")
	state.outline = true
	state.mode = readMode
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, outlineRows.At(1))
	if state.mode != readMode || state.previewScroll <= 0 {
		t.Fatalf("outline left read view: mode=%v scroll=%v", state.mode, state.previewScroll)
	}
}

func TestRenderedOutlineDistinguishesRepeatedHeadings(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Repeat\n\n" + strings.Repeat("Paragraph\n\n", 40) + "# Repeat\n")
	state.outline = true
	state.mode = renderedMode
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, outlineRows.At(1))
	if state.mode != renderedMode || state.previewScroll <= 0 {
		t.Fatalf("second repeated heading jumped to first: mode=%v scroll=%v", state.mode, state.previewScroll)
	}
}

func TestRenderedOutlineJumpKeepsTypingInTargetHeading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outline-type.md")
	source := "# First\r\n\r\n" + strings.Repeat("段落内容。\r\n\r\n", 45) + "## Target\r\n"
	original := append([]byte{0xef, 0xbb, 0xbf}, []byte(source)...)
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	state.outline = true
	state.mode = renderedMode
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, outlineRows.At(1))
	if state.mode != renderedMode || state.previewScroll <= 0 {
		t.Fatalf("outline left rendered view: mode=%v scroll=%v", state.mode, state.previewScroll)
	}
	if driver.Snapshot().Focused != richInputID {
		t.Fatalf("outline activation left focus on %v", driver.Snapshot().Focused)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatalf("typing after a rendered outline jump: %v", err)
	}
	want := strings.Replace(source, "## Target\r\n", "## Target新\r\n", 1)
	if got := state.session.Current().Text(); got != want || state.mode != renderedMode || !state.session.Current().Dirty() {
		t.Fatalf("outline jump then typing mode=%v source=%q, want %q", state.mode, got, want)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, append([]byte{0xef, 0xbb, 0xbf}, []byte(want)...)) {
		t.Fatalf("outline-jump typing changed BOM/CRLF bytes: %q, %v", saved, err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, original) {
		t.Fatalf("outline-jump Undo/Save did not restore the original: %q, %v", saved, err)
	}
	if got := state.session.Current().Text(); got != source || state.session.Current().Dirty() {
		t.Fatalf("outline-jump Undo=%q dirty=%v", got, state.session.Current().Dirty())
	}
}

// An outline jump changes the active block, so it must not drop a pending
// composition in any native editor: marked text stays where it was typed and
// the next keystroke may not land in the target heading.
func TestRenderedOutlineJumpKeepsPendingEditorComposition(t *testing.T) {
	source := "# First\n\n```go\nbody\n```\n\n| A | B |\n| --- | --- |\n| 1 | 2 |\n\n# Second\n"
	setup := func(t *testing.T) (*model, *app.Driver) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "outline-compose.md")
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
	t.Run("code block", func(t *testing.T) {
		state, driver := setup(t)
		clickID(t, driver, renderedRows.At(1))
		if !state.codeBlock.active {
			t.Fatalf("code block did not activate: %+v", state.codeBlock)
		}
		if err := driver.Compose("ni"); err != nil {
			t.Fatal(err)
		}
		before := state.session.Current().Text()
		clickID(t, driver, outlineRows.At(1))
		if state.codeBlock.input == nil {
			t.Fatal("the outline jump cleared the code editor while composing")
		}
		if _, composing := state.codeBlock.input.Composition(); !composing {
			t.Fatal("the outline jump discarded the pending code composition")
		}
		if state.session.Current().Text() != before || state.session.Current().Dirty() {
			t.Fatalf("the outline jump committed preedit into source: %q", state.session.Current().Text())
		}
		if err := driver.Type("你"); err != nil {
			t.Fatal(err)
		}
		if got := state.codeBlock.input.String(); got != "body你" {
			t.Fatalf("typing after the refused jump left the code block: %q", got)
		}
		if strings.Contains(state.session.Current().Text(), "Second你") {
			t.Fatalf("the next keystroke landed in the heading: %q", state.session.Current().Text())
		}
	})
	t.Run("table cell", func(t *testing.T) {
		state, driver := setup(t)
		clickID(t, driver, renderedTableCells.At(2))
		if !state.table.active || state.table.input == nil {
			t.Fatalf("table cell did not activate: %+v", state.table)
		}
		if err := driver.Compose("ni"); err != nil {
			t.Fatal(err)
		}
		before := state.session.Current().Text()
		clickID(t, driver, outlineRows.At(1))
		if state.table.input == nil {
			t.Fatal("the outline jump cleared the table editor while composing")
		}
		if _, composing := state.table.input.Composition(); !composing {
			t.Fatal("the outline jump discarded the pending cell composition")
		}
		if state.session.Current().Text() != before || state.session.Current().Dirty() {
			t.Fatalf("the outline jump committed cell preedit into source: %q", state.session.Current().Text())
		}
		if err := driver.Type("你"); err != nil {
			t.Fatal(err)
		}
		if got := state.table.input.String(); !strings.Contains(got, "你") || strings.HasPrefix(got, "#") {
			t.Fatalf("typing after the refused jump left the table cell: %q", got)
		}
		if strings.Contains(state.session.Current().Text(), "Second你") {
			t.Fatalf("the next keystroke landed in the heading: %q", state.session.Current().Text())
		}
	})
}
