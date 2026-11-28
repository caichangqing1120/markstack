package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
	"github.com/vivaui/viva/text"
	"markstack/internal/markdownedit"
	"markstack/internal/session"
	"markstack/internal/workspace"
)

type formatCommand int

const (
	cmdParagraph formatCommand = iota
	cmdHeading1
	cmdHeading2
	cmdHeading3
	cmdHeading4
	cmdHeading5
	cmdHeading6
	cmdPromote
	cmdDemote
	cmdQuote
	cmdBullet
	cmdOrdered
	cmdTask
	cmdToggleTask
	cmdIndent
	cmdOutdent
	cmdAbove
	cmdBelow
	cmdCodeBlock
	cmdMathBlock
	cmdLegacyTable
	cmdTable
	cmdFlowchart
	cmdSequence
	cmdRule
	cmdTOC
	cmdFrontMatter
	cmdFootnote
	cmdNote
	cmdTip
	cmdImportant
	cmdWarning
	cmdCaution
	cmdBold
	cmdItalic
	cmdUnderline
	cmdStrike
	cmdSubscript
	cmdSuperscript
	cmdHighlight
	cmdInlineCode
	cmdInlineMath
	cmdLink
	cmdImage
	cmdClear
	cmdUndo
	cmdRedo
	cmdSave
)

var formatCallouts = [...]string{"NOTE", "TIP", "IMPORTANT", "WARNING", "CAUTION"}

var formatWrappers = map[formatCommand][2]string{
	cmdBold: {"**", "**"}, cmdItalic: {"*", "*"}, cmdUnderline: {"<u>", "</u>"},
	cmdStrike: {"~~", "~~"}, cmdSubscript: {"<sub>", "</sub>"}, cmdSuperscript: {"<sup>", "</sup>"},
	cmdHighlight: {"==", "=="}, cmdInlineCode: {"`", "`"}, cmdInlineMath: {"$", "$"},
}

type formatChord struct {
	key       string
	modifiers gfx.Modifiers
}

var formatChords = map[formatChord]formatCommand{
	{"b", 0}: cmdBold, {"i", 0}: cmdItalic, {"u", 0}: cmdUnderline, {"`", 0}: cmdInlineCode,
	{"k", 0}: cmdLink, {"\\", 0}: cmdClear,
	{"x", gfx.ModifierShift}: cmdStrike, {"h", gfx.ModifierShift}: cmdHighlight,
	{"t", gfx.ModifierShift}: cmdToggleTask, {"i", gfx.ModifierShift}: cmdImage,
	{"]", gfx.ModifierAlt}: cmdPromote, {"[", gfx.ModifierAlt}: cmdDemote,
	{"c", gfx.ModifierAlt}: cmdCodeBlock, {"b", gfx.ModifierAlt}: cmdMathBlock,
	{"q", gfx.ModifierAlt}: cmdQuote, {"o", gfx.ModifierAlt}: cmdOrdered,
	{"u", gfx.ModifierAlt}: cmdBullet, {"x", gfx.ModifierAlt}: cmdTask, {"-", gfx.ModifierAlt}: cmdRule,
}

func (state *model) onFormatShortcut(event gfx.KeyEvent) gfx.EventResult {
	if !state.canFormatMarkdown() || state.fileMenuOpen || !state.formatMenu.trigger.IsZero() {
		return gfx.EventResult{}
	}
	if state.linkOpen && event.Target != linkInputID {
		return gfx.EventResult{}
	}
	switch event.Target {
	case searchID, workspaceQueryID, treeNameID, findInputID, replaceInputID, outlineFilterID, codeLanguageInputID:
		return gfx.EventResult{}
	}
	primary := primaryModifier()
	if event.Modifiers&primary == 0 {
		return gfx.EventResult{}
	}
	key := strings.ToLower(event.Text)
	command, ok := formatChords[formatChord{key, event.Modifiers &^ primary}]
	if event.Modifiers == primary && len(key) == 1 && key[0] >= '0' && key[0] <= '6' {
		command, ok = cmdParagraph+formatCommand(key[0]-'0'), true
	}
	if !ok {
		return gfx.EventResult{}
	}
	if state.refuseComposingFormat() {
		return gfx.EventResult{Handled: true, Repaint: true, Focus: state.focusRequest}
	}
	if event.Target == linkInputID {
		return gfx.EventResult{}
	}
	state.runFormat(command)
	focus := state.focusRequest
	if focus.IsZero() {
		focus = state.formatFocus()
	}
	return gfx.EventResult{Handled: true, Repaint: true, Focus: focus}
}

type formatViewTarget struct {
	index, cell int
	selection   text.Range
	virtual     *renderedVirtualState
}

type formatContext struct {
	document          *session.Document
	source            string
	start, end, caret int
	selected          text.Range
	block             markdownedit.Block
	blockFound        bool
	view              formatViewTarget
}

