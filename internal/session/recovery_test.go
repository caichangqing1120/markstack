package session

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"markstack/internal/workspace"
)

func TestRecoveryStoreRoundTripIsPrivateAndDoesNotTouchDocuments(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	original := append([]byte{0xef, 0xbb, 0xbf}, []byte("# 原文\r\n")...)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	baseline, err := workspace.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	store := RecoveryStore{Path: filepath.Join(root, "MarkStack", "recovery.json")}
	want := RecoverySnapshot{
		Version: 1, Root: root, ActiveID: 2,
		Drafts: []RecoveryDraft{
			{ID: 1, Path: path, Title: "note.md", Body: "# 修改\r\n", Saved: "# 原文\r\n", BOM: true, Newline: "\r\n", DiskExists: true, DiskSHA256: sha256.Sum256(original), DiskLocation: baseline.Location()},
			{ID: 2, Title: "未命名-2.md", Body: "草稿", Saved: "", Newline: "\n"},
		},
	}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	dirInfo, err := os.Stat(filepath.Dir(store.Path))
	if err != nil || dirInfo.Mode().Perm() != 0700 {
		t.Fatalf("recovery directory mode=%v err=%v", dirInfo.Mode(), err)
	}
	fileInfo, err := os.Stat(store.Path)
	if err != nil || fileInfo.Mode().Perm() != 0600 {
		t.Fatalf("recovery file mode=%v err=%v", fileInfo.Mode(), err)
	}
	got, err := store.Load()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("recovery roundtrip=%+v err=%v, want=%+v", got, err, want)
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, original) {
		t.Fatalf("journal touched Markdown file=%q err=%v", data, err)
	}
	if err := store.Save(RecoverySnapshot{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(store.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("clean journal still exists: %v", err)
	}
}

func TestRecoveryStorePreservesCorruptOrUnsupportedJournal(t *testing.T) {
	store := RecoveryStore{Path: filepath.Join(t.TempDir(), "MarkStack", "recovery.json")}
	if err := os.Mkdir(filepath.Dir(store.Path), 0700); err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{[]byte("{broken"), []byte(`{"version":99,"drafts":[]}`), {0xff}} {
		if err := os.WriteFile(store.Path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Load(); !errors.Is(err, ErrCorruptRecovery) {
			t.Fatalf("corrupt load error=%v", err)
		}
		if err := store.Save(RecoverySnapshot{}); !errors.Is(err, ErrCorruptRecovery) {
			t.Fatalf("clean save erased corrupt journal: %v", err)
		}
		got, err := os.ReadFile(store.Path)
		if err != nil || !bytes.Equal(got, raw) {
			t.Fatalf("corrupt journal changed=%x err=%v", got, err)
		}
	}
	if err := os.Truncate(store.Path, maxRecoveryBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); !errors.Is(err, ErrCorruptRecovery) {
		t.Fatalf("oversized journal error=%v", err)
	}
}

func TestRecoveryStoreRejectsSymlinkWithoutFollowingTarget(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "MarkStack")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside.json")
	want := []byte("private outside")
	if err := os.WriteFile(target, want, 0600); err != nil {
		t.Fatal(err)
	}
	store := RecoveryStore{Path: filepath.Join(dir, "recovery.json")}
	if err := os.Symlink(target, store.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); !errors.Is(err, ErrUnsafeRecoveryPath) {
		t.Fatalf("symlink load error=%v", err)
	}
	if err := store.Save(RecoverySnapshot{Drafts: []RecoveryDraft{{ID: 1, Title: "未命名.md", Body: "dirty", Newline: "\n"}}}); !errors.Is(err, ErrUnsafeRecoveryPath) {
		t.Fatalf("symlink save error=%v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("outside journal target changed=%q err=%v", got, err)
	}
}

func TestRecoveryStoreRejectsCleanOrMalformedDrafts(t *testing.T) {
	store := RecoveryStore{Path: filepath.Join(t.TempDir(), "MarkStack", "recovery.json")}
	for _, snapshot := range []RecoverySnapshot{
		{Drafts: []RecoveryDraft{{ID: 1, Title: "note.md", Body: "same", Saved: "same", Newline: "\n"}}},
		{Drafts: []RecoveryDraft{{ID: 1, Path: "/tmp/no-baseline.md", Title: "no-baseline.md", Body: "draft", Newline: "\n"}}},
		{Drafts: []RecoveryDraft{{ID: 1, Title: "a.md", Body: "a", Newline: "\n"}, {ID: 1, Title: "b.md", Body: "b", Newline: "\n"}}},
		{Drafts: []RecoveryDraft{{ID: 1, Path: "/tmp/same.md", Title: "a.md", Body: "a", Newline: "\n", DiskExists: true, DiskLocation: "/tmp/same.md"}, {ID: 2, Path: "/tmp/same.md", Title: "b.md", Body: "b", Newline: "\n", DiskExists: true, DiskLocation: "/tmp/same.md"}}},
		{Drafts: []RecoveryDraft{{ID: 1, Title: "bad.md", Body: "\xff", Newline: "\n"}}},
	} {
		if err := store.Save(snapshot); !errors.Is(err, ErrCorruptRecovery) {
			t.Errorf("invalid snapshot %+v accepted: %v", snapshot, err)
		}
	}
	if _, err := os.Lstat(store.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid drafts created journal: %v", err)
	}
}

func TestRecoverySessionCapturesOnlyDirtyTabsAndRestoresActiveDraft(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "named.md")
	original := append([]byte{0xef, 0xbb, 0xbf}, []byte("# 原文\r\n")...)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	session := New()
	session.Root = root
	untitled := session.NewDocument()
	untitled.Replace("未保存草稿")
	named, err := session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	named.Replace("# 本地修改\r\n")
	snapshot := session.RecoverySnapshot()
	if len(snapshot.Drafts) != 2 || snapshot.ActiveID != named.ID || snapshot.Drafts[0].ID != untitled.ID || !snapshot.Drafts[1].DiskExists || snapshot.Drafts[1].DiskSHA256 != sha256.Sum256(original) {
		t.Fatalf("dirty-only snapshot=%+v", snapshot)
	}
	restored := New()
	if err := restored.RestoreRecovery(snapshot); err != nil {
		t.Fatal(err)
	}
	if len(restored.Tabs) != 2 || restored.Current().ID != named.ID || restored.Tabs[0].Text() != "未保存草稿" || restored.Current().Text() != "# 本地修改\r\n" || !restored.Tabs[0].Dirty() || !restored.Current().Dirty() {
		t.Fatalf("restored session tabs=%+v active=%d", restored.Tabs, restored.Active)
	}
	if next := restored.NewDocument(); next.ID <= named.ID {
		t.Fatalf("new ID %d reused recovered ID %d", next.ID, named.ID)
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, original) {
		t.Fatalf("restore wrote disk=%q err=%v", data, err)
	}
}

func TestRecoveryRestoreExternalChangeKeepsConflictAcrossRestarts(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "named.md")
	original := append([]byte{0xef, 0xbb, 0xbf}, []byte("# 原文\r\n")...)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	session := New()
	document, err := session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	document.Replace("# 恢复的草稿\r\n")
	snapshot := session.RecoverySnapshot()
	external := []byte("# 外部更新\n")
	if err := os.WriteFile(path, external, 0600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		restored := New()
		if err := restored.RestoreRecovery(snapshot); err != nil {
			t.Fatal(err)
		}
		if !restored.Current().Dirty() || restored.Current().Text() != "# 恢复的草稿\r\n" {
			t.Fatalf("conflict recovery lost draft: %+v", restored.Current())
		}
		if err := restored.Save(restored.Active, ""); !errors.Is(err, workspace.ErrConflict) {
			t.Fatalf("external change was overwritten: %v", err)
		}
		if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, external) {
			t.Fatalf("external content changed=%q err=%v", data, err)
		}
		snapshot = restored.RecoverySnapshot()
		if snapshot.Drafts[0].DiskSHA256 != sha256.Sum256(original) {
			t.Fatal("second recovery adopted the external writer as its baseline")
		}
	}
	other := filepath.Join(root, "recovered.md")
	restored := New()
	if err := restored.RestoreRecovery(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := restored.Save(restored.Active, other); err != nil {
		t.Fatal(err)
	}
	want := append([]byte{0xef, 0xbb, 0xbf}, []byte("# 恢复的草稿\r\n")...)
	if data, err := os.ReadFile(other); err != nil || !bytes.Equal(data, want) || restored.Current().Dirty() {
		t.Fatalf("recovered Save As=%q dirty=%v err=%v", data, restored.Current().Dirty(), err)
	}
}

