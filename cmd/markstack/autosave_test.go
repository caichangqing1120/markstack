package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/text"

	"markstack/internal/session"
)

func autoSaveTestModel(t *testing.T) (*model, *time.Time) {
	t.Helper()
	state := newModel()
	state.preferences.AutoSave = true
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	state.autoSave.clock = func() time.Time { return now }
	state.autoSave.delay = time.Second
	t.Cleanup(state.stopAutoSave)
	return state, &now
}

func advanceAutoSave(state *model, now *time.Time) {
	*now = now.Add(2 * time.Second)
	state.drainAutoSave()
}

func TestAutoSaveDebouncesNamedBOMCRLFMarkdown(t *testing.T) {
	state, now := autoSaveTestModel(t)
	path := filepath.Join(t.TempDir(), "note.md")
	original := append([]byte{0xef, 0xbb, 0xbf}, []byte("# 标题\r\n正文\r\n")...)
	if err := os.WriteFile(path, original, 0640); err != nil {
		t.Fatal(err)
	}
	document, err := state.session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(document.Text(), "正文")
	if err := document.EditRange(start, start+len("正文"), "新文"); err != nil {
		t.Fatal(err)
	}
	state.scheduleAutoSave()
	state.drainAutoSave()
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, original) {
		t.Fatalf("auto-save wrote before debounce: %q, %v", data, err)
	}
	advanceAutoSave(state, now)
	want := append([]byte{0xef, 0xbb, 0xbf}, []byte("# 标题\r\n新文\r\n")...)
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, want) || document.Dirty() {
		t.Fatalf("auto-save lost BOM/CRLF or dirty state: %q, %v dirty=%v", data, err, document.Dirty())
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0640 {
		t.Fatalf("auto-save changed mode: %v, %v", info, err)
	}
}

func TestAutoSaveNeverNamesDraftOrPersistsPreedit(t *testing.T) {
	state, now := autoSaveTestModel(t)
	unnamed := state.session.Current()
	unnamed.Replace("# 未命名草稿")
	state.scheduleAutoSave()
	advanceAutoSave(state, now)
	if unnamed.Path != "" || !unnamed.Dirty() {
		t.Fatalf("unnamed draft auto-saved: path=%q dirty=%v", unnamed.Path, unnamed.Dirty())
	}
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("# 原文\n"), 0644); err != nil {
		t.Fatal(err)
	}
	document, err := state.session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	document.Editor.SetMarkedText("pin", text.Range{Start: 3, End: 3})
	state.scheduleAutoSave()
	advanceAutoSave(state, now)
	if data, err := os.ReadFile(path); err != nil || string(data) != "# 原文\n" || document.Dirty() {
		t.Fatalf("preedit reached disk: %q, %v dirty=%v", data, err, document.Dirty())
	}
	document.Editor.Insert("新")
	state.scheduleAutoSave()
	advanceAutoSave(state, now)
	if data, err := os.ReadFile(path); err != nil || !bytes.Contains(data, []byte("新")) || bytes.Contains(data, []byte("pin")) {
		t.Fatalf("committed IME result=%q, %v", data, err)
	}
}

func TestAutoSaveExternalConflictDoesNotRetryUnchangedDraft(t *testing.T) {
	state, now := autoSaveTestModel(t)
	path := filepath.Join(t.TempDir(), "note.mh")
	if err := os.WriteFile(path, []byte("<p>原文</p>"), 0644); err != nil {
		t.Fatal(err)
	}
	document, err := state.session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	document.Replace("<p>草稿</p>")
	state.scheduleAutoSave()
	if err := os.WriteFile(path, []byte("<p>外部</p>"), 0644); err != nil {
		t.Fatal(err)
	}
	advanceAutoSave(state, now)
	if data, err := os.ReadFile(path); err != nil || string(data) != "<p>外部</p>" || !document.Dirty() || state.message == "" {
		t.Fatalf("conflict overwrote source or draft: %q, %v message=%q", data, err, state.message)
	}
	if err := os.WriteFile(path, []byte("<p>原文</p>"), 0644); err != nil {
		t.Fatal(err)
	}
	state.scheduleAutoSave()
	advanceAutoSave(state, now)
	if data, err := os.ReadFile(path); err != nil || string(data) != "<p>原文</p>" || !document.Dirty() {
		t.Fatalf("unchanged failed draft auto-retried: %q, %v", data, err)
	}
}

