package richedit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"html"
	"io"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	htmltoken "golang.org/x/net/html"
)

var (
	ErrReadOnlyLeaf = errors.New("HTML text is read-only")
	ErrStaleLeaf    = errors.New("HTML source interval is stale")
)

type Kind uint8

const (
	Paragraph Kind = iota + 1
	Heading
	ListItem
	Quote
	TableCell
	Image
	Code
	Unknown
)

type Leaf struct {
	Start, End  int
	Visible     string
	Editable    bool
	Bold        bool
	Italic      bool
	Underline   bool
	Strike      bool
	Mono        bool
	Link        string
	SafeLink    bool
	ImageSource string
	// SafeAsset means syntactically local; the loader must still confine it to the workspace.
	SafeAsset    bool
	sourceDigest [sha256.Size]byte
}

type Block struct {
	Kind           Kind
	Start, End     int
	OpenEnd        int
	CloseStart     int
	Level          int
	Number         int
	Row, Column    int
	TableStart     int
	Visible        string
	Leaves         []Leaf
	ReadOnly       bool
	ReadOnlyReason string
}

type Document struct{ Blocks []Block }

type element struct {
	tag          string
	start        int
	contentStart int
	block        int
	unknownRoot  bool
	unsafe       bool
	listNext     int
	link         string
	safeLink     bool
}

type parser struct {
	source []byte
	digest [sha256.Size]byte
	result Document
	stack  []element
	row    int
	column int
}

func Parse(source []byte) Document {
	result, _ := parseWithContext(nil, source)
	return result
}

func ParseContext(ctx context.Context, source []byte) (Document, error) {
	return parseWithContext(ctx, source)
}

func parseContextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

type tokenizerContextReader struct {
	ctx    context.Context
	source *bytes.Reader
}

func (reader *tokenizerContextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.source.Read(buffer)
}

func parseWithContext(ctx context.Context, source []byte) (Document, error) {
	if err := parseContextErr(ctx); err != nil {
		return Document{}, err
	}
	if !utf8.Valid(source) || bytes.IndexByte(source, 0) >= 0 {
		return Document{Blocks: []Block{{Kind: Unknown, Start: 0, End: len(source), Visible: "无法无损解析的 HTML 原文", ReadOnly: true, ReadOnlyReason: "invalid encoding"}}}, nil
	}
	contentStart := 0
	if bytes.HasPrefix(source, []byte{0xef, 0xbb, 0xbf}) {
		contentStart = 3
	}
	current := parser{source: source, digest: sha256.Sum256(source), row: -1}
	reader := bytes.NewReader(source[contentStart:])
	var input io.Reader = reader
	if ctx != nil && ctx.Done() != nil {
		input = &tokenizerContextReader{ctx: ctx, source: reader}
	}
	tokenizer := htmltoken.NewTokenizer(input)
	offset := contentStart
	for {
		kind := tokenizer.Next()
		if err := parseContextErr(ctx); err != nil {
			return Document{}, err
		}
		if kind == htmltoken.ErrorToken {
			if tokenizer.Err() != io.EOF {
				if err := current.markOpen(ctx, "invalid HTML token"); err != nil {
					return Document{}, err
				}
			}
			if offset < len(source) && current.unknownIndex() < 0 {
				if index := current.activeBlock(); index >= 0 {
					current.result.Blocks[index].Visible = string(source[offset:])
					current.readOnly(index, "malformed HTML source")
				} else {
					index := current.addBlock(Unknown, offset)
					current.result.Blocks[index].End, current.result.Blocks[index].Visible = len(source), string(source[offset:])
					current.readOnly(index, "malformed HTML source")
				}
			}
			break
		}
		raw := append([]byte(nil), tokenizer.Raw()...)
		start := offset
		offset += len(raw)
		token := tokenizer.Token()
		switch kind {
		case htmltoken.StartTagToken, htmltoken.SelfClosingTagToken:
			current.start(token, start, offset, kind == htmltoken.SelfClosingTagToken)
		case htmltoken.EndTagToken:
			current.end(token.Data, start, offset)
		case htmltoken.TextToken:
			current.text(raw, start, offset)
		case htmltoken.CommentToken, htmltoken.DoctypeToken:
			if index := current.activeBlock(); index >= 0 && current.unknownIndex() < 0 && kind == htmltoken.DoctypeToken {
				current.readOnly(index, "doctype inside a block")
			}
		}
	}
	if len(current.stack) > 0 {
		if err := current.markOpen(ctx, "unclosed HTML element"); err != nil {
			return Document{}, err
		}
		for _, frame := range current.stack {
			if err := parseContextErr(ctx); err != nil {
				return Document{}, err
			}
			if frame.block >= 0 {
				current.closeBlock(frame, len(source), len(source))
			} else if containerTag(frame.tag) {
				for index := range current.result.Blocks {
					if err := parseContextErr(ctx); err != nil {
						return Document{}, err
					}
					if current.result.Blocks[index].Start >= frame.start {
						current.readOnly(index, "unclosed HTML container")
					}
				}
			}
		}
	}
	for index := range current.result.Blocks {
		if err := parseContextErr(ctx); err != nil {
			return Document{}, err
		}
		block := &current.result.Blocks[index]
		visible, err := assembleVisible(ctx, block.Leaves, block.Visible)
		if err != nil {
			return Document{}, err
		}
		block.Visible = visible
		if block.End == 0 {
			block.End = len(source)
			current.readOnly(index, "unclosed HTML block")
		}
		if block.ReadOnly {
			for leaf := range block.Leaves {
				if err := parseContextErr(ctx); err != nil {
					return Document{}, err
				}
				block.Leaves[leaf].Editable = false
			}
		}
	}
	return current.result, nil
}