func (state *model) canFormatMarkdown() bool {
	current := state.session.Current()
	return current != nil && !current.ReadOnly() && current.Kind == workspace.MarkdownKind && state.mode != readMode
}

func (state *model) formatFocus() gfx.ID {
	if state.mode == editMode || state.mode == splitMode {
		return sourceID
	}
	switch {
	case state.codeBlock.active:
		return codeInputID
	case state.table.active:
		return tableInputID
	case state.rich.active:
		return richInputID
	}
	return gfx.ID{}
}

func (state *model) formatContext() formatContext {
	current := state.session.Current()
	source := current.Text()
	context := formatContext{document: current, source: source, view: formatViewTarget{index: -1, cell: -1}}
	context.selected = directedCodeSelection(current.Editor)
	context.caret = byteOffsetOfRune(source, current.Editor.Caret())
	if state.mode == renderedMode {
		index, input := state.renderedStructureInput()
		if input != nil {
			context.view.index, context.view.selection = index, directedCodeSelection(input)
			context.view.virtual = state.rich.virtualState()
			visible := input.String()
			rich := &state.rich
			switch {
			case state.codeBlock.active:
				context.block, context.blockFound = state.codeBlock.block, true
				context.caret = formatCodeOffset(source, state.codeBlock.block.Code, input.Caret())
			case state.table.active:
				context.block, context.blockFound = state.table.block, true
				context.view.cell = state.table.row*state.parsed.Blocks[index].Columns + state.table.column
				context.caret = state.table.cell.Start + byteOffsetOfRune(visible, input.Caret())
			case rich.virtual:
				context.caret = rich.virtualStart + byteOffsetOfRune(visible, input.Caret())
			case rich.active:
				context.block, context.blockFound = rich.block, true
				context.caret = rich.block.CaretSourceOffset(byteOffsetOfRune(visible, input.Caret()))
			}
			context.selected = formatCaretRange(source, context.caret)
		} else if state.selection.active && state.selection.editable {
			context.caret = state.selection.start
		}
	} else {
		for _, block := range markdownedit.Parse([]byte(source)) {
			if block.End > 0 && context.caret >= formatLineStart(source, block.Start) && context.caret <= formatLineEnd(source, block.End) {
				context.block, context.blockFound = block, true
				break
			}
		}
	}
	context.start, context.end = formatLineStart(source, context.caret), formatLineEnd(source, context.caret)
	if context.blockFound {
		context.start, context.end = formatLineStart(source, context.block.Start), formatLineEnd(source, context.block.End)
		if context.block.Kind == markdownedit.FencedCode && !context.block.Code.Math {
			context.start = formatLineStart(source, context.block.Code.Start-1)
			context.end = formatLineEnd(source, context.block.Code.End)
		}
	}
	if virtual := context.view.virtual; virtual != nil {
		context.start, context.end = formatLineStart(source, virtual.start), virtual.end-len(virtual.suffix)
	}
	return context
}

func formatCodeOffset(source string, code markdownedit.CodeBody, caret int) int {
	position := code.Start
	for caret > 0 && position < code.End {
		if strings.HasPrefix(source[position:], "\r\n") {
			position += 2
		} else {
			_, size := utf8.DecodeRuneInString(source[position:code.End])
			position += size
		}
		caret--
	}
	return position
}

func formatLineStart(source string, offset int) int {
	return strings.LastIndexByte(source[:max(0, min(offset, len(source)))], '\n') + 1
}

func formatLineEnd(source string, offset int) int {
	end := sourceLineEnd(source, max(0, min(offset, len(source))))
	if end > 0 && source[end-1] == '\r' {
		end--
	}
	return end
}

func formatCaretRange(source string, at int) text.Range {
	position := utf8.RuneCountInString(source[:max(0, min(at, len(source)))])
	return text.Range{Start: position, End: position}
}

var markdownBlockPrefix = regexp.MustCompile(`^(?:#{1,6}(?:[ \t]+|$)|>[ \t]?|(?:[-+*]|[0-9]{1,9}[.)])[ \t]+(?:\[[ xX]\][ \t]+)?)`)
var atxClosing = regexp.MustCompile(`[ \t]+#+[ \t]*$`)

func splitFormatPrefix(line string) (indent, prefix, body string) {
	body = strings.TrimLeft(line, " \t")
	indent = line[:len(line)-len(body)]
	prefix = markdownBlockPrefix.FindString(body)
	body = body[len(prefix):]
	return
}

func formatHeadingLevel(prefix string) int {
	return len(prefix) - len(strings.TrimLeft(prefix, "#"))
}

