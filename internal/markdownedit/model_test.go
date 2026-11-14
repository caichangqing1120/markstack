package markdownedit

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func findBlock(t *testing.T, blocks []Block, kind Kind) Block {
	t.Helper()
	for _, block := range blocks {
		if block.Kind == kind {
			return block
		}
	}
	t.Fatalf("no %s block in %+v", kind, blocks)
	return Block{}
}

func TestParseSupportsSimpleBlocksAndProtectsComplexOnes(t *testing.T) {
	source := []byte("# 标题\r\n\r\n正文\r\n\r\n- 第一项\r\n- 第二项\r\n\r\n> 引用\r\n\r\n```go\r\ncode\r\n```\r\n\r\n| A | B |\r\n|---|---|\r\n| 1 | 2 |\r\n\r\n<!-- raw -->\r\n")
	blocks := Parse(source)
	for _, test := range []struct {
		kind     Kind
		visible  string
		editable bool
	}{
		{Heading, "标题", true}, {Paragraph, "正文", true},
		{ListItem, "第一项", true}, {Quote, "引用", true},
		{FencedCode, "", false}, {Table, "", false}, {Raw, "", false},
	} {
		block := findBlock(t, blocks, test.kind)
		if block.Visible != test.visible || block.Editable != test.editable {
			t.Errorf("%s = %+v", test.kind, block)
		}
	}
	previous := 0
	for _, block := range blocks {
		if block.Start < previous || block.End > len(source) || block.Start >= block.End {
			t.Fatalf("invalid source range: %+v", block)
		}
		previous = block.End
	}
}

func TestReplaceHeadingPreservesUTF8AndUnrelatedCRLFBytes(t *testing.T) {
	source := []byte("# 标题\r\n\r\n正文\r\n")
	block := findBlock(t, Parse(source), Heading)
	got, err := Replace(source, block, "新标题")
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte("# 新标题\r\n\r\n正文\r\n"); !bytes.Equal(got, want) {
		t.Fatalf("patched bytes = %q, want %q", got, want)
	}
}

func TestMixedATXHeadingEditsOnlyItsPlainLeaf(t *testing.T) {
	source := []byte("# Title续**粗** tail\r\n\r\nAfter\r\n")
	heading := findBlock(t, Parse(source), Heading)
	if !heading.Editable || heading.Visible != "Title续粗 tail" {
		t.Fatalf("mixed heading = %+v", heading)
	}
	next, err := Replace(source, heading, "Title续新粗 tail")
	want := []byte("# Title续新**粗** tail\r\n\r\nAfter\r\n")
	if err != nil || !bytes.Equal(next, want) {
		t.Fatalf("mixed heading patch=%q, want %q, err=%v", next, want, err)
	}
	for _, raw := range [][]byte{[]byte("Setext **bold**\n======\n"), []byte("# ***nested*** tail\n")} {
		if block := findBlock(t, Parse(raw), Heading); block.Editable {
			t.Fatalf("unsafe heading became editable: %+v", block)
		}
	}
}

func TestReplaceListItemTouchesOnlySelectedSourceRange(t *testing.T) {
	source := []byte("- first\n- second\n\n> quote\n")
	var second Block
	for _, block := range Parse(source) {
		if block.Kind == ListItem && block.Visible == "second" {
			second = block
		}
	}
	if !second.Editable {
		t.Fatal("second list item is not editable")
	}
	got, err := Replace(source, second, "第二项")
	if err != nil || string(got) != "- first\n- 第二项\n\n> quote\n" {
		t.Fatalf("list edit = %q, %v", got, err)
	}
}

func TestReplaceRejectsStaleBlockAndUnsupportedSyntax(t *testing.T) {
	source := []byte("# Original\n\n```go\ncode\n```\n")
	blocks := Parse(source)
	heading := findBlock(t, blocks, Heading)
	changed := bytes.Replace(source, []byte("Original"), []byte("Modified"), 1)
	if _, err := Replace(changed, heading, "New"); !errors.Is(err, ErrStaleBlock) {
		t.Fatalf("stale edit = %v", err)
	}
	code := findBlock(t, blocks, FencedCode)
	if _, err := Replace(source, code, "new code"); !errors.Is(err, ErrReadOnlyBlock) {
		t.Fatalf("unsupported edit = %v", err)
	}
}

