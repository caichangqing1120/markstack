package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoveryAfterLineFreeSaveRetainsKnownCRLF(test *testing.T) {
	for _, extension := range []string{".md", ".mh", ".go", ".txt"} {
		for _, saved := range []string{"", "saved"} {
			for _, saveAs := range []bool{false, true} {
				test.Run(fmt.Sprintf("%s/saved-%q/save-as-%v", extension, saved, saveAs), func(context *testing.T) {
					root := context.TempDir()
					original := filepath.Join(root, "original"+extension)
					const bom = "\xef\xbb\xbf"
					const initial = bom + "old\r\nline"
					if err := os.WriteFile(original, []byte(initial), 0640); err != nil {
						context.Fatal(err)
					}
					assertDisk := func(path, want string, mode os.FileMode) {
						context.Helper()
						body, err := os.ReadFile(path)
						if err != nil || string(body) != want {
							context.Fatalf("disk %s = %q, want %q; err=%v", path, body, want, err)
						}
						info, err := os.Stat(path)
						if err != nil || info.Mode().Perm() != mode {
							context.Fatalf("disk permissions %s: %v, want %o; err=%v", path, info, mode, err)
						}
					}
					opened := New()
					document, err := opened.Open(original)
					if err != nil {
						context.Fatal(err)
					}
					document.Replace(saved)
					target, mode := original, os.FileMode(0640)
					if saveAs {
						target, mode = filepath.Join(root, "copy"+extension), 0644
					}
					if err := opened.Save(opened.Active, target); err != nil {
						context.Fatal(err)
					}
					document.Replace("draft")
					store := RecoveryStore{Path: filepath.Join(root, "private-state", "recovery.json")}
					if err := store.Save(opened.RecoverySnapshot()); err != nil {
						context.Fatal(err)
					}
					journal, err := store.Load()
					if err != nil {
						context.Fatal(err)
					}
					restarted := New()
					if err := restarted.RestoreRecovery(journal); err != nil {
						context.Fatalf("valid line-free CRLF draft cannot be restored: %v", err)
					}
					recovered := restarted.Current()
					if recovered.Path != target || recovered.Text() != "draft" || !recovered.Dirty() || recovered.Newline() != "\r\n" {
						context.Fatalf("recovered path=%q text=%q dirty=%v newline=%q", recovered.Path, recovered.Text(), recovered.Dirty(), recovered.Newline())
					}
					assertDisk(target, bom+saved, mode)
					if err := recovered.EditRange(len("draft"), len("draft"), "\nnext"); err != nil {
						context.Fatal(err)
					}
					if err := restarted.Save(restarted.Active, ""); err != nil {
						context.Fatal(err)
					}
					assertDisk(target, bom+"draft\r\nnext", mode)
					if saveAs {
						assertDisk(original, initial, 0640)
					}
				})
			}
		}
	}
}

func TestRecoveryRejectsNewlineMetadataAgainstExplicitDiskLineEnding(test *testing.T) {
	for _, baseline := range []struct{ contents, claimed string }{{"old\nline", "\r\n"}, {"old\r\nline", "\n"}} {
		test.Run(fmt.Sprintf("%q", baseline.contents), func(context *testing.T) {
			filename := filepath.Join(context.TempDir(), "original.md")
			if err := os.WriteFile(filename, []byte(baseline.contents), 0640); err != nil {
				context.Fatal(err)
			}
			opened := New()
			document, err := opened.Open(filename)
			if err != nil {
				context.Fatal(err)
			}
			document.Replace("draft")
			journal := opened.RecoverySnapshot()
			journal.Drafts[0].Newline = baseline.claimed
			if err := New().RestoreRecovery(journal); !errors.Is(err, ErrCorruptRecovery) {
				context.Fatalf("inconsistent explicit line ending returned %v", err)
			}
		})
	}
}
