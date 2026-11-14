package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestRenderedMixedHistorySharesSourceCapacity(test *testing.T) {
	const prefix = "<!-- untouched -->\n\n"
	const suffix = "\n\nTail **keep** &amp; <!-- opaque -->\n"
	state, driver, filename, _ := renderedSourceCollisionFixture(test, prefix+"abcd"+suffix, false, 1200)
	clickID(test, driver, renderedRows.At(1))
	state.rich.input.MoveTo(2, false)
	type stage struct {
		body  string
		row   int
		caret int
	}
	stages := []stage{{"abcd", 1, 2}}
	check := func(wanted stage) {
		test.Helper()
		if state.mode != renderedMode || !state.rich.active || state.rich.index != wanted.row || state.rich.input.Caret() != wanted.caret || driver.Snapshot().Focused != richInputID {
			test.Fatalf("capacity replay lost rendered position: mode=%v active=%v row=%d, want row=%d caret=%d", state.mode, state.rich.active, state.rich.index, wanted.row, wanted.caret)
		}
		source := strings.ReplaceAll(prefix+wanted.body+suffix, "\n", "\r\n")
		if state.session.Current().Text() != source {
			test.Fatalf("capacity replay changed source: %q, want %q", state.session.Current().Text(), source)
		}
	}
	record := func(body string, row, caret int) {
		test.Helper()
		wanted := stage{body, row, caret}
		check(wanted)
		stages = append(stages, wanted)
		cursor := state.rich.appliedEdits + state.rich.sourceHistoryOffset
		if cursor != len(stages)-1 {
			test.Fatalf("view history cursor=%d, want %d", cursor, len(stages)-1)
		}
		for _, edit := range state.rich.structureUndo {
			if edit.position <= cursor-256 || edit.position > cursor {
				test.Fatal("structure metadata retained a transaction outside the source history window")
			}
		}
		for _, edit := range state.rich.crossUndo {
			if edit.position <= cursor-256 || edit.position > cursor {
				test.Fatal("format metadata retained a transaction outside the source history window")
			}
		}
	}
	heading := ""
	for cycle := 0; cycle < 52; cycle++ {
		state.rich.input.MoveTo(2, false)
		if err := driver.Key(gfx.KeyEnter, 0); err != nil {
			test.Fatal(err)
		}
		record(heading+"ab\n\ncd", 2, 0)
		if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
			test.Fatal(err)
		}
		record(heading+"abcd", 1, 2)
		level := 1 + cycle%2
		state.applyHeadingLevel(level)
		if err := driver.Pump(); err != nil {
			test.Fatal(err)
		}
		heading = strings.Repeat("#", level) + " "
		record(heading+"abcd", 1, 2)
		if err := driver.Type("P"); err != nil {
			test.Fatal(err)
		}
		record(heading+"abPcd", 1, 3)
		if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
			test.Fatal(err)
		}
		record(heading+"abcd", 1, 2)
	}
	for _, command := range []rune{'z', 'y'} {
		for operation := 1; operation <= 256; operation++ {
			if _, err := driver.ShortcutChord(primaryModifier(), command); err != nil {
				test.Fatal(err)
			}
			position := 260 - operation
			if command == 'y' {
				position = 4 + operation
			}
			check(stages[position])
			if state.rich.appliedEdits+state.rich.sourceHistoryOffset != position {
				test.Fatalf("capacity cursor lost unit %c/%d", command, operation)
			}
		}
		if command == 'z' && (state.session.Current().Editor.CanUndo() || len(state.rich.structureUndo) != 0 || len(state.rich.crossUndo) != 0) {
			test.Fatal("view metadata outlived source undo capacity")
		}
		if command == 'y' && (state.session.Current().Editor.CanRedo() || len(state.rich.structureRedo) != 0 || len(state.rich.crossRedo) != 0) {
			test.Fatal("view metadata invented additional source redo units")
		}
		body := stages[260]
		if command == 'z' {
			body = stages[4]
		}
		wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(strings.ReplaceAll(prefix+body.body+suffix, "\n", "\r\n"))...)
		expectRenderedCollisionSave(test, state, driver, filename, wanted)
	}
}

func TestRenderedOwnedHistoryRejectsStaleIdentity(test *testing.T) {
	for _, kind := range []string{"structure", "format"} {
		for _, stale := range []string{"source", "document", "position"} {
			test.Run(fmt.Sprintf("%s/%s", kind, stale), func(context *testing.T) {
				state, driver, _, _ := renderedSourceCollisionFixture(context, "abcd\n", false, 1200)
				clickID(context, driver, renderedRows.At(0))
				state.rich.input.MoveTo(2, false)
				if kind == "structure" {
					if err := driver.Key(gfx.KeyEnter, 0); err != nil {
						context.Fatal(err)
					}
				} else {
					state.applyBoldBlock()
					if err := driver.Pump(); err != nil {
						context.Fatal(err)
					}
				}
				current := state.session.Current()
				switch stale {
				case "source":
					current.Editor.Insert("external")
				case "document":
					state.rich.documentID++
				case "position":
					state.rich.sourceHistoryOffset++
				}
				before, selection, caret := current.Text(), current.Editor.Selection(), current.Editor.Caret()
				canUndo, canRedo := current.Editor.CanUndo(), current.Editor.CanRedo()
				var handled bool
				if kind == "structure" {
					_, handled = state.stepStructureHistory(true)
				} else {
					_, handled = state.stepCrossHistory(true, false)
				}
				if handled || current.Text() != before || current.Editor.Selection() != selection || current.Editor.Caret() != caret || current.Editor.CanUndo() != canUndo || current.Editor.CanRedo() != canRedo {
					context.Fatal("stale owned view metadata consumed authoritative source history")
				}
				if stale == "document" {
					if _, err := state.onRenderedTextEvent(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextUndo}); err != nil {
						context.Fatal(err)
					}
					if current.Text() != before || current.Editor.Selection() != selection || current.Editor.Caret() != caret || current.Editor.CanUndo() != canUndo || current.Editor.CanRedo() != canRedo {
						context.Fatal("a stale rendered document consumed another document's source history")
					}
				}
			})
		}
	}
}
