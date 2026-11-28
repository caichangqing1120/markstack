package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"markstack/internal/markdownedit"
	"markstack/internal/markdownsyntax"
)

func TestM14CardsPaintAndEditInPlace(t *testing.T) {
	state, driver := testDriver(t)
	path := filepath.Join(t.TempDir(), "cards.md")
	before := "---\r\ntitle: 原文\r\ntags: [one, two]\r\n---\r\n\r\n> [!nOtE] Custom\r\n> First\r\n>\r\n>Second\r\n\r\n# Tail\r\n"
	if err := os.WriteFile(path, []byte("\ufeff"+before), 0600); err != nil {
		t.Fatal(err)
	}
	state.openFile(path)
	for _, mode := range []gfx.ID{readModeID, splitModeID, renderedModeID} {
		clickID(t, driver, mode)
		if len(state.preview.blocks) != 3 || state.preview.blocks[0].card.label != "YAML Front Matter" ||
			state.preview.blocks[1].card.label != "NOTE 提示 · Custom" || state.preview.blocks[2].heading != 1 {
			t.Fatalf("mode %v did not project two cards and a heading: %v", state.mode, state.parsed.Blocks)
		}
	}
	current := state.session.Current()
	clickID(t, driver, renderedRows.At(0))
	if state.rich.block.Kind != markdownedit.FrontMatter || driver.Snapshot().Focused != richInputID {
		t.Fatal("metadata did not acquire rendered editor focus")
	}
	m12Select(t, driver, 7, 9)
	if err := driver.Compose("tentative"); err != nil {
		t.Fatal(err)
	}
	if current.Text() != before {
		t.Fatal("metadata persisted tentative IME input")
	}
	if err := driver.Type("新值"); err != nil {
		t.Fatal(err)
	}
	metadataEdited := strings.Replace(before, "原文", "新值", 1)
	if current.Text() != metadataEdited || state.mode != renderedMode {
		t.Fatalf("metadata edit: %q in mode %v", current.Text(), state.mode)
	}
	m12Chord(t, driver, primaryModifier(), 'z')
	if current.Text() != before {
		t.Fatalf("metadata undo: %q", current.Text())
	}
	m12Chord(t, driver, primaryModifier()|gfx.ModifierShift, 'z')
	if current.Text() != metadataEdited {
		t.Fatalf("metadata redo: %q", current.Text())
	}
	clickID(t, driver, renderedRows.At(1))
	if state.rich.block.Kind != markdownedit.Callout || driver.Snapshot().Focused != richInputID {
		t.Fatal("callout did not acquire rendered editor focus")
	}
	m12Select(t, driver, 0, 5)
	if err := driver.Type("First revised"); err != nil {
		t.Fatal(err)
	}
	m12Select(t, driver, 13, 13)
	m12Key(t, driver, gfx.KeyEnter)
	if err := driver.Type("Inserted"); err != nil {
		t.Fatal(err)
	}
	after := strings.Replace(metadataEdited, "> First\r\n", "> First revised\r\n> Inserted\r\n", 1)
	if current.Text() != after || state.mode != renderedMode || driver.Snapshot().Focused != richInputID {
		t.Fatalf("callout edit: %q in mode %v", current.Text(), state.mode)
	}
	m12Chord(t, driver, primaryModifier(), 's')
	if saved, err := os.ReadFile(path); err != nil || string(saved) != "\ufeff"+after {
		t.Fatalf("BOM/CRLF changed on save: %q (%v)", saved, err)
	}
}

