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
	delta := 0
	changed := false
	for index, line := range lines {
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
				lines[index] = line[remove:]
				delta -= remove
				changed = true
			}
		} else {
			lines[index] = "\t" + line
			delta++
			changed = true
		}
	}
	if !changed {
		return
	}
	current.Editor.SetSelection(text.Range{
		Start: utf8.RuneCountInString(source[:lineStart]),
		End:   utf8.RuneCountInString(source[:lineEnd]),
	})
	current.Editor.Paste(strings.Join(lines, "\n"))
	if selection.Start != selection.End {
		current.Editor.SetSelection(text.Range{Start: selection.Start, End: selection.End + delta})
	} else {
		current.Editor.MoveTo(max(0, selection.Start+delta), false)
	}
}
