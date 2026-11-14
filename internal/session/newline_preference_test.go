package session

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveWithoutLineEndingsRetainsKnownNewlinePreference(test *testing.T) {
	for _, extension := range []string{".md", ".mh", ".go", ".txt"} {
		for _, newline := range []string{"\n", "\r\n"} {
			for _, body := range []string{"", "plain"} {
				for _, saveAs := range []bool{false, true} {
					test.Run(fmt.Sprintf("%s/newline-%q/body-%q/save-as-%v", extension, newline, body, saveAs), func(context *testing.T) {
						root := context.TempDir()
						filename := filepath.Join(root, "original"+extension)
						initial := append([]byte{0xef, 0xbb, 0xbf}, []byte("old"+newline+"line")...)
						if err := os.WriteFile(filename, initial, 0640); err != nil {
							context.Fatal(err)
						}
						session := New()
						document, err := session.Open(filename)
						if err != nil {
							context.Fatal(err)
						}
						document.Replace(body)
						target := ""
						if saveAs {
							target = filepath.Join(root, "copy"+extension)
						}
						if err := session.Save(session.Active, target); err != nil {
							context.Fatal(err)
						}
						if document.Newline() != newline {
							context.Fatalf("save without a line ending forgot the known preference: got %q, want %q", document.Newline(), newline)
						}
						if err := session.Save(session.Active, ""); err != nil {
							context.Fatal(err)
						}
						stored, err := os.ReadFile(document.Path)
						if err != nil || !bytes.Equal(stored, append([]byte{0xef, 0xbb, 0xbf}, []byte(body)...)) || document.Dirty() {
							context.Fatalf("line-free save changed bytes or dirty state: %q, %v", stored, err)
						}
						if err := document.EditRange(len(body), len(body), "\nnext"); err != nil {
							context.Fatal(err)
						}
						if err := session.Save(session.Active, ""); err != nil {
							context.Fatal(err)
						}
						stored, err = os.ReadFile(document.Path)
						wanted := append([]byte{0xef, 0xbb, 0xbf}, []byte(body+newline+"next")...)
						if err != nil || !bytes.Equal(stored, wanted) || document.Dirty() {
							context.Fatalf("continued edit changed newline format: %q, want %q, error=%v", stored, wanted, err)
						}
						info, err := os.Stat(filename)
						if err != nil || info.Mode().Perm() != 0640 {
							context.Fatalf("source permissions changed: %v", err)
						}
						if saveAs {
							stored, err = os.ReadFile(filename)
							if err != nil || !bytes.Equal(stored, initial) {
								context.Fatal("Save As changed the original file")
							}
						}
					})
				}
			}
		}
	}
}

func TestSaveWithExplicitLineEndingsUpdatesNewlinePreference(test *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		test.Run(fmt.Sprintf("newline-%q", newline), func(context *testing.T) {
			filename := filepath.Join(context.TempDir(), "explicit.md")
			if err := os.WriteFile(filename, []byte("old\r\nline"), 0640); err != nil {
				context.Fatal(err)
			}
			session := New()
			document, err := session.Open(filename)
			if err != nil {
				context.Fatal(err)
			}
			body := "new" + newline + "line"
			document.Replace(body)
			if err := session.Save(session.Active, ""); err != nil {
				context.Fatal(err)
			}
			stored, err := os.ReadFile(filename)
			if err != nil || string(stored) != body || document.Newline() != newline {
				context.Fatalf("explicit newline was overridden: bytes=%q preference=%q error=%v", stored, document.Newline(), err)
			}
		})
	}
}