func TestM14AlertColorsAndExtendedRuns(t *testing.T) {
	state, driver := testDriver(t)
	current := state.session.Current()
	current.Replace("==highlight== <u>underline</u> ~sub~ ^sup^ <sub>also sub</sub> <sup>also sup</sup> ~~strike~~\n\n" +
		"> [!NOTE]\n> Note\n\n> [!TIP]\n> Tip\n\n> [!IMPORTANT]\n> Important\n\n> [!WARNING]\n> Warning\n\n> [!CAUTION]\n> Caution\n")
	clickID(t, driver, readModeID)
	if len(state.preview.blocks) != 6 {
		t.Fatalf("preview has %d blocks", len(state.preview.blocks))
	}
	for _, night := range []bool{false, true} {
		state.dark = night
		state.preview.layoutKey = previewLayoutKey{}
		state.invalidator.Invalidate()
		if err := driver.Pump(); err != nil {
			t.Fatal(err)
		}
		colors := map[gfx.Color]bool{}
		for index, name := range formatCallouts {
			card := state.preview.blocks[index+1].card
			alert, _ := markdownsyntax.AlertType(name)
			if card.label != alert.Label() || colors[card.accent] {
				t.Fatalf("duplicate accent or wrong label for %s: %v", name, card)
			}
			colors[card.accent] = true
		}
	}
	runs := state.preview.blocks[0].technical
	styles := make(map[string]technicalRun)
	for _, run := range runs {
		styles[run.text] = run
	}
	for label, style := range map[string]markdownsyntax.Style{"highlight": markdownsyntax.Highlight, "underline": markdownsyntax.Underline, "sub": markdownsyntax.Subscript, "sup": markdownsyntax.Superscript} {
		if styles[label].extended != 1<<style {
			t.Errorf("%q: style=%v in %v", label, styles[label].extended, runs)
		}
	}
	if !styles["strike"].strike || styles["sub"].bounds.Y <= styles["sup"].bounds.Y || styles["sup"].style.Size >= styles["underline"].style.Size {
		t.Fatalf("missing strike or invalid script geometry: %v", styles)
	}
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if !state.rich.mixed || !state.preview.blocks[0].technicalEditable {
		t.Fatal("extended styles disappeared from the active block")
	}
	for _, offset := range []int{0, 3, 12, 21, 26, len(state.rich.input.String())} {
		caret, ok := state.rich.caretBoundsAt(offset, false)
		resolved, hit := state.rich.offsetAt(gfx.Point{X: caret.X, Y: caret.Y + caret.Height/2})
		if !ok || !hit || resolved != offset {
			t.Fatalf("styled caret byte %d mapped back to %d", offset, resolved)
		}
	}
}

func TestM14InvalidMetadataDraftCannotBeSavedOrAbandonedAccidentally(t *testing.T) {
	state, driver := testDriver(t)
	path := filepath.Join(t.TempDir(), "draft.md")
	source := "---\ntitle: Original\n---\n\nTail\n"
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	state.openFile(path)
	clickID(t, driver, renderedModeID)
	if !hasText(driver, "title: Original") {
		t.Fatal("metadata content was not painted")
	}
	clickID(t, driver, renderedRows.At(0))
	m12Select(t, driver, 7, 15)
	if err := driver.Type("[broken"); err != nil {
		t.Fatal(err)
	}
	if !state.hasCardDraft() || state.session.Current().Text() != source {
		t.Fatal("invalid YAML should stay visible without changing the source")
	}
	m12Chord(t, driver, primaryModifier(), 's')
	clickID(t, driver, editModeID)
	if !state.hasCardDraft() || state.mode != renderedMode || state.rich.input.String() != "title: [broken" || state.prepareDocumentNavigation() {
		t.Fatal("save or navigation allowed an invalid draft to disappear")
	}
	if saved, err := os.ReadFile(path); err != nil || string(saved) != source {
		t.Fatalf("invalid YAML reached disk: %q (%v)", saved, err)
	}
	m12Key(t, driver, gfx.KeyEscape)
	if state.hasCardDraft() || state.rich.input.String() != "title: Original" {
		t.Fatal("Escape did not restore the last valid card")
	}
}

func TestM14SelectionBubbleUsesFormattingCommands(t *testing.T) {
	for _, mode := range []struct {
		name string
		id   gfx.ID
	}{{"source", editModeID}, {"split", splitModeID}, {"rendered", renderedModeID}} {
		t.Run(mode.name, func(t *testing.T) {
			state, driver := testDriver(t)
			current := state.session.Current()
			current.Replace("One word here\n")
			clickID(t, driver, mode.id)
			input := sourceID
			if mode.id == renderedModeID {
				clickID(t, driver, renderedRows.At(0))
				input = richInputID
			} else {
				clickID(t, driver, sourceID)
			}
			for _, index := range []int{0, 1, 2, 3, 4, 5} {
				m12Select(t, driver, 4, 8)
				for button := range selectionFormatOptions {
					m12Hit(t, driver, selectionFormatButtons.At(button))
				}
				wrapper := formatWrappers[selectionFormatOptions[index].command]
				clickID(t, driver, selectionFormatButtons.At(index))
				if current.Text() != "One "+wrapper[0]+"word"+wrapper[1]+" here\n" || driver.Snapshot().Focused != input {
					t.Fatalf("button %d: %q focus=%v", index, current.Text(), driver.Snapshot().Focused)
				}
				if mode.id != renderedModeID {
					m12Select(t, driver, 4+len(wrapper[0]), 8+len(wrapper[0]))
				}
				clickID(t, driver, selectionFormatButtons.At(7))
				if current.Text() != "One word here\n" {
					t.Fatalf("clear after %d: %q", index, current.Text())
				}
			}
			m12Select(t, driver, 4, 8)
			clickID(t, driver, selectionFormatButtons.At(6))
			if err := driver.Type("https://example.invalid"); err != nil {
				t.Fatal(err)
			}
			m12Key(t, driver, gfx.KeyEnter)
			if current.Text() != "One [word](https://example.invalid) here\n" {
				t.Fatalf("link button: %q", current.Text())
			}
		})
	}
}