func (state *model) currentFormatHeading() int {
	if !state.canFormatMarkdown() {
		return 0
	}
	if state.mode == renderedMode && state.rich.active && state.rich.index >= 0 && state.rich.index < len(state.parsed.Blocks) && state.rich.block.Kind == markdownedit.Heading {
		return state.parsed.Blocks[state.rich.index].Level
	}
	current := state.session.Current()
	source := current.Text()
	if state.mode == renderedMode {
		return 0
	}
	caret := byteOffsetOfRune(source, current.Editor.Caret())
	_, prefix, _ := splitFormatPrefix(source[formatLineStart(source, caret):formatLineEnd(source, caret)])
	return formatHeadingLevel(prefix)
}

func (state *model) runFormat(command formatCommand) {
	if !state.canFormatMarkdown() || state.refuseComposingFormat() {
		return
	}
	state.headingMenuOpen = false
	if state.linkOpen {
		state.dismissLinkEditor()
		if command == cmdLink {
			return
		}
	}
	if command == cmdSave {
		state.saveActive(false)
		return
	}
	if command == cmdUndo || command == cmdRedo {
		state.formatHistory(command == cmdUndo)
		return
	}
	if _, inline := formatWrappers[command]; inline || command == cmdLink || command == cmdClear || command == cmdImage {
		if state.mode == renderedMode {
			state.prepareInlineShortcut()
		}
		state.formatInline(command)
		return
	}
	context := state.formatContext()
	switch {
	case command <= cmdTask:
		state.formatBlockPrefix(context, command)
	case command == cmdToggleTask:
		if context.block.Task {
			mark := context.block.TaskStart + 1
			value := "x"
			if context.block.Checked {
				value = " "
			}
			state.commitFormat(context, mark, mark+1, value, context.caret, context.start, context.block.Kind, -1)
		}
	case command == cmdIndent || command == cmdOutdent:
		if state.mode == renderedMode && state.rich.active {
			state.focusRequest = state.indentRenderedList(command == cmdOutdent).Focus
		} else {
			state.indentFormatSource(context, command == cmdOutdent)
		}
	case command == cmdLegacyTable:
		state.insertFormatTable(2, 2)
	case command == cmdTable:
		state.insertFormatTable(3, 3)
	default:
		state.insertFormatBlock(context, command)
	}
}

func (state *model) indentFormatSource(context formatContext, outdent bool) {
	patch, body, err := markdownedit.PatchListIndent([]byte(context.source), context.block, outdent)
	if err != nil {
		return
	}
	caret := body + max(0, context.caret-context.block.Start)
	state.commitFormat(context, patch.Start, patch.End, patch.Text, caret, 0, "", -1)
}

func (state *model) formatBlockPrefix(context formatContext, command formatCommand) {
	source, start := context.source, context.start
	if context.blockFound && (context.block.Kind == markdownedit.FencedCode || context.block.Kind == markdownedit.Table || context.block.Kind == markdownedit.Raw) {
		return
	}
	if state.mode != renderedMode && (!context.blockFound || context.block.Kind != markdownedit.Heading) {
		start = formatLineStart(source, context.caret)
	}
	end := formatLineEnd(source, start)
	indent, prefix, body := splitFormatPrefix(source[start:end])
	level := formatHeadingLevel(prefix)
	setext := context.blockFound && context.block.Kind == markdownedit.Heading && level == 0 && context.end > end
	if setext {
		if strings.HasPrefix(strings.TrimSpace(source[end:context.end]), "=") {
			level = 1
		} else {
			level = 2
		}
		end = context.end
	}
	if command == cmdPromote || command == cmdDemote {
		if command == cmdPromote {
			level = max(1, level-1)
		} else {
			level = (level + 1) % 7
		}
		command = cmdParagraph + formatCommand(level)
	}
	next, kind := "", markdownedit.Paragraph
	switch command {
	case cmdHeading1, cmdHeading2, cmdHeading3, cmdHeading4, cmdHeading5, cmdHeading6:
		next, kind = strings.Repeat("#", int(command-cmdParagraph))+" ", markdownedit.Heading
	case cmdQuote:
		next, kind = "> ", markdownedit.Quote
	case cmdBullet:
		next, kind = "- ", markdownedit.ListItem
	case cmdOrdered:
		next, kind = "1. ", markdownedit.ListItem
	case cmdTask:
		next, kind = "- [ ] ", markdownedit.ListItem
	}
	if !setext && (prefix == next && command != cmdQuote || command == cmdTask && context.block.Task) {
		return
	}
	if command == cmdQuote && strings.HasPrefix(prefix, ">") {
		next, kind = "", markdownedit.Paragraph
	}
	if level > 0 && !setext && kind != markdownedit.Heading {
		body = atxClosing.ReplaceAllString(body, "")
	}
	if kind == markdownedit.Heading || kind == markdownedit.Paragraph {
		if context.block.List != nil {
			indent = ""
		}
	}
	value := indent + next + body
	caret := start + len(indent+next) + min(len(body), max(0, context.caret-start-len(indent+prefix)))
	if !setext && value != source[start:end] && strings.HasSuffix(source[start:end], body) {
		end -= len(body)
		value = indent + next
	}
	state.commitFormat(context, start, end, value, caret, start, kind, -1)
}

