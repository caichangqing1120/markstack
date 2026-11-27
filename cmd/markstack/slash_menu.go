package main

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"markstack/internal/markdownedit"
)

var (
	slashItems = controlTree.Sub(page, controlTree.Name("slash-items"))
	slashSize  = controlTree.Sub(page, controlTree.Name("slash-table-size"))
)

type slashMenu struct {
	documentID, index, start int
	selected, rows, columns  int
	query                    string
	typed                    bool
}

var slashCommands = [...]struct{ label, keywords string }{
	{"表格", "table"}, {"代码块", "code fence"}, {"引用", "quote"},
	{"任务列表", "task todo checklist"}, {"水平分割线", "rule horizontal divider"}, {"本地图片", "image picture photo"},
}

func (state *model) startSlashMenu(shortcut bool) gfx.EventResult {
	start, _, ok := state.m2ParagraphRange()
	if !ok || state.pendingCompositionFocus() != (gfx.ID{}) {
		return gfx.EventResult{}
	}
	value := state.rich.input.String()
	if !shortcut && value != "/" {
		return gfx.EventResult{}
	}
	state.slash = slashMenu{documentID: state.rich.documentID, index: state.rich.index, start: start, rows: 2, columns: 2, typed: value == "/"}
	state.headingMenuOpen = false
	state.invalidator.Invalidate()
	return gfx.EventResult{Handled: true, Repaint: true, Focus: richInputID}
}

func (state *model) slashMatches() []int {
	menu := &state.slash
	start, _, ok := state.m2ParagraphRange()
	if menu.documentID == 0 || !ok || menu.documentID != state.rich.documentID || menu.index != state.rich.index || menu.start != start {
		state.slash = slashMenu{}
		return nil
	}
	query := menu.query
	if menu.typed {
		var found bool
		query, found = strings.CutPrefix(state.rich.input.CommittedString(), "/")
		if !found {
			state.slash = slashMenu{}
			return nil
		}
	}
	query = strings.ToLower(strings.TrimSpace(query))
	if query != menu.query && menu.typed {
		menu.selected, menu.query = 0, query
	}
	var matches []int
	for index, command := range slashCommands {
		if strings.Contains(command.label, query) || strings.Contains(command.keywords, query) {
			matches = append(matches, index)
		}
	}
	menu.selected = min(menu.selected, max(0, len(matches)-1))
	return matches
}

func (state *model) onSlashKey(event gfx.KeyEvent) gfx.EventResult {
	matches := state.slashMatches()
	menu := &state.slash
	if menu.documentID == 0 || event.Target != richInputID || state.pendingCompositionFocus() != (gfx.ID{}) {
		return gfx.EventResult{}
	}
	switch event.Key {
	case gfx.KeyTab:
		state.slash = slashMenu{}
		return gfx.EventResult{Repaint: true}
	case gfx.KeyEscape:
		state.slash = slashMenu{}
	case gfx.KeyEnter:
		if len(matches) > 0 {
			return state.insertSlashCommand(matches[menu.selected])
		}
	case gfx.KeyUp, gfx.KeyDown, gfx.KeyLeft, gfx.KeyRight:
		if len(matches) == 0 {
			break
		}
		delta := 1
		if event.Key == gfx.KeyUp || event.Key == gfx.KeyLeft {
			delta = -1
		}
		if matches[menu.selected] == 0 && (event.Key == gfx.KeyLeft || event.Key == gfx.KeyRight) {
			menu.columns = min(12, max(1, menu.columns+delta))
		} else if matches[menu.selected] == 0 && event.Modifiers == gfx.ModifierShift {
			menu.rows = min(20, max(1, menu.rows+delta))
		} else if event.Key == gfx.KeyUp || event.Key == gfx.KeyDown {
			menu.selected = (menu.selected + delta + len(matches)) % len(matches)
		}
	default:
		return gfx.EventResult{}
	}
	return gfx.EventResult{Handled: true, Repaint: true, Focus: richInputID}
}

func (state *model) slashTextEvent(event app.TextEvent) bool {
	if state.slash.documentID == 0 || state.slash.typed {
		return false
	}
	menu := &state.slash
	if event.Kind == app.TextInsert && !event.HasReplacement {
		if !strings.ContainsAny(event.Text, "\r\n") && len(menu.query)+len(event.Text) <= 120 {
			menu.query += event.Text
			menu.selected = 0
		}
		return true
	}
	if event.Kind == app.TextCommandEvent && event.Command == app.TextDeleteBackward {
		_, size := utf8.DecodeLastRuneInString(menu.query)
		menu.query = menu.query[:len(menu.query)-size]
		menu.selected = 0
		return true
	}
	return false
}

