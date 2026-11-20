package main

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"markstack/internal/markdownedit"
	"markstack/internal/workspace"
)

const maxDocumentMatches = 10000

type renderedMatch struct {
	block      int
	start, end int
}

func (state *model) openDocumentFind() {
	if current := state.session.Current(); current != nil && (current.Kind == workspace.CodeKind || current.Kind == workspace.PlainKind) {
		selection := current.Editor.Selection()
		value := current.Text()
		state.findScope = text.Span{Start: byteOffsetOfRune(value, selection.Start), End: byteOffsetOfRune(value, selection.End)}
		state.findSelectedOnly = false
	}
	state.findOpen = true
	state.headingMenuOpen = false
	if state.mode == splitMode || state.mode == readMode && state.session.Current().Kind != workspace.RichHTMLKind {
		state.mode = editMode
	}
	state.focusRequest = findInputID
}

func (state *model) sourceReplaceAvailable() bool {
	current := state.session.Current()
	return current != nil && state.mode == editMode && (current.Kind == workspace.CodeKind || current.Kind == workspace.PlainKind)
}

func (state *model) syncDocumentFind() {
	if !state.findOpen {
		return
	}
	current := state.session.Current()
	if current == nil {
		return
	}
	query, source := state.findEditor.String(), current.Text()
	if state.findDeferred && state.pendingCompositionFocus() == (gfx.ID{}) {
		// The last answer skipped a source fallback because a composition owned
		// the keyboard. With the composition finished, answer the query fully.
		state.findDeferred = false
		state.findDocument = 0
	}
	if state.findDocument == current.ID && state.findQuery == query && state.findSource == source && state.findMode == state.mode &&
		state.findAppliedRegex == state.findRegex && state.findAppliedCase == state.findCaseSensitive && state.findAppliedScope == state.findSelectedOnly {
		if state.findPositionDeferred {
			state.selectDocumentMatch()
		}
		return
	}
	state.findDocument, state.findQuery, state.findSource = current.ID, query, source
	state.findAppliedRegex, state.findAppliedCase, state.findAppliedScope = state.findRegex, state.findCaseSensitive, state.findSelectedOnly
	state.findIndex = 0
	state.findMatches = state.findMatches[:0]
	state.findEnds = state.findEnds[:0]
	state.findVisual = state.findVisual[:0]
	if state.findError != "" && state.message == state.findError {
		state.message = ""
	}
	state.findError = ""
	state.findDeferred = false
	state.findPositionDeferred = false
	if query == "" {
		state.findMode = state.mode
		return
	}
	if state.sourceReplaceAvailable() {
		pattern := query
		if !state.findRegex {
			pattern = regexp.QuoteMeta(pattern)
		}
		if !state.findCaseSensitive {
			pattern = "(?i)" + pattern
		}
		matcher, err := regexp.Compile(pattern)
		if err != nil {
			state.findError = "正则表达式无效: " + err.Error()
			state.message = state.findError
			state.findMode = editMode
			return
		}
		indices := matcher.FindAllStringIndex(source, maxDocumentMatches+1)
		if len(indices) > maxDocumentMatches {
			state.findError = "匹配过多，请缩小搜索范围"
			state.message = state.findError
			state.findMode = editMode
			return
		}
		for _, pair := range indices {
			if pair[0] == pair[1] || state.findSelectedOnly && (pair[0] < state.findScope.Start || pair[1] > state.findScope.End) {
				continue
			}
			state.findMatches = append(state.findMatches, pair[0])
			state.findEnds = append(state.findEnds, pair[1])
		}
		state.findMode = editMode
		state.selectDocumentMatch()
		return
	}
	if current.Kind == workspace.RichHTMLKind && state.mode != editMode {
		state.html.sync(current)
		for index, block := range state.html.parsed.Blocks {
			for offset := 0; offset <= len(block.Visible)-len(query); {
				found := strings.Index(block.Visible[offset:], query)
				if found < 0 {
					break
				}
				offset += found
				state.findMatches = append(state.findMatches, len(state.findMatches))
				state.findVisual = append(state.findVisual, renderedMatch{block: index, start: offset, end: offset + len(query)})
				offset += len(query)
			}
		}
		state.findMode = state.mode
		state.selectDocumentMatch()
		return
	}
	if state.mode == renderedMode {
		state.parsed.Parse(source)
		blocks := markdownedit.Parse([]byte(source))
		mapping := renderedMapping(&state.parsed, blocks)
		complexMatch := false
		for index := range state.parsed.Blocks {
			value := renderedPlain(&state.parsed, index)
			sourceIndex := mapping[index]
			for offset := 0; offset <= len(value)-len(query); {
				found := strings.Index(value[offset:], query)
				if found < 0 {
					break
				}
				offset += found
				if sourceIndex < 0 || source[blocks[sourceIndex].Start:blocks[sourceIndex].End] != value {
					complexMatch = true
					break
				}
				state.findMatches = append(state.findMatches, len(state.findMatches))
				state.findVisual = append(state.findVisual, renderedMatch{block: index, start: offset, end: offset + len(query)})
				offset += len(query)
			}
			if complexMatch {
				break
			}
		}
		if !complexMatch {
			state.findMode = renderedMode
			state.selectDocumentMatch()
			return
		}
		// A complex match normally falls back to the source view. That switch
		// would clear a pending composition, so keep the rendered editor and
		// answer this query with the safely rendered matches instead.
		if state.pendingCompositionFocus() != (gfx.ID{}) {
			state.findDeferred = true
			state.findMode = renderedMode
			state.selectDocumentMatch()
			return
		}
		state.mode = editMode
		state.findMatches = state.findMatches[:0]
		state.findVisual = state.findVisual[:0]
	}
	state.findMode = editMode
	for offset := 0; offset <= len(source)-len(query); {
		found := strings.Index(source[offset:], query)
		if found < 0 {
			break
		}
		offset += found
		state.findMatches = append(state.findMatches, offset)
		offset += len(query)
	}
	state.selectDocumentMatch()
}

