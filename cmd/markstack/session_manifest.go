package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/vivaui/viva/app"

	"markstack/internal/session"
	"markstack/internal/workspace"
)

type sessionWriter = snapshotWriter[session.SessionManifest]

func newSessionWriter(save func(session.SessionManifest) error, wake *app.Invalidator) *sessionWriter {
	return newSnapshotWriter(save, wake)
}

func (state *model) enableSessionManifest(store session.SessionStore, restore bool) error {
	manifest, err := store.Load()
	if err != nil {
		return err
	}
	if restore && (manifest.Root != "" || len(manifest.Tabs) > 0) {
		warnings, err := state.session.RestoreManifest(manifest)
		if err != nil {
			return err
		}
		message := state.message
		state.resetDocumentView()
		if state.session.Current().Kind == workspace.RichHTMLKind {
			state.mode = renderedMode
		}
		state.message = message
		if len(warnings) > 0 {
			state.message = fmt.Sprintf("会话恢复跳过 %d 个不可用标签", len(warnings))
		}
	}
	state.sessionLast = state.session.Manifest()
	state.sessionWriter = newSessionWriter(store.Save, &state.invalidator)
	if !restore && !reflect.DeepEqual(manifest, state.sessionLast) {
		state.sessionWriter.Schedule(state.sessionLast)
	}
	return nil
}

func (state *model) currentSessionManifest() session.SessionManifest {
	manifest := state.session.Manifest()
	if !state.quitDiscarded {
		return manifest
	}
	kept := manifest.Tabs[:0]
	for _, tab := range manifest.Tabs {
		if tab.Dirty {
			if manifest.ActiveID == tab.ID {
				manifest.ActiveID = 0
			}
			continue
		}
		kept = append(kept, tab)
	}
	manifest.Tabs = kept
	return manifest
}

func (state *model) scheduleSessionManifest() {
	if state.sessionWriter == nil {
		return
	}
	manifest := state.currentSessionManifest()
	if reflect.DeepEqual(manifest, state.sessionLast) {
		return
	}
	state.sessionLast = manifest
	state.sessionWriter.Schedule(manifest)
}

func (state *model) flushSessionManifest() error {
	if state.sessionWriter == nil {
		return nil
	}
	state.scheduleSessionManifest()
	return state.sessionWriter.Flush()
}

func (state *model) stopSessionManifest() error {
	if state.sessionWriter == nil {
		return nil
	}
	state.scheduleSessionManifest()
	err := state.sessionWriter.Close()
	state.sessionWriter = nil
	return err
}

func (state *model) drainSessionManifestErrors() {
	if state.sessionWriter == nil {
		return
	}
	for {
		select {
		case err := <-state.sessionWriter.errors:
			if err != nil {
				state.message = "会话快照失败：" + err.Error()
			} else if strings.HasPrefix(state.message, "会话快照失败：") {
				state.message = ""
			}
		default:
			return
		}
	}
}

func defaultSessionStore() (session.SessionStore, error) {
	if path := os.Getenv("MARKSTACK_SESSION_PATH"); path != "" {
		return session.SessionStore{Path: path}, nil
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return session.SessionStore{}, err
	}
	return session.SessionStore{Path: filepath.Join(root, "MarkStack", "session.json")}, nil
}

func sessionManifestEnabled() bool {
	return os.Getenv("VIVA_HEADLESS") != "1" || os.Getenv("MARKSTACK_SESSION_PATH") != ""
}
