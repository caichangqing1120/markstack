package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

func TestRenderedStyledPreeditPreservesReaderRunAndSource(t *testing.T) {
	engine := app.PlatformTextEngine()
	if engine == nil {
		t.Skip("platform text engine unavailable")
	}
	for _, test := range []struct {
		name, markup, text, prefix, suffix string
	}{
		{"bold", "**boldword**", "boldword", "前 ", " tail"},
		{"italic", "*italicword*", "italicword", "前 ", " tail"},
		{"strike", "~~strikeword~~", "strikeword", "前 ", " tail"},
		{"link", "[linklabel](https://example.invalid/preserved)", "linklabel", "前 ", " tail"},
		{"whole-bold", "**boldword**", "boldword", "", ""},
		{"whole-italic", "*italicword*", "italicword", "", ""},
		{"whole-strike", "~~strikeword~~", "strikeword", "", ""},
		{"whole-link", "[linklabel](https://example.invalid/preserved)", "linklabel", "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "Mixed.md")
			source := test.prefix + test.markup + test.suffix + "\r\n\r\nTail\r\n<!--preserve-->\r\n"
			original := []byte("\ufeff" + source)
			if err := os.WriteFile(path, original, 0644); err != nil {
				t.Fatal(err)
			}
			state := newModel()
			state.openFile(path)
			var canvas gfx.Canvas
			canvas.SetTextEngine(engine)
			driver, err := app.NewDriver(newConfig(state), &canvas)
			if err != nil {
				t.Fatal(err)
			}
			defer driver.Close()
			if err := driver.Resize(740, 520); err != nil {
				t.Fatal(err)
			}
			clickID(t, driver, renderedModeID)
			var reading gfx.TextStyle
			for _, label := range driver.Snapshot().Texts {
				if label.Value == test.text {
					reading = label.Style
				}
			}
			if reading.Color.Alpha == 0 {
				t.Fatal("reader style missing")
			}
			clickID(t, driver, renderedRows.At(0))
			start := utf8.RuneCountInString(test.prefix) + 2
			if err := driver.SelectText(start, start+2); err != nil {
				t.Fatal(err)
			}
			if err := driver.Compose("pin"); err != nil {
				t.Fatal(err)
			}
			pending := test.text[:2] + "pin" + test.text[4:]
			found := false
			for _, label := range driver.Snapshot().Texts {
				if label.Value != pending || label.Style.Color.Alpha == 0 {
					continue
				}
				found = true
				if label.Style.Size != reading.Size || label.Style.Weight != reading.Weight || label.Style.Italic != reading.Italic || label.Style.Color != reading.Color {
					t.Errorf("preedit changed reader style: read=%+v pending=%+v", reading, label.Style)
				}
			}
			if !found {
				t.Errorf("styled preedit run %q was flattened or hidden", pending)
			}
			if state.session.Current().Text() != source || state.session.Current().Dirty() {
				t.Fatal("styled preedit changed committed source")
			}
			if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
				t.Fatal(err)
			}
			if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, original) {
				t.Fatalf("styled preedit reached disk: %q, %v", saved, err)
			}
			if err := driver.Type("新"); err != nil {
				t.Fatal(err)
			}
			replacement := test.text[:2] + "新" + test.text[4:]
			want := strings.Replace(source, test.markup, strings.Replace(test.markup, test.text, replacement, 1), 1)
			if state.session.Current().Text() != want {
				t.Fatalf("styled commit changed markup or neighbors: %q", state.session.Current().Text())
			}
			if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
				t.Fatal(err)
			}
			if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, []byte("\ufeff"+want)) {
				t.Fatalf("styled Save changed BOM/CRLF: %q, %v", saved, err)
			}
			if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
				t.Fatal(err)
			}
			if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
				t.Fatal(err)
			}
			if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, original) {
				t.Fatalf("styled Undo/Save did not restore original: %q, %v", saved, err)
			}
		})
	}
}

