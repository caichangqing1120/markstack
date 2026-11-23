package exportpdf

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"unicode"
)

func testFont(t *testing.T) []byte {
	t.Helper()
	path := os.Getenv("MARKSTACK_PDF_FONT")
	if path == "" {
		font, err := BundledFont()
		if err != nil {
			t.Fatal(err)
		}
		return font
	}
	font, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if os.Getenv("MARKSTACK_PDF_ARTIFACT_DIR") != "" {
			t.Fatalf("PDF acceptance requires a Unicode font: %s", path)
		}
		t.Skipf("Unicode PDF font unavailable: %s", path)
	}
	if err != nil {
		t.Fatal(err)
	}
	return font
}

func pdfText(t *testing.T, data []byte) string {
	t.Helper()
	var command *exec.Cmd
	if _, err := exec.LookPath("pdftotext"); err == nil {
		command = exec.Command("pdftotext", "-layout", "-", "-")
	} else if runtime.GOOS == "darwin" {
		if _, err := exec.LookPath("swift"); err != nil {
			t.Skip("PDF text extractor unavailable")
		}
		command = exec.Command("swift", "-e", `import Foundation; import PDFKit
guard let document = PDFDocument(data: FileHandle.standardInput.readDataToEndOfFile()) else { exit(2) }
for index in 0..<document.pageCount {
    if index > 0 { print("\u{000C}", terminator: "") }
    print(document.page(at: index)?.string ?? "")
}`)
	} else {
		t.Skip("PDF text extractor unavailable")
	}
	command.Stdin = bytes.NewReader(data)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("PDF text extraction: %v: %s", err, output)
	}
	return string(output)
}

func savePDFArtifact(t *testing.T, name string, data []byte) {
	t.Helper()
	if directory := os.Getenv("MARKSTACK_PDF_ARTIFACT_DIR"); directory != "" {
		if err := os.MkdirAll(directory, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRenderRejectsUnsupportedSupplementaryText(t *testing.T) {
	font, err := BundledFont()
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		name, source, codepoint string
		render                  func([]byte, func(string) ([]byte, error), []byte) ([]byte, error)
	}{
		{"markdown-inline", "# Heading\n\nBefore **\U0001f642** after.\n", "U+1F642", RenderMarkdown},
		{"first-supplementary-rune", "First **\U00010000**", "U+10000", RenderMarkdown},
		{"markdown-code", "```text\n\U0002000b\n```\n", "U+2000B", RenderMarkdown},
		{"rich-entity", "<p>Before &#x1F642; after.</p>", "U+1F642", RenderRichHTML},
		{"rich-table", "<table><tr><td>\U0002000b</td><td>tail</td></tr></table>", "U+2000B", RenderRichHTML},
		{"rich-literal", "<p>Lead</p><script>\U0001f642</script>", "U+1F642", RenderRichHTML},
		{"missing-image-label", "<p><img src='missing.png' alt='&#x1F642;'></p>", "U+1F642", RenderRichHTML},
		{"heading", "# Heading \U0001f642", "U+1F642", RenderMarkdown},
		{"list", "- Item \U0001f642", "U+1F642", RenderMarkdown},
		{"quote", "> Quote \U0001f642", "U+1F642", RenderMarkdown},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			data, err := fixture.render([]byte(fixture.source), nil, font)
			if len(data) != 0 || err == nil || !strings.Contains(err.Error(), fixture.codepoint) {
				t.Fatalf("expected refusal with %s and no PDF bytes, got %d bytes and %v", fixture.codepoint, len(data), err)
			}
		})
	}
}

func TestRenderAllowsSupplementaryTextOutsidePDFLayout(t *testing.T) {
	font, err := BundledFont()
	if err != nil {
		t.Fatal(err)
	}
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	loadImage := func(path string) ([]byte, error) {
		if path == "local.png" {
			return pngData.Bytes(), nil
		}
		return nil, os.ErrNotExist
	}
	for _, fixture := range []struct{ name, source string }{
		{"link-destination", "<p><a href='https://example.test/\U0001f642'>safe label</a></p>"},
		{"root-text", "<p>safe label</p>\U0001f642"},
		{"standalone-inline", "<strong>\U0001f642</strong><p>safe label</p>"},
		{"standalone-unavailable-image", "<img src='missing.png' alt='\U0001f642'><p>safe label</p>"},
		{"loaded-image-alternative", "<p>safe label <img src='local.png' alt='\U0001f642'></p>"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			data, err := RenderRichHTML([]byte(fixture.source), loadImage, font)
			if err != nil || !bytes.HasPrefix(data, []byte("%PDF-")) {
				t.Fatalf("text outside PDF layout prevented export: %v", err)
			}
			if output := pdfText(t, data); !strings.Contains(output, "safe label") {
				t.Fatalf("PDF omitted supported text: %q", output)
			}
		})
	}
}

