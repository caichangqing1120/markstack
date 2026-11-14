package session

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPreferencesStoreDefaultsAndPrivateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "settings.json")
	store := PreferencesStore{Path: path}
	defaultState, err := store.Load()
	if err != nil || defaultState != (Preferences{Version: 1, SourceSize: 16, ReadingSize: 16, ReadingTheme: "standard"}) {
		t.Fatalf("missing preferences=%+v, %v", defaultState, err)
	}
	next := Preferences{Version: 1, Dark: true, SourceSize: 20, ReadingSize: 18, ReadingTheme: "soft"}
	if err := store.Save(next); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil || loaded != next {
		t.Fatalf("reloaded preferences=%+v, %v", loaded, err)
	}
	if info, err := os.Stat(filepath.Dir(path)); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("settings directory mode=%v, %v", info, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("settings file mode=%v, %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "recent.json")); !os.IsNotExist(err) {
		t.Fatalf("settings wrote recent history: %v", err)
	}
}

func TestPreferencesAutoSaveDefaultsOffAndReadsLegacyVersionOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "settings.json")
	store := PreferencesStore{Path: path}
	if state, err := store.Load(); err != nil || state.AutoSave {
		t.Fatalf("new settings unexpectedly auto-save: %+v, %v", state, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"version":1,"dark":false,"sourceSize":16,"readingSize":16,"readingTheme":"standard"}`)
	if err := os.WriteFile(path, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	state, err := store.Load()
	if err != nil || state.AutoSave {
		t.Fatalf("legacy settings unexpectedly auto-save: %+v, %v", state, err)
	}
	state.AutoSave = true
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil || !loaded.AutoSave {
		t.Fatalf("enabled auto-save did not persist: %+v, %v", loaded, err)
	}
}

func TestPreferencesStoreRejectsCorruptValuesAndExistingBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "settings.json")
	store := PreferencesStore{Path: path}
	for _, state := range []Preferences{
		{Version: 2, SourceSize: 16, ReadingSize: 16, ReadingTheme: "standard"},
		{Version: 1, SourceSize: 13, ReadingSize: 16, ReadingTheme: "standard"},
		{Version: 1, SourceSize: 16, ReadingSize: 23, ReadingTheme: "standard"},
		{Version: 1, SourceSize: 16, ReadingSize: 16, ReadingTheme: "unknown"},
	} {
		if err := store.Save(state); !errors.Is(err, ErrCorruptPreferences) {
			t.Fatalf("invalid settings %+v accepted: %v", state, err)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("invalid settings created file: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	corrupt := []byte(`{"version":1,"sourceSize":16,"readingSize":16,"readingTheme":"standard","unexpected":true}`)
	if err := os.WriteFile(path, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); !errors.Is(err, ErrCorruptPreferences) {
		t.Fatalf("unknown setting field=%v", err)
	}
	if err := store.Save(DefaultPreferences()); !errors.Is(err, ErrCorruptPreferences) {
		t.Fatalf("corrupt file overwritten: %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, corrupt) {
		t.Fatalf("corrupt bytes changed: %q, %v", data, err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte{'x'}, 4097), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); !errors.Is(err, ErrCorruptPreferences) {
		t.Fatalf("oversized settings=%v", err)
	}
}

func TestPreferencesStoreRejectsSymlinkAndLooseDirectory(t *testing.T) {
	root := t.TempDir()
	private := filepath.Join(root, "private")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(private, "settings.json")
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	store := PreferencesStore{Path: path}
	if _, err := store.Load(); !errors.Is(err, ErrUnsafePreferencesPath) {
		t.Fatalf("symlink load=%v", err)
	}
	if err := store.Save(DefaultPreferences()); !errors.Is(err, ErrUnsafePreferencesPath) {
		t.Fatalf("symlink save=%v", err)
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "private" {
		t.Fatalf("outside file changed: %q, %v", data, err)
	}
	loose := filepath.Join(root, "loose")
	if err := os.Mkdir(loose, 0755); err != nil {
		t.Fatal(err)
	}
	if err := (PreferencesStore{Path: filepath.Join(loose, "settings.json")}).Save(DefaultPreferences()); !errors.Is(err, ErrUnsafePreferencesPath) {
		t.Fatalf("loose directory accepted=%v", err)
	}
}

func TestPreferencesStoreRefusesWidePermissionExistingFile(t *testing.T) {
	root := t.TempDir()
	private := filepath.Join(root, "private")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(private, "settings.json")
	data := []byte(`{"version":1,"dark":false,"sourceSize":16,"readingSize":16,"readingTheme":"standard"}`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	store := PreferencesStore{Path: path}
	if _, err := store.Load(); !errors.Is(err, ErrUnsafePreferencesPath) {
		t.Fatalf("wide-permission file loaded: %v", err)
	}
	if err := store.Save(DefaultPreferences()); !errors.Is(err, ErrUnsafePreferencesPath) {
		t.Fatalf("wide-permission file overwritten: %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("wide-permission bytes changed: %q, %v", got, err)
	}
}

func TestPreferencesStoreRejectsParentSwappedBeforeOpen(t *testing.T) {
	parent := t.TempDir()
	private := filepath.Join(parent, "private")
	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	store := PreferencesStore{Path: filepath.Join(private, "settings.json")}
	err := store.save(DefaultPreferences(), func() {
		if err := os.Rename(private, filepath.Join(parent, "old-private")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, private); err != nil {
			t.Fatal(err)
		}
	})
	if !errors.Is(err, ErrUnsafePreferencesPath) {
		t.Fatalf("swapped parent=%v", err)
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("outside directory changed: %v, %v", entries, err)
	}
}
