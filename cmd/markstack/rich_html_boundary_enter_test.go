package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestRichHTMLBoundaryEnterKeepsRightLeafStyleAndHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "boundary.mh")
	source := "<p>before <strong>bold</strong> after</p>\n<!--keep-->"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, htmlLeafRows.At(0))
	if err := driver.Key(gfx.KeyEnd, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	split := "<p>before </p>\n<p><strong>bold</strong> after</p>\n<!--keep-->"
	if state.session.Current().Text() != split || state.html.blockIndex != 1 || state.html.leafIndex != 0 || state.html.input.Caret() != 0 {
		t.Fatalf("boundary Enter=%q block=%d leaf=%d", state.session.Current().Text(), state.html.blockIndex, state.html.leafIndex)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	want := "<p>before </p>\n<p><strong>新bold</strong> after</p>\n<!--keep-->"
	if state.session.Current().Text() != want {
		t.Fatalf("right leaf style lost: %q", state.session.Current().Text())
	}
	for _, expected := range []string{split, source} {
		if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil || state.session.Current().Text() != expected {
			t.Fatalf("boundary Undo=%q, %v, want %q", state.session.Current().Text(), err, expected)
		}
	}
	if state.html.leafIndex != 0 || state.html.input.Caret() != len("before ") {
		t.Fatalf("undo did not restore source leaf caret: leaf=%d caret=%d", state.html.leafIndex, state.html.input.Caret())
	}
	for _, expected := range []string{split, want} {
		if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil || state.session.Current().Text() != expected {
			t.Fatalf("boundary Redo=%q, %v, want %q", state.session.Current().Text(), err, expected)
		}
	}
	if err := state.session.Save(state.session.Active, ""); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != want {
		t.Fatalf("boundary Save=%q, %v", data, err)
	}
}

func TestRichHTMLBoundaryAfterBoldAndAtStyledEnds(t *testing.T) {
	for _, test := range []struct {
		source, want string
		leaf         int
		key          gfx.Key
	}{
		{`<p>before <strong>bold</strong> after</p>`, "<p>before <strong>bold</strong></p>\n<p> after</p>", 1, gfx.KeyEnd},
		{`<p><strong>bold</strong></p>`, "<p><br></p>\n<p><strong>bold</strong></p>", 0, gfx.KeyHome},
		{`<p><strong>bold</strong></p>`, "<p><strong>bold</strong></p>\n<p><br></p>", 0, gfx.KeyEnd},
	} {
		t.Run(test.want, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "ends.mh")
			if err := os.WriteFile(path, []byte(test.source), 0644); err != nil {
				t.Fatal(err)
			}
			state, driver := testDriver(t)
			state.openFile(path)
			if err := driver.Resize(1200, 780); err != nil {
				t.Fatal(err)
			}
			clickID(t, driver, htmlLeafRows.At(test.leaf))
			if err := driver.Key(test.key, 0); err != nil {
				t.Fatal(err)
			}
			if err := driver.Key(gfx.KeyEnter, 0); err != nil {
				t.Fatal(err)
			}
			if state.session.Current().Text() != test.want || state.mode != renderedMode || !state.html.active {
				t.Fatalf("styled boundary Enter=%q, want %q; message=%q", state.session.Current().Text(), test.want, state.message)
			}
		})
	}
}