func assembleVisible(ctx context.Context, leaves []Leaf, suffix string) (string, error) {
	switch len(leaves) {
	case 0:
		return suffix, nil
	case 1:
		return leaves[0].Visible + suffix, nil
	}
	length := len(suffix)
	for _, leaf := range leaves {
		if err := parseContextErr(ctx); err != nil {
			return "", err
		}
		length += len(leaf.Visible)
	}
	var visible strings.Builder
	visible.Grow(length)
	for _, leaf := range leaves {
		if err := parseContextErr(ctx); err != nil {
			return "", err
		}
		visible.WriteString(leaf.Visible)
	}
	visible.WriteString(suffix)
	return visible.String(), nil
}

func (parser *parser) activeBlock() int {
	for index := len(parser.stack) - 1; index >= 0; index-- {
		if parser.stack[index].block >= 0 {
			return parser.stack[index].block
		}
	}
	return -1
}

func (parser *parser) unknownIndex() int {
	for index := len(parser.stack) - 1; index >= 0; index-- {
		if parser.stack[index].unknownRoot {
			return index
		}
	}
	return -1
}

func (parser *parser) addBlock(kind Kind, start int) int {
	parser.result.Blocks = append(parser.result.Blocks, Block{Kind: kind, Start: start})
	return len(parser.result.Blocks) - 1
}

func (parser *parser) readOnly(index int, reason string) {
	block := &parser.result.Blocks[index]
	block.ReadOnly = true
	if block.ReadOnlyReason == "" {
		block.ReadOnlyReason = reason
	}
}

func (parser *parser) markOpen(ctx context.Context, reason string) error {
	for _, frame := range parser.stack {
		if err := parseContextErr(ctx); err != nil {
			return err
		}
		if frame.block >= 0 {
			parser.readOnly(frame.block, reason)
		}
	}
	return nil
}

func (parser *parser) closeBlock(frame element, closeStart, end int) {
	block := &parser.result.Blocks[frame.block]
	block.CloseStart, block.End = closeStart, end
	if frame.unknownRoot {
		block.Visible = string(parser.source[block.Start:end])
	} else if !block.ReadOnly && len(block.Leaves) == 0 && (block.Kind == Paragraph || block.Kind == Heading || block.Kind == ListItem || block.Kind == TableCell) {
		block.Leaves = append(block.Leaves, Leaf{Start: frame.contentStart, End: frame.contentStart, Editable: true, sourceDigest: parser.digest})
	} else if !block.ReadOnly && len(block.Leaves) > 0 && block.Leaves[len(block.Leaves)-1].ImageSource != "" && block.Kind != Code {
		block.Leaves = append(block.Leaves, Leaf{Start: closeStart, End: closeStart, Editable: true, sourceDigest: parser.digest})
	}
}