func TestFencedCodeBodyPreservesBOMCRLFFenceAndNeighbors(t *testing.T) {
	source := []byte("\xef\xbb\xbf# Before\r\n\r\n```go title=\"x\"\r\npackage main\r\nprintln(\"好\")\r\n```\r\n\r\nTail\r\n")
	block := findBlock(t, Parse(source), FencedCode)
	if !block.Code.Editable || block.Editable || block.Code.Info != `go title="x"` || block.Code.Visible != "package main\nprintln(\"好\")" || string(source[block.Code.Start:block.Code.End]) != "package main\r\nprintln(\"好\")\r\n" {
		t.Fatalf("fenced code source map = %+v", block)
	}
	if unchanged, err := ReplaceCode(source, block, block.Code.Visible); err != nil || !bytes.Equal(unchanged, source) {
		t.Fatalf("no-op code edit changed bytes: %q, %v", unchanged, err)
	}
	next, err := ReplaceCode(source, block, "package main\nprintln(\"新\")\nend")
	want := bytes.Replace(source, []byte("package main\r\nprintln(\"好\")\r\n"), []byte("package main\r\nprintln(\"新\")\r\nend\r\n"), 1)
	if err != nil || !bytes.Equal(next, want) {
		t.Fatalf("code edit changed fences or neighbors: %q, %v", next, err)
	}
	if _, err := ReplaceCode(append(append([]byte(nil), source...), 'x'), block, "changed"); !errors.Is(err, ErrStaleBlock) {
		t.Fatalf("stale code edit = %v", err)
	}
}

func TestFencedCodeEmptyTildeAndDuplicateBlocks(t *testing.T) {
	empty := []byte("~~~js\n~~~\n")
	block := findBlock(t, Parse(empty), FencedCode)
	if !block.Code.Editable || block.Code.Start != block.Code.End || block.Code.Visible != "" || block.Code.Info != "js" {
		t.Fatalf("empty fenced code = %+v", block)
	}
	if next, err := ReplaceCode(empty, block, "ok"); err != nil || string(next) != "~~~js\nok\n~~~\n" {
		t.Fatalf("empty fence edit = %q, %v", next, err)
	}
	bomEmpty := []byte("\xef\xbb\xbf~~~js\r\n~~~~  \r\n")
	editorSource := bomEmpty[3:]
	block = findBlock(t, Parse(editorSource), FencedCode)
	if !block.Code.Editable || block.Code.Start != block.Code.End {
		t.Fatalf("BOM-stripped CRLF empty fence = %+v", block)
	}
	if next, err := ReplaceCode(editorSource, block, "你好\nnext"); err != nil || string(append(append([]byte(nil), bomEmpty[:3]...), next...)) != "\xef\xbb\xbf~~~js\r\n你好\r\nnext\r\n~~~~  \r\n" {
		t.Fatalf("BOM-stripped CRLF empty edit = %q, %v", next, err)
	}
	source := []byte("~~~go\nsame\n~~~\n\n~~~go\nsame\n~~~\n")
	var codes []Block
	for _, parsed := range Parse(source) {
		if parsed.Kind == FencedCode {
			codes = append(codes, parsed)
		}
	}
	if len(codes) != 2 || !codes[0].Code.Editable || !codes[1].Code.Editable || codes[0].Code.Start == codes[1].Code.Start {
		t.Fatalf("duplicate fences = %+v", codes)
	}
	next, err := ReplaceCode(source, codes[1], "second")
	if err != nil || string(next) != "~~~go\nsame\n~~~\n\n~~~go\nsecond\n~~~\n" {
		t.Fatalf("duplicate target patch = %q, %v", next, err)
	}
}

func TestFencedCodeRejectsUnclosedIndentedNestedAndClosingLine(t *testing.T) {
	for _, source := range [][]byte{
		[]byte("```go\nbody\n"),
		[]byte("  ```go\nbody\n  ```\n"),
		[]byte("> ```go\n> body\n> ```\n"),
	} {
		for _, block := range Parse(source) {
			if block.Kind == FencedCode && block.Code.Editable {
				t.Errorf("unsafe fence editable in %q: %+v", source, block)
			}
		}
	}
	source := []byte("```go\nfirst\n```\nTail\n")
	block := findBlock(t, Parse(source), FencedCode)
	if _, err := ReplaceCode(source, block, "first\n```\nlast"); !errors.Is(err, ErrReadOnlyBlock) {
		t.Fatalf("closing fence inside code accepted: %v", err)
	}
}

