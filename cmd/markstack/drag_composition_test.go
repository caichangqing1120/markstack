package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func TestDropKeepsPendingComposition(test *testing.T) {
	owners := append(append([]compositionCancelCase{}, cancelCompositionCases...), sourceKindCompositionCases...)
	for _, owner := range owners {
		for _, kind := range []string{"file", "directory"} {
			for _, draft := range []string{"clean", "dirty"} {
				test.Run(owner.name+"/"+kind+"/"+draft, func(scenario *testing.T) {
					state, driver, filename, original, encode := openMenuSaveFixture(scenario, owner, "bom-crlf")
					dropped := scenario.TempDir()
					if kind == "file" {
						dropped = filepath.Join(dropped, "incoming.md")
						if err := os.WriteFile(dropped, []byte("# Incoming\n"), 0640); err != nil {
							scenario.Fatal(err)
						}
					}
					if draft == "dirty" {
						if err := driver.SelectText(0, 1); err != nil {
							scenario.Fatal(err)
						}
						if err := driver.Type("X"); err != nil {
							scenario.Fatal(err)
						}
					}
					if err := driver.SelectText(1, 0); err != nil {
						scenario.Fatal(err)
					}
					current, input := state.session.Current(), owner.editor(state)
					focus := driver.Snapshot().Focused
					if input == nil || focus == (gfx.ID{}) {
						scenario.Fatal("fixture did not focus a native editor")
					}
					committed, recovery := current.Editor.CommittedString(), state.session.RecoverySnapshot()
					before := app.TextStateOf(input)
					tabCount, workspace := len(state.session.Tabs), state.session.Root
					if err := driver.Compose("ni"); err != nil {
						scenario.Fatal(err)
					}
					composition := app.TextStateOf(input)
					if state.pendingCompositionFocus() != focus || current.Dirty() != (draft == "dirty") {
						scenario.Fatal("fixture has the wrong draft or composition owner")
					}
					requireNativeComposition(scenario, driver, focus, composition, "before drag")
					if accepted, err := driver.Drop(gfx.FileDropEvent{Phase: gfx.FileDropOver, X: 500, Y: 200}); err != nil || accepted {
						scenario.Errorf("composing drag-over = %v, %v; want refusal", accepted, err)
					}
					if accepted, err := dropAt(driver, 500, 200, dropped); err != nil || accepted {
						scenario.Errorf("accepted drop during composition = %v, %v", accepted, err)
					}
					if state.session.Current() != current || state.session.Pending() != nil || state.session.Root != workspace || len(state.session.Tabs) != tabCount || owner.editor(state) != input {
						scenario.Fatal("drop changed workspace, tabs, confirmation or editor")
					}
					requireNativeComposition(scenario, driver, focus, composition, "after refused drop")
					if err := driver.Compose("nihao"); err != nil {
						scenario.Fatal(err)
					}
					requireCompositionExtended(scenario, "after refused drop", composition, app.TextStateOf(input))
					if current.Editor.CommittedString() != committed || !reflect.DeepEqual(state.session.RecoverySnapshot(), recovery) {
						scenario.Fatal("drop changed the committed draft or its recovery snapshot")
					}
					if data, err := os.ReadFile(filename); err != nil || !bytes.Equal(data, encode(original)) {
						scenario.Fatalf("drop changed original disk bytes: %q, %v", data, err)
					}
					cancelCompositionFixture(scenario, state, owner)
					after := app.TextStateOf(input)
					if after.Value != before.Value || after.Selection != before.Selection || after.SelectionReversed != before.SelectionReversed || state.pendingCompositionFocus() != (gfx.ID{}) {
						scenario.Fatal("cancel did not restore the original text and directed selection")
					}
					if err := driver.Pump(); err != nil {
						scenario.Fatal(err)
					}
					if accepted, err := dropAt(driver, 500, 200, dropped); err != nil || !accepted {
						scenario.Fatalf("drop after cancel = %v, %v; want acceptance", accepted, err)
					}
				})
			}
		}
	}
}