func TestRenderMarkdownChineseTextFlowsAcrossPages(t *testing.T) {
	source := "# 墨栈导出\n\n第一段中文可选中。\n\n" + strings.Repeat("后续段落应该完整分页。\n\n", 100) + "最后一段。\n"
	data, err := RenderMarkdown([]byte(source), nil, testFont(t))
	if err != nil || !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatalf("PDF header=%q, error=%v", data[:min(len(data), 12)], err)
	}
	savePDFArtifact(t, "Markdown.pdf", data)
	text := pdfText(t, data)
	for _, value := range []string{"墨栈导出", "第一段中文可选中。", "最后一段。"} {
		if !strings.Contains(text, value) {
			t.Fatalf("PDF omitted %q from selectable text: %q", value, text[:min(len(text), 200)])
		}
	}
	if !strings.Contains(text, "\f") {
		t.Fatal("long PDF did not contain a page boundary")
	}
}

func TestRenderRichHTMLKeepsUnknownContentInert(t *testing.T) {
	source := `<p>正文 &amp; 中文</p><x-unknown>未知</x-unknown><script>alert(1)</script><p><a href="javascript:alert(2)">危险链接</a></p>`
	data, err := RenderRichHTML([]byte(source), nil, testFont(t))
	if err != nil {
		t.Fatal(err)
	}
	savePDFArtifact(t, "Rich.pdf", data)
	text := pdfText(t, data)
	for _, value := range []string{"正文 & 中文", "未知", "危险链接", "<script>alert(1)</script>"} {
		if !strings.Contains(text, value) {
			t.Fatalf("PDF omitted inert content %q: %q", value, text)
		}
	}
	if bytes.Contains(data, []byte("/JavaScript")) || bytes.Contains(data, []byte("/URI (javascript:")) {
		t.Fatal("PDF retained an active script or dangerous link")
	}
}

func TestRenderMarkdownTableAndLocalImage(t *testing.T) {
	var imageBytes bytes.Buffer
	bitmap := image.NewRGBA(image.Rect(0, 0, 320, 180))
	for row := range 180 {
		for column := range 320 {
			if column < 160 {
				bitmap.Set(column, row, color.RGBA{R: 215, G: 66, B: 55, A: 255})
			} else {
				bitmap.Set(column, row, color.RGBA{R: 28, G: 141, B: 168, A: 255})
			}
		}
	}
	if err := png.Encode(&imageBytes, bitmap); err != nil {
		t.Fatal(err)
	}
	loads := 0
	load := func(path string) ([]byte, error) {
		loads++
		if path != "assets/p.png" {
			t.Fatalf("unexpected asset path: %q", path)
		}
		return imageBytes.Bytes(), nil
	}
	source := "| 列 | 值 |\n| --- | --- |\n| A | 中文 |\n\n![本地图](assets/p.png)\n\n![远程](https://example.org/p.png)\n"
	data, err := RenderMarkdown([]byte(source), load, testFont(t))
	if err != nil {
		t.Fatal(err)
	}
	if loads != 1 || !bytes.Contains(data, []byte("/Subtype /Image")) {
		t.Fatalf("local image missing or remote fetched: loads=%d image=%v", loads, bytes.Contains(data, []byte("/Subtype /Image")))
	}
	savePDFArtifact(t, "Table-Image.pdf", data)
	text := pdfText(t, data)
	for _, value := range []string{"列", "值", "中文", "图片不可用"} {
		if !strings.Contains(text, value) {
			t.Fatalf("PDF omitted %q: %q", value, text)
		}
	}
}

func TestRenderRichHTMLTableCellRetainsLocalImage(t *testing.T) {
	var imageBytes bytes.Buffer
	bitmap := image.NewRGBA(image.Rect(0, 0, 320, 180))
	for row := range 180 {
		for column := range 320 {
			bitmap.Set(column, row, color.RGBA{R: 30, G: 156, B: 127, A: 255})
		}
	}
	if err := png.Encode(&imageBytes, bitmap); err != nil {
		t.Fatal(err)
	}
	loads := 0
	load := func(path string) ([]byte, error) {
		loads++
		if path != "assets/cell.png" {
			t.Fatalf("unexpected table asset: %q", path)
		}
		return imageBytes.Bytes(), nil
	}
	source := `<table><tbody><tr><th>列</th><td>说明 <img src="assets/cell.png" alt="单元格图"></td></tr></tbody></table>`
	data, err := RenderRichHTML([]byte(source), load, testFont(t))
	if err != nil {
		t.Fatal(err)
	}
	if loads != 1 || !bytes.Contains(data, []byte("/Subtype /Image")) {
		t.Fatalf("table image omitted: loads=%d embedded=%v", loads, bytes.Contains(data, []byte("/Subtype /Image")))
	}
	if text := pdfText(t, data); !strings.Contains(text, "说明") || !strings.Contains(text, "列") {
		t.Fatalf("table text lost: %q", text)
	}
	savePDFArtifact(t, "Table-Cell-Image.pdf", data)
}

