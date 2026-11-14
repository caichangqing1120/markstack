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
	if len(blocks) != len(state.parsed.Blocks) || index >= len(blocks) || blocks[index].Kind != markdownedit.FencedCode || !blocks[index].Code.Editable || strings.ReplaceAll(state.parsed.Blocks[index].Text, "\r\n", "\n") != blocks[index].Code.Visible {
		offset := 0
		if index < len(blocks) {
			offset = blocks[index].Start
		}
		return state.codeBlockSourceFallback(current, offset)
	}
	block := blocks[index]
	state.deactivateRenderedView(current)
	state.table = tableEditor{}
	state.selection = renderedSelection{}
	state.codeBlock = codeBlockEditor{
		active: true, documentID: current.ID, renderedIndex: index, sourceIndex: index,
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
	cursor, firstRemoved, totalRemoved := 0, 0, 0
	for index, position := range lines {
		next.WriteString(value[cursor:position])
		if backward {
			removed := codeLineIndent(value, position)
			if index == 0 {
				firstRemoved = removed
			}
			totalRemoved += removed
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
		newStart = max(utf8.RuneCountInString(value[:lines[0]]), selection.Start-firstRemoved)
		newEnd = max(newStart, selection.End-totalRemoved)
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
	end := len(next) - (len(code.source) - code.block.Code.End)
	replacement := string(next[code.block.Code.Start:end])
	caret := len(strings.ReplaceAll(value[:byteOffsetOfRune(value, code.input.Caret())], "\n", current.Newline()))
	start, endBefore, inserted := renderedInputSourceEdit(code.source[code.block.Code.Start:code.block.Code.End], replacement, caret)
	current.Editor.BreakUndoGroup()
	err = current.EditRange(code.block.Code.Start+start, code.block.Code.Start+endBefore, inserted)
	current.Editor.BreakUndoGroup()
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
