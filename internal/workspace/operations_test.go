package workspace

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateMarkdownAndFolderStayWithinWorkspace(t *testing.T) {
	root := t.TempDir()
	if err := CreateFolder(root, "notes"); err != nil {
		t.Fatal(err)
	}
	data := append([]byte{0xef, 0xbb, 0xbf}, []byte("# 中文\r\n")...)
	snapshot, err := CreateMarkdown(root, filepath.Join("notes", "draft.md"), data)
	if err != nil || !snapshot.BOM || snapshot.Newline != "\r\n" || !bytes.Equal(snapshot.Bytes, data) {
		t.Fatalf("new Markdown snapshot=%+v err=%v", snapshot, err)
	}
	if err := CreateFolder(root, "notes"); !errors.Is(err, ErrConflict) {
		t.Fatalf("existing directory collision=%v", err)
	}
	if _, err := CreateMarkdown(root, filepath.Join("notes", "draft.md"), []byte("overwrite")); !errors.Is(err, ErrConflict) {
		t.Fatalf("existing file collision=%v", err)
	}
	if got, err := os.ReadFile(snapshot.Path); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("new file overwritten=%q err=%v", got, err)
	}
}

func TestRenameNeverClobbersAndPreservesBytesAndMode(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "old.md")
	target := filepath.Join(root, "target.md")
	want := append([]byte{0xef, 0xbb, 0xbf}, []byte("draft\r\n")...)
	if err := os.WriteFile(old, want, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("existing"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Rename(root, "old.md", "target.md"); !errors.Is(err, ErrConflict) {
		t.Fatalf("rename collision=%v", err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "existing" {
		t.Fatalf("target overwritten=%q err=%v", got, err)
	}
	if err := Rename(root, "old.md", "moved.md"); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(root, "moved.md")
	info, err := os.Stat(moved)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("renamed permissions=%v err=%v", info.Mode(), err)
	}
	if got, err := os.ReadFile(moved); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("renamed bytes=%q err=%v", got, err)
	}
	if _, err := os.Lstat(old); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old path remains=%v", err)
	}
}

func TestRenameFolderMovesDescendantsWithoutClobber(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "notes", "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes", "nested", "file.md"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Rename(root, "notes", "archive"); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(root, "archive", "nested", "file.md")); err != nil || string(got) != "keep" {
		t.Fatalf("renamed folder content=%q err=%v", got, err)
	}
}

func TestWorkspaceOperationsRejectEscapeHiddenAndSymlinkAncestor(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "notes"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../escape.md", ".hidden.md", "node_modules/package.md", filepath.Join("linked", "outside.md"), "a/b/../draft.md"} {
		if _, err := CreateMarkdown(root, name, []byte("unsafe")); !errors.Is(err, ErrOutsideWorkspace) {
			t.Errorf("unsafe create %q = %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "notes", "file.md"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "notes", "file.md"), filepath.Join(root, "linked.md")); err != nil {
		t.Fatal(err)
	}
	if err := Rename(root, "linked.md", "linked-new.md"); !errors.Is(err, ErrOutsideWorkspace) {
		t.Errorf("symlink leaf rename=%v", err)
	}
	for _, target := range []string{"../escape.md", filepath.Join("linked", "outside.md"), ".hidden.md"} {
		if err := Rename(root, filepath.Join("notes", "file.md"), target); !errors.Is(err, ErrOutsideWorkspace) {
			t.Errorf("unsafe rename %q = %v", target, err)
		}
	}
	if _, err := os.ReadFile(filepath.Join(outside, "outside.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outside workspace was modified: %v", err)
	}
}