func TestTableCellSourceRangesAndPatchesPreserveCRLF(t *testing.T) {
	source := []byte("| Name |  Note  |\r\n| :--- | ---: |\r\n| 甲 |  |\r\n\r\nTail\r\n")
	table := findBlock(t, Parse(source), Table)
	if len(table.Cells) != 4 {
		t.Fatalf("table cells=%+v", table.Cells)
	}
	for index, want := range []struct {
		row, column int
		text        string
	}{
		{0, 0, "Name"}, {0, 1, "Note"}, {1, 0, "甲"}, {1, 1, ""},
	} {
		cell := table.Cells[index]
		if cell.Row != want.row || cell.Column != want.column || cell.Visible != want.text || !cell.Editable || cell.Start < 0 || cell.End > len(source) || cell.Start > cell.End || string(source[cell.Start:cell.End]) != want.text {
			t.Fatalf("cell %d=%+v, want %+v", index, cell, want)
		}
	}
	next, err := ReplaceCell(source, table, 0, 1, "备注")
	if err != nil || string(next) != "| Name |  备注  |\r\n| :--- | ---: |\r\n| 甲 |  |\r\n\r\nTail\r\n" {
		t.Fatalf("header patch=%q, %v", next, err)
	}
	table = findBlock(t, Parse(next), Table)
	next, err = ReplaceCell(next, table, 1, 1, "乙")
	if err != nil || string(next) != "| Name |  备注  |\r\n| :--- | ---: |\r\n| 甲 |  乙|\r\n\r\nTail\r\n" {
		t.Fatalf("empty cell patch=%q, %v", next, err)
	}
}

func TestTableCellRejectsStaleAndStructuralEdits(t *testing.T) {
	source := []byte("| A | B |\n| --- | --- |\n| one | two |\n")
	table := findBlock(t, Parse(source), Table)
	if _, err := ReplaceCell(append([]byte(nil), source[:len(source)-1]...), table, 1, 0, "new"); !errors.Is(err, ErrStaleBlock) {
		t.Fatalf("stale table cell=%v", err)
	}
	for _, value := range []string{"a|b", "line\nbreak", "**bold**", "escaped\\pipe"} {
		if _, err := ReplaceCell(source, table, 1, 0, value); !errors.Is(err, ErrReadOnlyBlock) {
			t.Errorf("structural value %q=%v", value, err)
		}
	}
	if string(source) != "| A | B |\n| --- | --- |\n| one | two |\n" {
		t.Fatal("rejected cell edit changed source")
	}
}

func TestTableCellComplexAndPaddedCellsRemainReadOnly(t *testing.T) {
	source := []byte("| A | B | C |\n| --- | --- | --- |\n| **bold** | escaped\\|pipe |\n| plain |  | done |\n")
	table := findBlock(t, Parse(source), Table)
	if len(table.Cells) != 9 {
		t.Fatalf("table cells=%+v", table.Cells)
	}
	for _, index := range []int{3, 4, 5} {
		if table.Cells[index].Editable {
			t.Fatalf("complex or parser-padded cell %d editable: %+v", index, table.Cells[index])
		}
	}
	if !table.Cells[6].Editable || !table.Cells[7].Editable || !table.Cells[8].Editable || table.Cells[7].Visible != "" {
		t.Fatalf("ordinary or physical empty cell lost: %+v", table.Cells[6:])
	}
}

func TestTableCellDuplicateTablePatchesOnlySelectedLocation(t *testing.T) {
	source := []byte("| A |\n| --- |\n| same |\n\n| A |\n| --- |\n| same |\n")
	var tables []Block
	for _, block := range Parse(source) {
		if block.Kind == Table {
			tables = append(tables, block)
		}
	}
	if len(tables) != 2 {
		t.Fatalf("tables=%+v", tables)
	}
	next, err := ReplaceCell(source, tables[1], 1, 0, "second")
	if err != nil || string(next) != "| A |\n| --- |\n| same |\n\n| A |\n| --- |\n| second |\n" {
		t.Fatalf("duplicate table patch=%q, %v", next, err)
	}
}

func TestTableCellCanClearAndRefillPhysicalCell(t *testing.T) {
	source := []byte("| A | B |\n| --- | --- |\n| one | two |\n")
	table := findBlock(t, Parse(source), Table)
	next, err := ReplaceCell(source, table, 1, 1, "")
	if err != nil || string(next) != "| A | B |\n| --- | --- |\n| one |  |\n" {
		t.Fatalf("clear cell=%q, %v", next, err)
	}
	table = findBlock(t, Parse(next), Table)
	next, err = ReplaceCell(next, table, 1, 1, "新")
	if err != nil || string(next) != "| A | B |\n| --- | --- |\n| one |  新|\n" {
		t.Fatalf("refill cell=%q, %v", next, err)
	}
}

func TestNestedOrFormattedBlocksStayReadOnlyWithoutRewriting(t *testing.T) {
	source := []byte("- **bold** text\n  - nested\n\nline with <span>raw</span>\n")
	for _, block := range Parse(source) {
		if strings.Contains(string(source[block.Start:block.End]), "**bold**") || strings.Contains(string(source[block.Start:block.End]), "<span>") {
			if block.Editable {
				t.Fatalf("formatted source unexpectedly editable: %+v", block)
			}
		}
	}
}