// closeDocumentFind closes the find bar and reports which control owns the
// next keystroke. A rendered match becomes the active native block so typing
// continues at the jump target instead of being dropped on an invisible view.
func (state *model) closeDocumentFind() gfx.ID {
	state.syncDocumentFind()
	state.findOpen = false
	if focus := state.pendingCompositionFocus(); focus != (gfx.ID{}) {
		return focus
	}
	current := state.session.Current()
	if current == nil {
		return gfx.ID{}
	}
	if current.Kind == workspace.MarkdownKind && state.mode == renderedMode && state.findMode == renderedMode && len(state.findVisual) > 0 {
		index := state.findIndex
		if index < 0 || index >= len(state.findVisual) {
			index = 0
		}
		match := state.findVisual[index]
		result := state.activateRendered(match.block)
		if state.rich.active {
			value := state.rich.input.String()
			if match.start >= 0 && match.end >= match.start && match.end <= len(value) {
				state.rich.input.SetSelection(text.Range{
					Start: utf8.RuneCountInString(value[:match.start]), End: utf8.RuneCountInString(value[:match.end]),
				})
			}
		}
		if result.Focus != (gfx.ID{}) {
			return result.Focus
		}
	}
	if state.mode == editMode || state.mode == splitMode {
		return sourceID
	}
	if state.mode == renderedMode {
		focus, blockIndex := gfx.ID{}, -1
		switch {
		case current.Kind == workspace.RichHTMLKind && state.html.active:
			if state.html.blockIndex >= 0 && state.html.blockIndex < len(state.html.blockY) && state.html.rect.Height > 0 {
				top := state.html.blockY[state.html.blockIndex]
				if top-state.html.scroll < 42 || top-state.html.scroll > state.html.rect.Height-60 {
					state.html.scroll = max(0, min(top-80, max(0, state.html.height-state.html.rect.Height)))
				}
			}
			return htmlInputID
		case state.rich.active:
			focus, blockIndex = richInputID, state.rich.index
		case state.codeBlock.active:
			focus, blockIndex = codeInputID, state.codeBlock.renderedIndex
		case state.table.active:
			focus, blockIndex = tableInputID, state.table.renderedIndex
		}
		if blockIndex >= 0 && blockIndex < len(state.preview.blocks) && state.previewRect.Height > 0 {
			top := state.preview.blocks[blockIndex].top + 46
			if top-state.previewScroll < 42 || top-state.previewScroll > state.previewRect.Height-60 {
				state.previewScroll = max(0, top-80)
			}
		}
		return focus
	}
	return gfx.ID{}
}

func (state *model) stepDocumentFind(direction int) {
	if len(state.findMatches) == 0 {
		return
	}
	state.findIndex = (state.findIndex + direction + len(state.findMatches)) % len(state.findMatches)
	state.selectDocumentMatch()
}