func TestRecoveryRestoreDeletedOrSymlinkSwappedFileCannotSaveInPlace(t *testing.T) {
	for _, action := range []string{"delete", "symlink"} {
		t.Run(action, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "note.md")
			if err := os.WriteFile(path, []byte("original\n"), 0600); err != nil {
				t.Fatal(err)
			}
			session := New()
			document, err := session.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			document.Replace("draft\n")
			snapshot := session.RecoverySnapshot()
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "outside.md")
			if action == "symlink" {
				if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			}
			restored := New()
			if err := restored.RestoreRecovery(snapshot); err != nil {
				t.Fatal(err)
			}
			if err := restored.Save(restored.Active, ""); !errors.Is(err, workspace.ErrConflict) {
				t.Fatalf("%s Save conflict=%v", action, err)
			}
			if action == "delete" {
				if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("deleted path recreated: %v", err)
				}
			} else if data, err := os.ReadFile(outside); err != nil || string(data) != "outside" {
				t.Fatalf("symlink target changed=%q err=%v", data, err)
			}
		})
	}
}

func TestRecoveryConflictCannotBeBypassedThroughParentDirectoryAlias(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(real, "note.md")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	session := New()
	document, err := session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	document.Replace("draft")
	snapshot := session.RecoverySnapshot()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	restored := New()
	if err := restored.RestoreRecovery(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := restored.Save(restored.Active, filepath.Join(alias, "note.md")); !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("directory alias bypassed recovered conflict: %v", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing original was silently recreated: %v", err)
	}
}

func TestRecoveryRestoreMalformedSnapshotDoesNotReplaceExistingSession(t *testing.T) {
	session := New()
	session.Current().Replace("keep current unsaved")
	broken := RecoverySnapshot{Version: 1, Drafts: []RecoveryDraft{{ID: 1, Title: "bad.md", Body: "same", Saved: "same", Newline: "\n"}}}
	if err := session.RestoreRecovery(broken); !errors.Is(err, ErrCorruptRecovery) {
		t.Fatalf("malformed restore error=%v", err)
	}
	if len(session.Tabs) != 1 || session.Current().Text() != "keep current unsaved" || !session.Current().Dirty() {
		t.Fatalf("malformed restore replaced session: %+v", session)
	}
}

func TestRecoveryRestoreDoesNotTrustReplacedWorkspaceRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "workspace")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	session := New()
	if changed, err := session.RequestWorkspace(root); err != nil || !changed {
		t.Fatalf("workspace changed=%v err=%v", changed, err)
	}
	document, err := session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	document.Replace("dirty draft")
	snapshot := session.RecoverySnapshot()
	if err := os.Rename(root, filepath.Join(base, "moved")); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "note.md")
	if err := os.WriteFile(outsideFile, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, root); err != nil {
		t.Fatal(err)
	}
	restored := New()
	if err := restored.RestoreRecovery(snapshot); err != nil {
		t.Fatal(err)
	}
	if restored.Root != "" || len(restored.Nodes) != 0 || restored.Current().Text() != "dirty draft" || !restored.Current().Dirty() {
		t.Fatalf("untrusted workspace was restored: root=%q nodes=%+v document=%+v", restored.Root, restored.Nodes, restored.Current())
	}
	if err := restored.Save(restored.Active, ""); !errors.Is(err, workspace.ErrConflict) {
		t.Fatalf("same-byte outside replacement bypassed conflict: %v", err)
	}
	if data, err := os.ReadFile(outsideFile); err != nil || string(data) != "original" {
		t.Fatalf("outside content changed=%q err=%v", data, err)
	}
}

