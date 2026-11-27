package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadWorkspaceMarkdownPreservesSourceAndRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	source := "\ufeff# 中文\r\n\r\nOriginal.\r\n"
	if err := os.WriteFile(filepath.Join(root, "inside.md"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := LoadWorkspaceMarkdown(root, "inside.md")
	if err != nil || !snapshot.BOM || snapshot.Newline != "\r\n" || string(snapshot.Encode(snapshot.Text)) != source {
		t.Fatalf("rooted load lost source metadata: %+v (%v)", snapshot, err)
	}
	if err := Verify(snapshot); err != nil {
		t.Fatalf("rooted snapshot cannot be verified before saving: %v", err)
	}
	if _, err := LoadWorkspaceMarkdown(root, filepath.Join("..", "outside.md")); !errors.Is(err, ErrOutsideWorkspace) {
		t.Fatalf("traversal error = %v", err)
	}
	outside := t.TempDir()
	target := filepath.Join(outside, "outside.md")
	if err := os.WriteFile(target, []byte("# outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "alias.md")); err != nil {
		t.Skipf("cannot create symlinks: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "alias-dir")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"alias.md", filepath.Join("alias-dir", "outside.md")} {
		if _, err := LoadWorkspaceMarkdown(root, path); !errors.Is(err, ErrOutsideWorkspace) {
			t.Fatalf("symlink escape %q returned %v", path, err)
		}
	}
}
