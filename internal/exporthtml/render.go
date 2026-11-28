package exporthtml

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	mdtext "github.com/yuin/goldmark/text"
	htmlnode "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var ErrTooLarge = errors.New("HTML export is too large")

const maxImageBytes = 8 << 20

const documentStyle = `
html{color:#293238;background:#fff;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;font-size:16px;line-height:1.65}
body{max-width:840px;margin:48px auto;padding:0 28px;overflow-wrap:anywhere}
h1,h2,h3,h4,h5,h6{line-height:1.3;margin:1.3em 0 .5em}
h1{font-size:2em;border-bottom:1px solid #dce2e6;padding-bottom:.25em}h2{font-size:1.5em;border-bottom:1px solid #e5e9ec;padding-bottom:.2em}
p,ul,ol,blockquote,pre,table{margin:0 0 1em}blockquote{border-left:3px solid #76a2b6;margin-left:0;padding-left:16px;color:#58646a}
pre{background:#f3f5f6;padding:16px;overflow-x:auto}code{font-family:ui-monospace,SFMono-Regular,monospace}p code,li code{background:#f3f5f6;padding:1px 3px}
table{border-collapse:collapse;width:100%}th,td{border:1px solid #dce2e6;padding:7px 10px;text-align:left}th{background:#f3f5f6}
img{display:block;max-width:100%;height:auto;margin:12px 0}.missing-image,.unsupported{color:#657179}.unsupported pre{white-space:pre-wrap}
a{color:#20779a}del{color:#69747a}
.markstack-vector-block{display:block;max-width:100%;height:auto;margin:0 auto 1em}.markstack-vector-inline{max-width:100%}
@media print{body{max-width:none;margin:0;padding:0}a{color:inherit}pre,blockquote,table,img{break-inside:avoid}}
@media(max-width:700px){body{margin:24px auto;padding:0 18px}}
`

var allowedTags = map[string]atom.Atom{
	"p": atom.P, "h1": atom.H1, "h2": atom.H2, "h3": atom.H3, "h4": atom.H4, "h5": atom.H5, "h6": atom.H6,
	"ul": atom.Ul, "ol": atom.Ol, "li": atom.Li, "blockquote": atom.Blockquote,
	"pre": atom.Pre, "code": atom.Code, "strong": atom.Strong, "em": atom.Em, "del": atom.Del,
	"a": atom.A, "table": atom.Table, "thead": atom.Thead, "tbody": atom.Tbody,
	"tr": atom.Tr, "th": atom.Th, "td": atom.Td, "hr": atom.Hr, "br": atom.Br,
}

type imageValue struct {
	uri string
	ok  bool
}

type sanitizer struct {
	load        func(string) ([]byte, error)
	cache       map[string]imageValue
	unsupported []string
	limit       int
	used        int
	vectors     []Vector
	pdf         bool
}

func RenderMarkdown(source []byte, loadImage func(string) ([]byte, error)) ([]byte, error) {
	return renderMarkdown(source, loadImage, 32<<20)
}

func MarkdownForPDF(source []byte, loadImage func(string) ([]byte, error)) ([]byte, []Vector, error) {
	return renderMarkdownDocument(source, loadImage, 32<<20, true)
}

func RenderRichHTML(source []byte, loadImage func(string) ([]byte, error)) ([]byte, error) {
	if !utf8.Valid(source) {
		return nil, errors.New("HTML export requires UTF-8")
	}
	if len(source) > 16<<20 {
		return nil, ErrTooLarge
	}
	return renderFragment(bytes.TrimPrefix(source, []byte{0xef, 0xbb, 0xbf}), nil, loadImage, 32<<20, nil, false)
}

func renderMarkdown(source []byte, loadImage func(string) ([]byte, error), limit int) ([]byte, error) {
	output, _, err := renderMarkdownDocument(source, loadImage, limit, false)
	return output, err
}

func renderMarkdownDocument(source []byte, loadImage func(string) ([]byte, error), limit int, pdf bool) ([]byte, []Vector, error) {
	if !utf8.Valid(source) {
		return nil, nil, errors.New("HTML export requires UTF-8")
	}
	if len(source) > 16<<20 || limit < 1 {
		return nil, nil, ErrTooLarge
	}
	source = bytes.TrimPrefix(source, []byte{0xef, 0xbb, 0xbf})
	root := markdownParser.Parser().Parse(mdtext.NewReader(source))
	raw := rawHTML(root, source)
	vectors, err := collectVectors(root, source, limit)
	if err != nil {
		return nil, nil, err
	}
	var fragment bytes.Buffer
	if err := markdownParser.Renderer().Render(&fragment, source, root); err != nil {
		return nil, nil, err
	}
	output, err := renderFragment(fragment.Bytes(), raw, loadImage, limit, vectors, pdf)
	return output, vectors, err
}

