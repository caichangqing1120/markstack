package main

import (
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"markstack/internal/markdownedit"
	"markstack/internal/session"
	"markstack/internal/workspace"
)

type codeBlockEditor struct {
	active        bool
	documentID    int
	renderedIndex int
	sourceIndex   int
	source        string
	block         markdownedit.Block
	input         *text.Editor
	textInput     app.TextInput
	view          components.CodeEditor
	languageInput *text.Editor
	languageText  app.TextInput
	emptySelected bool
}

const (
	codeHeaderHeight           = 29
	codeLanguageSuggestionRows = 6
)

var (
	codeLanguageButtons     = controlTree.Sub(page, controlTree.Name("code-language-buttons"))
	codeCopyButtons         = controlTree.Sub(page, controlTree.Name("code-copy-buttons"))
	codeLanguageInputID     = controlTree.ID(page, "code-language-input")
	codeLanguageApplyID     = controlTree.ID(page, "code-language-apply")
	codeLanguageSuggestions = controlTree.Sub(page, controlTree.Name("code-language-suggestions"))
)

func (state *model) codeBlockComposing() bool {
	if state.codeBlock.input == nil {
		return false
	}
	_, composing := state.codeBlock.input.Composition()
	return composing
}

func (state *model) activateCodeBlock(index int) gfx.EventResult {
	current := state.session.Current()
	if current == nil || state.mode != renderedMode || index < 0 || index >= len(state.parsed.Blocks) || state.parsed.Blocks[index].Kind != markdown.Code {
		return gfx.EventResult{}
	}
	if focus := state.pendingCompositionFocus(); !focus.IsZero() {
		return gfx.EventResult{Handled: true, Focus: focus}
	}
	blocks := markdownedit.Parse([]byte(current.Text()))
	sourceIndex := specialSourceBlockIndex(current.Text(), &state.parsed, index, blocks)
	if sourceIndex < 0 {
		rendered := state.parsed.Blocks[index]
		offset := rendered.Start
		if rendered.Start >= 0 && rendered.Start < rendered.End && rendered.End <= len(current.Text()) {
			offset = min(rendered.End, sourceLineEnd(current.Text(), rendered.Start)+1)
		}
		return state.codeBlockSourceFallback(current, offset)
	}
	block := blocks[sourceIndex]
	state.deactivateRenderedView(current)
	state.table = tableEditor{}
	state.selection = renderedSelection{}
	state.codeBlock = codeBlockEditor{
		active: true, documentID: current.ID, renderedIndex: index, sourceIndex: sourceIndex,
		source: current.Text(), block: block, input: text.NewEditor(block.Code.Visible),
	}
	state.codeBlock.input.MoveTo(utf8.RuneCountInString(block.Code.Visible), false)
	return gfx.EventResult{Handled: true, Repaint: true, Focus: codeInputID}
}

func (state *model) codeBlockSourceFallback(current *session.Document, offset int) gfx.EventResult {
	state.codeBlock = codeBlockEditor{}
	state.mode = editMode
	state.message = "此块请在 Markdown 源码模式编辑"
	state.focusRequest = sourceID
	offset = max(0, min(offset, len(current.Text())))
	current.Editor.MoveTo(utf8.RuneCountInString(current.Text()[:offset]), false)
	return gfx.EventResult{Handled: true, Repaint: true, Focus: sourceID}
}

func (state *model) onCodeBlockTextEvent(event app.TextEvent) (app.TextState, error) {
	input := state.codeBlock.input
	if input == nil {
		return app.TextState{}, nil
	}
	if event.Kind == app.TextCommandEvent && event.Command == app.TextSelectAll {
		state.selectFencedCode()
		return app.TextStateOf(input), nil
	}
	state.codeBlock.emptySelected = false
	if event.Kind == app.TextCommandEvent && (event.Command == app.TextUndo || event.Command == app.TextRedo) {
		if _, composing := input.Composition(); composing {
			return app.TextStateOf(input), nil
		}
		state.historyReveal = true
		return state.stepCodeBlockHistory(event.Command), nil
	}
	event.Apply(input)
	return app.TextStateOf(input), nil
}