func TestReplaceRefusesTextThatWouldChangeMarkdownStructure(t *testing.T) {
	source := []byte("# Title\n\nplain\n\n- item\n")
	for _, test := range []struct {
		kind Kind
		text string
	}{
		{Heading, "**bold**"},
		{Paragraph, "first\nsecond"},
		{ListItem, "[link](https://example.com)"},
	} {
		block := findBlock(t, Parse(source), test.kind)
		if _, err := Replace(source, block, test.text); !errors.Is(err, ErrReadOnlyBlock) {
			t.Errorf("%s replacement %q = %v", test.kind, test.text, err)
		}
	}
}

func TestParseDoesNotExposeUnsupportedOrInvalidSourceAsEditable(t *testing.T) {
	for _, source := range [][]byte{
		[]byte("Setext\n======\n"),
		[]byte("one  \ntwo\n"),
		[]byte("<script>alert(1)</script>\n"),
		[]byte{'#', ' ', 0xff, '\n'},
	} {
		for _, block := range Parse(source) {
			if block.Editable {
				t.Errorf("unexpected editable block in %q: %+v", source, block)
			}
		}
	}
}

func TestSplitPlainTextNodesRemainEditableWithoutTouchingOtherBlocks(t *testing.T) {
	source := []byte("# Markdown 示例\r\n\r\n打开一个本地文件夹，左侧会列出其中的 Markdown 文档。\r\n\r\n**bold** stays\r\n")
	blocks := Parse(source)
	heading := findBlock(t, blocks, Heading)
	paragraph := findBlock(t, blocks, Paragraph)
	if !heading.Editable || heading.Visible != "Markdown 示例" || !paragraph.Editable || paragraph.Visible != "打开一个本地文件夹，左侧会列出其中的 Markdown 文档。" {
		t.Fatalf("ordinary text became read-only: heading=%+v paragraph=%+v", heading, paragraph)
	}
	next, err := Replace(source, paragraph, "新内容 有空格。")
	if err != nil {
		t.Fatal(err)
	}
	if want := "# Markdown 示例\r\n\r\n新内容 有空格。\r\n\r\n**bold** stays\r\n"; string(next) != want {
		t.Fatalf("plain paragraph patch = %q", next)
	}
	if block := findBlock(t, Parse(next), Paragraph); !block.Editable {
		t.Fatal("new plain paragraph cannot be edited again")
	}
}

func TestSingleBoldAndLinkLabelPatchPreservesMarkup(t *testing.T) {
	source := []byte("\xef\xbb\xbf# Title\r\n\r\n**bold**\r\n\r\n[label](https://example.com/path)\r\n\r\nplain **mixed**\r\n")
	blocks := Parse(source)
	var bold, link, mixed Block
	for _, block := range blocks {
		switch block.Visible {
		case "bold":
			bold = block
		case "label":
			link = block
		default:
			if bytes.Contains(source[block.Start:block.End], []byte("mixed")) {
				mixed = block
			}
		}
	}
	if !bold.Editable || !link.Editable || !mixed.Editable || mixed.Visible != "plain mixed" {
		t.Fatalf("single styled nodes or mixed block permissions: bold=%+v link=%+v mixed=%+v", bold, link, mixed)
	}
	next, err := Replace(source, bold, "新字")
	if err != nil || string(next) != "\xef\xbb\xbf# Title\r\n\r\n**新字**\r\n\r\n[label](https://example.com/path)\r\n\r\nplain **mixed**\r\n" {
		t.Fatalf("bold patch = %q, err=%v", next, err)
	}
	for _, block := range Parse(next) {
		if block.Visible == "label" {
			link = block
		}
	}
	next, err = Replace(next, link, "新链接")
	if err != nil || string(next) != "\xef\xbb\xbf# Title\r\n\r\n**新字**\r\n\r\n[新链接](https://example.com/path)\r\n\r\nplain **mixed**\r\n" {
		t.Fatalf("link label patch = %q, err=%v", next, err)
	}
}

func TestStyledLabelRejectsStructuralInputWithoutChangingSource(t *testing.T) {
	source := []byte("[label](https://example.com)\n\nTail\n")
	block := findBlock(t, Parse(source), Paragraph)
	if !block.Editable {
		t.Fatal("single link label unexpectedly read-only")
	}
	if _, err := Replace(source, block, "bad](/other)"); !errors.Is(err, ErrReadOnlyBlock) {
		t.Fatalf("structural link label accepted: %v", err)
	}
	if string(source) != "[label](https://example.com)\n\nTail\n" {
		t.Fatal("source changed after rejected inline edit")
	}
}

