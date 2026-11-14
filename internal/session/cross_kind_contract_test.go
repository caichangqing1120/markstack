package session

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"markstack/internal/workspace"
)

// crossKindFixture is one supported document kind with a unique source interval
// a local edit can replace, so every kind runs the same byte-level contract.
type crossKindFixture struct {
	name        string
	kind        workspace.DocumentKind
	text        string
	marker      string
	replacement string
	edited      string
}

func crossKindFixtures() []crossKindFixture {
	return []crossKindFixture{
		{
			name: "note.md", kind: workspace.MarkdownKind,
			text: "# 墨栈标题\n\n一段正文\n", marker: "一段正文", replacement: "改写正文",
			edited: "# 墨栈标题\n\n改写正文\n",
		},
		{
			name: "note.mh", kind: workspace.RichHTMLKind,
			text: "<p>前 <strong>粗</strong> 后</p>\n<!--keep-->\n", marker: "粗", replacement: "新",
			edited: "<p>前 <strong>新</strong> 后</p>\n<!--keep-->\n",
		},
		{
			name: "main.go", kind: workspace.CodeKind,
			text: "package main\n\n// 尾注释\n", marker: "尾注释", replacement: "改注释",
			edited: "package main\n\n// 改注释\n",
		},
		{
			name: "note.txt", kind: workspace.PlainKind,
			text: "纯文本 **星号** 与 <标签>\n", marker: "纯文本", replacement: "纯文本二",
			edited: "纯文本二 **星号** 与 <标签>\n",
		},
	}
}

// crossKindVariant is one encoding shape every kind must preserve exactly.
type crossKindVariant struct {
	name string
	bom  bool
	crlf bool
}

func crossKindVariants() []crossKindVariant {
	return []crossKindVariant{
		{name: "lf"},
		{name: "bom-crlf", bom: true, crlf: true},
	}
}

// withVariant turns LF fixture text into the exact bytes of one encoding shape.
func withVariant(variant crossKindVariant, text string) []byte {
	if variant.crlf {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	if variant.bom {
		return append([]byte{0xef, 0xbb, 0xbf}, text...)
	}
	return []byte(text)
}

// otherKindPath names a supported path whose kind differs from the fixture's,
// so a cross-kind save can be attempted without leaving the supported set.
func otherKindPath(directory string, fixture crossKindFixture) string {
	if fixture.kind == workspace.RichHTMLKind {
		return filepath.Join(directory, "wrong.md")
	}
	return filepath.Join(directory, "wrong.mh")
}

func requireBytes(t *testing.T, path string, want []byte, context string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", context, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s = %q, want %q", context, got, want)
	}
}

