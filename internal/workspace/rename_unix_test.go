//go:build darwin || linux

package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeRenameExcludesExistingDestinationAtSyscall(t *testing.T) {
	root := t.TempDir()
	oldPath, targetPath := filepath.Join(root, "old.md"), filepath.Join(root, "target.md")
	if err := os.WriteFile(oldPath, []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("other writer"), 0600); err != nil {
		t.Fatal(err)
	}
	oldDir, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer oldDir.Close()
	newDir, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer newDir.Close()
	if err := renameNoReplace(oldDir, "old.md", newDir, "target.md"); !errors.Is(err, os.ErrExist) {
		t.Fatalf("native rename silently replaced destination: %v", err)
	}
	if source, err := os.ReadFile(oldPath); err != nil || string(source) != "source" {
		t.Fatalf("source changed=%q err=%v", source, err)
	}
	if target, err := os.ReadFile(targetPath); err != nil || string(target) != "other writer" {
		t.Fatalf("target changed=%q err=%v", target, err)
	}
}
