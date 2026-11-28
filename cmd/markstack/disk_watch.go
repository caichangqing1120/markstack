package main

import (
	"fmt"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"markstack/internal/session"
	"markstack/internal/workspace"
)

const (
	diskPollInterval = 2 * time.Second
	largePreviewText = "超大文件只读视图（>16 MiB）"
)

var (
	diskReloadID        = controlTree.ID(page, "disk-reload")
	diskIgnoreID        = controlTree.ID(page, "disk-ignore")
	diskSaveAsID        = controlTree.ID(page, "disk-save-as")
	diskReloadConfirmID = controlTree.ID(page, "disk-reload-confirm")
	diskReloadCancelID  = controlTree.ID(page, "disk-reload-cancel")
	diskReloadScope     = controlTree.ID(page, "disk-reload-scope")
	diskReloadBackdrop  = controlTree.ID(page, "disk-reload-backdrop")
)

type diskWatchEntry struct {
	path                  string
	baseline, observed    workspace.DiskRevision
	ignored, ignoredDirty bool
}

type diskReloadRequest struct {
	document *session.Document
	editor   *text.Editor
	draft    string
	revision workspace.DiskRevision
}

type diskWatch struct {
	entries map[*session.Document]diskWatchEntry
	timer   *time.Timer
	due     time.Time
	confirm *diskReloadRequest
}

func (state *model) onReadOnlyTextEvent(event app.TextEvent) (app.TextState, error) {
	editor := state.session.Current().Editor
	if event.Kind == app.TextSetSelection {
		event.Apply(editor)
	} else if event.Kind == app.TextCommandEvent {
		switch event.Command {
		case app.TextSelectAll, app.TextMoveLeft, app.TextMoveRight, app.TextMoveWordLeft, app.TextMoveWordRight,
			app.TextMoveLeftAndModifySelection, app.TextMoveRightAndModifySelection, app.TextMoveWordLeftAndModifySelection,
			app.TextMoveWordRightAndModifySelection, app.TextMoveLineStart, app.TextMoveLineEnd, app.TextMoveDocumentStart,
			app.TextMoveDocumentEnd, app.TextMoveLineStartAndModifySelection, app.TextMoveLineEndAndModifySelection,
			app.TextMoveDocumentStartAndModifySelection, app.TextMoveDocumentEndAndModifySelection:
			event.Apply(editor)
		}
	}
	return app.TextStateOf(editor), nil
}

func (state *model) syncDiskWatch() {
	watch := &state.diskWatch
	for document := range watch.entries {
		found := false
		for _, tab := range state.session.Tabs {
			if tab == document && tab.Path != "" {
				found = true
				break
			}
		}
		if !found {
			delete(watch.entries, document)
		}
	}
	for _, document := range state.session.Tabs {
		if document.Path == "" {
			continue
		}
		entry := watch.entries[document]
		baseline := document.SavedRevision()
		if entry.path == document.Path && entry.baseline.Same(baseline) {
			continue
		}
		if watch.entries == nil {
			watch.entries = make(map[*session.Document]diskWatchEntry)
		}
		watch.entries[document] = diskWatchEntry{path: document.Path, baseline: baseline, observed: baseline}
	}
	if len(watch.entries) == 0 {
		state.stopDiskWatch()
	} else if watch.timer == nil {
		watch.due = time.Now().Add(diskPollInterval)
		watch.timer = time.AfterFunc(diskPollInterval, state.invalidator.Invalidate)
	}
}

func (state *model) stopDiskWatch() {
	watch := &state.diskWatch
	if watch.timer != nil {
		watch.timer.Stop()
	}
	*watch = diskWatch{}
}

func (state *model) drainDiskWatch() {
	watch := &state.diskWatch
	if watch.timer == nil || time.Now().Before(watch.due) {
		return
	}
	watch.timer.Stop()
	for document, entry := range watch.entries {
		if document.Path != entry.path || !document.SavedRevision().Same(entry.baseline) {
			continue
		}
		state.observeDisk(document, false)
	}
	watch.timer = nil
	state.syncDiskWatch()
}

