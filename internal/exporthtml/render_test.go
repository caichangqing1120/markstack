package exporthtml

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

func smallPNG(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	if err := png.Encode(&output, image.NewNRGBA(image.Rect(0, 0, 2, 1))); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestRenderMarkdownKeepsReadableGFMStructure(t *testing.T) {
	source := []byte("# 中文标题\n\n正文有 **加粗** 和 [安全链接](https://example.org/a?x=1&y=2)。\n\n- 列表\n- [x] 完成\n\n| 列 | 值 |\n| --- | --- |\n| 一 | 二 |\n\n```go\nfmt.Println(\"你好\")\n```\n")
	result, err := RenderMarkdown(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"<h1>中文标题</h1>", "<strong>加粗</strong>", "<ul>", "☑", "<table>", "<pre><code", "fmt.Println", "@media print", `href="https://example.org/a?x=1&amp;y=2"`} {
		if !bytes.Contains(result, []byte(expected)) {
			t.Errorf("export omitted %q: %s", expected, result)
		}
	}
	if !bytes.Contains(result, []byte(`default-src 'none'`)) {
		t.Fatal("export omitted restrictive CSP")
	}
	if bytes.Contains(result, []byte("<input")) {
		t.Fatal("task checkbox became an active form control")
	}
}

func TestM14MarkdownSyntaxAndRawHTML(t *testing.T) {
	source := []byte("\ufeff---\r\ntitle: Hidden metadata\r\ntags: [one, two]\r\n---\r\n# Body\r\n\r\n" +
		"> [!nOtE] Title <script>\n> **Strong** and ==highlight==.\n>\n> Second paragraph.\n\n" +
		"> [!TIP]\n> Tip body\n\n> [!IMPORTANT]\n> Important body\n\n> [!WARNING]\n> Warning body\n\n> [!CAUTION]\n> Caution body\n\n" +
		"<u>Under</u> ~sub~ ^sup^ <sub>also sub</sub> <sup>also sup</sup> ~~strike~~ ~not sub~ `==literal==`.\n\n" +
		`<div class="callout callout-note" onclick="active()"><img src="raw.png"></div>`)
	output, err := RenderMarkdown(source, func(path string) ([]byte, error) {
		t.Fatalf("raw image was loaded: %s", path)
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`<h1>Body</h1>`, `<div class="callout callout-note">`, `Title &lt;script&gt;`, `<strong>Strong</strong>`,
		`<p>Second paragraph.</p>`, `<div class="callout callout-tip">`, `<div class="callout callout-important">`,
		`<div class="callout callout-warning">`, `<div class="callout callout-caution">`,
		`<mark>highlight</mark>`, `<u>Under</u>`, `<sub>sub</sub>`, `<sup>sup</sup>`,
		`<sub>also sub</sub>`, `<sup>also sup</sup>`, `<del>strike</del>`, `~not sub~`, `<code>==literal==</code>`,
	} {
		if !bytes.Contains(output, []byte(expected)) {
			t.Errorf("missing %q in %s", expected, output)
		}
	}
	for _, forbidden := range []string{`Hidden metadata`, `<script>`, `<img src="raw.png">`, `<del>not sub</del>`} {
		if bytes.Contains(output, []byte(forbidden)) {
			t.Errorf("unexpected %q", forbidden)
		}
	}
	assertInertDOM(t, output)
}

func TestRenderMarkdownEmbedsJPEGAndGIFButNotSVG(t *testing.T) {
	bitmap := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	var jpegData, gifData bytes.Buffer
	if err := jpeg.Encode(&jpegData, bitmap, nil); err != nil {
		t.Fatal(err)
	}
	if err := gif.Encode(&gifData, bitmap, nil); err != nil {
		t.Fatal(err)
	}
	source := []byte("![photo](assets/p.jpg)\n\n![animation](assets/a.gif)\n\n![vector](assets/x.svg)\n")
	result, err := RenderMarkdown(source, func(relative string) ([]byte, error) {
		switch relative {
		case "assets/p.jpg":
			return jpegData.Bytes(), nil
		case "assets/a.gif":
			return gifData.Bytes(), nil
		default:
			return []byte(`<svg onload="alert(1)"></svg>`), nil
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(result, []byte("data:image/jpeg;base64,")) || !bytes.Contains(result, []byte("data:image/gif;base64,")) || !bytes.Contains(result, []byte("图片不可用: vector")) || bytes.Contains(result, []byte("data:image/svg")) {
		t.Fatalf("raster allowlist output=%s", result)
	}
	assertInertDOM(t, result)
}

func TestRenderMarkdownNeverActivatesRawHTMLOrRemoteMedia(t *testing.T) {
	source := []byte("<script>alert('bad')</script>\n\n<img src=x onerror=alert(1)>\n\n[unsafe](javascript:alert(1))\n\n![remote](https://example.org/p.png)\n\n![svg](data:image/svg+xml;base64,PHN2Zz4=)\n")
	loads := 0
	result, err := RenderMarkdown(source, func(string) ([]byte, error) {
		loads++
		return nil, errors.New("must not load unsafe media")
	})
	if err != nil {
		t.Fatal(err)
	}
	output := string(result)
	for _, forbidden := range []string{"<script", "<iframe", `href="javascript:`, `src="https://`, `src="data:image/svg`} {
		if strings.Contains(output, forbidden) {
			t.Errorf("unsafe HTML escaped sanitizer: %q", forbidden)
		}
	}
	if loads != 0 || !strings.Contains(output, "原始 HTML") || !strings.Contains(output, "图片不可用") || !strings.Contains(output, "&lt;script&gt;") {
		t.Fatalf("unsafe fallback=%q loads=%d", output, loads)
	}
	assertInertDOM(t, result)
}

func TestRenderMarkdownEmbedsOnlyBoundedLocalRasterImages(t *testing.T) {
	imageBytes := smallPNG(t)
	loads := 0
	source := []byte("![first](assets/p.png)\n\n![second](assets/p.png)\n\n![missing](assets/missing.png)\n")
	result, err := RenderMarkdown(source, func(relative string) ([]byte, error) {
		loads++
		if relative == "assets/p.png" {
			return imageBytes, nil
		}
		return nil, errors.New("missing")
	})
	if err != nil {
		t.Fatal(err)
	}
	if loads != 2 || bytes.Count(result, []byte("data:image/png;base64,")) != 2 || !bytes.Contains(result, []byte("图片不可用: missing")) || bytes.Contains(result, []byte(`src="assets/`)) {
		t.Fatalf("image export loads=%d output=%s", loads, result)
	}
	assertInertDOM(t, result)
	oversized := bytes.Repeat([]byte{'x'}, 8<<20+1)
	result, err = RenderMarkdown([]byte("![large](assets/large.png)"), func(string) ([]byte, error) { return oversized, nil })
	if err != nil || !bytes.Contains(result, []byte("图片不可用: large")) || bytes.Contains(result, []byte("data:image")) {
		t.Fatalf("oversized image output=%d bytes, %v", len(result), err)
	}
}

func TestRenderMarkdownRejectsInvalidEncodingAndOutputBudget(t *testing.T) {
	if _, err := RenderMarkdown([]byte{0xff}, nil); err == nil {
		t.Fatal("invalid UTF-8 exported")
	}
	if _, err := renderMarkdown([]byte("# heading"), nil, 32); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("small output budget=%v", err)
	}
}

func assertInertDOM(t *testing.T, data []byte) {
	t.Helper()
	root, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode {
			if node.Data == "script" || node.Data == "iframe" || node.Data == "object" || node.Data == "form" {
				t.Errorf("active element %q", node.Data)
			}
			for _, attribute := range node.Attr {
				if strings.HasPrefix(attribute.Key, "on") || attribute.Key == "src" && !strings.HasPrefix(attribute.Val, "data:image/png;") && !strings.HasPrefix(attribute.Val, "data:image/jpeg;") && !strings.HasPrefix(attribute.Val, "data:image/gif;") {
					t.Errorf("unsafe attribute %s=%q", attribute.Key, attribute.Val)
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(root)
}

// TestRenderMarkdownLongMixedControlledSampleKeepsEveryRunAndAllowlist is the
// D17 HTML sample: long Chinese mixed content, a six column wide table with a
// local image inside a cell, both list kinds, a quote, a code fence, unknown
// nodes and malicious attributes. It asserts the parsed DOM structure, every
// expected text run, the single bounded local image and the inert fallbacks.
func TestRenderMarkdownLongMixedControlledSampleKeepsEveryRunAndAllowlist(t *testing.T) {
	imageBytes := smallPNG(t)
	loads := 0
	load := func(relative string) ([]byte, error) {
		loads++
		if relative != "assets/cell.png" {
			return nil, errors.New("unexpected asset: " + relative)
		}
		return imageBytes, nil
	}
	var source strings.Builder
	source.WriteString("# 长中文标题\n\n")
	for index := 1; index <= 12; index++ {
		fmt.Fprintf(&source, "第%02d段中文混排：**加粗**、*斜体*、`行内代码` 与 [安全链接](https://example.org/%d) 以及普通中文文字。\n\n", index, index)
	}
	source.WriteString("- 无序列表甲\n- 无序列表乙\n\n1. 有序列表甲\n2. 有序列表乙\n\n> 引用中文段落\n\n")
	source.WriteString("| 第一列 | 第二列 | 第三列 | 第四列 | 第五列 | 第六列 |\n| --- | --- | --- | --- | --- | --- |\n")
	source.WriteString("| ![单元格图](assets/cell.png) | 宽表右一 | 宽表右二 | 宽表右三 | 宽表右四 | 宽表右五 |\n")
	source.WriteString("| 宽表甲 | 宽表乙 | 宽表丙 | 宽表丁 | 宽表戊 | 宽表己 |\n\n")
	source.WriteString("```go\nfmt.Println(\"代码中文\")\n```\n\n")
	source.WriteString("<script>alert('bad')</script>\n\n<x-widget onclick=\"alert(1)\">未知节点</x-widget>\n\n")
	source.WriteString("<span onmouseover=\"alert(2)\">事件</span>\n\n[危险链接](javascript:alert(3))\n\n![远程图](https://example.org/p.png)\n\n![矢量图](data:image/svg+xml;base64,PHN2Zz4=)\n")
	result, err := RenderMarkdown([]byte(source.String()), load)
	if err != nil {
		t.Fatal(err)
	}
	if loads != 1 {
		t.Fatalf("local image loads=%d want one cached load", loads)
	}
	assertInertDOM(t, result)
	root, err := html.Parse(bytes.NewReader(result))
	if err != nil {
		t.Fatal(err)
	}
	findAll := func(kind atom.Atom) []*html.Node {
		var nodes []*html.Node
		var visit func(*html.Node)
		visit = func(node *html.Node) {
			if node.Type == html.ElementNode && node.DataAtom == kind {
				nodes = append(nodes, node)
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				visit(child)
			}
		}
		visit(root)
		return nodes
	}
	var textOf func(*html.Node) string
	textOf = func(node *html.Node) string {
		var output strings.Builder
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.TextNode {
				output.WriteString(child.Data)
			}
			output.WriteString(textOf(child))
		}
		return output.String()
	}
	if headings := findAll(atom.H1); len(headings) != 1 || strings.TrimSpace(textOf(headings[0])) != "长中文标题" {
		t.Fatalf("h1 nodes=%d", len(headings))
	}
	if lists := findAll(atom.Ul); len(lists) != 1 {
		t.Fatalf("unordered lists=%d", len(lists))
	}
	if lists := findAll(atom.Ol); len(lists) != 1 {
		t.Fatalf("ordered lists=%d", len(lists))
	}
	tables := findAll(atom.Table)
	if len(tables) != 1 {
		t.Fatalf("tables=%d", len(tables))
	}
	headers := findAll(atom.Th)
	cells := findAll(atom.Td)
	if len(headers) != 6 || len(cells) != 12 {
		t.Fatalf("wide table headers=%d cells=%d want 6/12", len(headers), len(cells))
	}
	tabledImages := 0
	for _, cell := range cells {
		for _, image := range findAllWithin(cell, atom.Img) {
			tabledImages++
			if src := attribute(image, "src"); !strings.HasPrefix(src, "data:image/png;base64,") {
				t.Errorf("table image src=%q", src)
			}
		}
	}
	if tabledImages != 1 {
		t.Fatalf("images inside table cells=%d want 1", tabledImages)
	}
	if images := findAll(atom.Img); len(images) != 1 {
		t.Errorf("export images=%d want only the bounded local raster", len(images))
	}
	if quotes := findAll(atom.Blockquote); len(quotes) != 1 || !strings.Contains(textOf(quotes[0]), "引用中文段落") {
		t.Errorf("blockquotes=%d", len(quotes))
	}
	pres := findAll(atom.Pre)
	if len(pres) != 2 {
		t.Fatalf("pre blocks=%d want code fence and inert appendix", len(pres))
	}
	codeSurface := textOf(pres[0])
	if !strings.Contains(codeSurface, `fmt.Println("代码中文")`) {
		t.Errorf("code fence text=%q", codeSurface)
	}
	appendix := textOf(pres[1])
	for _, expected := range []string{"<script>alert('bad')</script>", "<x-widget onclick=\"alert(1)\">", "onmouseover"} {
		if !strings.Contains(appendix, expected) {
			t.Errorf("inert appendix omitted %q: %q", expected, appendix)
		}
	}
	allText := strings.Join([]string{textOf(findAll(atom.Body)[0]), appendix}, "\n")
	for _, expected := range []string{
		"第01段中文混排", "第12段中文混排", "加粗", "斜体", "行内代码", "安全链接",
		"无序列表甲", "无序列表乙", "有序列表甲", "有序列表乙", "引用中文段落",
		"第一列", "第二列", "第三列", "第四列", "第五列", "第六列",
		"宽表右一", "宽表右二", "宽表右三", "宽表右四", "宽表右五", "宽表甲", "宽表乙", "宽表丙", "宽表丁", "宽表戊", "宽表己",
		"代码中文", "未知节点", "事件", "危险链接", "图片不可用: 远程图", "图片不可用: 矢量图",
	} {
		if !strings.Contains(allText, expected) {
			t.Errorf("HTML export lost %q", expected)
		}
	}
	for _, forbidden := range []string{`href="javascript:`, `src="https://`, `src="data:image/svg`, "<script", "<iframe", "<form", "<object", `onerror="`, `onclick="`, `onmouseover="`} {
		if strings.Contains(string(result), forbidden) {
			t.Errorf("active markup %q survived sanitizer", forbidden)
		}
	}
}

func findAllWithin(node *html.Node, kind atom.Atom) []*html.Node {
	var nodes []*html.Node
	var visit func(*html.Node)
	visit = func(current *html.Node) {
		if current.Type == html.ElementNode && current.DataAtom == kind {
			nodes = append(nodes, current)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(node)
	return nodes
}
