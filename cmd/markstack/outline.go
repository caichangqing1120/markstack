package main

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"markstack/internal/icons"

	"markstack/internal/workspace"
)

var (
	outlineFilterID = controlTree.ID(page, "outline-filter")
	outlineClearID  = controlTree.ID(page, "outline-filter-clear")
	outlineFolds    = controlTree.Sub(page, controlTree.Name("outline-folds"))
)

type outlinePanel struct {
	documentID                    int
	source                        string
	entries                       []outlineEntry
	parsed                        markdown.Document
	anchors                       map[string]int
	filter                        *text.Editor
	input                         app.TextInput
	filterRect, rowsRect          ui.Rect
	query                         string
	rows                          []int
	indexDirty                    bool
	folded                        map[string]bool
	scroll                        float32
	active                        int
	caret                         outlineCaret
	followViewport, followPreview bool
}

type outlineCaret struct {
	mode                   viewMode
	block, caret, position int
}

func outlineOf(source string) []outlineEntry {
	var parsed markdown.Document
	parseTechnicalMarkdown(&parsed, source, false)
	return outlineFrom(source, &parsed)
}

func outlineFrom(source string, parsed *markdown.Document) []outlineEntry {
	var entries []outlineEntry
	used := make(map[string]int)
	byteOffset, runeOffset := 0, 0
	for index := range parsed.Blocks {
		block := &parsed.Blocks[index]
		if block.Kind != markdown.Heading {
			continue
		}
		runeOffset += utf8.RuneCountInString(source[byteOffset:block.Start])
		byteOffset = block.Start
		var title strings.Builder
		for _, span := range parsed.Spans(block) {
			title.WriteString(span.Text)
		}
		base := headingSlug(title.String())
		anchor := base
		if suffix, exists := used[base]; exists {
			for {
				suffix++
				anchor = base + "-" + strconv.Itoa(suffix)
				if _, taken := used[anchor]; !taken {
					break
				}
			}
			used[base] = suffix
		}
		used[anchor] = 0
		entries = append(entries, outlineEntry{Title: title.String(), Level: block.Level, Rune: runeOffset, start: block.Start, anchor: anchor})
	}
	return entries
}

func headingSlug(title string) string {
	var slug strings.Builder
	for _, character := range strings.TrimSpace(title) {
		switch {
		case unicode.IsSpace(character):
			slug.WriteByte('-')
		case character == '-' || character == '_' || unicode.IsLetter(character) || unicode.IsNumber(character) || unicode.IsMark(character):
			slug.WriteRune(unicode.ToLower(character))
		}
	}
	return slug.String()
}

func (panel *outlinePanel) index(documentID int, source string) {
	if panel.documentID == documentID && panel.source == source {
		return
	}
	if panel.documentID != documentID {
		panel.folded, panel.scroll = make(map[string]bool), 0
		panel.active = -1
		panel.followViewport = false
	}
	panel.documentID, panel.source = documentID, source
	panel.followViewport = false
	parseTechnicalMarkdown(&panel.parsed, source, false)
	panel.entries = outlineFrom(source, &panel.parsed)
	panel.anchors = make(map[string]int, len(panel.entries))
	for index, entry := range panel.entries {
		panel.anchors[entry.anchor] = index
	}
	panel.indexDirty = true
}

func (panel *outlinePanel) visibleRows(query string) []int {
	query = strings.ToLower(strings.TrimSpace(query))
	if panel.query == query && !panel.indexDirty {
		return panel.rows
	}
	panel.query, panel.indexDirty = query, false
	panel.rows = panel.rows[:0]
	hiddenBelow := 7
	for index, entry := range panel.entries {
		if query != "" {
			if strings.Contains(strings.ToLower(entry.Title), query) {
				panel.rows = append(panel.rows, index)
			}
			continue
		}
		if entry.Level > hiddenBelow {
			continue
		}
		hiddenBelow = 7
		panel.rows = append(panel.rows, index)
		if panel.folded[entry.anchor] {
			hiddenBelow = entry.Level
		}
	}
	return panel.rows
}

