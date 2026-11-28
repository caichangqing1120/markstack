package exportdocx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"reflect"
	"strings"
	"testing"
)

func readParts(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	parts := make(map[string][]byte)
	for _, file := range archive.File {
		if _, exists := parts[file.Name]; exists {
			t.Fatalf("duplicate ZIP part %q", file.Name)
		}
		payload, err := io.ReadAll(mustOpen(t, file))
		if err != nil {
			t.Fatal(err)
		}
		parts[file.Name] = payload
		if strings.HasSuffix(file.Name, ".xml") || strings.HasSuffix(file.Name, ".rels") {
			var root struct{ XMLName xml.Name }
			decoder := xml.NewDecoder(bytes.NewReader(payload))
			if err := decoder.Decode(&root); err != nil || root.XMLName.Space == "" {
				t.Fatalf("%s: root=%v, error=%v", file.Name, root.XMLName, err)
			}
			if _, err := decoder.Token(); err != io.EOF {
				t.Fatalf("%s: trailing data or invalid XML: %v", file.Name, err)
			}
		}
	}
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml", "word/styles.xml", "word/numbering.xml", "word/_rels/document.xml.rels"} {
		if len(parts[name]) == 0 {
			t.Errorf("missing part %q", name)
		}
	}
	return parts
}

func mustOpen(t *testing.T, file *zip.File) io.Reader {
	t.Helper()
	reader, err := file.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reader.Close() })
	return reader
}

func requireXML(t *testing.T, part []byte, expected ...string) {
	t.Helper()
	for _, value := range expected {
		if !bytes.Contains(part, []byte(value)) {
			t.Errorf("XML missing %q", value)
		}
	}
}

