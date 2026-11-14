package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"

	"markstack/internal/session"
)

func TestApplicationRecoveryJournalRestoresDirtyDraftAndClearsAfterSave(t *testing.T) {
	store := session.RecoveryStore{Path: filepath.Join(t.TempDir(), "MarkStack", "recovery.json")}
	state := newModel()
	if err := state.enableRecovery(store); err != nil {
		t.Fatal(err)
	}
	var canvas gfx.Canvas
	driver, err := app.NewDriver(newConfig(state), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	if err := driver.Type("未保存草稿"); err != nil {
		t.Fatal(err)
	}
	if err := state.flushRecovery(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Load()
	if err != nil || len(snapshot.Drafts) != 1 || snapshot.Drafts[0].Body != "未保存草稿" {
		t.Fatalf("dirty journal=%+v err=%v", snapshot, err)
	}
	if err := state.stopRecovery(); err != nil {
		t.Fatal(err)
	}
	driver.Close()
	restarted := newModel()
	if err := restarted.enableRecovery(store); err != nil {
		t.Fatal(err)
	}
	defer restarted.stopRecovery()
	if restarted.session.Current().Text() != "未保存草稿" || !restarted.session.Current().Dirty() {
		t.Fatalf("restarted draft=%q dirty=%v", restarted.session.Current().Text(), restarted.session.Current().Dirty())
	}
	var secondCanvas gfx.Canvas
	secondDriver, err := app.NewDriver(newConfig(restarted), &secondCanvas)
	if err != nil {
		t.Fatal(err)
	}
	defer secondDriver.Close()
	if !hasText(secondDriver, "• 未命名-1.md") || !hasText(secondDriver, "已恢复未保存草稿") {
		t.Fatal("restored draft was not visibly marked unsaved")
	}
	path := filepath.Join(t.TempDir(), "saved.md")
	if err := secondDriver.QueueSaveFile(path, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := secondDriver.ShortcutChord(primaryModifier(), 's'); err != nil {
		t.Fatal(err)
	}
	if err := restarted.flushRecovery(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(store.Path); !os.IsNotExist(err) {
		t.Fatalf("saved draft retained journal: %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "未保存草稿" {
		t.Fatalf("recovered Save As=%q err=%v", data, err)
	}
}

func TestRecoveryWriterKeepsLatestSnapshotAfterBurst(t *testing.T) {
	var mutex sync.Mutex
	var writes []string
	writer := newRecoveryWriter(func(snapshot session.RecoverySnapshot) error {
		mutex.Lock()
		writes = append(writes, snapshot.Drafts[0].Body)
		mutex.Unlock()
		return nil
	}, nil)
	defer writer.Close()
	for _, body := range []string{"one", "two", "latest"} {
		writer.Schedule(session.RecoverySnapshot{Version: 1, Drafts: []session.RecoveryDraft{{ID: 1, Title: "draft.md", Body: body, Newline: "\n"}}})
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if len(writes) == 0 || writes[len(writes)-1] != "latest" {
		t.Fatalf("journal writes=%q", writes)
	}
}

func TestRecoveryWriterConcurrentErrorDrainNeverBlocksFlush(t *testing.T) {
	writer := newRecoveryWriter(func(session.RecoverySnapshot) error {
		return errors.New("disk unavailable")
	}, nil)
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-writer.errors:
				runtime.Gosched()
			case <-stop:
				return
			}
		}
	}()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for attempt := 0; attempt < 20000; attempt++ {
			select {
			case writer.errors <- errors.New("previous failure"):
			default:
			}
			writer.Schedule(session.RecoverySnapshot{Version: 1, Drafts: []session.RecoveryDraft{{ID: 1, Title: "note.md", Body: "draft", Newline: "\n"}}})
			_ = writer.Flush()
		}
	}()
	select {
	case <-finished:
		close(stop)
		if err := writer.Close(); err == nil {
			t.Fatal("failing writer unexpectedly succeeded")
		}
	case <-time.After(10 * time.Second):
		close(stop)
		t.Fatal("concurrent error consumption blocked the recovery writer")
	}
}

func TestRecoveryWriterNeverFinishesWithOlderBlockedWrite(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var mutex sync.Mutex
	var writes []string
	writer := newRecoveryWriter(func(snapshot session.RecoverySnapshot) error {
		body := snapshot.Drafts[0].Body
		if body == "first" {
			close(started)
			<-release
		}
		mutex.Lock()
		writes = append(writes, body)
		mutex.Unlock()
		return nil
	}, nil)
	defer writer.Close()
	snapshot := func(body string) session.RecoverySnapshot {
		return session.RecoverySnapshot{Version: 1, Drafts: []session.RecoveryDraft{{ID: 1, Title: "draft.md", Body: body, Newline: "\n"}}}
	}
	writer.Schedule(snapshot("first"))
	firstFlush := make(chan error, 1)
	go func() { firstFlush <- writer.Flush() }()
	<-started
	writer.Schedule(snapshot("middle"))
	writer.Schedule(snapshot("latest"))
	close(release)
	if err := <-firstFlush; err != nil {
		t.Fatal(err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if len(writes) < 2 || writes[len(writes)-1] != "latest" {
		t.Fatalf("out-of-order journal writes=%q", writes)
	}
}

func TestRecoveryWriterRetriesTheLatestSnapshotAfterWriteFailure(t *testing.T) {
	attempts := 0
	var saved string
	writer := newRecoveryWriter(func(snapshot session.RecoverySnapshot) error {
		attempts++
		if attempts == 1 {
			return errors.New("disk unavailable")
		}
		saved = snapshot.Drafts[0].Body
		return nil
	}, nil)
	defer writer.Close()
	writer.Schedule(session.RecoverySnapshot{Version: 1, Drafts: []session.RecoveryDraft{{ID: 1, Title: "draft.md", Body: "latest", Newline: "\n"}}})
	if err := writer.Flush(); err == nil {
		t.Fatal("failed journal write reported success")
	}
	if err := writer.Flush(); err != nil || attempts != 2 || saved != "latest" {
		t.Fatalf("retry attempts=%d saved=%q err=%v", attempts, saved, err)
	}
}

func TestApplicationRecoveryReportsJournalWriteFailure(t *testing.T) {
	state := newModel()
	state.recoveryLast = state.session.RecoverySnapshot()
	state.recoveryWriter = newRecoveryWriter(func(session.RecoverySnapshot) error { return errors.New("disk unavailable") }, &state.invalidator)
	defer state.stopRecovery()
	state.session.Current().Replace("dirty draft")
	if err := state.flushRecovery(); err == nil {
		t.Fatal("journal failure was not reported")
	}
	state.drainRecoveryErrors()
	if !strings.Contains(state.message, "恢复快照失败") || !strings.Contains(state.message, "disk unavailable") {
		t.Fatalf("journal failure status=%q", state.message)
	}
}

func TestApplicationRecoveryCapturesRenderedEditWithoutSavingDisk(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("# Title\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store := session.RecoveryStore{Path: filepath.Join(root, "MarkStack", "recovery.json")}
	state := newModel()
	if err := state.enableRecovery(store); err != nil {
		t.Fatal(err)
	}
	defer state.stopRecovery()
	state.openFile(path)
	var canvas gfx.Canvas
	driver, err := app.NewDriver(newConfig(state), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	clickID(t, driver, renderedModeID)
	clickID(t, driver, renderedRows.At(0))
	if err := driver.Type("新"); err != nil {
		t.Fatal(err)
	}
	if err := state.flushRecovery(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Load()
	if err != nil || len(snapshot.Drafts) != 1 || snapshot.Drafts[0].Body != "# Title新\n" || !snapshot.Drafts[0].DiskExists {
		t.Fatalf("rendered recovery snapshot=%+v err=%v", snapshot, err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "# Title\n" {
		t.Fatalf("rendered recovery saved without consent: %q err=%v", data, err)
	}
}

func TestApplicationQuitDiscardClearsJournalButCancelKeepsIt(t *testing.T) {
	store := session.RecoveryStore{Path: filepath.Join(t.TempDir(), "MarkStack", "recovery.json")}
	state := newModel()
	if err := state.enableRecovery(store); err != nil {
		t.Fatal(err)
	}
	state.session.Current().Replace("unsaved work")
	if err := state.flushRecovery(); err != nil {
		t.Fatal(err)
	}
	allowed := false
	state.requestClose(func() { allowed = true })
	state.resolve(session.Cancel)
	if allowed || state.session.Current().Text() != "unsaved work" {
		t.Fatal("cancel lost the unsaved draft")
	}
	if err := state.flushRecovery(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.Path); err != nil {
		t.Fatalf("cancel removed recovery journal: %v", err)
	}
	state.requestClose(func() { allowed = true })
	state.resolve(session.Discard)
	if !allowed {
		t.Fatal("discard did not finish Quit")
	}
	if err := state.stopRecovery(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(store.Path); !os.IsNotExist(err) {
		t.Fatalf("explicit discard retained recovery journal: %v", err)
	}
}

func TestApplicationRecoveryKeepsDraftAfterFailedSaveAndCanceledQuit(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	store := session.RecoveryStore{Path: filepath.Join(root, "MarkStack", "recovery.json")}
	state := newModel()
	if err := state.enableRecovery(store); err != nil {
		t.Fatal(err)
	}
	defer state.stopRecovery()
	var canvas gfx.Canvas
	driver, err := app.NewDriver(newConfig(state), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	state.openFile(path)
	state.session.Current().Replace("local draft")
	if err := driver.Resize(1201, 780); err != nil {
		t.Fatal(err)
	}
	if err := state.flushRecovery(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	state.requestClose(func() { t.Fatal("failed Save allowed Quit") })
	state.resolve(session.Save)
	if state.session.Pending() == nil || state.session.Current().Text() != "local draft" {
		t.Fatal("failed Save lost pending dirty draft")
	}
	state.resolve(session.Cancel)
	if err := state.flushRecovery(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Load()
	if err != nil || len(snapshot.Drafts) != 1 || snapshot.Drafts[0].Body != "local draft" {
		t.Fatalf("failed save journal=%+v err=%v", snapshot, err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "external" {
		t.Fatalf("external disk changed=%q err=%v", data, err)
	}
}

func TestApplicationRecoveryCleanStartupCreatesNoJournal(t *testing.T) {
	store := session.RecoveryStore{Path: filepath.Join(t.TempDir(), "MarkStack", "recovery.json")}
	state := newModel()
	if err := state.enableRecovery(store); err != nil {
		t.Fatal(err)
	}
	var canvas gfx.Canvas
	driver, err := app.NewDriver(newConfig(state), &canvas)
	if err != nil {
		t.Fatal(err)
	}
	driver.Close()
	if err := state.stopRecovery(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(store.Path); !os.IsNotExist(err) {
		t.Fatalf("clean app created recovery journal: %v", err)
	}
}

func TestHeadlessRecoveryRequiresAnExplicitIsolatedPath(t *testing.T) {
	t.Setenv("VIVA_HEADLESS", "1")
	t.Setenv("MARKSTACK_RECOVERY_PATH", "")
	if recoveryEnabled() {
		t.Fatal("headless acceptance would touch the user's recovery journal")
	}
	t.Setenv("MARKSTACK_RECOVERY_PATH", filepath.Join(t.TempDir(), "recovery.json"))
	if !recoveryEnabled() {
		t.Fatal("explicit headless recovery fixture was disabled")
	}
}

func TestApplicationUnsafeRecoveryJournalKeepsOriginalAndIsNotOverwritten(t *testing.T) {
	root := t.TempDir()
	store := session.RecoveryStore{Path: filepath.Join(root, "MarkStack", "recovery.json")}
	writer := newModel()
	writer.session.Current().Replace("recovered draft")
	if err := store.Save(writer.session.RecoverySnapshot()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store.Path, 0644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	state := newModel()
	state.session.Current().Replace("current draft")
	if err := state.enableRecovery(store); !errors.Is(err, session.ErrUnsafeRecoveryPath) {
		t.Fatalf("unsafe journal startup error=%v", err)
	}
	if state.session.Current().Text() != "current draft" || len(state.session.Tabs) != 1 || state.recoveryWriter != nil {
		t.Fatalf("unsafe journal changed the session: tabs=%d text=%q", len(state.session.Tabs), state.session.Current().Text())
	}
	if after, err := os.ReadFile(store.Path); err != nil || !bytes.Equal(after, before) {
		t.Fatalf("unsafe journal was overwritten: %q, %v", after, err)
	}
	if err := state.stopRecovery(); err != nil {
		t.Fatal(err)
	}
}