func TestRenderedCrossStylePreeditMatchesSafeCommitWithoutFlatteningNeighbors(t *testing.T) {
	engine := app.PlatformTextEngine()
	if engine == nil {
		t.Skip("platform text engine unavailable")
	}
	for _, test := range []struct {
		name, markup, from, to, pending, want, kept, style string
	}{
		{"bold-italic", "前 **bold** 中 *italicword* [keep](https://example.invalid/unchanged)", "ld", "italic", "pin", "前 **bo**新*word* [keep](https://example.invalid/unchanged)", "word", "italic"},
		{"bold-strike", "前 **bold** 中 ~~strikeword~~ [keep](https://example.invalid/unchanged)", "ld", "strike", "pin", "前 **bo**新~~word~~ [keep](https://example.invalid/unchanged)", "word", "strike"},
		{"unicode", "前 **加粗文字** 中 *斜体文字* [keep](https://example.invalid/unchanged)", "文字", "斜体", strings.Repeat("候选", 30), "前 **加粗**新*文字* [keep](https://example.invalid/unchanged)", "文字", "italic"},
	} {
		t.Run(test.name, func(t *testing.T) {
			prefix := strings.Repeat("排版", 12) + " "
			source := "# Title\r\n\r\n" + prefix + test.markup + "\r\n\r\nTail\r\n<!--keep-->\r\n"
			path := filepath.Join(t.TempDir(), "CrossPreedit.md")
			original := append([]byte{0xef, 0xbb, 0xbf}, []byte(source)...)
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			state := newModel()
			state.openFile(path)
			var canvas gfx.Canvas
			canvas.SetTextEngine(engine)
			driver, err := app.NewDriver(newConfig(state), &canvas)
			if err != nil {
				t.Fatal(err)
			}
			defer driver.Close()
			if err := driver.Resize(740, 780); err != nil {
				t.Fatal(err)
			}
			clickID(t, driver, renderedModeID)
			clickID(t, driver, renderedRows.At(1))
			visible := state.rich.input.String()
			from := strings.Index(visible, test.from)
			to := strings.Index(visible, test.to) + len(test.to)
			if err := driver.SelectText(utf8.RuneCountInString(visible[:from]), utf8.RuneCountInString(visible[:to])); err != nil {
				t.Fatal(err)
			}
			for _, pending := range []string{"p", test.pending} {
				if err := driver.Compose(pending); err != nil {
					t.Fatal(err)
				}
				if !state.rich.mixed || renderedPlain(&state.parsed, 1) != state.rich.input.String() {
					t.Errorf("cross-style preedit flattened the reader or lost text: mixed=%v parsed=%q editor=%q", state.rich.mixed, renderedPlain(&state.parsed, 1), state.rich.input.String())
				}
				if state.session.Current().Text() != source || state.session.Current().Dirty() {
					t.Fatal("cross-style preedit reached committed source")
				}
				kept, linked := false, false
				for _, label := range driver.Snapshot().Texts {
					if label.Value == test.kept && (test.style == "strike" || label.Style.Italic) {
						kept = true
					}
					if label.Value == "keep" {
						linked = label.Style.Color == referenceMarkdownColors(state.dark).link
					}
				}
				if !kept || !linked || state.preview.LinkCount() != 1 || state.preview.Link(0) != "https://example.invalid/unchanged" {
					t.Errorf("preedit lost unselected inline style/link: kept=%v linked=%v links=%d", kept, linked, state.preview.LinkCount())
				}
				underlines := 0
				palette := referenceMarkdownColors(state.dark).palette
				for _, rectangle := range canvas.Rectangles() {
					if rectangle.Color == palette.ink && rectangle.Bounds.Height == 1 && rectangle.Bounds.Width > 1 && rectangle.Bounds.Y >= state.rich.rect.Y && rectangle.Bounds.Y < state.rich.rect.Y+state.rich.rect.Height {
						underlines++
					}
				}
				if underlines == 0 || len(pending) > 100 && underlines < 2 {
					t.Errorf("cross-style preedit lost its shaped marked underline: %d", underlines)
				}
				for _, label := range canvas.Texts() {
					if label.Value == "" || label.Style.Color.Alpha <= 0 || !label.HasClip || label.ClipOf != state.previewRect || label.Bounds.Y < state.rich.rect.Y || label.Bounds.Y >= state.rich.rect.Y+state.rich.rect.Height {
						continue
					}
					if label.Bounds.X < state.rich.rect.X-0.01 || label.Bounds.X+canvas.TextWidthStyle(label.Value, label.Style) > state.rich.rect.X+state.rich.rect.Width+0.01 {
						t.Errorf("cross-style native glyph exceeded input bounds: %q at %+v, input=%+v", label.Value, label.Bounds, state.rich.rect)
					}
				}
				if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
					t.Fatal(err)
				}
				if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, original) {
					t.Fatalf("cross-style marked text reached disk: %q, %v", saved, err)
				}
			}
			if err := driver.Type("新"); err != nil {
				t.Fatal(err)
			}
			want := "# Title\r\n\r\n" + prefix + test.want + "\r\n\r\nTail\r\n<!--keep-->\r\n"
			if got := state.session.Current().Text(); got != want {
				t.Fatalf("commit differed from safe preedit: %q, want %q", got, want)
			}
			if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
				t.Fatal(err)
			}
			if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, append([]byte{0xef, 0xbb, 0xbf}, []byte(want)...)) {
				t.Fatalf("cross-style commit changed bytes: %q, %v", saved, err)
			}
			if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
				t.Fatal(err)
			}
			if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
				t.Fatal(err)
			}
			if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, original) {
				t.Fatalf("cross-style Undo/Save did not restore original: %q, %v", saved, err)
			}
		})
	}
}

func TestRenderedCrossStyleProjectionPreservesOtherBlockSourceAnchors(t *testing.T) {
	state, driver := testDriver(t)
	source := "# Top\n\nbefore **bold** after *italicword*\n\n- [x] Task\n\n| A | B |\n| --- | --- |\n| one | two |\n\n```go\nkeep()\n```\n\n<!--unknown-->\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	starts, ends := make([]int, len(state.parsed.Blocks)), make([]int, len(state.parsed.Blocks))
	for index, block := range state.parsed.Blocks {
		starts[index], ends[index] = block.Start, block.End
	}
	if err := driver.SelectText(9, 24); err != nil {
		t.Fatal(err)
	}
	for _, marked := range []string{"pin", strings.Repeat("候选", 16), "p"} {
		if err := driver.Compose(marked); err != nil {
			t.Fatal(err)
		}
		if !state.rich.mixed || len(state.parsed.Blocks) != len(starts) {
			t.Fatalf("projection changed block identity: mixed=%v blocks=%d", state.rich.mixed, len(state.parsed.Blocks))
		}
		for index, block := range state.parsed.Blocks {
			if block.Start != starts[index] || block.End != ends[index] {
				t.Errorf("preedit changed original block %d source anchor: %d/%d, want %d/%d", index, block.Start, block.End, starts[index], ends[index])
			}
		}
		if renderedPlain(&state.parsed, 2) != "Task" || renderedPlain(&state.parsed, 3) != "A\tB\none\ttwo" || renderedPlain(&state.parsed, 4) != "keep()" || renderedPlain(&state.parsed, 5) != "<!--unknown-->" {
			t.Fatal("preedit changed neighboring task/table/code/unknown presentation")
		}
		if state.session.Current().Text() != source {
			t.Fatal("preedit changed the source authority")
		}
	}
}

