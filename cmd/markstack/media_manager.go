package main

import (
	"context"
	"fmt"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"markstack/internal/markdownmedia"
	"markstack/internal/workspace"
)

var (
	mediaMenuID       = controlTree.ID(page, "media-manager")
	mediaCloseID      = controlTree.ID(page, "media-close")
	mediaTrashID      = controlTree.ID(page, "media-trash")
	mediaConfirmID    = controlTree.ID(page, "media-confirm")
	mediaCancelID     = controlTree.ID(page, "media-cancel")
	mediaRefreshID    = controlTree.ID(page, "media-refresh")
	mediaReferencedID = controlTree.ID(page, "media-referenced")
	mediaOrphansID    = controlTree.ID(page, "media-orphans")
	mediaBackID       = controlTree.ID(page, "media-back")
	mediaScope        = controlTree.ID(page, "media-scope")
	mediaBackdropID   = controlTree.ID(page, "media-backdrop")
	mediaRows         = controlTree.Sub(page, controlTree.Name("media-rows"))
)

type mediaManager struct {
	open, orphans, confirming, rechecking bool
	root                                  string
	result                                markdownmedia.AuditResult
	scan                                  panelScan[markdownmedia.AuditResult]
	selected                              map[string]bool
	detail                                *markdownmedia.AuditedImage
	status                                string
	rows                                  ui.Rect
	scroll, extent                        float32
	trash                                 func(markdownmedia.AuditResult, string, markdownmedia.AuditedImage) (workspace.TrashResult, error)
}

func (state *model) openMediaManager() {
	if !state.prepareDocumentNavigation() {
		return
	}
	panel := &state.mediaManager
	panel.open, panel.orphans = true, false
	panel.root = state.session.Root
	state.scanMedia(false)
}

func (state *model) closeMediaManager() {
	panel := &state.mediaManager
	panel.scan.stop()
	trash := panel.trash
	*panel = mediaManager{trash: trash}
}

func (state *model) scanMedia(recheck bool) {
	panel := &state.mediaManager
	panel.confirming, panel.rechecking = false, recheck
	if !recheck {
		panel.selected = make(map[string]bool)
		panel.result, panel.detail = markdownmedia.AuditResult{}, nil
		panel.scroll = 0
	}
	panel.status = ""
	root := panel.root
	if root == "" {
		panel.status = "请先打开工作区"
		return
	}
	panel.scan.start(state.invalidator.Invalidate, func(ctx context.Context) (markdownmedia.AuditResult, error) {
		return markdownmedia.Audit(ctx, root)
	})
}

func (state *model) mediaDraftsPending() bool {
	for _, document := range state.session.Tabs {
		if (document.Kind == workspace.MarkdownKind || document.Kind == workspace.RichHTMLKind) && (document.Dirty() || state.documentComposing(document)) {
			return true
		}
	}
	return false
}

func (state *model) drainMediaManager() {
	panel := &state.mediaManager
	if !panel.open {
		return
	}
	if panel.root != state.session.Root {
		state.closeMediaManager()
		return
	}
	result, ready := panel.scan.receive()
	if !ready {
		return
	}
	recheck := panel.rechecking
	panel.rechecking = false
	if result.err != nil {
		panel.status = "扫描失败：" + result.err.Error()
		clear(panel.selected)
		return
	}
	if !recheck {
		panel.result = result.data
		return
	}
	confirmed, err := result.data.ConfirmedOrphans(panel.result, panel.selected)
	panel.result = result.data
	clear(panel.selected)
	if err != nil || state.mediaDraftsPending() || !workspace.TrashAvailable() {
		panel.status = "引用、图片或草稿已变化，未移动文件"
		return
	}
	trash := panel.trash
	if trash == nil {
		trash = markdownmedia.AuditResult.Trash
	}
	var failures int
	var firstError error
	for _, image := range confirmed {
		if _, err := trash(panel.result, panel.root, image); err != nil {
			failures++
			if firstError == nil {
				firstError = err
			}
		}
	}
	state.refresh()
	state.scanMedia(false)
	panel.status = fmt.Sprintf("已移入回收站 %d 项", len(confirmed)-failures)
	if failures != 0 {
		panel.status += fmt.Sprintf("，失败 %d 项：%v", failures, firstError)
	}
}