func TestRenderLongMixedParagraphUsesAvailableWidth(t *testing.T) {
	lead := strings.Repeat("甲", 30)
	tail := strings.Repeat("丙", 25)
	font := testFont(t)
	plain, err := RenderMarkdown([]byte(lead+" 乙 "+tail), nil, font)
	if err != nil {
		t.Fatal(err)
	}
	mixed, err := RenderMarkdown([]byte(lead+" **乙** "+tail), nil, font)
	if err != nil {
		t.Fatal(err)
	}
	savePDFArtifact(t, "Mixed-Long.pdf", mixed)
	firstLine := func(data []byte) string {
		return strings.SplitN(strings.TrimSpace(pdfText(t, data)), "\n", 2)[0]
	}
	plainLine, mixedLine := firstLine(plain), firstLine(mixed)
	if len([]rune(mixedLine))+3 < len([]rune(plainLine)) {
		t.Fatalf("mixed paragraph wraps early: plain first line=%q, mixed first line=%q", plainLine, mixedLine)
	}
	text := pdfText(t, mixed)
	if strings.Count(text, "甲") != 30 || strings.Count(text, "乙") != 1 || strings.Count(text, "丙") != 25 {
		t.Fatalf("mixed paragraph lost source glyphs: %q", text)
	}
}

func TestRenderMarkdownOrderedListKeepsNumberAndText(t *testing.T) {
	source := "9. 第九项\n10. 第十项\n"
	data, err := RenderMarkdown([]byte(source), nil, testFont(t))
	if err != nil {
		t.Fatal(err)
	}
	text := pdfText(t, data)
	if first, second := strings.Index(text, "9. 第九项"), strings.Index(text, "10. 第十项"); first < 0 || second <= first {
		t.Fatalf("ordered list number/order lost: %q", text)
	}
}

func TestRenderMarkdownQuoteDoesNotExposeBorderAsText(t *testing.T) {
	data, err := RenderMarkdown([]byte("> 引用中文\n"), nil, testFont(t))
	if err != nil {
		t.Fatal(err)
	}
	text := pdfText(t, data)
	if !strings.Contains(text, "引用中文") || strings.Contains(text, "│") {
		t.Fatalf("blockquote border became text: %q", text)
	}
}

func TestRenderWelcomePDFArtifact(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "examples", "Welcome.md"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := RenderMarkdown(source, nil, testFont(t))
	if err != nil {
		t.Fatal(err)
	}
	savePDFArtifact(t, "Welcome-renderer.pdf", data)
	text := pdfText(t, data)
	if !strings.Contains(text, "墨栈") || !strings.Contains(text, "你的文字留在本机") || !strings.Contains(text, "Markdown 文档") || strings.Contains(text, "│") {
		t.Fatalf("Welcome PDF lost content or exposed quote border: %q", text)
	}
}

func TestRenderMarkdownInlineStylesAndSafeLink(t *testing.T) {
	source := "常规 **加粗** *斜体* `代码` [链接](https://example.org) [危险](javascript:alert(1))\n"
	data, err := RenderMarkdown([]byte(source), nil, testFont(t))
	if err != nil {
		t.Fatal(err)
	}
	text := pdfText(t, data)
	for _, value := range []string{"常规", "加粗", "斜体", "代码", "链接", "危险"} {
		if !strings.Contains(text, value) {
			t.Fatalf("styled PDF lost %q: %q", value, text)
		}
	}
	if !bytes.Contains(data, []byte("/URI (https://example.org)")) || bytes.Contains(data, []byte("/URI (javascript:")) {
		t.Fatal("PDF lost a safe link or activated an unsafe URL")
	}
}

func TestRenderPDFRejectsInvalidSourceAndMissingFont(t *testing.T) {
	if _, err := RenderMarkdown([]byte{0xff}, nil, testFont(t)); err == nil {
		t.Fatal("invalid UTF-8 exported")
	}
	if _, err := RenderMarkdown([]byte("# Title"), nil, nil); err == nil {
		t.Fatal("missing Unicode font exported")
	}
}