func (state *model) selectFencedCode() gfx.EventResult {
	result := gfx.EventResult{Handled: true, Repaint: true, Focus: codeInputID}
	if focus := state.pendingCompositionFocus(); !focus.IsZero() {
		result.Focus = focus
		return result
	}
	code, current := &state.codeBlock, state.session.Current()
	if current == nil || !code.active || code.documentID != current.ID || code.source != current.Text() || code.input == nil {
		return gfx.EventResult{}
	}
	if code.input.RuneCount() == 0 && !code.emptySelected {
		code.emptySelected = true
		return result
	}
	if code.input.Selection() != (text.Range{End: code.input.RuneCount()}) {
		code.input.SetSelection(text.Range{End: code.input.RuneCount()})
		return result
	}
	state.deactivateRenderedView(current)
	state.codeBlock = codeBlockEditor{}
	state.mode, state.focusRequest = editMode, sourceID
	current.Editor.SetSelection(text.Range{End: current.Editor.RuneCount()})
	result.Focus = sourceID
	return result
}

func (state *model) openCodeLanguage(index int) gfx.EventResult {
	if focus := state.pendingFormatCompositionFocus(); !focus.IsZero() {
		return gfx.EventResult{Handled: true, Focus: focus}
	}
	if !state.codeBlock.active || state.codeBlock.renderedIndex != index {
		result := state.activateCodeBlock(index)
		if !state.codeBlock.active {
			return result
		}
	}
	if state.codeBlock.block.Code.Math {
		return gfx.EventResult{}
	}
	input := text.NewEditor(markdownedit.FenceLanguage(state.codeBlock.block.Code.Info))
	input.SetSelection(text.Range{End: input.RuneCount()})
	state.codeBlock.languageInput = input
	return gfx.EventResult{Handled: true, Repaint: true, Focus: codeLanguageInputID}
}

func (state *model) finishCodeLanguage(apply bool) gfx.EventResult {
	if state.codeBlock.languageInput == nil {
		return gfx.EventResult{}
	}
	return state.applyCodeLanguage(apply, strings.TrimSpace(state.codeBlock.languageInput.String()))
}

func (state *model) applyCodeLanguage(apply bool, language string) gfx.EventResult {
	code, current := &state.codeBlock, state.session.Current()
	if code.languageInput == nil || current == nil || !code.active || code.documentID != current.ID || code.source != current.Text() {
		return gfx.EventResult{}
	}
	if focus := state.pendingFormatCompositionFocus(); !focus.IsZero() {
		return gfx.EventResult{Handled: true, Focus: focus}
	}
	result := gfx.EventResult{Handled: true, Repaint: true, Focus: codeInputID}
	if !apply {
		code.languageInput = nil
		return result
	}
	patch, err := markdownedit.PatchFenceLanguage([]byte(code.source), code.block, language)
	if err != nil {
		state.message = "无效的代码语言标识"
		result.Focus = codeLanguageInputID
		return result
	}
	source := code.source
	if patch.Text != source[patch.Start:patch.End] {
		bodyStart := code.block.Code.Start
		selection := directedCodeSelection(code.input)
		before := codeSelectionInSource(source, bodyStart, code.input.String(), selection)
		next := source[:patch.Start] + patch.Text + source[patch.End:]
		updatedStart := bodyStart + len(patch.Text) - (patch.End - patch.Start)
		after := codeSelectionInSource(next, updatedStart, code.input.String(), selection)
		if err := applyMarkdownPatch(current, patch, before, after); err != nil {
			state.message = err.Error()
			result.Focus = codeLanguageInputID
			return result
		}
		blocks := markdownedit.Parse([]byte(next))
		code.block, code.source = blocks[code.sourceIndex], next
		state.moveInactiveRenderedHistory(current, source, 1, true)
	}
	code.languageInput = nil
	state.message = ""
	return result
}