func (state *model) insertFormatTable(rows, columns int) {
	if !state.canFormatMarkdown() || state.refuseComposingFormat() || rows < 1 || columns < 1 || rows > 6 || columns > 6 {
		return
	}
	context := state.formatContext()
	newline := context.document.Newline()
	var header strings.Builder
	for column := 1; column <= columns; column++ {
		fmt.Fprintf(&header, "| %s %d ", state.local("列", "Column"), column)
	}
	value := header.String() + "|" + newline + strings.Repeat("| --- ", columns) + "|"
	value += strings.Repeat(newline+strings.Repeat("|     ", columns)+"|", rows-1)
	start, end, before, after := formatInsertion(context)
	at := start + len(before)
	state.commitFormat(context, start, end, before+value+after, at+2, at, markdownedit.Table, 0)
}

func formatInsertion(context formatContext) (start, end int, before, after string) {
	newline := context.document.Newline()
	if strings.TrimSpace(context.source[context.start:context.end]) == "" {
		if context.start > 0 {
			previousEnd := context.start - 1
			previousStart := formatLineStart(context.source, previousEnd)
			if strings.TrimSpace(context.source[previousStart:previousEnd]) != "" {
				before = newline
			}
		}
		return context.start, context.end, before, formatBlockSuffix(context.source[context.end:], newline)
	}
	return context.end, context.end, newline + newline, formatBlockSuffix(context.source[context.end:], newline)
}

func formatBlockSuffix(rest, newline string) string {
	if rest == "" {
		return ""
	}
	if strings.HasPrefix(rest, newline) {
		after, _ := strings.CutPrefix(rest, newline)
		if strings.TrimSpace(after) == "" || strings.TrimSpace(after[:formatLineEnd(after, 0)]) == "" {
			return ""
		}
		return newline
	}
	return newline + newline
}

func (state *model) insertFormatBlock(context formatContext, command formatCommand) {
	source, newline := context.source, context.document.Newline()
	start, end, before, after := formatInsertion(context)
	value, kind, offset := "", markdownedit.Kind(""), 0
	switch command {
	case cmdCodeBlock:
		body := ""
		if context.block.Kind != markdownedit.FencedCode && context.block.Kind != markdownedit.Table && context.block.Kind != markdownedit.Raw {
			start, end, before = context.start, context.end, ""
			after = formatBlockSuffix(source[end:], newline)
			body = source[start:end]
			if context.block.Editable && context.block.Kind != markdownedit.Paragraph && !strings.ContainsAny(body, "\r\n") {
				_, _, body = splitFormatPrefix(body)
			}
			if state.mode != renderedMode && context.selected.Start != context.selected.End {
				from, to := min(context.selected.Start, context.selected.End), max(context.selected.Start, context.selected.End)
				selectionStart, selectionEnd := byteOffsetOfRune(source, from), byteOffsetOfRune(source, to)
				start, end = formatLineStart(source, selectionStart), formatLineEnd(source, selectionEnd)
				if selectionEnd == formatLineStart(source, selectionEnd) && selectionEnd > start {
					end = formatLineEnd(source, selectionEnd-len(newline))
				}
				body, after = source[start:end], formatBlockSuffix(source[end:], newline)
			}
		}
		fence := "```"
		for strings.Contains(body, fence) {
			fence += "`"
		}
		value, kind, offset = fence+newline+body+newline+fence, markdownedit.FencedCode, len(fence+newline)
	case cmdMathBlock:
		value, kind, offset = "$$"+newline+newline+"$$", markdownedit.FencedCode, len("$$"+newline)
	case cmdFlowchart, cmdSequence:
		starter := "flowchart TD" + newline + "    A[Start] --> B[End]"
		if command == cmdSequence {
			starter = "sequenceDiagram" + newline + "    Alice->>Bob: Hello"
		}
		opening := "```mermaid" + newline
		value, kind, offset = opening+starter+newline+"```", markdownedit.FencedCode, len(opening)
	case cmdRule:
		value = "---"
	case cmdTOC:
		value = "[TOC]"
	case cmdAbove, cmdBelow:
		if command == cmdAbove {
			start, end, before, after = context.start, context.start, "", ""
			value = newline + newline
		} else {
			start = context.end
			separation := 0
			for separation < 2 && strings.HasPrefix(source[start:], newline) {
				start += len(newline)
				separation++
			}
			end, before, after = start, strings.Repeat(newline, 2-separation), ""
			if start < len(source) {
				value = newline + newline
			}
		}
		kind = markdownedit.Paragraph
	case cmdFrontMatter:
		first := formatLineEnd(source, 0)
		if strings.TrimSpace(source[:first]) == "---" && frontMatterEnd(source, first) >= 0 {
			return
		}
		start, end, before, after = 0, 0, "", ""
		value = strings.Join([]string{"---", "title: ", "date: ", "---", "", ""}, newline)
		offset = len("---" + newline + "title: ")
	case cmdFootnote:
		state.insertFormatFootnote(context)
		return
	default:
		if command < cmdNote || command > cmdCaution {
			return
		}
		value = "> [!" + formatCallouts[command-cmdNote] + "]" + newline + "> " + state.local("内容", "Content")
	}
	at := start + len(before)
	state.commitFormat(context, start, end, before+value+after, at+offset, at, kind, -1)
}