func TestRenderRichHTMLRejectsTableRowTooTallForPage(t *testing.T) {
	source := "<table><tr><td>" + strings.Repeat("超长内容", 1800) + "</td></tr></table>"
	data, err := RenderRichHTML([]byte(source), nil, testFont(t))
	if !errors.Is(err, ErrLayout) || data != nil {
		t.Fatalf("oversized table row produced PDF=%d bytes, error=%v", len(data), err)
	}
}

// The PDF inspector parses the bytes the renderer produced instead of calling
// an external tool: poppler is not part of the test environment, every page has
// to be checked, and text runs have to be proven present in the real content
// streams. go-pdf/fpdf writes UTF-16BE literal strings for the embedded UTF-8
// font, so the page text decodes without font glyph tables.

type pdfObject struct {
	dictionary string
	stream     []byte
	decoded    []byte
}

type pdfImagePlacement struct {
	name       string
	x, y, w, h float64
}

type inspectedPDF struct {
	mediaWidth, mediaHeight float64
	objects                 map[int]pdfObject
	pages                   [][]byte
	pageTexts               [][]string
	images                  [][]pdfImagePlacement
	fontFile                []byte
	toUnicode               bool
	typeZero                bool
}

var (
	pdfObjectStart = regexp.MustCompile(`(?s)(\d+)\s+0\s+obj\b`)
	pdfPageType    = regexp.MustCompile(`/Type\s*/Page\b`)
	pdfPagesType   = regexp.MustCompile(`/Type\s*/Pages\b`)
	pdfCount       = regexp.MustCompile(`/Count\s+(\d+)`)
	pdfMediaBox    = regexp.MustCompile(`/MediaBox\s*\[\s*([\d.]+)\s+([\d.]+)\s+([\d.]+)\s+([\d.]+)\s*\]`)
	pdfContentsRef = regexp.MustCompile(`/Contents\s+(\d+)\s+0\s+R`)
	pdfContentsArr = regexp.MustCompile(`/Contents\s*\[([^\]]*)\]`)
	pdfKids        = regexp.MustCompile(`/Kids\s*\[([^\]]*)\]`)
	pdfFontFile2   = regexp.MustCompile(`/FontFile2\s+(\d+)\s+0\s+R`)
	pdfImagePlace  = regexp.MustCompile(`q\s+([\d.]+)\s+0\s+0\s+([\d.]+)\s+([\d.]+)\s+([\d.]+)\s+cm\s+/(\S+)\s+Do\s+Q`)
	pdfXObjectDict = regexp.MustCompile(`/XObject\s*<<([^>]*)>>`)
	pdfXObjectRef  = regexp.MustCompile(`/(\S+)\s+\d+\s+0\s+R\b`)
)

