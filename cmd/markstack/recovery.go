package main

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/vivaui/viva/app"

	"markstack/internal/session"
)

type snapshotWriter[Snapshot any] struct {
	save     func(Snapshot) error
	wake     *app.Invalidator
	queue    chan Snapshot
	flush    chan chan error
	stop     chan chan error
	errors   chan error
	done     chan struct{}
	mutex    sync.Mutex
	closed   bool
	closeErr error
}

type recoveryWriter = snapshotWriter[session.RecoverySnapshot]

func newRecoveryWriter(save func(session.RecoverySnapshot) error, wake *app.Invalidator) *recoveryWriter {
	return newSnapshotWriter(save, wake)
}

func newSnapshotWriter[Snapshot any](save func(Snapshot) error, wake *app.Invalidator) *snapshotWriter[Snapshot] {
	writer := &snapshotWriter[Snapshot]{
		save: save, wake: wake,
		queue: make(chan Snapshot, 1), flush: make(chan chan error), stop: make(chan chan error),
		errors: make(chan error, 1), done: make(chan struct{}),
	}
	go writer.run()
	return writer
}

func (writer *snapshotWriter[Snapshot]) Schedule(snapshot Snapshot) {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	if writer.closed {
		return
	}
	select {
	case writer.queue <- snapshot:
	default:
		select {
		case <-writer.queue:
		default:
		}
		writer.queue <- snapshot
	}
}

func (writer *snapshotWriter[Snapshot]) Flush() error {
	writer.mutex.Lock()
	if writer.closed {
		writer.mutex.Unlock()
		<-writer.done
		return writer.closeErr
	}
	acknowledge := make(chan error, 1)
	writer.flush <- acknowledge
	writer.mutex.Unlock()
	return <-acknowledge
}

func (writer *snapshotWriter[Snapshot]) Close() error {
	writer.mutex.Lock()
	if writer.closed {
		writer.mutex.Unlock()
		<-writer.done
		return writer.closeErr
	}
	writer.closed = true
	acknowledge := make(chan error, 1)
	writer.mutex.Unlock()
	writer.stop <- acknowledge
	err := <-acknowledge
	writer.mutex.Lock()
	writer.closeErr = err
	close(writer.done)
	writer.mutex.Unlock()
	return err
}

func (writer *snapshotWriter[Snapshot]) run() {
	var latest Snapshot
	var pending bool
	var timer *time.Timer
	var tick <-chan time.Time
	drain := func() {
		for {
			select {
			case latest = <-writer.queue:
				pending = true
			default:
				return
			}
		}
	}
	write := func() error {
		drain()
		if !pending {
			return nil
		}
		err := writer.save(latest)
		if err == nil {
			pending = false
		}
		select {
		case writer.errors <- err:
		default:
			select {
			case <-writer.errors:
			default:
			}
			writer.errors <- err
		}
		if writer.wake != nil {
			writer.wake.Invalidate()
		}
		return err
	}
	stopTimer := func() {
		if timer != nil {
			timer.Stop()
			tick = nil
		}
	}
	for {
		select {
		case latest = <-writer.queue:
			pending = true
			if timer == nil {
				timer = time.NewTimer(250 * time.Millisecond)
			} else {
				timer.Reset(250 * time.Millisecond)
			}
			tick = timer.C
		case <-tick:
			tick = nil
			_ = write()
		case acknowledge := <-writer.flush:
			stopTimer()
			acknowledge <- write()
		case acknowledge := <-writer.stop:
			stopTimer()
			acknowledge <- write()
			return
		}
	}
}

func (state *model) enableRecovery(store session.RecoveryStore) error {
	snapshot, err := store.Load()
	if err != nil {
		return err
	}
	if len(snapshot.Drafts) > 0 {
		if err := state.session.RestoreRecovery(snapshot); err != nil {
			return err
		}
		state.message = "已恢复未保存草稿"
	}
	state.autoSave.recovered = make(map[int]string, len(snapshot.Drafts))
	if len(snapshot.Drafts) > 0 {
		for _, document := range state.session.Tabs {
			if document.Dirty() {
				state.autoSave.recovered[document.ID] = document.Editor.CommittedString()
			}
		}
	}
	state.recoveryLast = state.session.RecoverySnapshot()
	state.recoveryWriter = newRecoveryWriter(store.Save, &state.invalidator)
	return nil
}

func (state *model) scheduleRecovery() {
	if state.recoveryWriter == nil {
		return
	}
	snapshot := state.session.RecoverySnapshot()
	if state.recoveryDiscarded {
		snapshot = session.RecoverySnapshot{Version: 1}
	}
	if reflect.DeepEqual(snapshot, state.recoveryLast) {
		return
	}
	state.recoveryLast = snapshot
	state.recoveryWriter.Schedule(snapshot)
}

func (state *model) flushRecovery() error {
	if state.recoveryWriter == nil {
		return nil
	}
	state.scheduleRecovery()
	return state.recoveryWriter.Flush()
}

func (state *model) stopRecovery() error {
	if state.recoveryWriter == nil {
		return nil
	}
	state.scheduleRecovery()
	err := state.recoveryWriter.Close()
	state.recoveryWriter = nil
	return err
}

func (state *model) drainRecoveryErrors() {
	if state.recoveryWriter == nil {
		return
	}
	for {
		select {
		case err := <-state.recoveryWriter.errors:
			state.recoveryWriteErr = err
		default:
			return
		}
	}
}

func defaultRecoveryStore() (session.RecoveryStore, error) {
	if path := os.Getenv("MARKSTACK_RECOVERY_PATH"); path != "" {
		return session.RecoveryStore{Path: path}, nil
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return session.RecoveryStore{}, err
	}
	return session.RecoveryStore{Path: filepath.Join(root, "MarkStack", "recovery.json")}, nil
}

func recoveryEnabled() bool {
	return os.Getenv("VIVA_HEADLESS") != "1" || os.Getenv("MARKSTACK_RECOVERY_PATH") != ""
}
