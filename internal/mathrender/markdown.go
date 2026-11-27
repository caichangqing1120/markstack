package mathrender

import (
	"bytes"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	gmparser "github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

type Inline struct {
	ast.BaseInline
	Range text.Segment
}

var KindInline = ast.NewNodeKind("MarkStackInlineMath")

func (*Inline) Kind() ast.NodeKind                 { return KindInline }
func (node *Inline) Dump(source []byte, level int) { ast.DumpHelper(node, source, level, nil, nil) }
func (node *Inline) Body(source []byte) string {
	return string(source[node.Range.Start+1 : node.Range.Stop-1])
}

type Display struct {
	ast.BaseBlock
	Start, End, BodyStart, BodyEnd int
	SingleLine                     bool
}

var KindDisplay = ast.NewNodeKind("MarkStackDisplayMath")

func (*Display) Kind() ast.NodeKind                 { return KindDisplay }
func (*Display) IsRaw() bool                        { return true }
func (node *Display) Dump(source []byte, level int) { ast.DumpHelper(node, source, level, nil, nil) }
func (node *Display) Body(source []byte) string     { return string(source[node.BodyStart:node.BodyEnd]) }

type Markdown struct{}

func (Markdown) Extend(markdown goldmark.Markdown) {
	markdown.Parser().AddOptions(gmparser.WithBlockParsers(util.Prioritized(displayParser{}, 650)), gmparser.WithInlineParsers(util.Prioritized(inlineParser{}, 150)))
}

type inlineParser struct{}

func (inlineParser) Trigger() []byte { return []byte{'$'} }

func (inlineParser) Parse(_ ast.Node, reader text.Reader, _ gmparser.Context) ast.Node {
	line, rangeAt := reader.PeekLine()
	source := reader.Source()
	start := rangeAt.Start
	if start > 0 && (source[start-1] == '$' || escaped(source, start)) || len(line) < 3 || line[0] != '$' || line[1] == '$' {
		return nil
	}
	end := inlineClose(line, 1, 1)
	if end < 0 {
		return nil
	}
	reader.Advance(end + 1)
	return &Inline{Range: text.NewSegment(start, start+end+1)}
}

func escaped(source []byte, position int) bool {
	end := position
	for position > 0 && source[position-1] == '\\' {
		position--
	}
	return (end-position)%2 != 0
}

func inlineClose(line []byte, offset, length int) int {
	remainder := line[offset:]
	first, _ := utf8.DecodeRune(remainder)
	if unicode.IsSpace(first) || first == '$' {
		return -1
	}
	if breakAt := bytes.IndexAny(remainder, "\r\n"); breakAt >= 0 {
		remainder = remainder[:breakAt]
	}
	for index, value := range remainder {
		position := offset + index
		if value == '`' && !escaped(line, position) {
			return -1
		}
		if value != '$' || escaped(line, position) {
			continue
		}
		end := position + length
		if index == 0 || end > len(line) || length == 2 && line[position+1] != '$' || end < len(line) && line[end] == '$' {
			return -1
		}
		last, _ := utf8.DecodeLastRune(remainder[:index])
		next, _ := utf8.DecodeRune(line[end:])
		if unicode.IsSpace(last) || unicode.IsDigit(next) || currencyLike(remainder[:index]) {
			return -1
		}
		return position
	}
	return -1
}

func currencyLike(body []byte) bool {
	first, _ := utf8.DecodeRune(body)
	if !unicode.IsDigit(first) {
		return false
	}
	return bytes.IndexAny(body, "\\^_={}+*/<>−") < 0 && bytes.ContainsAny(body, " \t")
}

type displayParser struct{}

func (displayParser) Trigger() []byte                               { return []byte{'$'} }
func (displayParser) CanInterruptParagraph() bool                   { return true }
func (displayParser) CanAcceptIndentedLine() bool                   { return false }
func (displayParser) Close(ast.Node, text.Reader, gmparser.Context) {}

func (displayParser) Open(parent ast.Node, reader text.Reader, ctx gmparser.Context) (ast.Node, gmparser.State) {
	line, span := reader.PeekLine()
	if parent.Kind() != ast.KindDocument || ctx.BlockOffset() != 0 || !bytes.HasPrefix(line, []byte("$$")) {
		return nil, gmparser.NoChildren
	}
	trimmed := bytes.TrimRight(line, " \t\r\n")
	if len(trimmed) != 2 {
		if end := inlineClose(trimmed, 2, 2); end >= 0 && end+2 == len(trimmed) {
			return &Display{Start: span.Start, End: span.Start + len(bytes.TrimRight(line, "\r\n")), BodyStart: span.Start + 2, BodyEnd: span.Start + end, SingleLine: true}, gmparser.NoChildren
		}
		return nil, gmparser.NoChildren
	}
	if len(line) == 0 || line[len(line)-1] != '\n' {
		return nil, gmparser.NoChildren
	}
	source := reader.Source()
	for position := span.Stop; position < len(source); {
		offset := bytes.IndexByte(source[position:], '\n')
		end := len(source)
		if offset >= 0 {
			end = position + offset
		}
		candidate := bytes.TrimSuffix(source[position:end], []byte("\r"))
		if bytes.Equal(bytes.TrimRight(candidate, " \t"), []byte("$$")) {
			return &Display{Start: span.Start, End: position + len(candidate), BodyStart: span.Stop, BodyEnd: position}, gmparser.NoChildren
		}
		fence := bytes.TrimLeft(candidate, " ")
		if len(candidate)-len(fence) < 4 && (bytes.HasPrefix(fence, []byte("```")) || bytes.HasPrefix(fence, []byte("~~~"))) {
			break
		}
		position = end + 1
	}
	return nil, gmparser.NoChildren
}

func (displayParser) Continue(node ast.Node, reader text.Reader, _ gmparser.Context) gmparser.State {
	display := node.(*Display)
	_, span := reader.PeekLine()
	if display.SingleLine || span.Start > display.BodyEnd {
		return gmparser.Close
	}
	reader.AdvanceToEOL()
	if span.Start == display.BodyEnd {
		return gmparser.Close
	}
	return gmparser.Continue | gmparser.NoChildren
}