func inspectPDF(t *testing.T, data []byte) inspectedPDF {
	t.Helper()
	if !bytes.HasPrefix(data, []byte("%PDF-")) || !bytes.HasSuffix(bytes.TrimSpace(data), []byte("%%EOF")) {
		t.Fatal("not a complete PDF")
	}
	objects := map[int]pdfObject{}
	var order []int
	starts := pdfObjectStart.FindAllSubmatchIndex(data, -1)
	for index, match := range starts {
		number := 0
		fmt.Sscanf(string(data[match[2]:match[3]]), "%d", &number)
		start := match[1]
		end := len(data)
		if index+1 < len(starts) {
			end = starts[index+1][0]
		}
		chunk := data[start:end]
		dictionary := string(chunk)
		object := pdfObject{dictionary: dictionary}
		if marker := bytes.Index(chunk, []byte("stream\r\n")); marker >= 0 {
			object.dictionary = string(chunk[:marker])
			rawStart := marker + len("stream\r\n")
			if raw := chunk[rawStart:]; bytes.Contains(raw, []byte("endstream")) {
				raw = raw[:bytes.LastIndex(raw, []byte("endstream"))]
				raw = bytes.TrimSuffix(raw, []byte("\r\n"))
				object.stream = raw
			}
		} else if marker := bytes.Index(chunk, []byte("stream\n")); marker >= 0 {
			object.dictionary = string(chunk[:marker])
			rawStart := marker + len("stream\n")
			if raw := chunk[rawStart:]; bytes.Contains(raw, []byte("endstream")) {
				raw = raw[:bytes.LastIndex(raw, []byte("endstream"))]
				raw = bytes.TrimSuffix(raw, []byte("\n"))
				object.stream = raw
			}
		}
		if bytes.Contains([]byte(object.dictionary), []byte("/FlateDecode")) && len(object.stream) > 0 {
			reader, err := zlib.NewReader(bytes.NewReader(object.stream))
			if err == nil {
				object.decoded, _ = io.ReadAll(reader)
				reader.Close()
				if object.decoded == nil {
					t.Fatalf("object %d stream did not inflate", number)
				}
			}
		}
		objects[number] = object
		order = append(order, number)
		for _, resources := range pdfXObjectDict.FindAllStringSubmatch(object.dictionary, -1) {
			names := make(map[string]bool)
			for _, reference := range pdfXObjectRef.FindAllStringSubmatch(resources[1], -1) {
				name := reference[1]
				if names[name] {
					t.Errorf("PDF object %d repeats XObject resource /%s", number, name)
				}
				names[name] = true
			}
		}
	}
	if len(objects) == 0 || len(order) != len(starts) {
		t.Fatalf("parsed %d of %d PDF objects", len(objects), len(starts))
	}
	inspected := inspectedPDF{objects: objects, toUnicode: bytes.Contains(data, []byte("/ToUnicode")), typeZero: bytes.Contains(data, []byte("/Subtype /Type0"))}
	// Page order and media box come from the page tree.
	pageNumbers := func() []int {
		for _, number := range order {
			dictionary := objects[number].dictionary
			if pdfPagesType.MatchString(dictionary) {
				if kids := pdfKids.FindStringSubmatch(dictionary); kids != nil {
					var numbers []int
					for _, reference := range regexp.MustCompile(`(\d+)\s+0\s+R`).FindAllStringSubmatch(kids[1], -1) {
						value := 0
						fmt.Sscanf(reference[1], "%d", &value)
						numbers = append(numbers, value)
					}
					return numbers
				}
			}
		}
		return nil
	}()
	if kids := pageNumbers; len(kids) > 0 {
		children := 0
		for _, number := range order {
			if pdfPageType.MatchString(objects[number].dictionary) && !pdfPagesType.MatchString(objects[number].dictionary) {
				children++
			}
		}
		if children != len(kids) {
			t.Fatalf("page tree children=%d page objects=%d", len(kids), children)
		}
		var declared int
		for _, number := range order {
			if pdfPagesType.MatchString(objects[number].dictionary) {
				if count := pdfCount.FindStringSubmatch(objects[number].dictionary); count != nil {
					fmt.Sscanf(count[1], "%d", &declared)
				}
			}
		}
		if declared != len(kids) {
			t.Fatalf("page tree /Count=%d kids=%d", declared, len(kids))
		}
	}
	var pages []int
	for _, number := range order {
		dictionary := objects[number].dictionary
		if pdfPageType.MatchString(dictionary) && !pdfPagesType.MatchString(dictionary) {
			pages = append(pages, number)
		}
	}
	if len(pages) == 0 {
		t.Fatal("PDF has no page object")
	}
	if len(pageNumbers) > 0 && len(pageNumbers) != len(pages) {
		t.Fatalf("page tree lists %d pages but %d page objects exist", len(pageNumbers), len(pages))
	}
	// Use the page-tree order when fpdf wrote one; object order otherwise.
	if len(pageNumbers) > 0 {
		pages = pageNumbers
	}
	if box := pdfMediaBox.FindStringSubmatch(objects[pages[0]].dictionary); box == nil {
		for _, number := range order {
			if match := pdfMediaBox.FindStringSubmatch(objects[number].dictionary); match != nil {
				box = match
				break
			}
		}
		if box == nil {
			t.Fatal("PDF has no MediaBox")
		}
		inspected.mediaWidth, inspected.mediaHeight = parseFloat(box[3]), parseFloat(box[4])
	} else {
		inspected.mediaWidth, inspected.mediaHeight = parseFloat(box[3]), parseFloat(box[4])
	}
	for _, number := range pages {
		dictionary := objects[number].dictionary
		var content []byte
		for _, reference := range pdfContentsRef.FindAllStringSubmatch(dictionary, -1) {
			id := 0
			fmt.Sscanf(reference[1], "%d", &id)
			content = append(content, objects[id].decoded...)
		}
		if array := pdfContentsArr.FindStringSubmatch(dictionary); array != nil {
			for _, reference := range regexp.MustCompile(`(\d+)\s+0\s+R`).FindAllStringSubmatch(array[1], -1) {
				id := 0
				fmt.Sscanf(reference[1], "%d", &id)
				content = append(content, objects[id].decoded...)
			}
		}
		texts := pdfContentTexts(content)
		inspected.pages = append(inspected.pages, content)
		inspected.pageTexts = append(inspected.pageTexts, texts)
		inspected.images = append(inspected.images, pdfContentImages(content))
	}
	if file := pdfFontFile2.FindStringSubmatch(dataToDictionary(data, objects, order)); file != nil {
		id := 0
		fmt.Sscanf(file[1], "%d", &id)
		inspected.fontFile = objects[id].decoded
	}
	return inspected
}

