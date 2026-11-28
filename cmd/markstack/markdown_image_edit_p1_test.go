package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"markstack/internal/markdownmedia"
	"markstack/internal/session"
	"markstack/internal/workspace"
)

func openImageInspectorForTest(t *testing.T, state *model) {
	t.Helper()
	current := state.session.Current()
	if current == nil {
		t.Fatal("no active document")
	}
	refs := markdownmedia.ImageReferences([]byte(current.Text()))
	if len(refs) == 0 {
		t.Fatalf("no image reference in %q", current.Text())
	}
	state.imageInspector = imageInspectorState{
		open:       true,
		documentID: current.ID,
		path:       current.Path,
		source:     current.Text(),
		ref:        refs[0],
	}
}

func TestRenameImageRejectedWhenReferencedByAnotherDocument(t *testing.T) {
	root := t.TempDir()
	assets := filepath.Join(root, "assets")
	if err := os.MkdirAll(assets, 0700); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(assets, "p.png")
	if err := os.WriteFile(original, []byte("png"), 0600); err != nil {
		t.Fatal(err)
	}
	docA := filepath.Join(root, "a.md")
	docB := filepath.Join(root, "b.md")
	if err := os.WriteFile(docA, []byte("![p](assets/p.png)\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(docB, []byte("![p](assets/p.png)\n"), 0600); err != nil {
		t.Fatal(err)
	}

	state := newModel()
	if _, err := state.session.RequestWorkspace(root); err != nil {
		t.Fatal(err)
	}
	if _, err := state.session.Open(docA); err != nil {
		t.Fatal(err)
	}
	openImageInspectorForTest(t, state)

	err := state.renameMarkdownImage("renamed.png")
	if err == nil || !strings.Contains(err.Error(), "b.md") {
		t.Fatalf("expected shared-asset rejection naming b.md, got: %v", err)
	}
	if _, statErr := os.Stat(original); statErr != nil {
		t.Fatalf("original image should remain on disk: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(assets, "renamed.png")); !os.IsNotExist(statErr) {
		t.Fatalf("renamed image should not be created: %v", statErr)
	}
}

func TestDiscardAndReloadRollbackPendingImageRename(t *testing.T) {
	t.Run("discard-tab", func(t *testing.T) {
		root := t.TempDir()
		assets := filepath.Join(root, "assets")
		if err := os.MkdirAll(assets, 0700); err != nil {
			t.Fatal(err)
		}
		original := filepath.Join(assets, "p.png")
		renamed := filepath.Join(assets, "renamed.png")
		if err := os.WriteFile(original, []byte("png"), 0600); err != nil {
			t.Fatal(err)
		}
		docA := filepath.Join(root, "a.md")
		if err := os.WriteFile(docA, []byte("![p](assets/p.png)\n"), 0600); err != nil {
			t.Fatal(err)
		}

		state := newModel()
		if _, err := state.session.RequestWorkspace(root); err != nil {
			t.Fatal(err)
		}
		if _, err := state.session.Open(docA); err != nil {
			t.Fatal(err)
		}
		openImageInspectorForTest(t, state)
		if err := state.renameMarkdownImage("renamed.png"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(renamed); err != nil {
			t.Fatalf("expected renamed image before discard: %v", err)
		}
		if state.session.RequestClose(state.session.Active) {
			t.Fatal("expected dirty document close confirmation")
		}
		state.resolve(session.Discard)
		if _, err := os.Stat(original); err != nil {
			t.Fatalf("expected original image restored after discard: %v", err)
		}
		if _, err := os.Stat(renamed); !os.IsNotExist(err) {
			t.Fatalf("expected renamed image removed after discard: %v", err)
		}
	})

	t.Run("reload-disk", func(t *testing.T) {
		root := t.TempDir()
		assets := filepath.Join(root, "assets")
		if err := os.MkdirAll(assets, 0700); err != nil {
			t.Fatal(err)
		}
		original := filepath.Join(assets, "p.png")
		renamed := filepath.Join(assets, "renamed.png")
		if err := os.WriteFile(original, []byte("png"), 0600); err != nil {
			t.Fatal(err)
		}
		docA := filepath.Join(root, "a.md")
		if err := os.WriteFile(docA, []byte("![p](assets/p.png)\n"), 0600); err != nil {
			t.Fatal(err)
		}

		state := newModel()
		if _, err := state.session.RequestWorkspace(root); err != nil {
			t.Fatal(err)
		}
		if _, err := state.session.Open(docA); err != nil {
			t.Fatal(err)
		}
		openImageInspectorForTest(t, state)
		if err := state.renameMarkdownImage("renamed.png"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(docA, []byte("![p](assets/p.png)\nupdated externally\n"), 0600); err != nil {
			t.Fatal(err)
		}
		rev := workspace.ReadDiskRevision(docA)
		state.reloadDisk(state.session.Current(), rev, true)
		if _, err := os.Stat(original); err != nil {
			t.Fatalf("expected original image restored after reload: %v", err)
		}
		if _, err := os.Stat(renamed); !os.IsNotExist(err) {
			t.Fatalf("expected renamed image removed after reload: %v", err)
		}
	})
}

func TestMediaDraftsPendingIncludesRichHTML(t *testing.T) {
	root := t.TempDir()
	note := filepath.Join(root, "note.mh")
	if err := os.WriteFile(note, []byte("<p>hello</p>"), 0600); err != nil {
		t.Fatal(err)
	}
	state := newModel()
	if _, err := state.session.RequestWorkspace(root); err != nil {
		t.Fatal(err)
	}
	doc, err := state.session.Open(note)
	if err != nil {
		t.Fatal(err)
	}
	if state.mediaDraftsPending() {
		t.Fatal("clean .mh document should not block media cleanup")
	}
	if err := doc.EditRange(0, len(doc.Text()), `<p><img src="assets/new.png"></p>`); err != nil {
		t.Fatal(err)
	}
	if !state.mediaDraftsPending() {
		t.Fatal("dirty .mh document must block media cleanup")
	}
}

func TestRenameImageUpdatesAlternateRelativeReferencesInSameDocument(t *testing.T) {
	root := t.TempDir()
	assets := filepath.Join(root, "assets")
	if err := os.MkdirAll(assets, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "p.png"), []byte("png"), 0600); err != nil {
		t.Fatal(err)
	}
	docA := filepath.Join(root, "a.md")
	if err := os.WriteFile(docA, []byte("![first](assets/p.png)\n\n![second](./assets/p.png)\n"), 0600); err != nil {
		t.Fatal(err)
	}
	state := newModel()
	if _, err := state.session.RequestWorkspace(root); err != nil {
		t.Fatal(err)
	}
	if _, err := state.session.Open(docA); err != nil {
		t.Fatal(err)
	}
	openImageInspectorForTest(t, state)
	if err := state.renameMarkdownImage("renamed.png"); err != nil {
		t.Fatal(err)
	}
	got := state.session.Current().Text()
	if strings.Contains(got, "p.png") || strings.Count(got, "assets/renamed.png") != 2 {
		t.Fatalf("expected both references updated to assets/renamed.png, got:\n%s", got)
	}
}

func TestMultiTabDiscardThenCancelPreservesRenamedImageOnDisk(t *testing.T) {
	root := t.TempDir()
	assets := filepath.Join(root, "assets")
	if err := os.MkdirAll(assets, 0700); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(assets, "p.png")
	renamed := filepath.Join(assets, "renamed.png")
	if err := os.WriteFile(original, []byte("png"), 0600); err != nil {
		t.Fatal(err)
	}
	docA := filepath.Join(root, "a.md")
	docB := filepath.Join(root, "b.md")
	if err := os.WriteFile(docA, []byte("![p](assets/p.png)\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(docB, []byte("second\n"), 0600); err != nil {
		t.Fatal(err)
	}

	state := newModel()
	if _, err := state.session.RequestWorkspace(root); err != nil {
		t.Fatal(err)
	}
	if _, err := state.session.Open(docA); err != nil {
		t.Fatal(err)
	}
	openImageInspectorForTest(t, state)
	if err := state.renameMarkdownImage("renamed.png"); err != nil {
		t.Fatal(err)
	}
	tabB, err := state.session.Open(docB)
	if err != nil {
		t.Fatal(err)
	}
	if err := tabB.EditRange(0, len(tabB.Text()), "second edited\n"); err != nil {
		t.Fatal(err)
	}

	if state.session.RequestQuit() {
		t.Fatal("expected quit confirmation for two dirty tabs")
	}
	state.resolve(session.Discard)
	state.resolve(session.Cancel)

	if _, err := os.Stat(renamed); err != nil {
		t.Fatalf("expected renamed image preserved when quit was canceled: %v", err)
	}
	if _, err := os.Stat(original); !os.IsNotExist(err) {
		t.Fatalf("original image should not be restored on cancel: %v", err)
	}

	if state.session.RequestQuit() {
		t.Fatal("expected quit confirmation again")
	}
	state.resolve(session.Discard)
	state.resolve(session.Discard)

	if _, err := os.Stat(original); err != nil {
		t.Fatalf("expected original image restored after quit completed: %v", err)
	}
	if _, err := os.Stat(renamed); !os.IsNotExist(err) {
		t.Fatalf("expected renamed image removed after quit completed: %v", err)
	}
}
