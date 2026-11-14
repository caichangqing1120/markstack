package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
	"markstack/internal/workspace"
)

func dropAt(driver interface {
	Drop(gfx.FileDropEvent) (bool, error)
}, x, y float32, paths ...string) (bool, error) {
	return driver.Drop(gfx.FileDropEvent{Phase: gfx.FileDropDrop, X: x, Y: y, Paths: paths})
}

func TestDropOpensMarkdownBatchWithoutPartialTabs(t *testing.T) {
	root := t.TempDir()
	first, second, invalid := filepath.Join(root, "a.md"), filepath.Join(root, "b.md"), filepath.Join(root, "bad.md")
	for path, value := range map[string][]byte{first: []byte("# A"), second: []byte("# B"), invalid: {0, 1, 2}} {
		if err := os.WriteFile(path, value, 0644); err != nil {
			t.Fatal(err)
		}
	}
	state, driver := testDriver(t)
	draft := state.session.Current()
	draft.Replace("local draft")
	if accepted, err := driver.Drop(gfx.FileDropEvent{Phase: gfx.FileDropOver, X: 500, Y: 200}); err != nil || !accepted {
		t.Fatalf("hover = %v, %v", accepted, err)
	}
	if accepted, err := dropAt(driver, 500, 200, first, second); err != nil || !accepted {
		t.Fatalf("drop = %v, %v", accepted, err)
	}
	if len(state.session.Tabs) != 3 || state.session.Current().Path != second || draft.Text() != "local draft" || !draft.Dirty() {
		t.Fatalf("tabs=%d active=%q draft=%q", len(state.session.Tabs), state.session.Current().Path, draft.Text())
	}
	if accepted, err := dropAt(driver, 500, 200, first, invalid); err != nil || accepted {
		t.Fatalf("invalid drop = %v, %v", accepted, err)
	}
	if len(state.session.Tabs) != 3 || state.session.Current().Path != second || draft.Text() != "local draft" {
		t.Fatal("failed drop changed the active document or dirty draft")
	}
}

func TestDropWorkspaceHonorsUnsavedCancellation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "inside.md"), []byte("# Inside"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	draft := state.session.Current()
	draft.Replace("not saved")
	state.message = "previous drop failed"
	if accepted, err := dropAt(driver, 500, 200, root); err != nil || !accepted || state.session.Pending() == nil {
		t.Fatalf("folder drop = %v, %v, pending=%v", accepted, err, state.session.Pending())
	}
	if state.message != "" {
		t.Fatalf("accepted folder drop retained stale error: %q", state.message)
	}
	clickID(t, driver, cancelConfirmID)
	if state.session.Root != "" || state.session.Current() != draft || !draft.Dirty() || draft.Text() != "not saved" {
		t.Fatal("canceling folder drop lost the draft")
	}
	if accepted, err := dropAt(driver, 500, 200, root); err != nil || !accepted {
		t.Fatalf("second folder drop = %v, %v", accepted, err)
	}
	clickID(t, driver, discardConfirmID)
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if state.session.Root != canonical || len(state.session.Nodes) != 1 || state.session.Nodes[0].Name != "inside.md" {
		t.Fatalf("confirmed workspace = %q, nodes=%+v", state.session.Root, state.session.Nodes)
	}
}

func TestDropRejectsSymlinksMixedPathsAndModal(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "note.md")
	link := filepath.Join(root, "alias.md")
	linkDir := filepath.Join(root, "alias-dir")
	if err := os.WriteFile(file, []byte("# Note"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, linkDir); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	for _, paths := range [][]string{{link}, {linkDir}, {file, root}, {file, link}, nil} {
		if accepted, err := dropAt(driver, 500, 200, paths...); err != nil || accepted {
			t.Fatalf("unsafe drop %q = %v, %v", paths, accepted, err)
		}
	}
	if accepted, err := dropAt(driver, 500, 15, file); err != nil || accepted {
		t.Fatalf("titlebar drop = %v, %v", accepted, err)
	}
	state.session.Current().Replace("dirty")
	clickID(t, driver, tabClose.At(0))
	if accepted, err := dropAt(driver, 500, 200, file); err != nil || accepted {
		t.Fatalf("modal drop = %v, %v", accepted, err)
	}
	if len(state.session.Tabs) != 1 || state.session.Current().Text() != "dirty" {
		t.Fatal("refused drop changed the draft")
	}
}

func TestDropOpensRichHTMLWithoutChangingOriginalBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rich.mh")
	original := []byte(`<p>富文本</p>`)
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	if accepted, err := dropAt(driver, 500, 200, path); err != nil || !accepted {
		t.Fatalf("rich drop=%v, %v", accepted, err)
	}
	if state.session.Current().Kind != workspace.RichHTMLKind || state.mode != renderedMode || state.session.Current().Text() != string(original) || state.session.Current().Dirty() {
		t.Fatalf("rich drop did not open native document: mode=%v current=%+v", state.mode, state.session.Current())
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != string(original) {
		t.Fatalf("rich drop changed disk: %q, %v", data, err)
	}
}