func parseFloat(value string) float64 {
	result := 0.0
	fmt.Sscanf(value, "%f", &result)
	return result
}

// dataToDictionary concatenates the object dictionaries so the font descriptor
// reference can be found without another full walk.
func dataToDictionary(_ []byte, objects map[int]pdfObject, order []int) string {
	var output strings.Builder
	for _, number := range order {
		output.WriteString(objects[number].dictionary)
		output.WriteByte('\n')
	}
	return output.String()
}

// pdfContentTexts extracts the literal strings FPDF draws with Tj, unescaping
// PDF literal-string escapes and decoding the UTF-16BE bytes of the UTF-8 font.
func pdfContentTexts(content []byte) []string {
	var texts []string
	for index := 0; index < len(content); index++ {
		if content[index] != '(' {
			continue
		}
		value, next, ok := pdfLiteral(content, index)
		if !ok {
			continue
		}
		index = next
		if runes, ok := utf16BE(value); ok {
			texts = append(texts, string(runes))
		}
	}
	return texts
}

// pdfLiteral reads one PDF literal string starting at the opening parenthesis.
func pdfLiteral(content []byte, start int) ([]byte, int, bool) {
	var output []byte
	for index := start + 1; index < len(content); index++ {
		switch content[index] {
		case '(':
			depth := 1
			for index++; index < len(content) && depth > 0; index++ {
				switch content[index] {
				case '\\':
					index++
				case '(':
					depth++
				case ')':
					depth--
				}
			}
			return nil, index, false
		case ')':
			return output, index, true
		case '\\':
			index++
			if index >= len(content) {
				return nil, index, false
			}
			escaped := content[index]
			switch escaped {
			case 'n':
				output = append(output, '\n')
			case 'r':
				output = append(output, '\r')
			case 't':
				output = append(output, '\t')
			case 'b':
				output = append(output, '\b')
			case 'f':
				output = append(output, '\f')
			case '(', ')', '\\':
				output = append(output, escaped)
			case '\r':
				if index+1 < len(content) && content[index+1] == '\n' {
					index++
				}
			case '\n':
				// Line continuation contributes nothing.
			default:
				if escaped >= '0' && escaped <= '7' {
					value := int(escaped - '0')
					for count := 0; count < 2 && index+1 < len(content) && content[index+1] >= '0' && content[index+1] <= '7'; count++ {
						index++
						value = value*8 + int(content[index]-'0')
					}
					output = append(output, byte(value))
				} else {
					output = append(output, escaped)
				}
			}
		default:
			output = append(output, content[index])
		}
	}
	return nil, len(content), false
}

func utf16BE(value []byte) ([]rune, bool) {
	if len(value) == 0 || len(value)%2 != 0 {
		return nil, false
	}
	runes := make([]rune, 0, len(value)/2)
	for index := 0; index < len(value); index += 2 {
		runes = append(runes, rune(value[index])<<8|rune(value[index+1]))
	}
	return runes, true
}

func pdfContentImages(content []byte) []pdfImagePlacement {
	var placements []pdfImagePlacement
	for _, match := range pdfImagePlace.FindAllSubmatch(content, -1) {
		placements = append(placements, pdfImagePlacement{
			name: string(match[5]),
			w:    parseFloat(string(match[1])),
			h:    parseFloat(string(match[2])),
			x:    parseFloat(string(match[3])),
			y:    parseFloat(string(match[4])),
		})
	}
	return placements
}

