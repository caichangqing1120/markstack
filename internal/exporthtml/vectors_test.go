package exporthtml

import (
	"strings"
	"testing"
)

func TestMarkdownExportsOnlyGeneratedStaticSVG(t *testing.T) {
	input := "\xef\xbb\xbfPrice \\$100, $$10 and $$20; inline $x_1$ and `$notMath$`.\r\n\r\n" +
		"$$\r\n\\frac{a}{b}\r\n$$\r\n\r\n" +
		"```mermaid\r\ngraph LR\r\nA[\"<script>alert(1)</script>\"] --> B{结束}\r\n```\r\n\r\n" +
		"```mermaid\npie\n\"A\": 5\n```\n\nUnsupported $\\custom{x}$.\n\n" +
		"<svg onload=alert(1)><script>alert(1)</script></svg>\n\n<markstack-vector data-index=\"1\"></markstack-vector>"
	data, err := RenderMarkdown([]byte(input), nil)
	if err != nil {
		t.Fatal(err)
	}
	output := string(data)
	if strings.Count(output, "<svg ") != 3 || strings.Contains(output, "<script>") || strings.Contains(output, "<markstack-vector") || strings.Contains(output, "<svg onload") {
		t.Fatalf("unsafe or missing vectors: %s", output)
	}
	for _, expected := range []string{"Price $100, $$10 and $$20", "<code>$notMath$</code>", "<polyline", "<polygon", "&lt;script&gt;", "<pre><code>pie", "不支持的公式"} {
		if !strings.Contains(output, expected) {
			t.Errorf("missing %q in %s", expected, output)
		}
	}
}