func (state *model) replaceDocumentMatches(all bool) {
	if !state.findOpen || !state.sourceReplaceAvailable() {
		return
	}
	current := state.session.Current()
	if _, composing := current.Editor.Composition(); composing {
		return
	}
	state.syncDocumentFind()
	if state.findError != "" || len(state.findMatches) == 0 || state.findDocument != current.ID || state.findSource != current.Text() {
		return
	}
	before := current.Text()
	replacement := state.replaceEditor.String()
	if strings.IndexByte(replacement, 0) >= 0 {
		state.message = "替换文本包含 NUL 字节，已取消"
		return
	}
	if all {
		replacement = strings.ReplaceAll(strings.ReplaceAll(replacement, "\r\n", "\n"), "\r", "\n")
		if current.Newline() == "\r\n" {
			replacement = strings.ReplaceAll(replacement, "\n", "\r\n")
		}
		var result strings.Builder
		projected := int64(len(before))
		for index, start := range state.findMatches {
			projected += int64(len(replacement) - (state.findEnds[index] - start))
		}
		if projected < 0 || projected > workspace.MaxDocumentSize || !current.FitsSavedSize(int(projected)) {
			state.message = "替换结果超过 16 MiB，已取消"
			return
		}
		result.Grow(int(projected))
		previous := 0
		for index, start := range state.findMatches {
			result.WriteString(before[previous:start])
			result.WriteString(replacement)
			previous = state.findEnds[index]
		}
		result.WriteString(before[previous:])
		if result.String() == before {
			return
		}
		current.Editor.ReplaceRange(text.Range{Start: 0, End: current.Editor.RuneCount()}, result.String())
		if state.findSelectedOnly {
			state.findScope.End += len(result.String()) - len(before)
		}
	} else {
		index := state.findIndex
		start, end := state.findMatches[index], state.findEnds[index]
		normalized := strings.ReplaceAll(strings.ReplaceAll(replacement, "\r\n", "\n"), "\r", "\n")
		if current.Newline() == "\r\n" {
			normalized = strings.ReplaceAll(normalized, "\n", "\r\n")
		}
		if !current.FitsSavedSize(len(before) - (end - start) + len(normalized)) {
			state.message = "替换结果超过 16 MiB，已取消"
			return
		}
		if err := current.EditRange(start, end, replacement); err != nil {
			state.message = err.Error()
			return
		}
		if state.findSelectedOnly {
			state.findScope.End += len(current.Text()) - len(before)
		}
	}
	state.syncDocumentFind()
}

func (state *model) selectDocumentMatch() {
	if len(state.findMatches) == 0 {
		state.findPositionDeferred = false
		return
	}
	if state.pendingCompositionFocus() != (gfx.ID{}) {
		// Marked text owns the keyboard: scrolling a match into view would also
		// scroll the composing editor away and drop the focus it still owns.
		state.findPositionDeferred = true
		return
	}
	state.findPositionDeferred = false
	if current := state.session.Current(); current != nil && current.Kind == workspace.RichHTMLKind && state.findMode != editMode && state.mode == state.findMode {
		match := state.findVisual[state.findIndex]
		if match.block < len(state.html.blockY) && state.html.rect.Height > 0 {
			top := state.html.blockY[match.block]
			if top-state.html.scroll < 42 || top-state.html.scroll > state.html.rect.Height-60 {
				state.html.scroll = max(0, min(top-80, max(0, state.html.height-state.html.rect.Height)))
			}
		}
		return
	}
	if state.findMode == renderedMode && state.mode == renderedMode {
		match := state.findVisual[state.findIndex]
		if match.block < len(state.preview.blocks) && state.previewRect.Height > 0 {
			top := state.preview.blocks[match.block].top + 46
			if top-state.previewScroll < 42 || top-state.previewScroll > state.previewRect.Height-60 {
				state.previewScroll = max(0, top-80)
			}
		}
		return
	}
	current := state.session.Current()
	source := current.Text()
	start := state.findMatches[state.findIndex]
	end := start + len(state.findQuery)
	if state.sourceReplaceAvailable() && state.findIndex < len(state.findEnds) {
		end = state.findEnds[state.findIndex]
	}
	current.Editor.SetSelection(text.Range{
		Start: utf8.RuneCountInString(source[:start]), End: utf8.RuneCountInString(source[:end]),
	})
}

func findCommand(canvas *gfx.Canvas, id gfx.ID, caption, label string, bounds ui.Rect, active, enabled bool, palette colors, action func()) {
	if icon, ok := captionIcon(caption); ok {
		iconButton(canvas, id, icon, "", label, bounds, active, !enabled, palette, action)
		return
	}
	if enabled {
		labeledButton(canvas, id, caption, label, bounds, active, palette, action)
		return
	}
	canvas.LabelCenter(caption, bounds, gfx.TextStyle{Size: 12, Weight: gfx.WeightMedium, Color: palette.muted})
	canvas.HitWithOptions(id, bounds, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: label, Disabled: true})
}

