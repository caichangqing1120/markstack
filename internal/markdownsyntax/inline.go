package markdownsyntax

import (
	"bytes"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

type Style uint8

const (
	Highlight Style = iota
	Underline
	Subscript
	Superscript
)

var styleTags = [...]string{"mark", "u", "sub", "sup"}

type Inline struct {
	ast.BaseInline
	Style       Style
	Open, Close text.Segment
}

var kindInline = ast.NewNodeKind("MarkStackInlineStyle")
var kindToken = ast.NewNodeKind("MarkStackInlineToken")

func (*Inline) Kind() ast.NodeKind                 { return kindInline }
func (node *Inline) Dump(source []byte, level int) { ast.DumpHelper(node, source, level, nil, nil) }

type token struct {
	ast.BaseInline
	segment            text.Segment
	style              Style
	shorthand, closing bool
}

func (*token) Kind() ast.NodeKind                 { return kindToken }
func (node *token) Dump(source []byte, level int) { ast.DumpHelper(node, source, level, nil, nil) }

type Markdown struct{}

func (Markdown) Extend(markdown goldmark.Markdown) {
	markdown.Parser().AddOptions(
		parser.WithInlineParsers(util.Prioritized(inlineParser{}, 350)),
		parser.WithBlockParsers(util.Prioritized(calloutParser{}, 790)),
		parser.WithASTTransformers(util.Prioritized(inlinePairs{}, 100)),
	)
	markdown.Renderer().AddOptions(renderer.WithNodeRenderers(util.Prioritized(syntaxRenderer{}, 500)))
}

type inlineParser struct{}

func (inlineParser) Trigger() []byte { return []byte{'<', '=', '~', '^'} }

func (inlineParser) Parse(parent ast.Node, reader text.Reader, context parser.Context) ast.Node {
	line, segment := reader.PeekLine()
	if len(line) == 0 || segment.Padding != 0 {
		return nil
	}
	parsed := &token{}
	width := 0
	if line[0] == '<' {
		for style := Underline; style <= Superscript; style++ {
			name := styleTags[style]
			if bytes.HasPrefix(line, []byte("<"+name+">")) {
				parsed.style, width = style, len(name)+2
				break
			}
			if bytes.HasPrefix(line, []byte("</"+name+">")) {
				parsed.style, width, parsed.closing = style, len(name)+3, true
				break
			}
		}
	} else {
		marker := line[0]
		for width < len(line) && line[width] == marker {
			width++
		}
		if reader.PrecendingCharacter() == rune(marker) {
			return nil
		}
		if marker == '~' && width == 2 {
			return extension.NewStrikethroughParser().Parse(parent, reader, context)
		}
		parsed.shorthand = true
		switch {
		case marker == '=' && width == 2:
			parsed.style = Highlight
		case marker == '~' && width == 1:
			parsed.style = Subscript
		case marker == '^' && width == 1:
			parsed.style = Superscript
		default:
			reader.Advance(width)
			return ast.NewTextSegment(segment.WithStop(segment.Start + width))
		}
	}
	if width == 0 {
		return nil
	}
	parsed.segment = segment.WithStop(segment.Start + width)
	reader.Advance(width)
	return parsed
}

type inlinePairs struct{}

func (inlinePairs) Transform(root *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	_ = ast.Walk(root, func(parent ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			return ast.WalkContinue, nil
		}
		var pending [6]*token
		for child := parent.FirstChild(); child != nil; {
			next := child.NextSibling()
			if mark, ok := child.(*token); ok {
				key := int(mark.style)
				if mark.shorthand && mark.style != Highlight {
					key += 2
				}
				if open := pending[key]; open != nil && (mark.closing || mark.shorthand) && validPair(source, open, mark) {
					span := &Inline{Style: mark.style, Open: open.segment, Close: mark.segment}
					parent.InsertBefore(parent, open, span)
					for inner := open.NextSibling(); inner != mark; {
						following := inner.NextSibling()
						if literal, ok := inner.(*token); ok {
							span.AppendChild(span, ast.NewTextSegment(literal.segment))
							parent.RemoveChild(parent, literal)
						} else {
							span.AppendChild(span, inner)
						}
						inner = following
					}
					parent.RemoveChild(parent, open)
					parent.RemoveChild(parent, mark)
					for index, other := range pending {
						if other != nil && other.segment.Start >= open.segment.Start {
							pending[index] = nil
						}
					}
				} else if !mark.closing {
					pending[key] = mark
				}
			}
			child = next
		}
		for child := parent.FirstChild(); child != nil; {
			next := child.NextSibling()
			if literal, ok := child.(*token); ok {
				parent.ReplaceChild(parent, child, ast.NewTextSegment(literal.segment))
			}
			child = next
		}
		return ast.WalkContinue, nil
	})
}

func validPair(source []byte, open, close *token) bool {
	if open.segment.Stop >= close.segment.Start {
		return false
	}
	body := source[open.segment.Stop:close.segment.Start]
	if bytes.ContainsAny(body, "\r\n") {
		return false
	}
	if !open.shorthand {
		return true
	}
	if len(bytes.TrimSpace(body)) != len(body) {
		return false
	}
	return open.style == Highlight || bytes.IndexFunc(body, unicode.IsSpace) < 0
}

type syntaxRenderer struct{}

func (syntaxRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindInline, renderInline)
	reg.Register(kindCallout, renderCallout)
}

func renderInline(writer util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	tag := styleTags[node.(*Inline).Style]
	if entering {
		_, _ = writer.WriteString("<" + tag + ">")
	} else {
		_, _ = writer.WriteString("</" + tag + ">")
	}
	return ast.WalkContinue, nil
}
