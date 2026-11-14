package session

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSessionStoreRoundTripKeepsOnlyPrivateTabMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "MarkStack", "session.json")
	store := SessionStore{Path: path}
	want := SessionManifest{
		Version: 1, Root: "/work/notes", ActiveID: 3,
		Tabs: []SessionTab{
			{ID: 1, Path: "/work/notes/first.md", Caret: 12},
			{ID: 2, Dirty: true, Caret: 3},
			{ID: 3, Path: "/work/notes/draft.mh", Dirty: true, Caret: 8},
		},
	}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	directory, err := os.Stat(filepath.Dir(path))
	if err != nil || directory.Mode().Perm() != 0700 {
		t.Fatalf("private directory = %v, %v", directory, err)
	}
	file, err := os.Stat(path)
	if err != nil || file.Mode().Perm() != 0600 {
		t.Fatalf("private manifest = %v, %v", file, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || bytes.Contains(data, []byte("document body")) || bytes.Contains(data, []byte("saved")) {
		t.Fatalf("manifest leaked content or could not be read: %q, %v", data, err)
	}
	got, err := store.Load()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("manifest = %+v, %v; want %+v", got, err, want)
	}
	if err := store.Save(SessionManifest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty manifest remained: %v", err)
	}
}

func TestSessionStoreRejectsMalformedAndOversizedManifest(t *testing.T) {
	store := SessionStore{Path: filepath.Join(t.TempDir(), "private", "session.json")}
	valid := SessionManifest{Version: 1, ActiveID: 1, Tabs: []SessionTab{{ID: 1, Path: "/notes/one.md", Caret: 2}}}
	for _, bad := range []SessionManifest{
		{Version: 99},
		{Version: 1, Root: "relative"},
		{Version: 1, Tabs: []SessionTab{{ID: 0, Path: "/notes/one.md"}}},
		{Version: 1, Tabs: []SessionTab{{ID: 1, Path: "relative.md"}}},
		{Version: 1, Tabs: []SessionTab{{ID: 1, Path: "/notes/a/../b.md"}}},
		{Version: 1, Tabs: []SessionTab{{ID: 1, Path: "/notes/one.docx"}}},
		{Version: 1, Tabs: []SessionTab{{ID: 1, Path: "/notes/one.md", Caret: -1}}},
		{Version: 1, ActiveID: 2, Tabs: []SessionTab{{ID: 1, Path: "/notes/one.md"}}},
		{Version: 1, Tabs: []SessionTab{{ID: 1, Path: "/notes/one.md"}, {ID: 1, Dirty: true}}},
		{Version: 1, Tabs: []SessionTab{{ID: 1, Path: "/notes/one.md"}, {ID: 2, Path: "/notes/one.md"}}},
		{Version: 1, Tabs: []SessionTab{{ID: 1, Dirty: false}}},
		{Version: 1, Tabs: []SessionTab{{ID: 1, Path: "/notes/\xff.md"}}},
	} {
		if err := store.Save(bad); !errors.Is(err, ErrCorruptManifest) {
			t.Errorf("invalid manifest %+v accepted: %v", bad, err)
		}
	}
	if _, err := os.Lstat(store.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid manifests created a file: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(store.Path), 0700); err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{[]byte("{broken"), []byte(`{"version":1,"tabs":[],"unknown":true}`), []byte(`{"version":1,"tabs":[]} trailing`), {0xff}} {
		if err := os.WriteFile(store.Path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Load(); !errors.Is(err, ErrCorruptManifest) {
			t.Fatalf("corrupt load = %v", err)
		}
		if err := store.Save(valid); !errors.Is(err, ErrCorruptManifest) {
			t.Fatalf("corrupt overwrite = %v", err)
		}
		if got, err := os.ReadFile(store.Path); err != nil || !bytes.Equal(got, raw) {
			t.Fatalf("corrupt manifest changed: %q, %v", got, err)
		}
	}
	if err := os.Truncate(store.Path, maxManifestBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); !errors.Is(err, ErrCorruptManifest) {
		t.Fatalf("oversized manifest = %v", err)
	}
}

func TestSessionStoreRefusesUnsafePathsWithoutTouchingTargets(t *testing.T) {
	root := t.TempDir()
	private := filepath.Join(root, "private")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.json")
	if err := os.WriteFile(outside, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	store := SessionStore{Path: filepath.Join(private, "session.json")}
	if err := os.Symlink(outside, store.Path); err != nil {
		t.Fatal(err)
	}
	valid := SessionManifest{Version: 1, Tabs: []SessionTab{{ID: 1, Path: "/notes/one.md"}}}
	if _, err := store.Load(); !errors.Is(err, ErrUnsafeManifestPath) {
		t.Fatalf("symlink load = %v", err)
	}
	if err := store.Save(valid); !errors.Is(err, ErrUnsafeManifestPath) {
		t.Fatalf("symlink overwrite = %v", err)
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "untouched" {
		t.Fatalf("outside target changed: %q, %v", got, err)
	}
	permissive := SessionStore{Path: filepath.Join(root, "public", "session.json")}
	if err := os.Mkdir(filepath.Dir(permissive.Path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := permissive.Save(valid); !errors.Is(err, ErrUnsafeManifestPath) {
		t.Fatalf("public directory accepted: %v", err)
	}
	if _, err := os.Lstat(permissive.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("public directory contains manifest: %v", err)
	}
	if err := os.Symlink(private, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	linked := SessionStore{Path: filepath.Join(root, "linked", "another.json")}
	if err := linked.Save(valid); !errors.Is(err, ErrUnsafeManifestPath) {
		t.Fatalf("symlink directory accepted: %v", err)
	}
}

func TestSessionStoreBoundsTabCountAndPathLength(t *testing.T) {
	store := SessionStore{Path: filepath.Join(t.TempDir(), "private", "session.json")}
	tooMany := SessionManifest{Version: 1}
	for index := 1; index <= maxManifestTabs+1; index++ {
		tooMany.Tabs = append(tooMany.Tabs, SessionTab{ID: index, Dirty: true})
	}
	if err := store.Save(tooMany); !errors.Is(err, ErrCorruptManifest) {
		t.Fatalf("too many tabs accepted: %v", err)
	}
	longPath := SessionManifest{Version: 1, Tabs: []SessionTab{{ID: 1, Path: "/" + strings.Repeat("a", 4097) + ".md"}}}
	if err := store.Save(longPath); !errors.Is(err, ErrCorruptManifest) {
		t.Fatalf("long path accepted: %v", err)
	}
}

func TestSessionStoreRefusesWidePermissionManifest(t *testing.T) {
	store := SessionStore{Path: filepath.Join(t.TempDir(), "private", "session.json")}
	valid := SessionManifest{Version: 1, Tabs: []SessionTab{{ID: 1, Path: "/notes/one.md", Caret: 2}}}
	if err := store.Save(valid); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store.Path, 0644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); !errors.Is(err, ErrUnsafeManifestPath) {
		t.Fatalf("permissive manifest load error=%v", err)
	}
	if err := store.Save(valid); !errors.Is(err, ErrUnsafeManifestPath) {
		t.Fatalf("permissive manifest overwrite error=%v", err)
	}
	if after, err := os.ReadFile(store.Path); err != nil || !bytes.Equal(after, before) {
		t.Fatalf("permissive manifest changed: %q, %v", after, err)
	}
}
