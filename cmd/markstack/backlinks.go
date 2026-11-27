package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"markstack/internal/workspace"
)

type panelScanResult[Data any] struct {
	data Data
	err  error
}

type panelScan[Data any] struct {
	cancel context.CancelFunc
	ready  <-chan panelScanResult[Data]
}

func (scan *panelScan[Data]) stop() {
	if scan.cancel != nil {
		scan.cancel()
	}
	scan.cancel, scan.ready = nil, nil
}

func (scan *panelScan[Data]) start(invalidate func(), work func(context.Context) (Data, error)) {
	scan.stop()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan panelScanResult[Data], 1)
	scan.cancel, scan.ready = cancel, ready
	go func() {
		data, err := work(ctx)
		if ctx.Err() == nil {
			ready <- panelScanResult[Data]{data, err}
			invalidate()
		}
	}()
}

func (scan *panelScan[Data]) receive() (panelScanResult[Data], bool) {
	select {
	case result := <-scan.ready:
		scan.stop()
		return result, true
	default:
		return panelScanResult[Data]{}, false
	}
}

var (
	outlineTabID       = controlTree.ID(page, "outline-tab")
	backlinksTabID     = controlTree.ID(page, "backlinks-tab")
	backlinksRefreshID = controlTree.ID(page, "backlinks-refresh")
	backlinkRows       = controlTree.Sub(page, controlTree.Name("backlink-rows"))
)

type backlinksData struct {
	rows       []workspace.Backlink
	incomplete bool
}

type backlinksPanel struct {
	selected bool
	root     string
	target   string
	scan     panelScan[backlinksData]
	data     backlinksData
	err      error
	rows     ui.Rect
	scroll   float32
}

const backlinkRowHeight = 46

func (state *model) syncBacklinks() {
	panel := &state.backlinks
	current := state.session.Current()
	root, target := state.session.Root, ""
	if state.outline && panel.selected && root != "" && current != nil && current.Kind == workspace.MarkdownKind && current.Path != "" {
		if relative, err := filepath.Rel(root, current.SavedLocation()); err == nil && filepath.IsLocal(relative) {
			target = current.SavedLocation()
		}
	}
	if target == "" {
		root = ""
	}
	if root == panel.root && target == panel.target {
		return
	}
	panel.scan.stop()
	panel.root, panel.target = root, target
	panel.data, panel.err, panel.scroll = backlinksData{}, nil, 0
	if target != "" {
		panel.scan.start(state.invalidator.Invalidate, func(ctx context.Context) (backlinksData, error) {
			rows, incomplete, err := workspace.Backlinks(ctx, root, target)
			return backlinksData{rows, incomplete}, err
		})
	}
}

func (state *model) refreshBacklinks() {
	state.backlinks.target = ""
	state.syncBacklinks()
	state.invalidator.Invalidate()
}

func (state *model) drainBacklinks() {
	state.syncBacklinks()
	if result, ready := state.backlinks.scan.receive(); ready {
		state.backlinks.data, state.backlinks.err = result.data, result.err
	}
}

func (state *model) paintRightPane(canvas *gfx.Canvas, theme ui.Theme, palette colors, bounds ui.Rect) {
	canvas.Fill(bounds, palette.sidebar)
	canvas.Fill(ui.Rect{X: bounds.X, Y: bounds.Y, Width: 1, Height: bounds.Height}, palette.border)
	tabWidth := (bounds.Width - 24) / 2
	button(canvas, outlineTabID, "大纲", ui.Rect{X: bounds.X + 8, Y: bounds.Y + 5, Width: tabWidth, Height: 26}, !state.backlinks.selected, palette, func() {
		state.backlinks.selected = false
		state.syncBacklinks()
	})
	labeledButton(canvas, backlinksTabID, "反向链接", "反向链接 / Backlinks", ui.Rect{X: bounds.X + 16 + tabWidth, Y: bounds.Y + 5, Width: tabWidth, Height: 26}, state.backlinks.selected, palette, func() {
		state.backlinks.selected = true
		state.syncBacklinks()
	})
	if state.backlinks.selected {
		state.paintBacklinks(canvas, palette, bounds)
		return
	}
	state.paintOutline(canvas, theme, palette, bounds)
}

