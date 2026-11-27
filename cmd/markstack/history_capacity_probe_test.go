package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestRefillGroupDoesNotPartiallyUndoAtCapacity(test *testing.T) {
	for _, count := range []int{255, 256, 257} {
		for _, route := range []string{"active", "source", "tab"} {
			test.Run(fmt.Sprintf("%d/%s", count, route), func(context *testing.T) {
				const prefix = "First\r\n\r\n"
				const suffix = "\r\n\r\nLast\r\n"
				state, driver, filename, original := renderedSourceCollisionFixture(context, "First\n\nSolo\n\nLast\n", false, 1200)
				owner, index := state.session.Current(), state.session.Active
				clickID(context, driver, renderedRows.At(1))
				shortcut := func(character rune) {
					context.Helper()
					if _, err := driver.ShortcutChord(primaryModifier(), character); err != nil {
						context.Fatal(err)
					}
				}
				shortcut('a')
				if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
					context.Fatal(err)
				}
				empty := prefix + suffix
				if owner.Text() != empty {
					context.Fatalf("clear left %q", owner.Text())
				}
				for range count {
					if err := driver.Type("x"); err != nil {
						context.Fatal(err)
					}
				}
				filled := prefix + strings.Repeat("x", count) + suffix
				if owner.Text() != filled {
					context.Fatal("unexpected source before layout navigation")
				}
				switch route {
				case "source":
					clickID(context, driver, editModeID)
					clickID(context, driver, renderedModeID)
				case "tab":
					neighbor := filepath.Join(context.TempDir(), "neighbor.txt")
					if err := os.WriteFile(neighbor, []byte("neighbor\r\n"), 0600); err != nil {
						context.Fatal(err)
					}
					if !state.openFile(neighbor) {
						context.Fatal(state.message)
					}
					state.invalidator.Invalidate()
					if err := driver.Pump(); err != nil {
						context.Fatal(err)
					}
					clickID(context, driver, tabRows.At(index))
					if state.mode != renderedMode {
						clickID(context, driver, renderedModeID)
					}
				}
				if state.session.Current() != owner || owner.Text() != filled {
					context.Fatal("navigation changed the editing target or source")
				}
				shortcut('z')
				actual := owner.Text()
				if actual != empty && !(count > maxRenderedHistory && actual == filled) {
					context.Fatalf("partial or missing undo: %d characters remain of %d; status %q", len(actual)-len(empty), count, state.message)
				}
				if actual == empty {
					shortcut('y')
					if owner.Text() != filled {
						context.Fatalf("redo did not restore the group: %d characters remain of %d", len(owner.Text())-len(empty), count)
					}
				}
				if stored, err := os.ReadFile(filename); err != nil || string(stored) != string(original) {
					context.Fatalf("history wrote source before Save: %v", err)
				}
			})
		}
	}
}