func TestAutoSaveTracksDocumentAcrossTabsAndHonorsPendingConfirmation(t *testing.T) {
	state, now := autoSaveTestModel(t)
	root := t.TempDir()
	firstPath, secondPath := filepath.Join(root, "first.go"), filepath.Join(root, "second.txt")
	for _, path := range []string{firstPath, secondPath} {
		if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	first, err := state.session.Open(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	first.Replace("first new")
	state.scheduleAutoSave()
	second, err := state.session.Open(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	second.Replace("second new")
	state.scheduleAutoSave()
	if state.session.RequestClose(state.session.Active) {
		t.Fatal("dirty tab closed without confirmation")
	}
	advanceAutoSave(state, now)
	if firstData, _ := os.ReadFile(firstPath); string(firstData) != "old" {
		t.Fatalf("pending confirmation saved another tab: %q", firstData)
	}
	if secondData, _ := os.ReadFile(secondPath); string(secondData) != "old" {
		t.Fatalf("pending confirmation saved active tab: %q", secondData)
	}
	if _, err := state.session.Resolve(session.Cancel, ""); err != nil {
		t.Fatal(err)
	}
	state.scheduleAutoSave()
	advanceAutoSave(state, now)
	if firstData, _ := os.ReadFile(firstPath); string(firstData) != "first new" {
		t.Fatalf("first tab not auto-saved: %q", firstData)
	}
	if secondData, _ := os.ReadFile(secondPath); string(secondData) != "second new" {
		t.Fatalf("second tab not auto-saved: %q", secondData)
	}
}

func TestAutoSaveDisableAndUndoToCleanCancelPendingWrites(t *testing.T) {
	state, now := autoSaveTestModel(t)
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	document, err := state.session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	document.Replace("changed")
	state.scheduleAutoSave()
	document.Replace("original")
	state.scheduleAutoSave()
	advanceAutoSave(state, now)
	if data, _ := os.ReadFile(path); string(data) != "original" || document.Dirty() {
		t.Fatalf("undo-to-clean still auto-saved: %q dirty=%v", data, document.Dirty())
	}
	document.Replace("changed again")
	state.scheduleAutoSave()
	state.preferences.AutoSave = false
	state.scheduleAutoSave()
	advanceAutoSave(state, now)
	if data, _ := os.ReadFile(path); string(data) != "original" || !document.Dirty() {
		t.Fatalf("disabled auto-save wrote disk: %q dirty=%v", data, document.Dirty())
	}
}

func TestAutoSavePersistsEmptyNamedDocument(t *testing.T) {
	state, now := autoSaveTestModel(t)
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("remove me"), 0644); err != nil {
		t.Fatal(err)
	}
	document, err := state.session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	document.Replace("")
	state.scheduleAutoSave()
	advanceAutoSave(state, now)
	if data, err := os.ReadFile(path); err != nil || len(data) != 0 || document.Dirty() {
		t.Fatalf("empty document not auto-saved: %q, %v dirty=%v", data, err, document.Dirty())
	}
}

func TestAutoSaveRenamedDocumentStartsNewDebounce(t *testing.T) {
	state, now := autoSaveTestModel(t)
	root := t.TempDir()
	oldPath := filepath.Join(root, "old.md")
	newPath := filepath.Join(root, "new.md")
	if err := os.WriteFile(oldPath, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	if changed, err := state.session.RequestWorkspace(root); err != nil || !changed {
		t.Fatalf("workspace: %v, %v", changed, err)
	}
	document, err := state.session.Open(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	document.Replace("changed")
	state.scheduleAutoSave()
	*now = now.Add(500 * time.Millisecond)
	if err := state.session.RenameNode(oldPath, "new.md"); err != nil {
		t.Fatal(err)
	}
	state.drainAutoSave()
	*now = now.Add(600 * time.Millisecond)
	state.drainAutoSave()
	if data, err := os.ReadFile(newPath); err != nil || string(data) != "original" {
		t.Fatalf("old timer saved retargeted document: %q, %v", data, err)
	}
	advanceAutoSave(state, now)
	if data, err := os.ReadFile(newPath); err != nil || string(data) != "changed" || document.Dirty() {
		t.Fatalf("renamed document not saved after new debounce: %q, %v", data, err)
	}
}

func TestAutoSaveWaitsForRenderedComposition(t *testing.T) {
	for _, name := range []string{"markdown", "rich-html"} {
		t.Run(name, func(t *testing.T) {
			state, now := autoSaveTestModel(t)
			extension := ".md"
			if name == "rich-html" {
				extension = ".mh"
			}
			path := filepath.Join(t.TempDir(), "note"+extension)
			if err := os.WriteFile(path, []byte("original"), 0644); err != nil {
				t.Fatal(err)
			}
			document, err := state.session.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			document.Replace("committed")
			input := text.NewEditor("visible")
			input.SetMarkedText("preedit", text.Range{Start: 0, End: 0})
			if name == "markdown" {
				state.rich.active, state.rich.documentID, state.rich.input = true, document.ID, input
			} else {
				state.html.active, state.html.documentID, state.html.input = true, document.ID, input
			}
			state.scheduleAutoSave()
			advanceAutoSave(state, now)
			if data, err := os.ReadFile(path); err != nil || string(data) != "original" {
				t.Fatalf("rendered preedit allowed auto-save: %q, %v", data, err)
			}
			input.Insert("done")
			state.scheduleAutoSave()
			advanceAutoSave(state, now)
			if data, err := os.ReadFile(path); err != nil || string(data) != "committed" {
				t.Fatalf("committed rendered edit not auto-saved: %q, %v", data, err)
			}
		})
	}
}

func TestAutoSaveFailedDraftRemainsRecoverable(t *testing.T) {
	state, now := autoSaveTestModel(t)
	path := filepath.Join(t.TempDir(), "note.mh")
	if err := os.WriteFile(path, []byte("<p>old</p>"), 0644); err != nil {
		t.Fatal(err)
	}
	document, err := state.session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	document.Replace("<p>draft</p>")
	state.scheduleAutoSave()
	if err := os.WriteFile(path, []byte("<p>external</p>"), 0644); err != nil {
		t.Fatal(err)
	}
	advanceAutoSave(state, now)
	snapshot := state.session.RecoverySnapshot()
	if len(snapshot.Drafts) != 1 || snapshot.Drafts[0].Path != path || snapshot.Drafts[0].Body != "<p>draft</p>" {
		t.Fatalf("failed auto-save lost recovery draft: %+v", snapshot.Drafts)
	}
}

func TestAutoSaveTimerOnlyWritesOnUIWake(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	state.preferences.AutoSave = true
	state.autoSave.delay = 50 * time.Millisecond
	t.Cleanup(state.stopAutoSave)
	document, err := state.session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	document.Replace("updated")
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	// The paint may request focus, which queues an immediate wake of its own.
	// Drain those unrelated wakes so the wait below can only see the auto-save
	// timer's wake; nothing here pumps, so no wake can write the document.
	for len(driver.Wakes()) > 0 {
		<-driver.Wakes()
	}
	select {
	case <-driver.Wakes():
	case <-time.After(2 * time.Second):
		t.Fatal("auto-save timer did not wake the UI")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "original" {
		t.Fatalf("timer wrote disk outside UI wake: %q, %v", data, err)
	}
	if err := driver.Pump(); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "updated" || document.Dirty() {
		t.Fatalf("UI wake did not auto-save: %q, %v dirty=%v", data, err, document.Dirty())
	}
}

func TestAutoSaveDoesNotWriteUntouchedRecoveredDraft(t *testing.T) {
	for _, extension := range []string{".md", ".mh", ".go", ".txt"} {
		t.Run(extension, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "note"+extension)
			original := []byte("original\n")
			if err := os.WriteFile(path, original, 0644); err != nil {
				t.Fatal(err)
			}
			before := session.New()
			document, err := before.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			document.Replace("recovered draft")
			store := session.RecoveryStore{Path: filepath.Join(root, "private", "recovery.json")}
			if err := store.Save(before.RecoverySnapshot()); err != nil {
				t.Fatal(err)
			}
			state, now := autoSaveTestModel(t)
			if err := state.enableRecovery(store); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = state.stopRecovery() })
			recovered := state.session.Current()
			state.scheduleAutoSave()
			advanceAutoSave(state, now)
			if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, original) || !recovered.Dirty() {
				t.Fatalf("startup auto-wrote recovery draft: %q, %v dirty=%v", data, err, recovered.Dirty())
			}
			state.preferences.AutoSave = false
			state.scheduleAutoSave()
			state.preferences.AutoSave = true
			state.scheduleAutoSave()
			advanceAutoSave(state, now)
			if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, original) || !recovered.Dirty() {
				t.Fatalf("toggle auto-wrote untouched recovery draft: %q, %v dirty=%v", data, err, recovered.Dirty())
			}
			if snapshot, err := store.Load(); err != nil || len(snapshot.Drafts) != 1 || snapshot.Drafts[0].Body != "recovered draft" {
				t.Fatalf("startup lost recovery journal: %+v, %v", snapshot.Drafts, err)
			}
			recovered.Replace("recovered draft edited")
			state.scheduleAutoSave()
			advanceAutoSave(state, now)
			if data, err := os.ReadFile(path); err != nil || string(data) != "recovered draft edited" || recovered.Dirty() {
				t.Fatalf("new edit did not auto-save: %q, %v dirty=%v", data, err, recovered.Dirty())
			}
		})
	}
}

