package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestSourceCompositionIsNotDirtyOrSavableUntilCommit(t *testing.T) {
	for _, extension := range []string{"md", "go", "txt"} {
		t.Run(extension, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "note."+extension)
			if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
				t.Fatal(err)
			}
			state, driver := testDriver(t)
			state.openFile(path)
			if err := driver.Resize(1200, 780); err != nil {
				t.Fatal(err)
			}
			clickID(t, driver, sourceID)
			state.session.Current().Editor.MoveTo(3, false)
			if err := driver.Compose("pin"); err != nil {
				t.Fatal(err)
			}
			if state.session.Current().Text() != "oldpin" || state.session.Current().Dirty() {
				t.Fatalf("marked text=%q dirty=%v", state.session.Current().Text(), state.session.Current().Dirty())
			}
			if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(state.message, "composition") {
				t.Fatalf("Save did not reject preedit: %q", state.message)
			}
			if bytes, err := os.ReadFile(path); err != nil || string(bytes) != "old" {
				t.Fatalf("preedit saved to disk: %q, %v", bytes, err)
			}
			if err := driver.Type("新"); err != nil {
				t.Fatal(err)
			}
			if state.session.Current().Text() != "old新" || !state.session.Current().Dirty() {
				t.Fatalf("committed text=%q dirty=%v", state.session.Current().Text(), state.session.Current().Dirty())
			}
			if err := driver.Key(gfx.KeyEscape, 0); err != nil {
				t.Fatal(err)
			}
		})
	}
}
