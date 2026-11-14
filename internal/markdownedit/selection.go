package markdownedit

import (
	"bytes"
	"crypto/sha256"
	"unicode/utf8"
)

type CrossBlockEdge struct {
	Offset int
	Retain string
}

func VisibleRangeAtSource(source []byte, block Block, start, end int) (int, int, error) {
	if sha256.Sum256(source) != block.source || block.Start < 0 || block.End < block.Start || block.End > len(source) {
		return 0, 0, ErrStaleBlock
	}
	if !block.Editable || start < block.Start || end > block.End || start > end || !utf8.Valid(source[block.Start:start]) || !utf8.Valid(source[start:end]) {
		return 0, 0, ErrReadOnlyBlock
	}
	if !block.styled {
		if string(source[block.Start:block.End]) == block.Visible {
			return start - block.Start, end - block.Start, nil
		}
		return 0, 0, ErrReadOnlyBlock
	}
	if len(block.parts) == 0 {
		if block.textFrom >= block.Start && block.textTo <= block.End && block.textFrom <= start && end <= block.textTo && string(source[block.textFrom:block.textTo]) == block.Visible {
			return start - block.textFrom, end - block.textFrom, nil
		}
		return 0, 0, ErrReadOnlyBlock
	}
	for position, part := range block.parts {
		if part.sourceStart < block.Start || part.sourceEnd > block.End || part.sourceStart > part.sourceEnd || part.visibleStart < 0 || part.visibleEnd > len(block.Visible) || part.visibleStart > part.visibleEnd ||
			string(source[part.sourceStart:part.sourceEnd]) != block.Visible[part.visibleStart:part.visibleEnd] || start < part.sourceStart || end > part.sourceEnd {
			continue
		}
		from, to := part.visibleStart+start-part.sourceStart, part.visibleStart+end-part.sourceStart
		if start == end && position > 0 {
			previous := block.parts[position-1]
			if previous.visibleEnd == from && previous.sourceEnd != start {
				return 0, 0, ErrReadOnlyBlock
			}
		}
		return from, to, nil
	}
	return 0, 0, ErrReadOnlyBlock
}

func CrossBlockBoundary(source []byte, block Block, visibleOffset int, selectedEnd bool) (int, error) {
	edge, err := CrossBlockEdgeAt(source, block, visibleOffset, selectedEnd)
	if err != nil {
		return 0, err
	}
	if edge.Retain != "" {
		return 0, ErrReadOnlyBlock
	}
	return edge.Offset, nil
}

func CrossBlockEdgeAt(source []byte, block Block, visibleOffset int, selectedEnd bool) (CrossBlockEdge, error) {
	if sha256.Sum256(source) != block.source || block.Start < 0 || block.End < block.Start || block.End > len(source) {
		return CrossBlockEdge{}, ErrStaleBlock
	}
	if !block.Editable || visibleOffset < 0 || visibleOffset > len(block.Visible) || !utf8.ValidString(block.Visible[:visibleOffset]) {
		return CrossBlockEdge{}, ErrReadOnlyBlock
	}
	if visibleOffset == 0 {
		return CrossBlockEdge{Offset: block.Start}, nil
	}
	if visibleOffset == len(block.Visible) {
		return CrossBlockEdge{Offset: block.End}, nil
	}
	if !block.styled {
		if string(source[block.Start:block.End]) != block.Visible {
			return CrossBlockEdge{}, ErrReadOnlyBlock
		}
		return CrossBlockEdge{Offset: block.Start + visibleOffset}, nil
	}
	if len(block.parts) == 0 && block.Kind == Paragraph && block.textFrom > block.Start && block.textTo < block.End &&
		string(source[block.textFrom:block.textTo]) == block.Visible {
		open, close := source[block.Start:block.textFrom], source[block.textTo:block.End]
		if bytes.Equal(open, close) && (bytes.Equal(open, []byte("*")) || bytes.Equal(open, []byte("**")) || bytes.Equal(open, []byte("_")) || bytes.Equal(open, []byte("__"))) {
			return CrossBlockEdge{Offset: block.textFrom + visibleOffset, Retain: string(open)}, nil
		}
	}
	validPart := func(part inlinePart) bool {
		return part.crossSafe && part.visibleStart >= 0 && part.visibleEnd <= len(block.Visible) && part.visibleStart < part.visibleEnd &&
			part.rawStart >= block.Start && part.rawStart <= part.sourceStart && part.sourceStart <= part.sourceEnd &&
			part.sourceEnd <= part.rawEnd && part.rawEnd <= block.End &&
			string(source[part.sourceStart:part.sourceEnd]) == block.Visible[part.visibleStart:part.visibleEnd]
	}
	for _, part := range block.parts {
		if visibleOffset > part.visibleStart && visibleOffset < part.visibleEnd {
			if validPart(part) {
				edge := CrossBlockEdge{Offset: part.sourceStart + visibleOffset - part.visibleStart}
				if selectedEnd {
					edge.Retain = string(source[part.rawStart:part.sourceStart])
				} else {
					edge.Retain = string(source[part.sourceEnd:part.rawEnd])
				}
				return edge, nil
			}
			return CrossBlockEdge{}, ErrReadOnlyBlock
		}
	}
	if selectedEnd {
		for index := len(block.parts) - 1; index >= 0; index-- {
			part := block.parts[index]
			if visibleOffset == part.visibleEnd && validPart(part) {
				return CrossBlockEdge{Offset: part.rawEnd}, nil
			}
		}
	} else {
		for _, part := range block.parts {
			if visibleOffset == part.visibleStart && validPart(part) {
				return CrossBlockEdge{Offset: part.rawStart}, nil
			}
		}
	}
	return CrossBlockEdge{}, ErrReadOnlyBlock
}

