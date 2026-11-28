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
	if err != nil || defaultState != (Preferences{Version: 1, SourceSize: 16, ReadingSize: 16, LineSpacing: "standard", EditorWidth: "standard", ReadingTheme: "standard", Language: "system", UITextSize: "medium", PDFPreset: "standard"}) {
		t.Fatalf("missing preferences=%+v, %v", defaultState, err)
	}
	next := Preferences{Version: 1, Dark: true, SourceSize: 20, ReadingSize: 18, LineSpacing: "relaxed", EditorWidth: "wide", ReadingTheme: "soft", Language: "en", UITextSize: "large", ShowTabNumbers: true, AutoCheckUpdates: true, PDFHeader: true, PDFFooter: true, PDFPreset: "compact"}
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

func TestPreferencesReadingThemesRoundTrip(t *testing.T) {
	for _, theme := range []string{"standard", "soft", "github", "parchment", "academic", "nord"} {
		t.Run(theme, func(t *testing.T) {
			store := PreferencesStore{Path: filepath.Join(t.TempDir(), "private", "settings.json")}
			want := DefaultPreferences()
			want.ReadingTheme = theme
			for _, dark := range []bool{false, true} {
				want.Dark = dark
				if err := store.Save(want); err != nil {
					t.Fatal(err)
				}
				if got, err := store.Load(); err != nil || got != want {
					t.Fatalf("settings=%+v, %v; want %+v", got, err, want)
				}
			}
		})
	}
}

func TestPreferencesFormattingToolbarDefaultsVisibleAndPersistsOptOut(t *testing.T) {
	store := PreferencesStore{Path: filepath.Join(t.TempDir(), "private", "settings.json")}
	defaults, err := store.Load()
	if err != nil || defaults.HideFormatBar {
		t.Fatalf("default toolbar preference: %+v, %v", defaults, err)
	}
	if err := store.Save(defaults); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(store.Path); err != nil || bytes.Contains(saved, []byte("hideFormatBar")) {
		t.Fatalf("default preference should be omitted: %s, %v", saved, err)
	}
	legacy, err := store.Load()
	if err != nil || legacy.HideFormatBar {
		t.Fatalf("settings without the field should show the toolbar: %+v, %v", legacy, err)
	}
	legacy.HideFormatBar = true
	if err := store.Save(legacy); err != nil {
		t.Fatal(err)
	}
	if hidden, err := store.Load(); err != nil || !hidden.HideFormatBar {
		t.Fatalf("opt-out was not saved: %+v, %v", hidden, err)
	}
}

func TestPreferencesCodeEditorDefaultsOnAndPersistsOptOut(t *testing.T) {
	store := PreferencesStore{Path: filepath.Join(t.TempDir(), "private", "settings.json")}
	defaults, err := store.Load()
	if err != nil || defaults.HideCodeLineNumbers || defaults.DisableCodeWrap {
		t.Fatalf("new code editor preferences: %+v, %v", defaults, err)
	}
	if err := store.Save(defaults); err != nil {
		t.Fatal(err)
	}
	if saved, err := os.ReadFile(store.Path); err != nil || bytes.Contains(saved, []byte("hideCodeLineNumbers")) || bytes.Contains(saved, []byte("disableCodeWrap")) {
		t.Fatalf("default preference fields should be omitted: %s, %v", saved, err)
	}
	legacy, err := store.Load()
	if err != nil || legacy.HideCodeLineNumbers || legacy.DisableCodeWrap {
		t.Fatalf("legacy preferences disabled line numbers or wrap: %+v, %v", legacy, err)
	}
	legacy.HideCodeLineNumbers, legacy.DisableCodeWrap = true, true
	if err := store.Save(legacy); err != nil {
		t.Fatal(err)
	}
	if disabled, err := store.Load(); err != nil || !disabled.HideCodeLineNumbers || !disabled.DisableCodeWrap {
		t.Fatalf("code editor opt-outs were not saved: %+v, %v", disabled, err)
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
	if err != nil || state.AutoSave || state.AutoCheckUpdates || state.ShowTabNumbers || state.Language != "system" || state.UITextSize != "medium" {
		t.Fatalf("legacy settings unexpectedly auto-save: %+v, %v", state, err)
	}
	if !state.Valid() || state.PDFHeader || state.PDFFooter || state.PDFPreset != "standard" || state.LineSpacing != "standard" || state.EditorWidth != "standard" {
		t.Fatalf("legacy PDF preferences did not default: %+v", state)
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

func TestPreferencesFontRangeAndLayoutOptions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "settings.json")
	store := PreferencesStore{Path: path}
	state := DefaultPreferences()
	for size := 12; size <= 32; size++ {
		state.SourceSize, state.ReadingSize = size, 44-size
		if !state.Valid() {
			t.Fatalf("font size %d rejected: %+v", size, state)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		fields         string
		spacing, width string
	}{{`"lineSpacing":"","editorWidth":""`, "standard", "standard"}, {`"lineSpacing":"compact","editorWidth":"full"`, "compact", "full"}, {`"lineSpacing":"double"`, "", ""}, {`"editorWidth":"narrow"`, "", ""}} {
		data := `{"version":1,"sourceSize":13,"readingSize":31,"readingTheme":"standard",` + scenario.fields + `}`
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := store.Load()
		if scenario.spacing == "" {
			if !errors.Is(err, ErrCorruptPreferences) {
				t.Fatalf("invalid settings %s: %v", data, err)
			}
		} else if err != nil || got.LineSpacing != scenario.spacing || got.EditorWidth != scenario.width || got.SourceSize != 13 || got.ReadingSize != 31 {
			t.Fatalf("settings %s loaded as %+v, %v", data, got, err)
		}
	}
}

func TestPreferencesPDFPresetValidation(t *testing.T) {
	store := PreferencesStore{Path: filepath.Join(t.TempDir(), "private", "settings.json")}
	state := DefaultPreferences()
	for _, preset := range []string{"standard", "compact", "academic", "", "unknown"} {
		state.PDFPreset = preset
		err := store.Save(state)
		if preset == "" || preset == "unknown" {
			if state.Valid() || !errors.Is(err, ErrCorruptPreferences) {
				t.Fatalf("accepted invalid PDF preset %q: %v", preset, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("saving preset %q: %v", preset, err)
		}
		if got, err := store.Load(); err != nil || got.PDFPreset != preset {
			t.Fatalf("loading preset %q: %+v, %v", preset, got, err)
		}
	}
}

func TestPreferencesStoreRejectsCorruptValuesAndExistingBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "settings.json")
	store := PreferencesStore{Path: path}
	for _, state := range []Preferences{
		{Version: 2, SourceSize: 16, ReadingSize: 16, ReadingTheme: "standard"},
		{Version: 1, SourceSize: 11, ReadingSize: 16, ReadingTheme: "standard", Language: "system", UITextSize: "medium", PDFPreset: "standard"},
		{Version: 1, SourceSize: 16, ReadingSize: 33, ReadingTheme: "standard", Language: "system", UITextSize: "medium", PDFPreset: "standard"},
		{Version: 1, SourceSize: 16, ReadingSize: 16, ReadingTheme: "unknown"},
		{Version: 1, SourceSize: 16, ReadingSize: 16, ReadingTheme: "standard", Language: "unknown"},
		{Version: 1, SourceSize: 16, ReadingSize: 16, ReadingTheme: "standard", UITextSize: "huge"},
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