func codeSelectionInSource(source string, bodyStart int, visible string, selection text.Range) text.Range {
	base := utf8.RuneCountInString(source[:bodyStart])
	if bodyStart < 2 || source[bodyStart-2] != '\r' {
		return shiftedSelection(selection, base)
	}
	position := func(offset int) int {
		return base + offset + strings.Count(visible[:byteOffsetOfRune(visible, offset)], "\n")
	}
	return text.Range{Start: position(selection.Start), End: position(selection.End)}
}

func (state *model) copyCode(index int) gfx.EventResult {
	if index < 0 || index >= len(state.parsed.Blocks) || state.parsed.Blocks[index].Kind != markdown.Code {
		return gfx.EventResult{}
	}
	if focus := state.pendingFormatCompositionFocus(); !focus.IsZero() {
		return gfx.EventResult{Handled: true, Focus: focus}
	}
	value := state.parsed.Blocks[index].Text
	if err := app.SetClipboardText(value); err != nil {
		state.message = err.Error()
	} else {
		state.message = "已复制代码"
		state.showToast("已复制代码块", "Copied code block")
	}
	result := gfx.EventResult{Handled: true, Repaint: true}
	if state.codeBlock.active && state.codeBlock.renderedIndex == index {
		result.Focus = codeInputID
	}
	return result
}

func (state *model) onCodeControl(target gfx.ID) gfx.EventResult {
	if index, ok := target.In(codeCopyButtons); ok {
		return state.copyCode(index)
	}
	if index, ok := target.In(codeLanguageButtons); ok {
		return state.openCodeLanguage(index)
	}
	if target == codeLanguageApplyID {
		return state.finishCodeLanguage(true)
	}
	if index, ok := target.In(codeLanguageSuggestions); ok {
		options := state.fenceLanguageSuggestions()
		if index >= 0 && index < len(options) {
			return state.applyCodeLanguage(true, options[index])
		}
	}
	return gfx.EventResult{}
}

func (state *model) fenceLanguageSuggestions() []string {
	input := state.codeBlock.languageInput
	if input == nil {
		return nil
	}
	if _, composing := input.Composition(); composing {
		return nil
	}
	query := input.String()
	if selection := input.Selection(); selection.Start == 0 && selection.End == input.RuneCount() {
		query = ""
	}
	return workspace.CodeLanguageSuggestions(query, codeLanguageSuggestionRows)
}