func (state *model) outlinePosition() int {
	panel := &state.outlinePanel
	current := state.session.Current()
	state.stats.update(current.Editor.CommittedString())
	caret := outlineCaret{mode: state.mode, block: -1, caret: current.Editor.Caret()}
	caret.position = state.stats.byteAt(caret.caret)
	if state.mode == renderedMode {
		switch {
		case state.rich.active:
			caret.block, caret.caret = state.rich.index, state.rich.input.Caret()
			if state.virtualParagraphNeedsPlaceholder() {
				caret.position = state.rich.virtualStart
			}
		case state.codeBlock.active:
			caret.block, caret.caret = state.codeBlock.renderedIndex, state.codeBlock.input.Caret()
		case state.table.active:
			caret.block, caret.caret = state.table.renderedIndex, state.table.input.Caret()
		case state.selection.active:
			caret.block, caret.caret = state.selection.extentIndex, state.selection.lastOffset
		}
		if caret.block >= 0 && caret.block < len(state.parsed.Blocks) && !state.virtualParagraphNeedsPlaceholder() {
			caret.position = state.parsed.Blocks[caret.block].Start
		}
	}
	if caret != panel.caret {
		panel.caret = caret
		panel.followViewport = false
	}
	if state.mode == readMode || panel.followViewport && panel.followPreview {
		if len(state.preview.blocks) > 0 {
			top := state.previewScroll + 8
			index := sort.Search(len(state.preview.blocks), func(index int) bool { return state.preview.blocks[index].top > top }) - 1
			if index >= 0 && index < len(state.parsed.Blocks) {
				return state.parsed.Blocks[index].Start
			}
			if state.parsed.Blocks[0].Kind == markdown.Heading && state.preview.blocks[0].top < state.previewRect.Height-46 {
				return state.parsed.Blocks[0].Start
			}
			return -1
		}
	} else if panel.followViewport {
		if offset, ok := state.source.OffsetAtY(state.sourceWritingScroll() + 8); ok {
			return offset
		}
	}
	return caret.position
}

func (state *model) paintOutline(canvas *gfx.Canvas, theme ui.Theme, palette colors, bounds ui.Rect) {
	current := state.session.Current()
	if current == nil || current.ReadOnly() {
		state.outlines = nil
		return
	}
	panel := &state.outlinePanel
	active := -1
	var rows []int
	top := bounds.Y + 40
	switch current.Kind {
	case workspace.MarkdownKind:
		panel.index(current.ID, current.Editor.CommittedString())
		state.outlines = panel.entries
		position := state.outlinePosition()
		active = sort.Search(len(panel.entries), func(index int) bool { return panel.entries[index].start > position }) - 1
		query := panel.filter.String()
		filterWidth := bounds.Width - 20
		if query != "" {
			filterWidth = bounds.Width - 46
		}
		panel.filterRect = ui.Rect{X: bounds.X + 10, Y: bounds.Y + 36, Width: max(40, filterWidth), Height: 27}
		focused := canvas.Focused() == outlineFilterID
		theme.Metrics.FontSizeM = 12
		components.PaintInput(canvas, theme, panel.filterRect, components.InputSnapshot{
			ID: outlineFilterID, Label: "筛选大纲标题", Value: query, Placeholder: state.local("筛选标题", "Filter headings"), Focused: focused,
			PlatformEditing: focused && query != "", ShowCaret: focused && query == "",
		})
		if query != "" {
			iconButton(canvas, outlineClearID, icons.X, "", "清空标题筛选", ui.Rect{X: bounds.X + bounds.Width - 32, Y: bounds.Y + 36, Width: 24, Height: 27}, false, false, palette, func() {
				panel.filter = text.NewEditor("")
				panel.scroll = 0
				state.focusRequest = outlineFilterID
			})
		}
		if strings.ToLower(strings.TrimSpace(query)) != panel.query {
			panel.scroll = 0
		}
		rows = panel.visibleRows(query)
		top = bounds.Y + 71
	case workspace.RichHTMLKind:
		state.outlines = state.richHTMLHeadingEntries()
		canvas.Clip(ui.Rect{X: bounds.X + 6, Y: top, Width: bounds.Width - 12, Height: bounds.Height - 40})
		for index, entry := range state.outlines {
			row := ui.Rect{X: bounds.X + 7, Y: top + 5 + float32(index)*30, Width: bounds.Width - 14, Height: 27}
			state.paintOutlineRow(canvas, palette, row, index, entry, false, false)
		}
		canvas.Unclip()
		return
	default:
		state.outlines = nil
		return
	}
	const rowHeight = 28
	panel.rowsRect = ui.Rect{X: bounds.X + 6, Y: top, Width: bounds.Width - 12, Height: max(0, bounds.Y+bounds.Height-top)}
	if active != panel.active {
		panel.active = active
		row := sort.SearchInts(rows, active)
		if row < len(rows) && rows[row] == active {
			panel.scroll = min(panel.scroll, float32(row*rowHeight))
			panel.scroll = max(panel.scroll, float32((row+1)*rowHeight)-panel.rowsRect.Height)
		}
	}
	panel.scroll = max(0, min(panel.scroll, float32(len(rows)*rowHeight)-panel.rowsRect.Height))
	marked := sort.SearchInts(rows, active)
	if panel.query == "" && marked > 0 && (marked == len(rows) || rows[marked] != active) {
		active = rows[marked-1]
	}
	canvas.Clip(panel.rowsRect)
	defer canvas.Unclip()
	for rowIndex := int(panel.scroll / rowHeight); rowIndex < len(rows); rowIndex++ {
		index := rows[rowIndex]
		row := ui.Rect{X: panel.rowsRect.X + 1, Y: top + float32(rowIndex*rowHeight) - panel.scroll, Width: panel.rowsRect.Width - 2, Height: rowHeight - 1}
		if row.Y >= top+panel.rowsRect.Height {
			break
		}
		entry := state.outlines[index]
		foldable := panel.query == "" && index+1 < len(state.outlines) && state.outlines[index+1].Level > entry.Level
		state.paintOutlineRow(canvas, palette, row, index, entry, index == active, foldable)
	}
	if len(rows) == 0 && panel.query != "" {
		canvas.LabelCenter(state.local("没有匹配的标题", "No matching headings"), ui.Rect{X: panel.rowsRect.X, Y: panel.rowsRect.Y + 24, Width: panel.rowsRect.Width, Height: 24}, gfx.TextStyle{Size: 12, Color: palette.muted})
	} else if len(rows) == 0 && panel.rowsRect.Height >= 90 {
		centerX := panel.rowsRect.X + panel.rowsRect.Width/2
		emptyY := panel.rowsRect.Y + min(float32(56), panel.rowsRect.Height*0.25)
		canvas.DrawIcon(icons.ListTree, ui.Rect{X: centerX - 11, Y: emptyY, Width: 22, Height: 22}, palette.muted.WithAlpha(0.45))
		canvas.LabelCenter(state.local("暂无大纲", "No headings yet"), ui.Rect{X: panel.rowsRect.X + 8, Y: emptyY + 28, Width: panel.rowsRect.Width - 16, Height: 20}, gfx.TextStyle{Size: 12, Weight: gfx.WeightMedium, Color: palette.muted})
		canvas.LabelCenter(state.local("输入 # 标题 自动生成", "Type # Heading to build"), ui.Rect{X: panel.rowsRect.X + 8, Y: emptyY + 48, Width: panel.rowsRect.Width - 16, Height: 18}, gfx.TextStyle{Size: 11, Color: palette.muted.WithAlpha(0.75)})
	}
}

