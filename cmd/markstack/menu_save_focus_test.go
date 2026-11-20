package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"

	"markstack/internal/workspace"
)

func TestMenuSaveImmediateUndoKeepsCodeFocus(test *testing.T) {
	directory := test.TempDir()
	for _, name := range []string{"SETTINGS", "RECOVERY", "SESSION", "RECENT"} {
		test.Setenv("MARKSTACK_"+name+"_PATH", filepath.Join(directory, name+".json"))
	}
	owner := sourceKindCompositionCases[0]
	state, driver, _, _ := owner.open(test)
	if driver.Snapshot().Focused != sourceID {
		test.Fatal("code editor did not take native focus")
	}
	original := state.session.Current().Text()
	if err := driver.SelectText(0, 1); err != nil {
		test.Fatal(err)
	}
	if err := driver.Type("X"); err != nil {
		test.Fatal(err)
	}
	clickID(test, driver, fileMenuID)
	clickID(test, driver, saveID)
	if err := driver.Pump(); err != nil {
		test.Fatal(err)
	}
	if driver.Snapshot().Focused != sourceID {
		test.Fatalf("menu Save left focus on %v, want code editor", driver.Snapshot().Focused)
	}
	if handled, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil || !handled || state.session.Current().Text() != original {
		test.Fatalf("immediate Undo handled=%v err=%v source=%q", handled, err, state.session.Current().Text())
	}
	if handled, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil || !handled {
		test.Fatalf("immediate Redo handled=%v err=%v", handled, err)
	}
}

func openMenuSaveFixture(test *testing.T, owner compositionCancelCase, encoding string) (*model, *app.Driver, string, string, func(string) []byte) {
	test.Helper()
	directory := test.TempDir()
	for _, key := range []string{"SETTINGS", "RECOVERY", "SESSION", "RECENT"} {
		test.Setenv("MARKSTACK_"+key+"_PATH", filepath.Join(directory, strings.ToLower(key)+".json"))
	}
	body := owner.source
	if owner.fixture != "" {
		data, err := os.ReadFile(filepath.Join("..", "..", "internal", "richedit", "testdata", owner.fixture))
		if err != nil {
			test.Fatal(err)
		}
		body = strings.TrimPrefix(string(data), "\ufeff")
	}
	if encoding == "bom-crlf" {
		body = strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n")
	}
	encode := func(value string) []byte {
		if encoding == "bom-crlf" {
			return []byte("\ufeff" + value)
		}
		return []byte(value)
	}
	filename := filepath.Join(directory, "sample"+owner.ext)
	if err := os.WriteFile(filename, encode(body), 0640); err != nil {
		test.Fatal(err)
	}
	state, driver := testDriver(test)
	if err := driver.QueueOpenFiles([]string{filename}, nil); err != nil {
		test.Fatal(err)
	}
	clickID(test, driver, openFileID)
	state.outline, state.mode = true, renderedMode
	if err := driver.Resize(1201, 780); err != nil {
		test.Fatal(err)
	}
	if owner.sourceMode {
		clickID(test, driver, editModeID)
	}
	owner.activate(test, state, driver)
	return state, driver, filename, body, encode
}