func renderFragment(fragment []byte, raw []string, loadImage func(string) ([]byte, error), limit int, vectors []Vector, pdf bool) ([]byte, error) {
	context := &htmlnode.Node{Type: htmlnode.ElementNode, DataAtom: atom.Div, Data: "div"}
	nodes, err := htmlnode.ParseFragment(bytes.NewReader(fragment), context)
	if err != nil {
		return nil, err
	}
	clean := &htmlnode.Node{Type: htmlnode.ElementNode, DataAtom: atom.Div, Data: "div"}
	filter := &sanitizer{load: loadImage, cache: make(map[string]imageValue), limit: limit, vectors: vectors, pdf: pdf}
	for _, node := range nodes {
		if err := filter.append(clean, node); err != nil {
			return nil, err
		}
	}
	raw = append(raw, filter.unsupported...)
	var output bytes.Buffer
	output.WriteString(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data:; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'"><meta name="viewport" content="width=device-width,initial-scale=1"><title>墨栈导出</title><style>`)
	output.WriteString(documentStyle)
	output.WriteString(`</style></head><body><main>`)
	if output.Len() > limit {
		return nil, ErrTooLarge
	}
	for child := clean.FirstChild; child != nil; child = child.NextSibling {
		if err := htmlnode.Render(&output, child); err != nil {
			return nil, err
		}
		if output.Len() > limit {
			return nil, ErrTooLarge
		}
	}
	output.WriteString(`</main>`)
	if len(raw) > 0 {
		output.WriteString(`<section class="unsupported"><h2>原始 HTML（未执行）</h2><pre>`)
		output.WriteString(html.EscapeString(strings.Join(raw, "\n")))
		output.WriteString(`</pre></section>`)
	}
	output.WriteString(`</body></html>`)
	if output.Len() > limit {
		return nil, ErrTooLarge
	}
	return output.Bytes(), nil
}

func (filter *sanitizer) append(parent, source *htmlnode.Node) error {
	if source.Type == htmlnode.TextNode {
		parent.AppendChild(&htmlnode.Node{Type: htmlnode.TextNode, Data: source.Data})
		return nil
	}
	if source.Type != htmlnode.ElementNode {
		return nil
	}
	tag := strings.ToLower(source.Data)
	if tag == "markstack-vector" && len(filter.vectors) > 0 {
		index, err := strconv.Atoi(attribute(source, "data-index"))
		if err == nil && index >= 0 && index < len(filter.vectors) {
			return filter.vector(parent, index)
		}
	}
	if tag == "script" || tag == "style" || tag == "iframe" || tag == "object" || tag == "form" || tag == "svg" || tag == "math" {
		filter.recordUnsupported(source)
		return nil
	}
	if tag == "img" {
		alt, destination := attribute(source, "alt"), attribute(source, "src")
		image := filter.image(destination)
		if !image.ok {
			fallback := &htmlnode.Node{Type: htmlnode.ElementNode, DataAtom: atom.Span, Data: "span", Attr: []htmlnode.Attribute{{Key: "class", Val: "missing-image"}}}
			fallback.AppendChild(&htmlnode.Node{Type: htmlnode.TextNode, Data: "[图片不可用: " + alt + "]"})
			parent.AppendChild(fallback)
			return nil
		}
		parent.AppendChild(&htmlnode.Node{Type: htmlnode.ElementNode, DataAtom: atom.Img, Data: "img", Attr: []htmlnode.Attribute{{Key: "src", Val: image.uri}, {Key: "alt", Val: alt}}})
		return nil
	}
	if tag == "input" && attribute(source, "type") == "checkbox" {
		glyph := "☐ "
		for _, entry := range source.Attr {
			if entry.Key == "checked" {
				glyph = "☑ "
			}
		}
		parent.AppendChild(&htmlnode.Node{Type: htmlnode.TextNode, Data: glyph})
		return nil
	}
	kind, ok := allowedTags[tag]
	if !ok {
		filter.recordUnsupported(source)
		for child := source.FirstChild; child != nil; child = child.NextSibling {
			if err := filter.append(parent, child); err != nil {
				return err
			}
		}
		return nil
	}
	if tag == "a" && !safeLink(attribute(source, "href")) {
		kind, tag = atom.Span, "span"
	}
	clean := &htmlnode.Node{Type: htmlnode.ElementNode, DataAtom: kind, Data: tag}
	if tag == "a" {
		clean.Attr = append(clean.Attr, htmlnode.Attribute{Key: "href", Val: attribute(source, "href")})
	}
	if tag == "ol" {
		if start, err := strconv.Atoi(attribute(source, "start")); err == nil && start > 0 && start <= 100000 {
			clean.Attr = append(clean.Attr, htmlnode.Attribute{Key: "start", Val: strconv.Itoa(start)})
		}
	}
	parent.AppendChild(clean)
	for child := source.FirstChild; child != nil; child = child.NextSibling {
		if err := filter.append(clean, child); err != nil {
			return err
		}
	}
	return nil
}

func (filter *sanitizer) vector(parent *htmlnode.Node, index int) error {
	vector := filter.vectors[index]
	if len(vector.Scene.Ops) == 0 {
		code := &htmlnode.Node{Type: htmlnode.ElementNode, DataAtom: atom.Code, Data: "code"}
		code.AppendChild(&htmlnode.Node{Type: htmlnode.TextNode, Data: vector.Label + " [不支持的公式]"})
		if !vector.Inline {
			pre := &htmlnode.Node{Type: htmlnode.ElementNode, DataAtom: atom.Pre, Data: "pre"}
			pre.AppendChild(code)
			parent.AppendChild(pre)
		} else {
			parent.AppendChild(code)
		}
		return nil
	}
	if filter.pdf {
		node := &htmlnode.Node{Type: htmlnode.ElementNode, Data: "markstack-vector", Attr: []htmlnode.Attribute{{Key: "data-index", Val: strconv.Itoa(index)}}}
		parent.AppendChild(node)
		return nil
	}
	svg := vector.Scene.SVG(vector.Label, vector.Inline)
	if len(svg) > filter.limit-filter.used {
		return ErrTooLarge
	}
	filter.used += len(svg)
	context := &htmlnode.Node{Type: htmlnode.ElementNode, DataAtom: atom.Div, Data: "div"}
	nodes, err := htmlnode.ParseFragment(strings.NewReader(svg), context)
	for _, node := range nodes {
		parent.AppendChild(node)
	}
	return err
}

func (filter *sanitizer) recordUnsupported(node *htmlnode.Node) {
	var output bytes.Buffer
	if htmlnode.Render(&output, node) == nil {
		filter.unsupported = append(filter.unsupported, output.String())
	}
}

func (filter *sanitizer) image(destination string) imageValue {
	if !safeImagePath(destination) || filter.load == nil {
		return imageValue{}
	}
	if cached, ok := filter.cache[destination]; ok {
		return cached
	}
	filter.cache[destination] = imageValue{}
	data, err := filter.load(destination)
	if err != nil || len(data) > maxImageBytes {
		return imageValue{}
	}
	configuration, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "png" && format != "jpeg" && format != "gif" || configuration.Width <= 0 || configuration.Height <= 0 || configuration.Width > 4096 || configuration.Height > 4096 || configuration.Width > (8<<20)/configuration.Height {
		return imageValue{}
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return imageValue{}
	}
	encoded := base64.StdEncoding.EncodedLen(len(data))
	if filter.used+encoded > filter.limit {
		return imageValue{}
	}
	filter.used += encoded
	value := imageValue{uri: fmt.Sprintf("data:image/%s;base64,%s", format, base64.StdEncoding.EncodeToString(data)), ok: true}
	filter.cache[destination] = value
	return value
}

func safeImagePath(destination string) bool {
	return destination != "" && !strings.HasPrefix(destination, "/") && !strings.ContainsAny(destination, "\\:\x00\r\n\t") && path.Clean(destination) == destination
}

func safeLink(destination string) bool {
	parsed, err := url.Parse(strings.TrimSpace(destination))
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return parsed.Host != ""
	case "mailto":
		return parsed.Opaque != "" || parsed.Path != ""
	default:
		return false
	}
}

func attribute(node *htmlnode.Node, name string) string {
	for _, entry := range node.Attr {
		if entry.Key == name {
			return entry.Val
		}
	}
	return ""
}

func rawHTML(root ast.Node, source []byte) []string {
	var raw []string
	var visit func(ast.Node)
	visit = func(node ast.Node) {
		switch typed := node.(type) {
		case *ast.RawHTML:
			raw = append(raw, string(typed.Segments.Value(source)))
		case *ast.HTMLBlock:
			var lines []string
			for index := range typed.Lines().Len() {
				segment := typed.Lines().At(index)
				lines = append(lines, string(segment.Value(source)))
			}
			if typed.HasClosure() {
				lines = append(lines, string(typed.ClosureLine.Value(source)))
			}
			raw = append(raw, strings.Join(lines, "\n"))
		}
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			visit(child)
		}
	}
	visit(root)
	return raw
}