func TestRenderedCrossStylePreeditUndoAndUnmarkPreserveSourceHistory(t *testing.T) {
	for _, finish := range []string{"undo", "unmark"} {
		t.Run(finish, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "CrossStyle.md")
			source := "前 **bold** 中 *italicword* [keep](https://example.invalid/preserved)\r\n\r\nTail\r\n<!--keep-->\r\n"
			original := append([]byte{0xef, 0xbb, 0xbf}, []byte(source)...)
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			state, driver := testDriver(t)
			state.openFile(path)
			clickID(t, driver, renderedModeID)
			clickID(t, driver, renderedRows.At(0))
			if err := driver.SelectText(4, 15); err != nil {
				t.Fatal(err)
			}
			if err := driver.Compose("pin"); err != nil {
				t.Fatal(err)
			}
			if finish == "undo" {
				if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
					t.Fatal(err)
				}
				if state.session.Current().Text() != source || state.session.Current().Dirty() || renderedPlain(&state.parsed, 0) != "前 bold 中 italicword keep" {
					t.Fatal("Undo left the preedit in the source or reader")
				}
				if _, composing := state.rich.input.Composition(); composing {
					t.Fatal("Undo kept the discarded composition active")
				}
				if err := driver.SelectText(3, 3); err != nil {
					t.Fatal(err)
				}
				if err := driver.Type("新"); err != nil {
					t.Fatal(err)
				}
				if got := state.session.Current().Text(); got != "前 **b新old** 中 *italicword* [keep](https://example.invalid/preserved)\r\n\r\nTail\r\n<!--keep-->\r\n" {
					t.Fatalf("typing after canceled preedit changed markup: %q", got)
				}
			} else {
				if err := driver.Compose(""); err != nil {
					t.Fatal(err)
				}
				if got := state.session.Current().Text(); got != "前 **bo**pin*word* [keep](https://example.invalid/preserved)\r\n\r\nTail\r\n<!--keep-->\r\n" {
					t.Fatalf("Unmark did not commit the projected replacement: %q", got)
				}
			}
			if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
				t.Fatal(err)
			}
			if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
				t.Fatal(err)
			}
			if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
				t.Fatal(err)
			}
			if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, original) {
				t.Fatalf("Undo/Save after %s did not restore BOM/CRLF and source: %q, %v", finish, saved, err)
			}
		})
	}
}

func TestRenderedCrossStyleCommitKeepsOriginalSelectionWhenTextSharesEdges(t *testing.T) {
	for _, finish := range []string{"insert", "unmark", "paste", "same-text", "same-text-unmark", "same-text-paste"} {
		t.Run(finish, func(t *testing.T) {
			source := "前 **bold** 中 *italicword* [keep](https://example.invalid/preserved)\r\n\r\nTail\r\n<!--keep-->\r\n"
			path := filepath.Join(t.TempDir(), "SharedEdges.md")
			original := append([]byte{0xef, 0xbb, 0xbf}, []byte(source)...)
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			state, driver := testDriver(t)
			state.openFile(path)
			clickID(t, driver, renderedModeID)
			clickID(t, driver, renderedRows.At(0))
			if err := driver.SelectText(4, 15); err != nil {
				t.Fatal(err)
			}
			candidate, want := "ldXitalic", "前 **bo**ldXitalic*word* [keep](https://example.invalid/preserved)\r\n\r\nTail\r\n<!--keep-->\r\n"
			if strings.HasPrefix(finish, "same-text") {
				candidate, want = "ld 中 italic", "前 **bo**ld 中 italic*word* [keep](https://example.invalid/preserved)\r\n\r\nTail\r\n<!--keep-->\r\n"
			}
			if finish != "paste" && finish != "same-text-paste" {
				if err := driver.Compose(candidate); err != nil {
					t.Fatal(err)
				}
				spans := state.parsed.Spans(&state.parsed.Blocks[0])
				if len(spans) < 4 || spans[1].Text != "bo" || spans[2].Text != candidate || spans[3].Text != "word" || state.session.Current().Text() != source {
					t.Fatalf("projection did not retain the selected source edges: %+v", spans)
				}
			}
			switch finish {
			case "insert", "same-text":
				if err := driver.Type(candidate); err != nil {
					t.Fatal(err)
				}
			case "unmark", "same-text-unmark":
				if err := driver.Compose(""); err != nil {
					t.Fatal(err)
				}
			case "paste", "same-text-paste":
				driver.SetClipboard(candidate)
				if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
					t.Fatal(err)
				}
			}
			if got := state.session.Current().Text(); got != want {
				t.Fatalf("commit changed the formatting shown in preedit: %q, want %q", got, want)
			}
			for _, step := range []struct {
				key  rune
				mods gfx.Modifiers
				want string
			}{
				{'s', primaryModifier(), want},
				{'z', primaryModifier(), source},
				{'z', primaryModifier() | gfx.ModifierShift, want},
				{'z', primaryModifier(), source},
				{'s', primaryModifier(), source},
			} {
				if _, err := driver.ShortcutChord(step.mods, step.key); err != nil {
					t.Fatal(err)
				}
				if got := state.session.Current().Text(); got != step.want {
					t.Fatalf("key %v+%c changed cross-style history: %q, want %q", step.mods, step.key, got, step.want)
				}
			}
			if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, original) {
				t.Fatalf("shared-edge Undo/Save did not restore BOM/CRLF: %q, %v", saved, err)
			}
		})
	}
}

