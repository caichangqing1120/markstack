//go:build darwin

package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestTrashMovesTaskFileToSystemTrashAndCanRestore(t *testing.T) {
	root := t.TempDir()
	name := fmt.Sprintf("markstack-%d-%s.md", os.Getpid(), filepath.Base(filepath.Dir(root)))
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte("recoverable"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := Trash(root, name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := os.Lstat(result.Trashed); err == nil {
			if err := os.Rename(result.Trashed, path); err != nil {
				t.Errorf("restore task file from Trash: %v", err)
			}
		}
	})
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("original still exists after Trash: %v", err)
	}
	if result.Trashed == "" || result.Original == "" {
		t.Fatalf("Trash returned no restorable location: %+v", result)
	}
	if data, err := os.ReadFile(result.Trashed); err != nil || string(data) != "recoverable" {
		t.Fatalf("Trashed file=%q err=%v", data, err)
	}
	if err := os.Rename(result.Trashed, path); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "recoverable" {
		t.Fatalf("restored task file=%q err=%v", data, err)
	}
}

func TestTrashRejectsSymlinkAndOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked.md")); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"linked.md", "../outside.md"} {
		if _, err := Trash(root, relative); !errors.Is(err, ErrOutsideWorkspace) {
			t.Errorf("unsafe Trash %q = %v", relative, err)
		}
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "outside" {
		t.Fatalf("outside file changed=%q err=%v", data, err)
	}
}