func frontMatterEnd(source string, firstEnd int) int {
	for start := sourceLineEnd(source, firstEnd) + 1; start < len(source); {
		end := formatLineEnd(source, start)
		if strings.TrimSpace(source[start:end]) == "---" {
			return end
		}
		if !strings.Contains(source[start:end], ":") {
			break
		}
		start = sourceLineEnd(source, end) + 1
	}
	return -1
}

func (state *model) insertFormatFootnote(context formatContext) {
	source, newline := context.source, context.document.Newline()
	start := context.caret
	if context.block.Kind == markdownedit.FencedCode || context.block.Kind == markdownedit.Table {
		start = context.end
	}
	if frontEnd := formatLineEnd(source, start); start < frontEnd && source[start] == '\r' {
		start = frontEnd
	}
	number := 1
	for strings.Contains(source, "[^"+strconv.Itoa(number)+"]") {
		number++
	}
	ref := "[^" + strconv.Itoa(number) + "]"
	gap := newline + newline
	if strings.HasSuffix(source, gap) || source == "" {
		gap = ""
	} else if strings.HasSuffix(source, newline) {
		gap = newline
	}
	if start == len(source) && gap == "" {
		gap = newline + newline
	}
	value := ref + source[start:] + gap + ref + ": " + state.local("脚注", "Footnote") + newline
	state.commitFormat(context, start, len(source), value, start+len(ref), start, "", -1)
}

func (state *model) commitFormat(context formatContext, start, end int, value string, caret, targetByte int, kind markdownedit.Kind, cell int) {
	if start < 0 || end < start || end > len(context.source) || context.document != state.session.Current() || context.document.Text() != context.source {
		return
	}
	if context.source[start:end] == value {
		return
	}
	next := context.source[:start] + value + context.source[end:]
	selection := formatCaretRange(next, caret)
	if state.mode != renderedMode {
		if err := applyMarkdownPatch(context.document, markdownedit.SourcePatch{Start: start, End: end, Text: value}, context.selected, selection); err != nil {
			state.message = err.Error()
		}
		state.focusRequest = sourceID
		state.invalidator.Invalidate()
		return
	}
	var parsed markdown.Document
	formulas, display := parseTechnicalMarkdown(&parsed, next, true)
	blocks := markdownedit.Parse([]byte(next))
	normalizeRenderedTasks(&parsed, blocks)
	target := formatTargetAt(&parsed, blocks, next, targetByte, caret, kind, cell)
	if (kind == markdownedit.FencedCode || kind == markdownedit.Table) && target.index < 0 {
		state.message = state.local("无法安全创建此块；原文未修改", "Could not create this block safely; source is unchanged")
		return
	}
	if target.index < 0 && context.block.Editable && (end <= context.block.Start || start >= context.block.End) {
		anchor := context.block.Start
		if end <= anchor {
			anchor += len(value) - (end - start)
		}
		target = formatTargetAt(&parsed, blocks, next, anchor, anchor, context.block.Kind, -1)
		if target.index >= 0 {
			target.selection = context.view.selection
		}
	}
	if target.index >= 0 && target.virtual == nil && target.cell < 0 && context.view.index >= 0 && context.view.virtual == nil && context.block.Editable && kind != "" {
		mapping := renderedMapping(&parsed, blocks)
		if index := mapping[target.index]; index >= 0 && blocks[index].Visible == context.block.Visible {
			target.selection = context.view.selection
		}
	}
	current := context.document
	history, position := state.renderedHistorySnapshot(current)
	if err := applyMarkdownPatch(current, markdownedit.SourcePatch{Start: start, End: end, Text: value}, context.selected, selection); err != nil {
		state.message = err.Error()
		return
	}
	state.parsed, state.preview.formulas, state.preview.displayMath = parsed, formulas, display
	state.preview.layoutKey = previewLayoutKey{}
	state.restoreFormatTarget(target)
	rich := &state.rich
	rich.renderedHistory, rich.sourceHistoryOffset = history, position+1
	rich.crossRedo, rich.structureRedo = nil, nil
	beforeView := context.view
	rich.structureUndo = appendRenderedHistory(rich.structureUndo, renderedStructureEdit{
		beforeSourceHash: sha256.Sum256([]byte(context.source)), afterSourceHash: sha256.Sum256([]byte(next)), position: position + 1,
		beforeIndex: beforeView.index, afterIndex: target.index, beforeVirtual: beforeView.virtual, afterVirtual: target.virtual,
		formatBefore: &beforeView, formatAfter: &target,
	})
	rich.trimHistory(current)
	state.invalidator.Invalidate()
}