func (state *model) paintCodeControls(canvas *gfx.Canvas, theme ui.Theme, palette colors, clip ui.Rect, x, y float32, blocks []markdownedit.Block) {
	source := state.session.Current().Text()
	for index := state.preview.firstVisible(clip.Y - y); index < len(state.preview.blocks); index++ {
		layout := &state.preview.blocks[index]
		top := y + layout.top
		if top >= clip.Y+clip.Height {
			break
		}
		if layout.kind != markdown.Code || top+codeHeaderHeight <= clip.Y || layout.width < 90 {
			continue
		}
		active := state.codeBlock.active && state.codeBlock.renderedIndex == index
		if !active && (layout.vector.Width > 0 || layout.mathFallback) {
			continue
		}
		header := ui.Rect{X: x, Y: top, Width: layout.width, Height: codeHeaderHeight}
		state.preview.paintCodeHeader(canvas, header)
		copyBounds := ui.Rect{X: x + layout.width - 58, Y: top + 3, Width: 52, Height: 23}
		paintCodeControl(canvas, palette, codeCopyButtons.At(index), copyBounds, state.local("复制", "Copy"), state.local("复制代码", "Copy code"))
		language := state.codeLanguageBadge(state.parsed.Blocks[index].Info)
		font := palette.textSize(11)
		languageWidth := min(126, layout.width-72, canvas.TextWidthWeight(language, font, gfx.WeightSemibold)+16)
		field := ui.Rect{X: x + 7, Y: top + 5, Width: languageWidth, Height: 19}
		if active && state.codeBlock.languageInput != nil {
			input := state.codeBlock.languageInput
			field.Width = max(1, min(float32(150), layout.width-91))
			field.Y, field.Height = top+3, 23
			inputTheme := theme
			inputTheme.Metrics.FontSizeM = 12
			components.PaintInput(canvas, inputTheme, field, components.InputSnapshot{ID: codeLanguageInputID, Label: state.local("代码语言", "Code language"), Value: input.String(), Placeholder: state.local("搜索语言", "Search languages"), Focused: canvas.Focused() == codeLanguageInputID, PlatformEditing: true, NavigationKeys: true})
			paintCodeControl(canvas, palette, codeLanguageApplyID, ui.Rect{X: field.X + field.Width + 2, Y: field.Y, Width: 25, Height: field.Height}, "✓", "应用代码语言")
			state.paintCodeLanguageSuggestions(canvas, palette, field, clip)
		} else {
			id := codeLanguageButtons.At(index)
			editable := state.mode == renderedMode && !state.preview.displayMath[index] && layout.width >= 135 && specialSourceBlockIndex(source, &state.parsed, index, blocks) >= 0
			fill := palette.sidebar
			if editable && (canvas.Hovered() == id || canvas.Focused() == id) {
				fill = palette.selected
			}
			canvas.Rounded(field, 5, fill)
			canvas.LabelCenter(ellipsizeWeight(canvas, language, field.Width-6, font, gfx.WeightSemibold), field, gfx.TextStyle{Size: font, Weight: gfx.WeightSemibold, Color: palette.ink})
			if editable {
				canvas.HitWithOptions(id, field, state.local("更改代码语言：", "Change code language: ")+language, gfx.HitOptions{Role: gfx.RoleButton})
			}
		}
	}
}

func (state *model) codeLanguageBadge(info string) string {
	language := markdownedit.FenceLanguage(info)
	switch strings.ToLower(language) {
	case "":
		return state.local("纯文本", "Plain text")
	case "go", "golang":
		return "Go"
	case "py", "python":
		return "Python"
	case "yaml", "yml":
		return "YAML"
	case "sql":
		return "SQL"
	default:
		return language
	}
}

func (state *model) paintCodeLanguageSuggestions(canvas *gfx.Canvas, palette colors, field, clip ui.Rect) {
	_, suggestFocused := canvas.Focused().In(codeLanguageSuggestions)
	if canvas.Focused() != codeLanguageInputID && !suggestFocused {
		return
	}
	options := state.fenceLanguageSuggestions()
	if len(options) == 0 {
		return
	}
	const rowHeight = 23
	box := ui.Rect{X: max(clip.X+2, field.X), Y: field.Y + field.Height + 3, Width: max(124, field.Width), Height: float32(len(options)*rowHeight + 4)}
	box.X = min(box.X, clip.X+clip.Width-box.Width-2)
	if box.Y+box.Height > clip.Y+clip.Height {
		box.Y = field.Y - 3 - box.Height
	}
	canvas.PushLayer(7)
	defer canvas.PopLayer()
	canvas.RoundedBorder(box, 5, 1, palette.border, palette.background)
	for index, option := range options {
		bounds := ui.Rect{X: box.X + 2, Y: box.Y + 2 + float32(index*rowHeight), Width: box.Width - 4, Height: rowHeight}
		id := codeLanguageSuggestions.At(index)
		paintCodeControl(canvas, palette, id, bounds, option, state.local("使用 ", "Use ")+option)
	}
}

func paintCodeControl(canvas *gfx.Canvas, palette colors, id gfx.ID, bounds ui.Rect, value, label string) {
	if canvas.Hovered() == id || canvas.Focused() == id {
		canvas.Rounded(bounds, 5, palette.selected)
	}
	canvas.LabelCenter(value, bounds, gfx.TextStyle{Size: 12, Color: palette.ink})
	canvas.HitWithOptions(id, bounds, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: label})
}