func TestMarkdownSemantics(t *testing.T) {
	source := "\xef\xbb\xbf# Heading one\r\n\r\n## 二级\n\n### Three\n\n#### Four\n\n##### Five\n\n###### Six\n\n" +
		"中文 & English **bold** *italic* ~~strike~~ `x < y` [site](https://example.com/?a=1&b=2)  \nnext line\n\n" +
		"3. Ordered\n   - Nested bullet\n4. Next number\n\n- Bullet\n  1. Nested number\n\n> Quoted\n\n```go\n  first\n\t second\n```\n\n" +
		"| Head | 标题 |\n|---|---|\n| **Cell** | `内联` |\n\n---\n"
	data, err := RenderMarkdown([]byte(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	parts := readParts(t, data)
	document, styles := parts["word/document.xml"], parts["word/styles.xml"]
	var stylesheet struct {
		Styles []struct {
			ID      string `xml:"styleId,attr"`
			Outline *struct {
				Level int `xml:"val,attr"`
			} `xml:"pPr>outlineLvl"`
		} `xml:"style"`
	}
	if err := xml.Unmarshal(styles, &stylesheet); err != nil {
		t.Fatal(err)
	}
	levels := make(map[string]int)
	for _, style := range stylesheet.Styles {
		if style.Outline != nil {
			levels[style.ID] = style.Outline.Level
		}
	}
	for level := range 6 {
		id := fmt.Sprintf("Heading%d", level+1)
		requireXML(t, document, `<w:pStyle w:val="`+id+`"/>`)
		if actual, exists := levels[id]; !exists || actual != level {
			t.Errorf("%s outline level=%d, present=%v; want %d", id, actual, exists, level)
		}
	}
	requireXML(t, document, "中文 &amp; English", "二级", "Nested bullet", "Nested number",
		`<w:b/>`, `<w:i/>`, `<w:strike/>`, `<w:rStyle w:val="InlineCode"/>`, `x &lt; y`,
		`<w:hyperlink r:id=`, `<w:br/>`, `<w:pStyle w:val="Quote"/>`, `<w:ilvl w:val="1"/>`,
		`<w:pStyle w:val="CodeBlock"/>`, `xml:space="preserve">  first</w:t>`, `<w:tab/>`, `xml:space="preserve"> second</w:t>`,
		`<w:tbl>`, `<w:tblHeader/>`, `<w:shd`, `<w:tblBorders>`, `>Cell</w:t>`, `>内联</w:t>`, `<w:bottom`)
	requireXML(t, styles, `w:styleId="Normal"`, `w:styleId="Hyperlink"`, `<w:left`, `Courier New`)
	requireXML(t, parts["word/numbering.xml"], `w:val="bullet"`, `w:val="decimal"`, `<w:startOverride w:val="3"/>`)
	requireXML(t, parts["word/_rels/document.xml.rels"], `TargetMode="External"`, `https://example.com/?a=1&amp;b=2`)
}

func TestRichHTMLImagesAndSafeLinks(t *testing.T) {
	fixtures := make(map[string][]byte)
	for _, format := range []struct {
		path   string
		encode func(io.Writer, image.Image) error
	}{
		{"wide.png", png.Encode},
		{"photo.jpg", func(out io.Writer, img image.Image) error { return jpeg.Encode(out, img, nil) }},
		{"icon.gif", func(out io.Writer, img image.Image) error { return gif.Encode(out, img, nil) }},
	} {
		var buffer bytes.Buffer
		if err := format.encode(&buffer, image.NewRGBA(image.Rect(0, 120, 960, 360))); err != nil {
			t.Fatal(err)
		}
		fixtures[format.path] = buffer.Bytes()
	}
	loads := map[string]int{}
	source := `<h1>文档</h1><div><p><b>Bold</b> <i>Italic</i> <s>Strike</s>` +
		` <a href="jav&#x61;script:alert(1)">unsafe link</a>` +
		` <a href="mailto:hello@example.net">Mail</a><a href="http://example.net/">HTTP</a>` +
		` <a href="//example.net/">relative link</a></p></div>` +
		`<p><img src="wide.png" alt="Wide &amp; tall"><img src="photo.jpg" alt="Photo"><img src="wide.png" alt="Again">` +
		`<img src="missing.png" alt="Missing"><img src="missing.png" alt="Still missing"><img src="corrupt.png" alt="Corrupt">` +
		`<img src="https://example.net/remote.png" alt="Remote"><img src="/secret.png" alt="Absolute">` +
		`<img src="../secret.png" alt="Traversal"><img src="%2e%2e%2fsecret.png" alt="Encoded traversal"><img src="file:///secret.png" alt="File URL">` +
		`<img src="icon.gif" alt="Icon"></p><script>hidden</script>` + "<p>clean\x01文字\ufffe</p>"
	data, err := RenderRichHTML([]byte(source), func(name string) ([]byte, error) {
		loads[name]++
		return fixtures[name], nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := (map[string]int{"wide.png": 1, "photo.jpg": 1, "missing.png": 1, "corrupt.png": 1, "icon.gif": 1}); !reflect.DeepEqual(loads, want) {
		t.Errorf("loaded paths %v; want %v", loads, want)
	}
	parts := readParts(t, data)
	doc, rels := parts["word/document.xml"], parts["word/_rels/document.xml.rels"]
	requireXML(t, doc, `<w:b/>`, `<w:i/>`, `<w:strike/>`, `unsafe link`, `relative link`, `clean文字`,
		`Missing`, `Still missing`, `Corrupt`, `Remote`, `Absolute`, `Traversal`, `File URL`,
		`<wp:extent cx="5486400" cy="1371600"/>`, `descr="Wide &amp; tall"`)
	if got := bytes.Count(doc, []byte("<w:drawing>")); got != 4 {
		t.Errorf("drawings = %d; want 4", got)
	}
	if bytes.Count(doc, []byte("<w:hyperlink ")) != 2 || bytes.Contains(doc, []byte("hidden")) || bytes.Contains(rels, []byte("javascript")) {
		t.Fatal("unsafe content exported as active content")
	}
	requireXML(t, rels, `mailto:hello@example.net`, `http://example.net/`, `media/image1.png`, `media/image2.jpg`, `media/image3.gif`)
	for name, fixture := range map[string]string{"image1.png": "wide.png", "image2.jpg": "photo.jpg", "image3.gif": "icon.gif"} {
		if !bytes.Equal(parts["word/media/"+name], fixtures[fixture]) {
			t.Errorf("embedded %s does not match %s", name, fixture)
		}
	}
}

func TestOutputBudget(t *testing.T) {
	data := make([]byte, maxOutput/64)
	doc := document{}
	for index := range 64 {
		doc.media = append(doc.media, part{fmt.Sprintf("word/media/image%d.png", index+1), data})
	}
	if output, err := doc.pack(); !errors.Is(err, ErrTooLarge) || output != nil {
		t.Fatalf("oversize package emitted %d bytes, error=%v", len(output), err)
	}
}

func TestMarkdownRawHTMLStaysInert(t *testing.T) {
	source := []byte("# Heading\n\n<table><tr><td>保留内容</td></tr></table>\n\n" +
		`<script>example()</script><img src="local.png" alt="photo">`)
	data, err := RenderMarkdown(source, func(path string) ([]byte, error) {
		t.Fatalf("raw HTML tried to load %q", path)
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	document := readParts(t, data)["word/document.xml"]
	requireXML(t, document, `w:val="Heading1"`, `&lt;td&gt;保留内容&lt;/td&gt;`, `&lt;script&gt;example()&lt;/script&gt;`)
	if bytes.Contains(document, []byte("<w:drawing>")) || bytes.Contains(document, []byte("<w:tbl>")) {
		t.Fatal("raw HTML was interpreted as document markup")
	}
}
