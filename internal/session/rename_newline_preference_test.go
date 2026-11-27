package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRenameAfterLineFreeSaveKeepsNewlinePreference(test *testing.T) {
	for _, extension := range []string{".md", ".mh", ".go", ".txt"} {
		for _, newline := range []string{"\n", "\r\n"} {
			for _, directory := range []bool{false, true} {
				for _, saveAs := range []bool{false, true} {
					test.Run(fmt.Sprintf("%s/newline-%q/folder-%v/save-as-%v", extension, newline, directory, saveAs), func(context *testing.T) {
						root, err := filepath.EvalSymlinks(context.TempDir())
						if err != nil {
							context.Fatal(err)
						}
						originDir := filepath.Join(root, "notes")
						if err := os.Mkdir(originDir, 0755); err != nil {
							context.Fatal(err)
						}
						original := filepath.Join(originDir, "before"+extension)
						const bom = "\xef\xbb\xbf"
						initial := bom + "first" + newline + "second"
						if err := os.WriteFile(original, []byte(initial), 0640); err != nil {
							context.Fatal(err)
						}
						opened := New()
						if _, err := opened.RequestWorkspace(root); err != nil {
							context.Fatal(err)
						}
						document, err := opened.Open(original)
						if err != nil {
							context.Fatal(err)
						}
						pathBeforeRename, permission := original, os.FileMode(0640)
						saved := ""
						if saveAs {
							pathBeforeRename, permission = filepath.Join(originDir, "copy"+extension), 0644
							saved = "saved"
						}
						document.Replace(saved)
						if err := opened.Save(opened.Active, pathBeforeRename); err != nil {
							context.Fatal(err)
						}
						assertDisk := func(path, want string, mode os.FileMode) {
							context.Helper()
							data, err := os.ReadFile(path)
							if err != nil || string(data) != want {
								context.Fatalf("unexpected bytes at %s: %q, err=%v", path, data, err)
							}
							info, err := os.Stat(path)
							if err != nil || info.Mode().Perm() != mode {
								context.Fatalf("unexpected mode at %s: %v, err=%v", path, info, err)
							}
						}
						document.Replace("draft")
						sourcePath, name := pathBeforeRename, "after"+extension
						destination := filepath.Join(originDir, name)
						if directory {
							sourcePath, name = originDir, "archive"
							destination = filepath.Join(root, name, filepath.Base(pathBeforeRename))
						}
						if err := opened.RenameNode(sourcePath, name); err != nil {
							context.Fatal(err)
						}
						if _, err := os.Lstat(pathBeforeRename); !errors.Is(err, os.ErrNotExist) {
							context.Fatalf("renamed path still exists or stat failed: %v", err)
						}
						assertDisk(destination, bom+saved, permission)
						if document.Path != destination || !document.Dirty() || document.Text() != "draft" {
							context.Fatalf("rename did not retarget the open draft: path=%q dirty=%v text=%q", document.Path, document.Dirty(), document.Text())
						}
						if document.Newline() != newline {
							context.Fatalf("rename discarded known newline: got %q, want %q", document.Newline(), newline)
						}
						restarted := New()
						if err := restarted.RestoreRecovery(opened.RecoverySnapshot()); err != nil {
							context.Fatal(err)
						}
						recovered := restarted.Current()
						if recovered.Path != destination || recovered.Newline() != newline || recovered.Text() != "draft" {
							context.Fatalf("recovered wrong path, text or newline: path=%q newline=%q text=%q", recovered.Path, recovered.Newline(), recovered.Text())
						}
						if err := recovered.EditRange(len("draft"), len("draft"), "\nnext"); err != nil {
							context.Fatal(err)
						}
						if err := restarted.Save(restarted.Active, ""); err != nil {
							context.Fatal(err)
						}
						assertDisk(destination, bom+"draft"+newline+"next", permission)
						if saveAs {
							if directory {
								original = filepath.Join(root, name, filepath.Base(original))
							}
							assertDisk(original, initial, 0640)
						}
					})
				}
			}
		}
	}
}
