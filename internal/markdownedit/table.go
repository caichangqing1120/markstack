package markdownedit

import (
	"bytes"
	"crypto/sha256"
	"slices"
	"strings"
)

type SourcePatch struct {
	Start, End int
	Text       string
}

func (patch SourcePatch) apply(source []byte) []byte {
	next := make([]byte, 0, len(source)-(patch.End-patch.Start)+len(patch.Text))
	next = append(next, source[:patch.Start]...)
	next = append(next, patch.Text...)
	return append(next, source[patch.End:]...)
}

type TableAction int

const (
	RowAbove TableAction = iota
	RowBelow
	RowDelete
	ColumnLeft
	ColumnRight
	ColumnDelete
	AlignLeft
	AlignCenter
	AlignRight
	TableDelete
	TableFormat
)

type TablePatch struct {
	SourcePatch
	Cell Cell
}

type tableLine struct {
	prefix, suffix string
	cells          []string
}

func tableLines(source []byte, block Block) ([]tableLine, string, int, bool) {
	if block.Kind != Table || block.Start < 0 || block.End <= block.Start || block.End > len(source) || len(block.Cells) == 0 {
		return nil, "", 0, false
	}
	if block.Start > 0 && source[block.Start-1] != '\n' && !(block.Start == 3 && bytes.Equal(source[:3], []byte("\xef\xbb\xbf"))) {
		return nil, "", 0, false
	}
	last := block.Cells[len(block.Cells)-1]
	columns := last.Column + 1
	if columns < 1 || len(block.Cells)%columns != 0 || last.Row != len(block.Cells)/columns-1 {
		return nil, "", 0, false
	}
	for _, cell := range block.Cells {
		if !cell.Editable || cell.Start < block.Start || cell.End > block.End {
			return nil, "", 0, false
		}
	}
	var lines []tableLine
	newline := ""
	remaining := string(source[block.Start:block.End])
	for {
		raw, rest, more := strings.Cut(remaining, "\n")
		if more {
			ending := "\n"
			if strings.HasSuffix(raw, "\r") {
				ending, raw = "\r\n", strings.TrimSuffix(raw, "\r")
			}
			if newline != "" && newline != ending {
				return nil, "", 0, false
			}
			newline = ending
		}
		if strings.ContainsAny(raw, "\\\r") {
			return nil, "", 0, false
		}
		body := strings.Trim(raw, " \t")
		left := len(raw) - len(strings.TrimLeft(raw, " \t"))
		if body == "" {
			return nil, "", 0, false
		}
		right := left + len(body)
		if raw[left] == '|' {
			left++
		}
		if right > left && raw[right-1] == '|' {
			right--
		}
		cells := strings.Split(raw[left:right], "|")
		if len(cells) != columns {
			return nil, "", 0, false
		}
		lines = append(lines, tableLine{raw[:left], raw[right:], cells})
		if !more {
			break
		}
		remaining = rest
	}
	return lines, newline, columns, newline != "" && len(lines) == last.Row+2
}

func CanEditTable(source []byte, block Block) bool {
	if sha256.Sum256(source) != block.source {
		return false
	}
	_, _, _, ok := tableLines(source, block)
	return ok
}

func PatchTable(source []byte, block Block, row, column int, action TableAction) (TablePatch, error) {
	if sha256.Sum256(source) != block.source {
		return TablePatch{}, ErrStaleBlock
	}
	lines, newline, columns, ok := tableLines(source, block)
	if !ok || row < 0 || row >= len(lines)-1 || column < 0 || column >= columns {
		return TablePatch{}, ErrReadOnlyBlock
	}
	patch := SourcePatch{Start: block.Start, End: block.End}
	if action == TableDelete || action == RowDelete && len(lines) == 2 || action == ColumnDelete && columns == 1 {
		if bytes.HasPrefix(source[patch.End:], []byte("\r\n")) {
			patch.End += 2
		} else if bytes.HasPrefix(source[patch.End:], []byte("\n")) {
			patch.End++
		}
		return TablePatch{SourcePatch: patch}, nil
	}
	separator := lines[1]
	rows := slices.Delete(lines, 1, 2)
	switch action {
	case RowAbove, RowBelow:
		indent := rows[row].prefix
		indent = indent[:len(indent)-len(strings.TrimLeft(indent, " \t"))]
		if action == RowBelow {
			row++
		}
		blank := tableLine{prefix: indent + "|", suffix: "|", cells: make([]string, columns)}
		for index := range blank.cells {
			blank.cells[index] = "   "
		}
		rows = slices.Insert(rows, row, blank)
	case RowDelete:
		rows = slices.Delete(rows, row, row+1)
		row = min(row, len(rows)-1)
	case ColumnLeft, ColumnRight:
		if action == ColumnRight {
			column++
		}
		for index := range rows {
			rows[index].cells = slices.Insert(rows[index].cells, column, "   ")
			if column == 0 && !strings.HasSuffix(rows[index].prefix, "|") {
				rows[index].prefix += "|"
			}
			if column == columns && !strings.HasPrefix(rows[index].suffix, "|") {
				rows[index].suffix = "|" + rows[index].suffix
			}
		}
		separator.cells = slices.Insert(separator.cells, column, " --- ")
		columns++
	case ColumnDelete:
		for index := range rows {
			rows[index].cells = slices.Delete(rows[index].cells, column, column+1)
			if columns == 2 {
				rows[index].ensureEdges()
			}
		}
		separator.cells = slices.Delete(separator.cells, column, column+1)
		columns--
		if columns == 1 {
			separator.ensureEdges()
		}
		column = min(column, columns-1)
	case AlignLeft, AlignCenter, AlignRight:
		old := separator.cells[column]
		marker := strings.Trim(strings.Trim(old, " \t"), ":")
		if action != AlignRight {
			marker = ":" + marker
		}
		if action != AlignLeft {
			marker += ":"
		}
		separator.cells[column] = old[:len(old)-len(strings.TrimLeft(old, " \t"))] + marker + old[len(strings.TrimRight(old, " \t")):]
	case TableFormat:
		for _, line := range append(rows, separator) {
			for index, cell := range line.cells {
				line.cells[index] = " " + strings.Trim(cell, " \t") + " "
			}
		}
		for index := range rows {
			rows[index].ensureEdges()
		}
		separator.ensureEdges()
	default:
		return TablePatch{}, ErrReadOnlyBlock
	}
	var replacement strings.Builder
	for index, line := range rows {
		if index != 0 {
			replacement.WriteString(newline)
		}
		line.write(&replacement)
		if index == 0 {
			replacement.WriteString(newline)
			separator.write(&replacement)
		}
	}
	patch.Text = replacement.String()
	for _, updated := range Parse(patch.apply(source)) {
		if updated.Kind == Table && updated.Start == patch.Start && updated.End == patch.Start+len(patch.Text) && len(updated.Cells) == len(rows)*columns {
			cell := updated.Cells[row*columns+column]
			if cell.Editable && cell.Row == row && cell.Column == column {
				return TablePatch{SourcePatch: patch, Cell: cell}, nil
			}
		}
	}
	return TablePatch{}, ErrReadOnlyBlock
}

func (line *tableLine) ensureEdges() {
	if !strings.HasSuffix(line.prefix, "|") {
		line.prefix += "|"
	}
	if !strings.HasPrefix(line.suffix, "|") {
		line.suffix = "|" + line.suffix
	}
}

func (line tableLine) write(out *strings.Builder) {
	out.WriteString(line.prefix)
	out.WriteString(strings.Join(line.cells, "|"))
	out.WriteString(line.suffix)
}
