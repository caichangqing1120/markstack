package richedit

import (
	"bytes"
	"crypto/sha256"
	"io"
	"strings"

	htmltoken "golang.org/x/net/html"
)

func MergeParagraphs(source []byte, document Document, firstIndex, secondIndex int) ([]byte, error) {
	return mergeEditableBlocks(source, document, firstIndex, secondIndex, Paragraph, "p")
}

func mergeEditableBlocks(source []byte, document Document, firstIndex, secondIndex int, kind Kind, tag string) ([]byte, error) {
	if firstIndex < 0 || secondIndex != firstIndex+1 || secondIndex >= len(document.Blocks) {
		return nil, ErrReadOnlyLeaf
	}
	first, second := document.Blocks[firstIndex], document.Blocks[secondIndex]
	digest := sha256.Sum256(source)
	for _, block := range []Block{first, second} {
		if len(block.Leaves) == 0 {
			return nil, ErrReadOnlyLeaf
		}
		for _, leaf := range block.Leaves {
			if leaf.sourceDigest != digest {
				return nil, ErrStaleLeaf
			}
		}
	}
	if first.Kind != kind || second.Kind != kind || first.ReadOnly || second.ReadOnly ||
		first.Start < 0 || first.End > len(source) || second.End > len(source) ||
		first.OpenEnd < first.Start || first.CloseStart < first.OpenEnd || first.CloseStart > first.End ||
		second.OpenEnd < second.Start || second.CloseStart < second.OpenEnd || second.CloseStart > second.End ||
		first.End > second.Start || strings.TrimSpace(string(source[first.End:second.Start])) != "" ||
		!strings.EqualFold(string(source[second.Start:second.OpenEnd]), "<"+tag+">") {
		return nil, ErrReadOnlyLeaf
	}
	for _, block := range []Block{first, second} {
		for _, leaf := range block.Leaves {
			if !leaf.Editable || leaf.ImageSource != "" || leaf.Start < block.OpenEnd || leaf.End > block.CloseStart {
				return nil, ErrReadOnlyLeaf
			}
		}
		if block.Visible != "" {
			if _, _, err := splitInlineStack(source, block, block.CloseStart); err != nil {
				return nil, err
			}
		} else {
			raw := source[block.OpenEnd:block.CloseStart]
			if len(raw) != 0 && !bytes.HasPrefix(bytes.ToLower(raw), []byte("<br")) {
				return nil, ErrReadOnlyLeaf
			}
		}
	}
	left := source[first.OpenEnd:first.CloseStart]
	right := source[second.OpenEnd:second.CloseStart]
	if first.Visible == "" {
		left = nil
	}
	if second.Visible == "" {
		right = nil
	}
	if first.Visible == "" && second.Visible == "" {
		left = []byte("<br>")
	}
	if first.Visible != "" && second.Visible != "" {
		left, right = coalesceInlineBoundary(left, right)
	}
	result := make([]byte, 0, len(source)-(second.Start-first.CloseStart)+len(right))
	result = append(result, source[:first.OpenEnd]...)
	result = append(result, left...)
	result = append(result, right...)
	result = append(result, source[second.CloseStart:second.End]...)
	result = append(result, source[second.End:]...)
	parsed := Parse(result)
	if len(parsed.Blocks) != len(document.Blocks)-1 || firstIndex >= len(parsed.Blocks) || parsed.Blocks[firstIndex].Kind != kind || parsed.Blocks[firstIndex].ReadOnly || parsed.Blocks[firstIndex].Visible != first.Visible+second.Visible {
		return nil, ErrReadOnlyLeaf
	}
	return result, nil
}

type mergeInlineToken struct {
	kind  htmltoken.TokenType
	raw   []byte
	frame *inlineSplitFrame
}

func parseMergeInlineTokens(content []byte) ([]mergeInlineToken, bool) {
	tokenizer := htmltoken.NewTokenizer(bytes.NewReader(content))
	var tokens []mergeInlineToken
	var stack []*inlineSplitFrame
	for {
		kind := tokenizer.Next()
		if kind == htmltoken.ErrorToken {
			return tokens, tokenizer.Err() == io.EOF && len(stack) == 0
		}
		raw := append([]byte(nil), tokenizer.Raw()...)
		switch kind {
		case htmltoken.TextToken:
			tokens = append(tokens, mergeInlineToken{kind: kind, raw: raw})
		case htmltoken.StartTagToken:
			token := tokenizer.Token()
			tag := strings.ToLower(token.Data)
			if !inlineTag(tag) || unsafeAttributes(token.Attr) || tag == "a" && !safeLink(attribute(token.Attr, "href")) {
				return nil, false
			}
			frame := &inlineSplitFrame{tag: tag, open: raw}
			stack = append(stack, frame)
			tokens = append(tokens, mergeInlineToken{kind: kind, raw: raw, frame: frame})
		case htmltoken.EndTagToken:
			token := tokenizer.Token()
			tag := strings.ToLower(token.Data)
			if len(stack) == 0 || stack[len(stack)-1].tag != tag {
				return nil, false
			}
			frame := stack[len(stack)-1]
			frame.close = raw
			stack = stack[:len(stack)-1]
			tokens = append(tokens, mergeInlineToken{kind: kind, raw: raw, frame: frame})
		default:
			return nil, false
		}
	}
}

func coalesceInlineBoundary(left, right []byte) ([]byte, []byte) {
	leftTokens, leftOK := parseMergeInlineTokens(left)
	rightTokens, rightOK := parseMergeInlineTokens(right)
	if !leftOK || !rightOK {
		return left, right
	}
	leftEnd, rightStart := len(left), 0
	for leftIndex, rightIndex := len(leftTokens)-1, 0; leftIndex >= 0 && rightIndex < len(rightTokens); leftIndex, rightIndex = leftIndex-1, rightIndex+1 {
		closing, opening := leftTokens[leftIndex], rightTokens[rightIndex]
		if closing.kind != htmltoken.EndTagToken || opening.kind != htmltoken.StartTagToken ||
			!bytes.Equal(closing.frame.open, opening.frame.open) || !bytes.Equal(closing.frame.close, opening.frame.close) {
			break
		}
		leftEnd -= len(closing.raw)
		rightStart += len(opening.raw)
	}
	return left[:leftEnd], right[rightStart:]
}
