package session

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"markstack/internal/workspace"
)

func TestRenameDirtyOpenFileRetargetsSaveAndRecoveryBaseline(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "notes"), 0755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(root, "notes", "old.md")
	original := append([]byte{0xef, 0xbb, 0xbf}, []byte("# 原文\r\n")...)
	if err := os.WriteFile(old, original, 0600); err != nil {
		t.Fatal(err)
	}
	session := New()
	if changed, err := session.RequestWorkspace(root); err != nil || !changed {
		t.Fatalf("workspace changed=%v err=%v", changed, err)
	}
	document, err := session.Open(old)
	if err != nil {
		t.Fatal(err)
	}
	document.Replace("# 本地修改\r\n")
	if err := session.RenameNode(old, "new.md"); err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(session.Root, "notes", "new.md")
	if document.Path != newPath || document.Title != "new.md" || !document.Dirty() {
		t.Fatalf("renamed dirty tab=%+v", document)
	}
	if _, err := os.Lstat(old); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old path still exists: %v", err)
	}
	if data, err := os.ReadFile(newPath); err != nil || !bytes.Equal(data, original) {
		t.Fatalf("rename wrote unsaved buffer=%q err=%v", data, err)
	}
	snapshot := session.RecoverySnapshot()
	if len(snapshot.Drafts) != 1 || snapshot.Drafts[0].Path != newPath || snapshot.Drafts[0].DiskLocation != newPath {
		t.Fatalf("renamed recovery baseline=%+v", snapshot)
	}
	restarted := New()
	if err := restarted.RestoreRecovery(snapshot); err != nil || restarted.Current().Path != newPath || !restarted.Current().Dirty() || restarted.Current().Text() != "# 本地修改\r\n" {
		t.Fatalf("renamed recovery failed: document=%+v err=%v", restarted.Current(), err)
	}
	if err := session.Save(session.Active, ""); err != nil {
		t.Fatal(err)
	}
	want := append([]byte{0xef, 0xbb, 0xbf}, []byte("# 本地修改\r\n")...)
	if data, err := os.ReadFile(newPath); err != nil || !bytes.Equal(data, want) || document.Dirty() {
		t.Fatalf("renamed Save=%q dirty=%v err=%v", data, document.Dirty(), err)
	}
}

