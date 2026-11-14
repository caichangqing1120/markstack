package session

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/vivaui/viva/text"
)

func TestSessionManifestRestoresOrderedCleanTabsAndCaret(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.md")
	second := filepath.Join(root, "second.txt")
	if err := os.WriteFile(first, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("中abc"), 0600); err != nil {
		t.Fatal(err)
	}
	opened := New()
	opened.Root = root
	firstTab, err := opened.Open(first)
	if err != nil {
		t.Fatal(err)
	}
	firstTab.Editor.MoveTo(2, false)
	secondTab, err := opened.Open(second)
	if err != nil {
		t.Fatal(err)
	}
	secondTab.Editor.MoveTo(3, false)
	manifest := opened.Manifest()
	if manifest.ActiveID != secondTab.ID || len(manifest.Tabs) != 2 || manifest.Tabs[0].ID != firstTab.ID || manifest.Tabs[0].Caret != 2 || manifest.Tabs[1].Caret != 3 {
		t.Fatalf("clean tab manifest = %+v", manifest)
	}
	restored := New()
	warnings, err := restored.RestoreManifest(manifest)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("clean restore warnings=%q err=%v", warnings, err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.Tabs) != 2 || restored.Tabs[0].Path != first || restored.Tabs[0].Editor.Caret() != 2 || restored.Tabs[1].Path != second || restored.Tabs[1].Editor.Caret() != 3 || restored.Active != 1 || restored.Root != canonicalRoot {
		t.Fatalf("restored tabs=%+v active=%d root=%q", restored.Tabs, restored.Active, restored.Root)
	}
	for _, document := range restored.Tabs {
		if document.Dirty() {
			t.Fatalf("clean tab became dirty: %+v", document)
		}
	}
}