func openCrossKind(t *testing.T, root string, fixture crossKindFixture, variant crossKindVariant) (*Session, *Document, int, string, []byte) {
	t.Helper()
	path := filepath.Join(root, fixture.name)
	original := withVariant(variant, fixture.text)
	if err := os.WriteFile(path, original, 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	s := New()
	document, err := s.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if document.Kind != fixture.kind {
		t.Fatalf("opened kind = %v, want %v", document.Kind, fixture.kind)
	}
	if document.Dirty() {
		t.Fatal("a freshly opened document is already dirty")
	}
	return s, document, s.Active, path, original
}

// TestCrossKindSaveContract runs the shared save contract for .md/.mh/.go/.txt
// under LF and BOM+CRLF: a no-op save rewrites nothing, a local edit changes
// only its own interval, Undo plus Save restores the original bytes, and
// same-kind Save As copies the preserved bytes without touching the source.
func TestCrossKindSaveContract(t *testing.T) {
	for _, fixture := range crossKindFixtures() {
		for _, variant := range crossKindVariants() {
			t.Run(fixture.name+"/"+variant.name, func(t *testing.T) {
				root := t.TempDir()
				s, document, index, path, original := openCrossKind(t, root, fixture, variant)
				opened, err := os.Lstat(path)
				if err != nil {
					t.Fatal(err)
				}

				// No-op Save keeps bytes, permissions and the file identity.
				if err := s.Save(index, ""); err != nil {
					t.Fatalf("no-op save: %v", err)
				}
				saved, err := os.Lstat(path)
				if err != nil {
					t.Fatal(err)
				}
				requireBytes(t, path, original, "no-op save bytes")
				if saved.Mode().Perm() != 0640 {
					t.Fatalf("no-op save changed permissions to %v", saved.Mode().Perm())
				}
				if !os.SameFile(opened, saved) {
					t.Fatal("no-op save replaced the file instead of leaving it alone")
				}
				if document.Dirty() || document.Composing() {
					t.Fatal("no-op save left the document dirty or composing")
				}

				// A local range edit changes exactly that interval.
				start := strings.Index(document.Text(), fixture.marker)
				if start < 0 {
					t.Fatalf("fixture marker %q missing from opened text %q", fixture.marker, document.Text())
				}
				if err := document.EditRange(start, start+len(fixture.marker), fixture.replacement); err != nil {
					t.Fatal(err)
				}
				if err := s.Save(index, ""); err != nil {
					t.Fatalf("save after local edit: %v", err)
				}
				requireBytes(t, path, withVariant(variant, fixture.edited), "local edit bytes")
				if document.Dirty() {
					t.Fatal("saved document still reports a dirty draft")
				}

				// Undo then Save returns the file to its original bytes.
				if !document.Editor.Undo() {
					t.Fatal("the local edit produced no undo step")
				}
				if err := s.Save(index, ""); err != nil {
					t.Fatalf("save after undo: %v", err)
				}
				requireBytes(t, path, original, "undo save bytes")
				if document.Dirty() {
					t.Fatal("document stayed dirty after undoing the only edit")
				}
				if !document.Editor.Redo() {
					t.Fatal("the undone edit produced no redo step")
				}
				if !document.Dirty() {
					t.Fatal("redo did not restore the dirty draft")
				}
				if !document.Editor.Undo() {
					t.Fatal("second undo step missing")
				}

				// Same-kind Save As copies the preserved bytes and leaves the source.
				target := filepath.Join(root, "copy"+filepath.Ext(fixture.name))
				if err := s.Save(index, target); err != nil {
					t.Fatalf("save as %s: %v", target, err)
				}
				requireBytes(t, path, original, "source bytes after save as")
				requireBytes(t, target, original, "save as bytes")
				if document.Path != target {
					t.Fatalf("save as retargeted the document to %q, want %q", document.Path, target)
				}
				if document.Dirty() {
					t.Fatal("save as left the document dirty")
				}

				// A canceled Save As (no path chosen yet) writes nothing.
				unnamed := s.NewDocument()
				unnamed.Replace("取消另存为的草稿")
				if err := s.Save(s.Active, ""); !errors.Is(err, ErrPathRequired) {
					t.Fatalf("unnamed draft save = %v, want ErrPathRequired", err)
				}
				if !unnamed.Dirty() || unnamed.Path != "" {
					t.Fatalf("canceled save as changed the draft: path=%q dirty=%v", unnamed.Path, unnamed.Dirty())
				}

				// A different document kind is never written through the same tab.
				wrong := otherKindPath(root, fixture)
				before := document.SavedLocation()
				if err := s.Save(index, wrong); err == nil {
					t.Fatalf("saved %s as a different document kind", fixture.name)
				}
				if _, err := os.Lstat(wrong); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("cross-kind save as created %s: %v", wrong, err)
				}
				if document.SavedLocation() != before {
					t.Fatal("cross-kind save as changed the saved location")
				}

				// An existing target is a conflict, and neither file is overwritten.
				occupied := filepath.Join(root, "occupied"+filepath.Ext(fixture.name))
				if err := os.WriteFile(occupied, []byte("other"), 0644); err != nil {
					t.Fatal(err)
				}
				document.Replace("local draft 草稿")
				if err := s.Save(index, occupied); !errors.Is(err, workspace.ErrConflict) {
					t.Fatalf("save as existing target = %v, want ErrConflict", err)
				}
				requireBytes(t, occupied, []byte("other"), "occupied target bytes")
				requireBytes(t, target, original, "current document bytes after conflict")
				if document.Path != target || !document.Dirty() {
					t.Fatalf("conflict changed the document: path=%q dirty=%v", document.Path, document.Dirty())
				}
			})
		}
	}
}