func (state *model) paintMediaManager(canvas *gfx.Canvas, palette colors, viewport ui.Rect) {
	panel := &state.mediaManager
	if !panel.open {
		return
	}
	canvas.PushLayer(12)
	defer canvas.PopLayer()
	canvas.PushFocusScope(mediaScope)
	defer canvas.PopFocusScope()
	canvas.TrapFocus(mediaScope)
	canvas.Fill(viewport, ui.RGBA(15, 19, 22, 0.45))
	canvas.HitWithOptions(mediaBackdropID, viewport, "图片资源管理", gfx.HitOptions{Role: gfx.RoleGroup, Backdrop: true, SkipFocus: true})
	width, height := min(float32(720), viewport.Width-44), min(float32(580), viewport.Height-58)
	if panel.confirming {
		height = 182
	}
	box := ui.Rect{X: (viewport.Width - width) / 2, Y: (viewport.Height - height) / 2, Width: width, Height: height}
	canvas.RoundedBorder(box, 7, 1, palette.border, palette.background)
	label := func(value string, x, y, w, h, font float32, color ui.Color) {
		canvas.Label(ellipsize(canvas, value, w, font), ui.Rect{X: x, Y: y, Width: w, Height: h}, gfx.TextStyle{Size: font, Color: color})
	}
	if panel.confirming {
		label("移入系统回收站？", box.X+20, box.Y+12, width-40, 36, 17, palette.ink)
		label(fmt.Sprintf("已选择 %d 张未引用图片。确认后将重新核对引用。", len(panel.selected)), box.X+20, box.Y+58, width-40, 28, 13, palette.muted)
		button(canvas, mediaCancelID, "取消", ui.Rect{X: box.X + width - 228, Y: box.Y + height - 53, Width: 74, Height: 32}, false, palette, func() { panel.confirming = false })
		button(canvas, mediaConfirmID, "确认移入回收站", ui.Rect{X: box.X + width - 145, Y: box.Y + height - 53, Width: 125, Height: 32}, true, palette, func() { state.scanMedia(true) })
		canvas.SetInitialFocus(mediaCancelID)
		canvas.SetEscapeTarget(mediaCancelID)
		return
	}
	label("图片资源管理", box.X+18, box.Y+9, width-190, 30, 17, palette.ink)
	button(canvas, mediaCloseID, "关闭", ui.Rect{X: box.X + width - 70, Y: box.Y + 10, Width: 52, Height: 28}, false, palette, state.closeMediaManager)
	canvas.SetInitialFocus(mediaCloseID)
	canvas.SetEscapeTarget(mediaCloseID)
	if panel.scan.ready == nil && panel.root != "" {
		button(canvas, mediaRefreshID, "重新扫描", ui.Rect{X: box.X + width - 152, Y: box.Y + 10, Width: 76, Height: 28}, false, palette, func() { state.scanMedia(false) })
	}
	label(fmt.Sprintf("图片总占用：%d 字节", panel.result.TotalBytes), box.X+18, box.Y+40, width-36, 24, 12, palette.muted)
	if detail := panel.detail; detail == nil {
		for index, category := range []struct {
			id     gfx.ID
			label  string
			orphan bool
		}{{mediaReferencedID, fmt.Sprintf("已引用图片 (%d)", len(panel.result.Referenced)), false}, {mediaOrphansID, fmt.Sprintf("未引用图片 (%d)", len(panel.result.Orphans)), true}} {
			button(canvas, category.id, category.label, ui.Rect{X: box.X + 18 + float32(index)*138, Y: box.Y + 71, Width: 130, Height: 28}, panel.orphans == category.orphan, palette, func() {
				panel.orphans, panel.scroll = category.orphan, 0
			})
		}
	} else {
		button(canvas, mediaBackID, "返回", ui.Rect{X: box.X + 18, Y: box.Y + 71, Width: 60, Height: 28}, false, palette, func() { panel.detail, panel.scroll = nil, 0 })
		label(detail.Relative, box.X+86, box.Y+71, width-104, 28, 13, palette.ink)
	}
	panel.rows = ui.Rect{X: box.X + 18, Y: box.Y + 105, Width: width - 36, Height: height - 160}
	state.paintMediaRows(canvas, palette)
	status := panel.status
	if status == "" {
		switch {
		case panel.rechecking:
			status = "正在重新核对引用与文件版本…"
		case panel.scan.ready != nil:
			status = "正在扫描工作区…"
		case panel.result.Incomplete:
			status = "扫描不完整：存在跳过的内容或已达扫描上限"
		case state.mediaDraftsPending():
			status = "存在未保存的 Markdown 草稿，暂不可清理"
		case !workspace.TrashAvailable():
			status = "当前运行环境不支持系统回收站"
		default:
			status = fmt.Sprintf("已勾选 %d 项", len(panel.selected))
		}
	}
	label(status, box.X+18, box.Y+height-44, width-172, 28, 12, palette.muted)
	if panel.orphans && panel.detail == nil && panel.scan.ready == nil && !panel.result.Incomplete && len(panel.selected) > 0 && !state.mediaDraftsPending() && workspace.TrashAvailable() {
		button(canvas, mediaTrashID, "移入回收站", ui.Rect{X: box.X + width - 137, Y: box.Y + height - 46, Width: 119, Height: 32}, true, palette, func() { panel.confirming = true })
	}
}

