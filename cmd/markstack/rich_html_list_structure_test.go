package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestRichHTMLOrderedListEnterTypingUndoRedoKeepsNumbers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ordered.mh")
	source := `<ol start="9"><li>AlphaBeta</li><li>Tail</li></ol>` + "\n<!--keep-->"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, htmlLeafRows.At(0))
	if err := driver.Key(gfx.KeyHome, 0); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if err := driver.Key(gfx.KeyRight, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	split := `<ol start="9"><li>Alpha</li>` + "\n" + `<li>Beta</li><li>Tail</li></ol>` + "\n<!--keep-->"
	if state.session.Current().Text() != split || state.html.blockIndex != 1 || state.html.input.Caret() != 0 || len(state.html.parsed.Blocks) != 3 || state.html.parsed.Blocks[2].Number != 11 {
		t.Fatalf("ordered Enter=%q block=%d message=%q", state.session.Current().Text(), state.html.blockIndex, state.message)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	want := `<ol start="9"><li>Alpha</li>` + "\n" + `<li>新Beta</li><li>Tail</li></ol>` + "\n<!--keep-->"
	if state.session.Current().Text() != want {
		t.Fatalf("ordered continuation=%q", state.session.Current().Text())
	}
	for _, expected := range []string{split, source} {
		if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil || state.session.Current().Text() != expected {
			t.Fatalf("ordered Undo=%q, %v, want %q", state.session.Current().Text(), err, expected)
		}
	}
	for _, expected := range []string{split, want} {
		if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil || state.session.Current().Text() != expected {
			t.Fatalf("ordered Redo=%q, %v, want %q", state.session.Current().Text(), err, expected)
		}
	}
	if err := state.session.Save(state.session.Active, ""); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != want {
		t.Fatalf("ordered Save=%q, %v", data, err)
	}
}

func TestRichHTMLStyledListEnterBackspaceRestoresCleanSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "styled-list.mh")
	source := `<ul><li>before <strong>bold</strong></li></ul>`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, htmlLeafRows.At(1))
	if err := driver.Key(gfx.KeyHome, 0); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := driver.Key(gfx.KeyRight, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	split := "<ul><li>before <strong>bo</strong></li>\n<li><strong>ld</strong></li></ul>"
	if state.session.Current().Text() != split || state.html.blockIndex != 1 || state.html.leafIndex != 0 {
		t.Fatalf("styled list Enter=%q, message=%q", state.session.Current().Text(), state.message)
	}
	if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != source || state.session.Current().Dirty() {
		t.Fatalf("styled list Enter→Backspace=%q dirty=%v", state.session.Current().Text(), state.session.Current().Dirty())
	}
}
