package main

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/text"

	"markstack/internal/markdownedit"
	"markstack/internal/session"
)

const m2FencePrefix = "(```|···|、、、|｀｀｀)"
const m2FenceLanguage = "[a-zA-Z0-9_+.#-]*"

var m2Marker = regexp.MustCompile("^(#{1,6}|[-*]|1\\.|>|--[-]?|`{1,2}|·{1,2}|、{1,2}|｀{1,2}|" + m2FencePrefix + m2FenceLanguage + "[ \t]*)$")
var m2Fence = regexp.MustCompile("^" + m2FencePrefix + "(" + m2FenceLanguage + ")$")
var m2Inline = regexp.MustCompile("\\*\\*([^*\\r\\n]+)\\*\\*|`([^`\\r\\n]+)`")
var m2HalfBold = regexp.MustCompile("\\*\\*[^*\\r\\n]+\\*$")

func normalizeFenceTrigger(value string) (string, bool) {
	match := m2Fence.FindStringSubmatch(strings.TrimRight(value, " \t"))
	if match == nil {
		return "", false
	}
	return "```" + match[2], true
}

func literalMarkdown(value string) string {
	var result strings.Builder
	for _, character := range value {
		if strings.ContainsRune("\\`*_{}[]()#+-.!<>|~&", character) {
			result.WriteByte('\\')
		}
		result.WriteRune(character)
	}
	return result.String()
}

func visibleLiteralMarkdown(value string) string {
	var result strings.Builder
	for index := 0; index < len(value); index++ {
		if value[index] == '\\' && index+1 < len(value) {
			index++
		}
		result.WriteByte(value[index])
	}
	return result.String()
}

func (state *model) m2ParagraphRange() (int, int, bool) {
	rich, current := &state.rich, state.session.Current()
	if current == nil || state.mode != renderedMode || !rich.active || rich.input == nil || rich.source != current.Text() || rich.documentID != current.ID || rich.block.Kind != markdownedit.Paragraph || rich.block.IsCard() {
		return 0, 0, false
	}
	start, end := rich.block.Start, rich.block.End
	if rich.virtual {
		start, end = rich.virtualStart, rich.virtualEnd-len(rich.virtualSuffix)
	}
	source := rich.source
	if start < 0 || end < start || end > len(source) || start != 0 && source[start-1] != '\n' {
		return 0, 0, false
	}
	if !rich.virtual {
		if source[start:end] != rich.block.Visible {
			return 0, 0, false
		}
		end += len(source[end:]) - len(strings.TrimLeft(source[end:], " \t"))
	}
	return start, end, end == len(source) || source[end] == '\r' || source[end] == '\n'
}

func (state *model) applyM2Patch(start, end int, replacement string, index, caret, beforeCaret int, target *renderedVirtualState) bool {
	current := state.session.Current()
	prior := &state.rich
	before, beforeIndex, beforeVirtual := prior.source, prior.index, prior.virtualState()
	if current == nil || before != current.Text() || current.Composing() || start < 0 || end < start || end > len(before) {
		return false
	}
	after := before[:start] + replacement + before[end:]
	var parsed markdown.Document
	formulas, display := parseTechnicalMarkdown(&parsed, after, true)
	if after == before || !current.FitsSavedSize(len(after)) || index < 0 || index > len(parsed.Blocks) || target == nil && index == len(parsed.Blocks) {
		return false
	}
	blocks := markdownedit.Parse([]byte(after))
	normalizeRenderedTasks(&parsed, blocks)
	if !validM2Target(&parsed, blocks, after, current.ID, index, caret, target) {
		return false
	}
	history, position := state.renderedHistorySnapshot(current)
	selection := current.Editor.Selection()
	if current.Editor.Caret() == selection.Start {
		selection.Start, selection.End = selection.End, selection.Start
	}
	afterCaret := utf8.RuneCountInString(after[:start+len(replacement)])
	if err := current.EditRange(start, end, replacement, session.RangeEditSelection{Before: selection, After: text.Range{Start: afterCaret, End: afterCaret}}); err != nil {
		state.message = err.Error()
		return false
	}
	state.preview.layoutKey = previewLayoutKey{}
	state.parsed, state.preview.formulas, state.preview.displayMath = parsed, formulas, display
	if !state.restoreRenderedStructureView(index, caret, target) {
		current.Editor.Undo()
		state.parseEditableMarkdown(before)
		return false
	}
	state.rememberRenderedStructure(history, position, before, beforeIndex, beforeCaret, beforeVirtual)
	state.historyReveal = true
	state.invalidator.Invalidate()
	return true
}