func TestRenderedCrossStyleIdenticalTextKeepsEarlierAndLaterHistory(t *testing.T) {
	source := "前 **bold** 中 *italicword* [keep](https://example.invalid/preserved)\r\n\r\nTail\r\n<!--keep-->\r\n"
	path := filepath.Join(t.TempDir(), "History.md")
	original := append([]byte{0xef, 0xbb, 0xbf}, []byte(source)...)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.SelectText(1, 1); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	before := "前X **bold** 中 *italicword* [keep](https://example.invalid/preserved)\r\n\r\nTail\r\n<!--keep-->\r\n"
	if err := driver.SelectText(5, 16); err != nil {
		t.Fatal(err)
	}
	if err := driver.Compose("ld 中 italic"); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != before || state.rich.input.String() != "前X bold 中 italicword keep" {
		t.Fatalf("canceling an identical preedit undid earlier typing: %q, %q", state.session.Current().Text(), state.rich.input.String())
	}
	if _, composing := state.rich.input.Composition(); composing {
		t.Fatal("Undo kept the identical preedit active")
	}
	if err := driver.SelectText(5, 16); err != nil {
		t.Fatal(err)
	}
	driver.SetClipboard("ld 中 italic")
	if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
		t.Fatal(err)
	}
	formatted := "前X **bo**ld 中 italic*word* [keep](https://example.invalid/preserved)\r\n\r\nTail\r\n<!--keep-->\r\n"
	if got := state.session.Current().Text(); got != formatted {
		t.Fatalf("identical visible replacement did not commit its source range: %q", got)
	}
	if err := driver.SelectText(1, 1); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("Y"); err != nil {
		t.Fatal(err)
	}
	after := "前YX **bo**ld 中 italic*word* [keep](https://example.invalid/preserved)\r\n\r\nTail\r\n<!--keep-->\r\n"
	for _, step := range []struct {
		mods gfx.Modifiers
		want string
	}{
		{primaryModifier(), formatted},
		{primaryModifier(), before},
		{primaryModifier(), source},
		{primaryModifier() | gfx.ModifierShift, before},
		{primaryModifier() | gfx.ModifierShift, formatted},
		{primaryModifier() | gfx.ModifierShift, after},
		{primaryModifier(), formatted},
		{primaryModifier(), before},
		{primaryModifier(), source},
	} {
		if _, err := driver.ShortcutChord(step.mods, 'z'); err != nil {
			t.Fatal(err)
		}
		if got := state.session.Current().Text(); got != step.want {
			t.Fatalf("identical-text history changed another transaction: %q, want %q", got, step.want)
		}
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, original) {
		t.Fatalf("identical-text history did not restore BOM/CRLF and source: %q, %v", saved, err)
	}
}

func TestRenderedCrossStyleExplicitMarkedRangeKeepsOneUndoTransaction(t *testing.T) {
	for _, finish := range []string{"cancel", "unmark", "insert", "paste"} {
		t.Run(finish, func(t *testing.T) {
			source := "前 **bold** 中 *italicword* [keep](https://example.invalid/preserved)\r\n\r\nTail\r\n<!--keep-->\r\n"
			path := filepath.Join(t.TempDir(), "Explicit.md")
			original := []byte("\ufeff" + source)
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			state, driver := testDriver(t)
			state.openFile(path)
			clickID(t, driver, renderedModeID)
			clickID(t, driver, renderedRows.At(0))
			if err := driver.SelectText(4, 15); err != nil {
				t.Fatal(err)
			}
			apply := func(event app.TextEvent) {
				t.Helper()
				if _, err := state.onRenderedTextEvent(event); err != nil {
					t.Fatal(err)
				}
				state.syncRendered(state.session.Current())
			}
			apply(app.TextEvent{Kind: app.TextSetMarked, Text: "pin"})
			apply(app.TextEvent{Kind: app.TextSetMarked, Text: "pinyin", HasReplacement: true, Replacement: app.TextRange{Start: 4, End: 7}})
			if got := state.session.Current().Text(); got != source || state.session.Current().Dirty() {
				t.Fatalf("explicit marked range committed a candidate: %q", got)
			}
			if finish != "cancel" {
				event := app.TextEvent{Kind: app.TextUnmark}
				if finish == "insert" || finish == "paste" {
					event = app.TextEvent{Kind: app.TextInsert, Text: "拼音", HasReplacement: true, Replacement: app.TextRange{Start: 4, End: 10}}
					if finish == "paste" {
						event.Kind = app.TextPaste
					}
				}
				apply(event)
				want := "前 **bo**pinyin*word* [keep](https://example.invalid/preserved)\r\n\r\nTail\r\n<!--keep-->\r\n"
				if finish == "insert" || finish == "paste" {
					want = "前 **bo**拼音*word* [keep](https://example.invalid/preserved)\r\n\r\nTail\r\n<!--keep-->\r\n"
				}
				if got := state.session.Current().Text(); got != want {
					t.Fatalf("explicit marked commit=%q, want=%q", got, want)
				}
			}
			apply(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextUndo})
			if got := state.session.Current().Text(); got != source || state.rich.input.String() != "前 bold 中 italicword keep" {
				t.Fatalf("Undo did not restore the original selection: source=%q input=%q message=%q", got, state.rich.input.String(), state.message)
			}
			if _, composing := state.rich.input.Composition(); composing {
				t.Fatal("Undo left an explicit marked range active")
			}
			if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
				t.Fatal(err)
			}
			if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, original) {
				t.Fatalf("explicit range Undo/Save changed original bytes: %q, %v", saved, err)
			}
		})
	}
}

