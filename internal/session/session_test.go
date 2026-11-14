package session

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"markstack/internal/workspace"
)

func TestEditingOneRangeKeepsBOMAndOtherCRLFLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	initial := append([]byte{0xef, 0xbb, 0xbf}, []byte("# old\r\n\r\nnext\r\n")...)
	if err := os.WriteFile(path, initial, 0644); err != nil {
		t.Fatal(err)
	}
	s := New()
	document, err := s.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := document.EditRange(2, 5, "新"); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(s.Active, ""); err != nil {
		t.Fatal(err)
	}
	want := append([]byte{0xef, 0xbb, 0xbf}, []byte("# 新\r\n\r\nnext\r\n")...)
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) || document.Dirty() {
		t.Fatalf("one range edit = %q, dirty=%v, err=%v", got, document.Dirty(), err)
	}
}

func TestRequestSameWorkspaceKeepsDirtyDraftWithoutConfirmation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	s := New()
	if changed, err := s.RequestWorkspace(root); err != nil || !changed {
		t.Fatalf("initial workspace: changed=%v err=%v", changed, err)
	}
	document, err := s.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	document.Replace("recovered draft")
	if changed, err := s.RequestWorkspace(root); err != nil || !changed || s.Pending() != nil {
		t.Fatalf("same workspace opened confirmation: changed=%v err=%v pending=%+v", changed, err, s.Pending())
	}
	if s.Current() != document || document.Text() != "recovered draft" || !document.Dirty() {
		t.Fatal("same workspace discarded the dirty draft")
	}
}

func TestExternalModificationRetainsDirtyEditorAndDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("first"), 0644); err != nil {
		t.Fatal(err)
	}
	s := New()
	document, err := s.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	document.Replace("local")
	if err := os.WriteFile(path, []byte("external"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(s.Active, ""); !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("save conflict = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "external" || document.Text() != "local" || !document.Dirty() {
		t.Fatalf("conflict disk=%q buffer=%q dirty=%v err=%v", got, document.Text(), document.Dirty(), err)
	}
}

func TestSaveAsKeepsSourceBOMAndLineEndings(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.md")
	target := filepath.Join(root, "copy.md")
	want := append([]byte{0xef, 0xbb, 0xbf}, []byte("# heading\r\n\r\nbody\r\n")...)
	if err := os.WriteFile(source, want, 0644); err != nil {
		t.Fatal(err)
	}
	s := New()
	if _, err := s.Open(source); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(s.Active, target); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("Save As changed source bytes: %q, %v", got, err)
	}
}

func TestSaveAsExistingTargetFailsWithoutOverwritingEitherFile(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.md")
	target := filepath.Join(root, "existing.md")
	if err := os.WriteFile(source, []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("other"), 0644); err != nil {
		t.Fatal(err)
	}
	s := New()
	document, err := s.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	document.Replace("local unsaved")
	if err := s.Save(s.Active, target); !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("Save As existing file = %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "other" || document.Path != source || !document.Dirty() {
		t.Fatalf("Save As target=%q, document=%q dirty=%v, %v", got, document.Path, document.Dirty(), err)
	}
}

func TestOpenDeduplicatesTabsAndTracksDirtyText(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "one.md")
	if err := os.WriteFile(path, []byte("# One"), 0644); err != nil {
		t.Fatal(err)
	}
	s := New()
	first, err := s.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if first.Editor.Caret() != 0 {
		t.Fatalf("newly opened document starts at rune %d, want the first line", first.Editor.Caret())
	}
	again, err := s.Open(path)
	if err != nil || first != again || len(s.Tabs) != 2 {
		t.Fatalf("dedup failed: %p %p, %d tabs, %v", first, again, len(s.Tabs), err)
	}
	first.Replace("# Changed")
	if !first.Dirty() {
		t.Fatal("edit should mark the tab dirty")
	}
	if err := s.Save(s.Active, ""); err != nil || first.Dirty() {
		t.Fatalf("save failed or stayed dirty: %v", err)
	}
}

