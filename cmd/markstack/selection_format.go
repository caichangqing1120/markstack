package main

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"markstack/internal/markdownedit"
	"markstack/internal/session"
	"markstack/internal/workspace"
)

var selectionFormatButtons = controlTree.Sub(page, controlTree.Name("selection-format"))

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
	const width, height = 164, 32
	if !ok || clip.Width < width+8 || !contains(clip, ui.Point{X: anchor.X, Y: anchor.Y}) {
		return
	}
	x := max(clip.X+4, min(anchor.X-width/2, clip.X+clip.Width-width-4))
	y := anchor.Y - height - 6
	if y < clip.Y+2 {
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
	for index, option := range []struct{ caption, zh, en, marker string }{
		{"B", "加粗", "Bold", "**"}, {"I", "斜体", "Italic", "*"}, {"</>", "行内代码", "Inline Code", "`"},
		{state.local("链接", "Link"), "链接", "Link", ""},
	} {
		bounds := ui.Rect{X: x + 4 + float32(index)*39, Y: y + 4, Width: 39, Height: 24}
		id := selectionFormatButtons.At(index)
		if canvas.Hovered() == id {
			canvas.Rounded(bounds, 5, palette.selected)
		}
		label := state.local(option.zh, option.en)
		style := gfx.TextStyle{Size: palette.textSize(12), Weight: gfx.WeightMedium, Color: palette.ink, Italic: index == 1}
		if index == 0 {
			style.Weight = gfx.WeightBold
		}
		canvas.LabelCenter(option.caption, bounds, style)
		canvas.HitWithOptions(id, bounds, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: label, SkipFocus: true})
		if canvas.Clicked(id) {
			canvas.AfterPaint(func() { state.applySelectionFormat(focus, selection, option.marker) })
		}
	}
}

func (state *model) applySelectionFormat(focus gfx.ID, selection text.Range, marker string) {
	if state.refuseComposingFormat() {
		return
	}
	if focus == richInputID {
		state.prepareInlineShortcut()
		if marker == "" {
			state.openLinkShortcut()
		} else {
			state.applyWholeBlockStyle(marker)
		}
		return
	}
	current := state.session.Current()
	if marker == "" {
		state.openLinkEditor()
		state.sourceLink = sourceLinkSelection{document: current, selection: selection, source: current.Text()}
		return
	}
	state.wrapSourceSelection(current, selection, marker, "")
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
