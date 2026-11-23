package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestMenuExportRestoresEditingFocus(test *testing.T) {
	requirePDFFont(test)
	for _, owner := range cancelCompositionCases {
		for _, format := range []struct {
			name   string
			item   gfx.ID
			prefix string
		}{
			{"html", menuExportHTMLID, "<!doctype html>"},
			{"pdf", menuExportPDFID, "%PDF-"},
		} {
			for _, result := range []struct {
				name string
				err  error
			}{
				{"accept", nil},
				{"cancel", app.ErrCanceled},
				{"error", errors.New("test picker failure")},
			} {
				test.Run(owner.name+"/"+format.name+"/"+result.name, func(context *testing.T) {
					state, driver, source, original, encode := openMenuSaveFixture(context, owner, "bom-crlf")
					current, input := state.session.Current(), owner.editor(state)
					focus := compositionFocusByCase[owner.name]
					if input == nil || driver.Snapshot().Focused != focus {
						context.Fatal("fixture input is not focused")
					}
					if err := driver.SelectText(0, 1); err != nil {
						context.Fatal(err)
					}
					if err := driver.Type("X"); err != nil {
						context.Fatal(err)
					}
					changed, recovery := current.Text(), state.session.RecoverySnapshot()
					if changed == original || !current.Dirty() {
						context.Fatal("fixture did not produce a committed draft")
					}
					selection, caret := input.Selection(), input.Caret()
					target := filepath.Join(filepath.Dir(source), "export."+format.name)
					path := target
					if result.err != nil {
						path = ""
					}
					if err := driver.QueueSaveFile(path, result.err); err != nil {
						context.Fatal(err)
					}
					clickID(context, driver, fileMenuID)
					clickID(context, driver, format.item)
					if err := driver.Pump(); err != nil {
						context.Fatal(err)
					}
					if state.fileMenuOpen || state.session.Current() != current || current.Path != source || owner.editor(state) != input || input.Selection() != selection || input.Caret() != caret {
						context.Fatal("export changed document, editor identity or selection")
					}
					if current.Text() != changed || !reflect.DeepEqual(state.session.RecoverySnapshot(), recovery) {
						context.Fatal("export changed the committed draft or recovery")
					}
					if data, err := os.ReadFile(source); err != nil || !bytes.Equal(data, encode(original)) {
						context.Fatalf("export changed source bytes: %v", err)
					}
					if info, err := os.Stat(source); err != nil || info.Mode().Perm() != 0640 {
						context.Fatal("export changed source permissions")
					}
					if result.err == nil {
						if output, err := os.ReadFile(target); err != nil || !bytes.HasPrefix(output, []byte(format.prefix)) {
							context.Fatalf("missing export output: %v, %q", err, state.message)
						}
					} else if _, err := os.Stat(target); !os.IsNotExist(err) {
						context.Fatalf("canceled or failed picker wrote a target: %v", err)
					}
					if result.name == "error" && state.message != result.err.Error() {
						context.Fatalf("picker error was not reported: %q", state.message)
					}
					if got := driver.Snapshot().Focused; got != focus {
						context.Fatalf("export left focus on %v, want %v", got, focus)
					}
					if handled, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil || !handled || current.Text() != original {
						context.Fatalf("Undo after export handled=%v err=%v source=%q", handled, err, current.Text())
					}
					if handled, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil || !handled || current.Text() != changed {
						context.Fatalf("Redo after export handled=%v err=%v source=%q", handled, err, current.Text())
					}
					if err := driver.Type("Y"); err != nil || current.Text() == changed || driver.Snapshot().Focused != focus {
						context.Fatalf("typing did not reach the existing editor: %v", err)
					}
				})
			}
		}
	}
}

func TestMenuExportDoesNotStealFindFocus(test *testing.T) {
	owner := cancelCompositionCases[len(cancelCompositionCases)-1]
	state, driver, _, _, _ := openMenuSaveFixture(test, owner, "lf")
	clickID(test, driver, findDocumentID)
	clickID(test, driver, findInputID)
	if got := driver.Snapshot().Focused; got != findInputID {
		test.Fatalf("Find fixture has focus %v", got)
	}
	if err := driver.QueueSaveFile("", app.ErrCanceled); err != nil {
		test.Fatal(err)
	}
	clickID(test, driver, fileMenuID)
	clickID(test, driver, menuExportHTMLID)
	if err := driver.Pump(); err != nil {
		test.Fatal(err)
	}
	if _, requested := driver.LastSaveFileRequest(); !requested {
		test.Fatal("export did not open the picker")
	}
	if !state.findOpen || state.fileMenuEditor(driver.Snapshot().Focused) != nil {
		test.Fatal("export closed Find or moved focus into an editor")
	}
}

func TestFileMenuEditingActionDoesNotRestoreReplacedEditor(test *testing.T) {
	state, _, _, _, _ := openMenuSaveFixture(test, cancelCompositionCases[0], "lf")
	state.rememberFileMenuEditingFocus(richInputID)
	if state.fileMenuReturnFocus.editor == nil {
		test.Fatal("fixture editing focus was not captured")
	}
	state.runFileMenuEditingAction(func() {
		state.rich.input = text.NewEditor(state.rich.input.String())
	})
	if !state.focusRequest.IsZero() {
		test.Fatal("menu action restored focus to a replacement editor")
	}
}

func TestMenuExportRefusalKeepsCompositionFocus(test *testing.T) {
	for _, owner := range cancelCompositionCases {
		for _, format := range []struct {
			name string
			item gfx.ID
		}{{"HTML", menuExportHTMLID}, {"PDF", menuExportPDFID}} {
			test.Run(owner.name+"/"+format.name, func(context *testing.T) {
				state, driver, source, original, encode := openMenuSaveFixture(context, owner, "bom-crlf")
				current, input := state.session.Current(), owner.editor(state)
				if err := driver.Compose("ni"); err != nil {
					context.Fatal(err)
				}
				before, recovery := app.TextStateOf(input), state.session.RecoverySnapshot()
				if !before.Composing {
					context.Fatal("fixture composition did not start")
				}
				focus := compositionFocusByCase[owner.name]
				clickID(context, driver, fileMenuID)
				clickID(context, driver, format.item)
				if state.fileMenuOpen || !strings.Contains(state.message, "再导出 "+format.name) {
					context.Fatalf("unexpected export refusal: %q", state.message)
				}
				if _, requested := driver.LastSaveFileRequest(); requested {
					context.Fatal("composition reached the export picker")
				}
				pumpQueuedFrame(context, driver, format.name)
				requireNativeComposition(context, driver, focus, before, format.name)
				if err := driver.Compose("nihao"); err != nil {
					context.Fatal(err)
				}
				requireCompositionExtended(context, format.name, before, app.TextStateOf(input))
				if current.Editor.CommittedString() != original || !reflect.DeepEqual(state.session.RecoverySnapshot(), recovery) {
					context.Fatal("preedit changed the committed document or recovery")
				}
				if data, err := os.ReadFile(source); err != nil || !bytes.Equal(data, encode(original)) {
					context.Fatalf("preedit changed source bytes: %v", err)
				}
			})
		}
	}
}