func (parser *parser) start(token htmltoken.Token, start, end int, selfClosing bool) {
	tag := strings.ToLower(token.Data)
	frame := element{tag: tag, start: start, contentStart: end, block: -1, unsafe: unsafeAttributes(token.Attr)}
	void := selfClosing || tag == "br" || tag == "img" || tag == "hr" || tag == "meta" || tag == "link" || tag == "input"
	if parser.unknownIndex() >= 0 {
		if !void {
			parser.stack = append(parser.stack, frame)
		}
		return
	}
	active := parser.activeBlock()
	if tag == "ol" && active < 0 {
		frame.listNext = 1
		if startValue, err := strconv.Atoi(attribute(token.Attr, "start")); err == nil && startValue > 0 && startValue <= 1000000 {
			frame.listNext = startValue
		}
	}
	if tag == "tr" && active < 0 {
		parser.row++
		parser.column = 0
	}
	if tag == "br" || tag == "img" {
		parser.media(tag, token, start, end, active)
		return
	}
	if active < 0 {
		blockKind, level := blockKind(tag)
		switch blockKind {
		case Paragraph, Heading, ListItem, Quote, TableCell, Code:
			frame.block = parser.addBlock(blockKind, start)
			parser.result.Blocks[frame.block].Level = level
			parser.result.Blocks[frame.block].OpenEnd = end
			if blockKind == ListItem {
				for index := len(parser.stack) - 1; index >= 0; index-- {
					if parser.stack[index].tag == "ol" {
						parser.result.Blocks[frame.block].Number = parser.stack[index].listNext
						parser.stack[index].listNext++
						break
					}
					if parser.stack[index].tag == "ul" {
						break
					}
				}
			}
			if parser.unsafeAncestor() {
				parser.readOnly(frame.block, "unsafe HTML ancestor")
			}
			if blockKind == TableCell {
				parser.result.Blocks[frame.block].Row, parser.result.Blocks[frame.block].Column = parser.row, parser.column
				parser.column++
				tableStart := -1
				for index := len(parser.stack) - 1; index >= 0; index-- {
					if parser.stack[index].tag == "table" {
						tableStart = parser.stack[index].start
						break
					}
				}
				if tableStart < 0 {
					parser.readOnly(frame.block, "table cell outside its table")
				} else {
					parser.result.Blocks[frame.block].TableStart = tableStart
				}
			}
			if blockKind == Code {
				parser.readOnly(frame.block, "code block")
			}
			if (blockKind == ListItem && !parser.hasAncestor("ul", "ol")) || (blockKind == TableCell && !parser.hasAncestor("tr")) {
				parser.readOnly(frame.block, "element outside its container")
			}
		case Unknown:
			if !containerTag(tag) {
				frame.block = parser.addBlock(Unknown, start)
				parser.result.Blocks[frame.block].OpenEnd = end
				frame.unknownRoot = true
				parser.readOnly(frame.block, "unsupported HTML element")
			}
		}
	} else if !inlineTag(tag) && !nestedBlockWrapper(tag, parser.result.Blocks[active].Kind) {
		parser.readOnly(active, "unsupported nested HTML element")
	}
	if active >= 0 && frame.unsafe {
		parser.readOnly(active, "unsafe HTML attribute")
	}
	if frame.block >= 0 && frame.unsafe {
		parser.readOnly(frame.block, "unsafe HTML attribute")
	}
	if tag == "a" {
		frame.link = attribute(token.Attr, "href")
		frame.safeLink = safeLink(frame.link)
		if active >= 0 && frame.link != "" && !frame.safeLink {
			parser.readOnly(active, "unsafe link")
		}
	}
	if void {
		if frame.block >= 0 {
			parser.closeBlock(frame, end, end)
		}
		return
	}
	parser.stack = append(parser.stack, frame)
}

