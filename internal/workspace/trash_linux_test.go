package workspace

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxTrashWritesRecoverablePayloadAndMetadata(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "private", "xdg")
	t.Setenv("XDG_DATA_HOME", dataHome)
	const name = "旧 档案 #1.md"
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte("recover me"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := Trash(root, name)
	if err != nil || result.Original != path {
		t.Fatalf("trash result=%+v err=%v", result, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source remains: %v", err)
	}
	if data, err := os.ReadFile(result.Trashed); err != nil || string(data) != "recover me" {
		t.Fatalf("payload=%q err=%v", data, err)
	}
	metadata, err := os.ReadFile(filepath.Join(dataHome, "Trash", "info", filepath.Base(result.Trashed)+".trashinfo"))
	if err != nil || !strings.HasPrefix(string(metadata), "[Trash Info]\nPath="+(&url.URL{Path: path}).EscapedPath()+"\nDeletionDate=") {
		t.Fatalf("metadata=%q err=%v", metadata, err)
	}
}

func TestLinuxTrashRejectsSymlinkAndUnsafeTrashDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	path := filepath.Join(root, "keep.md")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, filepath.Join(root, "link.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := Trash(root, "link.md"); !errors.Is(err, ErrOutsideWorkspace) {
		t.Fatalf("symlink accepted: %v", err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "Trash")); err != nil {
		t.Fatal(err)
	}
	if _, err := Trash(root, "keep.md"); err == nil {
		t.Fatal("symlinked destination accepted")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "keep" {
		t.Fatalf("source changed: %q %v", data, err)
	}
}