func (state *model) indentFencedCode(outdent bool) {
	code, current := &state.codeBlock, state.session.Current()
	if current == nil || code.input == nil || code.documentID != current.ID || code.source != current.Text() ||
		code.block.Code.Visible != code.input.String() || current.Composing() || state.codeBlockComposing() {
		return
	}
	before := directedCodeSelection(code.input)
	if before.Start != before.End {
		state.indentSelectedCodeLines(outdent)
	} else if outdent {
		state.unindentCodeLine()
	} else {
		code.input.Insert("\t")
	}
	state.syncCodeBlockEdit(current, &before)
}

func directedCodeSelection(input *text.Editor) text.Range {
	selection := input.Selection()
	if input.Caret() == selection.Start {
		selection.Start, selection.End = selection.End, selection.Start
	}
	return selection
}

func (state *model) unindentCodeLine() {
	input := state.codeBlock.input
	if input == nil || input.Selection().Start != input.Selection().End {
		return
	}
	value := input.String()
	caret := input.Caret()
	position := byteOffsetOfRune(value, caret)
	start := strings.LastIndexByte(value[:position], '\n') + 1
	indent := codeLineIndent(value, start)
	if indent == 0 {
		return
	}
	startRune := utf8.RuneCountInString(value[:start])
	input.SetSelection(text.Range{Start: startRune, End: startRune + indent})
	input.Insert("")
	input.MoveTo(max(startRune, caret-indent), false)
}

func codeLineIndent(value string, start int) int {
	if start < len(value) && value[start] == '\t' {
		return 1
	}
	indent := 0
	for start+indent < len(value) && indent < 4 && value[start+indent] == ' ' {
		indent++
	}
	return indent
}

func (state *model) indentSelectedCodeLines(backward bool) {
	input := state.codeBlock.input
	selection := input.Selection()
	if selection.Start == selection.End {
		return
	}
	value := input.String()
	start := byteOffsetOfRune(value, selection.Start)
	end := byteOffsetOfRune(value, selection.End)
	lineStart := strings.LastIndexByte(value[:start], '\n') + 1
	var lines []int
	for lineStart < end {
		lines = append(lines, lineStart)
		next := strings.IndexByte(value[lineStart:], '\n')
		if next < 0 {
			break
		}
		lineStart += next + 1
	}
	if len(lines) == 0 {
		return
	}
	var next strings.Builder
	next.Grow(len(value) + len(lines))
	cursor, startRemoved, endRemoved := 0, 0, 0
	for _, position := range lines {
		next.WriteString(value[cursor:position])
		if backward {
			removed := codeLineIndent(value, position)
			startRemoved += max(0, min(start-position, removed))
			endRemoved += max(0, min(end-position, removed))
			cursor = position + removed
		} else {
			next.WriteByte('\t')
			cursor = position
		}
	}
	next.WriteString(value[cursor:])
	if next.String() == value {
		return
	}
	newStart, newEnd := selection.Start+1, selection.End+len(lines)
	if backward {
		newStart, newEnd = selection.Start-startRemoved, selection.End-endRemoved
	}
	if input.Caret() == selection.Start {
		newStart, newEnd = newEnd, newStart
	}
	input.SetSelection(text.Range{Start: 0, End: input.RuneCount()})
	input.Insert(next.String())
	input.SetSelection(text.Range{Start: newStart, End: newEnd})
}

