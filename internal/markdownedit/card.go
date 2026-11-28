package markdownedit

import (
	"bytes"
	"crypto/sha256"
	"strings"
	"unicode/utf8"

	"markstack/internal/markdownsyntax"
)

func CalloutBody(source string, card *markdownsyntax.Callout) string {
	if card.BodyStart > card.End {
		return ""
	}
	var body strings.Builder
	position := card.BodyStart
	for line := range strings.SplitSeq(source[position:card.End], "\n") {
		if position != card.BodyStart {
			body.WriteByte('\n')
		}
		body.WriteString(strings.TrimSuffix(line[quotePrefix(line):], "\r"))
		position += len(line) + 1
	}
	return body.String()
}

func MetadataBody(source string, metadata markdownsyntax.FrontMatter) string {
	body := strings.TrimSuffix(source[metadata.BodyStart:metadata.BodyEnd], "\n")
	return strings.ReplaceAll(strings.TrimSuffix(body, "\r"), "\r\n", "\n")
}

func quotePrefix(line string) int {
	trimmed := strings.TrimLeft(line, " ")
	if len(trimmed) == 0 || trimmed[0] != '>' {
		return 0
	}
	length := len(line) - len(trimmed) + 1
	if length < len(line) && (line[length] == ' ' || line[length] == '\t') {
		length++
	}
	return length
}

func (block Block) IsCard() bool { return block.Kind == Callout || block.Kind == FrontMatter }

func (block Block) CaretSourceOffsetIn(source string, offset int) int {
	if block.IsCard() {
		return block.cardOffset(source, max(0, min(offset, len(block.Visible))))
	}
	return block.CaretSourceOffset(offset)
}

func (block Block) cardOffset(source string, visible int) int {
	if block.cardBodyStart > block.cardBodyEnd {
		return block.cardBodyEnd
	}
	position := block.cardBodyStart
	for line := range strings.SplitSeq(source[position:block.cardBodyEnd], "\n") {
		prefix := 0
		if block.Kind == Callout {
			prefix = quotePrefix(line)
		}
		length := len(strings.TrimSuffix(line, "\r")) - prefix
		if visible <= length {
			return position + prefix + visible
		}
		visible -= length + 1
		position += len(line) + 1
	}
	return block.cardBodyEnd
}

func replaceCard(source []byte, block Block, visible string) ([]byte, error) {
	from, to, newEnd := visibleDiff(block.Visible, visible)
	return replaceCardRange(source, block, from, to, visible[from:newEnd])
}

func replaceCardRange(source []byte, block Block, from, to int, inserted string) ([]byte, error) {
	if sha256.Sum256(source) != block.source {
		return nil, ErrStaleBlock
	}
	if !EditableRange(block, from, to) || !utf8.ValidString(inserted) || strings.ContainsAny(inserted, "\r\x00") {
		return nil, ErrReadOnlyBlock
	}
	visible := block.Visible[:from] + inserted + block.Visible[to:]
	if visible == block.Visible {
		return bytes.Clone(source), nil
	}
	raw := string(source[block.Start:block.End])
	localized := block
	localized.cardBodyStart -= block.Start
	localized.cardBodyEnd -= block.Start
	start, end := block.Start+localized.cardOffset(raw, from), block.Start+localized.cardOffset(raw, to)
	lineEnd := "\n"
	if bytes.HasPrefix(source[block.cardHeaderEnd:], []byte("\r\n")) || block.cardHeaderEnd == block.End && bytes.Contains(source, []byte("\r\n")) {
		lineEnd = "\r\n"
	}
	separator := lineEnd
	if block.Kind == Callout {
		separator += "> "
	}
	replacement := strings.ReplaceAll(inserted, "\n", separator)
	if block.Kind == Callout && block.cardBodyStart > block.cardBodyEnd {
		replacement = separator + replacement
	}
	next := bytes.Join([][]byte{source[:start], []byte(replacement), source[end:]}, nil)
	for _, candidate := range Parse(next) {
		if candidate.Start == block.Start && candidate.Kind == block.Kind && candidate.Visible == visible && candidate.End == block.End+len(next)-len(source) {
			return next, nil
		}
	}
	return nil, ErrReadOnlyBlock
}