func TestRestoreManifestInterleavesCleanTabsWithDirtyRecovery(t *testing.T) {
	root := t.TempDir()
	clean := filepath.Join(root, "clean.md")
	dirty := filepath.Join(root, "dirty.md")
	for _, path := range []string{clean, dirty} {
		if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	opened := New()
	opened.Root = root
	cleanTab, err := opened.Open(clean)
	if err != nil {
		t.Fatal(err)
	}
	cleanTab.Editor.MoveTo(4, false)
	dirtyTab, err := opened.Open(dirty)
	if err != nil {
		t.Fatal(err)
	}
	dirtyTab.Replace("local draft")
	dirtyTab.Editor.MoveTo(3, false)
	untitled := opened.NewDocument()
	untitled.Replace("unsaved")
	untitled.Editor.MoveTo(5, false)
	opened.Active = opened.indexOf(cleanTab)
	manifest, recovery := opened.Manifest(), opened.RecoverySnapshot()
	if len(manifest.Tabs) != 3 || !manifest.Tabs[1].Dirty || !manifest.Tabs[2].Dirty || manifest.ActiveID != cleanTab.ID {
		t.Fatalf("mixed manifest=%+v", manifest)
	}
	if err := os.WriteFile(dirty, []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	restored := New()
	if err := restored.RestoreRecovery(recovery); err != nil {
		t.Fatal(err)
	}
	warnings, err := restored.RestoreManifest(manifest)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("mixed restore warnings=%q err=%v", warnings, err)
	}
	if len(restored.Tabs) != 3 || restored.Tabs[0].ID != cleanTab.ID || restored.Tabs[1].ID != dirtyTab.ID || restored.Tabs[2].ID != untitled.ID || restored.Active != 0 || restored.Tabs[0].Editor.Caret() != 4 || restored.Tabs[1].Editor.Caret() != 3 || restored.Tabs[2].Editor.Caret() != 5 {
		t.Fatalf("mixed tabs=%+v active=%d", restored.Tabs, restored.Active)
	}
	if restored.Tabs[1].Text() != "local draft" || !restored.Tabs[1].Dirty() || restored.Tabs[2].Text() != "unsaved" || !restored.Tabs[2].Dirty() {
		t.Fatal("dirty recovery was lost or replaced by a disk tab")
	}
	if err := restored.Save(1, ""); err == nil {
		t.Fatal("external update was overwritten by a restored draft")
	}
	if data, err := os.ReadFile(dirty); err != nil || string(data) != "external" {
		t.Fatalf("external file changed: %q, %v", data, err)
	}
}

func TestRestoreManifestSkipsMissingAndAliasedCleanTabs(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "first.md")
	if err := os.WriteFile(path, []byte("fresh"), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	manifest := SessionManifest{Version: 1, Root: root, ActiveID: 4, Tabs: []SessionTab{
		{ID: 2, Path: path, Caret: 99},
		{ID: 3, Path: filepath.Join(alias, "first.md"), Caret: 1},
		{ID: 4, Path: filepath.Join(root, "gone.md"), Caret: 1},
		{ID: 5, Dirty: true},
	}}
	restored := New()
	warnings, err := restored.RestoreManifest(manifest)
	if err != nil || len(warnings) != 3 {
		t.Fatalf("missing/alias restore warnings=%q err=%v", warnings, err)
	}
	if len(restored.Tabs) != 1 || restored.Tabs[0].ID != 2 || restored.Tabs[0].Editor.Caret() != 5 || restored.Tabs[0].Text() != "fresh" || restored.Active != 0 {
		t.Fatalf("skipped tabs=%+v active=%d", restored.Tabs, restored.Active)
	}
	if err := os.WriteFile(path, []byte("中"), 0600); err != nil {
		t.Fatal(err)
	}
	reloaded := New()
	if _, err := reloaded.RestoreManifest(SessionManifest{Version: 1, Tabs: []SessionTab{{ID: 2, Path: path, Caret: 99}}}); err != nil || reloaded.Current().Text() != "中" || reloaded.Current().Editor.Caret() != 1 {
		t.Fatalf("changed clean file was not loaded from current disk: %q caret=%d err=%v", reloaded.Current().Text(), reloaded.Current().Editor.Caret(), err)
	}
}

func TestRestoreManifestPreservesUnlistedDirtyDraftAndRejectsBadMetadata(t *testing.T) {
	recovered := New()
	recovered.Current().Replace("draft")
	recovery := recovered.RecoverySnapshot()
	restored := New()
	if err := restored.RestoreRecovery(recovery); err != nil {
		t.Fatal(err)
	}
	before := restored.Tabs[0]
	if _, err := restored.RestoreManifest(SessionManifest{Version: 1, ActiveID: 99, Tabs: []SessionTab{{ID: 1, Dirty: true}}}); !errors.Is(err, ErrCorruptManifest) || restored.Tabs[0] != before {
		t.Fatalf("corrupt manifest partially changed recovery: %v", err)
	}
	path := filepath.Join(t.TempDir(), "clean.md")
	if err := os.WriteFile(path, []byte("clean"), 0600); err != nil {
		t.Fatal(err)
	}
	manifest := SessionManifest{Version: 1, Tabs: []SessionTab{{ID: 7, Path: path, Caret: 2}}}
	warnings, err := restored.RestoreManifest(manifest)
	if err != nil || !reflect.DeepEqual(warnings, []string(nil)) || len(restored.Tabs) != 2 || restored.Tabs[0].ID != 7 || restored.Tabs[1] != before || restored.Tabs[1].Text() != "draft" {
		t.Fatalf("unlisted draft was lost: tabs=%+v warnings=%q err=%v", restored.Tabs, warnings, err)
	}
	if selection := restored.Tabs[1].Editor.Selection(); selection != (text.Range{Start: 0, End: 0}) {
		t.Fatalf("unlisted draft changed selection: %+v", selection)
	}
}

func TestRestoreManifestCanonicalizesWorkspaceRootForFileTree(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("body"), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "linked-workspace")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	restored := New()
	warnings, err := restored.RestoreManifest(SessionManifest{
		Version: 1, Root: alias, Tabs: []SessionTab{{ID: 2, Path: filepath.Join(alias, "note.md")}},
	})
	canonical, resolveErr := filepath.EvalSymlinks(root)
	if err != nil || resolveErr != nil || len(warnings) != 0 || restored.Root != canonical || len(restored.Nodes) != 1 || restored.Nodes[0].Path != filepath.Join(canonical, "note.md") {
		t.Fatalf("aliased workspace root=%q nodes=%+v warnings=%q errors=%v/%v", restored.Root, restored.Nodes, warnings, err, resolveErr)
	}
}
