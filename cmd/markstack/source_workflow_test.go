package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"

	"markstack/internal/workspace"
)

func TestCodeFindReplaceAllUnicodeRegexUndoesAsOneEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.go")
	source := "// 名称1\r\n// 名称2\r\n// NAME3\r\n"
	if err := os.WriteFile(path, append([]byte{0xef, 0xbb, 0xbf}, []byte(source)...), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	state.openDocumentFind()
	state.findEditor.Insert(`名称[0-9]`)
	state.findRegex = true
	state.syncDocumentFind()
	if len(state.findMatches) != 2 {
		t.Fatalf("regex matches = %v", state.findMatches)
	}
	state.replaceEditor.Insert("项目")
	state.replaceDocumentMatches(true)
	want := "// 项目\r\n// 项目\r\n// NAME3\r\n"
	if got := state.session.Current().Text(); got != want {
		t.Fatalf("replacement = %q, want %q", got, want)
	}
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	if !state.session.Current().Editor.Undo() || state.session.Current().Text() != source {
		t.Fatal("replace all did not undo in one step")
	}
	if !state.session.Current().Editor.Redo() || state.session.Current().Text() != want {
		t.Fatal("replace all did not redo in one step")
	}
	if err := state.session.Save(state.session.Active, ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "\xef\xbb\xbf"+want {
		t.Fatalf("saved BOM/CRLF = %q, err=%v", data, err)
	}
}

func TestPlainFindCaseAndSelectedScopeDoNotReplaceOutsideSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(path, []byte("Alpha alpha ALPHA\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, _ := testDriver(t)
	state.openFile(path)
	state.session.Current().Editor.SetSelection(text.Range{Start: 6, End: 11})
	state.openDocumentFind()
	state.findEditor.Insert("alpha")
	state.findCaseSensitive = false
	state.findSelectedOnly = true
	state.syncDocumentFind()
	if len(state.findMatches) != 1 || state.findMatches[0] != 6 {
		t.Fatalf("selected scope matches = %v", state.findMatches)
	}
	state.replaceEditor.Insert("beta")
	state.replaceDocumentMatches(true)
	if got := state.session.Current().Text(); got != "Alpha beta ALPHA\n" {
		t.Fatalf("selected replacement = %q", got)
	}
	state.findSelectedOnly = false
	state.syncDocumentFind()
	if len(state.findMatches) != 2 {
		t.Fatalf("case-insensitive matches = %v", state.findMatches)
	}
}

func TestCanceledFindAndExternalConflictPreserveCodeSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "code.py")
	if err := os.WriteFile(path, []byte("print('one')\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, _ := testDriver(t)
	state.openFile(path)
	state.openDocumentFind()
	state.findEditor.Insert("one")
	state.syncDocumentFind()
	state.findOpen = false
	state.replaceEditor.Insert("two")
	state.replaceDocumentMatches(false)
	if state.session.Current().Dirty() {
		t.Fatal("canceled find changed source")
	}
	state.openDocumentFind()
	state.syncDocumentFind()
	state.replaceDocumentMatches(false)
	if err := os.WriteFile(path, []byte("external\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := state.session.Save(state.session.Active, ""); !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("save after external write = %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "external\n" {
		t.Fatalf("external file overwritten: %q", data)
	}
}

func TestSourceTabShiftTabPreservesSelectionAndUndo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.go")
	source := "名称 := 1\n\treturn 名称\nlast\n"
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	current := state.session.Current()
	current.Editor.SetSelection(text.Range{Start: 0, End: len([]rune("名称 := 1\n\treturn 名称\n"))})
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, sourceID)
	current.Editor.SetSelection(text.Range{Start: 0, End: len([]rune("名称 := 1\n\treturn 名称\n"))})
	if err := driver.Key(gfx.KeyTab, 0); err != nil {
		t.Fatal(err)
	}
	want := "\t名称 := 1\n\t\treturn 名称\nlast\n"
	if current.Text() != want || current.Editor.Selection().Start != 0 {
		t.Fatalf("indent = %q, selection=%+v", current.Text(), current.Editor.Selection())
	}
	if !current.Editor.Undo() || current.Text() != source {
		t.Fatal("indent did not undo")
	}
	if !current.Editor.Redo() || current.Text() != want {
		t.Fatal("indent did not redo")
	}
	current.Editor.SetSelection(text.Range{Start: 0, End: len([]rune("\t名称 := 1\n\t\treturn 名称\n"))})
	if err := driver.Key(gfx.KeyTab, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if current.Text() != source {
		t.Fatalf("outdent = %q", current.Text())
	}
}

func TestCodeFindReplaceControlsWorkAtMinimumWindowWidth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.ts")
	if err := os.WriteFile(path, []byte("const Name1 = 1;\nconst name2 = 2;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, findDocumentID)
	if state.sourceRect.Y < state.findRect.Y+state.findRect.Height {
		t.Fatalf("find bar overlaps source: find=%+v source=%+v", state.findRect, state.sourceRect)
	}
	for _, id := range []gfx.ID{findInputID, findPreviousID, findNextID, findCaseID, findRegexID, findCloseID, replaceInputID, findScopeID, replaceOneID, replaceAllID} {
		found := false
		for _, hit := range driver.Snapshot().Hits {
			if hit.ID == id {
				found = true
				if id == findScopeID || id == replaceOneID || id == replaceAllID {
					if !hit.Disabled {
						t.Fatalf("unavailable action %v is enabled", id)
					}
				}
				if hit.Bounds.X < 0 || hit.Bounds.X+hit.Bounds.Width > 740 || hit.Bounds.Y < 0 || hit.Bounds.Y+hit.Bounds.Height > 520 {
					t.Fatalf("find control outside minimum window: %+v", hit)
				}
			}
		}
		if !found {
			t.Fatalf("find control %v not visible", id)
		}
	}
	if err := driver.Type("name[0-9]"); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, findRegexID)
	clickID(t, driver, findCaseID)
	if len(state.findMatches) != 2 {
		t.Fatalf("UI regex/case matches = %v", state.findMatches)
	}
	for _, hit := range driver.Snapshot().Hits {
		if (hit.ID == replaceOneID || hit.ID == replaceAllID) && hit.Disabled {
			t.Fatalf("matching action %v is disabled", hit.ID)
		}
	}
	clickID(t, driver, replaceInputID)
	if err := driver.Type("item"); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, replaceOneID)
	if got := state.session.Current().Text(); got != "const item = 1;\nconst name2 = 2;\n" {
		t.Fatalf("UI replace one = %q", got)
	}
	if !state.session.Current().Editor.Undo() || state.session.Current().Text() != "const Name1 = 1;\nconst name2 = 2;\n" {
		t.Fatal("UI replace one did not undo in one step")
	}
	clickID(t, driver, replaceAllID)
	if got := state.session.Current().Text(); got != "const item = 1;\nconst item = 2;\n" {
		t.Fatalf("UI replace all = %q", got)
	}
	clickID(t, driver, findCloseID)
	if state.findOpen {
		t.Fatal("close button left find open")
	}
}

func TestInvalidSourceRegexDoesNotChangeDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(path, []byte("alpha\n"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.openFile(path)
	state.openDocumentFind()
	state.findEditor.Insert("[")
	state.findRegex = true
	state.syncDocumentFind()
	if state.findError == "" || len(state.findMatches) != 0 {
		t.Fatalf("invalid regex error=%q matches=%v", state.findError, state.findMatches)
	}
	state.replaceEditor.Insert("replaced")
	state.replaceDocumentMatches(true)
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != "alpha\n" || state.session.Current().Dirty() {
		t.Fatal("invalid regex modified source")
	}
	state.findEditor.SetSelection(text.Range{Start: 0, End: state.findEditor.RuneCount()})
	state.findEditor.Insert("alpha")
	state.syncDocumentFind()
	if state.findError != "" || state.message != "" || len(state.findMatches) != 1 {
		t.Fatalf("corrected regex retains error=%q message=%q matches=%v", state.findError, state.message, state.findMatches)
	}
}

func TestReplaceAllRefusesOversizedResultWithoutEditing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.txt")
	source := strings.Repeat("x", 9000)
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	state, _ := testDriver(t)
	state.openFile(path)
	state.openDocumentFind()
	state.findEditor.Insert("x")
	state.replaceEditor.Insert(strings.Repeat("y", 2000))
	state.syncDocumentFind()
	state.replaceDocumentMatches(true)
	if state.session.Current().Text() != source || state.session.Current().Dirty() || state.message == "" {
		t.Fatalf("oversized replace changed source: dirty=%v message=%q", state.session.Current().Dirty(), state.message)
	}
}
