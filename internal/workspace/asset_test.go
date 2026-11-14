package workspace

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, image.NewNRGBA(image.Rect(0, 0, 2, 1))); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestWritePNGAssetCreatesContentAddressedLocalFile(t *testing.T) {
	root := t.TempDir()
	document := filepath.Join(root, "note.mh")
	if err := os.WriteFile(document, []byte("<p>note</p>"), 0644); err != nil {
		t.Fatal(err)
	}
	imageBytes := testPNG(t)
	first, err := WritePNGAsset(document, assetLocation(t, document), imageBytes)
	if err != nil || !strings.HasPrefix(first, "assets/") || !strings.HasSuffix(first, ".png") {
		t.Fatalf("write asset = %q, %v", first, err)
	}
	second, err := WritePNGAsset(document, assetLocation(t, document), imageBytes)
	if err != nil || second != first {
		t.Fatalf("deduplication = %q, %v; first=%q", second, err, first)
	}
	read, err := ReadAsset("", document, assetLocation(t, document), first)
	if err != nil || !bytes.Equal(read, imageBytes) {
		t.Fatalf("read pasted image = %d bytes, %v", len(read), err)
	}
	if entries, err := os.ReadDir(filepath.Join(root, "assets")); err != nil || len(entries) != 1 {
		t.Fatalf("asset files = %v, %v", entries, err)
	}
}

func TestWritePNGAssetRejectsUnsafeTargetAndInvalidData(t *testing.T) {
	root := t.TempDir()
	document := filepath.Join(root, "note.mh")
	if err := os.WriteFile(document, []byte("<p>note</p>"), 0644); err != nil {
		t.Fatal(err)
	}
	saved := assetLocation(t, document)
	if _, err := WritePNGAsset(document, saved, []byte("not PNG")); err == nil {
		t.Fatal("invalid image was written")
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "assets")); err != nil {
		t.Fatal(err)
	}
	if _, err := WritePNGAsset(document, saved, testPNG(t)); !errors.Is(err, ErrOutsideWorkspace) {
		t.Fatalf("symlink asset directory = %v", err)
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("outside directory modified: %v, %v", entries, err)
	}
	if _, err := WritePNGAsset(filepath.Join(root, "other.mh"), saved, testPNG(t)); !errors.Is(err, ErrOutsideWorkspace) {
		t.Fatalf("changed document identity = %v", err)
	}
}

