package session

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"markstack/internal/workspace"
)

func TestTemplatesCreateIndependentUnsavedMarkdownDrafts(t *testing.T) {
	s := New()
	first, err := s.NewFromTemplate(TemplateNote)
	if err != nil {
		t.Fatal(err)
	}
	note := "# 笔记\n\n## 摘要\n\n## 正文\n\n## 参考\n\n"
	if first.Text() != note || first.Path != "" || first.Title != "笔记-2.md" || !first.Dirty() {
		t.Fatalf("note draft=%+v text=%q", first, first.Text())
	}
	if selection := first.Editor.Selection(); selection.Start != 2 || selection.End != 4 {
		t.Fatalf("note title selection=%+v", selection)
	}
	first.Editor.Insert("项目记录")
	second, err := s.NewFromTemplate(TemplateProposal)
	if err != nil {
		t.Fatal(err)
	}
	proposal := "# 方案\n\n## 背景\n\n## 目标\n\n## 实施步骤\n\n## 验收标准\n\n"
	if second.Text() != proposal || second.Title != "方案-3.md" || second.Path != "" || !second.Dirty() || first.Text() != "# 项目记录\n\n## 摘要\n\n## 正文\n\n## 参考\n\n" {
		t.Fatalf("proposal=%q prior=%q", second.Text(), first.Text())
	}
	if selection := second.Editor.Selection(); selection.Start != 2 || selection.End != 4 {
		t.Fatalf("proposal title selection=%+v", selection)
	}
	before := len(s.Tabs)
	if _, err := s.NewFromTemplate(Template(255)); err == nil || len(s.Tabs) != before || s.Current() != second {
		t.Fatalf("invalid template changed session: %v, %d", err, len(s.Tabs))
	}
}

func TestTemplateSaveAsConflictAndCancelPreserveDraft(t *testing.T) {
	s := New()
	document, err := s.NewFromTemplate(TemplateNote)
	if err != nil {
		t.Fatal(err)
	}
	original := document.Text()
	root := t.TempDir()
	target := filepath.Join(root, "existing.md")
	if err := os.WriteFile(target, []byte("other"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(s.Active, target); !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("Save As collision=%v", err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "other" || document.Path != "" || !document.Dirty() {
		t.Fatalf("collision changed disk or draft: %q, %v", got, err)
	}
	if s.RequestClose(s.Active) || s.Pending() == nil {
		t.Fatal("template closed without unsaved confirmation")
	}
	if _, err := s.NewFromTemplate(TemplateProposal); err == nil || len(s.Tabs) != 2 || s.Pending() == nil {
		t.Fatalf("new template bypassed pending confirmation: %v", err)
	}
	if completed, err := s.Resolve(Cancel, ""); completed || err != nil || s.Current() != document || document.Text() != original {
		t.Fatalf("cancel changed draft: %v, %v", completed, err)
	}
	path := filepath.Join(root, "note.md")
	if err := s.Save(s.Active, path); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != original || document.Dirty() {
		t.Fatalf("explicit save=%q, dirty=%v, %v", got, document.Dirty(), err)
	}
}

func TestTemplateRecoveryStaysUnnamedAndDoesNotWriteDisk(t *testing.T) {
	s := New()
	document, err := s.NewFromTemplate(TemplateProposal)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := s.RecoverySnapshot()
	if len(snapshot.Drafts) != 1 || snapshot.Drafts[0].Path != "" || snapshot.Drafts[0].Body != document.Text() {
		t.Fatalf("template recovery snapshot=%+v", snapshot)
	}
	restored := New()
	if err := restored.RestoreRecovery(snapshot); err != nil {
		t.Fatal(err)
	}
	if restored.Current().Path != "" || restored.Current().Text() != document.Text() || !restored.Current().Dirty() {
		t.Fatalf("restored template=%+v", restored.Current())
	}
}
