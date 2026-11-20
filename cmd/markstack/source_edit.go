package main

import (
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/text"
)

func (state *model) indentSource(outdent bool) {
	if !state.sourceReplaceAvailable() {
		return
	}
	current := state.session.Current()
	if _, composing := current.Editor.Composition(); composing {
		return
	}
	selection := current.Editor.Selection()
	if !outdent && selection.Start == selection.End {
		current.Editor.Paste("\t")
		return
	}
	source := current.Text()
	edits, after := sourceIndentReplacements(source, selection, current.Editor.Caret(), outdent)
	if len(edits) == 0 {
		return
	}
	replacement := compactSourceIndent(source, edits)
	if err := current.Editor.ReplaceRangesWith([]text.Replacement{replacement}, text.ReplaceRangesOptions{AfterSelection: &after}); err != nil {
		state.message = err.Error()
	}
}

func compactSourceIndent(source string, edits []text.Replacement) text.Replacement {
	if len(edits) == 1 {
		return edits[0]
	}
	start, end := edits[0].Range.Start, edits[len(edits)-1].Range.End
	source = source[byteOffsetOfRune(source, start):]
	position := start
	var replacement strings.Builder
	for _, edit := range edits {
		prefix := byteOffsetOfRune(source, edit.Range.Start-position)
		replacement.WriteString(source[:prefix])
		replacement.WriteString(edit.Text)
		source = source[prefix+byteOffsetOfRune(source[prefix:], edit.Range.End-edit.Range.Start):]
		position = edit.Range.End
	}
	return text.Replacement{Range: text.Range{Start: start, End: end}, Text: replacement.String()}
}

func sourceIndentReplacements(source string, selection text.Range, caret int, outdent bool) ([]text.Replacement, text.Range) {
	start := byteOffsetOfRune(source, selection.Start)
	end := byteOffsetOfRune(source, selection.End)
	lineStart := strings.LastIndexByte(source[:start], '\n') + 1
	last := end
	if last > start && source[last-1] == '\n' {
		last--
	}
	lineEnd := len(source)
	if next := strings.IndexByte(source[last:], '\n'); next >= 0 {
		lineEnd = last + next
	}
	lines := strings.Split(source[lineStart:lineEnd], "\n")
	position := utf8.RuneCountInString(source[:lineStart])
	var edits []text.Replacement
	for _, line := range lines {
		if outdent {
			remove := 0
			if strings.HasPrefix(line, "\t") {
				remove = 1
			} else {
				for remove < 4 && remove < len(line) && line[remove] == ' ' {
					remove++
				}
			}
			if remove > 0 {
				edits = append(edits, text.Replacement{Range: text.Range{Start: position, End: position + remove}})
			}
		} else {
			edits = append(edits, text.Replacement{Range: text.Range{Start: position, End: position}, Text: "\t"})
		}
		position += utf8.RuneCountInString(line) + 1
	}
	mapPosition := func(original int) int {
		delta := 0
		for _, edit := range edits {
			if original < edit.Range.Start {
				break
			}
			if original <= edit.Range.End {
				return edit.Range.Start + delta
			}
			delta += utf8.RuneCountInString(edit.Text) - (edit.Range.End - edit.Range.Start)
		}
		return original + delta
	}
	after := text.Range{Start: mapPosition(selection.Start), End: mapPosition(selection.End)}
	if caret == selection.Start {
		after.Start, after.End = after.End, after.Start
	}
	return edits, after
}
