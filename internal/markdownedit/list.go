package markdownedit

import (
	"bytes"
	"crypto/sha256"
	"strings"

	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
)

type ListMarker struct {
	Line, Start, Content, Depth int
	Parent, Previous            int
}

func countListBlocks(source []byte, list *ast.List, parent int) int {
	count := list.ChildCount()
	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		marker, ok := listMarkerAt(source, item.Pos())
		if !ok || marker.Line <= parent {
			continue
		}
		child := item.FirstChild()
		if child != nil && isTextBlock(child) {
			child = child.NextSibling()
		}
		for ; child != nil; child = child.NextSibling() {
			if nested, ok := child.(*ast.List); ok {
				count += countListBlocks(source, nested, marker.Line)
			} else {
				count++
			}
		}
	}
	return count
}

func appendList(blocks []Block, source []byte, list *ast.List, fingerprint [32]byte, depth, parent int) []Block {
	previous := -1
	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		marker, ok := listMarkerAt(source, item.Pos())
		if !ok || marker.Line <= parent {
			blocks = append(blocks, readOnly(source, item, Raw, fingerprint))
			previous = -1
			continue
		}
		marker.Depth, marker.Parent, marker.Previous = depth, parent, previous
		previous = marker.Line
		child := item.FirstChild()
		block := Block{Kind: ListItem, Start: marker.Content, End: marker.Content, Editable: true, source: fingerprint}
		if child != nil && isTextBlock(child) {
			if _, task := child.FirstChild().(*extast.TaskCheckBox); task {
				block = taskBlock(source, child, fingerprint)
			} else {
				block = simple(source, child, ListItem, fingerprint)
			}
			child = child.NextSibling()
		}
		for sibling := child; sibling != nil; sibling = sibling.NextSibling() {
			if _, nested := sibling.(*ast.List); !nested {
				block.Editable = false
				break
			}
		}
		block.List = &marker
		blocks = append(blocks, block)
		for ; child != nil; child = child.NextSibling() {
			if nested, ok := child.(*ast.List); ok {
				blocks = appendList(blocks, source, nested, fingerprint, depth+1, marker.Line)
			} else {
				blocks = append(blocks, readOnly(source, child, Raw, fingerprint))
			}
		}
	}
	return blocks
}

func listMarkerAt(source []byte, marker int) (ListMarker, bool) {
	if marker < 0 || marker >= len(source) {
		return ListMarker{}, false
	}
	line := bytes.LastIndexByte(source[:marker], '\n') + 1
	if line == 0 && bytes.HasPrefix(source, []byte("\xef\xbb\xbf")) {
		line = 3
	}
	marker = line
	for marker < len(source) && (source[marker] == ' ' || source[marker] == '\t') {
		marker++
	}
	if marker == len(source) {
		return ListMarker{}, false
	}
	end := marker
	if bytes.ContainsRune([]byte("-+*"), rune(source[end])) {
		end++
	} else {
		for end < len(source) && source[end] >= '0' && source[end] <= '9' {
			end++
		}
		if end == marker || end-marker > 9 || end == len(source) || source[end] != '.' && source[end] != ')' {
			return ListMarker{}, false
		}
		end++
	}
	content := end
	for content < len(source) && (source[content] == ' ' || source[content] == '\t') {
		content++
	}
	if content == end && end < len(source) && source[end] != '\r' && source[end] != '\n' {
		return ListMarker{}, false
	}
	return ListMarker{Line: line, Start: marker, Content: content, Parent: -1, Previous: -1}, true
}

