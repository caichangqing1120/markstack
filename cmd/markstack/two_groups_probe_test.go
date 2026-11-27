package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestRefillCapacityPreservesNewerCompleteGroup(test *testing.T) {
	for _, route := range []string{"source", "tab"} {
		test.Run(route, func(context *testing.T) {
			state, driver, filename, original := renderedSourceCollisionFixture(context, "First\n\nSolo\n\nLast\n", true, 740)
			owner, active := state.session.Current(), state.session.Active
			const prefix = "First\r\n\r\n"
			const suffix = "\r\n\r\nLast\r\n"
			older := strings.Repeat("x", maxRenderedHistory/2+1)
			newer := strings.Repeat("y", maxRenderedHistory/2)
			shortcut := func(key rune) {
				context.Helper()
				if handled, err := driver.ShortcutChord(primaryModifier(), key); err != nil || !handled {
					context.Fatalf("shortcut %q: handled=%v err=%v", key, handled, err)
				}
			}
			mustText := func(label, want string) {
				context.Helper()
				if state.session.Current() != owner || owner.Text() != want || state.mode != renderedMode {
					context.Fatalf("%s: got length %d, want %d, mode %v, status %q", label, len(owner.Text()), len(want), state.mode, state.message)
				}
			}
			typeGroup := func(sequence string) {
				context.Helper()
				for _, key := range sequence {
					if err := driver.Type(string(key)); err != nil {
						context.Fatal(err)
					}
				}
			}
			clickID(context, driver, renderedRows.At(1))
			shortcut('a')
			if err := driver.Key(gfx.KeyBackspace, 0); err != nil {
				context.Fatal(err)
			}
			mustText("clear", prefix+suffix)
			typeGroup(older)
			mustText("first group", prefix+older+suffix)
			if err := driver.Key(gfx.KeyLeft, 0); err != nil {
				context.Fatal(err)
			}
			typeGroup(newer)
			combined := prefix + older[:len(older)-1] + newer + "x" + suffix
			mustText("second group", combined)
			switch route {
			case "source":
				clickID(context, driver, editModeID)
			case "tab":
				neighbor := filepath.Join(context.TempDir(), "neighbor.txt")
				if err := os.WriteFile(neighbor, []byte("neighbor\r\n"), 0640); err != nil {
					context.Fatal(err)
				}
				if !state.openFile(neighbor) {
					context.Fatal(state.message)
				}
				state.invalidator.Invalidate()
				if err := driver.Pump(); err != nil {
					context.Fatal(err)
				}
				clickID(context, driver, tabRows.At(active))
			}
			if state.mode != renderedMode {
				clickID(context, driver, renderedModeID)
			}
			mustText("navigation", combined)
			shortcut('z')
			mustText("undo complete newer group", prefix+older+suffix)
			shortcut('z')
			mustText("refuse truncated older group", prefix+older+suffix)
			clickID(context, driver, readModeID)
			clickID(context, driver, renderedModeID)
			mustText("navigate with redo available", prefix+older+suffix)
			shortcut('y')
			mustText("redo newer group", combined)
			if stored, err := os.ReadFile(filename); err != nil || string(stored) != string(original) {
				context.Fatalf("history wrote to disk before Save: %v", err)
			}
		})
	}
}
