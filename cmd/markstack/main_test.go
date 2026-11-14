package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func testDriver(t *testing.T) (*model, *app.Driver) {
	t.Helper()
	state := newModel()
	var canvas gfx.Canvas
	driver, err := app.NewDriver(newConfig(state), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	return state, driver
}

func clickID(t *testing.T, driver *app.Driver, id gfx.ID) {
	t.Helper()
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == id {
			if err := driver.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	if id == openFolderID || id == openFileID || id == saveID || id == saveAsID {
		clickID(t, driver, fileMenuID)
		clickID(t, driver, id)
		return
	}
	t.Fatalf("control %v is not visible", id)
}

func hasText(driver *app.Driver, value string) bool {
	for _, label := range driver.Snapshot().Labels {
		if strings.Contains(label.Value, value) {
			return true
		}
	}
	return false
}

func TestOpenWorkspaceAndSwitchViews(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sample.md")
	if err := os.WriteFile(path, []byte("# A native note\n\nBody text."), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	if err := driver.QueueOpenDirectory(root, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, openFolderID)
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if state.session.Root != canonicalRoot || !hasText(driver, "sample.md") {
		t.Fatalf("workspace root=%q, nodes=%+v", state.session.Root, state.session.Nodes)
	}
	clickID(t, driver, fileRows.At(0))
	canonicalPath := filepath.Join(canonicalRoot, "sample.md")
	if state.session.Current().Path != canonicalPath {
		t.Fatalf("opened %q, want %q", state.session.Current().Path, canonicalPath)
	}
	clickID(t, driver, splitModeID)
	if state.mode != splitMode || !hasText(driver, "A native note") {
		t.Fatal("split preview did not render")
	}
	clickID(t, driver, readModeID)
	if state.mode != readMode {
		t.Fatal("read mode did not activate")
	}
	clickID(t, driver, editModeID)
	if state.mode != editMode {
		t.Fatal("edit mode did not activate")
	}
}

func TestTypingAndSaveAs(t *testing.T) {
	state, driver := testDriver(t)
	clickID(t, driver, sourceID)
	if err := driver.Type("# Draft\n\nNative editing"); err != nil {
		t.Fatal(err)
	}
	if !state.session.Current().Dirty() || !strings.Contains(state.session.Current().Text(), "Native editing") {
		t.Fatal("text entry did not update document")
	}
	path := filepath.Join(t.TempDir(), "draft.md")
	if err := driver.QueueSaveFile(path, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, saveID)
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "Native editing") || state.session.Current().Dirty() {
		t.Fatalf("save = %q, %v, dirty=%v", data, err, state.session.Current().Dirty())
	}
}

func TestUnsavedCloseCanBeCanceledOrDiscarded(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("unsaved")
	if err := driver.Resize(1100, 740); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, tabClose.At(0))
	if state.session.Pending() == nil {
		t.Fatal("dirty tab closed without confirmation")
	}
	clickID(t, driver, cancelConfirmID)
	if state.session.Pending() != nil || state.session.Current().Text() != "unsaved" {
		t.Fatal("cancel discarded unsaved text")
	}
	clickID(t, driver, tabClose.At(0))
	clickID(t, driver, discardConfirmID)
	if state.session.Current().Text() != "" {
		t.Fatal("discard did not close the old tab")
	}
}

func TestFilenameFilterAndOutlineNavigation(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"notes.md": "# First\n\n## Second\n\nText", "other.md": "# Other",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	state, driver := testDriver(t)
	if err := driver.QueueOpenDirectory(root, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, openFolderID)
	clickID(t, driver, searchID)
	if err := driver.Type("notes"); err != nil {
		t.Fatal(err)
	}
	if len(state.visible) != 1 || state.visible[0].Name != "notes.md" {
		t.Fatalf("visible files after search: %+v", state.visible)
	}
	clickID(t, driver, fileRows.At(0))
	clickID(t, driver, outlineToggleID)
	if len(state.outlines) != 2 {
		t.Fatalf("outline: %+v", state.outlines)
	}
	clickID(t, driver, outlineRows.At(1))
	if state.session.Current().Editor.Caret() != 9 {
		t.Fatalf("outline caret = %d, want start of second heading", state.session.Current().Editor.Caret())
	}
}

func TestNarrowSplitKeepsBothEditorAndPreview(t *testing.T) {
	state, driver := testDriver(t)
	state.session.Current().Replace("# Both panes")
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, splitModeID)
	if state.sourceRect.Width <= 0 || state.previewRect.Width <= 0 {
		t.Fatalf("narrow split lost a pane: source=%+v preview=%+v", state.sourceRect, state.previewRect)
	}
}

func TestFilteringClampsStaleTreeScroll(t *testing.T) {
	root := t.TempDir()
	for index := range 60 {
		name := filepath.Join(root, "note-"+strconv.Itoa(index)+".md")
		if err := os.WriteFile(name, []byte("# Note"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	state, driver := testDriver(t)
	if err := driver.QueueOpenDirectory(root, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, openFolderID)
	state.treeScroll = 900
	state.search.Insert("note-59")
	if err := driver.Resize(1100, 740); err != nil {
		t.Fatal(err)
	}
	if len(state.visible) != 1 || state.treeScroll != 0 {
		t.Fatalf("filtered tree stays scrolled away: visible=%d scroll=%v", len(state.visible), state.treeScroll)
	}
}

func TestLargeWindowKeepsTyporaLikeSidebarProportion(t *testing.T) {
	state, driver := testDriver(t)
	if err := driver.Resize(1605, 955); err != nil {
		t.Fatal(err)
	}
	if state.treeRect.Width < 250 || state.treeRect.Width > 280 {
		t.Fatalf("sidebar content width = %v at a 1605px window", state.treeRect.Width)
	}
}

func TestOpenStandaloneFileDoesNotShowEmptyWorkspacePrompt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "standalone.md")
	if err := os.WriteFile(path, []byte("# Standalone"), 0644); err != nil {
		t.Fatal(err)
	}
	_, driver := testDriver(t)
	if err := driver.QueueOpenFiles([]string{path}, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, openFileID)
	if hasText(driver, "打开文件夹开始整理") {
		t.Fatal("standalone document still shows an empty-workspace prompt")
	}
}