func (state *model) observeDisk(document *session.Document, showAgain bool) diskWatchEntry {
	entry := state.diskWatch.entries[document]
	if entry.path != document.Path || !entry.baseline.Same(document.SavedRevision()) {
		state.syncDiskWatch()
		entry = state.diskWatch.entries[document]
	}
	if entry.path == "" {
		return entry
	}
	observed := workspace.ReadDiskRevision(entry.path)
	if showAgain || !observed.Same(entry.observed) {
		entry.ignored = false
	}
	entry.observed = observed
	state.diskWatch.entries[document] = entry
	return entry
}

func (state *model) openDiskReload(document *session.Document, expected workspace.DiskRevision) {
	if !state.prepareDocumentNavigation() || state.session.Current() != document {
		return
	}
	if !state.observeDisk(document, false).observed.Same(expected) {
		state.message = "磁盘版本已再次变化，请重试"
		return
	}
	if document.Dirty() {
		state.diskWatch.confirm = &diskReloadRequest{document, document.Editor, document.Editor.CommittedString(), expected}
		return
	}
	state.reloadDisk(document, expected, false)
}

func (state *model) reloadDisk(document *session.Document, expected workspace.DiskRevision, discardDraft bool) {
	if state.documentComposing(document) {
		state.message = session.ErrCompositionPending.Error()
		return
	}
	if err := state.session.Reload(document, expected, discardDraft); err != nil {
		state.observeDisk(document, false)
		state.message = "重新加载失败：" + err.Error()
		return
	}
	state.rollbackPendingImageRenames(state.session.Root, document)
	delete(state.imageRenames, document.ID)
	state.resetDocumentView()
	state.syncDiskWatch()
	state.refreshBacklinks()
	state.message = "已加载磁盘版本"
	if state.mode == editMode || state.mode == splitMode || document.ReadOnly() {
		state.deferEditingFocus()
	}
}

func (state *model) paintDocumentBanners(canvas *gfx.Canvas, palette colors, bounds ui.Rect) ui.Rect {
	document := state.session.Current()
	if document == nil {
		return bounds
	}
	reserve := func(height float32) ui.Rect {
		bar := ui.Rect{X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: height}
		bounds.Y += height
		bounds.Height = max(0, bounds.Height-height)
		canvas.Fill(bar, palette.bar)
		canvas.Fill(ui.Rect{X: bar.X, Y: bar.Y + bar.Height - 1, Width: bar.Width, Height: 1}, palette.border)
		return bar
	}
	if document.ReadOnly() {
		bar := reserve(32).Inset(gfx.Insets{Left: 12, Right: 12})
		label := fmt.Sprintf("%s · 前 %d KiB / %.1f MiB", largePreviewText, workspace.LargePreviewSize>>10, float64(document.DiskSize())/(1<<20))
		canvas.Label(ellipsize(canvas, label, bar.Width, 12), bar, gfx.TextStyle{Size: 12, Color: palette.ink})
	}
	entry, watched := state.diskWatch.entries[document]
	dirty := document.Dirty() || state.documentComposing(document)
	if !watched || entry.path != document.Path || !entry.baseline.Same(document.SavedRevision()) || entry.observed.Same(entry.baseline) || entry.ignored && entry.ignoredDirty == dirty {
		return bounds
	}
	message := "文件已在磁盘上更改"
	if !entry.observed.Available() {
		message = "磁盘文件已移除或不可访问"
	}
	if dirty {
		message += "，草稿仍保留"
	}
	buttonWidth := float32(180)
	if dirty {
		buttonWidth = 314
	}
	stack := bounds.Width < buttonWidth+224
	height := float32(36)
	if stack {
		height = 64
	}
	bar := reserve(height)
	labelWidth, actionsY := bar.Width-buttonWidth-22, bar.Y+4
	if stack {
		labelWidth, actionsY = bar.Width-24, bar.Y+31
	}
	canvas.Label(ellipsize(canvas, message, labelWidth, 12), ui.Rect{X: bar.X + 12, Y: bar.Y + 2, Width: labelWidth, Height: 30}, gfx.TextStyle{Size: 12, Color: palette.ink})
	ignore := func() {
		entry.ignored, entry.ignoredDirty = true, dirty
		state.diskWatch.entries[document] = entry
		state.focusRequest = state.pendingCompositionFocus()
	}
	if dirty {
		x := bar.X + max(0, bar.Width-buttonWidth-8)
		if entry.observed.Available() {
			button(canvas, diskReloadID, "加载磁盘版本", ui.Rect{X: x, Y: actionsY, Width: 114, Height: 28}, false, palette, func() { state.openDiskReload(document, entry.observed) })
		}
		button(canvas, diskIgnoreID, "保留当前草稿", ui.Rect{X: x + 120, Y: actionsY, Width: 110, Height: 28}, false, palette, ignore)
		button(canvas, diskSaveAsID, "另存为", ui.Rect{X: x + 236, Y: actionsY, Width: 72, Height: 28}, false, palette, func() { state.saveActive(true) })
	} else {
		x := bar.X + max(0, bar.Width-buttonWidth-8)
		if entry.observed.Available() {
			button(canvas, diskReloadID, "重新加载", ui.Rect{X: x, Y: actionsY, Width: 86, Height: 28}, false, palette, func() { state.openDiskReload(document, entry.observed) })
		}
		button(canvas, diskIgnoreID, "忽略", ui.Rect{X: x + 94, Y: actionsY, Width: 78, Height: 28}, false, palette, ignore)
	}
	return bounds
}

