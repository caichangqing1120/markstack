package markdownsyntax

import (
	"bytes"
	"strings"
	"testing"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

func TestMetadataDetection(t *testing.T) {
	for _, sample := range []struct {
		name, source, body string
	}{
		{"BOM and CRLF", "\ufeff---\r\ntitle: Heading\r\n---\r\n# Text\r\n", "\r\n# Text\r\n"},
		{"mapping and block scalar", "---\ntitle: 'quoted title'\ntags: [one, two]\nsummary: |-\n  prose: not a key\n---\nBody", "\nBody"},
		{"nested lists", "---\noptions:\n  one:\n    - first\n    - second\n---", ""},
		{"flow mapping root", "---\n{title: A, tags: [one, two]}\n---\nBody", "\nBody"},
		{"bad flow sequence", "---\ntags: [a,,b]\n---\nBody", "---\ntags: [a,,b]\n---\nBody"},
		{"Setext heading", "---\nA normal heading\n---\nBody", "---\nA normal heading\n---\nBody"},
		{"after paragraph", "Body\n\n---\ntitle: Value\n---", "Body\n\n---\ntitle: Value\n---"},
		{"unclosed fence", "---\ntitle: Value\nBody", "---\ntitle: Value\nBody"},
		{"unterminated flow", "---\ntags: [first, second\n---\nBody", "---\ntags: [first, second\n---\nBody"},
		{"unterminated string", "---\ntitle: \"missing\n---\nBody", "---\ntitle: \"missing\n---\nBody"},
		{"markdown-like colon", "---\ntitle: a line: of text\n---", "---\ntitle: a line: of text\n---"},
		{"tabs for indentation", "---\ntitle:\n\t- Value\n---", "---\ntitle:\n\t- Value\n---"},
	} {
		t.Run(sample.name, func(t *testing.T) {
			if got := string(StripMetadata([]byte(sample.source))); got != sample.body {
				t.Fatalf("got %q, want %q", got, sample.body)
			}
		})
	}
}

func TestInlineStylesRespectMarkdownBoundaries(t *testing.T) {
	markdown := goldmark.New(goldmark.WithExtensions(extension.GFM, Markdown{}))
	for _, sample := range []struct{ source, expected string }{
		{"==**bold**==", "<mark><strong>bold</strong></mark>"},
		{"<u>~one~ plus ^two^</u>", "<u><sub>one</sub> plus <sup>two</sup></u>"},
		{"~~strike~~ ~sub~ ~with spaces~ ^^literal^^", "<del>strike</del> <sub>sub</sub> ~with spaces~ ^^literal^^"},
		{"`<u>literal</u>`", "<code>&lt;u&gt;literal&lt;/u&gt;</code>"},
		{"\\==no== \\~no~", "==no== ~no~"},
		{"<u onclick=\"run()\">unsafe</u>", "<!-- raw HTML omitted -->unsafe&lt;/u&gt;"},
		{"<u><img src=\"something\">safe</u>", "<u><!-- raw HTML omitted -->safe</u>"},
		{"^no\nnewlines^", "^no\nnewlines^"},
	} {
		var rendered bytes.Buffer
		if err := markdown.Convert([]byte(sample.source), &rendered); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(rendered.String(), sample.expected) {
			t.Errorf("%q rendered %q, want %q", sample.source, rendered.String(), sample.expected)
		}
	}
}
