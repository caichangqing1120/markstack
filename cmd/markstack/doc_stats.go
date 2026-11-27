package main

import (
	"fmt"
	"sort"
	"unicode"
	"unicode/utf8"

	"github.com/vivaui/viva/text"

	"markstack/internal/workspace"
)

const statsStride = 256

type documentStats struct {
	source                 string
	characters, lines, cjk int
	offsets                []int
	lineStarts             []int
	words                  []text.Range
}

func (stats *documentStats) update(source string) {
	if stats.lines != 0 && stats.source == source {
		return
	}
	stats.source = source
	stats.characters, stats.lines, stats.cjk = 0, 1, 0
	stats.offsets, stats.words = stats.offsets[:0], stats.words[:0]
	stats.lineStarts = append(stats.lineStarts[:0], 0)
	start := -1
	lastEnd := 0
	for offset, character := range source {
		if stats.characters%statsStride == 0 {
			stats.offsets = append(stats.offsets, offset)
		}
		stats.characters++
		if character == '\n' || character == '\r' && (offset+1 == len(source) || source[offset+1] != '\n') {
			stats.lines++
			stats.lineStarts = append(stats.lineStarts, offset+1)
		}
		if cjkLetter(character) {
			if start >= 0 {
				stats.words = append(stats.words, text.Range{Start: start, End: lastEnd})
				start = -1
			}
			stats.cjk++
			stats.words = append(stats.words, text.Range{Start: offset, End: offset + utf8.RuneLen(character)})
		} else if wordLetter(character) || start >= 0 && unicode.IsMark(character) {
			if start < 0 {
				start = offset
			}
			lastEnd = offset + utf8.RuneLen(character)
		} else if start >= 0 {
			if character == '\'' || character == '’' {
				next, _ := utf8.DecodeRuneInString(source[offset+utf8.RuneLen(character):])
				if wordLetter(next) && !cjkLetter(next) {
					continue
				}
			}
			stats.words = append(stats.words, text.Range{Start: start, End: lastEnd})
			start = -1
		}
	}
	if start >= 0 {
		stats.words = append(stats.words, text.Range{Start: start, End: lastEnd})
	}
}

func (stats *documentStats) lineAt(offset int) int {
	return sort.Search(len(stats.lineStarts), func(index int) bool { return stats.lineStarts[index] > offset }) - 1
}

func wordLetter(character rune) bool {
	if character < utf8.RuneSelf {
		return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9'
	}
	return unicode.IsLetter(character) || unicode.IsNumber(character)
}

func cjkLetter(character rune) bool {
	return character >= 0x2e80 && unicode.IsLetter(character) && unicode.In(character, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul, unicode.Bopomofo)
}

func (stats *documentStats) byteAt(index int) int {
	index = max(0, index)
	if index >= stats.characters {
		return len(stats.source)
	}
	base := stats.offsets[index/statsStride]
	return base + byteOffsetOfRune(stats.source[base:], index%statsStride)
}

func (stats *documentStats) selected(start, end int) int {
	if start >= end {
		return 0
	}
	first := sort.Search(len(stats.words), func(index int) bool { return stats.words[index].End > start })
	last := sort.Search(len(stats.words), func(index int) bool { return stats.words[index].Start >= end })
	return last - first
}

func (state *model) statisticsLabel() string {
	current := state.session.Current()
	if current == nil || current.Kind != workspace.MarkdownKind || current.ReadOnly() {
		return ""
	}
	stats := &state.stats
	stats.update(current.Editor.CommittedString())
	words := fmt.Sprintf("%d 字", len(stats.words))
	selected := -1
	if state.mode == renderedMode {
		var editor *text.Editor
		switch {
		case state.selection.active:
			state.selectionStats.update(state.selection.visible)
			selected = len(state.selectionStats.words)
		case state.codeBlock.active:
			editor = state.codeBlock.input
		case state.table.active:
			editor = state.table.input
		case state.rich.active:
			editor = state.rich.input
		}
		if editor != nil && !editor.CompositionActive() {
			selection := editor.Selection()
			if selection.Start < selection.End {
				state.selectionStats.update(editor.CommittedString())
				selected = state.selectionStats.selected(state.selectionStats.byteAt(selection.Start), state.selectionStats.byteAt(selection.End))
			}
		}
	} else if state.mode != readMode && !current.Composing() {
		selection := current.Editor.Selection()
		if selection.Start < selection.End {
			selected = stats.selected(stats.byteAt(selection.Start), stats.byteAt(selection.End))
		}
	}
	if selected >= 0 {
		words = fmt.Sprintf("选中 %d / %s", selected, words)
	}
	reading := "0 分钟"
	if len(stats.words) > 0 {
		units := stats.cjk*5 + (len(stats.words)-stats.cjk)*8
		if units < 2000 {
			reading = "<1 分钟"
		} else {
			reading = fmt.Sprintf("约 %d 分钟", (units+1000)/2000)
		}
	}
	return fmt.Sprintf("%s  ·  %d 字符  ·  %d 行  ·  %s", words, stats.characters, stats.lines, reading)
}