func validM2Target(parsed *markdown.Document, blocks []markdownedit.Block, source string, documentID, index, caret int, target *renderedVirtualState) bool {
	if target != nil {
		_, ok := newRenderedVirtualEditor(documentID, source, index, caret, target)
		return ok
	}
	if parsed.Blocks[index].Kind == markdown.Code || parsed.Blocks[index].Kind == markdown.Table {
		position := specialSourceBlockIndex(source, parsed, index, blocks)
		if position < 0 {
			return false
		}
		block := blocks[position]
		if block.Kind == markdownedit.FencedCode {
			return block.Code.Editable && caret <= utf8.RuneCountInString(block.Code.Visible)
		}
		return len(block.Cells) != 0 && block.Cells[0].Editable && caret <= utf8.RuneCountInString(block.Cells[0].Visible)
	}
	position := renderedMapping(parsed, blocks)[index]
	return position >= 0 && caret <= utf8.RuneCountInString(blocks[position].Visible)
}

func (state *model) updateM2Typing(beforeCaret int) bool {
	rich := &state.rich
	value := rich.input.String()
	caret := rich.input.Caret()
	if rich.input.Selection().Start != caret {
		return false
	}
	caretByte := byteOffsetOfRune(value, caret)
	start, end, paragraph := state.m2ParagraphRange()
	marker, suffix := value[:caretByte], value[caretByte:]
	if rich.virtual && rich.block.Kind == markdownedit.ListItem && rich.virtualInsert == "- " && strings.HasPrefix("[ ] ", marker) && marker != "" {
		start, end = rich.virtualStart, rich.virtualEnd-len(rich.virtualSuffix)
		if marker != "[ ] " {
			return state.stageM2Literal(start, end, value, beforeCaret, markdownedit.ListItem)
		}
		start -= len("- ")
		marker, paragraph = "- [ ] ", true
	}
	if !paragraph {
		return false
	}
	if kind := m2SpaceKind(marker); kind != "" {
		if !state.stageM2Literal(start, end, marker+suffix, beforeCaret, markdownedit.Paragraph) {
			return false
		}
		end = state.rich.virtualEnd
		target := &renderedVirtualState{kind: kind, start: start + len(marker), end: start + len(marker) + len(suffix), insert: marker}
		state.applyM2Patch(start, end, marker+suffix, state.rich.index, 0, utf8.RuneCountInString(marker), target)
		return true
	}
	if caretByte == len(value) && (m2Marker.MatchString(value) || m2HalfBold.MatchString(value)) {
		return state.stageM2Literal(start, end, value, beforeCaret, markdownedit.Paragraph)
	}
	pair := m2Inline.FindAllStringIndex(value[:caretByte], -1)
	if len(pair) == 0 || pair[len(pair)-1][1] != caretByte {
		return false
	}
	next := rich.source[:start] + value + rich.source[end:]
	var parsed markdown.Document
	parsed.Parse(next)
	if rich.index >= len(parsed.Blocks) || parsed.Blocks[rich.index].Kind != markdown.Paragraph || parsed.Blocks[rich.index].Start != start || parsed.Blocks[rich.index].End != start+len(value) {
		return false
	}
	blocks := markdownedit.Parse([]byte(next))
	mapping := renderedMapping(&parsed, blocks)
	if mapping[rich.index] < 0 {
		return false
	}
	visible := blocks[mapping[rich.index]].Visible
	if visible == value || !strings.HasSuffix(visible, suffix) {
		return false
	}
	return state.applyM2Patch(start, end, value, rich.index, utf8.RuneCountInString(visible[:len(visible)-len(suffix)]), beforeCaret, nil)
}

