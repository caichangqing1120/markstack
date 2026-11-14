//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestFileTreeTrashRequiresConfirmationAndRefusesDirtyDocument(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("base"), 0600); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	if changed, err := state.session.RequestWorkspace(root); err != nil || !changed {
		t.Fatalf("workspace changed=%v err=%v", changed, err)
	}
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileRows.At(0))
	clickID(t, driver, treeTrashID)
	if state.fileAction != fileActionTrashConfirm {
		t.Fatal("Trash started without confirmation")
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 'n'); err != nil {
		t.Fatal(err)
	}
	if len(state.session.Tabs) != 2 {
		t.Fatal("confirmation allowed application shortcuts")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "base" {
		t.Fatalf("confirmation moved file before approval: %q err=%v", data, err)
	}
	clickID(t, driver, treeTrashCancelID)
	if state.fileAction != fileActionNone {
		t.Fatal("cancel left Trash confirmation open")
	}
	state.session.Current().Replace("dirty")
	if err := driver.Resize(741, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, treeTrashID)
	clickID(t, driver, treeTrashApproveID)
	if !strings.Contains(state.message, "save or discard") || state.session.Current().Text() != "dirty" || !state.session.Current().Dirty() {
		t.Fatalf("dirty Trash lost document: message=%q current=%+v", state.message, state.session.Current())
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "base" {
		t.Fatalf("dirty Trash moved file: %q err=%v", data, err)
	}
	if _, err := driver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, treeTrashID)
	clickID(t, driver, treeTrashApproveID)
	result := state.fileTrashResult
	t.Cleanup(func() {
		if result.Trashed != "" {
			if _, err := os.Lstat(result.Trashed); err == nil {
				if err := os.Rename(result.Trashed, path); err != nil {
					t.Errorf("restore test item from Trash: %v", err)
				}
			}
		}
	})
	if result.Trashed == "" || state.session.Current().Path == path || state.fileAction != fileActionNone {
		t.Fatalf("confirmed Trash did not close clean tab: result=%+v tabs=%+v", result, state.session.Tabs)
	}
	if data, err := os.ReadFile(result.Trashed); err != nil || string(data) != "dirty" {
		t.Fatalf("Trashed saved bytes=%q err=%v", data, err)
	}
	if err := os.Rename(result.Trashed, path); err != nil {
		t.Fatal(err)
	}
}

func TestFileTreeTrashDialogStaysInsideNarrowViewport(t *testing.T) {
	state, driver := testDriver(t)
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("note"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := state.session.RequestWorkspace(root); err != nil {
		t.Fatal(err)
	}
	if err := driver.Resize(740, 520); err != nil {
		t.Fatal(err)
	}
	clickID(t, driver, fileRows.At(0))
	clickID(t, driver, treeTrashID)
	for _, id := range []gfx.ID{treeTrashApproveID, treeTrashCancelID} {
		found := false
		for _, hit := range driver.Snapshot().Hits {
			if hit.ID == id {
				found = true
				if hit.Bounds.X < 0 || hit.Bounds.Y < 0 || hit.Bounds.X+hit.Bounds.Width > 740 || hit.Bounds.Y+hit.Bounds.Height > 520 {
					t.Errorf("Trash control outside viewport: %+v", hit.Bounds)
				}
			}
		}
		if !found {
			t.Fatalf("Trash action %v is missing", id)
		}
	}
	clickID(t, driver, treeTrashCancelID)
}
