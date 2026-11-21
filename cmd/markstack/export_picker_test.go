package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/vivaui/viva/app"

	"markstack/internal/workspace"
)

func TestExportRechecksSourceAfterPicker(t *testing.T) {
	requirePDFFont(t)
	formats := []struct {
		extension string
		run       func(*model, func(app.SaveFileOptions) (string, error))
	}{
		{"html", (*model).exportHTMLWithPicker},
		{"pdf", (*model).exportPDFWithPicker},
	}
	for _, format := range formats {
		for _, kind := range []string{"md", "mh"} {
			for _, scenario := range []string{"unchanged", "before-picker", "during-picker", "identity-during-picker", "source-moved", "source-symlink", "cancel", "occupied", "late-target"} {
				t.Run(format.extension+"/"+kind+"/"+scenario, func(t *testing.T) {
					root := t.TempDir()
					path := filepath.Join(root, "note."+kind)
					target := filepath.Join(root, "out."+format.extension)
					priorPath := filepath.Join(root, "prior-revision")
					original := []byte("\xef\xbb\xbf# Original\r\n\r\nKeep the remainder.\r\n")
					if kind == "mh" {
						original = []byte("\xef\xbb\xbf<h1>Original</h1>\r\n<p>Keep the remainder.</p>\r\n")
					}
					external := bytes.ReplaceAll(original, []byte("Original"), []byte("External"))
					draft := bytes.ReplaceAll(original[3:], []byte("Original"), []byte("Dirty draft"))
					if err := os.WriteFile(path, original, 0640); err != nil {
						t.Fatal(err)
					}
					if err := os.Chmod(path, 0640); err != nil {
						t.Fatal(err)
					}
					state, _ := testDriver(t)
					state.openFile(path)
					document := state.session.Current()
					if document == nil || document.Path != path {
						t.Fatal("did not open the private source")
					}
					document.Replace(string(draft))
					before := state.session.RecoverySnapshot()
					if !document.Dirty() || len(before.Drafts) != 1 {
						t.Fatal("did not prepare a dirty source")
					}
					if scenario == "before-picker" {
						if err := os.WriteFile(path, external, 0640); err != nil {
							t.Fatal(err)
						}
					}
					sentinel := []byte("Existing private target\n")
					if scenario == "occupied" {
						if err := os.WriteFile(target, sentinel, 0600); err != nil {
							t.Fatal(err)
						}
					}
					calls := 0
					format.run(state, func(options app.SaveFileOptions) (string, error) {
						calls++
						if options.DefaultExtension != format.extension {
							t.Fatalf("unexpected picker options: %+v", options)
						}
						if scenario == "during-picker" || scenario == "cancel" {
							if err := os.WriteFile(path, external, 0640); err != nil {
								t.Fatal(err)
							}
						}
						if scenario == "identity-during-picker" {
							if err := os.Rename(path, priorPath); err != nil {
								t.Fatal(err)
							}
							if err := os.WriteFile(path, original, 0640); err != nil {
								t.Fatal(err)
							}
							if err := os.Chmod(path, 0640); err != nil {
								t.Fatal(err)
							}
						}
						if scenario == "source-moved" || scenario == "source-symlink" {
							if err := os.Rename(path, priorPath); err != nil {
								t.Fatal(err)
							}
							if scenario == "source-symlink" {
								if err := os.Symlink(priorPath, path); err != nil {
									t.Fatal(err)
								}
							}
						}
						if scenario == "late-target" {
							if err := os.WriteFile(target, sentinel, 0600); err != nil {
								t.Fatal(err)
							}
						}
						if scenario == "cancel" {
							return "", app.ErrCanceled
						}
						return target, nil
					})
					if state.session.Current() != document || !document.Dirty() || !reflect.DeepEqual(state.session.RecoverySnapshot(), before) {
						t.Fatal("export changed document identity, dirty state, or recovery")
					}
					wantSource, wantCalls := original, 1
					if scenario == "before-picker" || scenario == "during-picker" || scenario == "cancel" {
						wantSource = external
					}
					if scenario == "before-picker" {
						wantCalls = 0
					}
					verifyPath := path
					if scenario == "source-moved" || scenario == "source-symlink" {
						verifyPath = priorPath
						if scenario == "source-moved" {
							if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
								t.Fatalf("moved source path changed: %v", err)
							}
						} else if link, err := os.Readlink(path); err != nil || link != priorPath {
							t.Fatalf("source link changed: %q, error=%v", link, err)
						}
					}
					if source, err := os.ReadFile(verifyPath); err != nil || !bytes.Equal(source, wantSource) {
						t.Fatalf("source bytes changed: %q, error=%v", source, err)
					}
					if info, err := os.Stat(verifyPath); err != nil || info.Mode().Perm() != 0640 {
						t.Fatalf("source permission changed: %v, error=%v", info, err)
					}
					if calls != wantCalls {
						t.Fatalf("picker called %d times, want %d", calls, wantCalls)
					}
					data, err := os.ReadFile(target)
					if scenario == "unchanged" {
						if err != nil || len(data) == 0 {
							t.Fatalf("control did not export: %v, message=%q", err, state.message)
						}
						return
					}
					if scenario == "cancel" {
						if !errors.Is(err, os.ErrNotExist) || state.message != "" {
							t.Fatalf("canceled export changed destination or status: error=%v, status=%q", err, state.message)
						}
						return
					}
					if scenario == "occupied" || scenario == "late-target" {
						if err != nil || !bytes.Equal(data, sentinel) || state.message != "导出目标已存在，请选择其他文件名" {
							t.Fatalf("existing target was not protected: %q, error=%v, status=%q", data, err, state.message)
						}
						if info, err := os.Stat(target); err != nil || info.Mode().Perm() != 0600 {
							t.Fatalf("existing target permission changed: %v, error=%v", info, err)
						}
						return
					}
					if !errors.Is(err, os.ErrNotExist) || state.message != workspace.ErrConflict.Error() {
						t.Fatalf("conflicted export wrote %d bytes, target error=%v, message=%q", len(data), err, state.message)
					}
				})
			}
		}
		t.Run(format.extension+"/untitled", func(t *testing.T) {
			state, _ := testDriver(t)
			document := state.session.Current()
			document.Replace("# Unnamed draft\n")
			before := state.session.RecoverySnapshot()
			target := filepath.Join(t.TempDir(), "untitled."+format.extension)
			format.run(state, func(app.SaveFileOptions) (string, error) { return target, nil })
			if data, err := os.ReadFile(target); err != nil || len(data) == 0 {
				t.Fatalf("untitled draft did not export: %v, status=%q", err, state.message)
			}
			if state.session.Current() != document || document.Path != "" || !document.Dirty() || !reflect.DeepEqual(before, state.session.RecoverySnapshot()) {
				t.Fatal("export changed the untitled draft")
			}
		})
	}
}
