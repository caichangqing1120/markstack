package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"markstack/internal/session"
)

func TestApplicationSessionManifestRestoresCleanTabsAndCaret(t *testing.T) {
	root := t.TempDir()
	first, second := filepath.Join(root, "first.md"), filepath.Join(root, "second.txt")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("first second"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	store := session.SessionStore{Path: filepath.Join(t.TempDir(), "private", "session.json")}
	state := newModel()
	state.session.Root = root
	if err := state.enableSessionManifest(store, true); err != nil {
		t.Fatal(err)
	}
	var canvas gfx.Canvas
	driver, err := app.NewDriver(newConfig(state), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	if !state.openFile(first) || !state.openFile(second) {
		t.Fatal("task files did not open")
	}
	state.session.Tabs[1].Editor.MoveTo(2, false)
	state.session.Tabs[2].Editor.MoveTo(3, false)
	state.session.Active = 1
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	if err := state.flushSessionManifest(); err != nil {
		t.Fatal(err)
	}
	driver.Close()
	if err := state.stopSessionManifest(); err != nil {
		t.Fatal(err)
	}
	saved, err := store.Load()
	if err != nil || len(saved.Tabs) != 2 || saved.ActiveID != saved.Tabs[0].ID || saved.Tabs[0].Caret != 2 || saved.Tabs[1].Caret != 3 {
		t.Fatalf("saved manifest=%+v err=%v", saved, err)
	}
	restarted := newModel()
	if err := restarted.enableSessionManifest(store, true); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.stopSessionManifest() })
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(restarted.session.Tabs) != 2 || restarted.session.Current().Path != first || restarted.session.Current().Editor.Caret() != 2 || restarted.session.Tabs[1].Path != second || restarted.session.Tabs[1].Editor.Caret() != 3 || restarted.session.Root != canonicalRoot {
		t.Fatalf("restarted tabs=%+v active=%d", restarted.session.Tabs, restarted.session.Active)
	}
	for _, path := range []string{first, second} {
		if data, err := os.ReadFile(path); err != nil || string(data) != "first second" {
			t.Fatalf("restore rewrote %s: %q, %v", path, data, err)
		}
	}
}

func TestApplicationSessionManifestMergesRecoveryBeforeCleanTabs(t *testing.T) {
	root := t.TempDir()
	clean, dirty := filepath.Join(root, "clean.md"), filepath.Join(root, "dirty.md")
	for _, path := range []string{clean, dirty} {
		if err := os.WriteFile(path, []byte("disk"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	opened := session.New()
	opened.Root = root
	if _, err := opened.Open(clean); err != nil {
		t.Fatal(err)
	}
	dirtyTab, err := opened.Open(dirty)
	if err != nil {
		t.Fatal(err)
	}
	dirtyTab.Replace("draft")
	manifestStore := session.SessionStore{Path: filepath.Join(t.TempDir(), "private", "session.json")}
	recoveryStore := session.RecoveryStore{Path: filepath.Join(t.TempDir(), "private", "recovery.json")}
	if err := manifestStore.Save(opened.Manifest()); err != nil {
		t.Fatal(err)
	}
	if err := recoveryStore.Save(opened.RecoverySnapshot()); err != nil {
		t.Fatal(err)
	}
	state := newModel()
	if err := state.enableRecovery(recoveryStore); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.stopRecovery() })
	if err := state.enableSessionManifest(manifestStore, true); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.stopSessionManifest() })
	if len(state.session.Tabs) != 2 || state.session.Tabs[0].Path != clean || state.session.Tabs[1].Path != dirty || state.session.Tabs[1].Text() != "draft" || !state.session.Tabs[1].Dirty() {
		t.Fatalf("dirty precedence tabs=%+v", state.session.Tabs)
	}
	if data, err := os.ReadFile(dirty); err != nil || string(data) != "disk" {
		t.Fatalf("dirty disk changed: %q, %v", data, err)
	}
	state.quitDiscarded = true
	if err := state.flushSessionManifest(); err != nil {
		t.Fatal(err)
	}
	if saved, err := manifestStore.Load(); err != nil || len(saved.Tabs) != 1 || saved.Tabs[0].Path != clean {
		t.Fatalf("discard kept dirty placeholder: %+v, %v", saved, err)
	}
}

