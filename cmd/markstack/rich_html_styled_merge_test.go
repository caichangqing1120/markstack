package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestRichHTMLStyledBackspaceMergesAndUndoesInOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "merge-styled.mh")
	source := "<p>before <strong>bo</strong></p>\n<p><strong>ld</strong> after</p>\n<!--keep-->"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, htmlLeafRows.At(2))
	if err := driver.Key(gfx.KeyHome, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
		t.Fatal(err)
	}
	merged := "<p>before <strong>bold</strong> after</p>\n<!--keep-->"
	if got := state.session.Current().Text(); got != merged || !state.html.active || state.html.blockIndex != 0 || state.html.leafIndex != 1 || state.html.input.Caret() != 2 {
		t.Fatalf("styled Backspace=%q block=%d leaf=%d message=%q", got, state.html.blockIndex, state.html.leafIndex, state.message)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	want := "<p>before <strong>bo新ld</strong> after</p>\n<!--keep-->"
	if state.session.Current().Text() != want {
		t.Fatalf("styled merge continuation=%q", state.session.Current().Text())
	}
	for _, expected := range []string{merged, source} {
		if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil || state.session.Current().Text() != expected {
			t.Fatalf("styled merge Undo=%q, %v, want %q", state.session.Current().Text(), err, expected)
		}
	}
	for _, expected := range []string{merged, want} {
		if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil || state.session.Current().Text() != expected {
			t.Fatalf("styled merge Redo=%q, %v, want %q", state.session.Current().Text(), err, expected)
		}
	}
	if err := state.session.Save(state.session.Active, ""); err != nil {
		t.Fatal(err)
	}
	if stored, err := os.ReadFile(path); err != nil || string(stored) != want {
		t.Fatalf("styled merge saved=%q, %v", stored, err)
	}
}

func TestRichHTMLStyledBackspaceRefusesUnsafeGapAndRightAttributes(t *testing.T) {
	for _, source := range []string{
		"<p>before</p>\n<!--keep-->\n<p><strong>after</strong></p>",
		"<p>before</p>\n<p data-id='keep'><strong>after</strong></p>",
	} {
		t.Run(source, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "unsafe.mh")
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
			if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
				t.Fatal(err)
			}
			if state.session.Current().Text() != source || state.session.Current().Dirty() || state.message == "" {
				t.Fatalf("unsafe Backspace changed source=%q message=%q", state.session.Current().Text(), state.message)
			}
		})
	}
}

func TestRichHTMLStyledEnterThenBackspaceRestoresCleanSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roundtrip.mh")
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
	if state.session.Current().Text() != split {
		t.Fatalf("styled split=%q", state.session.Current().Text())
	}
	if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != source || state.session.Current().Dirty() {
		t.Fatalf("Enter then Backspace left dirty source=%q", state.session.Current().Text())
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil || state.session.Current().Text() != split {
		t.Fatalf("merge Undo=%q, %v", state.session.Current().Text(), err)
	}
	if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil || state.session.Current().Text() != source {
		t.Fatalf("merge Redo=%q, %v", state.session.Current().Text(), err)
	}
}