func TestSingleItalicSourcePatchRetainsDelimiters(t *testing.T) {
	for _, source := range []string{"*italic*\n", "_italic_\n"} {
		block := findBlock(t, Parse([]byte(source)), Paragraph)
		if !block.Editable || block.Visible != "italic" {
			t.Fatalf("italic block = %+v", block)
		}
		next, err := Replace([]byte(source), block, "中文")
		want := strings.Replace(source, "italic", "中文", 1)
		if err != nil || string(next) != want {
			t.Fatalf("italic patch = %q, want %q, err=%v", next, want, err)
		}
	}
}

func TestMixedInlineEditsPatchOnlyOneLiteralSegment(t *testing.T) {
	source := []byte("before **bold** after\r\n\r\n[link](https://example.com) tail\r\n")
	blocks := Parse(source)
	if len(blocks) != 2 || !blocks[0].Editable || blocks[0].Visible != "before bold after" || !blocks[1].Editable || blocks[1].Visible != "link tail" {
		t.Fatalf("mixed paragraph map = %+v", blocks)
	}
	next, err := Replace(source, blocks[0], "before boldX after")
	if err != nil || string(next) != "before **boldX** after\r\n\r\n[link](https://example.com) tail\r\n" {
		t.Fatalf("bold leaf edit = %q, err=%v", next, err)
	}
	blocks = Parse(next)
	next, err = Replace(next, blocks[1], "link tail!")
	if err != nil || string(next) != "before **boldX** after\r\n\r\n[link](https://example.com) tail!\r\n" {
		t.Fatalf("plain tail edit = %q, err=%v", next, err)
	}
	whole, err := Replace(next, Parse(next)[0], "new whole paragraph")
	if err != nil || string(whole) != "new whole paragraph\r\n\r\n[link](https://example.com) tail!\r\n" {
		t.Fatalf("whole mixed replacement=%q, error=%v", whole, err)
	}
}

func TestMixedCrossLeafRawPatchRefusesToFlattenUntouchedLink(t *testing.T) {
	source := []byte("before **bold** after *italic* [link](https://example.invalid/preserved) tail\n")
	block := findBlock(t, Parse(source), Paragraph)
	if !block.Editable {
		t.Fatal("mixed fixture is not editable")
	}
	next, err := PatchRaw(source, block, "before Xic link tail")
	if !errors.Is(err, ErrReadOnlyBlock) || next != nil {
		t.Fatalf("cross-leaf patch = %q, err=%v", next, err)
	}
	if string(source) != "before **bold** after *italic* [link](https://example.invalid/preserved) tail\n" {
		t.Fatalf("rejected patch changed original source: %q", source)
	}
}

func TestMixedInlineEditableRangeStaysWithinOneLeaf(t *testing.T) {
	block := findBlock(t, Parse([]byte("before **bold** after *italic* [link](https://example.invalid) tail\n")), Paragraph)
	for _, test := range []struct {
		start, end int
		want       bool
	}{
		{7, 9, true},
		{7, 11, true},
		{7, 12, false},
		{7, 25, false},
		{0, 6, true},
	} {
		if got := EditableRange(block, test.start, test.end); got != test.want {
			t.Errorf("EditableRange(%d,%d)=%v, want %v", test.start, test.end, got, test.want)
		}
	}
}

func TestReplaceSelectionAcrossSimpleStylesPreservesUntouchedLink(t *testing.T) {
	source := []byte("before **bold** after *italicword* [link](https://example.invalid/preserved) tail\n")
	block := findBlock(t, Parse(source), Paragraph)
	start := strings.Index(block.Visible, "bold")
	end := strings.Index(block.Visible, "italicword") + len("italic")
	next, err := ReplaceSelection(source, block, start, end, "X")
	want := "before X*word* [link](https://example.invalid/preserved) tail\n"
	if err != nil || string(next) != want {
		t.Fatalf("cross-style replacement=%q, err=%v, want=%q", next, err, want)
	}
	visible := block.Visible[:start] + "X" + block.Visible[end:]
	viaVisible, err := Replace(source, block, visible)
	if err != nil || string(viaVisible) != want {
		t.Fatalf("visible replacement=%q, err=%v, want=%q", viaVisible, err, want)
	}
	if string(source) != "before **bold** after *italicword* [link](https://example.invalid/preserved) tail\n" {
		t.Fatal("source was modified in place")
	}
}

func TestCrossBlockBoundaryMapsWholeStyleAndPlainSuffix(t *testing.T) {
	source := []byte("**加粗** 后面 [链接](https://example.invalid/keep) 尾段\r\n")
	block := findBlock(t, Parse(source), Paragraph)
	if !block.Editable {
		t.Fatal("mixed paragraph was not editable")
	}
	for _, testcase := range []struct {
		name   string
		visual int
		end    bool
		want   int
	}{
		{"start of whole paragraph", 0, false, block.Start},
		{"end of bold group", len("加粗"), true, bytes.Index(source, []byte(" 后面"))},
		{"plain suffix", len("加粗 后"), true, bytes.Index(source, []byte("后面")) + len("后")},
		{"end of whole paragraph", len(block.Visible), true, block.End},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			got, err := CrossBlockBoundary(source, block, testcase.visual, testcase.end)
			if err != nil || got != testcase.want {
				t.Fatalf("boundary=%d, err=%v, want %d", got, err, testcase.want)
			}
		})
	}
}

