package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"markstack/internal/session"
	"markstack/internal/workspace"
)

func (state *model) openRecentPanel() {
	state.fileMenuOpen = false
	state.recentOpen = true
	state.recentShowFiles = len(state.recents.Workspaces) == 0 && len(state.recents.Files) > 0
}

func (state *model) openRecentWorkspace(path string) {
	changed, err := state.session.RequestWorkspace(path)
	if err != nil {
		state.message = err.Error()
		return
	}
	state.recentOpen = false
	if changed {
		state.cancelWorkspaceSearch()
		state.collapsed = make(map[string]bool)
		state.resetDocumentView()
		state.recordRecentWorkspace(state.session.Root)
	} else {
		state.message = ""
	}
}

func (state *model) openRecentFile(path string) {
	if state.openFile(path) {
		state.recentOpen = false
	}
}

func (state *model) clearRecents() {
	next := state.recents
	next.Clear()
	if state.recentStore != nil {
		if err := state.recentStore.Save(next); err != nil {
			state.message = "最近记录未清空：" + err.Error()
			return
		}
	}
	state.recents = next
}

func (state *model) paintRecentPanel(canvas *gfx.Canvas, palette colors, viewport ui.Rect) {
	canvas.PushLayer(10)
	canvas.PushFocusScope(recentScope)
	canvas.Fill(viewport, ui.RGBA(15, 19, 22, 0.45))
	canvas.HitWithOptions(recentBackdropID, viewport, "关闭最近打开", gfx.HitOptions{Role: gfx.RoleButton, Backdrop: true})
	if canvas.Clicked(recentBackdropID) {
		state.recentOpen = false
	}
	width := min(float32(520), viewport.Width-32)
	rows := max(len(state.recents.Workspaces), len(state.recents.Files))
	height := min(float32(420), max(float32(182), float32(rows)*34+148))
	box := ui.Rect{X: (viewport.Width - width) / 2, Y: (viewport.Height - height) / 2, Width: width, Height: height}
	canvas.RoundedBorder(box, 5, 1, palette.border, palette.background)
	canvas.Label("最近打开", ui.Rect{X: box.X + 20, Y: box.Y + 14, Width: width - 72, Height: 28}, gfx.TextStyle{Size: 16, Weight: gfx.WeightSemibold, Color: palette.ink})
	labeledButton(canvas, recentCloseID, "×", "关闭最近打开", ui.Rect{X: box.X + width - 44, Y: box.Y + 14, Width: 24, Height: 24}, false, palette, func() { state.recentOpen = false })
	labeledButton(canvas, recentWorkspacesID, "工作区", "最近工作区", ui.Rect{X: box.X + 20, Y: box.Y + 56, Width: 88, Height: 27}, !state.recentShowFiles, palette, func() { state.recentShowFiles = false })
	labeledButton(canvas, recentFilesID, "文件", "最近文件", ui.Rect{X: box.X + 114, Y: box.Y + 56, Width: 70, Height: 27}, state.recentShowFiles, palette, func() { state.recentShowFiles = true })
	paths := state.recents.Workspaces
	if state.recentShowFiles {
		paths = state.recents.Files
	}
	list := ui.Rect{X: box.X + 16, Y: box.Y + 94, Width: width - 32, Height: min(float32(272), height-136)}
	canvas.Clip(list)
	for index, path := range paths {
		row := ui.Rect{X: list.X + 4, Y: list.Y + float32(index)*34, Width: list.Width - 8, Height: 32}
		if canvas.Hovered() == recentRows.At(index) {
			canvas.Rounded(row, 4, palette.selected)
		}
		canvas.Label(ellipsize(canvas, filepath.Base(path), row.Width-20, 12), ui.Rect{X: row.X + 10, Y: row.Y + 1, Width: row.Width - 20, Height: 16}, gfx.TextStyle{Size: 12, Color: palette.ink})
		canvas.Label(ellipsize(canvas, filepath.Dir(path), row.Width-20, 10), ui.Rect{X: row.X + 10, Y: row.Y + 16, Width: row.Width - 20, Height: 14}, gfx.TextStyle{Size: 10, Color: palette.muted})
		label := "打开最近工作区 " + path
		if state.recentShowFiles {
			label = "打开最近文件 " + path
		}
		canvas.HitWithOptions(recentRows.At(index), row, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: path})
		if canvas.Clicked(recentRows.At(index)) {
			if state.recentShowFiles {
				state.openRecentFile(path)
			} else {
				state.openRecentWorkspace(path)
			}
		}
	}
	canvas.Unclip()
	if len(paths) == 0 {
		canvas.Label("暂无最近项目", ui.Rect{X: list.X + 12, Y: list.Y + 18, Width: list.Width - 24, Height: 26}, gfx.TextStyle{Size: 12, Color: palette.muted})
	}
	button(canvas, recentClearID, "清空记录", ui.Rect{X: box.X + 20, Y: box.Y + height - 40, Width: 80, Height: 26}, false, palette, state.clearRecents)
	canvas.SetInitialFocus(recentCloseID)
	canvas.SetEscapeTarget(recentCloseID)
	canvas.TrapFocus(recentScope)
	canvas.PopFocusScope()
	canvas.PopLayer()
}

