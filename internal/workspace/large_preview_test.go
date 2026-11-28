package workspace

import (
	"errors"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestOversizedPreviewReadsBoundedUTF8Prefix(t *testing.T) {
	root := t.TempDir()
	prefix := "\ufeff# Start\r\n"
	head := prefix + strings.Repeat("a", LargePreviewSize-len(prefix)-1) + "雪"
	path := referenceFixture(t, root, "large.md", head)
	if err := os.Truncate(path, MaxDocumentSize+512); err != nil {
		t.Fatal(err)
	}
	preview, err := LoadView(path)
	if err != nil || !preview.Preview || !preview.BOM || preview.Newline != "\r\n" || preview.Size() != MaxDocumentSize+512 || len(preview.Bytes) != LargePreviewSize-1 || !utf8.ValidString(preview.Text) {
		t.Fatalf("preview: size=%d prefix=%d BOM=%v newline=%q readonly=%v err=%v", preview.Size(), len(preview.Bytes), preview.BOM, preview.Newline, preview.Preview, err)
	}
	if _, err := Load(path); !errors.Is(err, ErrDocumentTooLarge) {
		t.Fatalf("full read: %v", err)
	}
	if _, err := Write(preview, []byte("must not replace the document")); !errors.Is(err, ErrReadOnlyPreview) {
		t.Fatalf("preview write: %v", err)
	}
	if rooted, err := LoadWorkspaceMarkdownView(root, "large.md"); err != nil || rooted.Text != preview.Text || !rooted.Preview {
		t.Fatalf("rooted preview: readonly=%v err=%v", rooted.Preview, err)
	}
	if err := os.WriteFile(path, []byte(prefix), 0600); err != nil {
		t.Fatal(err)
	}
	if next, err := LoadView(path); err != nil || next.Preview || next.Text != "# Start\r\n" || next.Revision().Same(preview.Revision()) {
		t.Fatalf("normal view: %+v %v", next, err)
	}
}