func (state *model) insertSlashCommand(command int) gfx.EventResult {
	result := gfx.EventResult{Handled: true, Repaint: true, Focus: richInputID}
	start, end, ok := state.m2ParagraphRange()
	if !ok || state.pendingCompositionFocus() != (gfx.ID{}) {
		return result
	}
	current, menu := state.session.Current(), state.slash
	newline := current.Newline()
	prefix := ""
	index := state.rich.index
	if !menu.typed && state.rich.input.String() != "" {
		start, prefix, index = end, newline+newline, index+1
	}
	var replacement string
	var target *renderedVirtualState
	caret := 0
	switch command {
	case 0:
		replacement = slashTable(menu.rows, menu.columns, newline)
	case 1:
		replacement = "```" + newline + newline + "```"
	case 2, 3:
		kind := markdownedit.Quote
		replacement = "> "
		if command == 3 {
			kind, replacement = markdownedit.ListItem, "- [ ] "
		}
		position := start + len(prefix) + len(replacement)
		target = &renderedVirtualState{kind: kind, start: position, end: position, insert: replacement}
	case 4, 5:
		replacement = "---"
		if command == 5 {
			path, err := state.pickMarkdownImage(current)
			if err != nil {
				state.message = err.Error()
				return result
			}
			if path == "" {
				return result
			}
			replacement = "![图片](" + path + ")"
		}
		replacement += newline + newline
		position := start + len(prefix) + len(replacement)
		target = &renderedVirtualState{kind: markdownedit.Paragraph, start: position, end: position, insert: newline + newline}
		index++
	default:
		return result
	}
	state.slash = slashMenu{}
	if state.applyM2Patch(start, end, prefix+replacement, index, caret, state.rich.input.Caret(), target) {
		result.Focus = state.focusRequest
	}
	return result
}

func slashTable(rows, columns int, newline string) string {
	var table strings.Builder
	for row := 0; row < rows+1; row++ {
		if row > 0 {
			table.WriteString(newline)
		}
		table.WriteByte('|')
		for column := range columns {
			switch row {
			case 0:
				fmt.Fprintf(&table, " 列 %d |", column+1)
			case 1:
				table.WriteString(" --- |")
			default:
				table.WriteString("   |")
			}
		}
	}
	return table.String()
}

func (state *model) paintSlashMenu(canvas *gfx.Canvas, palette colors, bounds ui.Rect) {
	if state.slash.documentID == 0 {
		return
	}
	matches := state.slashMatches()
	if state.slash.documentID == 0 || state.rich.rect.Width <= 0 {
		state.slash = slashMenu{}
		return
	}
	menu := &state.slash
	width, height := min(float32(244), bounds.Width-12), float32(max(1, len(matches))*28+10)
	tableSelected := len(matches) > 0 && matches[menu.selected] == 0
	if tableSelected {
		height += 29
	}
	y := state.rich.rect.Y + state.rich.rect.Height + 4
	if y+height > bounds.Y+bounds.Height {
		y = max(bounds.Y, state.rich.rect.Y-height-4)
	}
	box := ui.Rect{X: min(state.rich.rect.X, bounds.X+bounds.Width-width-6), Y: y, Width: width, Height: height}
	canvas.PushLayer(8)
	defer canvas.PopLayer()
	canvas.RoundedBorder(box, 5, 1, palette.border, palette.background)
	if len(matches) == 0 {
		canvas.Label("无匹配项", ui.Rect{X: box.X + 9, Y: box.Y + 5, Width: width - 18, Height: 28}, gfx.TextStyle{Size: 13, Color: palette.muted})
	}
	for row, command := range matches {
		button(canvas, slashItems.At(command), slashCommands[command].label, ui.Rect{X: box.X + 5, Y: box.Y + 5 + float32(row)*28, Width: width - 10, Height: 27}, row == menu.selected, palette, func() {
			state.insertSlashCommand(command)
		})
	}
	if tableSelected {
		line := box.Y + height - 32
		for axis, field := range []*int{&menu.rows, &menu.columns} {
			left := box.X + 5 + float32(axis)*width/2
			label, limit := "行", 20
			if axis == 1 {
				label, limit = "列", 12
			}
			canvas.LabelCenter(strconv.Itoa(*field)+label, ui.Rect{X: left + 24, Y: line, Width: width/2 - 56, Height: 26}, gfx.TextStyle{Size: 12, Color: palette.ink})
			labeledButton(canvas, slashSize.At(2*axis), "−", "减少"+label, ui.Rect{X: left, Y: line, Width: 24, Height: 26}, false, palette, func() {
				*field = max(1, *field-1)
				state.focusRequest = richInputID
			})
			labeledButton(canvas, slashSize.At(2*axis+1), "+", "增加"+label, ui.Rect{X: left + width/2 - 32, Y: line, Width: 24, Height: 26}, false, palette, func() {
				*field = min(limit, *field+1)
				state.focusRequest = richInputID
			})
		}
	}
}
