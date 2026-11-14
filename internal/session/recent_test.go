package session

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecentStateKeepsNewestEightUniquePaths(t *testing.T) {
	var state RecentState
	for index := range 12 {
		state.AddWorkspace(fmt.Sprintf("/work/%02d", index))
		state.AddFile(fmt.Sprintf("/notes/%02d.md", index))
	}
	state.AddWorkspace("/work/06")
	state.AddFile("/notes/06.md")
	state.AddFile("/notes/unsupported.xlsx")
	state.AddWorkspace("relative")
	if len(state.Workspaces) != 8 || state.Workspaces[0] != "/work/06" || len(state.Files) != 8 || state.Files[0] != "/notes/06.md" {
		t.Fatalf("recent order/cap: %+v", state)
	}
	for _, path := range state.Files {
		if strings.HasSuffix(path, ".xlsx") {
			t.Fatalf("unsupported file entered history: %q", path)
		}
	}
	state.Clear()
	if len(state.Workspaces) != 0 || len(state.Files) != 0 {
		t.Fatalf("clear retained paths: %+v", state)
	}
}

func TestRecentStorePersistsPrivatePathsOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "recent.json")
	store := RecentStore{Path: path}
	state := RecentState{}
	state.AddWorkspace("/work/notes")
	state.AddFile("/work/notes/one.md")
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	directory, err := os.Stat(filepath.Dir(path))
	if err != nil || directory.Mode().Perm() != 0700 {
		t.Fatalf("directory mode=%v, err=%v", directory, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("file mode=%v, err=%v", info, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || bytes.Contains(data, []byte("document body")) || bytes.Contains(data, []byte("saved")) {
		t.Fatalf("history contained content or could not be read: %q, %v", data, err)
	}
	loaded, err := store.Load()
	if err != nil || loaded.Version != 1 || len(loaded.Workspaces) != 1 || loaded.Workspaces[0] != "/work/notes" || len(loaded.Files) != 1 || loaded.Files[0] != "/work/notes/one.md" {
		t.Fatalf("loaded history=%+v, %v", loaded, err)
	}
	loaded.Clear()
	if err := store.Save(loaded); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("clear left history file: %v", err)
	}
}

func TestRecentStoreKeepsRichHTMLFilePath(t *testing.T) {
	store := RecentStore{Path: filepath.Join(t.TempDir(), "private", "recent.json")}
	state := RecentState{}
	state.AddFile("/notes/rich.mh")
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil || len(loaded.Files) != 1 || loaded.Files[0] != "/notes/rich.mh" {
		t.Fatalf("recent rich file=%+v, %v", loaded, err)
	}
}

func TestRecentStorePreservesCorruptAndUnsafeTargets(t *testing.T) {
	root := t.TempDir()
	private := filepath.Join(root, "private")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(private, "recent.json")
	store := RecentStore{Path: path}
	corrupt := []byte(`{"version":99,"workspaces":["relative"],"files":[]}`)
	if err := os.WriteFile(path, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); !errors.Is(err, ErrCorruptRecent) {
		t.Fatalf("corrupt load = %v", err)
	}
	valid := RecentState{}
	valid.AddFile("/notes/valid.md")
	if err := store.Save(valid); !errors.Is(err, ErrCorruptRecent) {
		t.Fatalf("corrupt overwrite = %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, corrupt) {
		t.Fatalf("corrupt history changed: %q, %v", got, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.json")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(valid); !errors.Is(err, ErrUnsafeRecentPath) {
		t.Fatalf("symlink overwrite = %v", err)
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "outside" {
		t.Fatalf("outside target changed: %q, %v", got, err)
	}
	permissive := RecentStore{Path: filepath.Join(root, "public", "recent.json")}
	if err := os.Mkdir(filepath.Dir(permissive.Path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := permissive.Save(valid); !errors.Is(err, ErrUnsafeRecentPath) {
		t.Fatalf("permissive directory accepted: %v", err)
	}
	if _, err := os.Lstat(permissive.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("permissive directory contains history: %v", err)
	}
}
