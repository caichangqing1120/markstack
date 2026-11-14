package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"markstack/internal/workspace"
)

type fileActionKind uint8

const (
	fileActionNone fileActionKind = iota
	fileActionCreateFile
	fileActionCreateFolder
	fileActionRename
	fileActionTrashConfirm
)

func (state *model) selectedTreeNode() string {
	path := state.treeSelection
	if path == "" && state.session.Current() != nil {
		path = state.session.Current().Path
	}
	if path == "" || state.session.Root == "" {
		return ""
	}
	relative, err := filepath.Rel(state.session.Root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ""
	}
	return path
}

func (state *model) beginFileAction(action fileActionKind) {
	state.fileMenuOpen = false
	if state.session.Root == "" {
		state.message = "请先打开工作区"
		return
	}
	selected := state.selectedTreeNode()
	if action == fileActionRename || action == fileActionTrashConfirm {
		if selected == "" {
			state.message = "请先选择文件或文件夹"
			return
		}
	}
	state.fileAction = action
	state.fileActionTarget = selected
	if action == fileActionCreateFile || action == fileActionCreateFolder {
		parent := state.session.Root
		if selected != "" {
			if info, err := os.Lstat(selected); err == nil && info.IsDir() {
				parent = selected
			} else {
				parent = filepath.Dir(selected)
			}
		}
		state.fileActionTarget = parent
	}
	if action == fileActionTrashConfirm {
		state.invalidator.Invalidate()
		return
	}
	name := ""
	if action == fileActionRename {
		name = filepath.Base(selected)
	}
	state.treeName = text.NewEditor(name)
	if name != "" {
		state.treeName.SetSelection(text.Range{End: state.treeName.RuneCount()})
	}
	state.focusRequest, state.focusReset = treeNameID, true
	state.invalidator.Invalidate()
}

func (state *model) cancelFileAction() {
	state.fileAction = fileActionNone
	state.fileActionTarget = ""
	state.treeNameRect = ui.Rect{}
	state.invalidator.Invalidate()
}

func (state *model) applyFileAction() {
	if state.fileAction == fileActionNone || state.fileAction == fileActionTrashConfirm {
		return
	}
	name := state.treeName.String()
	if strings.TrimSpace(name) == "" {
		state.message = "请输入名称"
		return
	}
	root := state.session.Root
	var resultPath string
	var recentErr error
	switch state.fileAction {
	case fileActionCreateFile:
		if filepath.Ext(name) == "" {
			name += ".md"
		}
		resultPath = filepath.Join(state.fileActionTarget, name)
		relative, err := filepath.Rel(root, resultPath)
		if err != nil {
			state.message = err.Error()
			return
		}
		snapshot, err := workspace.CreateMarkdown(root, relative, nil)
		if err != nil {
			state.message = err.Error()
			return
		}
		state.openFile(snapshot.Path)
		resultPath = snapshot.Path
	case fileActionCreateFolder:
		resultPath = filepath.Join(state.fileActionTarget, name)
		relative, err := filepath.Rel(root, resultPath)
		if err != nil {
			state.message = err.Error()
			return
		}
		if err := workspace.CreateFolder(root, relative); err != nil {
			state.message = err.Error()
			return
		}
	case fileActionRename:
		if err := state.session.RenameNode(state.fileActionTarget, name); err != nil {
			state.message = err.Error()
			return
		}
		resultPath = filepath.Join(filepath.Dir(state.fileActionTarget), name)
		state.rebaseCollapsedNodes(state.fileActionTarget, resultPath)
		recentErr = state.rebaseRecents(state.fileActionTarget, resultPath)
		state.resetDocumentView()
	}
	if nodes, err := workspace.Scan(root); err == nil {
		state.session.Nodes = nodes
	}
	state.treeSelection = resultPath
	state.cancelFileAction()
	if state.session.Current().Path == resultPath {
		state.deferEditingFocus()
	}
	state.message = "已完成文件操作"
	if recentErr != nil {
		state.message = "最近记录未保存：" + recentErr.Error()
	}
}

func (state *model) rebaseCollapsedNodes(oldPath, newPath string) {
	updated := make(map[string]bool, len(state.collapsed))
	for path, value := range state.collapsed {
		if path == oldPath || strings.HasPrefix(path, oldPath+string(filepath.Separator)) {
			path = newPath + strings.TrimPrefix(path, oldPath)
		}
		updated[path] = value
	}
	state.collapsed = updated
}

func (state *model) confirmTreeTrash() {
	if state.fileAction != fileActionTrashConfirm {
		return
	}
	result, err := state.session.TrashNode(state.fileActionTarget)
	if err != nil {
		state.cancelFileAction()
		state.message = err.Error()
		return
	}
	old := state.fileActionTarget
	recentErr := state.removeRecentPaths(old)
	for path := range state.collapsed {
		if path == old || strings.HasPrefix(path, old+string(filepath.Separator)) {
			delete(state.collapsed, path)
		}
	}
	state.fileTrashResult = result
	state.treeSelection = ""
	state.cancelFileAction()
	state.resetDocumentView()
	state.message = "已移到系统回收站"
	if recentErr != nil {
		state.message = "最近记录未保存：" + recentErr.Error()
	}
}