func TestCrossBlockBoundaryRejectsPartialStyleAndStaleSource(t *testing.T) {
	source := []byte("**加粗** 后面 [链接](https://example.invalid/keep) 尾段\n")
	block := findBlock(t, Parse(source), Paragraph)
	for _, visual := range []int{len("加"), strings.Index(block.Visible, "链接") + len("链")} {
		if _, err := CrossBlockBoundary(source, block, visual, true); !errors.Is(err, ErrReadOnlyBlock) {
			t.Fatalf("partial style boundary %d: %v", visual, err)
		}
	}
	stale := append([]byte(nil), source...)
	stale[0] = '!'
	if _, err := CrossBlockBoundary(stale, block, len("加粗 后"), true); !errors.Is(err, ErrStaleBlock) {
		t.Fatalf("stale boundary: %v", err)
	}
}

func TestCrossBlockBoundaryMapsWholeSingleStyle(t *testing.T) {
	source := []byte("# Title\r\n\r\n**bold**\r\n")
	block := findBlock(t, Parse(source), Paragraph)
	for _, testcase := range []struct {
		visual int
		end    bool
		want   int
	}{{0, false, block.Start}, {len(block.Visible), true, block.End}} {
		got, err := CrossBlockBoundary(source, block, testcase.visual, testcase.end)
		if err != nil || got != testcase.want {
			t.Fatalf("single style boundary=%d, err=%v, want %d", got, err, testcase.want)
		}
	}
	if _, err := CrossBlockBoundary(source, block, 2, true); !errors.Is(err, ErrReadOnlyBlock) {
		t.Fatalf("partial single style: %v", err)
	}
}

func TestCrossBlockEdgeKeepsPartialBoldSuffixAndPrefix(t *testing.T) {
	for _, style := range []struct {
		raw, text, marker string
	}{
		{"**bold** tail\n", "bold", "**"},
		{"_italic_ tail\n", "italic", "_"},
		{"~~strike~~ tail\n", "strike", "~~"},
	} {
		t.Run(style.text, func(t *testing.T) {
			source := []byte(style.raw)
			block := findBlock(t, Parse(source), Paragraph)
			content := bytes.Index(source, []byte(style.text))
			for _, selectedEnd := range []bool{false, true} {
				edge, err := CrossBlockEdgeAt(source, block, 2, selectedEnd)
				if err != nil || edge.Offset != content+2 || edge.Retain != style.marker {
					t.Fatalf("selectedEnd=%v edge=%+v, err=%v", selectedEnd, edge, err)
				}
			}
			if _, err := CrossBlockBoundary(source, block, 2, true); !errors.Is(err, ErrReadOnlyBlock) {
				t.Fatalf("raw boundary ignored retained marker: %v", err)
			}
		})
	}
}

func TestCrossBlockEdgePreservesPartialSingleBold(t *testing.T) {
	source := []byte("**bold**\n")
	block := findBlock(t, Parse(source), Paragraph)
	for _, selectedEnd := range []bool{false, true} {
		edge, err := CrossBlockEdgeAt(source, block, 2, selectedEnd)
		if err != nil || edge.Offset != len("**bo") || edge.Retain != "**" {
			t.Fatalf("selectedEnd=%v edge=%+v, err=%v", selectedEnd, edge, err)
		}
	}
}

func TestCrossBlockEdgeRejectsPartialLinkAndNestedStyle(t *testing.T) {
	for _, value := range []string{"[link](https://example.invalid/keep) tail\n", "***nested*** tail\n"} {
		source := []byte(value)
		block := findBlock(t, Parse(source), Paragraph)
		if _, err := CrossBlockEdgeAt(source, block, len("li"), true); !errors.Is(err, ErrReadOnlyBlock) {
			t.Fatalf("partial unsupported style %q: %v", value, err)
		}
	}
}

func TestReplaceSelectionAcrossStylesKeepsUnselectedStyledPrefix(t *testing.T) {
	source := []byte("before **bold** after *italicword* [link](https://example.invalid/preserved) tail\n")
	block := findBlock(t, Parse(source), Paragraph)
	start := strings.Index(block.Visible, "bold") + len("bo")
	end := strings.Index(block.Visible, "italicword") + len("italic")
	next, err := ReplaceSelection(source, block, start, end, "X")
	want := "before **bo**X*word* [link](https://example.invalid/preserved) tail\n"
	if err != nil || string(next) != want {
		t.Fatalf("partial cross-style replacement=%q, err=%v, want=%q", next, err, want)
	}
}

