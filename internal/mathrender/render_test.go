package mathrender

import (
	"reflect"
	"strings"
	"testing"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
	"markstack/internal/vectorrender"
)

func TestMathMarkdownBoundaries(t *testing.T) {
	source := []byte("Price \\$100, $10 and $20, or $$100 and $$200.\r\n" +
		"`$code$` [URL](https://example.test/$path$), then $x_2$ and $\\frac{a}{b}$.\r\n" +
		"\r\n$$\r\nUnclosed before code.\r\n" +
		"\r\n```text\r\n$$\r\n$x$\r\n$$\r\n```\r\n\r\n    $indented$\r\n\r\n" +
		"$$\r\n\\sqrt{x}\r\n\r\n+ 1\r\n$$\r\nFollowing $\\custom{x}$.\r\n\r\n$$a_1$$\r\n\r\n$$\r\nUnclosed")
	markdown := goldmark.New(goldmark.WithExtensions(extension.GFM, Markdown{}))
	root := markdown.Parser().Parse(text.NewReader(source))
	var inline, block []string
	_ = ast.Walk(root, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			switch value := node.(type) {
			case *Inline:
				inline = append(inline, value.Body(source))
			case *Display:
				block = append(block, value.Body(source))
			}
		}
		return ast.WalkContinue, nil
	})
	if !reflect.DeepEqual(inline, []string{"x_2", `\frac{a}{b}`, `\custom{x}`}) || !reflect.DeepEqual(block, []string{"\\sqrt{x}\r\n\r\n+ 1\r\n", "a_1"}) {
		t.Fatalf("inline=%q display=%q", inline, block)
	}
}

func TestStructuredMathLayout(t *testing.T) {
	value := `\sum_{i=1}^{n} \frac{\sqrt{x_i}}{\alpha+\Omega} \ge \int_0^\infty \prod_{k=1}^m \left(\begin{matrix}a&b\\c&d\end{matrix}\right)`
	scene, err := Layout(value, 18, nil)
	if err != nil || scene.Width < 100 || scene.Height < 36 {
		t.Fatalf("layout: %+v %v", scene, err)
	}
	var lines int
	var combined strings.Builder
	for _, op := range scene.Ops {
		combined.WriteString(op.Text)
		if op.Kind == vectorrender.Line {
			lines++
		}
	}
	if lines != 2 || !strings.Contains(combined.String(), "α+Ω") || !strings.Contains(combined.String(), "abcd") {
		t.Fatalf("missing structure: lines=%d glyphs=%s", lines, combined.String())
	}
	if svg := scene.SVG(`<script>alert("math")</script>`, true); !strings.Contains(svg, "<polyline") || strings.Contains(svg, "<script>") || !strings.Contains(svg, "vertical-align:") {
		t.Fatalf("unexpected SVG: %s", svg)
	}
}

func TestMalformedMathFailsClosed(t *testing.T) {
	for _, source := range []string{`\def\x{y}`, `\frac{a}`, `x^2^3`, `\sqrt{`, `\begin{matrix}a&b\\c\end{matrix}`, strings.Repeat("{", 60), strings.Repeat("x", 4097)} {
		if scene, err := Layout(source, 16, nil); err == nil || len(scene.Ops) != 0 {
			t.Errorf("expected safe fallback for %.64q", source)
		}
	}
}