func TestOpenManyRejectsInvalidFileWithoutPartialTabs(t *testing.T) {
	root := t.TempDir()
	valid := filepath.Join(root, "valid.md")
	invalid := filepath.Join(root, "binary.md")
	if err := os.WriteFile(valid, []byte("# Valid"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(invalid, []byte{0, 1, 2}, 0644); err != nil {
		t.Fatal(err)
	}
	s := New()
	draft := s.Current()
	draft.Replace("unsaved draft")
	if err := s.OpenMany([]string{valid, invalid}); err == nil {
		t.Fatal("binary second file was accepted")
	}
	if len(s.Tabs) != 1 || s.Current() != draft || !draft.Dirty() || draft.Text() != "unsaved draft" {
		t.Fatalf("failed multi-open changed tabs or draft: %+v", s.Tabs)
	}
	if got, err := os.ReadFile(valid); err != nil || string(got) != "# Valid" {
		t.Fatalf("failed multi-open changed disk: %q, %v", got, err)
	}
}

func TestOpenManyReusesTabsAndSelectsLastPath(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.md")
	second := filepath.Join(root, "second.md")
	if err := os.WriteFile(first, []byte("# First"), 0644); err != nil {
		t.Fatal(err)
	}
	want := append([]byte{0xef, 0xbb, 0xbf}, []byte("# Second\r\n")...)
	if err := os.WriteFile(second, want, 0644); err != nil {
		t.Fatal(err)
	}
	s := New()
	document, err := s.Open(first)
	if err != nil {
		t.Fatal(err)
	}
	document.Replace("local edit")
	if err := s.OpenMany([]string{first, second, second}); err != nil {
		t.Fatal(err)
	}
	if len(s.Tabs) != 3 || s.Tabs[1] != document || s.Current().Path != second || !document.Dirty() || document.Text() != "local edit" {
		t.Fatalf("multi-open tabs=%d active=%q first=%q", len(s.Tabs), s.Current().Path, document.Text())
	}
	if err := s.Save(s.Active, ""); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(second)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("no-op save changed second file: %q, %v", got, err)
	}
}

func TestOpenManyCanonicalAliasesReuseTheSameTab(t *testing.T) {
	root := t.TempDir()
	alias := filepath.Join(root, "note.md")
	if err := os.WriteFile(alias, []byte("# Note"), 0644); err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(canonicalRoot, "note.md")
	s := New()
	first, err := s.Open(alias)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.OpenMany([]string{canonical, alias}); err != nil {
		t.Fatal(err)
	}
	if len(s.Tabs) != 2 || s.Current() != first {
		t.Fatalf("aliases opened separate tabs: %+v", s.Tabs)
	}
}

func TestRichHTMLSessionRejectsCrossKindSaveAndRename(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.mh")
	original := []byte("<p>原文</p>\r\n<!--keep-->")
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	s := New()
	if changed, err := s.RequestWorkspace(root); err != nil || !changed {
		t.Fatalf("open workspace=%v, %v", changed, err)
	}
	document, err := s.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if document.Kind != workspace.RichHTMLKind || document.Text() != string(original) {
		t.Fatalf("rich document=%+v", document)
	}
	if err := s.Save(s.Active, ""); err != nil {
		t.Fatal(err)
	}
	markdownTarget := filepath.Join(root, "converted.md")
	if err := s.Save(s.Active, markdownTarget); err == nil {
		t.Fatal("cross-kind Save As converted HTML to Markdown")
	}
	if _, err := os.Lstat(markdownTarget); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cross-kind target exists: %v", err)
	}
	if err := s.RenameNode(path, "converted.md"); err == nil {
		t.Fatal("cross-kind rename changed an open rich document")
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, original) || document.Path != path {
		t.Fatalf("cross-kind action changed source: %q, %v", got, err)
	}
}

func TestNewRichHTMLRecoveryRemainsRichAndUnnamed(t *testing.T) {
	s := New()
	document := s.NewRichHTML()
	if document.Kind != workspace.RichHTMLKind || document.Path != "" || !document.Dirty() || filepath.Ext(document.Title) != ".mh" {
		t.Fatalf("new rich document=%+v", document)
	}
	document.Replace("<p>new rich text</p>")
	restored := New()
	if err := restored.RestoreRecovery(s.RecoverySnapshot()); err != nil {
		t.Fatal(err)
	}
	if restored.Current().Kind != workspace.RichHTMLKind || restored.Current().Path != "" || restored.Current().Text() != "<p>new rich text</p>" || !restored.Current().Dirty() {
		t.Fatalf("restored rich draft=%+v", restored.Current())
	}
}

func TestCancelCloseRetainsUnsavedDocument(t *testing.T) {
	s := New()
	s.Current().Replace("draft")
	if s.RequestClose(0) || s.Pending() == nil {
		t.Fatal("dirty close should request confirmation")
	}
	if completed, err := s.Resolve(Cancel, ""); completed || err != nil {
		t.Fatalf("cancel = %v, %v", completed, err)
	}
	if len(s.Tabs) != 1 || s.Current().Text() != "draft" || !s.Current().Dirty() {
		t.Fatal("cancel lost the unsaved document")
	}
}

func TestSaveAsCancelKeepsPendingClose(t *testing.T) {
	s := New()
	s.Current().Replace("draft")
	s.RequestClose(0)
	if _, err := s.Resolve(Save, ""); err != ErrPathRequired {
		t.Fatalf("missing Save As path = %v", err)
	}
	if s.Pending() == nil || s.Current().Text() != "draft" {
		t.Fatal("failed Save As lost the pending document")
	}
	path := filepath.Join(t.TempDir(), "draft.md")
	if completed, err := s.Resolve(Save, path); !completed || err != nil {
		t.Fatalf("Save As = %v, %v", completed, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "draft" {
		t.Fatalf("saved content = %q, %v", data, err)
	}
}

func TestCancelWorkspaceSwitchRestoresEveryUnsavedTab(t *testing.T) {
	s := New()
	s.Current().Replace("first")
	s.NewDocument().Replace("second")
	root := t.TempDir()
	if switched, err := s.RequestWorkspace(root); switched || err != nil {
		t.Fatalf("request = %v, %v", switched, err)
	}
	if completed, err := s.Resolve(Discard, ""); completed || err != nil {
		t.Fatalf("first discard = %v, %v", completed, err)
	}
	if completed, err := s.Resolve(Cancel, ""); completed || err != nil {
		t.Fatalf("cancel = %v, %v", completed, err)
	}
	if s.Root != "" || len(s.Tabs) != 2 || s.Tabs[0].Text() != "first" || s.Tabs[1].Text() != "second" || !s.Tabs[0].Dirty() {
		t.Fatal("canceled workspace switch mutated documents")
	}
}

func TestCleanWorkspaceStartsWithFirstUntitledTab(t *testing.T) {
	s := New()
	if changed, err := s.RequestWorkspace(t.TempDir()); !changed || err != nil {
		t.Fatalf("switch = %v, %v", changed, err)
	}
	if s.Current().Title != "未命名-1.md" {
		t.Fatalf("first title = %q", s.Current().Title)
	}
}