func (state *model) enableRecents(store session.RecentStore) error {
	recents, err := store.Load()
	if err != nil {
		return err
	}
	state.recents = recents
	state.recentStore = &store
	return nil
}

func (state *model) recordRecentFile(path string) {
	state.recordRecentFiles([]string{path})
}

func (state *model) recordRecentFiles(paths []string) {
	if state.recentStore == nil {
		return
	}
	for _, path := range paths {
		if location, err := workspace.CanonicalLocation(path); err == nil {
			path = location
		}
		state.recents.AddFile(path)
	}
	state.saveRecents()
}

func (state *model) recordRecentWorkspace(path string) {
	if state.recentStore == nil {
		return
	}
	state.recents.AddWorkspace(path)
	state.saveRecents()
}

func (state *model) saveRecents() {
	if state.recentStore != nil {
		if err := state.recentStore.Save(state.recents); err != nil {
			state.message = "最近记录未保存：" + err.Error()
		}
	}
}

func (state *model) rebaseRecents(oldPath, newPath string) error {
	return state.rewriteRecents(oldPath, newPath)
}

func (state *model) removeRecentPaths(path string) error {
	return state.rewriteRecents(path, "")
}

func (state *model) rewriteRecents(oldPath, newPath string) error {
	if state.recentStore == nil {
		return nil
	}
	if location, err := workspace.CanonicalLocation(oldPath); err == nil {
		oldPath = location
	}
	if newPath != "" {
		if location, err := workspace.CanonicalLocation(newPath); err == nil {
			newPath = location
		}
	}
	changed := false
	rebase := func(paths []string) []string {
		next := make([]string, 0, len(paths))
		seen := make(map[string]bool, len(paths))
		for _, path := range paths {
			if path == oldPath || strings.HasPrefix(path, oldPath+string(filepath.Separator)) {
				changed = true
				if newPath == "" {
					continue
				}
				path = newPath + strings.TrimPrefix(path, oldPath)
			}
			if !seen[path] {
				seen[path] = true
				next = append(next, path)
			}
		}
		return next
	}
	next := state.recents
	next.Workspaces = rebase(next.Workspaces)
	next.Files = rebase(next.Files)
	if !changed {
		return nil
	}
	state.recents = next
	return state.recentStore.Save(next)
}

func defaultRecentStore() (session.RecentStore, error) {
	if path := os.Getenv("MARKSTACK_RECENT_PATH"); path != "" {
		return session.RecentStore{Path: path}, nil
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return session.RecentStore{}, err
	}
	return session.RecentStore{Path: filepath.Join(root, "MarkStack", "recent.json")}, nil
}

func recentHistoryEnabled() bool {
	return os.Getenv("VIVA_HEADLESS") != "1" || os.Getenv("MARKSTACK_RECENT_PATH") != ""
}
