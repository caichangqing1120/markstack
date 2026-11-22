package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/vivaui/viva/gfx"

	"markstack/internal/workspace"
)

func (state *model) onFileDrop(event gfx.FileDropEvent) gfx.EventResult {
	if event.Phase == gfx.FileDropLeave || state.imageInspector.open || event.X < 0 || event.X >= state.paneViewport || event.Y < 32 || event.Y >= state.dropHeight || state.session.Pending() != nil || state.fileAction != fileActionNone || state.fileMenuOpen || state.recentOpen {
		return gfx.EventResult{}
	}
	if event.Phase == gfx.FileDropOver {
		return gfx.EventResult{Handled: true}
	}
	if event.Phase != gfx.FileDropDrop {
		return gfx.EventResult{}
	}
	if err := state.openDroppedPaths(event.Paths); err != nil {
		state.message = err.Error()
		return gfx.EventResult{Repaint: true}
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (state *model) openDroppedPaths(paths []string) error {
	if len(paths) == 0 {
		return errors.New("没有可打开的文件")
	}
	directory := false
	for index, path := range paths {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("不是绝对路径：%s", path)
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return workspace.ErrOutsideWorkspace
		}
		if info.IsDir() {
			if index != 0 || len(paths) != 1 {
				return errors.New("一次只能打开一个工作区")
			}
			directory = true
		} else if !info.Mode().IsRegular() || workspace.KindOf(path) == workspace.UnsupportedKind {
			return fmt.Errorf("不是受支持的文档：%s", path)
		}
	}
	if directory {
		changed, err := state.session.RequestWorkspace(paths[0])
		if err != nil {
			return err
		}
		if changed {
			state.cancelWorkspaceSearch()
			state.collapsed = make(map[string]bool)
			state.resetDocumentView()
			state.recordRecentWorkspace(state.session.Root)
		} else {
			state.message = ""
		}
		return nil
	}
	if err := state.session.OpenMany(paths); err != nil {
		return err
	}
	state.resetDocumentView()
	if state.session.Current().Kind == workspace.RichHTMLKind {
		state.mode = renderedMode
	}
	state.recordRecentFiles(paths)
	return nil
}