func TestRenameFolderRebasesEveryOpenTabAndRetainsDirtyText(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "notes", "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(root, "notes", "a.md")
	second := filepath.Join(root, "notes", "nested", "b.md")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("base"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	session := New()
	if _, err := session.RequestWorkspace(root); err != nil {
		t.Fatal(err)
	}
	clean, err := session.Open(first)
	if err != nil {
		t.Fatal(err)
	}
	dirty, err := session.Open(second)
	if err != nil {
		t.Fatal(err)
	}
	dirty.Replace("local")
	if err := session.RenameNode(filepath.Join(root, "notes"), "archive"); err != nil {
		t.Fatal(err)
	}
	if clean.Path != filepath.Join(session.Root, "archive", "a.md") || clean.Dirty() || dirty.Path != filepath.Join(session.Root, "archive", "nested", "b.md") || !dirty.Dirty() || dirty.Text() != "local" {
		t.Fatalf("folder rename tabs clean=%+v dirty=%+v", clean, dirty)
	}
	for _, node := range session.Nodes {
		if strings.Contains(node.Path, string(filepath.Separator)+"notes"+string(filepath.Separator)) {
			t.Fatalf("file tree retained old path: %+v", node)
		}
	}
	if err := session.Save(session.Active, ""); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(dirty.Path); err != nil || string(data) != "local" {
		t.Fatalf("dirty child saved at wrong path=%q err=%v", data, err)
	}
}

func TestRenameRejectsExternalChangesAndDestinationCollisionWithoutMovingTabs(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "old.md")
	if err := os.WriteFile(old, []byte("base"), 0644); err != nil {
		t.Fatal(err)
	}
	session := New()
	if _, err := session.RequestWorkspace(root); err != nil {
		t.Fatal(err)
	}
	document, err := session.Open(old)
	if err != nil {
		t.Fatal(err)
	}
	document.Replace("dirty")
	if err := os.WriteFile(old, []byte("external"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := session.RenameNode(old, "new.md"); !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("external conflict=%v", err)
	}
	if document.Path != old || document.Text() != "dirty" || !document.Dirty() {
		t.Fatalf("conflict changed tab=%+v", document)
	}
	if _, err := os.Lstat(filepath.Join(root, "new.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("conflict created destination: %v", err)
	}
	if err := os.WriteFile(old, []byte("base"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "new.md"), []byte("other"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := session.RenameNode(old, "new.md"); !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("destination conflict=%v", err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "new.md")); err != nil || string(data) != "other" || document.Path != old {
		t.Fatalf("collision changed disk/tab=%q path=%q err=%v", data, document.Path, err)
	}
}

func TestRenameFolderChecksAllOpenTabsBeforeMovingAnyPath(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "notes")
	if err := os.Mkdir(directory, 0755); err != nil {
		t.Fatal(err)
	}
	first, second := filepath.Join(directory, "first.md"), filepath.Join(directory, "second.md")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("base"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	session := New()
	if _, err := session.RequestWorkspace(root); err != nil {
		t.Fatal(err)
	}
	firstDoc, err := session.Open(first)
	if err != nil {
		t.Fatal(err)
	}
	secondDoc, err := session.Open(second)
	if err != nil {
		t.Fatal(err)
	}
	firstDoc.Replace("local")
	if err := os.WriteFile(second, []byte("external"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := session.RenameNode(directory, "archive"); !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("folder conflict=%v", err)
	}
	if firstDoc.Path != first || secondDoc.Path != second || firstDoc.Text() != "local" || !firstDoc.Dirty() {
		t.Fatalf("folder conflict changed tabs: first=%+v second=%+v", firstDoc, secondDoc)
	}
	if _, err := os.Stat(directory); err != nil {
		t.Fatalf("source directory moved despite conflict: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "archive")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("destination created despite conflict: %v", err)
	}
}

func TestRenameRejectsInvalidNameWithoutChangingDisk(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	session := New()
	if _, err := session.RequestWorkspace(root); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../escape.md", "folder/other.md", ".hidden.md", "node_modules"} {
		if err := session.RenameNode(path, name); !errors.Is(err, workspace.ErrOutsideWorkspace) {
			t.Errorf("rename to %q = %v", name, err)
		}
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "keep" {
		t.Fatalf("invalid rename changed disk=%q err=%v", data, err)
	}
}

func TestTrashNodeRefusesDirtyTabAndExternallyChangedCleanTab(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("base"), 0600); err != nil {
		t.Fatal(err)
	}
	session := New()
	if _, err := session.RequestWorkspace(root); err != nil {
		t.Fatal(err)
	}
	document, err := session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	document.Replace("dirty")
	if _, err := session.TrashNode(path); !errors.Is(err, ErrDirtyDocument) {
		t.Fatalf("dirty Trash error=%v", err)
	}
	if document.Path != path || !document.Dirty() {
		t.Fatalf("dirty Trash changed tab=%+v", document)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "base" {
		t.Fatalf("dirty Trash changed disk=%q err=%v", data, err)
	}
	document.Editor.Undo()
	if err := os.WriteFile(path, []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := session.TrashNode(path); !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("external change Trash error=%v", err)
	}
	if document.Path != path || len(session.Tabs) != 2 {
		t.Fatalf("external conflict changed tabs=%+v", session.Tabs)
	}
}

func TestTrashFolderRefusesAnyDirtyDescendant(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "notes")
	if err := os.Mkdir(directory, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "draft.md")
	if err := os.WriteFile(path, []byte("base"), 0600); err != nil {
		t.Fatal(err)
	}
	session := New()
	if _, err := session.RequestWorkspace(root); err != nil {
		t.Fatal(err)
	}
	document, err := session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	document.Replace("dirty")
	if _, err := session.TrashNode(directory); !errors.Is(err, ErrDirtyDocument) {
		t.Fatalf("dirty folder Trash error=%v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "base" || document.Path != path {
		t.Fatalf("dirty folder moved=%q tab=%q err=%v", data, document.Path, err)
	}
}