func (state *model) stepCodeBlockHistory(command app.TextCommand) app.TextState {
	code := &state.codeBlock
	current := state.session.Current()
	if current == nil || code.input == nil {
		return app.TextState{}
	}
	if code.documentID != current.ID || code.source != current.Text() {
		return app.TextStateOf(code.input)
	}
	if result, handled := state.stepInactiveRenderedHistory(command, code.input); handled {
		return result
	}
	before := current.Text()
	changed := false
	if command == app.TextUndo {
		changed = state.stepMarkdownEdit(current, true)
	} else {
		changed = state.stepMarkdownEdit(current, false)
	}
	if !changed {
		return app.TextStateOf(code.input)
	}
	if state.restoreOtherRenderedHistory(current, before, command) {
		state.table, state.codeBlock = tableEditor{}, codeBlockEditor{}
		return app.TextStateOf(state.rich.input)
	}
	delta := 1
	if command == app.TextUndo {
		delta = -1
	}
	blocks := markdownedit.Parse([]byte(current.Text()))
	if code.sourceIndex >= 0 && code.sourceIndex < len(blocks) && (codeBlockSourceChange(before, current.Text(), code.block, blocks[code.sourceIndex]) || fenceLanguageSourceChange(before, current.Text(), code.block, blocks[code.sourceIndex])) {
		state.moveInactiveRenderedHistory(current, before, delta, false)
		caret := min(code.input.Caret(), utf8.RuneCountInString(blocks[code.sourceIndex].Code.Visible))
		code.input = text.NewEditor(blocks[code.sourceIndex].Code.Visible)
		if selection, valid := codeSourceHistorySelection(current, blocks[code.sourceIndex]); valid {
			code.input.SetSelection(selection)
		} else {
			code.input.MoveTo(caret, false)
		}
		code.block, code.source, code.languageInput = blocks[code.sourceIndex], current.Text(), nil
		return app.TextStateOf(code.input)
	}
	if result, restored := state.restoreSpecialSourceHistory(current, before, command); restored {
		return result
	}
	state.moveInactiveRenderedHistory(current, before, delta, false)
	previousState := app.TextStateOf(code.input)
	state.specialHistorySourceFallback()
	return previousState
}

func (state *model) syncCodeBlock(current *session.Document) {
	state.syncCodeBlockEdit(current, nil)
}

func (state *model) syncCodeBlockEdit(current *session.Document, beforeSelection *text.Range) {
	code := &state.codeBlock
	if !code.active {
		return
	}
	if code.documentID != current.ID || code.source != current.Text() || code.input == nil {
		state.codeBlock = codeBlockEditor{}
		return
	}
	defer state.moveInactiveRenderedHistory(current, current.Text(), 1, true)
	if _, composing := code.input.Composition(); composing || code.input.String() == code.block.Code.Visible {
		return
	}
	value := code.input.String()
	next, err := markdownedit.ReplaceCode([]byte(code.source), code.block, value)
	if err != nil {
		if beforeSelection != nil {
			code.input = text.NewEditor(code.block.Code.Visible)
			code.input.SetSelection(*beforeSelection)
			return
		}
		replacement := strings.ReplaceAll(value, "\n", current.Newline())
		if value != "" && !code.block.Code.SingleLine {
			replacement += current.Newline()
		}
		if code.block.Code.SingleLine && strings.ContainsRune(value, '\n') {
			replacement = current.Newline() + replacement + current.Newline()
		}
		current.Editor.BreakUndoGroup()
		editErr := current.EditRange(code.block.Code.Start, code.block.Code.End, replacement)
		current.Editor.BreakUndoGroup()
		if editErr != nil {
			state.message = editErr.Error()
			return
		}
		state.codeBlockSourceFallback(current, code.block.Code.Start+len(replacement))
		return
	}
	body := code.block.Code
	newline := current.Newline()
	if beforeSelection != nil {
		newline = "\n"
		if body.Start >= 2 && code.source[body.Start-2] == '\r' {
			newline = "\r\n"
		}
		if value == "" && body.Start < body.End && !body.SingleLine {
			next = []byte(code.source[:body.Start] + newline + code.source[body.End:])
		}
	}
	end := len(next) - (len(code.source) - body.End)
	replacement := string(next[body.Start:end])
	previousBody := code.source[body.Start:body.End]
	var patchEnd int
	if beforeSelection == nil {
		patchEnd = len(strings.ReplaceAll(value[:byteOffsetOfRune(value, code.input.Caret())], "\n", newline))
	} else {
		patchEnd = fencedIndentPatchEnd(previousBody, replacement)
	}
	start, endBefore, inserted := renderedInputSourceEdit(previousBody, replacement, patchEnd)
	if beforeSelection == nil {
		current.Editor.BreakUndoGroup()
		err = current.EditRange(code.block.Code.Start+start, code.block.Code.Start+endBefore, inserted)
		current.Editor.BreakUndoGroup()
	} else {
		before := codeSelectionInSource(code.source, body.Start, body.Visible, *beforeSelection)
		after := codeSelectionInSource(string(next), body.Start, value, directedCodeSelection(code.input))
		patch := markdownedit.SourcePatch{Start: body.Start + start, End: body.Start + endBefore, Text: inserted}
		err = applyMarkdownPatch(current, patch, before, after)
		if err != nil {
			code.input = text.NewEditor(code.block.Code.Visible)
			code.input.SetSelection(*beforeSelection)
		}
	}
	if err != nil {
		state.message = err.Error()
		return
	}
	if current.Text() != string(next) {
		state.codeBlockSourceFallback(current, code.block.Code.Start)
		return
	}
	blocks := markdownedit.Parse(next)
	if code.sourceIndex >= len(blocks) || !blocks[code.sourceIndex].Code.Editable || blocks[code.sourceIndex].Code.Visible != value {
		state.codeBlockSourceFallback(current, code.block.Code.Start)
		return
	}
	code.block, code.source = blocks[code.sourceIndex], current.Text()
	state.message = ""
}