func TestReplaceSelectionRefusesUnsupportedLinkOrNestedStyles(t *testing.T) {
	for _, value := range []string{
		"before **bold** [link](https://example.invalid/preserved) tail\n",
		"before ***nested*** after\n",
	} {
		source := []byte(value)
		block := findBlock(t, Parse(source), Paragraph)
		if !block.Editable {
			if !strings.Contains(value, "***nested***") {
				t.Fatalf("unsupported link fixture was not parsed as an editable mixed block: %+v", block)
			}
			continue
		}
		if next, err := ReplaceSelection(source, block, len("before "), len(block.Visible)-len(" tail"), "X"); !errors.Is(err, ErrReadOnlyBlock) || next != nil {
			t.Fatalf("unsafe selection in %q = %q, err=%v", source, next, err)
		}
		if string(source) != value {
			t.Fatalf("unsafe selection changed %q", source)
		}
	}
}

func TestReplaceSelectionRefusesBinaryOrMultilineInsertion(t *testing.T) {
	source := []byte("before **bold** after *italicword* tail\n")
	block := findBlock(t, Parse(source), Paragraph)
	start := strings.Index(block.Visible, "bold")
	end := strings.Index(block.Visible, "italicword") + len("italic")
	for _, replacement := range []string{"\x00", "x\ny", "x\ry", "\xff"} {
		if next, err := ReplaceSelection(source, block, start, end, replacement); !errors.Is(err, ErrReadOnlyBlock) || next != nil {
			t.Errorf("unsafe replacement %q = %q, err=%v", replacement, next, err)
		}
	}
}

func TestReplaceSelectionPreservesRemainingMarkerSpelling(t *testing.T) {
	for _, test := range []struct {
		source, first, last, replacement, want string
		selectedLast                           string
	}{
		{
			source: "left __bold__ and _italicword_ [link](https://example.invalid/keep)\n",
			first:  "bold", last: "italicword", selectedLast: "italic", replacement: "X ",
			want: "left X _word_ [link](https://example.invalid/keep)\n",
		},
		{
			source: "left ~~strike~~ and **boldword** [link](https://example.invalid/keep)\n",
			first:  "strike", last: "boldword", selectedLast: "bold", replacement: "X",
			want: "left X**word** [link](https://example.invalid/keep)\n",
		},
	} {
		source := []byte(test.source)
		block := findBlock(t, Parse(source), Paragraph)
		start := strings.Index(block.Visible, test.first)
		end := strings.Index(block.Visible, test.last) + len(test.selectedLast)
		next, err := ReplaceSelection(source, block, start, end, test.replacement)
		if err != nil || string(next) != test.want {
			t.Errorf("source=%q, replacement=%q, err=%v, want=%q", source, next, err, test.want)
		}
		if strings.Contains(test.source, "_italicword_") {
			if unsafe, err := ReplaceSelection(source, block, start, end, "X"); !errors.Is(err, ErrReadOnlyBlock) || unsafe != nil {
				t.Errorf("adjacent underscore marker was silently rewritten: %q, err=%v", unsafe, err)
			}
		}
	}
}

func TestToggleInlineFormatsOneVisibleSourceSegment(t *testing.T) {
	source := []byte("before after\n\nTail\n")
	block := findBlock(t, Parse(source), Paragraph)
	next, err := ToggleInline(source, block, 7, 12, "**")
	if err != nil || string(next) != "before **after**\n\nTail\n" {
		t.Fatalf("bold selected text = %q, err=%v", next, err)
	}
	block = findBlock(t, Parse(next), Paragraph)
	next, err = ToggleInline(next, block, 7, 12, "**")
	if err != nil || string(next) != "before after\n\nTail\n" {
		t.Fatalf("toggle off selected bold = %q, err=%v", next, err)
	}
	next, err = ToggleInline(next, findBlock(t, Parse(next), Paragraph), 7, 12, "*")
	if err != nil || string(next) != "before *after*\n\nTail\n" {
		t.Fatalf("italic selected text = %q, err=%v", next, err)
	}
}

func TestToggleInlineRejectsCrossStyleSelection(t *testing.T) {
	source := []byte("before **bold** after\n")
	block := findBlock(t, Parse(source), Paragraph)
	if _, err := ToggleInline(source, block, 5, 12, "**"); !errors.Is(err, ErrReadOnlyBlock) {
		t.Fatalf("cross-style selection = %v", err)
	}
	if string(source) != "before **bold** after\n" {
		t.Fatal("rejected selection changed original source")
	}
}