func blockKind(tag string) (Kind, int) {
	switch tag {
	case "p":
		return Paragraph, 0
	case "h1", "h2", "h3", "h4", "h5", "h6":
		return Heading, int(tag[1] - '0')
	case "li":
		return ListItem, 0
	case "blockquote":
		return Quote, 0
	case "td", "th":
		return TableCell, 0
	case "pre":
		return Code, 0
	default:
		return Unknown, 0
	}
}

func containerTag(tag string) bool {
	switch tag {
	case "ul", "ol", "table", "tbody", "tr":
		return true
	default:
		return false
	}
}

func inlineTag(tag string) bool {
	switch tag {
	case "strong", "b", "em", "i", "u", "s", "a", "code":
		return true
	default:
		return false
	}
}

func nestedBlockWrapper(tag string, kind Kind) bool {
	return tag == "p" && (kind == Quote || kind == ListItem || kind == TableCell)
}

func (parser *parser) hasAncestor(tags ...string) bool {
	for _, frame := range parser.stack {
		for _, tag := range tags {
			if frame.tag == tag {
				return true
			}
		}
	}
	return false
}

func (parser *parser) unsafeAncestor() bool {
	for _, frame := range parser.stack {
		if frame.unsafe {
			return true
		}
	}
	return false
}

func unsafeAttributes(attributes []htmltoken.Attribute) bool {
	for _, attribute := range attributes {
		key := strings.ToLower(attribute.Key)
		if strings.HasPrefix(key, "on") || key == "srcdoc" {
			return true
		}
	}
	return false
}

func attribute(attributes []htmltoken.Attribute, name string) string {
	for _, attribute := range attributes {
		if strings.EqualFold(attribute.Key, name) {
			return attribute.Val
		}
	}
	return ""
}

func safeLink(value string) bool {
	if value == "" || strings.ContainsAny(value, "\r\n\t") {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https", "http", "mailto":
		return true
	default:
		return false
	}
}

func safeAsset(value string) bool {
	if value == "" || strings.ContainsAny(value, "\\:\r\n\t") || strings.HasPrefix(value, "/") {
		return false
	}
	clean := path.Clean(value)
	return clean != "." && clean == value
}

func (parser *parser) media(tag string, token htmltoken.Token, start, end, active int) {
	if active < 0 {
		kind := Image
		if tag == "br" {
			kind = Paragraph
		}
		active = parser.addBlock(kind, start)
		parser.result.Blocks[active].End = end
	}
	block := &parser.result.Blocks[active]
	if parser.unsafeAncestor() {
		parser.readOnly(active, "unsafe HTML ancestor")
	}
	leaf := Leaf{Start: start, End: end, sourceDigest: parser.digest}
	if tag == "img" {
		leaf.Visible = attribute(token.Attr, "alt")
		leaf.ImageSource = attribute(token.Attr, "src")
		leaf.SafeAsset = safeAsset(leaf.ImageSource) && !parser.unsafeAncestor() && !unsafeAttributes(token.Attr)
		if unsafeAttributes(token.Attr) {
			parser.readOnly(active, "unsafe image attribute")
		}
	} else if len(block.Leaves) == 0 {
		leaf.Editable = !block.ReadOnly
	} else {
		leaf.Visible = "\n"
	}
	block.Leaves = append(block.Leaves, leaf)
}

