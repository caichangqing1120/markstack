package main

import (
	"time"

	"markstack/internal/session"
)

type autoSaveEntry struct {
	source string
	path   string
	due    time.Time
}

type autoSaveState struct {
	pending   map[int]autoSaveEntry
	blocked   map[int]string
	recovered map[int]string
	timer     *time.Timer
	armedDue  time.Time
	clock     func() time.Time
	delay     time.Duration
}

func (state *model) autoSaveNow() time.Time {
	if state.autoSave.clock != nil {
		return state.autoSave.clock()
	}
	return time.Now()
}

func (state *model) autoSaveDelay() time.Duration {
	if state.autoSave.delay > 0 {
		return state.autoSave.delay
	}
	return time.Second
}

func (state *model) documentComposing(document *session.Document) bool {
	if document.Composing() {
		return true
	}
	if document != state.session.Current() {
		return false
	}
	if document.Kind == 0 {
		return false
	}
	if state.rich.active && state.rich.input != nil && state.richComposing() {
		return true
	}
	if state.codeBlock.active && state.codeBlockComposing() {
		return true
	}
	if state.table.active && state.table.input != nil && state.tableComposing() {
		return true
	}
	return state.html.active && state.html.input != nil && state.htmlComposing()
}

func (state *model) scheduleAutoSave() {
	if !state.preferences.AutoSave {
		state.stopAutoSave()
		return
	}
	if state.session.Pending() != nil {
		state.cancelAutoSavePending()
		return
	}
	writer := &state.autoSave
	if writer.pending == nil {
		writer.pending = make(map[int]autoSaveEntry)
	}
	if writer.blocked == nil {
		writer.blocked = make(map[int]string)
	}
	now := state.autoSaveNow()
	open := make(map[int]bool, len(state.session.Tabs))
	for _, document := range state.session.Tabs {
		open[document.ID] = true
		if document.Path == "" || !document.Dirty() || state.documentComposing(document) {
			delete(writer.pending, document.ID)
			if !document.Dirty() {
				delete(writer.blocked, document.ID)
				delete(writer.recovered, document.ID)
			}
			continue
		}
		source := document.Editor.CommittedString()
		if recovered, ok := writer.recovered[document.ID]; ok {
			if recovered == source {
				delete(writer.pending, document.ID)
				continue
			}
			delete(writer.recovered, document.ID)
		}
		if blocked, ok := writer.blocked[document.ID]; ok && blocked == source {
			delete(writer.pending, document.ID)
			continue
		}
		delete(writer.blocked, document.ID)
		if pending, ok := writer.pending[document.ID]; ok && pending.source == source && pending.path == document.Path {
			continue
		}
		writer.pending[document.ID] = autoSaveEntry{source: source, path: document.Path, due: now.Add(state.autoSaveDelay())}
	}
	for id := range writer.pending {
		if !open[id] {
			delete(writer.pending, id)
		}
	}
	for id := range writer.blocked {
		if !open[id] {
			delete(writer.blocked, id)
		}
	}
	for id := range writer.recovered {
		if !open[id] {
			delete(writer.recovered, id)
		}
	}
	state.armAutoSave(now)
}

func (state *model) armAutoSave(now time.Time) {
	writer := &state.autoSave
	var earliest time.Time
	for _, pending := range writer.pending {
		if earliest.IsZero() || pending.due.Before(earliest) {
			earliest = pending.due
		}
	}
	if earliest.IsZero() {
		if writer.timer != nil {
			writer.timer.Stop()
			writer.timer = nil
		}
		writer.armedDue = time.Time{}
		return
	}
	if writer.timer != nil && writer.armedDue.Equal(earliest) {
		return
	}
	if writer.timer != nil {
		writer.timer.Stop()
	}
	wait := earliest.Sub(now)
	if wait < 0 {
		wait = 0
	}
	writer.armedDue = earliest
	writer.timer = time.AfterFunc(wait, func() { state.invalidator.Invalidate() })
}

func (state *model) drainAutoSave() {
	writer := &state.autoSave
	if writer.timer != nil {
		writer.timer.Stop()
		writer.timer = nil
		writer.armedDue = time.Time{}
	}
	if !state.preferences.AutoSave {
		state.stopAutoSave()
		return
	}
	if state.session.Pending() != nil {
		state.cancelAutoSavePending()
		return
	}
	now := state.autoSaveNow()
	for id, pending := range writer.pending {
		if pending.due.After(now) {
			continue
		}
		delete(writer.pending, id)
		for index, document := range state.session.Tabs {
			if document.ID != id {
				continue
			}
			if document.Path != pending.path || !document.Dirty() || state.documentComposing(document) || document.Editor.CommittedString() != pending.source {
				break
			}
			if err := state.session.Save(index, ""); err != nil {
				writer.blocked[id] = pending.source
				state.message = "自动保存失败：" + err.Error()
			} else {
				delete(writer.blocked, id)
				state.commitPendingImageRenames(document.ID)
				if document == state.session.Current() {
					state.message = "已自动保存 " + document.Title
				}
			}
			state.invalidator.Invalidate()
			break
		}
	}
	state.scheduleAutoSave()
}

func (state *model) cancelAutoSavePending() {
	writer := &state.autoSave
	if writer.timer != nil {
		writer.timer.Stop()
		writer.timer = nil
	}
	writer.armedDue = time.Time{}
	clear(writer.pending)
}

func (state *model) stopAutoSave() {
	state.cancelAutoSavePending()
	clear(state.autoSave.blocked)
}
