package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"markstack/internal/session"
	"markstack/internal/workspace"
)

func testRecentStore(t *testing.T) session.RecentStore {
	t.Helper()
	return session.RecentStore{Path: filepath.Join(t.TempDir(), "private", "recent.json")}
}

func TestRecentNavigationRecordsSuccessfulFileAndWorkspaceOpens(t *testing.T) {
	root := t.TempDir()
	first, second := filepath.Join(root, "first.md"), filepath.Join(root, "second.md")
	for path, body := range map[string]string{first: "# First", second: "# Second"} {
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	state, driver := testDriver(t)
	store := testRecentStore(t)
	if err := state.enableRecents(store); err != nil {
		t.Fatal(err)
	}
	if err := driver.QueueOpenDirectory(root, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, openFolderID)
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	firstCanonical := filepath.Join(canonical, "first.md")
	secondCanonical := filepath.Join(canonical, "second.md")
	if len(state.recents.Workspaces) != 1 || state.recents.Workspaces[0] != canonical {
		t.Fatalf("workspaces=%q", state.recents.Workspaces)
	}
	clickID(t, driver, fileRows.At(0))
	if err := driver.QueueOpenFiles([]string{second}, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, openFileID)
	if len(state.recents.Files) != 2 || state.recents.Files[0] != secondCanonical || state.recents.Files[1] != firstCanonical {
		t.Fatalf("files=%q", state.recents.Files)
	}
	if accepted, err := dropAt(driver, 500, 200, first); err != nil || !accepted {
		t.Fatalf("duplicate drop=%v, %v", accepted, err)
	}
	if len(state.recents.Files) != 2 || state.recents.Files[0] != firstCanonical || len(state.session.Tabs) != 3 {
		t.Fatalf("duplicate recent=%q tabs=%d", state.recents.Files, len(state.session.Tabs))
	}
	loaded, err := store.Load()
	if err != nil || len(loaded.Files) != 2 || loaded.Files[0] != firstCanonical || len(loaded.Workspaces) != 1 {
		t.Fatalf("persisted=%+v, %v", loaded, err)
	}
}

func TestRecentWorkspaceSwitchRecordsAfterConfirmation(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	state, driver := testDriver(t)
	store := testRecentStore(t)
	if err := state.enableRecents(store); err != nil {
		t.Fatal(err)
	}
	if accepted, err := dropAt(driver, 500, 200, first); err != nil || !accepted {
		t.Fatalf("initial workspace=%v, %v", accepted, err)
	}
	state.session.Current().Replace("dirty")
	if accepted, err := dropAt(driver, 500, 200, second); err != nil || !accepted || state.session.Pending() == nil {
		t.Fatalf("pending switch=%v, %v, %v", accepted, err, state.session.Pending())
	}
	if len(state.recents.Workspaces) != 1 {
		t.Fatalf("pending switch recorded too soon: %q", state.recents.Workspaces)
	}
	clickID(t, driver, cancelConfirmID)
	if len(state.recents.Workspaces) != 1 || state.session.Current().Text() != "dirty" {
		t.Fatalf("cancel changed history or draft: %q", state.recents.Workspaces)
	}
	if accepted, err := dropAt(driver, 500, 200, second); err != nil || !accepted {
		t.Fatalf("repeated switch=%v, %v", accepted, err)
	}
	clickID(t, driver, discardConfirmID)
	canonical, err := filepath.EvalSymlinks(second)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.recents.Workspaces) != 2 || state.recents.Workspaces[0] != canonical || state.session.Root != canonical {
		t.Fatalf("confirmed history=%q root=%q", state.recents.Workspaces, state.session.Root)
	}
}