func TestMenuSaveRestoresOrdinaryEditorFocus(test *testing.T) {
	owners := append(append([]compositionCancelCase{}, cancelCompositionCases...), sourceKindCompositionCases...)
	for _, owner := range owners {
		for _, encoding := range []string{"lf", "bom-crlf"} {
			for _, operation := range []string{"save", "save-as", "cancel", "dialog-error", "kind-error", "conflict", "keyboard", "menu-keyboard", "menu-keyboard-as", "menu-keyboard-cancel"} {
				test.Run(owner.name+"/"+encoding+"/"+operation, func(context *testing.T) {
					state, driver, filename, original, encode := openMenuSaveFixture(context, owner, encoding)
					current, input := state.session.Current(), owner.editor(state)
					focus := compositionFocusByCase[owner.name]
					if driver.Snapshot().Focused != focus || input == nil {
						context.Fatal("fixture input is not focused")
					}
					if err := driver.SelectText(0, 1); err != nil {
						context.Fatal(err)
					}
					if err := driver.Type("X"); err != nil {
						context.Fatal(err)
					}
					changed := owner.commitSource
					if changed == "" {
						_, size := utf8.DecodeRuneInString(original)
						changed = "X" + original[size:]
					}
					if encoding == "bom-crlf" {
						changed = strings.ReplaceAll(strings.ReplaceAll(changed, "\r\n", "\n"), "\n", "\r\n")
					}
					if current.Text() != changed {
						context.Fatalf("fixture edit=%q want=%q", current.Text(), changed)
					}
					selection, caret := input.Selection(), input.Caret()
					sourceInput, documentID := current.Editor, current.ID
					target := filename
					diskExpected := original
					control := saveAsID
					switch operation {
					case "save", "keyboard", "menu-keyboard":
						control, diskExpected = saveID, changed
					case "save-as", "menu-keyboard-as":
						target = filepath.Join(filepath.Dir(filename), "copy"+owner.ext)
						if err := driver.QueueSaveFile(target, nil); err != nil {
							context.Fatal(err)
						}
					case "cancel", "menu-keyboard-cancel":
						if err := driver.QueueSaveFile("", app.ErrCanceled); err != nil {
							context.Fatal(err)
						}
					case "dialog-error":
						if err := driver.QueueSaveFile("", errors.New("test picker failure")); err != nil {
							context.Fatal(err)
						}
					case "kind-error":
						if err := driver.QueueSaveFile(filepath.Join(filepath.Dir(filename), "copy.unknown"), nil); err != nil {
							context.Fatal(err)
						}
					case "conflict":
						control, diskExpected = saveID, "external\r\n"
						if err := os.WriteFile(filename, encode(diskExpected), 0640); err != nil {
							context.Fatal(err)
						}
					}
					if strings.HasPrefix(operation, "menu-keyboard") {
						clickID(context, driver, fileMenuID)
						modifiers := primaryModifier()
						if operation != "menu-keyboard" {
							modifiers |= gfx.ModifierShift
						}
						if _, err := driver.ShortcutChord(modifiers, 's'); err != nil {
							context.Fatal(err)
						}
					} else if operation == "keyboard" {
						if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
							context.Fatal(err)
						}
					} else {
						clickID(context, driver, fileMenuID)
						clickID(context, driver, control)
					}
					if err := driver.Pump(); err != nil {
						context.Fatal(err)
					}
					if state.fileMenuOpen || state.session.Current() != current || current.ID != documentID || current.Editor != sourceInput || owner.editor(state) != input || input.Selection() != selection || input.Caret() != caret {
						context.Fatal("save changed editor/document/selection identity")
					}
					if driver.Snapshot().Focused != focus {
						context.Fatalf("closed menu retained focus %v, want editor %v", driver.Snapshot().Focused, focus)
					}
					if data, err := os.ReadFile(filename); err != nil || !bytes.Equal(data, encode(diskExpected)) {
						context.Fatalf("original-path bytes=%q want=%q err=%v", data, encode(diskExpected), err)
					}
					if info, err := os.Stat(filename); err != nil || info.Mode().Perm() != 0640 {
						context.Fatal("original permissions changed")
					}
					if operation == "save-as" || operation == "menu-keyboard-as" {
						if current.Path != target {
							context.Fatal("Save As did not update the same document path")
						}
						if data, err := os.ReadFile(target); err != nil || !bytes.Equal(data, encode(changed)) {
							context.Fatal("Save As bytes are not committed source")
						}
					}
					if operation == "conflict" && state.message != workspace.ErrConflict.Error() {
						context.Fatalf("conflict error=%q", state.message)
					}
					if handled, err := driver.ShortcutChord(primaryModifier(), 'z'); err != nil || !handled || current.Text() != original {
						context.Fatalf("immediate Undo handled=%v err=%v source=%q", handled, err, current.Text())
					}
					if handled, err := driver.ShortcutChord(primaryModifier()|gfx.ModifierShift, 'z'); err != nil || !handled || current.Text() != changed {
						context.Fatalf("immediate Redo handled=%v err=%v source=%q", handled, err, current.Text())
					}
					if err := driver.Type("Y"); err != nil {
						context.Fatal(err)
					}
					if current.Text() == changed || driver.Snapshot().Focused != focus {
						context.Fatal("continued typing did not reach the original editing owner")
					}
				})
			}
		}
	}
}

func TestMenuSaveDoesNotStealFindOrSearchFocus(test *testing.T) {
	for _, control := range []gfx.ID{findInputID, replaceInputID, searchID} {
		test.Run(fmt.Sprint(control), func(context *testing.T) {
			state, driver, _, _, _ := openMenuSaveFixture(context, sourceKindCompositionCases[0], "lf")
			if control == searchID {
				if _, err := driver.ShortcutChord(primaryModifier(), 'f'); err != nil {
					context.Fatal(err)
				}
			} else {
				clickID(context, driver, findDocumentID)
				clickID(context, driver, control)
			}
			if driver.Snapshot().Focused != control {
				context.Fatal("non-document fixture focus is missing")
			}
			clickID(context, driver, fileMenuID)
			clickID(context, driver, saveID)
			if err := driver.Pump(); err != nil {
				context.Fatal(err)
			}
			if driver.Snapshot().Focused == sourceID || !state.findOpen && control != searchID {
				context.Fatal("save stole focus or hid the Find bar")
			}
		})
	}
}

func TestMenuSaveDoesNotRestoreStaleFocus(test *testing.T) {
	for _, scenario := range []string{"document", "mode", "input", "source-input", "inactive", "document-id", "other-control"} {
		test.Run(scenario, func(context *testing.T) {
			state, driver, _, _, _ := openMenuSaveFixture(context, cancelCompositionCases[0], "lf")
			state.rememberFileMenuEditingFocus(richInputID)
			if state.fileMenuReturnFocus.editor == nil {
				context.Fatal("fixture editing focus was not captured")
			}
			state.focusRequest = gfx.ID{}
			switch scenario {
			case "document":
				state.session.NewDocument()
				state.mode = editMode
				if err := driver.QueueSaveFile("", app.ErrCanceled); err != nil {
					context.Fatal(err)
				}
			case "mode":
				state.mode = readMode
			case "input":
				state.rich.input = text.NewEditor(state.rich.input.String())
			case "source-input":
				current := state.session.Current()
				current.Editor = text.NewEditor(current.Text())
			case "inactive":
				state.rich.active = false
			case "document-id":
				state.rich.documentID++
			case "other-control":
				state.rememberFileMenuEditingFocus(findInputID)
			}
			state.saveFromFileMenu(false)
			if !state.focusRequest.IsZero() {
				context.Fatalf("save restored a stale editor: %v", state.focusRequest)
			}
		})
	}
}
