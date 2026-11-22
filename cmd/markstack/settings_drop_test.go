package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"

	"markstack/internal/session"
)

func TestSettingsRefuseBackgroundFileDrops(test *testing.T) {
	for _, kind := range []string{"file", "directory"} {
		test.Run(kind, func(scenario *testing.T) {
			path := filepath.Join(scenario.TempDir(), "original.md")
			original := []byte("\xef\xbb\xbf# Original\r\n")
			if err := os.WriteFile(path, original, 0640); err != nil {
				scenario.Fatal(err)
			}
			dropped := scenario.TempDir()
			if kind == "file" {
				dropped = filepath.Join(dropped, "dropped.md")
				if err := os.WriteFile(dropped, []byte("# Dropped\n"), 0640); err != nil {
					scenario.Fatal(err)
				}
			}
			state, driver := testDriver(scenario)
			store := session.PreferencesStore{Path: filepath.Join(scenario.TempDir(), "private", "settings.json")}
			if err := state.enablePreferences(store); err != nil {
				scenario.Fatal(err)
			}
			if !state.openFile(path) {
				scenario.Fatal(state.message)
			}
			current := state.session.Current()
			current.Replace("# Local draft\r\n")
			current.Editor.SetSelection(text.Range{Start: 2, End: 7})
			selection := current.Editor.Selection()
			tabCount, workspace := len(state.session.Tabs), state.session.Root
			if err := driver.Resize(1200, 780); err != nil {
				scenario.Fatal(err)
			}
			clickID(scenario, driver, fileMenuID)
			clickID(scenario, driver, menuSettingsID)
			clickID(scenario, driver, appearanceDarkID)
			preferences, focus := state.preferences, driver.Snapshot().Focused
			if !state.settingsOpen || !preferences.Dark {
				scenario.Fatal("settings did not open and persist the chosen appearance")
			}
			if accepted, err := driver.Drop(gfx.FileDropEvent{Phase: gfx.FileDropOver, X: 500, Y: 200}); err != nil || accepted {
				scenario.Errorf("settings drop-over = %v, %v; want refusal", accepted, err)
			}
			if accepted, err := dropAt(driver, 500, 200, dropped); err != nil || accepted {
				scenario.Errorf("settings accepted background %s drop: %v, %v", kind, accepted, err)
			}
			if !state.settingsOpen || driver.Snapshot().Focused != focus || state.session.Pending() != nil || state.session.Root != workspace || len(state.session.Tabs) != tabCount || state.session.Current() != current {
				scenario.Fatal("drop changed modal focus, workspace, tabs or the active document")
			}
			if current.Text() != "# Local draft\r\n" || current.Editor.Selection() != selection || !current.Dirty() || !current.Editor.CanUndo() {
				scenario.Fatal("drop changed the draft, selection or history")
			}
			if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, original) {
				scenario.Fatalf("drop changed original bytes: %q, %v", data, err)
			}
			if stored, err := store.Load(); err != nil || stored != preferences || state.preferences != preferences {
				scenario.Fatalf("drop changed preferences: %+v, %v", stored, err)
			}
			if err := driver.Key(gfx.KeyEscape, 0); err != nil || state.settingsOpen {
				scenario.Fatalf("Escape did not close settings: %v", err)
			}
			if accepted, err := dropAt(driver, 500, 200, dropped); err != nil || !accepted {
				scenario.Fatalf("drop after closing settings = %v, %v; want acceptance", accepted, err)
			}
		})
	}
}