func formatTargetAt(parsed *markdown.Document, blocks []markdownedit.Block, source string, at, caret int, kind markdownedit.Kind, cell int) formatViewTarget {
	target := formatViewTarget{index: -1, cell: cell}
	for index, rendered := range parsed.Blocks {
		if at < rendered.Start || at > rendered.End {
			continue
		}
		if kind == markdownedit.Table || kind == markdownedit.FencedCode {
			for _, block := range blocks {
				if block.Kind == kind && specialBlockMatches(source, parsed, index, block) {
					target.index = index
					if kind == markdownedit.FencedCode {
						target.selection = formatCaretRange(block.Code.Visible, 0)
					}
					return target
				}
			}
		}
	}
	if kind == markdownedit.Table || kind == markdownedit.FencedCode {
		return target
	}
	for index, blockIndex := range renderedMapping(parsed, blocks) {
		if blockIndex < 0 {
			continue
		}
		rendered, block := parsed.Blocks[index], blocks[blockIndex]
		if at != formatLineStart(source, block.Start) && (at < rendered.Start || at > rendered.End) {
			continue
		}
		if kind != "" && block.Kind != kind {
			continue
		}
		position := 0
		for position < len(block.Visible) && block.CaretSourceOffset(position) < caret {
			_, size := utf8.DecodeRuneInString(block.Visible[position:])
			position += size
		}
		target.index, target.selection = index, formatCaretRange(block.Visible, position)
		return target
	}
	if kind == markdownedit.Paragraph || kind == markdownedit.Heading || kind == markdownedit.Quote || kind == markdownedit.ListItem {
		end := formatLineEnd(source, at)
		indent, prefix, body := splitFormatPrefix(source[at:end])
		if body == "" {
			index := 0
			for index < len(parsed.Blocks) && parsed.Blocks[index].Start < at {
				index++
			}
			target.index = index
			target.virtual = &renderedVirtualState{kind: kind, start: at + len(indent+prefix), end: end, insert: indent + prefix}
		}
	}
	return target
}

func (state *model) restoreFormatTarget(target formatViewTarget) {
	current := state.session.Current()
	state.deactivateRenderedView(current)
	state.codeBlock, state.table = codeBlockEditor{}, tableEditor{}
	state.focusRequest = gfx.ID{}
	if target.index < 0 {
		return
	}
	if target.cell > 0 && target.index < len(state.parsed.Blocks) && state.parsed.Blocks[target.index].Kind == markdown.Table {
		hit := target.cell
		for index := range target.index {
			if block := &state.parsed.Blocks[index]; block.Kind == markdown.Table {
				hit += len(state.parsed.Cells(block))
			}
		}
		state.focusRequest = state.activateTableCell(hit).Focus
	} else {
		state.restoreRenderedStructureView(target.index, 0, target.virtual)
	}
	if _, input := state.renderedStructureInput(); input != nil && state.mode == renderedMode {
		input.SetSelection(target.selection)
		state.historyReveal = true
		return
	}
	state.mode, state.focusRequest = renderedMode, gfx.ID{}
}

func (state *model) formatHistory(undo bool) {
	if state.mode == renderedMode {
		modifiers := primaryModifier()
		if !undo {
			modifiers |= gfx.ModifierShift
		}
		state.onRenderedDocumentHistoryShortcut(gfx.KeyEvent{Text: "z", Modifiers: modifiers})
	} else {
		state.stepMarkdownEdit(state.session.Current(), undo)
		state.focusRequest = sourceID
	}
	state.invalidator.Invalidate()
}

