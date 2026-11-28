package exporthtml

import (
	"bytes"
	"errors"
	"html"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
	"markstack/internal/markdownsyntax"
)

var gfmFragment = goldmark.New(goldmark.WithExtensions(extension.GFM, markdownsyntax.Markdown{}))

type fragmentBuffer struct {
	bytes.Buffer
}

func (fragment *fragmentBuffer) Write(data []byte) (int, error) {
	if len(data) > 32<<20-fragment.Len() {
		return 0, ErrTooLarge
	}
	return fragment.Buffer.Write(data)
}

func (fragment *fragmentBuffer) WriteString(data string) (int, error) {
	if len(data) > 32<<20-fragment.Len() {
		return 0, ErrTooLarge
	}
	return fragment.Buffer.WriteString(data)
}

func MarkdownFragment(source []byte) ([]byte, error) {
	if !utf8.Valid(source) {
		return nil, errors.New("Markdown export requires UTF-8")
	}
	if len(source) > 16<<20 {
		return nil, ErrTooLarge
	}
	source = bytes.TrimPrefix(source, []byte{0xef, 0xbb, 0xbf})
	source = markdownsyntax.StripMetadata(source)
	root := gfmFragment.Parser().Parse(text.NewReader(source))
	var fragment fragmentBuffer
	if err := gfmFragment.Renderer().Render(&fragment, source, root); err != nil {
		return nil, err
	}
	if raw := rawHTML(root, source); len(raw) != 0 {
		if _, err := fragment.WriteString("<h2>原始 HTML（未执行）</h2><pre>\n"); err != nil {
			return nil, err
		}
		for _, markup := range raw {
			if _, err := fragment.WriteString(html.EscapeString(strings.TrimRight(markup, "\r\n")) + "\n"); err != nil {
				return nil, err
			}
		}
		if _, err := fragment.WriteString("</pre>"); err != nil {
			return nil, err
		}
	}
	return fragment.Bytes(), nil
}