func TestLinkSelectionPreservesAdjacentStyledSource(t *testing.T) {
	source := []byte("before **bold** after\r\n\r\nTail\r\n")
	block := findBlock(t, Parse(source), Paragraph)
	next, err := LinkSelection(source, block, 12, 17, "https://example.com/doc")
	if err != nil || string(next) != "before **bold** [after](https://example.com/doc)\r\n\r\nTail\r\n" {
		t.Fatalf("link selection = %q, err=%v", next, err)
	}
	if _, err := LinkSelection(source, block, 5, 12, "https://example.com/doc"); !errors.Is(err, ErrReadOnlyBlock) {
		t.Fatalf("cross-style link selection = %v", err)
	}
	for _, destination := range []string{"javascript:alert(1)", "file:///tmp/secrets", "https://example.com/path with spaces", "//example.com/path"} {
		if _, err := LinkSelection(source, block, 12, 17, destination); !errors.Is(err, ErrReadOnlyBlock) {
			t.Errorf("unsafe destination %q = %v", destination, err)
		}
	}
}

func TestSingleStrikethroughAndSelectedStrikethroughPreserveSource(t *testing.T) {
	source := []byte("~~old~~\n\nTail\n")
	block := findBlock(t, Parse(source), Paragraph)
	if !block.Editable || block.Visible != "old" {
		t.Fatalf("strikethrough block = %+v", block)
	}
	next, err := Replace(source, block, "new")
	if err != nil || string(next) != "~~new~~\n\nTail\n" {
		t.Fatalf("strike patch = %q, err=%v", next, err)
	}
	source = []byte("before after\n")
	block = findBlock(t, Parse(source), Paragraph)
	next, err = ToggleInline(source, block, 7, 12, "~~")
	if err != nil || string(next) != "before ~~after~~\n" {
		t.Fatalf("selected strike = %q, err=%v", next, err)
	}
}

func TestTaskListLabelAndCheckboxPatchOnlyTheirSourceRanges(t *testing.T) {
	source := []byte("- [ ] Task\r\n- [x] Done\r\n")
	blocks := Parse(source)
	if len(blocks) != 2 || !blocks[0].Task || blocks[0].Checked || !blocks[0].Editable || blocks[0].Visible != "Task" || !blocks[1].Task || !blocks[1].Checked || !blocks[1].Editable || blocks[1].Visible != "Done" {
		t.Fatalf("task blocks = %+v", blocks)
	}
	next, err := Replace(source, blocks[0], "新任务")
	if err != nil || string(next) != "- [ ] 新任务\r\n- [x] Done\r\n" {
		t.Fatalf("task label edit = %q, err=%v", next, err)
	}
	blocks = Parse(next)
	next, err = ToggleTask(next, blocks[0])
	if err != nil || string(next) != "- [x] 新任务\r\n- [x] Done\r\n" {
		t.Fatalf("task toggle = %q, err=%v", next, err)
	}
	if _, err := ToggleTask(source, blocks[0]); !errors.Is(err, ErrStaleBlock) {
		t.Fatalf("stale task marker = %v", err)
	}
}

func TestTaskPlainUnicodeLabelRemainsEditable(test *testing.T) {
	for _, label := range []string{"\u4e2d", "\U0001f469\u200d\U0001f4bb", "e\u0301", "\u4e2d \U0001f469\u200d\U0001f4bb e\u0301Z"} {
		test.Run(label, func(context *testing.T) {
			source := []byte("- [ ] " + label + "\r\n")
			blocks := Parse(source)
			if len(blocks) != 1 || !blocks[0].Task || !blocks[0].Editable || blocks[0].Visible != label {
				context.Fatalf("plain Unicode task label was rejected: %+v", blocks)
			}
			next, err := Replace(source, blocks[0], label+"\u65b0")
			if err != nil || string(next) != "- [ ] "+label+"\u65b0\r\n" {
				context.Fatalf("Unicode task edit=%q, error=%v", next, err)
			}
			next, err = ToggleTask(source, blocks[0])
			if err != nil || string(next) != "- [x] "+label+"\r\n" {
				context.Fatalf("Unicode task toggle=%q, error=%v", next, err)
			}
		})
	}
}

func TestTaskStyledLabelStillRequiresSource(test *testing.T) {
	for _, label := range []string{"before **bold**", "[link](https://example.invalid)", "before `code`", "a &amp; b", "a \\ b"} {
		test.Run(label, func(context *testing.T) {
			blocks := Parse([]byte("- [ ] " + label + "\n"))
			if len(blocks) != 1 || blocks[0].Editable || blocks[0].Task {
				context.Fatalf("unsafe task label became editable: %+v", blocks)
			}
		})
	}
}
