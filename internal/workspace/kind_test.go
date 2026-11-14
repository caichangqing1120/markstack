package workspace

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDocumentKindClassifiesMarkdownAndRichHTML(t *testing.T) {
	for _, test := range []struct {
		path string
		kind DocumentKind
	}{
		{"note.md", MarkdownKind}, {"note.MARKDOWN", MarkdownKind}, {"note.mdx", MarkdownKind},
		{"note.mh", RichHTMLKind}, {"note.MH", RichHTMLKind},
		{"main.go", CodeKind}, {"app.js", CodeKind}, {"app.ts", CodeKind}, {"view.jsx", CodeKind}, {"view.tsx", CodeKind},
		{"data.json", CodeKind}, {"config.yml", CodeKind}, {"config.yaml", CodeKind}, {"page.html", CodeKind}, {"page.htm", CodeKind}, {"icon.svg", CodeKind},
		{"style.css", CodeKind}, {"script.py", CodeKind}, {"query.sql", CodeKind}, {"build.sh", CodeKind}, {"run.bash", CodeKind}, {"run.zsh", CodeKind},
		{"plain.txt", PlainKind}, {"events.log", PlainKind}, {"data.csv", PlainKind}, {"data.tsv", PlainKind},
		{"sheet.xlsx", UnsupportedKind}, {"doc.docx", UnsupportedKind}, {"image.png", UnsupportedKind}, {"unknown.bin", UnsupportedKind},
	} {
		if got := KindOf(test.path); got != test.kind {
			t.Errorf("kind of %q = %v, want %v", test.path, got, test.kind)
		}
	}
}

func TestWorkspaceScansOnlyExplicitTextKinds(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"readme.md", "rich.mh", "main.go", "literal.txt", "script.html", "image.png", "sheet.xlsx"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("text"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	nodes, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, node := range nodes {
		names = append(names, node.Name)
	}
	want := []string{"literal.txt", "main.go", "readme.md", "rich.mh", "script.html"}
	if !slices.Equal(names, want) {
		t.Fatalf("text-kind tree=%q, want %q", names, want)
	}
}

func TestWorkspaceScansRichHTMLAndCodeHTMLSeparately(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.md", "b.mh", "c.html"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("<p>text</p>"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	nodes, err := Scan(root)
	if err != nil || len(nodes) != 3 || nodes[0].Name != "a.md" || nodes[1].Name != "b.mh" || nodes[2].Name != "c.html" {
		t.Fatalf("workspace nodes=%+v, %v", nodes, err)
	}
}

func TestRichHTMLSnapshotPreservesRawBytesAndConflict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.mh")
	original := append([]byte{0xef, 0xbb, 0xbf}, []byte("<!--keep-->\r\n<p class='x' onclick='n'>A &amp; B</p>\r\n<script>never run</script>\r\n")...)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.BOM || snapshot.Newline != "\r\n" || !bytes.Equal(snapshot.Bytes, original) {
		t.Fatalf("rich snapshot=%+v", snapshot)
	}
	next, err := Write(snapshot, snapshot.Encode(snapshot.Text))
	if err != nil || !bytes.Equal(next.Bytes, original) {
		t.Fatalf("no-op save changed bytes: %q, %v", next.Bytes, err)
	}
	changed := strings.Replace(snapshot.Text, "A &amp; B", "A &amp; C", 1)
	updated, err := Write(next, next.Encode(changed))
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Replace(original, []byte("A &amp; B"), []byte("A &amp; C"), 1)
	if !bytes.Equal(updated.Bytes, want) || updated.Mode != 0600 {
		t.Fatalf("one edit changed unrelated source: %q", updated.Bytes)
	}
	if err := os.WriteFile(path, []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(updated, updated.Encode(updated.Text+"local")); !errors.Is(err, ErrConflict) {
		t.Fatalf("external change was overwritten: %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "external" {
		t.Fatalf("disk after conflict=%q, %v", got, err)
	}
}

func TestRichHTMLUnsupportedEncodingStaysReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.mh")
	if err := os.WriteFile(path, []byte{'<', 'p', '>', 0xff, '<', '/', 'p', '>'}, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); !errors.Is(err, ErrReadOnlyEncoding) {
		t.Fatalf("invalid encoding=%v", err)
	}
}