func (state *model) paintDocumentFind(canvas *gfx.Canvas, theme ui.Theme, palette colors, bounds ui.Rect) {
	state.findRect = ui.Rect{}
	if !state.findOpen || bounds.Width < 180 {
		return
	}
	replaceable := state.sourceReplaceAvailable()
	width := min(float32(430), bounds.Width-24)
	height := float32(34)
	if replaceable {
		height = 67
	}
	bar := ui.Rect{X: bounds.X + bounds.Width - width - 12, Y: bounds.Y + 4, Width: width, Height: height}
	state.findRect = bar
	canvas.Fill(bar, palette.bar)
	canvas.Fill(ui.Rect{X: bar.X, Y: bar.Y + bar.Height - 1, Width: bar.Width, Height: 1}, palette.border)
	controlsWidth := float32(123)
	if replaceable {
		controlsWidth = 173
	}
	field := ui.Rect{X: bar.X + 8, Y: bar.Y + 3, Width: bar.Width - controlsWidth, Height: 27}
	findTheme := theme
	findTheme.Metrics.FontSizeM = 12
	query := state.findEditor.String()
	focused := canvas.Focused() == findInputID
	components.PaintInput(canvas, findTheme, field, components.InputSnapshot{
		ID: findInputID, Label: "文内查找", Value: query, Placeholder: "查找当前文档",
		Focused: focused, PlatformEditing: focused && query != "", ShowCaret: focused && query == "",
	})
	count := "0/0"
	if len(state.findMatches) > 0 {
		count = strconv.Itoa(state.findIndex+1) + "/" + strconv.Itoa(len(state.findMatches))
	}
	canvas.LabelCenter(count, ui.Rect{X: field.X + field.Width + 2, Y: bar.Y + 3, Width: 39, Height: 27}, gfx.TextStyle{Size: 11, Color: palette.muted})
	x := field.X + field.Width + 43
	button(canvas, findPreviousID, "↑", ui.Rect{X: x, Y: bar.Y + 4, Width: 22, Height: 25}, false, palette, func() { state.stepDocumentFind(-1) })
	button(canvas, findNextID, "↓", ui.Rect{X: x + 24, Y: bar.Y + 4, Width: 22, Height: 25}, false, palette, func() { state.stepDocumentFind(1) })
	if replaceable {
		labeledButton(canvas, findCaseID, "Aa", "区分大小写", ui.Rect{X: x + 48, Y: bar.Y + 4, Width: 23, Height: 25}, state.findCaseSensitive, palette, func() { state.findCaseSensitive = !state.findCaseSensitive })
		labeledButton(canvas, findRegexID, ".*", "正则表达式", ui.Rect{X: x + 73, Y: bar.Y + 4, Width: 23, Height: 25}, state.findRegex, palette, func() { state.findRegex = !state.findRegex })
		button(canvas, findCloseID, "×", ui.Rect{X: x + 98, Y: bar.Y + 4, Width: 22, Height: 25}, false, palette, func() {
			state.focusRequest = state.closeDocumentFind()
		})
		replaceField := ui.Rect{X: bar.X + 8, Y: bar.Y + 36, Width: bar.Width - 161, Height: 27}
		focused := canvas.Focused() == replaceInputID
		components.PaintInput(canvas, findTheme, replaceField, components.InputSnapshot{
			ID: replaceInputID, Label: "替换文本", Value: state.replaceEditor.String(), Placeholder: "替换为",
			Focused: focused, PlatformEditing: focused && state.replaceEditor.String() != "", ShowCaret: focused && state.replaceEditor.String() == "",
		})
		left := replaceField.X + replaceField.Width + 4
		findCommand(canvas, findScopeID, "选区", "仅在选区中查找", ui.Rect{X: left, Y: bar.Y + 38, Width: 32, Height: 24}, state.findSelectedOnly, state.findScope.Start < state.findScope.End, palette, func() {
			state.findSelectedOnly = !state.findSelectedOnly
		})
		findCommand(canvas, replaceOneID, "替换", "替换当前匹配", ui.Rect{X: left + 34, Y: bar.Y + 38, Width: 51, Height: 24}, false, len(state.findMatches) > 0 && state.findError == "", palette, func() { state.replaceDocumentMatches(false) })
		findCommand(canvas, replaceAllID, "全部", "替换所有匹配", ui.Rect{X: left + 87, Y: bar.Y + 38, Width: 51, Height: 24}, false, len(state.findMatches) > 0 && state.findError == "", palette, func() { state.replaceDocumentMatches(true) })
	} else {
		button(canvas, findCloseID, "×", ui.Rect{X: x + 48, Y: bar.Y + 4, Width: 22, Height: 25}, false, palette, func() {
			state.focusRequest = state.closeDocumentFind()
		})
	}
}
