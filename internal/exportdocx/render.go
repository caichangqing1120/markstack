package exportdocx

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"markstack/internal/exporthtml"
)

type document struct {
	body, rels            limitedBuffer
	loadImage             func(string) ([]byte, error)
	images                map[string]picture
	links                 map[string]int
	media                 []part
	lists                 []listNumber
	mediaSize             int
	drawingID, relationID int
}

func RenderMarkdown(source []byte, loadImage func(string) ([]byte, error)) ([]byte, error) {
	fragment, err := exporthtml.MarkdownFragment(source)
	if errors.Is(err, exporthtml.ErrTooLarge) {
		return nil, ErrTooLarge
	}
	if err != nil {
		return nil, err
	}
	return render(fragment, loadImage)
}

func RenderRichHTML(source []byte, loadImage func(string) ([]byte, error)) ([]byte, error) {
	if len(source) > 16<<20 {
		return nil, ErrTooLarge
	}
	if !utf8.Valid(source) {
		return nil, errors.New("DOCX export requires UTF-8")
	}
	return render(bytes.TrimPrefix(source, []byte{0xef, 0xbb, 0xbf}), loadImage)
}

func render(source []byte, loadImage func(string) ([]byte, error)) ([]byte, error) {
	root := &html.Node{Type: html.ElementNode, DataAtom: atom.Div, Data: "div"}
	nodes, err := html.ParseFragment(bytes.NewReader(source), root)
	if err != nil {
		return nil, err
	}
	for _, node := range nodes {
		root.AppendChild(node)
	}
	doc := document{loadImage: loadImage, images: make(map[string]picture), links: make(map[string]int)}
	doc.body.write(xml.Header + `<w:document xmlns:w="` + wordNS + `" xmlns:r="` + relsNS + `" xmlns:wp="` + drawingNS + `wordprocessingDrawing" xmlns:a="` + drawingNS + `main" xmlns:pic="` + drawingNS + `picture"><w:body>`)
	doc.rels.write(xml.Header + `<Relationships xmlns="` + packageNS + `relationships">`)
	doc.relationship("styles", "styles.xml", false)
	doc.relationship("numbering", "numbering.xml", false)
	doc.blocks(root, blockContext{width: pageWidth})
	doc.body.write(`<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1800" w:bottom="1440" w:left="1800" w:header="720" w:footer="720" w:gutter="0"/></w:sectPr></w:body></w:document>`)
	doc.rels.write(`</Relationships>`)
	return doc.pack()
}

type blockContext struct {
	width int
	quote bool
	list  *listItem
}

type listItem struct {
	numID, level int
	pending      bool
}

func (ctx blockContext) imageWidth() int {
	width := ctx.width
	if ctx.list != nil {
		width -= (ctx.list.level + 1) * listStep
	} else if ctx.quote {
		width -= 360
	}
	return max(1, width)
}

func (doc *document) startParagraph(ctx blockContext, style string, rule bool) *inlineWriter {
	if style == "" && ctx.quote {
		style = "Quote"
	}
	indent := 0
	if style != "" || ctx.list != nil || rule {
		doc.body.write(`<w:p><w:pPr>`)
		if style != "" {
			fmt.Fprintf(&doc.body, `<w:pStyle w:val="%s"/>`, style)
		}
		if ctx.list != nil {
			if ctx.list.pending {
				fmt.Fprintf(&doc.body, `<w:numPr><w:ilvl w:val="%d"/><w:numId w:val="%d"/></w:numPr>`, ctx.list.level, ctx.list.numID)
				ctx.list.pending = false
			} else {
				indent = (ctx.list.level + 1) * listStep
			}
		}
		if rule {
			doc.body.write(`<w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="B5BEC5"/></w:pBdr>`)
		}
		if indent != 0 {
			fmt.Fprintf(&doc.body, `<w:ind w:left="%d"/>`, indent)
		}
		doc.body.write(`</w:pPr>`)
	} else {
		doc.body.write(`<w:p>`)
	}
	return &inlineWriter{doc: doc, width: ctx.imageWidth()}
}

func (doc *document) blocks(parent *html.Node, ctx blockContext) {
	var paragraph *inlineWriter
	closeParagraph := func() {
		if paragraph != nil {
			doc.body.write(`</w:p>`)
			paragraph = nil
		}
	}
	for node := parent.FirstChild; node != nil && doc.body.err == nil && doc.rels.err == nil; node = node.NextSibling {
		if ignored(node) {
			continue
		}
		if node.Type == html.ElementNode {
			switch node.DataAtom {
			case atom.P, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Pre, atom.Hr:
				closeParagraph()
				style := ""
				if len(node.Data) == 2 && node.Data[0] == 'h' && node.DataAtom != atom.Hr {
					style = "Heading" + node.Data[1:]
				} else if node.DataAtom == atom.Pre {
					style = "CodeBlock"
				}
				line := doc.startParagraph(ctx, style, node.DataAtom == atom.Hr)
				line.children(node, runFormat{preserve: node.DataAtom == atom.Pre})
				doc.body.write(`</w:p>`)
				continue
			case atom.Ul, atom.Ol, atom.Table, atom.Blockquote, atom.Div, atom.Section, atom.Article, atom.Main, atom.Figure, atom.Figcaption:
				closeParagraph()
				switch node.DataAtom {
				case atom.Ul, atom.Ol, atom.Table:
					doc.finishItem(ctx)
					if node.DataAtom == atom.Table {
						doc.table(node, ctx)
					} else {
						doc.list(node, ctx)
					}
				case atom.Blockquote:
					quote := ctx
					quote.quote = true
					doc.blocks(node, quote)
				default:
					doc.blocks(node, ctx)
				}
				continue
			}
		}
		if paragraph == nil {
			if node.Type == html.TextNode && strings.Trim(node.Data, " \t\n\r\f") == "" {
				continue
			}
			paragraph = doc.startParagraph(ctx, "", false)
		}
		paragraph.node(node, runFormat{})
	}
	closeParagraph()
}

func (doc *document) finishItem(ctx blockContext) {
	if ctx.list != nil && ctx.list.pending {
		doc.startParagraph(ctx, "", false)
		doc.body.write(`</w:p>`)
	}
}

func (doc *document) list(node *html.Node, ctx blockContext) {
	level := 0
	if ctx.list != nil {
		level = min(ctx.list.level+1, listLevels-1)
	}
	start := int32(1)
	if value, err := strconv.ParseInt(attribute(node, "start"), 10, 32); err == nil {
		start = int32(value)
	}
	doc.lists = append(doc.lists, listNumber{ordered: node.DataAtom == atom.Ol, level: level, start: start})
	numID := len(doc.lists)
	for item := node.FirstChild; item != nil && doc.body.err == nil && doc.rels.err == nil; item = item.NextSibling {
		if item.DataAtom == atom.Li {
			ctx.list = &listItem{numID: numID, level: level, pending: true}
			doc.blocks(item, ctx)
			doc.finishItem(ctx)
		}
	}
}

func attribute(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if attr.Namespace == "" && attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

func ignored(node *html.Node) bool {
	if node.Type == html.TextNode {
		return false
	}
	if node.Type != html.ElementNode || node.Namespace != "" {
		return true
	}
	switch node.DataAtom {
	case atom.Script, atom.Style, atom.Iframe, atom.Object, atom.Embed, atom.Form, atom.Template, atom.Noscript:
		return true
	}
	return false
}
