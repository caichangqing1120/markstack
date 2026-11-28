package main

import (
	"strings"
	"unicode"

	"github.com/vivaui/viva/markdown"

	"markstack/internal/markdownedit"
)

func specialSourceBlockIndex(source string, document *markdown.Document, renderedIndex int, blocks []markdownedit.Block) int {
	if renderedIndex < 0 || renderedIndex >= len(document.Blocks) {
		return -1
	}
	if len(blocks) == len(document.Blocks) && specialBlockMatches(source, document, renderedIndex, blocks[renderedIndex]) {
		return renderedIndex
	}
	match := -1
	for index, block := range blocks {
		if specialBlockMatches(source, document, renderedIndex, block) {
			if match >= 0 {
				return -1
			}
			match = index
		}
	}
	if match < 0 {
		return -1
	}
	if blocks[match].Code.Math {
		return match
	}
	return specialCommentSourceIndex(source, document.Blocks[renderedIndex], blocks, match)
}

func specialCommentSourceIndex(source string, rendered markdown.Block, blocks []markdownedit.Block, match int) int {
	var masked []byte
	sourceCount, maskedIndex := 0, -1
	for index, block := range blocks {
		if block.Comment {
			if block.Start < 0 || block.End <= block.Start || block.End > len(source) || block.Start < rendered.End && block.End > rendered.Start {
				return -1
			}
			if masked == nil {
				masked = []byte(source)
			}
			for offset := block.Start; offset < block.End; offset++ {
				if masked[offset] != '\r' && masked[offset] != '\n' {
					masked[offset] = ' '
				}
			}
			continue
		}
		if index == match {
			maskedIndex = sourceCount
		}
		sourceCount++
	}
	if masked == nil || maskedIndex < 0 {
		return -1
	}
	var projection markdown.Document
	projection.Parse(string(masked))
	if len(projection.Blocks) != sourceCount {
		return -1
	}
	projected := projection.Blocks[maskedIndex]
	if projected.Kind != rendered.Kind || projected.Start != rendered.Start || projected.End != rendered.End {
		return -1
	}
	return match
}

func specialBlockMatches(source string, document *markdown.Document, renderedIndex int, block markdownedit.Block) bool {
	rendered := &document.Blocks[renderedIndex]
	if rendered.Quote != 0 {
		return false
	}
	start, end := block.Start, block.End
	switch rendered.Kind {
	case markdown.Code:
		code := block.Code
		if block.Kind != markdownedit.FencedCode || !code.Editable || code.Start < 1 || code.End < code.Start || code.End > len(source) {
			return false
		}
		if code.Math {
			return rendered.Start == start && rendered.End == end && rendered.Info == "LaTeX" && rendered.Text == code.Visible
		}
		start, end = code.Start-1, code.End
	case markdown.Table:
		if block.Kind != markdownedit.Table || len(block.Cells) == 0 || rendered.Columns <= 0 || len(block.Cells) != len(document.Cells(rendered)) {
			return false
		}
	default:
		return false
	}
	if start < 0 || end < start || end > len(source) {
		return false
	}
	lineStart := strings.LastIndexByte(source[:start], '\n') + 1
	if lineStart != rendered.Start {
		return false
	}
	lineEnd := sourceLineEnd(source, end)
	if lineEnd > lineStart && source[lineEnd-1] == '\r' {
		lineEnd--
	}
	if lineEnd != rendered.End {
		return false
	}
	return rendered.Kind != markdown.Code || strings.ReplaceAll(rendered.Text, "\r\n", "\n") == block.Code.Visible
}

func sourceLineEnd(source string, offset int) int {
	if next := strings.IndexByte(source[offset:], '\n'); next >= 0 {
		return offset + next
	}
	return len(source)
}

func renderedTableCellSourceMatches(source string, rendered *markdown.Block, cell markdownedit.Cell) bool {
	if cell.Start < rendered.Start || cell.End < cell.Start || cell.End > rendered.End || rendered.End > len(source) {
		return false
	}
	lineStart := strings.LastIndexByte(source[:cell.Start], '\n') + 1
	if lineStart < rendered.Start {
		return false
	}
	row := strings.Count(source[rendered.Start:lineStart], "\n")
	if row == 1 || row > 1 && row-1 != cell.Row || row == 0 && cell.Row != 0 {
		return false
	}
	line := source[lineStart:sourceLineEnd(source, lineStart)]
	trimmed := strings.TrimSpace(line)
	position := lineStart + len(line) - len(strings.TrimLeftFunc(line, unicode.IsSpace))
	if strings.HasPrefix(trimmed, "|") {
		trimmed, position = trimmed[1:], position+1
	}
	column, start := 0, 0
	for end := 0; end <= len(trimmed); end++ {
		if end < len(trimmed) {
			switch trimmed[end] {
			case '\\':
				end++
				continue
			case '|':
			default:
				continue
			}
		} else if strings.TrimSpace(trimmed[start:end]) == "" {
			return false
		}
		if column == cell.Column {
			value := trimmed[start:end]
			from := position + end - len(strings.TrimLeftFunc(value, unicode.IsSpace))
			return cell.Start == from && cell.End == from+len(strings.TrimSpace(value))
		}
		column, start = column+1, end+1
	}
	return false
}
