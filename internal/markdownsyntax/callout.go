package markdownsyntax

import (
	"bytes"
	"html"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

type Alert struct {
	Name, Chinese, Glyph string
	RGB, DarkRGB         [3]uint8
}

func AlertType(name string) (Alert, bool) {
	switch strings.ToLower(name) {
	case "note":
		return Alert{"note", "提示", "ⓘ", [3]uint8{30, 95, 166}, [3]uint8{125, 184, 255}}, true
	case "tip":
		return Alert{"tip", "技巧", "✦", [3]uint8{26, 115, 68}, [3]uint8{105, 211, 146}}, true
	case "important":
		return Alert{"important", "重要", "◆", [3]uint8{113, 66, 164}, [3]uint8{195, 155, 251}}, true
	case "warning":
		return Alert{"warning", "警告", "⚠", [3]uint8{142, 88, 10}, [3]uint8{248, 192, 104}}, true
	case "caution":
		return Alert{"caution", "注意", "!", [3]uint8{174, 48, 55}, [3]uint8{255, 146, 150}}, true
	default:
		return Alert{}, false
	}
}

func (alert Alert) Label() string { return strings.ToUpper(alert.Name) + " " + alert.Chinese }
func (alert Alert) Class() string { return "callout callout-" + alert.Name }

func AlertClass(class string) (Alert, bool) {
	name, found := strings.CutPrefix(class, "callout callout-")
	alert, ok := AlertType(name)
	return alert, found && ok && class == alert.Class()
}

type Callout struct {
	ast.BaseBlock
	Alert                            Alert
	Title                            string
	Start, HeaderEnd, BodyStart, End int
}

var kindCallout = ast.NewNodeKind("MarkStackCallout")

func (*Callout) Kind() ast.NodeKind                 { return kindCallout }
func (node *Callout) Dump(source []byte, level int) { ast.DumpHelper(node, source, level, nil, nil) }

type calloutParser struct{}

var quoteParser = parser.NewBlockquoteParser()

func (calloutParser) Trigger() []byte             { return []byte{'>'} }
func (calloutParser) CanInterruptParagraph() bool { return true }
func (calloutParser) CanAcceptIndentedLine() bool { return false }
func (calloutParser) Close(node ast.Node, reader text.Reader, _ parser.Context) {
	callout := node.(*Callout)
	source := reader.Source()
	_ = ast.Walk(node, func(child ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && child.Type() == ast.TypeBlock && child.Lines().Len() > 0 {
			end := child.Lines().At(child.Lines().Len() - 1).Stop
			for end > callout.End && (source[end-1] == '\r' || source[end-1] == '\n') {
				end--
			}
			callout.End = max(callout.End, end)
		}
		return ast.WalkContinue, nil
	})
}

func (calloutParser) Open(parent ast.Node, reader text.Reader, context parser.Context) (ast.Node, parser.State) {
	line, segment := reader.PeekLine()
	width, indent := util.IndentWidth(line, reader.LineOffset())
	if width > 3 || indent >= len(line) || line[indent] != '>' {
		return nil, parser.NoChildren
	}
	header := bytes.TrimSpace(line[indent+1:])
	if !bytes.HasPrefix(header, []byte("[!")) {
		return nil, parser.NoChildren
	}
	end := bytes.IndexByte(header, ']')
	if end < 2 || end+1 < len(header) && header[end+1] != ' ' && header[end+1] != '\t' {
		return nil, parser.NoChildren
	}
	alert, ok := AlertType(string(header[2:end]))
	if !ok {
		return nil, parser.NoChildren
	}
	headerEnd := segment.Start + len(bytes.TrimRight(line, "\r\n"))
	reader.Advance(headerEnd - segment.Start)
	return &Callout{Alert: alert, Title: string(bytes.TrimSpace(header[end+1:])), Start: segment.Start, HeaderEnd: headerEnd, BodyStart: max(segment.Stop, headerEnd+1), End: headerEnd}, parser.HasChildren
}

func (calloutParser) Continue(node ast.Node, reader text.Reader, context parser.Context) parser.State {
	line, segment := reader.PeekLine()
	result := quoteParser.Continue(node, reader, context)
	if result&parser.Continue != 0 {
		node.(*Callout).End = segment.Start + len(bytes.TrimRight(line, "\r\n"))
	}
	return result
}

func renderCallout(writer util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		callout := node.(*Callout)
		title := callout.Alert.Label()
		if callout.Title != "" {
			title += " · " + callout.Title
		}
		_, _ = writer.WriteString(`<div class="` + callout.Alert.Class() + `"><p class="callout-title"><strong>` + html.EscapeString(title) + "</strong></p>\n")
	} else {
		_, _ = writer.WriteString("</div>\n")
	}
	return ast.WalkContinue, nil
}