func (parser *parser) text(raw []byte, start, end int) {
	if parser.unknownIndex() >= 0 {
		return
	}
	visible := html.UnescapeString(string(raw))
	index := parser.activeBlock()
	if index < 0 {
		if strings.TrimSpace(visible) != "" {
			index = parser.addBlock(Unknown, start)
			block := &parser.result.Blocks[index]
			block.End, block.Visible = end, visible
			parser.readOnly(index, "text outside a supported block")
		}
		return
	}
	block := &parser.result.Blocks[index]
	leaf := Leaf{Start: start, End: end, Visible: visible, Editable: !block.ReadOnly && block.Kind != Code, sourceDigest: parser.digest}
	for _, frame := range parser.stack {
		switch frame.tag {
		case "strong", "b":
			leaf.Bold = true
		case "em", "i":
			leaf.Italic = true
		case "u":
			leaf.Underline = true
		case "s":
			leaf.Strike = true
		case "code":
			leaf.Mono = true
		case "a":
			leaf.Link, leaf.SafeLink = frame.link, frame.safeLink
		}
	}
	block.Leaves = append(block.Leaves, leaf)
}

func (parser *parser) end(tag string, start, end int) {
	tag = strings.ToLower(tag)
	match := -1
	for index := len(parser.stack) - 1; index >= 0; index-- {
		if parser.stack[index].tag == tag {
			match = index
			break
		}
	}
	if match < 0 {
		if index := parser.activeBlock(); index >= 0 {
			parser.readOnly(index, "unmatched closing tag")
		} else if parser.unknownIndex() < 0 {
			index := parser.addBlock(Unknown, start)
			parser.result.Blocks[index].End, parser.result.Blocks[index].Visible = end, string(parser.source[start:end])
			parser.readOnly(index, "unmatched closing tag")
		}
		return
	}
	if match != len(parser.stack)-1 {
		parser.markOpen(nil, "mismatched closing tag")
	}
	for _, frame := range parser.stack[match:] {
		if frame.block >= 0 {
			parser.closeBlock(frame, start, end)
		}
	}
	parser.stack = parser.stack[:match]
}

func ReplaceLeaf(source []byte, leaf Leaf, visible string) ([]byte, error) {
	if !leaf.Editable || !utf8.ValidString(visible) || strings.ContainsAny(visible, "\x00\r\n") {
		return nil, ErrReadOnlyLeaf
	}
	if sha256.Sum256(source) != leaf.sourceDigest || leaf.Start < 0 || leaf.End < leaf.Start || leaf.End > len(source) || !utf8.Valid(source[:leaf.Start]) || !utf8.Valid(source[:leaf.End]) {
		return nil, ErrStaleLeaf
	}
	if visible == leaf.Visible {
		return append([]byte(nil), source...), nil
	}
	replacement := html.EscapeString(visible)
	raw := source[leaf.Start:leaf.End]
	switch {
	case string(raw) == leaf.Visible:
		prefix := 0
		for prefix < len(leaf.Visible) && prefix < len(visible) {
			original, size := utf8.DecodeRuneInString(leaf.Visible[prefix:])
			changed, _ := utf8.DecodeRuneInString(visible[prefix:])
			if original != changed {
				break
			}
			prefix += size
		}
		oldEnd, newEnd := len(leaf.Visible), len(visible)
		for oldEnd > prefix && newEnd > prefix {
			original, oldSize := utf8.DecodeLastRuneInString(leaf.Visible[:oldEnd])
			changed, newSize := utf8.DecodeLastRuneInString(visible[:newEnd])
			if original != changed {
				break
			}
			oldEnd -= oldSize
			newEnd -= newSize
		}
		replacement = string(raw[:prefix]) + html.EscapeString(visible[prefix:newEnd]) + string(raw[oldEnd:])
	case leaf.Visible != "" && strings.HasPrefix(visible, leaf.Visible):
		replacement = string(raw) + html.EscapeString(visible[len(leaf.Visible):])
	case leaf.Visible != "" && strings.HasSuffix(visible, leaf.Visible):
		replacement = html.EscapeString(visible[:len(visible)-len(leaf.Visible)]) + string(raw)
	}
	if html.UnescapeString(replacement) != visible {
		replacement = html.EscapeString(visible)
	}
	result := make([]byte, 0, len(source)-leaf.End+leaf.Start+len(replacement))
	result = append(result, source[:leaf.Start]...)
	result = append(result, replacement...)
	return append(result, source[leaf.End:]...), nil
}