func TestAutoSaveEmptyRecoveryDoesNotSuppressOrdinaryEdit(t *testing.T) {
	state, now := autoSaveTestModel(t)
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	document, err := state.session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	document.Replace("edited")
	store := session.RecoveryStore{Path: filepath.Join(root, "private", "recovery.json")}
	if err := state.enableRecovery(store); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.stopRecovery() })
	state.scheduleAutoSave()
	advanceAutoSave(state, now)
	if data, err := os.ReadFile(path); err != nil || string(data) != "edited" || document.Dirty() {
		t.Fatalf("empty recovery suppressed ordinary edit: %q, %v dirty=%v", data, err, document.Dirty())
	}
}

func TestAutoSaveStaysOffByDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.md")
	if err := os.WriteFile(path, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	state, driver := testDriver(t)
	if state.preferences.AutoSave {
		t.Fatal("auto-save is enabled without an explicit preference")
	}
	document, err := state.session.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := driver.Resize(1200, 780); err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Pump(); err != nil {
		t.Fatal(err)
	}
	state.scheduleAutoSave()
	if data, err := os.ReadFile(path); err != nil || string(data) != "original" || !document.Dirty() || len(state.autoSave.pending) != 0 {
		t.Fatalf("default-off auto-save wrote %q dirty=%v pending=%d", data, document.Dirty(), len(state.autoSave.pending))
	}
}