func (state *model) paintMediaRows(canvas *gfx.Canvas, palette colors) {
	panel := &state.mediaManager
	rows := panel.result.Referenced
	if panel.orphans {
		rows = panel.result.Orphans
	}
	count := len(rows)
	if panel.detail != nil {
		count = len(panel.detail.Documents)
	}
	const rowHeight = 30
	panel.extent = max(0, float32(count*rowHeight)-panel.rows.Height)
	panel.scroll = max(0, min(panel.scroll, panel.extent))
	canvas.Clip(panel.rows)
	defer canvas.Unclip()
	for index := int(panel.scroll / rowHeight); index < count; index++ {
		row := ui.Rect{X: panel.rows.X, Y: panel.rows.Y + float32(index*rowHeight) - panel.scroll, Width: panel.rows.Width, Height: rowHeight - 1}
		if row.Y >= panel.rows.Y+panel.rows.Height {
			break
		}
		label := func(value string, bounds ui.Rect, color ui.Color) {
			canvas.Label(ellipsize(canvas, value, bounds.Width, 12), bounds, gfx.TextStyle{Size: 12, Color: color})
		}
		if panel.detail != nil {
			label(panel.detail.Documents[index], row.Inset(gfx.Insets{Left: 8, Right: 8}), palette.ink)
			continue
		}
		image, id := rows[index], mediaRows.At(index)
		checked := panel.selected[image.Relative]
		if checked || canvas.Hovered() == id || canvas.FocusVisible() == id {
			canvas.Rounded(row, 4, palette.selected)
		}
		prefix, suffix := "", fmt.Sprintf("%d 字节 · %d 篇", image.Bytes, len(image.Documents))
		options := gfx.HitOptions{Role: gfx.RoleButton, Hint: suffix}
		if panel.orphans {
			prefix, suffix = "☐  ", fmt.Sprintf("%d 字节", image.Bytes)
			if checked {
				prefix = "☑  "
			}
			options = gfx.HitOptions{Role: gfx.RoleCheckbox, Checked: checked, Disabled: !image.Trashable() || panel.result.Incomplete || panel.scan.ready != nil || !workspace.TrashAvailable()}
		}
		label(prefix+image.Relative, ui.Rect{X: row.X + 8, Y: row.Y, Width: row.Width - 164, Height: row.Height}, palette.ink)
		label(suffix, ui.Rect{X: row.X + row.Width - 150, Y: row.Y, Width: 142, Height: row.Height}, palette.muted)
		canvas.HitWithOptions(id, row, image.Relative, options)
		if !options.Disabled && canvas.Clicked(id) {
			if !panel.orphans {
				panel.detail, panel.scroll = &image, 0
				return
			}
			if checked {
				delete(panel.selected, image.Relative)
			} else {
				panel.selected[image.Relative] = true
			}
		}
	}
	if count == 0 && panel.scan.ready == nil {
		canvas.LabelCenter("没有图片", panel.rows, gfx.TextStyle{Size: 12, Color: palette.muted})
	}
}