func TestRecentCorruptionDoesNotBlockNavigationOrOverwriteHistory(t *testing.T) {
	store := testRecentStore(t)
	if err := os.MkdirAll(filepath.Dir(store.Path), 0700); err != nil {
		t.Fatal(err)
	}
	corrupt := []byte("invalid recent JSON")
	if err := os.WriteFile(store.Path, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	if err := state.enableRecents(store); err == nil {
		t.Fatal("corrupt history was accepted")
	}
	root := t.TempDir()
	if err := driver.QueueOpenDirectory(root, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, openFolderID)
	if state.session.Root == "" {
		t.Fatal("history error blocked workspace navigation")
	}
	data, err := os.ReadFile(store.Path)
	if err != nil || !bytes.Equal(data, corrupt) {
		t.Fatalf("corrupt history overwritten: %q, %v", data, err)
	}
}

func TestRecentRecoveryDoesNotTreatDirtyDraftAsOpenedFile(t *testing.T) {
	writer := newModel()
	writer.session.Current().Replace("unsaved recovery draft")
	recovery := session.RecoveryStore{Path: filepath.Join(t.TempDir(), "private", "recovery.json")}
	if err := recovery.Save(writer.session.RecoverySnapshot()); err != nil {
		t.Fatal(err)
	}
	state := newModel()
	if err := state.enableRecovery(recovery); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.stopRecovery() })
	if err := state.enableRecents(testRecentStore(t)); err != nil {
		t.Fatal(err)
	}
	if state.session.Current().Text() != "unsaved recovery draft" || len(state.recents.Files) != 0 || len(state.recents.Workspaces) != 0 {
		t.Fatalf("recovered draft entered recent history: %+v", state.recents)
	}
}

func TestRecentUnavailableFileKeepsActiveDraftAndHistory(t *testing.T) {
	state := newModel()
	store := testRecentStore(t)
	if err := state.enableRecents(store); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing.md")
	state.recents.AddFile(missing)
	if err := store.Save(state.recents); err != nil {
		t.Fatal(err)
	}
	draft := state.session.Current()
	draft.Replace("keep this draft")
	state.openFile(missing)
	if state.message == "" || state.session.Current() != draft || draft.Text() != "keep this draft" || len(state.recents.Files) != 1 {
		t.Fatalf("stale recent mutated state: message=%q recent=%+v", state.message, state.recents)
	}
}

func TestRecentHeadlessNeedsExplicitTaskLocalPath(t *testing.T) {
	t.Setenv("VIVA_HEADLESS", "1")
	t.Setenv("MARKSTACK_RECENT_PATH", "")
	if recentHistoryEnabled() {
		t.Fatal("headless would access the user's recent file")
	}
	path := filepath.Join(t.TempDir(), "recent.json")
	t.Setenv("MARKSTACK_RECENT_PATH", path)
	if !recentHistoryEnabled() {
		t.Fatal("explicit task-local recent file was disabled")
	}
	store, err := defaultRecentStore()
	if err != nil || store.Path != path {
		t.Fatalf("store=%+v, %v", store, err)
	}
}

func TestRecentPanelReopensWorkspaceAndFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("# Recent note"), 0644); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	store := testRecentStore(t)
	known := session.RecentState{}
	known.AddWorkspace(canonical)
	known.AddFile(filepath.Join(canonical, "note.md"))
	if err := store.Save(known); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	if err := state.enableRecents(store); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, recentMenuID)
	if !state.recentOpen {
		t.Fatal("recent panel did not open")
	}
	clickID(t, driver, recentRows.At(0))
	if state.session.Root != canonical || state.recentOpen {
		t.Fatalf("workspace recent did not navigate: %q, open=%v", state.session.Root, state.recentOpen)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, recentMenuID)
	clickID(t, driver, recentFilesID)
	clickID(t, driver, recentRows.At(0))
	if state.session.Current().Text() != "# Recent note" || state.recentOpen || len(state.session.Tabs) != 2 {
		t.Fatalf("file recent did not open once: %+v", state.session.Tabs)
	}
}

func TestRecentMarkdownFilePreservesViewAndCanSwitchToSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recent.md")
	if err := os.WriteFile(path, []byte("# Heading"), 0644); err != nil {
		t.Fatal(err)
	}
	store := testRecentStore(t)
	history := session.RecentState{}
	location, err := workspace.CanonicalLocation(path)
	if err != nil {
		t.Fatal(err)
	}
	history.AddFile(location)
	if err := store.Save(history); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	if err := state.enableRecents(store); err != nil {
		t.Fatal(err)
	}
	state.mode = renderedMode
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, recentMenuID)
	clickID(t, driver, recentFilesID)
	clickID(t, driver, recentRows.At(0))
	if state.session.Current().Kind != workspace.MarkdownKind || state.mode != renderedMode {
		t.Fatalf("recent Markdown lost current view: kind=%v mode=%v", state.session.Current().Kind, state.mode)
	}
	clickID(t, driver, editModeID)
	if state.mode != editMode || state.sourceRect.Width == 0 {
		t.Fatalf("recent Markdown source not reachable: mode=%v source=%+v", state.mode, state.sourceRect)
	}
}

