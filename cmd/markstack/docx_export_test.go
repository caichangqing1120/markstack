package main

import (
	"archive/zip"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
)

func TestDOCXExportWithPickerEmbedsLocalAsset(t *testing.T) {
	for _, fixture := range []struct{ extension, draft string }{
		{"md", "## 草稿\n\n![照片](<./assets/本地 图片.png>)\n"},
		{"mh", `<h2>草稿</h2><p><img src="./assets/本地 图片.png" alt="照片"></p>`},
	} {
		t.Run(fixture.extension, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "note."+fixture.extension)
			original := []byte("\xef\xbb\xbfOriginal\r\n")
			if err := os.WriteFile(path, original, 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(root, "assets"), 0755); err != nil {
				t.Fatal(err)
			}
			image := imagePNG(t, 8, 4)
			if err := os.WriteFile(filepath.Join(root, "assets", "本地 图片.png"), image, 0644); err != nil {
				t.Fatal(err)
			}
			state, _ := testDriver(t)
			if !state.openFile(path) {
				t.Fatal(state.message)
			}
			document := state.session.Current()
			document.Replace(fixture.draft)
			target := filepath.Join(root, "output.docx")
			state.exportDOCXWithPicker(func(options app.SaveFileOptions) (string, error) {
				if options.SuggestedName != "note.docx" || options.DefaultExtension != "docx" {
					t.Fatalf("picker options=%+v", options)
				}
				return target, nil
			})
			archive, err := zip.OpenReader(target)
			if err != nil {
				t.Fatalf("open export: %v, status=%q", err, state.message)
			}
			defer archive.Close()
			body, err := fs.ReadFile(archive, "word/document.xml")
			if err != nil || !bytes.Contains(body, []byte("草稿")) || !bytes.Contains(body, []byte(`w:val="Heading2"`)) {
				t.Fatalf("DOCX did not render the committed draft: %s, %v", body, err)
			}
			if embedded, err := fs.ReadFile(archive, "word/media/image1.png"); err != nil || !bytes.Equal(image, embedded) {
				t.Fatalf("DOCX did not embed the workspace asset: %v", err)
			}
			if stored, err := os.ReadFile(path); err != nil || !bytes.Equal(stored, original) || !document.Dirty() {
				t.Fatalf("export changed the source or dirty state: %v", err)
			}
		})
	}
}
