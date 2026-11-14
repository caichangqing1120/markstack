package workspace

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteNewHTMLPublishesOnlyFreshTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "export.html")
	want := []byte("<!doctype html><h1>中文</h1>")
	if err := WriteNew(target, want); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(actual, want) {
		t.Fatalf("new export=%q, %v", actual, err)
	}
	if err := WriteNew(target, []byte("replace")); !errors.Is(err, ErrConflict) {
		t.Fatalf("existing target error=%v", err)
	}
	if actual, _ := os.ReadFile(target); !bytes.Equal(actual, want) {
		t.Fatalf("existing target overwritten: %q", actual)
	}
	if err := WriteNew(filepath.Join(root, "wrong.md"), want); err == nil {
		t.Fatal("export accepted a non-HTML target")
	}
}

func TestWriteNewHTMLNeverFollowsExistingTargetSymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.html")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "linked.html")
	if err := os.Symlink(outside, target); err != nil {
		t.Fatal(err)
	}
	if err := WriteNew(target, []byte("new")); !errors.Is(err, ErrConflict) {
		t.Fatalf("symlink export=%v", err)
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "private" {
		t.Fatalf("outside target modified: %q, %v", data, err)
	}
	if staged, err := filepath.Glob(filepath.Join(root, ".markstack-*")); err != nil || len(staged) != 0 {
		t.Fatalf("temporary files remain: %v, %v", staged, err)
	}
}

func TestWriteNewPDFAllowsBinaryAndRejectsOverwrite(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "export.pdf")
	want := []byte("%PDF-1.4\n\x00binary\n%%EOF\n")
	if err := WriteNewPDF(target, want); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(target); err != nil || !bytes.Equal(data, want) {
		t.Fatalf("PDF output=%q, %v", data, err)
	}
	if err := WriteNewPDF(target, want); !errors.Is(err, ErrConflict) {
		t.Fatalf("existing PDF target error=%v", err)
	}
	if data, err := os.ReadFile(target); err != nil || !bytes.Equal(data, want) {
		t.Fatalf("existing PDF changed=%q, %v", data, err)
	}
	for _, invalid := range []struct {
		name string
		path string
		data []byte
	}{
		{"extension", "wrong.html", want},
		{"header", "bad.pdf", []byte("not a PDF\n%%EOF")},
		{"truncated", "truncated.pdf", []byte("%PDF-1.4\nmissing trailer")},
	} {
		if err := WriteNewPDF(filepath.Join(root, invalid.path), invalid.data); !errors.Is(err, ErrInvalidPDFExport) {
			t.Errorf("%s error=%v", invalid.name, err)
		}
	}
}

func TestWriteNewPDFNeverFollowsExistingTargetSymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.pdf")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "linked.pdf")
	if err := os.Symlink(outside, target); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewPDF(target, []byte("%PDF-1.4\n%%EOF")); !errors.Is(err, ErrConflict) {
		t.Fatalf("symlink PDF target error=%v", err)
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "private" {
		t.Fatalf("outside PDF modified: %q, %v", data, err)
	}
}
