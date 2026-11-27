package exportpdf

import (
	"bytes"
	"strings"
	"testing"
)

func TestPDFExportsTechnicalTextAndVectorStrokes(t *testing.T) {
	source := "Before $x_2$ after.\n\n$$\n\\frac{m}{n}+\\sqrt{y}\n$$\n\n" +
		"```mermaid\nsequenceDiagram\nparticipant A as 客户端\nparticipant B as 服务端\nA->>B: 请求\nB-->>A: 完成\n```\n\n" +
		"| Term | Value |\n| --- | --- |\n| math | $z^3$ |\n\n```mermaid\npie\nA: 5\n```\n\n$\\unknown{x}$"
	data, err := RenderMarkdown([]byte(source), nil, testFont(t))
	if err != nil || !bytes.HasPrefix(data, []byte("%PDF-")) || len(data) < 1000 {
		t.Fatalf("PDF size=%d err=%v", len(data), err)
	}
	inspected := inspectPDF(t, data)
	var runs []string
	var strokes int
	for index, page := range inspected.pages {
		runs = append(runs, inspected.pageTexts[index]...)
		strokes += bytes.Count(page, []byte(" l S"))
		if len(inspected.images[index]) != 0 {
			t.Fatal("technical vectors were rasterized")
		}
	}
	if strokes < 4 || len(inspected.fontFile) == 0 || !inspected.toUnicode {
		t.Fatalf("expected graphics strokes and an embedded Unicode font, strokes=%d", strokes)
	}
	text := strings.Join(runs, "\n")
	for _, expected := range []string{"Before", "after.", "客户端", "服务端", "请求", "完成", "pie", `\unknown{x}`, "不支持的公式", "z"} {
		if !strings.Contains(text, expected) {
			t.Errorf("missing %q in %q", expected, text)
		}
	}
}