func TestRenderedCrossStyleFormatHistoryFollowsRevertedTyping(t *testing.T) {
	source := "前 **bold** 中 *italicword* [keep](https://example.invalid/preserved)\n"
	formatted := "前 **bo**ld 中 italic*word* [keep](https://example.invalid/preserved)\n"
	for _, test := range []struct {
		name        string
		formatFirst bool
		states      []string
	}{
		{"format-first", true, []string{
			"前X **bo**ld 中 italic*word* [keep](https://example.invalid/preserved)\n", formatted, source,
			formatted, "前X **bo**ld 中 italic*word* [keep](https://example.invalid/preserved)\n", formatted,
		}},
		{"format-last", false, []string{
			source, "前X **bold** 中 *italicword* [keep](https://example.invalid/preserved)\n", source,
			"前X **bold** 中 *italicword* [keep](https://example.invalid/preserved)\n", source, formatted,
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, driver := testDriver(t)
			state.session.Current().Replace(source)
			clickID(t, driver, renderedModeID)
			clickID(t, driver, renderedRows.At(0))
			format := func() {
				t.Helper()
				if err := driver.SelectText(4, 15); err != nil {
					t.Fatal(err)
				}
				driver.SetClipboard("ld 中 italic")
				if _, err := driver.ShortcutChord(primaryModifier(), 'v'); err != nil {
					t.Fatal(err)
				}
			}
			if test.formatFirst {
				format()
			}
			if err := driver.SelectText(1, 1); err != nil {
				t.Fatal(err)
			}
			if err := driver.Type("X"); err != nil {
				t.Fatal(err)
			}
			if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
				t.Fatal(err)
			}
			if !test.formatFirst {
				format()
			}
			if got := state.session.Current().Text(); got != formatted {
				t.Fatalf("typing then deleting did not restore formatted source: %q", got)
			}
			for index, want := range test.states {
				modifiers := primaryModifier()
				if index >= 3 {
					modifiers |= gfx.ModifierShift
				}
				if _, err := driver.ShortcutChord(modifiers, 'z'); err != nil {
					t.Fatal(err)
				}
				if got := state.session.Current().Text(); got != want || state.message != "" {
					t.Fatalf("format-only history skipped transaction %d: %q, want=%q, message=%q", index, got, want, state.message)
				}
			}
		})
	}
}