func (state *model) paintOutlineRow(canvas *gfx.Canvas, palette colors, row ui.Rect, index int, entry outlineEntry, active, foldable bool) {
	left := row.X + 8 + float32(entry.Level-1)*9
	if state.session.Current().Kind != workspace.MarkdownKind {
		left = row.X + 8 + float32(entry.Level-1)*10
	}
	if active {
		canvas.Rounded(row, 4, palette.selected)
		canvas.Fill(ui.Rect{X: row.X, Y: row.Y + 5, Width: 2, Height: row.Height - 10}, palette.accent)
	}
	if foldable {
		panel := &state.outlinePanel
		folded := panel.folded[entry.anchor]
		label, angle := "折叠 ", float32(math.Pi/2)
		if folded {
			label, angle = "展开 ", 0
		}
		id := outlineFolds.At(index)
		button := ui.Rect{X: left - 4, Y: row.Y + 2, Width: 19, Height: row.Height - 4}
		if canvas.Hovered() == id || canvas.FocusVisible() == id {
			canvas.Rounded(button, 4, palette.selected)
		}
		icon := ui.Rect{X: button.X + 3, Y: button.Y + (button.Height-13)/2, Width: 13, Height: 13}
		canvas.DrawIconTransformed(icons.ChevronRight, icon, palette.muted, gfx.IconTransform{Rotate: angle})
		canvas.HitWithOptions(id, button, label+entry.Title, gfx.HitOptions{Role: gfx.RoleButton, Expanded: !folded})
		if canvas.Clicked(id) {
			panel.folded[entry.anchor] = !folded
			panel.indexDirty = true
			state.invalidator.Invalidate()
		}
	}
	if state.session.Current().Kind == workspace.MarkdownKind {
		left += 18
	}
	labelBounds := ui.Rect{X: left, Y: row.Y, Width: max(0, row.X+row.Width-left-5), Height: row.Height}
	canvas.Label(ellipsize(canvas, entry.Title, labelBounds.Width, 12), labelBounds, gfx.TextStyle{Size: 12, Color: palette.ink})
	hit := row
	if foldable {
		hit.X, hit.Width = left-2, max(0, row.X+row.Width-left+2)
	}
	canvas.HitWithOptions(outlineRows.At(index), hit, entry.Title, gfx.HitOptions{Role: gfx.RoleButton, Selected: active})
	if canvas.Clicked(outlineRows.At(index)) {
		state.navigateOutline(index)
		if state.mode == editMode {
			state.deferEditingFocus()
		}
	}
}