func TestRecentPanelEscapeAndClearHistoryPreserveFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "preserve.md")
	if err := os.WriteFile(path, []byte("# Keep"), 0644); err != nil {
		t.Fatal(err)
	}
	store := testRecentStore(t)
	known := session.RecentState{}
	known.AddFile(path)
	if err := store.Save(known); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	if err := state.enableRecents(store); err != nil {
		t.Fatal(err)
	}
	state.session.Current().Replace("local draft")
	clickID(t, driver, fileMenuID)
	clickID(t, driver, recentMenuID)
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if state.recentOpen {
		t.Fatal("Escape did not close recent panel")
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, recentMenuID)
	clickID(t, driver, recentClearID)
	loaded, err := store.Load()
	if err != nil || len(loaded.Files) != 0 || len(loaded.Workspaces) != 0 || state.session.Current().Text() != "local draft" {
		t.Fatalf("clear changed draft or failed: %+v, %v", loaded, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "# Keep" {
		t.Fatalf("clear changed document: %q, %v", data, err)
	}
}

func TestRecentPanelLongPathFitsNarrowViewport(t *testing.T) {
	state, driver := testDriver(t)
	store := testRecentStore(t)
	known := session.RecentState{}
	known.AddWorkspace("/tmp/" + string(bytes.Repeat([]byte("long-path-"), 12)))
	if err := store.Save(known); err != nil {
		t.Fatal(err)
	}
	if err := state.enableRecents(store); err != nil {
		t.Fatal(err)
	}
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, recentMenuID)
	for _, id := range []gfx.ID{recentWorkspacesID, recentFilesID, recentClearID, recentCloseID, recentRows.At(0)} {
		found := false
		for _, hit := range driver.Snapshot().Hits {
			if hit.ID != id {
				continue
			}
			found = true
			if hit.Bounds.X < 0 || hit.Bounds.Y < 0 || hit.Bounds.X+hit.Bounds.Width > 740 || hit.Bounds.Y+hit.Bounds.Height > 520 {
				t.Fatalf("recent control %v outside narrow viewport: %+v", id, hit.Bounds)
			}
		}
		if !found {
			t.Fatalf("recent control %v missing", id)
		}
	}
}

func TestRecentPanelKeyboardOpensFocusedWorkspace(t *testing.T) {
	root := t.TempDir()
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	store := testRecentStore(t)
	known := session.RecentState{}
	known.AddWorkspace(canonical)
	if err := store.Save(known); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	if err := state.enableRecents(store); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, recentMenuID)
	for range 3 {
		if err := driver.Key(gfx.KeyTab, 0); err != nil {
			t.Fatal(err)
		}
	}
	if driver.Snapshot().Focused != recentRows.At(0) {
		t.Fatalf("keyboard focus did not reach recent row: %v", driver.Snapshot().Focused)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if state.session.Root != canonical || state.recentOpen {
		t.Fatalf("Enter did not reopen workspace: root=%q open=%v", state.session.Root, state.recentOpen)
	}
}

func TestRecentPanelUsesCompactHeightForOneEntry(t *testing.T) {
	state, driver := testDriver(t)
	store := testRecentStore(t)
	known := session.RecentState{}
	known.AddWorkspace("/tmp/one-workspace")
	if err := store.Save(known); err != nil {
		t.Fatal(err)
	}
	if err := state.enableRecents(store); err != nil {
		t.Fatal(err)
	}
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, recentMenuID)
	var tab, clear ui.Rect
	for _, hit := range driver.Snapshot().Hits {
		switch hit.ID {
		case recentWorkspacesID:
			tab = hit.Bounds
		case recentClearID:
			clear = hit.Bounds
		}
	}
	if clear.Y-tab.Y > 120 {
		t.Fatalf("one-entry recent panel is needlessly tall: tab=%+v clear=%+v", tab, clear)
	}
}

