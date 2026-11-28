package main

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"markstack/internal/markdownedit"
	"markstack/internal/session"
	"markstack/internal/workspace"
)

var selectionFormatButtons = controlTree.Sub(page, controlTree.Name("selection-format"))

var selectionFormatOptions = [...]struct {
	zh, en, caption string
	command         formatCommand
}{
	{"加粗", "Bold", "B", cmdBold}, {"下划线", "Underline", "U", cmdUnderline},
	{"斜体", "Italic", "I", cmdItalic}, {"删除线", "Strikethrough", "S", cmdStrike},
	{"高亮", "Highlight", "==", cmdHighlight}, {"行内代码", "Inline Code", "<>", cmdInlineCode},
	{"链接", "Link", "", cmdLink}, {"清除", "Clear", "", cmdClear},
}

type sourceLinkSelection struct {
	document  *session.Document
	selection text.Range
	source    string
	context   *formatContext
}

func (state *model) paintSelectionFormat(canvas *gfx.Canvas, palette colors) {
	current := state.session.Current()
	if current == nil || current.Kind != workspace.MarkdownKind || current.ReadOnly() ||
		state.palette.query != nil || state.fileMenuOpen || !state.formatMenu.trigger.IsZero() || state.recentOpen || state.settingsOpen || state.session.Pending() != nil ||
		state.findOpen || state.headingMenuOpen || state.linkOpen || state.slash.documentID != 0 || state.imageInspector.open ||
		state.fileAction != fileActionNone || state.mediaManager.open || state.diskWatch.confirm != nil || !state.pendingFormatCompositionFocus().IsZero() {
		return
	}
	focus := canvas.Focused()
	if pressed := canvas.Pressed(); pressed == sourceID || pressed == richInputID {
		return
	}
	var selection text.Range
	var anchor, clip ui.Rect
	var ok bool
	switch focus {
	case sourceID:
		if state.mode != editMode && state.mode != splitMode {
			return
		}
		selection = current.Editor.Selection()
		if selection.Start == selection.End {
			return
		}
		value := current.Text()
		selected := strings.TrimSpace(value[byteOffsetOfRune(value, selection.Start):byteOffsetOfRune(value, selection.End)])
		if selected == "" || strings.ContainsAny(selected, "\r\n") {
			return
		}
		clip = state.sourceRect
		anchor, ok = state.source.CaretBounds()
	case richInputID:
		rich := &state.rich
		if state.mode != renderedMode || !rich.active || rich.input == nil || state.selection.active || rich.block.Kind != markdownedit.Paragraph || rich.virtualLiteral {
			return
		}
		selection = rich.input.Selection()
		if selection.Start == selection.End {
			return
		}
		clip = state.previewRect
		value := rich.input.String()
		anchor, ok = rich.caretBoundsAt(byteOffsetOfRune(value, selection.Start), false)
		if end, exists := rich.CaretBounds(); exists && (!contains(clip, ui.Point{X: anchor.X, Y: anchor.Y}) || end.Y == anchor.Y) {
			if end.Y == anchor.Y {
				anchor.X = (anchor.X + end.X) / 2
			} else {
				anchor, ok = end, true
			}
		}
	default:
		return
	}
	const height = 32
	width := min(float32(352), clip.Width-8)
	if !ok || width < 200 || !contains(clip, ui.Point{X: anchor.X, Y: anchor.Y}) {
		return
	}
	x := max(clip.X+4, min(anchor.X-width/2, clip.X+clip.Width-width-4))
	y := anchor.Y - height - 6
	coveringEditor := false
	if focus == richInputID && state.rich.index > 0 && state.rich.index < len(state.preview.blocks) {
		previous := &state.preview.blocks[state.rich.index-1]
		bottom := clip.Y + 46 - state.previewScroll + previous.top + previous.height
		coveringEditor = y < bottom && (previous.kind == markdown.Code || previous.kind == markdown.Table || previous.card.label != "")
	}
	if y < clip.Y+2 || coveringEditor {
		y = anchor.Y + anchor.Height + 6
	}
	if y+height > clip.Y+clip.Height {
		return
	}
	box := ui.Rect{X: x, Y: y, Width: width, Height: height}
	canvas.PushLayer(7)
	defer canvas.PopLayer()
	canvas.Elevate(box, 10, gfx.ShadowSM)
	canvas.RoundedBorder(box, 10, 1, palette.border, palette.background)
	canvas.Block(box)
	buttonWidth := (width - 8) / float32(len(selectionFormatOptions))
	for index, option := range selectionFormatOptions {
		bounds := ui.Rect{X: x + 4 + float32(index)*buttonWidth, Y: y + 4, Width: buttonWidth, Height: 24}
		id := selectionFormatButtons.At(index)
		if canvas.Hovered() == id {
			canvas.Rounded(bounds, 5, palette.selected)
		}
		label := state.local(option.zh, option.en)
		caption := option.caption
		if caption == "" {
			caption = label
		}
		if option.command == cmdClear {
			shortcut := "Ctrl+\\"
			if primaryModifier() == gfx.ModifierMeta {
				shortcut = "⌘\\"
			}
			label = state.local("清除格式", "Clear formatting") + " (" + shortcut + ")"
		}
		style := gfx.TextStyle{Size: palette.textSize(12), Weight: gfx.WeightMedium, Color: palette.ink, Italic: option.command == cmdItalic}
		if option.command == cmdBold {
			style.Weight = gfx.WeightBold
		}
		canvas.LabelCenter(caption, bounds, style)
		if option.command == cmdUnderline || option.command == cmdStrike {
			lineY := bounds.Y + bounds.Height*.76
			if option.command == cmdStrike {
				lineY = bounds.Y + bounds.Height*.51
			}
			canvas.Fill(ui.Rect{X: bounds.X + bounds.Width/2 - 4, Y: lineY, Width: 8, Height: 1}, style.Color)
		}
		canvas.HitWithOptions(id, bounds, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: label, SkipFocus: true})
		if canvas.Clicked(id) {
			canvas.AfterPaint(func() { state.runFormat(option.command) })
		}
	}
}

