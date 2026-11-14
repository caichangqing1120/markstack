package richedit

import (
	"bytes"
	"crypto/sha256"
	"io"
	"strings"
	"unicode/utf8"

	htmltoken "golang.org/x/net/html"
)

func SplitParagraph(source []byte, block Block, visibleCaret int, newline string) ([]byte, error) {
	return splitEditableBlock(source, block, visibleCaret, newline, Paragraph, "p")
}

func splitEditableBlock(source []byte, block Block, visibleCaret int, newline string, kind Kind, tag string) ([]byte, error) {
	if (newline != "\n" && newline != "\r\n") || !canSplitBlock(block, kind) || visibleCaret < 0 || visibleCaret > len(block.Visible) || !utf8.ValidString(block.Visible[:visibleCaret]) {
		return nil, ErrReadOnlyLeaf
	}
	if block.Start < 0 || block.End > len(source) || block.OpenEnd < block.Start || block.CloseStart < block.OpenEnd {
		return nil, ErrReadOnlyLeaf
	}
	digest := sha256.Sum256(source)
	for _, leaf := range block.Leaves {
		if leaf.sourceDigest != digest {
			return nil, ErrStaleLeaf
		}
	}
	var left, right []byte
	leaf := block.Leaves[0]
	if len(block.Leaves) == 1 && leaf.Start == block.OpenEnd && leaf.End == block.CloseStart {
		raw := source[leaf.Start:leaf.End]
		cut := 0
		if leaf.Visible != "" {
			var ok bool
			cut, ok = rawOffsetAtVisible(string(raw), leaf.Visible, visibleCaret)
			if !ok {
				return nil, ErrReadOnlyLeaf
			}
		} else if len(raw) > 0 && !bytes.HasPrefix(bytes.ToLower(raw), []byte("<br")) {
			return nil, ErrReadOnlyLeaf
		}
		left, right = raw[:cut], raw[cut:]
		if leaf.Visible == "" {
			left, right = raw, []byte("<br>")
		}
	} else {
		cut, ok := styledCaretCut(source, block, visibleCaret)
		if !ok {
			return nil, ErrReadOnlyLeaf
		}
		closers, openers, err := splitInlineStack(source, block, cut)
		if err != nil {
			return nil, err
		}
		left = append(append([]byte(nil), source[block.OpenEnd:cut]...), closers...)
		right = append(append([]byte(nil), openers...), source[cut:block.CloseStart]...)
	}
	if len(left) == 0 {
		left = []byte("<br>")
	}
	if len(right) == 0 {
		right = []byte("<br>")
	}
	result := make([]byte, 0, len(source)+len(newline)+len(tag)*2+len("<br>")+5)
	result = append(result, source[:block.Start]...)
	result = append(result, source[block.Start:block.OpenEnd]...)
	result = append(result, left...)
	result = append(result, "</"+tag+">"...)
	result = append(result, newline...)
	result = append(result, "<"+tag+">"...)
	result = append(result, right...)
	result = append(result, source[block.CloseStart:block.End]...)
	result = append(result, source[block.End:]...)
	leftVisible, rightVisible := block.Visible[:visibleCaret], block.Visible[visibleCaret:]
	parsed := Parse(result)
	for index, candidate := range parsed.Blocks {
		if candidate.Start == block.Start && candidate.Kind == kind && !candidate.ReadOnly && candidate.Visible == leftVisible && index+1 < len(parsed.Blocks) {
			next := parsed.Blocks[index+1]
			if next.Kind == kind && !next.ReadOnly && next.Visible == rightVisible {
				return result, nil
			}
		}
	}
	return nil, ErrReadOnlyLeaf
}

func CanSplitParagraph(block Block) bool {
	return canSplitBlock(block, Paragraph)
}

func canSplitBlock(block Block, kind Kind) bool {
	if block.Kind != kind || block.ReadOnly || len(block.Leaves) == 0 || strings.Contains(block.Visible, "\n") {
		return false
	}
	for _, leaf := range block.Leaves {
		if !leaf.Editable || leaf.ImageSource != "" || leaf.Start < block.OpenEnd || leaf.End > block.CloseStart {
			return false
		}
	}
	return true
}

