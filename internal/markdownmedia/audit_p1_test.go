package markdownmedia

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAuditIncludesRichHTMLImageReferences(t *testing.T) {
	root := t.TempDir()
	assets := filepath.Join(root, "assets")
	if err := os.MkdirAll(assets, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "p.png"), []byte("png"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "orphan.png"), []byte("png"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "note.mh"), []byte(`<p><img src="assets/p.png"></p>`), 0600); err != nil {
		t.Fatal(err)
	}

	result, err := Audit(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if result.Incomplete {
		t.Fatalf("expected complete audit")
	}
	if len(result.Referenced) != 1 || result.Referenced[0].Relative != "assets/p.png" || len(result.Referenced[0].Documents) != 1 || result.Referenced[0].Documents[0] != "note.mh" {
		t.Fatalf("referenced = %+v, want assets/p.png referenced by note.mh", result.Referenced)
	}
	if len(result.Orphans) != 1 || result.Orphans[0].Relative != "assets/orphan.png" {
		t.Fatalf("orphans = %+v, want only assets/orphan.png", result.Orphans)
	}
}

func TestSharedImageReferencesDetectsOtherMarkdownAndRichDocuments(t *testing.T) {
	root := t.TempDir()
	assets := filepath.Join(root, "assets")
	if err := os.MkdirAll(assets, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "shared.png"), []byte("png"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("![img](assets/shared.png)\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.md"), []byte("![img](assets/shared.png)\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "note.mh"), []byte(`<p><img src="assets/shared.png"></p>`), 0600); err != nil {
		t.Fatal(err)
	}

	others, incomplete, err := SharedImageReferences(context.Background(), root, "a.md", "assets/shared.png")
	if err != nil || incomplete {
		t.Fatalf("scan failed: others=%v incomplete=%v err=%v", others, incomplete, err)
	}
	if len(others) != 2 || others[0] != "b.md" || others[1] != "note.mh" {
		t.Fatalf("others = %v, want [b.md note.mh]", others)
	}
}