// TestCrossKindSaveConflictKeepsDraft proves that an external change the editor
// did not make, an unlink, a swapped file or parent link, an occupied Save As
// target and a failing write all keep the external bytes on disk, keep the
// draft, and never clear the path or dirty state.
func TestCrossKindSaveConflictKeepsDraft(t *testing.T) {
	const draft = "保留的本地草稿"
	for _, fixture := range crossKindFixtures() {
		variant := crossKindVariants()[1] // BOM + CRLF exercises the byte contract
		t.Run(fixture.name+"/external update", func(t *testing.T) {
			root := t.TempDir()
			s, document, index, path, _ := openCrossKind(t, root, fixture, variant)
			document.Replace(draft)
			external := withVariant(variant, "外部改写\n")
			if err := os.WriteFile(path, external, 0640); err != nil {
				t.Fatal(err)
			}
			location := document.SavedLocation()

			err := s.Save(index, "")
			if !errors.Is(err, workspace.ErrConflict) {
				t.Fatalf("save over an external edit = %v, want ErrConflict", err)
			}
			requireBytes(t, path, external, "disk keeps the external version")
			if document.Text() != draft || !document.Dirty() {
				t.Fatalf("external conflict lost the draft: %q dirty=%v", document.Text(), document.Dirty())
			}
			if document.Path != path || document.SavedLocation() != location {
				t.Fatal("external conflict changed the document path")
			}
		})
		t.Run(fixture.name+"/external delete", func(t *testing.T) {
			root := t.TempDir()
			s, document, index, path, _ := openCrossKind(t, root, fixture, variant)
			document.Replace(draft)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}

			err := s.Save(index, "")
			if !errors.Is(err, workspace.ErrConflict) {
				t.Fatalf("save after delete = %v, want ErrConflict", err)
			}
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("a failed save recreated the deleted document: %v", err)
			}
			if document.Text() != draft || !document.Dirty() || document.Path != path {
				t.Fatalf("delete conflict lost the draft: %q dirty=%v path=%q", document.Text(), document.Dirty(), document.Path)
			}
		})
		t.Run(fixture.name+"/file symlink swap", func(t *testing.T) {
			root := t.TempDir()
			s, document, index, path, _ := openCrossKind(t, root, fixture, variant)
			document.Replace(draft)
			victim := filepath.Join(root, "victim"+filepath.Ext(fixture.name))
			if err := os.WriteFile(victim, []byte("victim bytes"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(victim, path); err != nil {
				t.Fatal(err)
			}

			err := s.Save(index, "")
			if err == nil {
				t.Fatal("save followed a swapped file link")
			}
			requireBytes(t, victim, []byte("victim bytes"), "symlink target bytes")
			if document.Text() != draft || !document.Dirty() || document.Path != path {
				t.Fatalf("symlink conflict lost the draft: %q dirty=%v path=%q", document.Text(), document.Dirty(), document.Path)
			}
		})
		t.Run(fixture.name+"/parent symlink swap", func(t *testing.T) {
			root := t.TempDir()
			container := filepath.Join(root, "container")
			if err := os.Mkdir(container, 0700); err != nil {
				t.Fatal(err)
			}
			s, document, index, path, original := openCrossKind(t, container, fixture, variant)
			document.Replace(draft)
			retired := filepath.Join(root, "retired")
			elsewhere := filepath.Join(root, "elsewhere")
			if err := os.Mkdir(elsewhere, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(container, retired); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(elsewhere, container); err != nil {
				t.Fatal(err)
			}

			if err := s.Save(index, ""); err == nil {
				t.Fatal("save followed a swapped parent directory link")
			}
			requireBytes(t, filepath.Join(retired, fixture.name), original, "retired parent bytes")
			if _, err := os.Lstat(filepath.Join(elsewhere, fixture.name)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("save wrote through the swapped parent: %v", err)
			}
			if document.Text() != draft || !document.Dirty() || document.Path != path {
				t.Fatalf("parent swap conflict lost the draft: %q dirty=%v path=%q", document.Text(), document.Dirty(), document.Path)
			}
		})
		t.Run(fixture.name+"/occupied save as target", func(t *testing.T) {
			root := t.TempDir()
			s, document, index, path, original := openCrossKind(t, root, fixture, variant)
			document.Replace(draft)
			occupied := filepath.Join(root, "occupied"+filepath.Ext(fixture.name))
			if err := os.WriteFile(occupied, []byte("other"), 0644); err != nil {
				t.Fatal(err)
			}

			if err := s.Save(index, occupied); !errors.Is(err, workspace.ErrConflict) {
				t.Fatalf("save as existing target = %v, want ErrConflict", err)
			}
			requireBytes(t, occupied, []byte("other"), "occupied target bytes")
			requireBytes(t, path, original, "source bytes after conflict")
			if document.Path != path || !document.Dirty() || document.Text() != draft {
				t.Fatalf("occupied target conflict changed the document: path=%q dirty=%v", document.Path, document.Dirty())
			}
		})
		t.Run(fixture.name+"/parent not a directory", func(t *testing.T) {
			root := t.TempDir()
			container := filepath.Join(root, "container")
			if err := os.Mkdir(container, 0700); err != nil {
				t.Fatal(err)
			}
			s, document, index, path, original := openCrossKind(t, container, fixture, variant)
			document.Replace(draft)
			retired := filepath.Join(root, "retired")
			if err := os.Rename(container, retired); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(container, []byte("blocker"), 0644); err != nil {
				t.Fatal(err)
			}

			if err := s.Save(index, ""); err == nil {
				t.Fatal("save succeeded although the parent directory is a regular file")
			}
			requireBytes(t, filepath.Join(retired, fixture.name), original, "retired parent bytes")
			requireBytes(t, container, []byte("blocker"), "blocker bytes")
			if document.Text() != draft || !document.Dirty() || document.Path != path {
				t.Fatalf("write failure lost the draft: %q dirty=%v path=%q", document.Text(), document.Dirty(), document.Path)
			}
		})
		if os.Geteuid() == 0 {
			continue
		}
		t.Run(fixture.name+"/permission write failure", func(t *testing.T) {
			root := t.TempDir()
			container := filepath.Join(root, "container")
			if err := os.Mkdir(container, 0700); err != nil {
				t.Fatal(err)
			}
			s, document, index, path, original := openCrossKind(t, container, fixture, variant)
			document.Replace(draft)
			if err := os.Chmod(container, 0500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(container, 0700) })

			if err := s.Save(index, ""); err == nil {
				t.Fatal("save succeeded in a directory without write permission")
			}
			requireBytes(t, path, original, "read-only parent bytes")
			if document.Text() != draft || !document.Dirty() || document.Path != path {
				t.Fatalf("permission failure lost the draft: %q dirty=%v path=%q", document.Text(), document.Dirty(), document.Path)
			}
		})
	}
}