func (state *model) formatInline(command formatCommand) {
	context := state.formatContext()
	source := context.source
	start, end := byteOffsetOfRune(source, min(context.selected.Start, context.selected.End)), byteOffsetOfRune(source, max(context.selected.Start, context.selected.End))
	if state.mode == renderedMode {
		if state.selection.active && state.selection.editable {
			start, end = state.selection.start, state.selection.end
		} else if state.rich.active && !state.rich.virtual && state.rich.block.Editable {
			input, block := state.rich.input, state.rich.block
			selected := input.Selection()
			from, to := byteOffsetOfRune(block.Visible, selected.Start), byteOffsetOfRune(block.Visible, selected.End)
			if command == cmdLink && block.Kind == markdownedit.Paragraph {
				state.openLinkShortcut()
				return
			}
			if from == to || from == 0 && to == len(block.Visible) {
				start, end = block.Start, block.End
			} else {
				var startErr, endErr error
				start, startErr = markdownedit.CrossBlockBoundary([]byte(source), block, from, false)
				end, endErr = markdownedit.CrossBlockBoundary([]byte(source), block, to, true)
				if startErr != nil || endErr != nil || end < start {
					state.message = state.local("选区跨越格式边界", "Selection crosses a formatting boundary")
					return
				}
			}
			if marker := formatWrappers[command][0]; marker == "**" || marker == "*" || marker == "~~" || marker == "`" {
				if from == to {
					from, to = 0, len(block.Visible)
				}
				if next, err := markdownedit.ToggleInline([]byte(source), block, from, to, marker); err == nil {
					patch := formatStringPatch(source, string(next))
					state.commitFormat(context, patch.Start, patch.End, patch.Text, context.caret, context.start, block.Kind, -1)
					return
				}
				if block.Kind == markdownedit.Paragraph && selected.Start != selected.End {
					state.message = state.local("选区跨越格式边界", "Selection crosses a formatting boundary")
					return
				}
			}
		} else if command == cmdLink && state.rich.active && state.rich.virtual && state.rich.input.String() == "" {
			state.openLinkEditor()
			return
		} else if state.codeBlock.active || state.table.active {
			if command == cmdClear {
				return
			}
			state.message = state.local("此编辑区域不支持行内格式", "Inline formatting is not available inside this editor")
			return
		}
	}
	if command == cmdImage {
		path, err := state.pickMarkdownImage(context.document)
		if err != nil && !errors.Is(err, app.ErrCanceled) {
			state.message = err.Error()
		}
		if err == nil && path != "" {
			if state.mode == renderedMode && state.rich.active {
				start, end = context.caret, context.caret
			}
			value := "![" + state.local("图片", "Image") + "](" + path + ")"
			state.commitFormat(context, start, end, value, start+len(value), context.start, "", -1)
		}
		return
	}
	if start == end && command == cmdClear {
		lineStart, lineEnd := formatLineStart(source, context.caret), formatLineEnd(source, context.caret)
		indent, prefix, _ := splitFormatPrefix(source[lineStart:lineEnd])
		start, end = lineStart+len(indent+prefix), lineEnd
	}
	if command == cmdLink {
		if start == end {
			lineStart, lineEnd := formatLineStart(source, context.caret), formatLineEnd(source, context.caret)
			indent, prefix, _ := splitFormatPrefix(source[lineStart:lineEnd])
			start, end = lineStart+len(indent+prefix), lineEnd
		}
		state.openLinkEditor()
		selection := text.Range{Start: formatCaretRange(source, start).Start, End: formatCaretRange(source, end).Start}
		state.sourceLink = sourceLinkSelection{document: context.document, source: source, selection: selection, context: &context}
		return
	}
	if command == cmdClear {
		start, end = enclosingInlineFormat(source, start, end)
		cleared := clearFormatText(source[start:end])
		state.commitFormat(context, start, end, cleared, start+len(cleared), context.start, context.block.Kind, -1)
		return
	}
	value := source[start:end]
	open, close := formatWrappers[command][0], formatWrappers[command][1]
	if strings.ContainsAny(value, "\r\n") {
		return
	}
	trimmed := strings.TrimSpace(value)
	if trimmed != "" {
		start += strings.Index(value, trimmed)
		end, value = start+len(trimmed), trimmed
	}
	var replacement string
	if len(value) >= len(open)+len(close) && strings.HasPrefix(value, open) && strings.HasSuffix(value, close) && !(command == cmdItalic && strings.HasPrefix(value, "**")) {
		replacement = value[len(open) : len(value)-len(close)]
	} else if start >= len(open) && end+len(close) <= len(source) && source[start-len(open):start] == open && source[end:end+len(close)] == close && !insideBolderFormat(source, start, end, command) {
		start, end, replacement = start-len(open), end+len(close), value
	} else {
		if command == cmdInlineCode {
			for strings.Contains(value, open) {
				open += "`"
			}
			close = open
			if strings.HasPrefix(value, "`") || strings.HasSuffix(value, "`") {
				open, close = open+" ", " "+close
			}
		}
		replacement = open + value + close
	}
	caret := start + len(replacement)
	if value == "" {
		caret = start + len(open)
	} else if context.view.index >= 0 {
		caret = min(caret, context.caret+len(replacement)-(end-start))
	}
	if state.mode != renderedMode && start != end && context.selected.Start != context.selected.End {
		next := source[:start] + replacement + source[end:]
		inner := strings.Index(replacement, value)
		if inner < 0 {
			inner, value = 0, replacement
		}
		selection := text.Range{Start: formatCaretRange(next, start+inner).Start, End: formatCaretRange(next, start+inner+len(value)).Start}
		if context.selected.Start > context.selected.End {
			selection.Start, selection.End = selection.End, selection.Start
		}
		if err := applyMarkdownPatch(context.document, markdownedit.SourcePatch{Start: start, End: end, Text: replacement}, context.selected, selection); err != nil {
			state.message = err.Error()
		}
		state.focusRequest = sourceID
		state.invalidator.Invalidate()
		return
	}
	state.commitFormat(context, start, end, replacement, caret, context.start, context.block.Kind, -1)
}

