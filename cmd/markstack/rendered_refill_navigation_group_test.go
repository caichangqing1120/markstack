package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestRenderedRefillNativeGroupSurvivesNavigation(t *testing.T) {
	for _, scenario := range []struct {
		name, original, empty, filled string
		row                           int
	}{
		{"whole", "Left\n\nRight\n", "\n", "New\n", 0},
		{"middle", "First\n\nSolo\n\nLast\n", "First\n\n\n\nLast\n", "First\n\nNew\n\nLast\n", 1},
		{"readonly-neighbor", "First\n\nSolo\n\n<div>opaque</div>\n", "First\n\n\n\n<div>opaque</div>\n", "First\n\nNew\n\n<div>opaque</div>\n", 1},
	} {
		navigations := []string{"none", "source", "tab"}
		if scenario.name == "middle" {
			navigations = append(navigations, "selection")
		}
		if scenario.name == "readonly-neighbor" {
			navigations = []string{"source-fallback"}
		}
		for _, navigation := range navigations {
			t.Run(scenario.name+"/"+navigation, func(t *testing.T) {
				state, driver, filename, original := renderedSourceCollisionFixture(t, scenario.original, false, 740)
				owner, selectedTab := state.session.Current(), state.session.Active
				clickID(t, driver, renderedRows.At(scenario.row))
				if scenario.name == "whole" {
					state.rich.input.MoveTo(0, false)
					if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
						t.Fatal(err)
					}
					if _, err := driver.ShortcutChord(primaryModifier(), 'x'); err != nil {
						t.Fatal(err)
					}
				} else {
					if _, err := driver.ShortcutChord(primaryModifier(), 'a'); err != nil {
						t.Fatal(err)
					}
					if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
						t.Fatal(err)
					}
				}
				bytesFor := func(source string) []byte {
					return []byte("\xef\xbb\xbf" + strings.ReplaceAll(source, "\n", "\r\n"))
				}
				expectRenderedCollisionSave(t, state, driver, filename, bytesFor(scenario.empty))
				for _, character := range "New" {
					if err := driver.Type(string(character)); err != nil {
						t.Fatal(err)
					}
					if err := driver.Pump(); err != nil {
						t.Fatal(err)
					}
				}
				expectRenderedCollisionSave(t, state, driver, filename, bytesFor(scenario.filled))
				switch navigation {
				case "source":
					clickID(t, driver, editModeID)
					clickID(t, driver, renderedModeID)
				case "source-fallback":
					clickID(t, driver, renderedRows.At(2))
					if state.mode != editMode || driver.Snapshot().Focused != sourceID {
						t.Fatal("read-only block did not fall back to the source editor")
					}
					if state.rich.active || state.rich.input != nil {
						t.Fatal("source fallback retained a hidden rendered editor")
					}
					clickID(t, driver, renderedModeID)
				case "tab":
					other := filepath.Join(t.TempDir(), "Other.txt")
					if err := os.WriteFile(other, []byte("Neighbor\r\n"), 0640); err != nil {
						t.Fatal(err)
					}
					if !state.openFile(other) {
						t.Fatal("failed to open another tab")
					}
					state.invalidator.Invalidate()
					if err := driver.Pump(); err != nil {
						t.Fatal(err)
					}
					clickID(t, driver, tabRows.At(selectedTab))
					if state.mode != renderedMode {
						clickID(t, driver, renderedModeID)
					}
				case "selection":
					if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
						t.Fatal(err)
					}
					if !state.selection.active || driver.Snapshot().Focused != renderedSelectionID {
						t.Fatal("setup did not enter a rendered cross-block selection")
					}
					if err := driver.Key(gfx.KeyEscape, 0); err != nil {
						t.Fatal(err)
					}
					if state.selection.active || !state.rich.active || driver.Snapshot().Focused != richInputID {
						t.Fatal("setup did not collapse to the rendered input")
					}
				}
				if state.session.Current() != owner || owner.Text() != strings.ReplaceAll(scenario.filled, "\n", "\r\n") {
					t.Fatal("navigation changed the active document or text")
				}
				for _, step := range []struct {
					key  rune
					want []byte
				}{{'z', bytesFor(scenario.empty)}, {'z', original}, {'y', bytesFor(scenario.empty)}, {'y', bytesFor(scenario.filled)}} {
					if _, err := driver.ShortcutChord(primaryModifier(), step.key); err != nil {
						t.Fatal(err)
					}
					if got := []byte("\xef\xbb\xbf" + owner.Text()); string(got) != string(step.want) {
						t.Fatalf("history %c changed the native group: got %q, want %q", step.key, got, step.want)
					}
					if state.mode != renderedMode || !state.rich.active || driver.Snapshot().Focused != richInputID {
						t.Fatal("history did not retain the rendered input target")
					}
					expectRenderedCollisionSave(t, state, driver, filename, step.want)
				}
			})
		}
	}
}