func TestRecentFileFollowsTreeRename(t *testing.T) {
	if !workspace.RenameAvailable() {
		t.Skip("native no-replace rename is unavailable")
	}
	root := t.TempDir()
	path := filepath.Join(root, "before.md")
	if err := os.WriteFile(path, []byte("# Keep"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	store := testRecentStore(t)
	if err := state.enableRecents(store); err != nil {
		t.Fatal(err)
	}
	if err := driver.QueueOpenDirectory(root, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, openFolderID)
	clickID(t, driver, fileRows.At(0))
	clickID(t, driver, treeRenameID)
	if _, err := driver.ShortcutChord(primaryModifier(), 'a'); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("after.md"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(canonical, "after.md")
	if len(state.recents.Files) != 1 || state.recents.Files[0] != want {
		t.Fatalf("renamed recent path=%q, want %q", state.recents.Files, want)
	}
	loaded, err := store.Load()
	if err != nil || len(loaded.Files) != 1 || loaded.Files[0] != want {
		t.Fatalf("persisted rename=%+v, %v", loaded, err)
	}
}

func TestRecentFileIsRemovedAfterSystemTrash(t *testing.T) {
	if !workspace.TrashAvailable() {
		t.Skip("native Trash is unavailable")
	}
	root := t.TempDir()
	if runtime.GOOS == "linux" {
		t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".test-data"))
	}
	if runtime.GOOS == "windows" && os.Getenv("CI") != "true" {
		t.Skip("native Recycle Bin integration is limited to the disposable CI runner")
	}
	path := filepath.Join(root, "to-trash.md")
	if err := os.WriteFile(path, []byte("# Restore me"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	store := testRecentStore(t)
	if err := state.enableRecents(store); err != nil {
		t.Fatal(err)
	}
	if err := driver.QueueOpenDirectory(root, nil); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, openFolderID)
	clickID(t, driver, fileRows.At(0))
	if len(state.recents.Files) != 1 {
		t.Fatalf("opened file not recorded: %+v", state.recents)
	}
	clickID(t, driver, treeTrashID)
	clickID(t, driver, treeTrashApproveID)
	result := state.fileTrashResult
	if result.Original == "" || result.Trashed == "" && runtime.GOOS != "windows" {
		t.Fatal("system Trash did not return a restorable item")
	}
	if result.Trashed != "" {
		t.Cleanup(func() {
			if err := os.Rename(result.Trashed, path); err != nil {
				t.Errorf("restore task fixture: %v", err)
			}
		})
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("source still exists after Trash: %v", err)
	}
	loaded, err := store.Load()
	if err != nil || len(state.recents.Files) != 0 || len(loaded.Files) != 0 {
		t.Fatalf("trashed file remains recent: memory=%+v disk=%+v err=%v", state.recents, loaded, err)
	}
}

func TestRecentPanelRejectsSymlinkedFileEntryWithoutTouchingDraft(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside body"), 0644); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(root, "linked.md")
	if err := os.Symlink(outside, entry); err != nil {
		t.Fatal(err)
	}
	store := testRecentStore(t)
	known := session.RecentState{}
	known.AddFile(entry)
	if err := store.Save(known); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	if err := state.enableRecents(store); err != nil {
		t.Fatal(err)
	}
	draft := state.session.Current()
	draft.Replace("local draft")
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, recentMenuID)
	clickID(t, driver, recentRows.At(0))
	if state.message == "" || state.session.Current() != draft || draft.Text() != "local draft" || len(state.session.Tabs) != 1 {
		t.Fatalf("symlinked recent entry changed state: message=%q tabs=%d text=%q", state.message, len(state.session.Tabs), draft.Text())
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "outside body" {
		t.Fatalf("symlinked recent entry touched its target: %q, %v", data, err)
	}
	loaded, err := store.Load()
	if err != nil || len(loaded.Files) != 1 || loaded.Files[0] != entry {
		t.Fatalf("refused recent entry was dropped from history: %+v, %v", loaded, err)
	}
}