func styledCaretCut(source []byte, block Block, caret int) (int, bool) {
	if caret == 0 {
		return block.OpenEnd, true
	}
	if caret == len(block.Visible) {
		return block.CloseStart, true
	}
	offset := 0
	for index, leaf := range block.Leaves {
		end := offset + len(leaf.Visible)
		if caret > offset && caret < end {
			rawOffset, ok := rawOffsetAtVisible(string(source[leaf.Start:leaf.End]), leaf.Visible, caret-offset)
			return leaf.Start + rawOffset, ok
		}
		if caret == end && index+1 < len(block.Leaves) {
			next := block.Leaves[index+1]
			return inlineBoundaryCut(source, leaf.End, next.Start)
		}
		offset = end
	}
	return 0, false
}

func inlineBoundaryCut(source []byte, start, end int) (int, bool) {
	if start < 0 || end < start || end > len(source) {
		return 0, false
	}
	tokenizer := htmltoken.NewTokenizer(bytes.NewReader(source[start:end]))
	cut, offset := start, start
	opened := false
	for {
		kind := tokenizer.Next()
		if kind == htmltoken.ErrorToken {
			return cut, tokenizer.Err() == io.EOF && offset == end
		}
		raw := tokenizer.Raw()
		switch kind {
		case htmltoken.EndTagToken:
			if opened {
				return 0, false
			}
			cut = offset + len(raw)
		case htmltoken.StartTagToken:
			opened = true
		default:
			return 0, false
		}
		offset += len(raw)
	}
}

type inlineSplitFrame struct {
	tag         string
	open, close []byte
}

func splitInlineStack(source []byte, block Block, cut int) ([]byte, []byte, error) {
	if cut < block.OpenEnd || cut > block.CloseStart {
		return nil, nil, ErrReadOnlyLeaf
	}
	tokenizer := htmltoken.NewTokenizer(bytes.NewReader(source[block.OpenEnd:block.CloseStart]))
	offset := block.OpenEnd
	var stack, active []*inlineSplitFrame
	captured := false
	for {
		kind := tokenizer.Next()
		if kind == htmltoken.ErrorToken {
			if tokenizer.Err() != io.EOF {
				return nil, nil, ErrReadOnlyLeaf
			}
			if !captured && cut == offset {
				active = append(active, stack...)
				captured = true
			}
			break
		}
		raw := append([]byte(nil), tokenizer.Raw()...)
		start, end := offset, offset+len(raw)
		if !captured && (cut == start || cut > start && cut < end && kind == htmltoken.TextToken) {
			active = append(active, stack...)
			captured = true
		}
		if cut > start && cut < end && kind != htmltoken.TextToken {
			return nil, nil, ErrReadOnlyLeaf
		}
		switch kind {
		case htmltoken.TextToken:
		case htmltoken.StartTagToken:
			token := tokenizer.Token()
			tag := strings.ToLower(token.Data)
			if !inlineTag(tag) || unsafeAttributes(token.Attr) || tag == "a" && !safeLink(attribute(token.Attr, "href")) {
				return nil, nil, ErrReadOnlyLeaf
			}
			stack = append(stack, &inlineSplitFrame{tag: tag, open: raw})
		case htmltoken.EndTagToken:
			token := tokenizer.Token()
			tag := strings.ToLower(token.Data)
			if len(stack) == 0 || stack[len(stack)-1].tag != tag {
				return nil, nil, ErrReadOnlyLeaf
			}
			stack[len(stack)-1].close = raw
			stack = stack[:len(stack)-1]
		default:
			return nil, nil, ErrReadOnlyLeaf
		}
		offset = end
	}
	if !captured || offset != block.CloseStart || len(stack) != 0 {
		return nil, nil, ErrReadOnlyLeaf
	}
	var closers, openers []byte
	for _, frame := range active {
		if len(frame.close) == 0 {
			return nil, nil, ErrReadOnlyLeaf
		}
		openers = append(openers, frame.open...)
	}
	for index := len(active) - 1; index >= 0; index-- {
		closers = append(closers, active[index].close...)
	}
	return closers, openers, nil
}