func TestWritePNGAssetRejectsParentSwappedBeforeOpen(t *testing.T) {
	parent := t.TempDir()
	notes := filepath.Join(parent, "notes")
	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(notes, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0755); err != nil {
		t.Fatal(err)
	}
	document := filepath.Join(notes, "note.mh")
	for _, path := range []string{document, filepath.Join(outside, "note.mh")} {
		if err := os.WriteFile(path, []byte("<p>note</p>"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	saved := assetLocation(t, document)
	imageBytes := testPNG(t)
	_, err := writePNGAsset(document, saved, imageBytes, func(stage string) {
		if stage != "document" {
			return
		}
		if err := os.Rename(notes, filepath.Join(parent, "old-notes")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, notes); err != nil {
			t.Fatal(err)
		}
	})
	if !errors.Is(err, ErrOutsideWorkspace) {
		t.Fatalf("swapped document directory = %v", err)
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 1 {
		t.Fatalf("outside directory changed: %v, %v", entries, err)
	}
}

func TestWritePNGAssetRejectsAssetDirectorySwappedBeforeOpen(t *testing.T) {
	root := t.TempDir()
	document := filepath.Join(root, "note.mh")
	if err := os.WriteFile(document, []byte("<p>note</p>"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	_, err := writePNGAsset(document, assetLocation(t, document), testPNG(t), func(stage string) {
		if stage != "assets" {
			return
		}
		if err := os.Rename(filepath.Join(root, "assets"), filepath.Join(root, "old-assets")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(root, "assets")); err != nil {
			t.Fatal(err)
		}
	})
	if !errors.Is(err, ErrOutsideWorkspace) {
		t.Fatalf("swapped asset directory = %v", err)
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("outside directory changed: %v, %v", entries, err)
	}
}

func assetLocation(t *testing.T, document string) string {
	t.Helper()
	location, err := CanonicalLocation(document)
	if err != nil {
		t.Fatal(err)
	}
	return location
}

func TestReadAssetConfinesRelativePathsToWorkspace(t *testing.T) {
	root := t.TempDir()
	notes := filepath.Join(root, "notes")
	assets := filepath.Join(root, "assets")
	if err := os.MkdirAll(notes, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(assets, 0755); err != nil {
		t.Fatal(err)
	}
	document := filepath.Join(notes, "note.mh")
	imagePath := filepath.Join(assets, "image.png")
	if err := os.WriteFile(document, []byte("<p>image</p>"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(imagePath, []byte("image bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	data, err := ReadAsset(root, document, assetLocation(t, document), "../assets/image.png")
	if err != nil || !bytes.Equal(data, []byte("image bytes")) {
		t.Fatalf("workspace relative asset=%q, %v", data, err)
	}
	for _, path := range []string{"../../outside.png", "/etc/passwd", "https://example.org/p.png", "assets/../image.png", "..\\outside.png"} {
		if _, err := ReadAsset(root, document, assetLocation(t, document), path); !errors.Is(err, ErrOutsideWorkspace) {
			t.Errorf("unsafe path %q = %v", path, err)
		}
	}
}

func TestReadAssetRejectsSymlinkEscapesAndOversizedFile(t *testing.T) {
	root := t.TempDir()
	document := filepath.Join(root, "note.mh")
	if err := os.WriteFile(document, []byte("<p>image</p>"), 0644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outside, []byte("private bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(root, "linkdir")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"link.png", "linkdir/outside.png"} {
		if data, err := ReadAsset(root, document, assetLocation(t, document), path); !errors.Is(err, ErrOutsideWorkspace) || bytes.Contains(data, []byte("private")) {
			t.Fatalf("symlink escape %q = %q, %v", path, data, err)
		}
	}
	large := filepath.Join(root, "large.png")
	file, err := os.Create(large)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(9 << 20); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAsset(root, document, assetLocation(t, document), "large.png"); !errors.Is(err, ErrAssetTooLarge) {
		t.Fatalf("oversized image=%v", err)
	}
}

func TestReadAssetStandaloneDocumentIsLimitedToItsDirectory(t *testing.T) {
	root := t.TempDir()
	document := filepath.Join(root, "note.mh")
	if err := os.WriteFile(document, []byte("<p>image</p>"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "local.png"), []byte("local"), 0644); err != nil {
		t.Fatal(err)
	}
	if data, err := ReadAsset("", document, assetLocation(t, document), "local.png"); err != nil || string(data) != "local" {
		t.Fatalf("standalone local image=%q, %v", data, err)
	}
	if _, err := ReadAsset("", document, assetLocation(t, document), "../outside.png"); !errors.Is(err, ErrOutsideWorkspace) {
		t.Fatalf("standalone escape=%v", err)
	}
}

func TestReadAssetRejectsSwappedDocumentParent(t *testing.T) {
	root := t.TempDir()
	notes := filepath.Join(root, "notes")
	if err := os.Mkdir(notes, 0755); err != nil {
		t.Fatal(err)
	}
	document := filepath.Join(notes, "note.mh")
	if err := os.WriteFile(document, []byte("<p>image</p>"), 0644); err != nil {
		t.Fatal(err)
	}
	saved := assetLocation(t, document)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "image.png"), []byte("outside private image"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(notes, filepath.Join(root, "original-notes")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, notes); err != nil {
		t.Fatal(err)
	}
	if data, err := ReadAsset(root, document, saved, "image.png"); !errors.Is(err, ErrOutsideWorkspace) || bytes.Contains(data, []byte("outside private")) {
		t.Fatalf("swapped parent exposed external bytes: %q, %v", data, err)
	}
}

func TestReadAssetRejectsRootSwappedBetweenValidationAndOpen(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "work")
	outside := filepath.Join(parent, "outside")
	for _, directory := range []string{root, outside} {
		if err := os.Mkdir(directory, 0755); err != nil {
			t.Fatal(err)
		}
	}
	document := filepath.Join(root, "note.mh")
	for _, path := range []string{document, filepath.Join(outside, "note.mh")} {
		if err := os.WriteFile(path, []byte("<p>image</p>"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "p.png"), []byte("safe image"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "p.png"), []byte("outside private image"), 0644); err != nil {
		t.Fatal(err)
	}
	saved := assetLocation(t, document)
	data, err := readAsset(root, document, saved, "p.png", func() {
		if err := os.Rename(root, filepath.Join(parent, "original-work")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, root); err != nil {
			t.Fatal(err)
		}
	})
	if !errors.Is(err, ErrOutsideWorkspace) || bytes.Contains(data, []byte("outside private")) {
		t.Fatalf("swapped root exposed external bytes: %q, %v", data, err)
	}
}