func PatchListIndent(source []byte, block Block, outdent bool) (SourcePatch, int, error) {
	if sha256.Sum256(source) != block.source {
		return SourcePatch{}, 0, ErrStaleBlock
	}
	if block.Kind != ListItem || block.List == nil || !block.Editable {
		return SourcePatch{}, 0, ErrReadOnlyBlock
	}
	blocks := Parse(source)
	marker := *block.List
	items := make(map[int]Block)
	for _, candidate := range blocks {
		if candidate.List != nil {
			items[candidate.List.Line] = candidate
		}
	}
	relative := marker.Previous
	if outdent {
		relative = marker.Parent
	} else if relative < 0 {
		for _, candidate := range blocks {
			if candidate.List != nil && candidate.List.Line < marker.Line && candidate.List.Parent == marker.Parent {
				relative = candidate.List.Line
			}
		}
		if peer, ok := items[relative]; ok {
			end, err := simpleListBranchEnd(source, items, *peer.List)
			if err != nil || end > marker.Line || len(bytes.TrimSpace(source[end:marker.Line])) != 0 {
				relative = -1
			}
		}
	}
	peer, ok := items[relative]
	if !ok {
		return SourcePatch{}, 0, ErrReadOnlyBlock
	}
	peerMarker := *peer.List
	newIndent, parent, depth := peerMarker.Content-peerMarker.Line, peerMarker.Line, marker.Depth+1
	if outdent {
		newIndent, parent, depth = peerMarker.Start-peerMarker.Line, peerMarker.Parent, marker.Depth-1
	}
	if bytes.ContainsAny(source[peerMarker.Line:peerMarker.Content], "\t") {
		return SourcePatch{}, 0, ErrReadOnlyBlock
	}
	change := newIndent - (marker.Start - marker.Line)
	if outdent && change >= 0 || !outdent && change <= 0 {
		return SourcePatch{}, 0, ErrReadOnlyBlock
	}
	branchEnd, err := simpleListBranchEnd(source, items, marker)
	if err != nil {
		return SourcePatch{}, 0, err
	}
	end := branchEnd
	if outdent {
		end, err = simpleListBranchEnd(source, items, peerMarker)
		if err != nil {
			return SourcePatch{}, 0, err
		}
	}
	var replacement strings.Builder
	positions := make(map[int]int)
	targetLine := marker.Line
	if end > branchEnd {
		ending := lineEndingBefore(source, branchEnd)
		remaining := string(source[branchEnd:end])
		if lineEndingBefore(source, end) == "" {
			remaining += ending
		}
		replacement.WriteString(remaining)
		targetLine += len(remaining)
		for line := range items {
			if line >= branchEnd && line < end {
				positions[line] = marker.Line + line - branchEnd
			}
		}
	}
	for cursor := marker.Line; cursor < branchEnd; {
		lineEnd := nextLine(source, cursor)
		line := source[cursor:lineEnd]
		if len(bytes.TrimSpace(line)) == 0 {
			replacement.Write(line)
		} else {
			item := items[cursor]
			if item.List == nil {
				return SourcePatch{}, 0, ErrReadOnlyBlock
			}
			indent := item.List.Start - cursor + change
			if indent < 0 || bytes.ContainsAny(source[cursor:item.List.Content], "\t") {
				return SourcePatch{}, 0, ErrReadOnlyBlock
			}
			positions[cursor] = marker.Line + replacement.Len()
			replacement.WriteString(strings.Repeat(" ", indent))
			replacement.Write(source[item.List.Start:lineEnd])
		}
		cursor = lineEnd
	}
	value := replacement.String()
	if end > branchEnd && lineEndingBefore(source, end) == "" {
		value = strings.TrimSuffix(value, lineEndingBefore(source, branchEnd))
	}
	patch := SourcePatch{Start: marker.Line, End: end, Text: value}
	if target, ok := verifyListMove(patch, source, blocks, items, positions, marker.Line, targetLine, parent, depth); ok {
		return patch, target, nil
	}
	ending := lineEndingBefore(source, marker.Line)
	if ending != "" {
		index := targetLine - patch.Start
		patch.Text = value[:index] + ending + value[index:]
		for line, position := range positions {
			if position >= targetLine {
				positions[line] += len(ending)
			}
		}
		if target, ok := verifyListMove(patch, source, blocks, items, positions, marker.Line, targetLine+len(ending), parent, depth); ok {
			return patch, target, nil
		}
	}
	return SourcePatch{}, 0, ErrReadOnlyBlock
}

func verifyListMove(patch SourcePatch, source []byte, blocks []Block, items map[int]Block, positions map[int]int, movedLine, targetLine, parent, depth int) (int, bool) {
	shift := func(line int) int {
		if mapped, ok := positions[line]; ok {
			return mapped
		}
		if line >= patch.End {
			return line + len(patch.Text) - (patch.End - patch.Start)
		}
		return line
	}
	next := Parse(patch.apply(source))
	if len(next) != len(blocks) {
		return 0, false
	}
	updatedItems := make(map[int]Block)
	for _, candidate := range next {
		if candidate.List != nil {
			updatedItems[candidate.List.Line] = candidate
		}
	}
	if len(updatedItems) != len(items) {
		return 0, false
	}
	for line, original := range items {
		updated, ok := updatedItems[shift(line)]
		wantParent := original.List.Parent
		if line == movedLine {
			wantParent = parent
		}
		if !ok || updated.List.Parent != shift(wantParent) || updated.Visible != original.Visible || updated.Task != original.Task || updated.Checked != original.Checked {
			return 0, false
		}
	}
	target := updatedItems[targetLine]
	if target.List == nil || target.List.Depth != depth || !target.Editable {
		return 0, false
	}
	return target.Start, true
}

func simpleListBranchEnd(source []byte, items map[int]Block, marker ListMarker) (int, error) {
	end := nextLine(source, marker.Line)
	for cursor := end; cursor < len(source); {
		lineEnd := nextLine(source, cursor)
		if len(bytes.TrimSpace(source[cursor:lineEnd])) == 0 {
			cursor = lineEnd
			continue
		}
		child, ok := items[cursor]
		if !ok || child.List.Depth <= marker.Depth {
			break
		}
		if !child.Editable {
			return 0, ErrReadOnlyBlock
		}
		end, cursor = lineEnd, lineEnd
	}
	return end, nil
}

func ExitEmptyList(source []byte, block Block) (SourcePatch, error) {
	if sha256.Sum256(source) != block.source {
		return SourcePatch{}, ErrStaleBlock
	}
	if block.Kind != ListItem || block.List == nil || !block.Editable || block.Visible != "" {
		return SourcePatch{}, ErrReadOnlyBlock
	}
	start := block.List.Line
	end := nextLine(source, start)
	ending := lineEndingBefore(source, end)
	for _, candidate := range Parse(source) {
		if candidate.List != nil && candidate.List.Parent == start {
			return SourcePatch{}, ErrReadOnlyBlock
		}
	}
	prefix := ""
	if start > 0 && source[start-1] == '\n' {
		before := source[:start-len(lineEndingBefore(source, start))]
		if len(bytes.TrimSpace(before[bytes.LastIndexByte(before, '\n')+1:])) != 0 {
			prefix = lineEndingBefore(source, start)
		}
	}
	return SourcePatch{Start: start, End: end - len(ending), Text: prefix}, nil
}

func nextLine(source []byte, start int) int {
	if relative := bytes.IndexByte(source[start:], '\n'); relative >= 0 {
		return start + relative + 1
	}
	return len(source)
}

func lineEndingBefore(source []byte, end int) string {
	if end >= 2 && source[end-2] == '\r' && source[end-1] == '\n' {
		return "\r\n"
	}
	if end >= 1 && source[end-1] == '\n' {
		return "\n"
	}
	return ""
}
