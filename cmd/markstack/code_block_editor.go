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
}

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
	state.message = "此围栏代码请在 Markdown 源码模式编辑"
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
		changed = current.Editor.Undo()
	} else {
		changed = current.Editor.Redo()
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
	if code.sourceIndex >= 0 && code.sourceIndex < len(blocks) && codeBlockSourceChange(before, current.Text(), code.block, blocks[code.sourceIndex]) {
		state.moveInactiveRenderedHistory(current, before, delta, false)
		caret := min(code.input.Caret(), utf8.RuneCountInString(blocks[code.sourceIndex].Code.Visible))
		code.input = text.NewEditor(blocks[code.sourceIndex].Code.Visible)
		if selection, valid := codeSourceHistorySelection(current, blocks[code.sourceIndex]); valid {
			code.input.SetSelection(selection)
		} else {
			code.input.MoveTo(caret, false)
		}
		code.block, code.source = blocks[code.sourceIndex], current.Text()
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
		replacement := strings.ReplaceAll(value, "\n", current.Newline())
		if value != "" {
			replacement += current.Newline()
		}
		if editErr := current.EditRange(code.block.Code.Start, code.block.Code.End, replacement); editErr != nil {
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
		if value == "" && body.Start < body.End {
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
		err = code.commitIndent(current.Editor, start, endBefore, inserted, newline, *beforeSelection)
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

func (code *codeBlockEditor) commitIndent(source *text.Editor, start, end int, inserted, newline string, before text.Range) error {
	body := code.block.Code
	prefixRunes := utf8.RuneCountInString(code.source[:body.Start])
	project := func(value string, selection text.Range) text.Range {
		position := func(index int) int {
			return prefixRunes + index + (len(newline)-1)*strings.Count(value[:byteOffsetOfRune(value, index)], "\n")
		}
		return text.Range{Start: position(selection.Start), End: position(selection.End)}
	}
	beforeSource := project(body.Visible, before)
	afterSource := project(code.input.String(), directedCodeSelection(code.input))
	edits := []text.Replacement{{Range: text.Range{
		Start: utf8.RuneCountInString(code.source[:body.Start+start]),
		End:   utf8.RuneCountInString(code.source[:body.Start+end]),
	}, Text: inserted}}
	options := text.ReplaceRangesOptions{AfterSelection: &afterSource}
	if err := source.ValidateReplaceRanges(edits, options); err != nil {
		return err
	}
	previous := directedCodeSelection(source)
	source.SetSelection(beforeSource)
	if err := source.ReplaceRangesWith(edits, options); err != nil {
		source.SetSelection(previous)
		return err
	}
	return nil
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
	markdownCodeLook(&look, state.dark, palette)
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
	code.view.Paint(canvas, codeTheme, bounds, snapshot)
}