// TestRenderLongChineseMixedDocumentCoversEveryPage builds one controlled long
// Chinese sample with mixed inline styles, both list kinds, a quote, a six
// column wide table, an image inside a table cell, a standalone image, a code
// fence and inert unknown/malicious HTML, then inspects every page of the PDF
// with the Go parser above: page count, per-page extractable Chinese text,
// section order, font embedding, image placement and inert output.
func TestRenderLongChineseMixedDocumentCoversEveryPage(t *testing.T) {
	var imageBytes bytes.Buffer
	bitmap := image.NewRGBA(image.Rect(0, 0, 640, 360))
	for row := range 360 {
		for column := range 640 {
			value := color.RGBA{R: 215, G: 66, B: 55, A: 255}
			if column >= 320 {
				value = color.RGBA{R: 28, G: 141, B: 168, A: 255}
			}
			bitmap.Set(column, row, value)
		}
	}
	if err := png.Encode(&imageBytes, bitmap); err != nil {
		t.Fatal(err)
	}
	loads := 0
	load := func(relative string) ([]byte, error) {
		loads++
		if relative != "assets/p.png" {
			t.Fatalf("unexpected asset %q", relative)
		}
		return imageBytes.Bytes(), nil
	}
	const sections = 30
	var source strings.Builder
	source.WriteString("# 墨栈长中文导出\n\n")
	for index := 1; index <= sections; index++ {
		fmt.Fprintf(&source, "第%02d段标记：这是中文混排段落，包含 **加粗**、*斜体*、`代码` 与 [链接](https://example.org/%d)。\n\n", index, index)
	}
	source.WriteString("- 无序列表第一项\n- 无序列表第二项\n\n1. 有序列表第一项\n2. 有序列表第二项\n\n> 引用中文段落继续\n\n")
	source.WriteString("| 第一列 | 第二列 | 第三列 | 第四列 | 第五列 | 第六列 |\n| --- | --- | --- | --- | --- | --- |\n")
	for row := 1; row <= 3; row++ {
		fmt.Fprintf(&source, "| 宽表单元%d | 数值%d | 说明%d | 数据%d | 内容%d | 备注%d |\n", row, row, row, row, row, row)
	}
	source.WriteString("| ![表格图](assets/p.png) | 表格图右 | 单元格三 | 单元格四 | 单元格五 | 单元格六 |\n\n")
	source.WriteString("![独立图](assets/p.png)\n\n```go\nfmt.Println(\"代码中文\")\n```\n\n")
	source.WriteString("<script>alert(1)</script>\n\n<x-widget onclick=\"alert(2)\">未知节点</x-widget>\n")
	data, err := RenderMarkdown([]byte(source.String()), load, testFont(t))
	if err != nil {
		t.Fatal(err)
	}
	savePDFArtifact(t, "Long-Mixed.pdf", data)
	if loads != 1 || !bytes.Contains(data, []byte("/Subtype /Image")) {
		t.Fatalf("table/standalone image loads=%d embedded=%v", loads, bytes.Contains(data, []byte("/Subtype /Image")))
	}
	pdf := inspectPDF(t, data)
	if len(pdf.pages) < 2 {
		t.Fatalf("long mixed sample rendered %d page(s), want multiple", len(pdf.pages))
	}
	if pdf.mediaWidth <= 0 || pdf.mediaHeight <= 0 {
		t.Fatalf("page media box=%vx%v", pdf.mediaWidth, pdf.mediaHeight)
	}
	if !pdf.typeZero || !pdf.toUnicode {
		t.Fatalf("font embedding: Type0=%v ToUnicode=%v", pdf.typeZero, pdf.toUnicode)
	}
	if len(pdf.fontFile) < 1024 || !bytes.HasPrefix(pdf.fontFile, []byte{0, 1, 0, 0}) {
		t.Fatalf("embedded font file=%d bytes, header=%x", len(pdf.fontFile), pdf.fontFile[:min(len(pdf.fontFile), 4)])
	}
	for _, forbidden := range []string{"/JavaScript", "/OpenAction", "/URI (javascript:", "/AA "} {
		if bytes.Contains(data, []byte(forbidden)) {
			t.Errorf("PDF contains active content %q", forbidden)
		}
	}
	compact := func(value string) string {
		return strings.Map(func(character rune) rune {
			if unicode.IsSpace(character) {
				return -1
			}
			return character
		}, value)
	}
	pages := make([]string, len(pdf.pageTexts))
	for index, runs := range pdf.pageTexts {
		pages[index] = compact(strings.Join(runs, ""))
		if pages[index] == "" {
			t.Errorf("page %d has no extractable text", index+1)
		}
	}
	headingPage, rawFirstLinePage := -1, -1
	for index, page := range pages {
		if strings.Contains(page, compact("原始 HTML（未执行）")) {
			headingPage = index
		}
		if strings.Contains(page, compact("<script>alert(1)</script>")) {
			rawFirstLinePage = index
		}
	}
	if headingPage < 0 || rawFirstLinePage < 0 || headingPage != rawFirstLinePage {
		t.Errorf("raw HTML heading page=%d first content page=%d, want the same page", headingPage+1, rawFirstLinePage+1)
	}
	all := compact(strings.Join(pages, ""))
	pageOfSection := map[int]int{}
	for index, page := range pages {
		for section := 1; section <= sections; section++ {
			marker := fmt.Sprintf("第%02d段标记", section)
			if !strings.Contains(page, marker) {
				continue
			}
			if previous, ok := pageOfSection[section]; ok {
				t.Errorf("section %d appears on pages %d and %d", section, previous+1, index+1)
			}
			pageOfSection[section] = index
		}
	}
	for section := 1; section <= sections; section++ {
		if _, ok := pageOfSection[section]; !ok {
			t.Errorf("section %d missing from every page", section)
		}
	}
	if pageOfSection[1] != 0 {
		t.Errorf("first section on page %d, want page 1", pageOfSection[1]+1)
	}
	if pageOfSection[sections] <= pageOfSection[1] {
		t.Errorf("sections did not flow across pages: first page %d last page %d", pageOfSection[1]+1, pageOfSection[sections]+1)
	}
	for _, lastPageText := range []string{"<script>alert(1)</script>", "</x-widget>"} {
		if !strings.Contains(pages[len(pages)-1], compact(lastPageText)) {
			t.Errorf("last page %d lost trailing inert content %q", len(pages), lastPageText)
		}
	}
	previous := -1
	for section := 1; section <= sections; section++ {
		page := pageOfSection[section]
		if page < previous {
			t.Errorf("section order went backwards at section %d", section)
		}
		previous = page
	}
	for _, expected := range []string{
		"墨栈长中文导出", "加粗", "斜体", "代码", "链接",
		"无序列表第一项", "无序列表第二项", "有序列表第一项", "有序列表第二项", "引用中文段落继续",
		"第一列", "第二列", "第三列", "第四列", "第五列", "第六列",
		"宽表单元1", "数值3", "说明3", "数据3", "内容3", "备注3",
		"表格图", "表格图右", "单元格三", "单元格四", "单元格五", "单元格六",
		"fmt.Println", "代码中文", "未知节点", "<script>alert(1)</script>",
	} {
		if !strings.Contains(all, compact(expected)) {
			t.Errorf("PDF text lost %q", expected)
		}
	}
	placed := 0
	for pageIndex, placements := range pdf.images {
		for _, placement := range placements {
			placed++
			if placement.w <= 0 || placement.h <= 0 {
				t.Errorf("page %d image %q has empty rect %+v", pageIndex+1, placement.name, placement)
			}
			if placement.x < 0 || placement.y < 0 || placement.x+placement.w > pdf.mediaWidth+0.01 || placement.y+placement.h > pdf.mediaHeight+0.01 {
				t.Errorf("page %d image %q is cropped by the page: %+v media=%vx%v", pageIndex+1, placement.name, placement, pdf.mediaWidth, pdf.mediaHeight)
			}
		}
	}
	if placed != 2 {
		t.Errorf("image placements=%d want the table cell and standalone image", placed)
	}
}