func TestRenderedEmptyParagraphTypesAfterReadOnlyFallback(t *testing.T) {
	state, driver, filename, _ := renderedSourceCollisionFixture(t, "First\n\nSolo\n\n<div>opaque</div>\n", false, 740)
	owner := state.session.Current()
	clickID(t, driver, renderedRows.At(1))
	if _, err := driver.ShortcutChord(primaryModifier(), 'a'); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
		t.Fatal(err)
	}
	bytesFor := func(source string) []byte {
		return []byte("\xef\xbb\xbf" + strings.ReplaceAll(source, "\n", "\r\n"))
	}
	expectRenderedCollisionSave(t, state, driver, filename, bytesFor("First\n\n\n\n<div>opaque</div>\n"))
	clickID(t, driver, renderedRows.At(1))
	if state.mode != editMode || driver.Snapshot().Focused != sourceID || state.rich.active || state.rich.input != nil {
		t.Fatal("read-only block did not fall back to the source editor")
	}
	if state.session.Current() != owner {
		t.Fatal("read-only fallback changed the active document")
	}
	if got := owner.Editor.Caret(); got != len("First\r\n\r\n\r\n\r\n") {
		t.Fatalf("fallback source caret=%d, want the start of the read-only block", got)
	}
	owner.Editor.MoveTo(len("First\r\n\r\n"), false)
	clickID(t, driver, renderedModeID)
	if state.mode != renderedMode || !state.rich.active || driver.Snapshot().Focused != richInputID {
		t.Fatal("returning to layout lost the empty paragraph anchor")
	}
	if err := driver.Type("New"); err != nil {
		t.Fatal(err)
	}
	if driver.Snapshot().Focused != richInputID {
		t.Fatal("typing at the empty anchor lost the rendered input")
	}
	expectRenderedCollisionSave(t, state, driver, filename, bytesFor("First\n\nNew\n\n<div>opaque</div>\n"))
}