func (state *model) paintBacklinks(canvas *gfx.Canvas, palette colors, bounds ui.Rect) {
	panel := &state.backlinks
	status := fmt.Sprintf("%d 处引用", len(panel.data.rows))
	switch {
	case panel.target == "":
		status = "请打开工作区内的 Markdown 文档"
	case panel.scan.ready != nil:
		status = "正在扫描…"
	case panel.err != nil:
		status = "扫描失败：" + panel.err.Error()
	case panel.data.incomplete:
		status += " · 结果不完整"
	}
	header := ui.Rect{X: bounds.X + 10, Y: bounds.Y + 37, Width: bounds.Width - 20, Height: 26}
	if panel.target != "" {
		header.Width -= 54
		button(canvas, backlinksRefreshID, "刷新", ui.Rect{X: header.X + header.Width + 4, Y: header.Y, Width: 50, Height: header.Height}, false, palette, state.refreshBacklinks)
	}
	canvas.Label(ellipsize(canvas, status, header.Width, 11), header, gfx.TextStyle{Size: 11, Color: palette.muted})
	panel.rows = ui.Rect{X: bounds.X + 7, Y: bounds.Y + 67, Width: bounds.Width - 14, Height: max(0, bounds.Height-67)}
	panel.scroll = max(0, min(panel.scroll, float32(len(panel.data.rows)*backlinkRowHeight)-panel.rows.Height))
	canvas.Clip(panel.rows)
	defer canvas.Unclip()
	for index := int(panel.scroll / backlinkRowHeight); index < len(panel.data.rows); index++ {
		link := panel.data.rows[index]
		row := ui.Rect{X: panel.rows.X, Y: panel.rows.Y + float32(index*backlinkRowHeight) - panel.scroll, Width: panel.rows.Width, Height: backlinkRowHeight}
		if row.Y >= panel.rows.Y+panel.rows.Height {
			break
		}
		id := backlinkRows.At(index)
		if canvas.Hovered() == id || canvas.FocusVisible() == id {
			canvas.Rounded(row, 4, palette.selected)
		}
		label := fmt.Sprintf("%s:%d", link.Relative, link.Line)
		location := ui.Rect{X: row.X + 6, Y: row.Y + 2, Width: row.Width - 12, Height: 20}
		context := location
		context.Y += 20
		canvas.Label(ellipsize(canvas, label, location.Width, 12), location, gfx.TextStyle{Size: 12, Color: palette.ink})
		canvas.Label(ellipsize(canvas, link.Context, context.Width, 11), context, gfx.TextStyle{Size: 11, Color: palette.muted})
		canvas.HitWithOptions(id, row, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: link.Context})
		if canvas.Clicked(id) {
			state.openBacklink(link)
			break
		}
	}
}

func (state *model) openBacklink(link workspace.Backlink) {
	if !state.prepareDocumentNavigation() {
		return
	}
	root := state.session.Root
	relative := filepath.FromSlash(link.Relative)
	snapshot, err := workspace.LoadWorkspaceMarkdown(root, relative)
	if err != nil || sha256.Sum256(snapshot.Bytes) != link.Fingerprint {
		state.message = "反向链接已变化，请刷新"
		return
	}
	for _, document := range state.session.Tabs {
		if document.SavedLocation() == snapshot.Location() && (document.Dirty() || !document.MatchesSavedRevision(snapshot)) {
			state.message = "引用文档有未保存或外部修改，无法定位到旧行号"
			return
		}
	}
	document, err := state.session.OpenWorkspaceMarkdown(relative)
	if err != nil || !document.MatchesSavedRevision(snapshot) || link.ByteOffset < 0 || link.ByteOffset >= len(document.Text()) {
		state.message = "反向链接已变化，请刷新"
		return
	}
	state.resetDocumentView()
	state.mode = editMode
	state.pendingSourceAnchor = utf8.RuneCountInString(document.Text()[:link.ByteOffset])
	document.Editor.MoveTo(state.pendingSourceAnchor, false)
	state.deferEditingFocus()
	state.recordRecentFile(document.Path)
}