func fencedIndentPatchEnd(before, after string) int {
	for len(before) > 0 && len(after) > 0 {
		previous, beforeWidth := utf8.DecodeLastRuneInString(before)
		current, afterWidth := utf8.DecodeLastRuneInString(after)
		if previous != current || beforeWidth != afterWidth {
			break
		}
		before = before[:len(before)-beforeWidth]
		after = after[:len(after)-afterWidth]
	}
	return len(after)
}

func (state *model) paintCodeBlock(canvas *gfx.Canvas, theme ui.Theme, palette colors, bounds ui.Rect) {
	appearance := state.markdownAppearance(palette)
	palette = appearance.palette
	code := &state.codeBlock
	value := code.input.String()
	selection := code.input.Selection()
	focused := canvas.Focused() == codeInputID
	snapshot := components.CodeEditorSnapshot{
		ID: codeInputID, Label: "围栏代码编辑器", Value: value,
		Focused: focused, ShowCaret: focused, Wrap: true,
		FontSize: state.readingSize() - 1, MonoFace: markdownCodeFace, Language: code.block.Code.Info,
		Caret:          byteOffsetOfRune(value, code.input.Caret()),
		SelectionStart: byteOffsetOfRune(value, selection.Start),
		SelectionEnd:   byteOffsetOfRune(value, selection.End),
	}
	if marked, composing := code.input.Composition(); composing {
		snapshot.CompositionStart = byteOffsetOfRune(value, marked.Start)
		snapshot.CompositionEnd = byteOffsetOfRune(value, marked.End)
	}
	var look components.MarkdownLook
	appearance.applyCodeLook(&look, state.dark)
	snapshot.Highlight = func(language, line string, dst []components.CodeEditorRun) []components.CodeEditorRun {
		dst = dst[:0]
		for _, run := range markdownCodeRuns(language, line, nil) {
			dst = append(dst, components.CodeEditorRun{Start: run.Start, End: run.End, Color: look.Code[run.Class]})
		}
		return dst
	}
	codeTheme := theme
	codeTheme.Foreground, codeTheme.CardForeground = palette.ink, palette.ink
	codeTheme.Background, codeTheme.Card = appearance.codeFill, appearance.codeFill
	codeTheme.Input, codeTheme.Border, codeTheme.Primary = appearance.codeBorder, appearance.codeBorder, appearance.codeBorder
	composingConfirmation := state.confirmationCompositionFocus() == codeInputID
	if composingConfirmation {
		canvas.PushFocusScope(confirmScope)
	}
	code.view.Paint(canvas, codeTheme, bounds, snapshot)
	if composingConfirmation {
		canvas.PopFocusScope()
	}
}
