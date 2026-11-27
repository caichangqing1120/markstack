package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsRenameUnicodeNoReplace(t *testing.T) {
	root := t.TempDir()
	for _, folder := range []string{"旧 笔记", "新 笔记"} {
		if err := os.Mkdir(filepath.Join(root, folder), 0700); err != nil {
			t.Fatal(err)
		}
	}
	oldName := filepath.Join("旧 笔记", "公式.md")
	newName := filepath.Join("新 笔记", "流程图.md")
	for name, body := range map[string]string{oldName: "source", newName: "target"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := Rename(root, oldName, newName); !errors.Is(err, ErrRenameTargetExists) {
		t.Fatalf("target must remain untouched: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(root, newName)); err != nil || string(data) != "target" {
		t.Fatalf("target changed: %q %v", data, err)
	}
	if err := os.Remove(filepath.Join(root, newName)); err != nil {
		t.Fatal(err)
	}
	if err := Rename(root, oldName, newName); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(root, newName)); err != nil || string(data) != "source" {
		t.Fatalf("renamed file: %q %v", data, err)
	}
	for _, unsafe := range []string{"NUL.md", "C:relative.md", "attachment.md:stream", "trailing.md."} {
		if err := Rename(root, newName, unsafe); !errors.Is(err, ErrOutsideWorkspace) {
			t.Errorf("accepted %q: %v", unsafe, err)
		}
	}
}
