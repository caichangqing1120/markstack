package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

func TestRichHTMLStyledEnterThenTypingHasExactSourceHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "styled.mh")
	source := "<p>before <strong>bold</strong> after</p>\n<!--keep-->"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
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
	split := "<p>before <strong>bo</strong></p>\n<p><strong>ld</strong> after</p>\n<!--keep-->"
	if got := state.session.Current().Text(); got != split || !state.html.active || state.html.blockIndex != 1 || state.html.leafIndex != 0 || state.html.input.Caret() != 0 {
		t.Fatalf("styled Enter state=%q, block=%d leaf=%d message=%q", got, state.html.blockIndex, state.html.leafIndex, state.message)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	want := "<p>before <strong>bo</strong></p>\n<p><strong>新ld</strong> after</p>\n<!--keep-->"
	if state.session.Current().Text() != want {
		t.Fatalf("styled continuation=%q", state.session.Current().Text())
	}
	for _, expected := range []string{split, source} {
		if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil || state.session.Current().Text() != expected {
			t.Fatalf("styled Undo=%q, %v, want %q", state.session.Current().Text(), err, expected)
		}
	}
	if !state.html.active || state.html.leafIndex != 1 || state.html.input.Caret() != 2 {
		t.Fatalf("styled undo focus leaf=%d caret=%d", state.html.leafIndex, state.html.input.Caret())
	}
	for _, expected := range []string{split, want} {
		if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil || state.session.Current().Text() != expected {
			t.Fatalf("styled Redo=%q, %v, want %q", state.session.Current().Text(), err, expected)
		}
	}
	if err := state.session.Save(state.session.Active, ""); err != nil {
		t.Fatal(err)
	}
	if stored, err := os.ReadFile(path); err != nil || string(stored) != want {
		t.Fatalf("styled Save=%q, %v", stored, err)
	}
}

func TestRichHTMLStyledSplitDoesNotOverlapAdjacentLeaf(t *testing.T) {
	path := filepath.Join(t.TempDir(), "styled-split.mh")
	if err := os.WriteFile(path, []byte("<p>before <strong>bo</strong></p>\n<p><strong>ld</strong> after</p>"), 0644); err != nil {
		t.Fatal(err)
	}
	state := newModel()
	var canvas gfx.Canvas
	session, err := headless.Open(newConfig(state), &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	state.openFile(path)
	if err := session.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	for _, problem := range headless.CheckLayout(session.Snapshot()) {
		if problem.Kind == headless.LayoutOverlap || problem.Kind == headless.LayoutSpill {
			t.Fatalf("styled split layout=%+v", problem)
		}
	}
}
