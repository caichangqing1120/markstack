package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"

	"markstack/internal/richedit"
)

func TestRichHTMLEmptyOrderedListEnterExitsThenTypesAndUndoes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "list-exit.mh")
	source := `<ol start="9"><li>First</li><li><br></li></ol>` + "\r\n<!--keep-->"
	original := append([]byte{0xef, 0xbb, 0xbf}, []byte(source)...)
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, htmlLeafRows.At(1))
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	exited := `<ol start="9"><li>First</li></ol>` + "\r\n<p><br></p>\r\n<!--keep-->"
	if state.session.Current().Text() != exited || state.mode != renderedMode || !state.html.active || state.html.blockIndex != 1 || state.html.parsed.Blocks[1].Kind != richedit.Paragraph || state.html.input.Caret() != 0 {
		t.Fatalf("empty list Enter=%q mode=%v block=%d message=%q", state.session.Current().Text(), state.mode, state.html.blockIndex, state.message)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	want := `<ol start="9"><li>First</li></ol>` + "\r\n<p>新</p>\r\n<!--keep-->"
	if state.session.Current().Text() != want {
		t.Fatalf("after list continuation=%q", state.session.Current().Text())
	}
	for _, expected := range []string{exited, source} {
		if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil || state.session.Current().Text() != expected {
			t.Fatalf("list exit Undo=%q, %v; want %q", state.session.Current().Text(), err, expected)
		}
	}
	for _, expected := range []string{exited, want} {
		if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil || state.session.Current().Text() != expected {
			t.Fatalf("list exit Redo=%q, %v; want %q", state.session.Current().Text(), err, expected)
		}
	}
	if err := state.session.Save(state.session.Active, ""); err != nil {
		t.Fatal(err)
	}
	if stored, err := os.ReadFile(path); err != nil || !bytes.Equal(stored, append([]byte{0xef, 0xbb, 0xbf}, []byte(want)...)) {
		t.Fatalf("list exit Save=%q, %v", stored, err)
	}
}

func TestRichHTMLOnlyEmptyListExitsButUnsafeGapRefuses(t *testing.T) {
	for _, test := range []struct {
		source, want string
		index        int
		refuse       bool
	}{
		{`<ul><li><br></li></ul>`, `<p><br></p>`, 0, false},
		{`<ul><li>one</li><!--keep--><li><br></li></ul>`, "", 1, true},
	} {
		t.Run(test.source, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "empty.mh")
			if err := os.WriteFile(path, []byte(test.source), 0644); err != nil {
				t.Fatal(err)
			}
			state, driver := testDriver(t)
			state.openFile(path)
			if err := driver.Resize(740, 520); err != nil {
				t.Fatal(err)
			}
			clickID(t, driver, htmlLeafRows.At(test.index))
			if err := driver.Key(gfx.KeyEnter, 0); err != nil {
				t.Fatal(err)
			}
			if test.refuse {
				if state.session.Current().Text() != test.source || state.session.Current().Dirty() || state.message == "" {
					t.Fatalf("middle empty list changed source=%q message=%q", state.session.Current().Text(), state.message)
				}
			} else if state.session.Current().Text() != test.want || state.html.parsed.Blocks[0].Kind != richedit.Paragraph {
				t.Fatalf("only empty list exit=%q, message=%q", state.session.Current().Text(), state.message)
			}
		})
	}
}

func TestRichHTMLMiddleEmptyOrderedListExitsWithOriginalNextNumber(t *testing.T) {
	path := filepath.Join(t.TempDir(), "middle.mh")
	source := `<ol start="9"><li>one</li><li><br></li><li>three</li></ol>`
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, htmlLeafRows.At(1))
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	exited := "<ol start=\"9\"><li>one</li></ol>\n<p><br></p>\n<ol start=\"11\"><li>three</li></ol>"
	if state.session.Current().Text() != exited || state.html.blockIndex != 1 || state.html.parsed.Blocks[2].Number != 11 {
		t.Fatalf("middle ordered exit=%q number=%d message=%q", state.session.Current().Text(), state.html.parsed.Blocks[2].Number, state.message)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	want := "<ol start=\"9\"><li>one</li></ol>\n<p>新</p>\n<ol start=\"11\"><li>three</li></ol>"
	if state.session.Current().Text() != want {
		t.Fatalf("middle ordered continuation=%q", state.session.Current().Text())
	}
	for _, expected := range []string{exited, source} {
		if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil || state.session.Current().Text() != expected {
			t.Fatalf("middle ordered Undo=%q, %v; want %q", state.session.Current().Text(), err, expected)
		}
	}
	for _, expected := range []string{exited, want} {
		if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil || state.session.Current().Text() != expected {
			t.Fatalf("middle ordered Redo=%q, %v; want %q", state.session.Current().Text(), err, expected)
		}
	}
}

func TestRichHTMLListExitBackspaceRestoresOriginalSourceAndHistory(t *testing.T) {
	for _, test := range []struct {
		source string
		index  int
	}{
		{`<ol start="9"><li>First</li><li><br></li></ol>`, 1},
		{`<ol start="9"><li>one</li><li><br></li><li>three</li></ol>`, 1},
		{`<ul><li><br></li></ul>`, 0},
	} {
		t.Run(test.source, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "roundtrip.mh")
			if err := os.WriteFile(path, []byte(test.source), 0644); err != nil {
				t.Fatal(err)
			}
			state, driver := testDriver(t)
			state.openFile(path)
			if err := driver.Resize(740, 520); err != nil {
				t.Fatal(err)
			}
			clickID(t, driver, htmlLeafRows.At(test.index))
			if err := driver.Key(gfx.KeyEnter, 0); err != nil {
				t.Fatal(err)
			}
			exited := state.session.Current().Text()
			if exited == test.source || state.html.parsed.Blocks[test.index].Kind != richedit.Paragraph {
				t.Fatalf("list did not exit: %q", exited)
			}
			if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
				t.Fatal(err)
			}
			if state.session.Current().Text() != test.source || state.session.Current().Dirty() || state.html.parsed.Blocks[test.index].Kind != richedit.ListItem {
				t.Fatalf("list Backspace did not restore clean source: %q", state.session.Current().Text())
			}
			if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil || state.session.Current().Text() != exited {
				t.Fatalf("Backspace Undo=%q, %v", state.session.Current().Text(), err)
			}
			if _, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil || state.session.Current().Text() != test.source {
				t.Fatalf("Backspace Redo=%q, %v", state.session.Current().Text(), err)
			}
		})
	}
}
