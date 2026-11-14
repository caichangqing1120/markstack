package main

import (
	"path/filepath"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"markstack/internal/icons"
	"markstack/internal/workspace"
)

func (state *model) paintTreeActions(canvas *gfx.Canvas, theme ui.Theme, palette colors, width float32) {
	hasRoot := state.session.Root != ""
	selected := state.selectedTreeNode() != ""
	y := float32(108)
	iconButton(canvas, fileNewID, icons.FilePlus2, "", "新建 Markdown 文件", ui.Rect{X: 11, Y: y, Width: 28, Height: 26}, false, !hasRoot, palette, func() { state.beginFileAction(fileActionCreateFile) })
	iconButton(canvas, folderNewID, icons.FolderPlus, "", "新建文件夹", ui.Rect{X: 44, Y: y, Width: 28, Height: 26}, false, !hasRoot, palette, func() { state.beginFileAction(fileActionCreateFolder) })
	iconButton(canvas, treeRenameID, icons.Pencil, "", "重命名选中项", ui.Rect{X: 77, Y: y, Width: 28, Height: 26}, false, !selected || !workspace.RenameAvailable(), palette, func() { state.beginFileAction(fileActionRename) })
	iconButton(canvas, treeTrashID, icons.Trash2, "", "移到系统回收站", ui.Rect{X: 110, Y: y, Width: 28, Height: 26}, false, !selected || !workspace.TrashAvailable(), palette, func() { state.beginFileAction(fileActionTrashConfirm) })
	state.treeNameRect = ui.Rect{}
	if state.fileAction == fileActionNone || state.fileAction == fileActionTrashConfirm {
		return
	}
	input := ui.Rect{X: 11, Y: 139, Width: width - 79, Height: 28}
	state.treeNameRect = input
	inputTheme := theme
	inputTheme.Metrics.FontSizeM = 12
	value := state.treeName.String()
	focused := canvas.Focused() == treeNameID
	components.PaintInput(canvas, inputTheme, input, components.InputSnapshot{
		ID: treeNameID, Label: "文件或文件夹名称", Value: value, Placeholder: "名称", Focused: focused,
		PlatformEditing: focused && value != "", ShowCaret: focused && value == "",
	})
	labeledButton(canvas, treeApplyID, "✓", "确认名称", ui.Rect{X: width - 64, Y: 140, Width: 26, Height: 26}, false, palette, state.applyFileAction)
	labeledButton(canvas, treeCancelID, "×", "取消文件操作", ui.Rect{X: width - 35, Y: 140, Width: 26, Height: 26}, false, palette, state.cancelFileAction)
}

func (state *model) paintTreeTrashConfirmation(canvas *gfx.Canvas, palette colors, viewport ui.Rect) {
	canvas.PushLayer(11)
	canvas.PushFocusScope(treeTrashScope)
	canvas.Fill(viewport, ui.RGBA(15, 19, 22, 0.45))
	canvas.HitWithOptions(treeTrashBackdropID, viewport, "取消回收站操作", gfx.HitOptions{Role: gfx.RoleButton, Backdrop: true})
	if canvas.Clicked(treeTrashBackdropID) {
		state.cancelFileAction()
	}
	width := min(float32(400), viewport.Width-40)
	box := ui.Rect{X: (viewport.Width - width) / 2, Y: (viewport.Height - 148) / 2, Width: width, Height: 148}
	canvas.RoundedBorder(box, 5, 1, palette.border, palette.background)
	canvas.Label("移到系统回收站？", ui.Rect{X: box.X + 20, Y: box.Y + 18, Width: width - 40, Height: 28}, gfx.TextStyle{Size: 16, Weight: gfx.WeightSemibold, Color: palette.ink})
	canvas.Label(ellipsize(canvas, filepath.Base(state.fileActionTarget), width-40, 12), ui.Rect{X: box.X + 20, Y: box.Y + 52, Width: width - 40, Height: 25}, gfx.TextStyle{Size: 12, Color: palette.muted})
	labeledButton(canvas, treeTrashCancelID, "取消", "取消回收站操作", ui.Rect{X: box.X + width - 198, Y: box.Y + 99, Width: 70, Height: 32}, false, palette, state.cancelFileAction)
	labeledButton(canvas, treeTrashApproveID, "移到回收站", "确认移到系统回收站", ui.Rect{X: box.X + width - 118, Y: box.Y + 99, Width: 100, Height: 32}, false, palette, state.confirmTreeTrash)
	canvas.SetInitialFocus(treeTrashCancelID)
	canvas.SetEscapeTarget(treeTrashCancelID)
	canvas.TrapFocus(treeTrashScope)
	canvas.PopFocusScope()
	canvas.PopLayer()
}
