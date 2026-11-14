//go:build darwin

package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestTrashNodeMovesCleanFileAndClosesTabAfterSystemSuccess(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, fmt.Sprintf("markstack-%d-%s.md", os.Getpid(), filepath.Base(filepath.Dir(root))))
	if err := os.WriteFile(path, []byte("restore me"), 0600); err != nil {
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
	result, err := session.TrashNode(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := os.Lstat(result.Trashed); err == nil {
			if err := os.Rename(result.Trashed, path); err != nil {
				t.Errorf("restore task item from Trash: %v", err)
			}
		}
	})
	if len(session.Tabs) != 1 || session.Current() == document || document.Path == "" {
		t.Fatalf("Trashed tab remained open: %+v", session.Tabs)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Trashed file remained at original path: %v", err)
	}
	if data, err := os.ReadFile(result.Trashed); err != nil || string(data) != "restore me" {
		t.Fatalf("system Trash lost bytes=%q err=%v", data, err)
	}
	if err := os.Rename(result.Trashed, path); err != nil {
		t.Fatal(err)
	}
}