func (state *model) paintDiskReloadConfirmation(canvas *gfx.Canvas, palette colors, viewport ui.Rect) {
	request := state.diskWatch.confirm
	if request == nil {
		return
	}
	canvas.PushLayer(12)
	defer canvas.PopLayer()
	canvas.PushFocusScope(diskReloadScope)
	defer canvas.PopFocusScope()
	canvas.TrapFocus(diskReloadScope)
	canvas.Fill(viewport, ui.RGBA(15, 19, 22, 0.45))
	canvas.HitWithOptions(diskReloadBackdrop, viewport, "重载磁盘版本确认", gfx.HitOptions{Role: gfx.RoleGroup, Backdrop: true, SkipFocus: true})
	box := ui.Rect{X: (viewport.Width - 440) / 2, Y: (viewport.Height - 170) / 2, Width: 440, Height: 170}
	canvas.RoundedBorder(box, 7, 1, palette.border, palette.background)
	canvas.Label("放弃草稿并加载磁盘版本？", ui.Rect{X: box.X + 20, Y: box.Y + 16, Width: 400, Height: 32}, gfx.TextStyle{Size: 17, Weight: gfx.WeightSemibold, Color: palette.ink})
	canvas.Label(ellipsize(canvas, request.document.Title, 400, 13), ui.Rect{X: box.X + 20, Y: box.Y + 57, Width: 400, Height: 28}, gfx.TextStyle{Size: 13, Color: palette.muted})
	button(canvas, diskReloadCancelID, "取消", ui.Rect{X: box.X + 199, Y: box.Y + 113, Width: 76, Height: 32}, false, palette, func() { state.diskWatch.confirm = nil })
	button(canvas, diskReloadConfirmID, "放弃草稿并加载", ui.Rect{X: box.X + 284, Y: box.Y + 113, Width: 136, Height: 32}, true, palette, func() {
		state.diskWatch.confirm = nil
		document := state.session.Current()
		if document != request.document || document.Editor != request.editor || document.Editor.CommittedString() != request.draft {
			state.message = "当前草稿已变化，请重新确认"
			return
		}
		state.reloadDisk(document, request.revision, true)
	})
	canvas.SetInitialFocus(diskReloadCancelID)
	canvas.SetEscapeTarget(diskReloadCancelID)
}
