package workspace

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotNoOpSavePreservesBOMCRLFAndFileIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	want := append([]byte{0xef, 0xbb, 0xbf}, []byte("# 第一行\r\n\r\n第二行\r\n")...)
	if err := os.WriteFile(path, want, 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.BOM || snapshot.Newline != "\r\n" || snapshot.Text != "# 第一行\r\n\r\n第二行\r\n" {
		t.Fatalf("snapshot metadata = %+v", snapshot)
	}
	result, err := Write(snapshot, snapshot.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("no-op save changed bytes: %x, %v", got, err)
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) || !bytes.Equal(result.Bytes, want) {
		t.Fatalf("no-op save replaced file: before=%v after=%v err=%v", before, after, err)
	}
}

func TestSnapshotRejectsExternalChangesBeforeWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("external edit"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(snapshot, []byte("local edit")); !errors.Is(err, ErrConflict) {
		t.Fatalf("external write conflict = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "external edit" {
		t.Fatalf("external content lost: %q, %v", got, err)
	}
}

func TestSnapshotWriteDistinguishesSizeAndEncoding(test *testing.T) {
	root := test.TempDir()
	path := filepath.Join(root, "note.md")
	encoded := bytes.Repeat([]byte{'a'}, MaxDocumentSize+1)
	copy(encoded, utf8BOM)
	copy(encoded[MaxDocumentSize-2:], "\r\n")
	exact := encoded[:MaxDocumentSize]
	snapshot, err := Write(Snapshot{Path: path, Mode: 0640}, exact)
	if err != nil || !bytes.Equal(snapshot.Bytes, exact) || !snapshot.BOM || snapshot.Newline != "\r\n" {
		test.Fatalf("exact-limit write did not preserve bytes and metadata: %v", err)
	}
	updated, err := Write(snapshot, exact)
	if err != nil || !snapshot.SameRevision(updated) {
		test.Fatalf("exact-limit in-place no-op changed file identity: %v", err)
	}
	target := filepath.Join(root, "copy.md")
	for _, destination := range []Snapshot{snapshot, {Path: target}} {
		if _, err := Write(destination, encoded); !errors.Is(err, ErrDocumentTooLarge) || !strings.Contains(err.Error(), "16 MiB") {
			test.Errorf("oversized write to %s did not report the size limit: %v", filepath.Base(destination.Path), err)
		}
		for _, invalid := range [][]byte{{0xff}, {'a', 0}} {
			if _, err := Write(destination, invalid); !errors.Is(err, ErrReadOnlyEncoding) {
				test.Errorf("invalid encoding was not rejected: %v", err)
			}
		}
	}
	if err := Verify(snapshot); err != nil {
		test.Fatalf("rejected write changed original bytes or identity: %v", err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0640 {
		test.Fatalf("rejected write changed original mode: %v", err)
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		test.Fatalf("rejected write created a destination: %v", err)
	}
}

func TestAtomicCreateNeverReplacesAnExistingTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "draft.md")
	if err := os.WriteFile(path, []byte("external writer"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicNew(path, []byte("local draft"), 0644); !errors.Is(err, ErrConflict) {
		t.Fatalf("atomic create collision=%v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "external writer" {
		t.Fatalf("existing target overwritten=%q err=%v", data, err)
	}
	newPath := filepath.Join(filepath.Dir(path), "fresh.md")
	if err := writeAtomicNew(newPath, []byte("new content"), 0600); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(newPath); err != nil || string(data) != "new content" {
		t.Fatalf("fresh atomic create=%q err=%v", data, err)
	}
}

func TestLoadRejectsNonUTF8WithoutChangingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.md")
	want := []byte{0x81, 0x40, 0xff}
	if err := os.WriteFile(path, want, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); !errors.Is(err, ErrReadOnlyEncoding) {
		t.Fatalf("non-UTF8 load = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("legacy bytes changed: %x, %v", got, err)
	}
}

func TestScanKeepsSupportedTextAndSkipsHiddenAndSymlinks(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"notes/idea.md": "# Idea", "notes/plain.txt": "skip", ".private/secret.md": "skip",
		"README.markdown": "# Home", "draft.mdx": "# Draft",
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "notes"), filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	nodes, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, node := range nodes {
		names = append(names, strings.Repeat("/", node.Depth)+node.Name)
	}
	want := []string{"/notes", "//idea.md", "//plain.txt", "/draft.mdx", "/README.markdown"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("nodes = %q, want %q", names, want)
	}
}

func TestReadRejectsInvalidEncodingAndSymlinks(t *testing.T) {
	root := t.TempDir()
	invalid := filepath.Join(root, "invalid.md")
	if err := os.WriteFile(invalid, []byte{0xff, 0xfe, 0x01}, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(invalid); err == nil {
		t.Fatal("invalid UTF-8 should be rejected")
	}
	linked := filepath.Join(root, "linked.md")
	if err := os.Symlink(invalid, linked); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(linked); err == nil {
		t.Fatal("symlink should be rejected")
	}
}

func TestSaveReplacesContentAndPreservesMode(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, "# 新内容\n"); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil || got != "# 新内容\n" {
		t.Fatalf("Read = %q, %v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %v, %v", info.Mode(), err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("save left temporary files: %v, %v", entries, err)
	}
}

func TestSaveErrorLeavesExistingFileUnchanged(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, string([]byte{0xff})); err == nil {
		t.Fatal("invalid UTF-8 should fail")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "original" {
		t.Fatalf("original content changed: %q, %v", data, err)
	}
}