func (state *model) wrapSourceSelection(current *session.Document, selection text.Range, marker, destination string) bool {
	source := current.Text()
	start, end := byteOffsetOfRune(source, selection.Start), byteOffsetOfRune(source, selection.End)
	selected := source[start:end]
	left := strings.TrimLeftFunc(selected, unicode.IsSpace)
	value := strings.TrimRightFunc(left, unicode.IsSpace)
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return false
	}
	start += len(selected) - len(left)
	end = start + len(value)
	prefix, suffix := marker, marker
	if marker == "`" {
		if start > 0 && source[start-1] == '`' || end < len(source) && source[end] == '`' {
			return false
		}
		for strings.Contains(value, prefix) {
			prefix += "`"
		}
		suffix = prefix
		if strings.HasPrefix(value, "`") || strings.HasSuffix(value, "`") {
			prefix += " "
			suffix = " " + suffix
		}
	} else if destination != "" {
		prefix, suffix = "[", "]("+destination+")"
		value = strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`).Replace(value)
	}
	from := utf8.RuneCountInString(source[:start]) + len(prefix)
	before, after := selection, text.Range{Start: from, End: from + utf8.RuneCountInString(value)}
	if current.Editor.Caret() == selection.Start {
		before.Start, before.End = before.End, before.Start
		after.Start, after.End = after.End, after.Start
	}
	if err := current.EditRange(start, end, prefix+value+suffix, session.RangeEditSelection{Before: before, After: after}); err != nil {
		state.message = err.Error()
		return false
	}
	state.focusRequest = sourceID
	state.invalidator.Invalidate()
	return true
}