func TestRenderRichHTMLDoesNotReserveSpaceForOrdinaryHeadingAndPre(t *testing.T) {
	for _, separator := range []string{"", "\n"} {
		source := strings.Repeat("<p>A</p>", 27) + "<h2> </h2>" + separator + "<pre>line</pre>"
		data, err := RenderRichHTML([]byte(source), nil, testFont(t))
		if err != nil {
			t.Fatal(err)
		}
		pages := inspectPDF(t, data).pageTexts
		if len(pages) != 1 || !strings.Contains(strings.Join(pages[0], ""), "line") {
			t.Fatalf("separator %q: ordinary pre ended up on %d pages, want 1", separator, len(pages))
		}
	}
}

func TestRenderLongRawHTMLPreContinuesAcrossPages(t *testing.T) {
	var source strings.Builder
	source.WriteString(`<html><body><main><p>正文</p></main><section class="unsupported"><h2>原始 HTML（未执行）</h2><pre>`)
	for line := 1; line <= 150; line++ {
		fmt.Fprintf(&source, "raw-line-%03d\n", line)
	}
	source.WriteString(`</pre></section></body></html>`)
	data, err := render([]byte(source.String()), testFont(t))
	if err != nil {
		t.Fatal(err)
	}
	pages := inspectPDF(t, data).pageTexts
	if len(pages) < 2 {
		t.Fatalf("long raw HTML rendered %d page(s), want multiple", len(pages))
	}
	first := strings.Join(pages[0], "")
	if !strings.Contains(first, "原始 HTML（未执行）") || !strings.Contains(first, "raw-line-001") {
		t.Errorf("raw HTML heading and first line split: %q", first)
	}
	all := make([]string, 0, len(pages))
	for _, page := range pages {
		all = append(all, strings.Join(page, ""))
	}
	text := strings.Join(all, "")
	for line := 1; line <= 150; line++ {
		marker := fmt.Sprintf("raw-line-%03d", line)
		if count := strings.Count(text, marker); count != 1 {
			t.Errorf("raw HTML %q occurs %d times, want once", marker, count)
		}
	}
	if !strings.Contains(all[len(all)-1], "raw-line-150") {
		t.Error("last page lost the final raw HTML line")
	}
}