// TestRefillTruncatedUnitRefusesAtomically proves that a recreated rendered
// input never replays the retained suffix of a native unit whose oldest edits
// the history bound already dropped. One Undo must refuse the whole unit:
// source, caret, selection, view cursor and both stacks stay intact, the
// refusal survives reactivation, and a later independent edit still undoes and
// redoes on its own.
func TestRefillTruncatedUnitRefusesAtomically(test *testing.T) {
	const prefix = "First\r\n\r\n"
	const suffix = "\r\n\r\nLast\r\n"
	const count = maxRenderedHistory + 1
	state, driver, filename, original := renderedSourceCollisionFixture(test, "First\n\nSolo\n\nLast\n", false, 1200)
	owner := state.session.Current()
	clickID(test, driver, renderedRows.At(1))
	shortcut := func(character rune) {
		test.Helper()
		if _, err := driver.ShortcutChord(primaryModifier(), character); err != nil {
			test.Fatal(err)
		}
	}
	shortcut('a')
	if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
		test.Fatal(err)
	}
	for range count {
		if err := driver.Type("x"); err != nil {
			test.Fatal(err)
		}
	}
	filled := prefix + strings.Repeat("x", count) + suffix
	if owner.Text() != filled {
		test.Fatal("unexpected source before layout navigation")
	}
	// Source mode discards the native input editor, so only the sampled
	// metadata survives and the 257-edit unit can no longer be replayed whole.
	clickID(test, driver, editModeID)
	clickID(test, driver, renderedModeID)
	if state.session.Current() != owner || owner.Text() != filled {
		test.Fatal("navigation changed the editing target or source")
	}
	stack := state.rich.crossUndo
	cursor := state.rich.appliedEdits + state.rich.sourceHistoryOffset
	marked := 0
	for _, edit := range stack {
		if edit.group != 0 {
			test.Fatalf("truncated sample published a group of %d", edit.group)
		}
		if edit.unrecovered {
			marked++
		}
	}
	if len(stack) != maxRenderedHistory || marked != 1 || !stack[len(stack)-1].unrecovered || stack[len(stack)-1].position != cursor {
		test.Fatalf("truncated unit sampled as %d marks in %d entries at cursor %d", marked, len(stack), cursor)
	}
	selection, caret := owner.Editor.Selection(), owner.Editor.Caret()
	canUndo, canRedo := owner.Editor.CanUndo(), owner.Editor.CanRedo()
	wantUndo, wantRedo := slices.Clone(state.rich.crossUndo), slices.Clone(state.rich.crossRedo)
	refused := func(label string) {
		test.Helper()
		if owner.Text() != filled {
			test.Fatalf("%s: truncated unit reverted to %d of %d characters", label, len(owner.Text())-len(prefix+suffix), count)
		}
		if state.message == "" {
			test.Fatalf("%s: refusal was not reported", label)
		}
		if state.mode != renderedMode {
			test.Fatalf("%s: refusal changed the mode to %v", label, state.mode)
		}
		if owner.Editor.Selection() != selection || owner.Editor.Caret() != caret {
			test.Fatalf("%s: source selection changed: selection=%+v caret=%d", label, owner.Editor.Selection(), owner.Editor.Caret())
		}
		if state.rich.appliedEdits+state.rich.sourceHistoryOffset != cursor {
			test.Fatalf("%s: view cursor=%d, want %d", label, state.rich.appliedEdits+state.rich.sourceHistoryOffset, cursor)
		}
		if !slices.Equal(state.rich.crossUndo, wantUndo) || !slices.Equal(state.rich.crossRedo, wantRedo) {
			test.Fatalf("%s: rendered history stacks changed", label)
		}
		if owner.Editor.CanUndo() != canUndo || owner.Editor.CanRedo() != canRedo {
			test.Fatalf("%s: source undo/redo availability changed", label)
		}
		if stored, err := os.ReadFile(filename); err != nil || string(stored) != string(original) {
			test.Fatalf("%s: refusal wrote source before Save: %v", label, err)
		}
	}
	shortcut('z')
	refused("undo")
	shortcut('z')
	refused("retry undo")
	// Reactivating the same block must not revive the truncated unit, and the
	// refusal must leave its own visible caret alone.
	clickID(test, driver, renderedRows.At(1))
	if !state.rich.active || state.rich.input == nil || state.rich.input.String() != strings.Repeat("x", count) {
		test.Fatal("reactivation lost the rendered input")
	}
	state.rich.input.MoveTo(state.rich.input.RuneCount(), false)
	inputSelection, inputCaret := state.rich.input.Selection(), state.rich.input.Caret()
	selection, caret = owner.Editor.Selection(), owner.Editor.Caret()
	canUndo, canRedo = owner.Editor.CanUndo(), owner.Editor.CanRedo()
	wantUndo, wantRedo = slices.Clone(state.rich.crossUndo), slices.Clone(state.rich.crossRedo)
	cursor = state.rich.appliedEdits + state.rich.sourceHistoryOffset
	shortcut('z')
	refused("reactivated undo")
	if state.rich.input.String() != strings.Repeat("x", count) ||
		state.rich.input.Selection() != inputSelection || state.rich.input.Caret() != inputCaret {
		test.Fatal("refusal changed the rendered input value, selection or caret")
	}
	// A later independent edit keeps its own unit in front of the refused one.
	if err := driver.Type("y"); err != nil {
		test.Fatal(err)
	}
	typed := prefix + strings.Repeat("x", count) + "y" + suffix
	if owner.Text() != typed {
		test.Fatal("independent edit was not inserted")
	}
	shortcut('z')
	if owner.Text() != filled {
		test.Fatalf("independent undo also reverted the truncated unit: %d characters remain", len(owner.Text())-len(prefix+suffix))
	}
	shortcut('y')
	if owner.Text() != typed {
		test.Fatal("independent redo did not restore the later edit")
	}
	if stored, err := os.ReadFile(filename); err != nil || string(stored) != string(original) {
		test.Fatalf("history wrote source before Save: %v", err)
	}
}
