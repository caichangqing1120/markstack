package richedit

import (
	"crypto/sha256"
	"html"
	"strings"
	"unicode/utf8"
)

type sourceSplice struct {
	start, end int
	value      string
}

func ReplaceSelection(source []byte, block Block, start, end int, replacement string) ([]byte, error) {
	if block.ReadOnly || start < 0 || end <= start || end > len(block.Visible) || !utf8.ValidString(block.Visible[:start]) || !utf8.ValidString(block.Visible[:end]) || !utf8.ValidString(replacement) || strings.ContainsAny(replacement, "\x00\r\n") {
		return nil, ErrReadOnlyLeaf
	}
	digest := sha256.Sum256(source)
	for _, leaf := range block.Leaves {
		if leaf.sourceDigest != digest {
			return nil, ErrStaleLeaf
		}
	}
	if block.Visible[start:end] == replacement {
		return append([]byte(nil), source...), nil
	}
	var splices []sourceSplice
	visibleOffset := 0
	for _, leaf := range block.Leaves {
		leafEnd := visibleOffset + len(leaf.Visible)
		if leaf.Visible == "" && !leaf.Editable && visibleOffset >= start && visibleOffset <= end {
			return nil, ErrReadOnlyLeaf
		}
		if leafEnd > start && visibleOffset < end {
			if !leaf.Editable || leaf.ImageSource != "" || leaf.sourceDigest != digest || leaf.Start < block.Start || leaf.End > block.End || leaf.End > len(source) || leaf.Start > leaf.End {
				if leaf.sourceDigest != digest {
					return nil, ErrStaleLeaf
				}
				return nil, ErrReadOnlyLeaf
			}
			raw := string(source[leaf.Start:leaf.End])
			from := max(0, start-visibleOffset)
			to := min(len(leaf.Visible), end-visibleOffset)
			rawStart, okStart := rawOffsetAtVisible(raw, leaf.Visible, from)
			rawEnd, okEnd := rawOffsetAtVisible(raw, leaf.Visible, to)
			if !okStart || !okEnd || rawEnd < rawStart {
				return nil, ErrReadOnlyLeaf
			}
			insert := ""
			if len(splices) == 0 {
				insert = html.EscapeString(replacement)
			}
			splices = append(splices, sourceSplice{start: leaf.Start + rawStart, end: leaf.Start + rawEnd, value: insert})
		}
		visibleOffset = leafEnd
	}
	if len(splices) == 0 {
		return nil, ErrReadOnlyLeaf
	}
	result := make([]byte, 0, len(source)+len(replacement))
	cursor := 0
	for _, splice := range splices {
		if splice.start < cursor {
			return nil, ErrReadOnlyLeaf
		}
		result = append(result, source[cursor:splice.start]...)
		result = append(result, splice.value...)
		cursor = splice.end
	}
	result = append(result, source[cursor:]...)
	want := block.Visible[:start] + replacement + block.Visible[end:]
	for _, candidate := range Parse(result).Blocks {
		if candidate.Start == block.Start && candidate.Kind == block.Kind && !candidate.ReadOnly && candidate.Visible == want {
			return result, nil
		}
	}
	return nil, ErrReadOnlyLeaf
}

func rawOffsetAtVisible(raw, visible string, target int) (int, bool) {
	if target < 0 || target > len(visible) || !utf8.ValidString(visible[:target]) {
		return 0, false
	}
	if raw == visible {
		return target, true
	}
	validate := func(offset int) (int, bool) {
		prefix := html.UnescapeString(raw[:offset])
		return offset, len(prefix) == target && prefix+html.UnescapeString(raw[offset:]) == visible
	}
	rawPosition, visiblePosition := 0, 0
	for rawPosition < len(raw) {
		ampersand := strings.IndexByte(raw[rawPosition:], '&')
		if ampersand < 0 {
			if target >= visiblePosition && target <= visiblePosition+len(raw)-rawPosition {
				return validate(rawPosition + target - visiblePosition)
			}
			break
		}
		ampersand += rawPosition
		plain := ampersand - rawPosition
		if target >= visiblePosition && target <= visiblePosition+plain {
			return validate(rawPosition + target - visiblePosition)
		}
		visiblePosition += plain
		rawPosition = ampersand
		next := strings.IndexByte(raw[ampersand+1:], '&')
		segmentEnd := len(raw)
		if next >= 0 {
			segmentEnd = ampersand + 1 + next
		}
		segment := raw[ampersand:segmentEnd]
		decoded := html.UnescapeString(segment)
		if decoded == segment {
			if target <= visiblePosition+len(segment) {
				return validate(ampersand + target - visiblePosition)
			}
			visiblePosition += len(segment)
			rawPosition = segmentEnd
			continue
		}
		consumed := entityPrefixLength(segment, decoded)
		if consumed == 0 {
			return 0, false
		}
		entity := html.UnescapeString(segment[:consumed])
		if target == visiblePosition {
			return validate(ampersand)
		}
		if target < visiblePosition+len(entity) {
			return 0, false
		}
		visiblePosition += len(entity)
		rawPosition += consumed
		if target == visiblePosition {
			return validate(rawPosition)
		}
		plain = segmentEnd - rawPosition
		if target <= visiblePosition+plain {
			return validate(rawPosition + target - visiblePosition)
		}
		visiblePosition += plain
		rawPosition = segmentEnd
	}
	if target == visiblePosition {
		return validate(rawPosition)
	}
	return 0, false
}

func entityPrefixLength(segment, decoded string) int {
	difference := len(segment) - len(decoded)
	if strings.HasPrefix(segment, "&#") {
		index := 2
		hexadecimal := index < len(segment) && (segment[index] == 'x' || segment[index] == 'X')
		if hexadecimal {
			index++
		}
		firstDigit := index
		for index < len(segment) {
			character := segment[index]
			if character >= '0' && character <= '9' || hexadecimal && (character >= 'a' && character <= 'f' || character >= 'A' && character <= 'F') {
				index++
				continue
			}
			break
		}
		if index > firstDigit {
			if index < len(segment) && segment[index] == ';' {
				index++
			}
			candidate := html.UnescapeString(segment[:index])
			if candidate != segment[:index] && index-len(candidate) == difference && candidate+segment[index:] == decoded {
				return index
			}
		}
	}
	for index := 2; index <= min(len(segment), 64); index++ {
		candidate := html.UnescapeString(segment[:index])
		if candidate != segment[:index] && index-len(candidate) == difference && candidate+segment[index:] == decoded {
			return index
		}
	}
	return 0
}