func (state *model) stageM2Literal(start, end int, value string, beforeCaret int, kind markdownedit.Kind) bool {
	rich := &state.rich
	encoded := literalMarkdown(value)
	target := &renderedVirtualState{kind: kind, start: start, end: start + len(encoded), literal: true}
	if kind == markdownedit.ListItem {
		target.insert = rich.virtualInsert
	}
	return state.applyM2Patch(start, end, encoded, rich.index, utf8.RuneCountInString(value), beforeCaret, target)
}

func (state *model) syncM2Literal(beforeCaret int) bool {
	rich := &state.rich
	if !rich.virtualLiteral || rich.input.String() == rich.block.Visible {
		return false
	}
	if _, composing := rich.input.Composition(); composing {
		return false
	}
	value := rich.input.String()
	start, end := rich.virtualStart, rich.virtualEnd-len(rich.virtualSuffix)
	plain := *rich
	plain.virtualLiteral = false
	if _, err := virtualBlockSource(&plain, value); err == nil {
		target := plain.virtualState()
		target.end = start + len(value) + len(target.suffix)
		if state.applyM2Patch(start, end, value, rich.index, rich.input.Caret(), beforeCaret, target) {
			return true
		}
	}
	return state.stageM2Literal(start, end, value, beforeCaret, rich.block.Kind)
}

func m2SpaceKind(value string) markdownedit.Kind {
	switch value {
	case "- ", "* ", "1. ", "- [ ] ":
		return markdownedit.ListItem
	case "> ":
		return markdownedit.Quote
	}
	if prefix, ok := strings.CutSuffix(value, " "); ok && len(prefix) >= 1 && len(prefix) <= 6 && strings.Trim(prefix, "#") == "" {
		return markdownedit.Heading
	}
	return ""
}

func (state *model) enterM2Marker() gfx.EventResult {
	start, end, ok := state.m2ParagraphRange()
	if !ok {
		return gfx.EventResult{}
	}
	rich := &state.rich
	value := rich.input.String()
	fence, fenced := normalizeFenceTrigger(value)
	if value != "---" && !fenced {
		return gfx.EventResult{}
	}
	if _, composing := rich.input.Composition(); composing {
		return gfx.EventResult{Handled: true}
	}
	if rich.input.Selection().Start != rich.input.RuneCount() {
		return gfx.EventResult{}
	}
	newline := state.session.Current().Newline()
	if fenced {
		replacement := fence + newline + newline + "```"
		if state.applyM2Patch(start, end, replacement, rich.index, 0, rich.input.Caret(), nil) {
			return gfx.EventResult{Handled: true, Repaint: true, Focus: codeInputID}
		}
		return gfx.EventResult{Handled: true, Repaint: true, Focus: richInputID}
	}
	if !rich.virtualLiteral && !state.stageM2Literal(start, end, value, rich.input.Caret(), markdownedit.Paragraph) {
		return gfx.EventResult{}
	}
	replacement := value + newline + newline
	position := start + len(replacement)
	target := &renderedVirtualState{kind: markdownedit.Paragraph, start: position, end: position, insert: newline + newline}
	if state.applyM2Patch(start, state.rich.virtualEnd, replacement, state.rich.index+1, 0, rich.input.Caret(), target) {
		return gfx.EventResult{Handled: true, Repaint: true, Focus: state.focusRequest}
	}
	return gfx.EventResult{}
}

func (state *model) m2TextResult() app.TextState {
	result := renderedHistoryInputState(state.rich.input)
	result.Redraw = true
	return result
}
