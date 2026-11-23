package richedit

import (
	"bytes"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "Rich-HTML.mh"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func leafWithText(t *testing.T, document Document, value string) Leaf {
	t.Helper()
	for _, block := range document.Blocks {
		for _, leaf := range block.Leaves {
			if leaf.Visible == value {
				return leaf
			}
		}
	}
	t.Fatalf("leaf %q not found", value)
	return Leaf{}
}

func TestParseWangEditorFragmentWithSourceMappedLeaves(t *testing.T) {
	source := fixture(t)
	document := Parse(source)
	if len(document.Blocks) < 11 {
		t.Fatalf("parsed only %d blocks: %+v", len(document.Blocks), document.Blocks)
	}
	first := document.Blocks[0]
	if first.Kind != Paragraph || first.Visible != "A & bold 斜体 link" || first.ReadOnly || string(source[first.Start:first.End]) != `<p data-id="note">A &amp; <strong>bold</strong> <em>斜体</em> <a href="https://example.org">link</a></p>` {
		t.Fatalf("paragraph source map=%+v", first)
	}
	if leaf := leafWithText(t, document, "A & "); !leaf.Editable || string(source[leaf.Start:leaf.End]) != "A &amp; " {
		t.Fatalf("entity leaf=%+v", leaf)
	}
	if leaf := leafWithText(t, document, "bold"); !leaf.Editable || !leaf.Bold {
		t.Fatalf("strong leaf=%+v", leaf)
	}
	if leaf := leafWithText(t, document, "斜体"); !leaf.Editable || !leaf.Italic {
		t.Fatalf("emphasis leaf=%+v", leaf)
	}
	if leaf := leafWithText(t, document, "link"); !leaf.SafeLink || leaf.Link != "https://example.org" {
		t.Fatalf("safe link leaf=%+v", leaf)
	}
	for _, test := range []struct {
		kind Kind
		text string
	}{
		{Heading, "标题"}, {ListItem, "一"}, {ListItem, "二"}, {Quote, "引用"},
		{TableCell, "列"}, {TableCell, "值"}, {Paragraph, ""}, {Paragraph, "本地图"}, {Code, "literal"},
	} {
		found := false
		for _, block := range document.Blocks {
			if block.Kind == test.kind && block.Visible == test.text {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing block kind=%v text=%q", test.kind, test.text)
		}
	}
	if leaf := leafWithText(t, document, "本地图"); leaf.ImageSource != "assets/p.png" || !leaf.SafeAsset || leaf.Editable {
		t.Fatalf("local image leaf=%+v", leaf)
	}
	if leaf := leafWithText(t, document, ""); !leaf.Editable || string(source[leaf.Start:leaf.End]) != "<br>" {
		t.Fatalf("empty paragraph leaf=%+v", leaf)
	}
	for _, block := range document.Blocks {
		if block.Kind == Unknown && (!block.ReadOnly || block.Start >= block.End || block.Visible == "") {
			t.Fatalf("unknown block not visibly read-only: %+v", block)
		}
	}
}

func TestRealMkDownSampleKeepsAdjacentParagraphsAndLiteralAmpersand(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("testdata", "Real-MkDown.mh"))
	if err != nil {
		t.Fatal(err)
	}
	if string(source) != "<p><strong>真实码克样本</strong></p><p>正文 A & B</p>" {
		t.Fatalf("reference app sample changed: %q", source)
	}
	document := Parse(source)
	if len(document.Blocks) != 2 || document.Blocks[0].Kind != Paragraph || document.Blocks[0].Visible != "真实码克样本" || document.Blocks[1].Kind != Paragraph || document.Blocks[1].Visible != "正文 A & B" {
		t.Fatalf("reference app paragraphs: %+v", document.Blocks)
	}
	bold := leafWithText(t, document, "真实码克样本")
	plain := leafWithText(t, document, "正文 A & B")
	if !bold.Editable || !bold.Bold || !plain.Editable || plain.Bold || document.Blocks[0].End != document.Blocks[1].Start {
		t.Fatalf("reference app leaves: bold=%+v plain=%+v", bold, plain)
	}
	if unchanged, err := ReplaceLeaf(source, plain, plain.Visible); err != nil || !bytes.Equal(unchanged, source) {
		t.Fatalf("reference app no-op changed bytes: %q, %v", unchanged, err)
	}
	appended, err := ReplaceLeaf(source, plain, plain.Visible+" 新")
	if err != nil || string(appended) != "<p><strong>真实码克样本</strong></p><p>正文 A & B 新</p>" {
		t.Fatalf("reference app append rewrote old text: %q, %v", appended, err)
	}
	for _, test := range []struct{ visible, want string }{
		{"新 正文 A & B", "<p><strong>真实码克样本</strong></p><p>新 正文 A & B</p>"},
		{"政文 A & B", "<p><strong>真实码克样本</strong></p><p>政文 A & B</p>"},
		{"正文 A & 新B", "<p><strong>真实码克样本</strong></p><p>正文 A & 新B</p>"},
		{"正文 A &", "<p><strong>真实码克样本</strong></p><p>正文 A &</p>"},
	} {
		updated, err := ReplaceLeaf(source, plain, test.visible)
		if err != nil || string(updated) != test.want {
			t.Errorf("real sample edit %q rewrote untouched text: %q, %v", test.visible, updated, err)
		}
	}
	updated, err := ReplaceLeaf(source, plain, "正文 X & 新")
	if err != nil || string(updated) != "<p><strong>真实码克样本</strong></p><p>正文 X &amp; 新</p>" {
		t.Fatalf("reference app edit changed other source: %q, %v", updated, err)
	}
}

func TestAppendingRichTextKeepsOriginalEntitySpelling(t *testing.T) {
	source := []byte("<p>&#x41; &amp; B</p>")
	leaf := leafWithText(t, Parse(source), "A & B")
	updated, err := ReplaceLeaf(source, leaf, leaf.Visible+" 新 &")
	if err != nil || string(updated) != "<p>&#x41; &amp; B 新 &amp;</p>" {
		t.Fatalf("append changed original entities: %q, %v", updated, err)
	}
	prepended, err := ReplaceLeaf(source, leaf, "新 "+leaf.Visible)
	if err != nil || string(prepended) != "<p>新 &#x41; &amp; B</p>" {
		t.Fatalf("prepend changed original entities: %q, %v", prepended, err)
	}
}

func TestReplaceLeafPreservesUntouchedEntityReferences(t *testing.T) {
	for _, test := range []struct {
		name, raw, before, after, want string
	}{
		{"replace", "&#x26; b &#60; &#x597D; &NotEqualTilde;", "& b < 好 ≂̸", "& 你<& < 好 ≂̸", "&#x26; 你&lt;&amp; &#60; &#x597D; &NotEqualTilde;"},
		{"insert-beside-entity", "&#x41;&#x42;", "AB", "A<B", "&#x41;&lt;&#x42;"},
		{"delete", "&#x41;中&#x42;", "A中B", "AB", "&#x41;&#x42;"},
		{"no-op", "&#x41;&amp;", "A&", "A&", "&#x41;&amp;"},
		{"compound-interior", "&NotEqualTilde; &#x41;", "≂̸ A", "≂X A", "≂X A"},
		{"numeric-boundary-merge", "&#65B &#x43;", "AB C", "A2 C", "A2 C"},
	} {
		t.Run(test.name, func(t *testing.T) {
			const prefix = "<p data-keep='&#65;'>"
			const suffix = "</p>\r\n<custom>opaque &#97;</custom>"
			source := []byte(prefix + test.raw + suffix)
			leaf := leafWithText(t, Parse(source), test.before)
			updated, err := ReplaceLeaf(source, leaf, test.after)
			if err != nil || string(updated) != prefix+test.want+suffix {
				t.Fatalf("patch = %q, %v; want %q", updated, err, prefix+test.want+suffix)
			}
			if visible := Parse(updated).Blocks[0].Visible; visible != test.after {
				t.Fatalf("visible = %q; want %q", visible, test.after)
			}
		})
	}
}

func TestRichTextInsertionCannotTurnOldAmpersandIntoAnEntity(t *testing.T) {
	source := []byte("<p>&am B</p>")
	leaf := leafWithText(t, Parse(source), "&am B")
	updated, err := ReplaceLeaf(source, leaf, "&amp; B")
	if err != nil || string(updated) != "<p>&amp;amp; B</p>" {
		t.Fatalf("insertion changed visible text through an entity: %q, %v", updated, err)
	}
}

func TestReplaceLeafPatchesOnlyOneSourceInterval(t *testing.T) {
	original := fixture(t)
	source := append([]byte{0xef, 0xbb, 0xbf}, bytes.ReplaceAll(original, []byte("\n"), []byte("\r\n"))...)
	document := Parse(source)
	leaf := leafWithText(t, document, "A & ")
	if leaf.Start < 3 || !bytes.Equal(source[:3], []byte{0xef, 0xbb, 0xbf}) {
		t.Fatalf("BOM offset=%d", leaf.Start)
	}
	unchanged, err := ReplaceLeaf(source, leaf, leaf.Visible)
	if err != nil || !bytes.Equal(unchanged, source) {
		t.Fatalf("no-op reserialized entity: %q, %v", unchanged, err)
	}
	updated, err := ReplaceLeaf(source, leaf, "A < B & 新")
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Replace(source, []byte("A &amp; "), []byte("A &lt; B &amp; 新"), 1)
	if !bytes.Equal(updated, want) || !bytes.Contains(updated, []byte("<!--keep this comment-->\r\n<x-unknown data-s=\"kept\">")) {
		t.Fatalf("leaf edit changed unrelated HTML: %q", updated)
	}
	if _, err := ReplaceLeaf(updated, leaf, "stale"); !errors.Is(err, ErrStaleLeaf) {
		t.Fatalf("stale interval accepted: %v", err)
	}
	if _, err := ReplaceLeaf(source, leaf, "line\nnext"); !errors.Is(err, ErrReadOnlyLeaf) {
		t.Fatalf("multiline leaf edit accepted: %v", err)
	}
	blank := []byte("<p><br></p>")
	blankLeaf := leafWithText(t, Parse(blank), "")
	filled, err := ReplaceLeaf(blank, blankLeaf, "开始")
	if err != nil || string(filled) != "<p>开始</p>" {
		t.Fatalf("empty paragraph edit=%q, %v", filled, err)
	}
	cell := []byte("<table><tbody><tr><td>old</td></tr></tbody></table>")
	cellLeaf := leafWithText(t, Parse(cell), "old")
	patched, err := ReplaceLeaf(cell, cellLeaf, "新 & <value>")
	if err != nil || string(patched) != "<table><tbody><tr><td>新 &amp; &lt;value&gt;</td></tr></tbody></table>" {
		t.Fatalf("table cell edit=%q, %v", patched, err)
	}
}

func TestUnsafeAndMalformedHTMLStayVisibleReadOnly(t *testing.T) {
	for _, test := range []struct {
		source string
		needle string
	}{
		{`<p onclick="run()">unsafe</p>`, "unsafe"},
		{`<p><a href="javascript:run()">danger</a></p>`, "danger"},
		{`<p><span data-x="1">unknown child</span></p>`, "unknown child"},
		{`<p><strong>broken</p>`, "broken"},
		{`<script>run()</script><p>safe</p>`, "<script>"},
		{`<iframe src="https://example.org"></iframe><p>safe</p>`, "<iframe"},
	} {
		document := Parse([]byte(test.source))
		visible, unsafe := false, false
		for _, block := range document.Blocks {
			visible = visible || strings.Contains(block.Visible, test.needle)
			if block.ReadOnly && strings.Contains(block.Visible, test.needle) {
				unsafe = true
				for _, leaf := range block.Leaves {
					if leaf.Editable {
						t.Fatalf("unsafe source %q has editable leaf %+v", test.source, leaf)
					}
				}
			}
		}
		if !visible || !unsafe {
			t.Fatalf("unsafe source was hidden or editable: %q, %+v", test.source, document.Blocks)
		}
	}
	unsafe := []byte(`<p onclick="run()">unsafe</p>`)
	leaf := leafWithText(t, Parse(unsafe), "unsafe")
	if _, err := ReplaceLeaf(unsafe, leaf, "changed"); !errors.Is(err, ErrReadOnlyLeaf) {
		t.Fatalf("unsafe attribute edit=%v", err)
	}
	remote := leafWithText(t, Parse([]byte(`<p><img src="https://example.org/a.png" alt="remote"></p>`)), "remote")
	if remote.SafeAsset || remote.Editable {
		t.Fatalf("remote image became active: %+v", remote)
	}
	parent := leafWithText(t, Parse([]byte(`<p><img src="../assets/image.png" alt="parent"></p>`)), "parent")
	if !parent.SafeAsset {
		t.Fatalf("workspace-relative parent image was rejected before root confinement: %+v", parent)
	}
}

func TestUnclosedContainerAndMalformedTagFailClosed(t *testing.T) {
	for _, source := range []string{
		"<ul><li>item</li>",
		"<table><tbody><tr><td>cell</td></tr></tbody>",
	} {
		document := Parse([]byte(source))
		if len(document.Blocks) != 1 || !document.Blocks[0].ReadOnly || len(document.Blocks[0].Leaves) == 0 || document.Blocks[0].Leaves[0].Editable {
			t.Fatalf("unclosed container retained editable leaf: %q %+v", source, document.Blocks)
		}
	}
	broken := `<p>safe</p><broken attr='`
	document := Parse([]byte(broken))
	if len(document.Blocks) < 2 || document.Blocks[len(document.Blocks)-1].Kind != Unknown || !document.Blocks[len(document.Blocks)-1].ReadOnly || !strings.Contains(document.Blocks[len(document.Blocks)-1].Visible, "<broken attr") {
		t.Fatalf("malformed trailing source was hidden: %+v", document.Blocks)
	}
}

func TestUnsafeContainerAttributesMakeDescendantLeavesReadOnly(t *testing.T) {
	for _, source := range []string{
		`<ul onclick="run()"><li>list</li></ul>`,
		`<table srcdoc="x"><tbody><tr><td>cell</td></tr></tbody></table>`,
	} {
		document := Parse([]byte(source))
		if len(document.Blocks) != 1 || !document.Blocks[0].ReadOnly || len(document.Blocks[0].Leaves) != 1 || document.Blocks[0].Leaves[0].Editable {
			t.Fatalf("unsafe parent retained editable descendant: %+v", document.Blocks)
		}
	}
}

func TestSourceIntervalsStayBoundedAcrossMalformedCorpus(t *testing.T) {
	parts := []string{"<p>", "</p>", "<ul>", "</ul>", "<li>", "</li>", "<strong>", "</strong>", "<x-unknown>", "</x-unknown>", "<script>", "</script>", "<br>", "&amp;", "字", "raw"}
	generator := rand.New(rand.NewSource(71))
	corpus := [][]byte{fixture(t), []byte("<p><br></p>"), []byte("<table><tbody><tr><td>cell</td></tr></tbody></table>")}
	for sample := 0; sample < 250; sample++ {
		var source strings.Builder
		for part := 0; part < 12; part++ {
			source.WriteString(parts[generator.Intn(len(parts))])
		}
		corpus = append(corpus, []byte(source.String()))
	}
	for _, source := range corpus {
		document := Parse(source)
		for _, block := range document.Blocks {
			if block.Start < 0 || block.End < block.Start || block.End > len(source) {
				t.Fatalf("out-of-bounds block in %q: %+v", source, block)
			}
			previous := block.Start
			for _, leaf := range block.Leaves {
				if leaf.Start < previous || leaf.End < leaf.Start || leaf.End > block.End {
					t.Fatalf("out-of-bounds leaf in %q: block=%+v leaf=%+v", source, block, leaf)
				}
				previous = leaf.End
				if !leaf.Editable {
					continue
				}
				unchanged, err := ReplaceLeaf(source, leaf, leaf.Visible)
				if err != nil || !bytes.Equal(unchanged, source) {
					t.Fatalf("no-op leaf rewrote %q: %v", source, err)
				}
			}
		}
	}
}

func TestEmptyParagraphKeepsZeroWidthEditableLeaf(t *testing.T) {
	source := []byte("<p></p>")
	document := Parse(source)
	if len(document.Blocks) != 1 || len(document.Blocks[0].Leaves) != 1 {
		t.Fatalf("empty paragraph lost input point: %+v", document.Blocks)
	}
	leaf := document.Blocks[0].Leaves[0]
	if !leaf.Editable || leaf.Visible != "" || leaf.Start != 3 || leaf.End != 3 {
		t.Fatalf("empty paragraph leaf=%+v", leaf)
	}
	updated, err := ReplaceLeaf(source, leaf, "新")
	if err != nil || string(updated) != "<p>新</p>" {
		t.Fatalf("empty paragraph insert=%q, %v", updated, err)
	}
}

func TestOrderedRichHTMLListRetainsStartNumber(t *testing.T) {
	document := Parse([]byte(`<ol start="5"><li>first</li><li>second</li></ol>`))
	if len(document.Blocks) != 2 || document.Blocks[0].Kind != ListItem || document.Blocks[0].Number != 5 || document.Blocks[1].Kind != ListItem || document.Blocks[1].Number != 6 || document.Blocks[0].ReadOnly || document.Blocks[1].ReadOnly {
		t.Fatalf("ordered list=%+v", document.Blocks)
	}
}

// richMatrixLeaf and richMatrixBlock describe the declared support classes for
// the .mh source map. Every expected raw string is an exact slice of the
// original fixture, so a passing matrix proves the parser never reserializes.
type richMatrixLeaf struct {
	raw, visible string
	editable     bool
	bold, italic bool
	underline    bool
	strike, mono bool
	link         string
	safeLink     bool
	imageSource  string
	safeAsset    bool
}

type richMatrixBlock struct {
	kind     Kind
	open     string
	raw      string
	close    string
	visible  string
	readOnly bool
	level    int
	number   int
	cell     bool
	row      int
	column   int
	leaves   []richMatrixLeaf
}

func TestRichHTMLSupportMatrixPreservesExactSourceIntervals(t *testing.T) {
	load := func(t *testing.T, fixture, literal string) []byte {
		t.Helper()
		if fixture == "" {
			return []byte(literal)
		}
		data, err := os.ReadFile(filepath.Join("testdata", fixture))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	for _, test := range []struct {
		name    string
		fixture string
		source  string
		blocks  []richMatrixBlock
	}{
		{
			name:    "real MkDown paragraphs",
			fixture: "Real-MkDown.mh",
			blocks: []richMatrixBlock{
				{kind: Paragraph, open: "<p>", raw: "<p><strong>真实码克样本</strong></p>", close: "</p>", visible: "真实码克样本",
					leaves: []richMatrixLeaf{{raw: "真实码克样本", visible: "真实码克样本", editable: true, bold: true}}},
				{kind: Paragraph, open: "<p>", raw: "<p>正文 A & B</p>", close: "</p>", visible: "正文 A & B",
					leaves: []richMatrixLeaf{{raw: "正文 A & B", visible: "正文 A & B", editable: true}}},
			},
		},
		{
			name:    "real MkDown structure",
			fixture: "Real-MkDown-Structure.mh",
			blocks: []richMatrixBlock{
				{kind: Paragraph, open: "<p>", raw: "<p>结构样本</p>", close: "</p>", visible: "结构样本",
					leaves: []richMatrixLeaf{{raw: "结构样本", visible: "结构样本", editable: true}}},
				{kind: ListItem, open: "<li>", raw: "<li>第一项</li>", close: "</li>", visible: "第一项",
					leaves: []richMatrixLeaf{{raw: "第一项", visible: "第一项", editable: true}}},
				{kind: ListItem, open: "<li>", raw: "<li>第二项</li>", close: "</li>", visible: "第二项",
					leaves: []richMatrixLeaf{{raw: "第二项", visible: "第二项", editable: true}}},
				{kind: ListItem, open: "<li>", raw: "<li></li>", close: "</li>",
					leaves: []richMatrixLeaf{{raw: "", visible: "", editable: true}}},
				{kind: TableCell, open: `<th colSpan="1" rowSpan="1" width="auto">`, raw: `<th colSpan="1" rowSpan="1" width="auto">列名</th>`, close: "</th>", visible: "列名", cell: true, row: 0, column: 0,
					leaves: []richMatrixLeaf{{raw: "列名", visible: "列名", editable: true}}},
				{kind: TableCell, open: `<th colSpan="1" rowSpan="1" width="auto">`, raw: `<th colSpan="1" rowSpan="1" width="auto">说明</th>`, close: "</th>", visible: "说明", cell: true, row: 0, column: 1,
					leaves: []richMatrixLeaf{{raw: "说明", visible: "说明", editable: true}}},
				{kind: TableCell, open: `<td colSpan="1" rowSpan="1" width="auto">`, raw: `<td colSpan="1" rowSpan="1" width="auto">甲</td>`, close: "</td>", visible: "甲", cell: true, row: 1, column: 0,
					leaves: []richMatrixLeaf{{raw: "甲", visible: "甲", editable: true}}},
				{kind: TableCell, open: `<td colSpan="1" rowSpan="1" width="auto">`, raw: `<td colSpan="1" rowSpan="1" width="auto">值 & 保留</td>`, close: "</td>", visible: "值 & 保留", cell: true, row: 1, column: 1,
					leaves: []richMatrixLeaf{{raw: "值 & 保留", visible: "值 & 保留", editable: true}}},
				{kind: Paragraph, open: "<p>", raw: "<p><br></p>", close: "</p>",
					leaves: []richMatrixLeaf{{raw: "<br>", visible: "", editable: true}}},
			},
		},
		{
			name:    "wangEditor fixture",
			fixture: "Rich-HTML.mh",
			blocks: []richMatrixBlock{
				{kind: Paragraph, open: `<p data-id="note">`, raw: `<p data-id="note">A &amp; <strong>bold</strong> <em>斜体</em> <a href="https://example.org">link</a></p>`, close: "</p>", visible: "A & bold 斜体 link",
					leaves: []richMatrixLeaf{
						{raw: "A &amp; ", visible: "A & ", editable: true},
						{raw: "bold", visible: "bold", editable: true, bold: true},
						{raw: " ", visible: " ", editable: true},
						{raw: "斜体", visible: "斜体", editable: true, italic: true},
						{raw: " ", visible: " ", editable: true},
						{raw: "link", visible: "link", editable: true, link: "https://example.org", safeLink: true},
					}},
				{kind: Heading, open: "<h2>", raw: "<h2>标题</h2>", close: "</h2>", visible: "标题", level: 2,
					leaves: []richMatrixLeaf{{raw: "标题", visible: "标题", editable: true}}},
				{kind: ListItem, open: "<li>", raw: "<li>一</li>", close: "</li>", visible: "一",
					leaves: []richMatrixLeaf{{raw: "一", visible: "一", editable: true}}},
				{kind: ListItem, open: "<li>", raw: "<li><s>二</s></li>", close: "</li>", visible: "二",
					leaves: []richMatrixLeaf{{raw: "二", visible: "二", editable: true, strike: true}}},
				{kind: Quote, open: "<blockquote>", raw: "<blockquote><p>引用</p></blockquote>", close: "</blockquote>", visible: "引用",
					leaves: []richMatrixLeaf{{raw: "引用", visible: "引用", editable: true}}},
				{kind: TableCell, open: "<th>", raw: "<th>列</th>", close: "</th>", visible: "列", cell: true, row: 0, column: 0,
					leaves: []richMatrixLeaf{{raw: "列", visible: "列", editable: true}}},
				{kind: TableCell, open: "<td>", raw: "<td>值</td>", close: "</td>", visible: "值", cell: true, row: 0, column: 1,
					leaves: []richMatrixLeaf{{raw: "值", visible: "值", editable: true}}},
				{kind: Paragraph, open: "<p>", raw: "<p><br></p>", close: "</p>",
					leaves: []richMatrixLeaf{{raw: "<br>", visible: "", editable: true}}},
				{kind: Paragraph, open: "<p>", raw: `<p><img src="assets/p.png" alt="本地图"></p>`, close: "</p>", visible: "本地图",
					leaves: []richMatrixLeaf{
						{raw: `<img src="assets/p.png" alt="本地图">`, visible: "本地图", imageSource: "assets/p.png", safeAsset: true},
						{raw: "", visible: "", editable: true},
					}},
				{kind: Code, open: "<pre>", raw: "<pre><code>literal</code></pre>", close: "</pre>", visible: "literal", readOnly: true,
					leaves: []richMatrixLeaf{{raw: "literal", visible: "literal", mono: true}}},
				{kind: Unknown, open: `<x-unknown data-s="kept">`, raw: `<x-unknown data-s="kept"><p>未知</p></x-unknown>`, close: "</x-unknown>", visible: `<x-unknown data-s="kept"><p>未知</p></x-unknown>`, readOnly: true},
				{kind: Unknown, open: "<script>", raw: "<script>alert(1)</script>", close: "</script>", visible: "<script>alert(1)</script>", readOnly: true},
			},
		},
		{
			name:   "entity and attribute paragraph between comments",
			source: `<!--pre keep--><p class='note' data-id="7">A &amp; <strong>粗</strong> B</p><!--post keep-->`,
			blocks: []richMatrixBlock{
				{kind: Paragraph, open: `<p class='note' data-id="7">`, raw: `<p class='note' data-id="7">A &amp; <strong>粗</strong> B</p>`, close: "</p>", visible: "A & 粗 B",
					leaves: []richMatrixLeaf{
						{raw: "A &amp; ", visible: "A & ", editable: true},
						{raw: "粗", visible: "粗", editable: true, bold: true},
						{raw: " B", visible: " B", editable: true},
					}},
			},
		},
		{
			name:   "nested style boundaries",
			source: `<p>a<strong>b<em>c</em></strong>d</p>`,
			blocks: []richMatrixBlock{
				{kind: Paragraph, open: "<p>", raw: `<p>a<strong>b<em>c</em></strong>d</p>`, close: "</p>", visible: "abcd",
					leaves: []richMatrixLeaf{
						{raw: "a", visible: "a", editable: true},
						{raw: "b", visible: "b", editable: true, bold: true},
						{raw: "c", visible: "c", editable: true, bold: true, italic: true},
						{raw: "d", visible: "d", editable: true},
					}},
			},
		},
		{
			name:   "simple table",
			source: `<table><tbody><tr><th>列</th><td>值</td></tr></tbody></table>`,
			blocks: []richMatrixBlock{
				{kind: TableCell, open: "<th>", raw: "<th>列</th>", close: "</th>", visible: "列", cell: true, row: 0, column: 0,
					leaves: []richMatrixLeaf{{raw: "列", visible: "列", editable: true}}},
				{kind: TableCell, open: "<td>", raw: "<td>值</td>", close: "</td>", visible: "值", cell: true, row: 0, column: 1,
					leaves: []richMatrixLeaf{{raw: "值", visible: "值", editable: true}}},
			},
		},
		{
			// Complex merged tables keep exact source intervals and scoped cell
			// text editing, while structural operations stay refused (asserted in
			// TestRichHTMLSupportMatrixOperationsKeepUntouchedSource).
			name:   "complex merged table",
			source: `<table><tbody><tr><td colspan="2">A</td></tr></tbody></table>`,
			blocks: []richMatrixBlock{
				{kind: TableCell, open: `<td colspan="2">`, raw: `<td colspan="2">A</td>`, close: "</td>", visible: "A", cell: true, row: 0, column: 0,
					leaves: []richMatrixLeaf{{raw: "A", visible: "A", editable: true}}},
			},
		},
		{
			name:   "unsafe attribute",
			source: `<p onclick="never()">unsafe</p>`,
			blocks: []richMatrixBlock{
				{kind: Paragraph, open: `<p onclick="never()">`, raw: `<p onclick="never()">unsafe</p>`, close: "</p>", visible: "unsafe", readOnly: true,
					leaves: []richMatrixLeaf{{raw: "unsafe", visible: "unsafe"}}},
			},
		},
		{
			name:   "unsafe link",
			source: `<p><a href="javascript:never()">danger</a></p>`,
			blocks: []richMatrixBlock{
				{kind: Paragraph, open: "<p>", raw: `<p><a href="javascript:never()">danger</a></p>`, close: "</p>", visible: "danger", readOnly: true,
					leaves: []richMatrixLeaf{{raw: "danger", visible: "danger", link: "javascript:never()"}}},
			},
		},
		{
			name:   "unsafe container",
			source: `<ul onclick="never()"><li>item</li></ul>`,
			blocks: []richMatrixBlock{
				{kind: ListItem, open: "<li>", raw: "<li>item</li>", close: "</li>", visible: "item", readOnly: true,
					leaves: []richMatrixLeaf{{raw: "item", visible: "item"}}},
			},
		},
		{
			name:   "read-only unknown island",
			source: `<p>one</p><x-unknown>raw</x-unknown><p>two</p>`,
			blocks: []richMatrixBlock{
				{kind: Paragraph, open: "<p>", raw: "<p>one</p>", close: "</p>", visible: "one",
					leaves: []richMatrixLeaf{{raw: "one", visible: "one", editable: true}}},
				{kind: Unknown, open: "<x-unknown>", raw: "<x-unknown>raw</x-unknown>", close: "</x-unknown>", visible: "<x-unknown>raw</x-unknown>", readOnly: true},
				{kind: Paragraph, open: "<p>", raw: "<p>two</p>", close: "</p>", visible: "two",
					leaves: []richMatrixLeaf{{raw: "two", visible: "two", editable: true}}},
			},
		},
		{
			name:   "safe and remote images",
			source: `<p><img src="assets/p.png" alt="图"></p><p><img src="https://example.org/p.png" alt="远程"></p>`,
			blocks: []richMatrixBlock{
				{kind: Paragraph, open: "<p>", raw: `<p><img src="assets/p.png" alt="图"></p>`, close: "</p>", visible: "图",
					leaves: []richMatrixLeaf{
						{raw: `<img src="assets/p.png" alt="图">`, visible: "图", imageSource: "assets/p.png", safeAsset: true},
						{raw: "", visible: "", editable: true},
					}},
				{kind: Paragraph, open: "<p>", raw: `<p><img src="https://example.org/p.png" alt="远程"></p>`, close: "</p>", visible: "远程",
					leaves: []richMatrixLeaf{
						{raw: `<img src="https://example.org/p.png" alt="远程">`, visible: "远程", imageSource: "https://example.org/p.png"},
						{raw: "", visible: "", editable: true},
					}},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := load(t, test.fixture, test.source)
			document := Parse(source)
			if len(document.Blocks) != len(test.blocks) {
				t.Fatalf("blocks = %d, want %d: %+v", len(document.Blocks), len(test.blocks), document.Blocks)
			}
			for index, want := range test.blocks {
				got := document.Blocks[index]
				if got.Kind != want.kind {
					t.Fatalf("block %d kind = %v, want %v", index, got.Kind, want.kind)
				}
				if got.Start < 0 || got.End < got.Start || got.End > len(source) {
					t.Fatalf("block %d interval [%d,%d) outside %d bytes", index, got.Start, got.End, len(source))
				}
				if raw := string(source[got.Start:got.End]); raw != want.raw {
					t.Fatalf("block %d raw = %q, want %q", index, raw, want.raw)
				}
				if want.open != "" {
					if got.OpenEnd < got.Start || got.OpenEnd > got.End || string(source[got.Start:got.OpenEnd]) != want.open {
						t.Fatalf("block %d open = %q, want %q", index, source[got.Start:got.OpenEnd], want.open)
					}
				}
				if want.close != "" {
					if got.CloseStart < got.Start || got.CloseStart > got.End || string(source[got.CloseStart:got.End]) != want.close {
						t.Fatalf("block %d close = %q, want %q", index, source[got.CloseStart:got.End], want.close)
					}
				}
				if got.Visible != want.visible || got.ReadOnly != want.readOnly {
					t.Fatalf("block %d visible/readOnly = %q/%v, want %q/%v", index, got.Visible, got.ReadOnly, want.visible, want.readOnly)
				}
				if want.level != 0 && got.Level != want.level {
					t.Fatalf("block %d level = %d, want %d", index, got.Level, want.level)
				}
				if want.number != 0 && got.Number != want.number {
					t.Fatalf("block %d number = %d, want %d", index, got.Number, want.number)
				}
				if want.cell {
					if start := got.TableStart; start < 0 || start+len("<table") > got.Start || string(source[start:start+len("<table")]) != "<table" {
						t.Fatalf("block %d table start = %d; cell %q is not inside a parsed <table>", index, got.TableStart, want.visible)
					}
					if got.Row != want.row || got.Column != want.column {
						t.Fatalf("block %d cell = row %d column %d, want row %d column %d", index, got.Row, got.Column, want.row, want.column)
					}
				}
				if len(got.Leaves) != len(want.leaves) {
					t.Fatalf("block %d leaves = %+v, want %d", index, got.Leaves, len(want.leaves))
				}
				for leafIndex, wantLeaf := range want.leaves {
					leaf := got.Leaves[leafIndex]
					if leaf.Start < got.Start || leaf.End < leaf.Start || leaf.End > got.End {
						t.Fatalf("block %d leaf %d interval [%d,%d) outside [%d,%d)", index, leafIndex, leaf.Start, leaf.End, got.Start, got.End)
					}
					if raw := string(source[leaf.Start:leaf.End]); raw != wantLeaf.raw {
						t.Fatalf("block %d leaf %d raw = %q, want %q", index, leafIndex, raw, wantLeaf.raw)
					}
					if leaf.Visible != wantLeaf.visible || leaf.Editable != wantLeaf.editable ||
						leaf.Bold != wantLeaf.bold || leaf.Italic != wantLeaf.italic || leaf.Underline != wantLeaf.underline ||
						leaf.Strike != wantLeaf.strike || leaf.Mono != wantLeaf.mono ||
						leaf.Link != wantLeaf.link || leaf.SafeLink != wantLeaf.safeLink ||
						leaf.ImageSource != wantLeaf.imageSource || leaf.SafeAsset != wantLeaf.safeAsset {
						t.Fatalf("block %d leaf %d = %+v, want %+v", index, leafIndex, leaf, wantLeaf)
					}
				}
				if got.ReadOnly {
					for _, leaf := range got.Leaves {
						if leaf.Editable {
							t.Fatalf("read-only block %d has editable leaf %+v", index, leaf)
						}
					}
				}
			}
			for index := 1; index < len(document.Blocks); index++ {
				previous, current := document.Blocks[index-1], document.Blocks[index]
				if current.Kind == TableCell && previous.Kind == TableCell && previous.TableStart == current.TableStart && previous.Row == current.Row && current.Column != previous.Column+1 {
					t.Fatalf("adjacent table cells %d,%d columns %d,%d are not consecutive", index-1, index, previous.Column, current.Column)
				}
			}
		})
	}
}