// ReplaceSelection edits a visible range spanning simple inline style groups.
// It keeps source outside those groups byte-identical and rejects a candidate
// whose reparsed visible text or block position differs from the requested edit.
func ReplaceSelection(source []byte, block Block, start, end int, replacement string) ([]byte, error) {
	if sha256.Sum256(source) != block.source || block.Start < 0 || block.End > len(source) {
		return nil, ErrStaleBlock
	}
	if !block.Editable || block.Kind != Paragraph || len(block.parts) < 2 || start < 0 || end <= start || end > len(block.Visible) || !utf8.ValidString(block.Visible[:start]) || !utf8.ValidString(block.Visible[:end]) || !utf8.ValidString(replacement) || bytes.ContainsAny([]byte(replacement), "\x00\r\n") {
		return nil, ErrReadOnlyBlock
	}
	first, last := -1, -1
	for index, part := range block.parts {
		if start >= part.visibleStart && start < part.visibleEnd {
			first = index
		}
		if end > part.visibleStart && end <= part.visibleEnd {
			last = index
		}
	}
	if first < 0 || last <= first {
		return nil, ErrReadOnlyBlock
	}
	for index := first; index <= last; index++ {
		part := block.parts[index]
		if !part.crossSafe || part.rawStart < block.Start || part.rawEnd > block.End || part.rawStart > part.sourceStart || part.rawEnd < part.sourceEnd || index > first && block.parts[index-1].rawEnd > part.rawStart {
			return nil, ErrReadOnlyBlock
		}
	}
	begin, finish := block.parts[first], block.parts[last]
	insert := make([]byte, 0, len(replacement)+end-start)
	if start > begin.visibleStart {
		insert = append(insert, source[begin.rawStart:begin.sourceStart]...)
		insert = append(insert, source[begin.sourceStart:begin.sourceStart+start-begin.visibleStart]...)
		insert = append(insert, source[begin.sourceEnd:begin.rawEnd]...)
	}
	insert = append(insert, replacement...)
	if end < finish.visibleEnd {
		insert = append(insert, source[finish.rawStart:finish.sourceStart]...)
		insert = append(insert, source[finish.sourceStart+end-finish.visibleStart:finish.sourceEnd]...)
		insert = append(insert, source[finish.sourceEnd:finish.rawEnd]...)
	}
	next := make([]byte, 0, len(source)-finish.rawEnd+begin.rawStart+len(insert))
	next = append(next, source[:begin.rawStart]...)
	next = append(next, insert...)
	next = append(next, source[finish.rawEnd:]...)
	want := block.Visible[:start] + replacement + block.Visible[end:]
	wantEnd := block.End + len(insert) - (finish.rawEnd - begin.rawStart)
	for _, candidate := range Parse(next) {
		if candidate.Kind == block.Kind && candidate.Start == block.Start && candidate.End == wantEnd && candidate.Visible == want && candidate.Editable {
			return next, nil
		}
	}
	return nil, ErrReadOnlyBlock
}