func TestApplicationSessionManifestSkipsRestoreForExplicitWorkspace(t *testing.T) {
	root := t.TempDir()
	previous, selected := filepath.Join(root, "previous"), filepath.Join(root, "selected")
	for _, path := range []string{previous, selected} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	store := session.SessionStore{Path: filepath.Join(t.TempDir(), "private", "session.json")}
	if err := store.Save(session.SessionManifest{Version: 1, Root: previous}); err != nil {
		t.Fatal(err)
	}
	state := newModel()
	if changed, err := state.session.RequestWorkspace(selected); err != nil || !changed {
		t.Fatalf("explicit workspace=%v, %v", changed, err)
	}
	selected, err := filepath.EvalSymlinks(selected)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.enableSessionManifest(store, false); err != nil {
		t.Fatal(err)
	}
	if err := state.flushSessionManifest(); err != nil {
		t.Fatal(err)
	}
	if err := state.stopSessionManifest(); err != nil {
		t.Fatal(err)
	}
	if state.session.Root != selected {
		t.Fatalf("old workspace replaced explicit path: %q", state.session.Root)
	}
	if saved, err := store.Load(); err != nil || saved.Root != selected {
		t.Fatalf("explicit workspace not persisted: %+v, %v", saved, err)
	}
}

func TestApplicationSessionManifestRejectsCorruptStoreWithoutOverwriting(t *testing.T) {
	store := session.SessionStore{Path: filepath.Join(t.TempDir(), "private", "session.json")}
	if err := os.MkdirAll(filepath.Dir(store.Path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	state := newModel()
	before := state.session.Current()
	if err := state.enableSessionManifest(store, true); !errors.Is(err, session.ErrCorruptManifest) || state.session.Current() != before {
		t.Fatalf("corrupt startup changed session: %v", err)
	}
	if data, err := os.ReadFile(store.Path); err != nil || string(data) != "{broken" {
		t.Fatalf("corrupt manifest was overwritten: %q, %v", data, err)
	}
}

func TestApplicationSessionManifestNoOpCreatesNoFile(t *testing.T) {
	store := session.SessionStore{Path: filepath.Join(t.TempDir(), "private", "session.json")}
	state := newModel()
	if err := state.enableSessionManifest(store, true); err != nil {
		t.Fatal(err)
	}
	var canvas gfx.Canvas
	driver, err := app.NewDriver(newConfig(state), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	driver.Close()
	if err := state.stopSessionManifest(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(store.Path); !os.IsNotExist(err) {
		t.Fatalf("empty default session created a manifest: %v", err)
	}
}

func TestApplicationCorruptManifestDoesNotHideRecoveredDraft(t *testing.T) {
	root := t.TempDir()
	recovery := session.RecoveryStore{Path: filepath.Join(root, "private", "recovery.json")}
	manifest := session.SessionStore{Path: filepath.Join(root, "private", "session.json")}
	opened := session.New()
	opened.Current().Replace("recover me")
	if err := recovery.Save(opened.RecoverySnapshot()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest.Path, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	state := newModel()
	if err := state.enableRecovery(recovery); err != nil {
		t.Fatal(err)
	}
	defer state.stopRecovery()
	if err := state.enableSessionManifest(manifest, true); !errors.Is(err, session.ErrCorruptManifest) || state.session.Current().Text() != "recover me" || !state.session.Current().Dirty() {
		t.Fatalf("corrupt manifest suppressed draft: %q, %v", state.session.Current().Text(), err)
	}
	if data, err := os.ReadFile(manifest.Path); err != nil || string(data) != "{broken" {
		t.Fatalf("corrupt manifest was overwritten: %q, %v", data, err)
	}
}

func TestSessionWriterKeepsLatestManifestAfterBurst(t *testing.T) {
	var mutex sync.Mutex
	var roots []string
	writer := newSessionWriter(func(manifest session.SessionManifest) error {
		mutex.Lock()
		roots = append(roots, manifest.Root)
		mutex.Unlock()
		return nil
	}, nil)
	defer writer.Close()
	for _, root := range []string{"/first", "/middle", "/latest"} {
		writer.Schedule(session.SessionManifest{Version: 1, Root: root})
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if !reflect.DeepEqual(roots, []string{"/latest"}) {
		t.Fatalf("session writer saved stale manifests: %q", roots)
	}
}
