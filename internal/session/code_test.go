package session

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"markstack/internal/workspace"
)

func TestCodeAndPlainDocumentsKeepKindsAndRawBytes(t *testing.T) {
	root := t.TempDir()
	code := filepath.Join(root, "main.go")
	plain := filepath.Join(root, "literal.txt")
	codeBytes := append([]byte{0xef, 0xbb, 0xbf}, []byte("package main\r\n\r\n// 中文\r\n")...)
	plainBytes := []byte("# literal heading\n<h1>still text</h1>\n")
	for path, data := range map[string][]byte{code: codeBytes, plain: plainBytes} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	opened := New()
	codeDocument, err := opened.Open(code)
	if err != nil || codeDocument.Kind != workspace.CodeKind {
		t.Fatalf("code open=%+v, %v", codeDocument, err)
	}
	if err := opened.Save(opened.Active, ""); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(code); err != nil || !bytes.Equal(data, codeBytes) {
		t.Fatalf("code no-op save=%q, %v", data, err)
	}
	if err := codeDocument.EditRange(len("package "), len("package main"), "tool"); err != nil {
		t.Fatal(err)
	}
	if err := opened.Save(opened.Active, ""); err != nil {
		t.Fatal(err)
	}
	want := append([]byte{0xef, 0xbb, 0xbf}, []byte("package tool\r\n\r\n// 中文\r\n")...)
	if data, err := os.ReadFile(code); err != nil || !bytes.Equal(data, want) || codeDocument.Dirty() {
		t.Fatalf("code edit/save=%q, %v", data, err)
	}
	plainDocument, err := opened.Open(plain)
	if err != nil || plainDocument.Kind != workspace.PlainKind || plainDocument.Text() != string(plainBytes) {
		t.Fatalf("literal plain open=%+v, %v", plainDocument, err)
	}
	if err := opened.Save(opened.Active, ""); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(plain); err != nil || !bytes.Equal(data, plainBytes) {
		t.Fatalf("plain no-op save=%q, %v", data, err)
	}
}

func TestCodeSaveAsAndRenameRejectImplicitKindConversion(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sample.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	opened := New()
	if changed, err := opened.RequestWorkspace(root); err != nil || !changed {
		t.Fatalf("workspace=%v, %v", changed, err)
	}
	document, err := opened.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	document.Replace("package code\n")
	plainTarget := filepath.Join(root, "wrong.txt")
	if err := opened.Save(opened.Active, plainTarget); err == nil {
		t.Fatal("code was silently saved as plain text")
	}
	if _, err := os.Lstat(plainTarget); !errors.Is(err, os.ErrNotExist) || !document.Dirty() {
		t.Fatalf("cross-kind Save As changed target or buffer: %v", err)
	}
	if err := opened.RenameNode(path, "wrong.txt"); err == nil {
		t.Fatal("open code file was renamed to a different kind")
	}
	if err := opened.RenameNode(path, "renamed.ts"); err != nil {
		t.Fatal(err)
	}
	if document.Kind != workspace.CodeKind || filepath.Base(document.Path) != "renamed.ts" || !document.Dirty() {
		t.Fatalf("same-kind rename lost draft: %+v", document)
	}
	if err := opened.Save(opened.Active, ""); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(document.Path); err != nil || string(data) != "package code\n" {
		t.Fatalf("same-kind renamed Save=%q, %v", data, err)
	}
}

func TestPlainDraftRecoveryAndBinaryCodeRefusal(t *testing.T) {
	opened := New()
	draft := opened.NewPlainText()
	if draft.Kind != workspace.PlainKind || draft.Path != "" || filepath.Ext(draft.Title) != ".txt" {
		t.Fatalf("plain draft=%+v", draft)
	}
	draft.Replace("# not Markdown\n")
	restored := New()
	if err := restored.RestoreRecovery(opened.RecoverySnapshot()); err != nil {
		t.Fatal(err)
	}
	if restored.Current().Kind != workspace.PlainKind || restored.Current().Text() != "# not Markdown\n" || !restored.Current().Dirty() {
		t.Fatalf("plain recovery=%+v", restored.Current())
	}
	root := t.TempDir()
	binaryPath := filepath.Join(root, "bad.go")
	if err := os.WriteFile(binaryPath, []byte{'a', 0, 'b'}, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := restored.Open(binaryPath); !errors.Is(err, workspace.ErrReadOnlyEncoding) || restored.Current().Text() != "# not Markdown\n" {
		t.Fatalf("binary code open=%v", err)
	}
}
