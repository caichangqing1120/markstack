package richedit

import (
	"crypto/sha256"
	"html"
	"strings"
	"unicode/utf8"
)

func ReplaceBlockSelection(source []byte, document Document, firstIndex, firstOffset, lastIndex, lastOffset int, replacement string) ([]byte, error) {
	if firstIndex < 0 || lastIndex <= firstIndex || lastIndex >= len(document.Blocks) || !utf8.ValidString(replacement) || strings.ContainsAny(replacement, "\x00\r\n") {
		return nil, ErrReadOnlyLeaf
	}
	first, last := document.Blocks[firstIndex], document.Blocks[lastIndex]
	if firstOffset < 0 || firstOffset > len(first.Visible) || lastOffset < 0 || lastOffset > len(last.Visible) || !utf8.ValidString(first.Visible[:firstOffset]) || !utf8.ValidString(last.Visible[:lastOffset]) {
		return nil, ErrReadOnlyLeaf
	}
	digest := sha256.Sum256(source)
	for index := firstIndex; index <= lastIndex; index++ {
		block := document.Blocks[index]
		if block.Kind != Paragraph || block.ReadOnly || len(block.Leaves) != 1 || block.Start < 0 || block.End > len(source) || block.OpenEnd < block.Start || block.CloseStart < block.OpenEnd || block.CloseStart > block.End {
			return nil, ErrReadOnlyLeaf
		}
		leaf := block.Leaves[0]
		if leaf.sourceDigest != digest {
			return nil, ErrStaleLeaf
		}
		if !leaf.Editable || leaf.ImageSource != "" || leaf.Start != block.OpenEnd || leaf.End != block.CloseStart {
			return nil, ErrReadOnlyLeaf
		}
		if index > firstIndex {
			previous := document.Blocks[index-1]
			if previous.End > block.Start || strings.TrimSpace(string(source[previous.End:block.Start])) != "" {
				return nil, ErrReadOnlyLeaf
			}
		}
	}
	firstLeaf, lastLeaf := first.Leaves[0], last.Leaves[0]
	firstRaw, firstOK := rawOffsetAtVisible(string(source[firstLeaf.Start:firstLeaf.End]), firstLeaf.Visible, firstOffset)
	lastRaw, lastOK := rawOffsetAtVisible(string(source[lastLeaf.Start:lastLeaf.End]), lastLeaf.Visible, lastOffset)
	if first.Visible == "" && firstOffset == 0 {
		raw := source[firstLeaf.Start:firstLeaf.End]
		if len(raw) == 0 || strings.HasPrefix(strings.ToLower(string(raw)), "<br") {
			firstRaw, firstOK = 0, true
			if last.Visible == "" {
				firstRaw = len(raw)
			}
		}
	}
	if last.Visible == "" && lastOffset == 0 {
		raw := source[lastLeaf.Start:lastLeaf.End]
		if len(raw) == 0 || strings.HasPrefix(strings.ToLower(string(raw)), "<br") {
			lastRaw, lastOK = len(raw), true
		}
	}
	if !firstOK || !lastOK {
		return nil, ErrReadOnlyLeaf
	}
	result := make([]byte, 0, len(source)+len(replacement))
	result = append(result, source[:firstLeaf.Start+firstRaw]...)
	result = append(result, html.EscapeString(replacement)...)
	result = append(result, source[lastLeaf.Start+lastRaw:lastLeaf.End]...)
	result = append(result, source[firstLeaf.End:first.End]...)
	result = append(result, source[last.End:]...)
	parsed := Parse(result)
	want := first.Visible[:firstOffset] + replacement + last.Visible[lastOffset:]
	if len(parsed.Blocks) != len(document.Blocks)-(lastIndex-firstIndex) || firstIndex >= len(parsed.Blocks) || parsed.Blocks[firstIndex].Kind != Paragraph || parsed.Blocks[firstIndex].ReadOnly || parsed.Blocks[firstIndex].Visible != want {
		return nil, ErrReadOnlyLeaf
	}
	return result, nil
}