func TestRenderedRefillNativeGroupsSeparateByCursorMotion(t *testing.T) {
	state, driver, filename, original := renderedSourceCollisionFixture(t, "Left\n\nRight\n", false, 740)
	clickID(t, driver, renderedRows.At(0))
	state.rich.input.MoveTo(0, false)
	if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'x'); err != nil {
		t.Fatal(err)
	}
	bytesFor := func(source string) []byte {
		return []byte("\xef\xbb\xbf" + strings.ReplaceAll(source, "\n", "\r\n"))
	}
	expectRenderedCollisionSave(t, state, driver, filename, bytesFor("\n"))
	for _, character := range "abc" {
		if err := driver.Type(string(character)); err != nil {
			t.Fatal(err)
		}
		if err := driver.Pump(); err != nil {
			t.Fatal(err)
		}
	}
	expectRenderedCollisionSave(t, state, driver, filename, bytesFor("abc\n"))
	state.rich.input.MoveTo(0, false)
	for _, character := range "de" {
		if err := driver.Type(string(character)); err != nil {
			t.Fatal(err)
		}
		if err := driver.Pump(); err != nil {
			t.Fatal(err)
		}
	}
	expectRenderedCollisionSave(t, state, driver, filename, bytesFor("deabc\n"))
	clickID(t, driver, editModeID)
	clickID(t, driver, renderedModeID)
	for _, step := range []struct {
		key  rune
		want []byte
	}{
		{'z', bytesFor("abc\n")}, {'z', bytesFor("\n")}, {'z', original},
		{'y', bytesFor("\n")}, {'y', bytesFor("abc\n")}, {'y', bytesFor("deabc\n")},
	} {
		if _, err := driver.ShortcutChord(primaryModifier(), step.key); err != nil {
			t.Fatal(err)
		}
		if got := []byte("\xef\xbb\xbf" + state.session.Current().Text()); string(got) != string(step.want) {
			t.Fatalf("history %c separated the native typing groups: got %q, want %q", step.key, got, step.want)
		}
	}
}

func TestRenderedRefillNativeGroupDoesNotReviveStaleSourceRedo(t *testing.T) {
	state, driver, filename, _ := renderedSourceCollisionFixture(t, "Left\n\nRight\n", false, 740)
	owner := state.session.Current()
	clickID(t, driver, renderedRows.At(0))
	state.rich.input.MoveTo(0, false)
	if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'x'); err != nil {
		t.Fatal(err)
	}
	for _, character := range "New" {
		if err := driver.Type(string(character)); err != nil {
			t.Fatal(err)
		}
		if err := driver.Pump(); err != nil {
			t.Fatal(err)
		}
	}
	expectRenderedCollisionSave(t, state, driver, filename, []byte("\xef\xbb\xbfNew\r\n"))
	if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
		t.Fatal(err)
	}
	if got := owner.Text(); got != "\r\n" {
		t.Fatalf("setup undo left %q, want the empty source", got)
	}
	if !owner.Editor.CanRedo() {
		t.Fatal("setup undo did not leave source redo")
	}
	clickID(t, driver, editModeID)
	if err := driver.Type("X"); err != nil {
		t.Fatal(err)
	}
	if owner.Editor.CanRedo() {
		t.Fatal("new source edit retained stale redo")
	}
	clickID(t, driver, renderedModeID)
	for _, key := range []rune{'y', 'y'} {
		if _, err := driver.ShortcutChord(primaryModifier(), key); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(owner.Text(), "New") {
			t.Fatalf("stale source redo revived the discarded native group: %q", owner.Text())
		}
	}
}

func TestRenderedRefillNativeGroupRejectsStaleIdentity(t *testing.T) {
	for _, stale := range []string{"source", "document"} {
		t.Run(stale, func(t *testing.T) {
			state, driver, _, _ := renderedSourceCollisionFixture(t, "Left\n\nRight\n", false, 740)
			clickID(t, driver, renderedRows.At(0))
			state.rich.input.MoveTo(0, false)
			if err := driver.Key(gfx.KeyDown, gfx.ModifierShift); err != nil {
				t.Fatal(err)
			}
			if _, err := driver.ShortcutChord(primaryModifier(), 'x'); err != nil {
				t.Fatal(err)
			}
			for _, character := range "New" {
				if err := driver.Type(string(character)); err != nil {
					t.Fatal(err)
				}
				if err := driver.Pump(); err != nil {
					t.Fatal(err)
				}
			}
			clickID(t, driver, editModeID)
			switch stale {
			case "source":
				state.rich.source += "stale"
			case "document":
				state.rich.documentID++
			}
			clickID(t, driver, renderedModeID)
			if _, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil {
				t.Fatal(err)
			}
			if got := state.session.Current().Text(); got != "Ne\r\n" {
				t.Fatalf("stale %s identity reused the sampled native group: got %q, want the single source step", stale, got)
			}
		})
	}
}
