package markdownmedia

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"markstack/internal/workspace"
)

func auditFixture(t *testing.T, root, relative, value string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAuditImagesAcrossAssetsAndMarkdownForms(t *testing.T) {
	root := t.TempDir()
	for relative, data := range map[string]string{
		"assets/common pic.png": "111", "notes/assets/from-html.svg": "22", "notes/assets/lonely.JPG": "3333",
		"notes/assets/scaled.webp": "4", "extra/wrong.png": "ignored", "assets/readme.txt": "ignored",
		"a.md":            "\ufeff![inline](assets/common%20pic.png#fragment)\r\n![again][pic]\r\n\r\n[pic]: <assets/common pic.png>\r\n",
		"notes/other.md":  "![different folder](../assets/common%20pic.png)\n\n<img src=\"assets/from-html.svg\" srcset=\"assets/scaled.webp 2x\">\n\n`![code](assets/lonely.JPG)`\n",
		".notes/cover.md": "[image link](../assets/common%20pic.png)\n",
	} {
		auditFixture(t, root, relative, data)
	}
	result, err := Audit(context.Background(), root)
	if err != nil || result.Incomplete || result.TotalBytes != 10 || len(result.Referenced) != 3 || len(result.Orphans) != 1 {
		t.Fatalf("audit=%+v err=%v", result, err)
	}
	if image := result.Referenced[0]; image.Relative != "assets/common pic.png" || !reflect.DeepEqual(image.Documents, []string{".notes/cover.md", "a.md", "notes/other.md"}) {
		t.Fatalf("first reference: %+v", image)
	}
	if result.Orphans[0].Relative != "notes/assets/lonely.JPG" || !result.Orphans[0].Trashable() {
		t.Fatalf("orphan: %+v", result.Orphans[0])
	}
}

func TestAuditRevalidatesReferencesAndImageIdentity(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	relative := "assets/orphan.png"
	image := auditFixture(t, root, relative, "original")
	read := func() AuditResult {
		t.Helper()
		result, err := Audit(ctx, root)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	previous := read()
	selected := map[string]bool{relative: true}
	if images, err := read().ConfirmedOrphans(previous, selected); err != nil || len(images) != 1 {
		t.Fatalf("initial confirmation: %+v %v", images, err)
	}
	ref := auditFixture(t, root, "late.md", "![new](assets/orphan.png)")
	if _, err := read().ConfirmedOrphans(previous, selected); !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("new reference accepted: %v", err)
	}
	if err := os.Remove(ref); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(image, []byte("changed image size"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := read().ConfirmedOrphans(previous, selected); !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("new image revision accepted: %v", err)
	}
	if workspace.TrashAvailable() {
		if _, err := previous.Trash(root, previous.Orphans[0]); !errors.Is(err, workspace.ErrConflict) {
			t.Fatalf("changed image reached OS Trash: %v", err)
		}
	}
	previous = read()
	big := auditFixture(t, root, "unscannable.md", "")
	if err := os.Truncate(big, workspace.MaxDocumentSize+1); err != nil {
		t.Fatal(err)
	}
	if partial := read(); !partial.Incomplete {
		t.Fatal("oversized Markdown must mark the audit incomplete")
	} else if _, err := partial.ConfirmedOrphans(previous, selected); !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("incomplete audit allowed deletion: %v", err)
	}
}
