package exportdocx

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type runFormat struct {
	bold, italic, strike, code, link, preserve bool
	underline, highlight                       bool
	vertical, color                            string
}

type inlineWriter struct {
	doc            *document
	width          int
	started, space bool
}

func (line *inlineWriter) children(node *html.Node, format runFormat) {
	for child := node.FirstChild; child != nil && line.doc.body.err == nil && line.doc.rels.err == nil; child = child.NextSibling {
		line.node(child, format)
	}
}

func (line *inlineWriter) node(node *html.Node, format runFormat) {
	if ignored(node) {
		return
	}
	if node.Type == html.TextNode {
		line.text(node.Data, format)
		return
	}
	switch node.DataAtom {
	case atom.Strong, atom.B:
		format.bold = true
	case atom.Em, atom.I:
		format.italic = true
	case atom.Del, atom.S, atom.Strike:
		format.strike = true
	case atom.Code:
		format.code = !format.preserve
	case atom.U:
		format.underline = line.doc.markdown
	case atom.Mark:
		format.highlight = line.doc.markdown
	case atom.Sub, atom.Sup:
		if line.doc.markdown {
			format.vertical = "subscript"
			if node.DataAtom == atom.Sup {
				format.vertical = "superscript"
			}
		}
	case atom.A:
		target := strings.TrimSpace(attribute(node, "href"))
		if !format.link && safeLink(target) {
			id := line.doc.links[target]
			if id == 0 {
				id = line.doc.relationship("hyperlink", target, true)
				line.doc.links[target] = id
			}
			line.flushSpace(format)
			fmt.Fprintf(&line.doc.body, `<w:hyperlink r:id="rId%d">`, id)
			format.link = true
			line.children(node, format)
			line.doc.body.write(`</w:hyperlink>`)
			return
		}
	case atom.Br:
		line.breakLine()
		return
	case atom.Img:
		picture := line.doc.image(attribute(node, "src"))
		alt := attribute(node, "alt")
		if picture.id == 0 {
			line.text(alt, format)
		} else {
			line.flushSpace(format)
			line.doc.draw(picture, line.width, alt)
			line.started = true
		}
		return
	case atom.Input:
		if attribute(node, "type") == "checkbox" {
			glyph := "☐ "
			for _, attr := range node.Attr {
				if attr.Key == "checked" {
					glyph = "☑ "
				}
			}
			line.text(glyph, format)
		}
		return
	}
	line.children(node, format)
}

func (line *inlineWriter) flushSpace(format runFormat) {
	if line.space && line.started {
		line.run(" ", format)
	}
	line.space = false
}

func (line *inlineWriter) breakLine() {
	line.doc.body.write(`<w:r><w:br/></w:r>`)
	line.started, line.space = false, false
}

func (line *inlineWriter) text(value string, format runFormat) {
	if value == "" {
		return
	}
	if format.code || format.preserve {
		line.flushSpace(format)
		for {
			index := strings.IndexAny(value, "\n\t")
			if index < 0 {
				if value != "" {
					line.run(value, format)
					line.started = true
				}
				break
			}
			if index > 0 {
				line.run(value[:index], format)
			}
			if value[index] == '\n' {
				line.breakLine()
			} else {
				line.doc.body.write(`<w:r><w:tab/></w:r>`)
				line.started = true
			}
			value = value[index+1:]
		}
		return
	}
	var normalized strings.Builder
	started := line.started
	for _, character := range value {
		switch character {
		case ' ', '\t', '\n', '\r', '\f':
			line.space = true
		default:
			if line.space && started {
				normalized.WriteByte(' ')
			}
			normalized.WriteRune(character)
			line.space = false
			started = true
		}
	}
	if normalized.Len() != 0 {
		line.run(normalized.String(), format)
		line.started = true
	}
}

func (line *inlineWriter) run(value string, format runFormat) {
	out := &line.doc.body
	out.write(`<w:r>`)
	if format.bold || format.italic || format.strike || format.code || format.link || format.underline || format.highlight || format.vertical != "" || format.color != "" {
		out.write(`<w:rPr>`)
		if format.link {
			out.write(`<w:rStyle w:val="Hyperlink"/>`)
		} else if format.code {
			out.write(`<w:rStyle w:val="InlineCode"/>`)
		}
		if format.link && format.code {
			out.write(monoFonts)
		}
		if format.bold {
			out.write(`<w:b/>`)
		}
		if format.italic {
			out.write(`<w:i/>`)
		}
		if format.strike {
			out.write(`<w:strike/>`)
		}
		if format.color != "" {
			fmt.Fprintf(out, `<w:color w:val="%s"/>`, format.color)
		}
		if format.highlight {
			out.write(`<w:highlight w:val="yellow"/>`)
		}
		if format.underline {
			out.write(`<w:u w:val="single"/>`)
		}
		if format.link && format.code {
			out.write(codeShade)
		}
		if format.vertical != "" {
			fmt.Fprintf(out, `<w:vertAlign w:val="%s"/>`, format.vertical)
		}
		out.write(`</w:rPr>`)
	}
	out.write(`<w:t xml:space="preserve">`)
	out.escape(value)
	out.write(`</w:t></w:r>`)
}

func safeLink(target string) bool {
	if strings.ContainsRune(target, '\\') || strings.IndexFunc(target, func(character rune) bool {
		return unicode.IsControl(character) || unicode.IsSpace(character) || character == 0xfffe || character == 0xffff
	}) >= 0 {
		return false
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return false
	}
	switch parsed.Scheme {
	case "http", "https":
		return parsed.Hostname() != ""
	case "mailto":
		return parsed.Opaque != "" && parsed.Host == ""
	}
	return false
}