func insideBolderFormat(source string, start, end int, command formatCommand) bool {
	return command == cmdItalic && start >= 2 && end+2 <= len(source) && source[start-2:start] == "**" && source[end:end+2] == "**"
}

func formatStringPatch(before, after string) markdownedit.SourcePatch {
	start, oldEnd, newEnd := 0, len(before), len(after)
	for start < oldEnd && start < newEnd {
		original, size := utf8.DecodeRuneInString(before[start:])
		next, _ := utf8.DecodeRuneInString(after[start:])
		if original != next {
			break
		}
		start += size
	}
	for oldEnd > start && newEnd > start {
		original, size := utf8.DecodeLastRuneInString(before[:oldEnd])
		next, _ := utf8.DecodeLastRuneInString(after[:newEnd])
		if original != next {
			break
		}
		oldEnd, newEnd = oldEnd-size, newEnd-size
	}
	return markdownedit.SourcePatch{Start: start, End: oldEnd, Text: after[start:newEnd]}
}

var clearMarkup = []*regexp.Regexp{
	regexp.MustCompile(`(^|[^!\\])\[([^\]\r\n]+)\]\([^\s()]+\)`),
	regexp.MustCompile(`(^|[^\\])(?:\*\*\*|___)([^\r\n]+?)(?:\*\*\*|___)`),
	regexp.MustCompile(`(^|[^\\])\*\*([^\r\n]+?)\*\*`), regexp.MustCompile(`(^|[^\\])__([^\r\n]+?)__`),
	regexp.MustCompile(`(^|[^\\])\*([^*\r\n]+)\*`), regexp.MustCompile(`(^|[^\\\w])_([^_\r\n]+)_`),
	regexp.MustCompile(`(^|[^\\])~~([^\r\n]+?)~~`), regexp.MustCompile(`(^|[^\\])==([^\r\n]+?)==`),
	regexp.MustCompile(`(^|[^\\])<u>([^\r\n]+?)</u>`), regexp.MustCompile(`(^|[^\\])<sub>([^\r\n]+?)</sub>`),
	regexp.MustCompile(`(^|[^\\])<sup>([^\r\n]+?)</sup>`),
}

var clearCodeSpans = regexp.MustCompile("`+")
var clearLinkTail = regexp.MustCompile(`^\]\((?:[^\s()\\]|\\.)+\)`)

func enclosingInlineFormat(source string, start, end int) (int, int) {
	for {
		before := start
		if start > 0 && source[start-1] == '[' && (start < 2 || !strings.ContainsRune(`!\`, rune(source[start-2]))) {
			if tail := clearLinkTail.FindString(source[end:]); tail != "" {
				start, end = start-1, end+len(tail)
			}
		}
		for _, pair := range [][2]string{{"***", "***"}, {"___", "___"}, {"**", "**"}, {"__", "__"}, {"~~", "~~"}, {"==", "=="}, {"<u>", "</u>"}, {"<sup>", "</sup>"}, {"<sub>", "</sub>"}, {"`", "`"}, {"*", "*"}, {"_", "_"}} {
			from, to := start-len(pair[0]), end+len(pair[1])
			if from >= 0 && to <= len(source) && source[from:start] == pair[0] && source[end:to] == pair[1] && (from == 0 || source[from-1] != '\\') {
				start, end = from, to
				break
			}
		}
		if start == before {
			return start, end
		}
	}
}

func clearFormatText(value string) string {
	var result strings.Builder
	var restore []string
	marker := "\x00"
	for strings.Contains(value, marker) {
		marker += "\x00"
	}
	last := 0
	ranges := clearCodeSpans.FindAllStringIndex(value, -1)
	for index := 0; index < len(ranges); index++ {
		open := ranges[index]
		if open[0] > 0 && value[open[0]-1] == '\\' {
			continue
		}
		for closing := index + 1; closing < len(ranges); closing++ {
			close := ranges[closing]
			code := value[open[1]:close[0]]
			if strings.ContainsAny(code, "\r\n") {
				break
			}
			if open[1]-open[0] == close[1]-close[0] {
				token := marker + strconv.Itoa(index) + marker
				result.WriteString(value[last:open[0]])
				result.WriteString(token)
				if strings.HasPrefix(code, " ") && strings.HasSuffix(code, " ") && strings.TrimSpace(code) != "" {
					code = code[1 : len(code)-1]
				}
				restore = append(restore, token, code)
				last, index = close[1], closing
				break
			}
		}
	}
	result.WriteString(value[last:])
	return strings.NewReplacer(restore...).Replace(clearFormatMarkup(result.String()))
}

func clearFormatMarkup(value string) string {
	for {
		before := value
		for _, pattern := range clearMarkup {
			value = pattern.ReplaceAllString(value, "${1}${2}")
		}
		if value == before {
			return value
		}
	}
}
