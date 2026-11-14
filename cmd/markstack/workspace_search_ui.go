package main

import (
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"markstack/internal/workspace"
)

func (state *model) paintWorkspaceSearch(canvas *gfx.Canvas, theme ui.Theme, palette colors, bounds ui.Rect) {
	search := &state.workspaceSearch
	state.treeRect = ui.Rect{}
	canvas.Fill(bounds, palette.sidebar)
	canvas.Fill(ui.Rect{X: bounds.Width - 1, Y: bounds.Y, Width: 1, Height: bounds.Height}, palette.border)
	labeledButton(canvas, workspaceBackID, "←", "返回文件树", ui.Rect{X: 9, Y: 43, Width: 25, Height: 25}, false, palette, state.closeWorkspaceSearch)
	canvas.Label("工作区搜索", ui.Rect{X: 42, Y: 43, Width: bounds.Width - 53, Height: 25}, gfx.TextStyle{Size: 13, Weight: gfx.WeightSemibold, Color: palette.ink})
	inputRect := ui.Rect{X: 11, Y: 75, Width: bounds.Width - 22, Height: 28}
	search.rect = inputRect
	inputTheme := theme
	inputTheme.Metrics.FontSizeM = 12
	value := search.queryEditor.String()
	focused := canvas.Focused() == workspaceQueryID
	components.PaintInput(canvas, inputTheme, inputRect, components.InputSnapshot{
		ID: workspaceQueryID, Label: "搜索工作区内容", Value: value, Placeholder: "搜索文件内容", Focused: focused,
		PlatformEditing: focused && value != "", ShowCaret: focused && value == "",
	})
	y := float32(108)
	labeledButton(canvas, workspaceCaseID, "Aa", "区分大小写", ui.Rect{X: 11, Y: y, Width: 31, Height: 25}, search.query.CaseSensitive, palette, func() {
		search.query.CaseSensitive = !search.query.CaseSensitive
		if value != "" {
			state.runWorkspaceSearch()
		}
	})
	labeledButton(canvas, workspaceRegexID, ".*", "正则表达式", ui.Rect{X: 45, Y: y, Width: 31, Height: 25}, search.query.Regex, palette, func() {
		search.query.Regex = !search.query.Regex
		if value != "" {
			state.runWorkspaceSearch()
		}
	})
	labeledButton(canvas, workspaceOnlyID, "M", "仅 Markdown", ui.Rect{X: 79, Y: y, Width: 31, Height: 25}, search.query.MarkdownOnly, palette, func() {
		search.query.MarkdownOnly = !search.query.MarkdownOnly
		if value != "" {
			state.runWorkspaceSearch()
		}
	})
	labeledButton(canvas, workspaceRunID, "⌕", "运行工作区搜索", ui.Rect{X: bounds.Width - 68, Y: y, Width: 28, Height: 25}, false, palette, state.runWorkspaceSearch)
	labeledButton(canvas, workspaceStopID, "×", "取消搜索", ui.Rect{X: bounds.Width - 36, Y: y, Width: 26, Height: 25}, false, palette, func() {
		state.cancelWorkspaceSearch()
		search.error = "搜索已取消"
	})
	canvas.Fill(ui.Rect{X: 7, Y: 139, Width: bounds.Width - 14, Height: 1}, palette.border)
	search.resultsRect = ui.Rect{X: 7, Y: 144, Width: bounds.Width - 14, Height: max(0, bounds.Y+bounds.Height-170)}
	search.resultsHeight = 0
	previous := ""
	for _, hit := range search.results {
		if hit.Path != previous {
			search.resultsHeight += 24
			previous = hit.Path
		}
		search.resultsHeight += 43
	}
	search.scroll = min(search.scroll, max(0, search.resultsHeight-search.resultsRect.Height))
	canvas.Clip(search.resultsRect)
	rowY := search.resultsRect.Y - search.scroll
	previous = ""
	for index, hit := range search.results {
		if hit.Path != previous {
			if rowY+24 >= search.resultsRect.Y && rowY < search.resultsRect.Y+search.resultsRect.Height {
				canvas.Label(ellipsize(canvas, hit.Relative, search.resultsRect.Width-16, 11), ui.Rect{X: 14, Y: rowY, Width: search.resultsRect.Width - 15, Height: 24}, gfx.TextStyle{Size: 11, Weight: gfx.WeightSemibold, Color: palette.muted})
			}
			rowY += 24
			previous = hit.Path
		}
		if rowY+43 >= search.resultsRect.Y && rowY < search.resultsRect.Y+search.resultsRect.Height {
			if canvas.Hovered() == workspaceResultRows.At(index) {
				canvas.Fill(ui.Rect{X: 9, Y: rowY, Width: bounds.Width - 18, Height: 41}, palette.selected)
			}
			caption := "文件名匹配"
			if !hit.FileName {
				caption = strconv.Itoa(hit.Line) + ":" + strconv.Itoa(hit.Column) + "  " + strings.TrimSpace(hit.Preview)
			}
			color := palette.ink
			if workspace.KindOf(hit.Path) == workspace.UnsupportedKind {
				color = palette.muted
			}
			canvas.Label(ellipsize(canvas, caption, bounds.Width-36, 11), ui.Rect{X: 18, Y: rowY + 3, Width: bounds.Width - 36, Height: 33}, gfx.TextStyle{Size: 11, Color: color})
			canvas.HitWithOptions(workspaceResultRows.At(index), ui.Rect{X: 9, Y: rowY, Width: bounds.Width - 18, Height: 41}, caption, gfx.HitOptions{
				Role: gfx.RoleButton, Disabled: workspace.KindOf(hit.Path) == workspace.UnsupportedKind, Hint: "打开第 " + strconv.Itoa(hit.Line) + " 行",
			})
			if canvas.Clicked(workspaceResultRows.At(index)) {
				state.openWorkspaceResult(hit)
			}
		}
		rowY += 43
	}
	canvas.Unclip()
	status := "输入关键词开始搜索"
	switch {
	case search.searching:
		status = "搜索中… " + strconv.Itoa(len(search.results))
	case search.error != "":
		status = search.error
	case len(search.results) > 0:
		status = strconv.Itoa(len(search.results)) + " 个结果"
		if search.truncated {
			status = "前 " + status
		}
	case strings.TrimSpace(value) != "":
		status = "无命中"
	}
	canvas.Label(ellipsize(canvas, status, bounds.Width-26, 11), ui.Rect{X: 14, Y: bounds.Y + bounds.Height - 25, Width: bounds.Width - 26, Height: 22}, gfx.TextStyle{Size: 11, Color: palette.muted})
}