func TestRenderedCrossStylePreeditArtifacts(t *testing.T) {
	artifacts := os.Getenv("MARKSTACK_COMPOSITION_ARTIFACT_DIR")
	if artifacts == "" {
		t.Skip("acceptance artifact directory not set")
	}
	if err := os.MkdirAll(artifacts, 0755); err != nil {
		t.Fatal(err)
	}
	prefix := strings.Repeat("排版", 12) + " "
	source := prefix + "前 **加粗文字** 中 *斜体文字* [保留链接](https://example.invalid/preserved)\r\n\r\nTail\r\n<!--keep-->\r\n"
	path := filepath.Join(t.TempDir(), "CrossStyle.md")
	original := append([]byte{0xef, 0xbb, 0xbf}, []byte(source)...)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	state := newModel()
	state.mode = renderedMode
	var canvas gfx.Canvas
	session, err := headless.Open(newConfig(state), &canvas, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	state.openFile(path)
	if err := session.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	driver := session.Driver()
	checkLayout := func() {
		t.Helper()
		for _, problem := range headless.CheckLayout(session.Snapshot()) {
			if problem.Kind != headless.LayoutOverlap {
				t.Errorf("cross-style artifact layout=%+v", problem)
			}
		}
		// The SDK overlap checker uses reference widths; native runs must be
		// checked with the same font engine that painted their positions.
		var runs []gfx.Text
		for _, label := range canvas.Texts() {
			if label.Value == "" || label.Style.Color.Alpha <= 0 || !label.HasClip || label.ClipOf != state.previewRect || label.Bounds.Y < state.rich.rect.Y || label.Bounds.Y >= state.rich.rect.Y+state.rich.rect.Height {
				continue
			}
			width := canvas.TextWidthStyle(label.Value, label.Style)
			if label.Bounds.X < state.rich.rect.X-0.01 || label.Bounds.X+width > state.rich.rect.X+state.rich.rect.Width+0.01 {
				t.Errorf("cross-style native run exceeded input: %q at %+v width=%v", label.Value, label.Bounds, width)
			}
			label.Bounds.Width = width
			runs = append(runs, label)
		}
		for first, a := range runs {
			for _, b := range runs[first+1:] {
				if a.Layer != b.Layer || a.Bounds.Y != b.Bounds.Y {
					continue
				}
				if overlap := min(a.Bounds.X+a.Bounds.Width, b.Bounds.X+b.Bounds.Width) - max(a.Bounds.X, b.Bounds.X); overlap > 0.5 {
					t.Errorf("cross-style native runs overlap by %v: %q at %+v and %q at %+v", overlap, a.Value, a.Bounds, b.Value, b.Bounds)
				}
			}
		}
		if len(runs) == 0 {
			t.Fatal("cross-style artifact has no visible native runs")
		}
	}
	clickID(t, driver, renderedRows.At(0))
	visible := state.rich.input.String()
	start := strings.Index(visible, "文字")
	end := strings.Index(visible, "斜体") + len("斜体")
	if err := driver.SelectText(utf8.RuneCountInString(visible[:start]), utf8.RuneCountInString(visible[:end])); err != nil {
		t.Fatal(err)
	}
	if err := driver.Compose(strings.Repeat("候选", 24)); err != nil {
		t.Fatal(err)
	}
	if !state.rich.mixed || renderedPlain(&state.parsed, 0) != state.rich.input.String() || state.preview.LinkCount() != 1 || state.session.Current().Text() != source || state.session.Current().Dirty() {
		t.Fatal("preedit artifact lost the reader projection, link, or original source")
	}
	if err := session.Screenshot(filepath.Join(artifacts, "rendered-cross-style-preedit-740.png")); err != nil {
		t.Fatal(err)
	}
	checkLayout()
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, original) {
		t.Fatalf("preedit artifact reached disk: %q, %v", saved, err)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if err := session.Screenshot(filepath.Join(artifacts, "rendered-cross-style-committed-740.png")); err != nil {
		t.Fatal(err)
	}
	checkLayout()
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	want := append([]byte{0xef, 0xbb, 0xbf}, []byte(prefix+"前 **加粗**新*文字* [保留链接](https://example.invalid/preserved)\r\n\r\nTail\r\n<!--keep-->\r\n")...)
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, want) {
		t.Fatalf("cross-style commit artifact changed source: %q, %v", saved, err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, original) {
		t.Fatalf("cross-style artifact Undo/Save did not restore the original: %q, %v", saved, err)
	}
}

func TestRenderedStyledWrappedPreeditUsesReaderGeometry(t *testing.T) {
	engine := app.PlatformTextEngine()
	if engine == nil {
		t.Skip("platform text engine unavailable")
	}
	for _, markup := range []string{"**boldword**", "*boldword*", "~~boldword~~", "[boldword](https://example.invalid/preserved)"} {
		t.Run(markup, func(t *testing.T) {
			state := newModel()
			source := "前 " + markup + " tail\n\nTail\n"
			state.session.Current().Replace(source)
			var canvas gfx.Canvas
			canvas.SetTextEngine(engine)
			driver, err := app.NewDriver(newConfig(state), &canvas)
			if err != nil {
				t.Fatal(err)
			}
			defer driver.Close()
			if err := driver.Resize(740, 520); err != nil {
				t.Fatal(err)
			}
			clickID(t, driver, renderedModeID)
			clickID(t, driver, renderedRows.At(0))
			if err := driver.SelectText(4, 6); err != nil {
				t.Fatal(err)
			}
			if err := driver.Compose(strings.Repeat("候选", 24)); err != nil {
				t.Fatal(err)
			}
			if !state.rich.mixed || state.session.Current().Text() != source {
				t.Fatalf("styled preedit lost its reader or changed source: mixed=%v source=%q", state.rich.mixed, state.session.Current().Text())
			}
			for _, problem := range headless.CheckLayout(driver.Snapshot()) {
				if problem.Kind == headless.LayoutOverlap {
					t.Errorf("styled preedit retained overlapping text: %+v", problem)
				}
			}
			glyphRuns := 0
			for _, label := range canvas.Texts() {
				if label.Style.Color.Alpha <= 0 || !label.HasClip || label.ClipOf != state.previewRect || label.Bounds.Y < state.rich.rect.Y || label.Bounds.Y >= state.rich.rect.Y+state.rich.rect.Height {
					continue
				}
				glyphRuns++
				width := canvas.TextWidthStyle(label.Value, label.Style)
				if label.Bounds.X < state.rich.rect.X-0.01 || label.Bounds.X+width > state.rich.rect.X+state.rich.rect.Width+0.01 {
					t.Errorf("native styled glyph advance exceeds input: %q x=%v width=%v input=%+v", label.Value, label.Bounds.X, width, state.rich.rect)
				}
			}
			if glyphRuns == 0 {
				t.Fatal("native styled glyph boundary check saw no painted runs")
			}
			marked, composing := state.rich.input.Composition()
			if !composing {
				t.Fatal("composition ended during repaint")
			}
			value := state.rich.input.String()
			start, end := byteOffsetOfRune(value, marked.Start), byteOffsetOfRune(value, marked.End)
			var probe gfx.Canvas
			probe.SetTextEngine(engine)
			selected := ui.RGB(19, 171, 53)
			state.preview.blocks[0].view.Paint(&probe, components.MarkdownPaint{
				X: state.rich.rect.X, Y: state.rich.rect.Y, Clip: state.previewRect,
				Look:      components.MarkdownLook{Selection: selected},
				Selection: components.MarkdownSelection{Anchor: start, Focus: end},
			})
			rows := make(map[float32]bool)
			for _, rectangle := range probe.Rectangles() {
				if rectangle.Color != selected {
					continue
				}
				rows[rectangle.Bounds.Y] = true
				want := rectangle.Bounds
				want.Y += want.Height - 2
				want.Height = 1
				found := false
				for _, painted := range canvas.Rectangles() {
					if painted.Bounds == want && painted.Color.Alpha > 0 {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("missing shaped marked underline at %+v", want)
				}
			}
			if len(rows) < 2 {
				t.Fatalf("marked text did not wrap: %v", rows)
			}
			caret, ok := state.rich.CaretBounds()
			if !ok {
				t.Fatal("styled preedit has no caret")
			}
			if offset, ok := state.rich.OffsetAt(ui.Point{X: caret.X, Y: caret.Y + caret.Height/2}); !ok || offset != end {
				t.Errorf("styled caret hit=%d, %v, want %d", offset, ok, end)
			}
			tailTop := state.previewRect.Y + 46 - state.previewScroll + state.preview.blocks[1].top
			if tailTop < state.rich.rect.Y+state.rich.rect.Height+16 {
				t.Errorf("styled preedit overlaps Tail: rect=%+v tail=%v", state.rich.rect, tailTop)
			}
		})
	}
}

func TestRenderedPlacedLineEdgesMoveAndExtendSelection(t *testing.T) {
	for _, source := range []string{"前 boldword tail\n", "前 **boldword** tail\n"} {
		t.Run(source, func(t *testing.T) {
			state, driver := testDriver(t)
			state.session.Current().Replace(source)
			clickID(t, driver, renderedModeID)
			clickID(t, driver, renderedRows.At(0))
			if err := driver.Key(gfx.KeyHome, 0); err != nil {
				t.Fatal(err)
			}
			if state.rich.input.Caret() != 0 {
				t.Fatalf("Home left the placed caret at %d", state.rich.input.Caret())
			}
			for step := 0; step < 4; step++ {
				if err := driver.Key(gfx.KeyRight, 0); err != nil {
					t.Fatal(err)
				}
			}
			for step := 0; step < 2; step++ {
				if err := driver.Key(gfx.KeyRight, gfx.ModifierShift); err != nil {
					t.Fatal(err)
				}
			}
			selection := state.rich.input.Selection()
			if selection.Start != 4 || selection.End != 6 {
				t.Fatalf("placed horizontal selection=%+v", selection)
			}
			if err := driver.Key(gfx.KeyEnd, gfx.ModifierShift); err != nil {
				t.Fatal(err)
			}
			selection = state.rich.input.Selection()
			if selection.Start != 4 || selection.End != state.rich.input.RuneCount() {
				t.Fatalf("Shift+End selection=%+v", selection)
			}
			if err := driver.Key(gfx.KeyLeft, gfx.ModifierMeta); err != nil {
				t.Fatal(err)
			}
			if state.rich.input.Caret() != 0 {
				t.Fatal("Command+Left did not move to the visible line start")
			}
			if err := driver.Key(gfx.KeyRight, gfx.ModifierMeta); err != nil {
				t.Fatal(err)
			}
			if state.rich.input.Caret() != state.rich.input.RuneCount() || state.session.Current().Text() != source {
				t.Fatal("Command+Right changed source or failed to reach the visible line end")
			}
		})
	}
}

func TestRenderedWrappedLineEdgesStayOnTheVisibleRow(t *testing.T) {
	engine := app.PlatformTextEngine()
	if engine == nil {
		t.Skip("platform text engine unavailable")
	}
	for _, styled := range []bool{false, true} {
		name := "plain"
		if styled {
			name = "bold"
		}
		t.Run(name, func(t *testing.T) {
			value := strings.Repeat("排版文字", 20)
			source := value + "\n\nTail\n"
			if styled {
				source = "**" + value + "**\n\nTail\n"
			}
			state := newModel()
			state.session.Current().Replace(source)
			var canvas gfx.Canvas
			canvas.SetTextEngine(engine)
			driver, err := app.NewDriver(newConfig(state), &canvas)
			if err != nil {
				t.Fatal(err)
			}
			defer driver.Close()
			if err := driver.Resize(740, 520); err != nil {
				t.Fatal(err)
			}
			clickID(t, driver, renderedModeID)
			clickID(t, driver, renderedRows.At(0))
			if err := driver.SelectText(utf8.RuneCountInString(value), utf8.RuneCountInString(value)); err != nil {
				t.Fatal(err)
			}
			caret, ok := state.rich.CaretBounds()
			if !ok || caret.Y <= state.rich.rect.Y {
				t.Fatalf("caret did not wrap: %+v, %v", caret, ok)
			}
			start, ok := state.rich.OffsetAt(ui.Point{X: state.rich.rect.X, Y: caret.Y + caret.Height/2})
			if !ok || start <= 0 {
				t.Fatalf("last visible row has no start: %d, %v", start, ok)
			}
			if err := driver.Key(gfx.KeyHome, 0); err != nil {
				t.Fatal(err)
			}
			if got := byteOffsetOfRune(value, state.rich.input.Caret()); got != start {
				t.Fatalf("Home moved to %d, want visible row start %d", got, start)
			}
			if err := driver.Key(gfx.KeyEnd, gfx.ModifierShift); err != nil {
				t.Fatal(err)
			}
			selection := state.rich.input.Selection()
			if selection.Start != utf8.RuneCountInString(value[:start]) || selection.End != utf8.RuneCountInString(value) || state.session.Current().Text() != source {
				t.Fatalf("Shift+End left visible row or changed source: %+v", selection)
			}
		})
	}
}

func TestRenderedEmptyStyledPreeditReservesItsLine(t *testing.T) {
	state, driver := testDriver(t)
	source := "**boldword**\n\nTail\n"
	state.session.Current().Replace(source)
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.SelectText(0, 8); err != nil {
		t.Fatal(err)
	}
	if _, err := state.onRenderedTextEvent(app.TextEvent{Kind: app.TextSetMarked}); err != nil {
		t.Fatal(err)
	}
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	if _, composing := state.rich.input.Composition(); !composing || state.session.Current().Text() != source {
		t.Fatal("empty preedit changed its source or lost composition")
	}
	if caret, ok := state.rich.CaretBounds(); !ok || caret.Height <= 0 {
		t.Fatalf("empty styled preedit caret=%+v, %v", caret, ok)
	}
	tailTop := state.previewRect.Y + 46 - state.previewScroll + state.preview.blocks[1].top
	if state.rich.rect.Height < 26 || tailTop < state.rich.rect.Y+state.rich.rect.Height+16 {
		t.Fatalf("empty styled preedit lost its lane: rect=%+v tail=%v", state.rich.rect, tailTop)
	}
}

func TestRenderedCrossStylePreeditPreservesStrikeDecoration(t *testing.T) {
	engine := app.PlatformTextEngine()
	if engine == nil {
		t.Skip("platform text engine unavailable")
	}
	prefix := strings.Repeat("排版", 12) + " "
	source := "# Title\r\n\r\n" + prefix + "前 **bold** 中 ~~strikeword~~ [keep](https://example.invalid/unchanged)\r\n\r\nTail\r\n<!--keep-->\r\n"
	committed := "# Title\r\n\r\n" + prefix + "前 **bo**新~~word~~ [keep](https://example.invalid/unchanged)\r\n\r\nTail\r\n<!--keep-->\r\n"
	path := filepath.Join(t.TempDir(), "StrikePreedit.md")
	original := append([]byte{0xef, 0xbb, 0xbf}, []byte(source)...)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	state := newModel()
	state.openFile(path)
	var canvas gfx.Canvas
	canvas.SetTextEngine(engine)
	driver, err := app.NewDriver(newConfig(state), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	if err := driver.Resize(740, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(1))
	visible := state.rich.input.String()
	from := strings.Index(visible, "ld")
	to := strings.Index(visible, "strike") + len("strike")
	if from < 0 || to <= from || from >= to {
		t.Fatalf("fixture selection missing from %q", visible)
	}
	selectRange := func() {
		t.Helper()
		if err := driver.SelectText(utf8.RuneCountInString(visible[:from]), utf8.RuneCountInString(visible[:to])); err != nil {
			t.Fatal(err)
		}
	}
	// assertStrike checks the painted strike decoration of one untouched tail,
	// not only its label value: the reader paints a one-pixel rule at the middle
	// of a struck run, so a flattened run would lose both the rule and its width.
	assertStrike := func(stage, tail string) {
		t.Helper()
		driver.Snapshot()
		var label gfx.Text
		found := false
		for _, candidate := range canvas.Texts() {
			if candidate.Value != tail || candidate.Style.Color.Alpha == 0 {
				continue
			}
			label, found = candidate, true
			break
		}
		if !found {
			t.Fatalf("%s: strike tail %q is not painted", stage, tail)
		}
		mid := label.Bounds.Y + label.Bounds.Height/2
		decorated, covered := 0, false
		for _, rectangle := range canvas.Rectangles() {
			if rectangle.Bounds.Height != 1 || rectangle.Color != label.Style.Color {
				continue
			}
			if rectangle.Bounds.Y < mid-1 || rectangle.Bounds.Y > mid+1 {
				continue
			}
			decorated++
			if rectangle.Bounds.X >= label.Bounds.X-1 && rectangle.Bounds.X <= label.Bounds.X+1 &&
				rectangle.Bounds.Width >= label.Bounds.Width-1 && rectangle.Bounds.Width <= label.Bounds.Width+1 {
				covered = true
			}
		}
		if !covered || decorated != 1 {
			t.Errorf("%s: strike tail %q decoration covered=%v pulses=%d label=%+v", stage, tail, covered, decorated, label.Bounds)
		}
	}
	assertPreeditOutOfDocument := func(stage string) {
		t.Helper()
		if state.session.Current().Text() != source || state.session.Current().Dirty() {
			t.Fatalf("%s: preedit reached committed source: %q dirty=%v", stage, state.session.Current().Text(), state.session.Current().Dirty())
		}
		if _, composing := state.rich.input.Composition(); !composing {
			t.Fatalf("%s: composition is not active", stage)
		}
		if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(state.message, "composition") {
			t.Errorf("%s: pending composition did not refuse Save: %q", stage, state.message)
		}
		if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, original) {
			t.Fatalf("%s: preedit reached disk: %q, %v", stage, saved, err)
		}
	}
	assertStrike("idle reader", "strikeword")
	selectRange()
	if err := driver.Compose("p"); err != nil {
		t.Fatal(err)
	}
	if !state.rich.mixed || renderedPlain(&state.parsed, 1) != state.rich.input.String() {
		t.Fatalf("short cross-style preedit flattened the reader: mixed=%v parsed=%q editor=%q", state.rich.mixed, renderedPlain(&state.parsed, 1), state.rich.input.String())
	}
	assertStrike("short preedit", "word")
	assertPreeditOutOfDocument("short preedit")
	long := strings.Repeat("候选", 30)
	if err := driver.Compose(long); err != nil {
		t.Fatal(err)
	}
	if !state.rich.mixed || renderedPlain(&state.parsed, 1) != state.rich.input.String() {
		t.Fatalf("long cross-style preedit flattened the reader: mixed=%v parsed=%q editor=%q", state.rich.mixed, renderedPlain(&state.parsed, 1), state.rich.input.String())
	}
	assertStrike("long preedit", "word")
	assertPreeditOutOfDocument("long preedit")
	for _, height := range []int{520, 780} {
		if err := driver.Resize(740, height); err != nil {
			t.Fatal(err)
		}
		assertStrike("repaint", "word")
		if state.session.Current().Text() != source {
			t.Fatalf("repaint at height %d changed source", height)
		}
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if _, composing := state.rich.input.Composition(); composing {
		t.Fatal("canceled preedit stayed composition active")
	}
	if state.session.Current().Text() != source || state.session.Current().Dirty() {
		t.Fatalf("canceled preedit changed source: %q dirty=%v", state.session.Current().Text(), state.session.Current().Dirty())
	}
	assertStrike("canceled repaint", "strikeword")
	selectRange()
	if err := driver.Compose("pin"); err != nil {
		t.Fatal(err)
	}
	assertStrike("renewed preedit", "word")
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if got := state.session.Current().Text(); got != committed {
		t.Fatalf("strike-tail commit=%q, want=%q", got, committed)
	}
	assertStrike("committed repaint", "word")
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, append([]byte{0xef, 0xbb, 0xbf}, []byte(committed)...)) {
		t.Fatalf("committed strike tail changed bytes: %q, %v", saved, err)
	}
	if strings.Contains(state.session.Current().Text(), "pin") || strings.Contains(state.session.Current().Text(), "候选") {
		t.Fatal("preedit text was committed into the source")
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, original) {
		t.Fatalf("strike-tail Undo/Save did not restore the original: %q, %v", saved, err)
	}
}