func TestRecoveryStoreRejectsLoosePermissionsWithoutTouchingJournal(t *testing.T) {
	root := t.TempDir()
	store := RecoveryStore{Path: filepath.Join(root, "MarkStack", "recovery.json")}
	draft := RecoverySnapshot{Drafts: []RecoveryDraft{{ID: 1, Title: "未命名.md", Body: "dirty", Newline: "\n"}}}
	if err := store.Save(draft); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store.Path, 0644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); !errors.Is(err, ErrUnsafeRecoveryPath) {
		t.Fatalf("permissive journal load error=%v", err)
	}
	if err := store.Save(RecoverySnapshot{}); !errors.Is(err, ErrUnsafeRecoveryPath) {
		t.Fatalf("permissive journal erased by clean save: %v", err)
	}
	if after, err := os.ReadFile(store.Path); err != nil || !bytes.Equal(after, before) {
		t.Fatalf("permissive journal changed: %q, %v", after, err)
	}
	public := RecoveryStore{Path: filepath.Join(root, "public", "recovery.json")}
	if err := os.Mkdir(filepath.Dir(public.Path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(public.Path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := public.Save(draft); !errors.Is(err, ErrUnsafeRecoveryPath) {
		t.Fatalf("public directory journal save error=%v", err)
	}
	if _, err := public.Load(); !errors.Is(err, ErrUnsafeRecoveryPath) {
		t.Fatalf("public directory journal load error=%v", err)
	}
	if _, err := os.Lstat(public.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("public directory contains a journal: %v", err)
	}
}
