package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"markstack/internal/workspace"
)

func TestFileTreeCreatesMarkdownThenRenamesDirtyOpenTab(t *testing.T) {
	root := t.TempDir()
	state, driver := testDriver(t)
	if changed, err := state.session.RequestWorkspace(root); err != nil || !changed {
		t.Fatalf("workspace changed=%v err=%v", changed, err)
	}
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileNewID)
	for range 3 {
		if driver.Snapshot().Focused == treeNameID {
			break
		}
		if err := driver.Pump(); err != nil {
			t.Fatal(err)
		}
	}
	if driver.Snapshot().Focused != treeNameID {
		t.Fatal("new Markdown name input did not take focus")
	}
	if err := driver.Type("draft.md"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(canonicalRoot, "draft.md")
	if state.session.Current().Path != old {
		t.Fatalf("new document path=%q, want=%q", state.session.Current().Path, old)
	}
	if err := driver.Type("# 本地草稿"); err != nil {
		t.Fatal(err)
	}
	if !state.session.Current().Dirty() {
		t.Fatal("new document input was not marked unsaved")
	}
	clickID(t, driver, treeRenameID)
	if _, err := driver.ShortcutChord(primaryModifier(), 'a'); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("renamed.md"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(canonicalRoot, "renamed.md")
	if current := state.session.Current(); current.Path != newPath || current.Title != "renamed.md" || !current.Dirty() || current.Text() != "# 本地草稿" {
		t.Fatalf("renamed dirty document=%+v", current)
	}
	if data, err := os.ReadFile(newPath); err != nil || len(data) != 0 {
		t.Fatalf("rename wrote unsaved text=%q err=%v", data, err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(newPath); err != nil || !bytes.Equal(data, []byte("# 本地草稿")) {
		t.Fatalf("renamed Save=%q err=%v", data, err)
	}
}

func TestFileTreeCreatesFolderAndRefusesExistingName(t *testing.T) {
	root := t.TempDir()
	state, driver := testDriver(t)
	if changed, err := state.session.RequestWorkspace(root); err != nil || !changed {
		t.Fatalf("workspace changed=%v err=%v", changed, err)
	}
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	for _, id := range []gfx.ID{fileNewID, folderNewID, treeRenameID, treeTrashID} {
		visible := false
		for _, hit := range driver.Snapshot().Hits {
			if hit.ID == id {
				visible = true
				if hit.Bounds.X < 0 || hit.Bounds.X+hit.Bounds.Width > 190 || hit.Bounds.Y+hit.Bounds.Height > 520 {
					t.Errorf("narrow tree control outside sidebar: %+v", hit.Bounds)
				}
			}
		}
		if !visible {
			t.Fatalf("tree action %v missing", id)
		}
	}
	clickID(t, driver, folderNewID)
	if err := driver.Type("notes"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(root, "notes")); err != nil || !info.IsDir() {
		t.Fatalf("folder creation=%v err=%v", info, err)
	}
	clickID(t, driver, rootSelectID)
	clickID(t, driver, folderNewID)
	if err := driver.Type("notes"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if state.fileAction == fileActionNone || state.message == "" {
		t.Fatalf("existing folder collision was not shown: action=%v message=%q", state.fileAction, state.message)
	}
	clickID(t, driver, treeCancelID)
	if state.fileAction != fileActionNone {
		t.Fatal("cancel left file name input open")
	}
}

func TestFileTreeRenameRebasesCollapsedFolderState(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "notes"), 0755); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	if changed, err := state.session.RequestWorkspace(root); err != nil || !changed {
		t.Fatalf("workspace changed=%v err=%v", changed, err)
	}
	old := filepath.Join(state.session.Root, "notes")
	state.collapsed[old] = true
	state.treeSelection = old
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, treeRenameID)
	if _, err := driver.ShortcutChord(primaryModifier(), 'a'); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("archive"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(state.session.Root, "archive")
	if !state.collapsed[newPath] || state.collapsed[old] || state.treeSelection != newPath {
		t.Fatalf("collapsed folder not rebased: collapsed=%+v selection=%q", state.collapsed, state.treeSelection)
	}
	if info, err := os.Stat(newPath); err != nil || !info.IsDir() {
		t.Fatalf("renamed folder=%v err=%v", info, err)
	}
}

func TestFileTreeCreateRefusesExistingNameWithoutTouchingDraft(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "taken.md")
	if err := os.WriteFile(existing, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	if changed, err := state.session.RequestWorkspace(root); err != nil || !changed {
		t.Fatalf("workspace changed=%v err=%v", changed, err)
	}
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	draft := state.session.Current()
	draft.Replace("unsaved draft")
	clickID(t, driver, fileNewID)
	if err := driver.Type("taken.md"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if state.fileAction != fileActionCreateFile || state.message == "" {
		t.Fatalf("existing name did not keep the name input with an error: action=%v message=%q", state.fileAction, state.message)
	}
	if len(state.session.Tabs) != 1 || state.session.Current() != draft || draft.Text() != "unsaved draft" || !draft.Dirty() {
		t.Fatalf("collision changed tabs or draft: tabs=%d text=%q", len(state.session.Tabs), draft.Text())
	}
	if data, err := os.ReadFile(existing); err != nil || string(data) != "keep" {
		t.Fatalf("collision overwrote existing file: %q, %v", data, err)
	}
	if err := driver.Type("../escape.md"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if state.message == "" || len(state.session.Tabs) != 1 {
		t.Fatalf("escaping name changed state: message=%q tabs=%d", state.message, len(state.session.Tabs))
	}
	if _, err := os.Lstat(filepath.Join(root, "..", "escape.md")); !os.IsNotExist(err) {
		t.Fatalf("escaping name created a file outside the workspace: %v", err)
	}
	clickID(t, driver, treeCancelID)
	if state.fileAction != fileActionNone || state.session.Current() != draft {
		t.Fatal("cancel after collision lost the draft")
	}
}

func TestFileTreeFolderRenameRebasesEveryOpenTab(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "notes"), 0755); err != nil {
		t.Fatal(err)
	}
	cleanPath := filepath.Join(root, "notes", "clean.md")
	dirtyPath := filepath.Join(root, "notes", "dirty.md")
	for path, body := range map[string]string{cleanPath: "clean body", dirtyPath: ""} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	state, driver := testDriver(t)
	if changed, err := state.session.RequestWorkspace(root); err != nil || !changed {
		t.Fatalf("workspace changed=%v err=%v", changed, err)
	}
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(state.session.Root, "notes")
	clean, dirty := filepath.Join(folder, "clean.md"), filepath.Join(folder, "dirty.md")
	if !state.openFile(clean) || !state.openFile(dirty) {
		t.Fatal("folder documents did not open")
	}
	state.session.Current().Replace("# dirty local")
	state.treeSelection = folder
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, treeRenameID)
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	if driver.Snapshot().Focused != treeNameID {
		t.Fatalf("folder rename name input did not take focus: action=%v focused=%v", state.fileAction, driver.Snapshot().Focused)
	}
	if err := driver.Type("archive"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	newFolder := filepath.Join(state.session.Root, "archive")
	canonicalNewFolder, err := filepath.EvalSymlinks(newFolder)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(folder); !os.IsNotExist(err) {
		t.Fatalf("old folder still exists: %v", err)
	}
	if len(state.session.Tabs) != 3 {
		t.Fatalf("rename changed tab count: %d", len(state.session.Tabs))
	}
	cleanTab, dirtyTab := state.session.Tabs[1], state.session.Tabs[2]
	wantClean, wantDirty := filepath.Join(canonicalNewFolder, "clean.md"), filepath.Join(canonicalNewFolder, "dirty.md")
	if cleanTab.Path != wantClean || cleanTab.Title != "clean.md" || cleanTab.Dirty() || cleanTab.Text() != "clean body" {
		t.Fatalf("clean tab not rebased: %+v", cleanTab)
	}
	if dirtyTab.Path != wantDirty || dirtyTab.Title != "dirty.md" || !dirtyTab.Dirty() || dirtyTab.Text() != "# dirty local" {
		t.Fatalf("dirty tab not rebased: %+v", dirtyTab)
	}
	if state.fileAction != fileActionNone || state.treeSelection != canonicalNewFolder {
		t.Fatalf("rename left action=%v selection=%q", state.fileAction, state.treeSelection)
	}
	archiveFound := false
	for _, node := range state.session.Nodes {
		if node.Name == "notes" && node.Depth == 1 {
			t.Fatalf("tree still lists the old folder: %+v", state.session.Nodes)
		}
		if node.Name == "archive" && node.Dir && node.Depth == 1 {
			archiveFound = true
		}
	}
	if !archiveFound {
		t.Fatalf("tree did not rescan rename: %+v", state.session.Nodes)
	}
	if data, err := os.ReadFile(wantDirty); err != nil || len(data) != 0 {
		t.Fatalf("folder rename wrote the unsaved draft: %q, %v", data, err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(wantDirty); err != nil || string(data) != "# dirty local" || dirtyTab.Dirty() {
		t.Fatalf("Save after folder rename wrote %q dirty=%v err=%v", data, dirtyTab.Dirty(), err)
	}
	if data, err := os.ReadFile(wantClean); err != nil || string(data) != "clean body" {
		t.Fatalf("clean tab changed on disk: %q, %v", data, err)
	}
}

func TestTemplateSaveAsCancelAndSavePreserveDraft(t *testing.T) {
	state, driver := testDriver(t)
	clickID(t, driver, fileMenuID)
	clickID(t, driver, noteTemplateID)
	note := state.session.Current()
	if err := driver.Type("项目"); err != nil {
		t.Fatal(err)
	}
	want := "# 项目\n\n## 摘要\n\n## 正文\n\n## 参考\n\n"
	if note.Text() != want || note.Path != "" || !note.Dirty() {
		t.Fatalf("note template=%+v", note)
	}
	if err := driver.QueueSaveFile("", app.ErrCanceled); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if note.Path != "" || !note.Dirty() || note.Text() != want {
		t.Fatalf("canceled template Save As changed the draft: %+v", note)
	}
	target := filepath.Join(t.TempDir(), "note.md")
	if err := driver.QueueSaveFile(target, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != want || note.Dirty() || note.Path != target {
		t.Fatalf("template Save As=%q dirty=%v path=%q err=%v", data, note.Dirty(), note.Path, err)
	}
	clickID(t, driver, fileMenuID)
	clickID(t, driver, proposalTemplateID)
	proposal := state.session.Current()
	if proposal.Path != "" || !proposal.Dirty() || proposal.Text() == note.Text() || note.Text() != want {
		t.Fatalf("second template changed the saved draft: proposal=%+v note=%q", proposal, note.Text())
	}
}

func TestFileTreeTrashFailureKeepsTabAndDisk(t *testing.T) {
	if !workspace.TrashAvailable() {
		t.Skip("system Trash is unavailable on this platform")
	}
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("base"), 0600); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	if changed, err := state.session.RequestWorkspace(root); err != nil || !changed {
		t.Fatalf("workspace changed=%v err=%v", changed, err)
	}
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileRows.At(0))
	opened := state.session.Current()
	clickID(t, driver, treeTrashID)
	if state.fileAction != fileActionTrashConfirm {
		t.Fatal("Trash did not ask for confirmation")
	}
	if err := os.WriteFile(path, []byte("external update"), 0600); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, treeTrashApproveID)
	if state.fileAction != fileActionNone || state.message == "" {
		t.Fatalf("failed Trash state action=%v message=%q", state.fileAction, state.message)
	}
	canonicalPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.session.Tabs) != 2 || state.session.Tabs[1] != opened || opened.Path != canonicalPath {
		t.Fatalf("failed Trash half-committed tabs: %+v opened=%+v path=%q", state.session.Tabs, opened, canonicalPath)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "external update" {
		t.Fatalf("failed Trash changed disk: %q, %v", data, err)
	}
	if len(state.session.Nodes) != 1 || state.session.Nodes[0].Name != "note.md" {
		t.Fatalf("failed Trash changed the tree: %+v", state.session.Nodes)
	}
}
