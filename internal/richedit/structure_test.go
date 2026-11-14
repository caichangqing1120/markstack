package richedit

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRichHTMLSupportMatrixOperationsKeepUntouchedSource drives one supported
// operation per declared support class (leaf, entity/attribute, style boundary,
// paragraph/list split and merge, simple table last-cell Tab, image, unknown
// node) and compares the whole result, so untouched attributes, entity
// spellings and comments must survive byte for byte.
func TestRichHTMLSupportMatrixOperationsKeepUntouchedSource(t *testing.T) {
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
	image := "assets/" + strings.Repeat("a", 64) + ".png"
	for _, test := range []struct {
		name           string
		fixture        string
		source         string
		keep           []string
		run            func(t *testing.T, source []byte) ([]byte, error)
		want           string
		wantFromSource func([]byte) string
		wantErr        error
	}{
		{
			name: "real MkDown leaf edit keeps entity spelling in the other paragraph", fixture: "Real-MkDown.mh",
			keep: []string{"<strong>真实码克样本</strong>", "正文 A & 新"},
			run: func(t *testing.T, source []byte) ([]byte, error) {
				return ReplaceLeaf(source, Parse(source).Blocks[1].Leaves[0], "正文 A & 新")
			},
			want: "<p><strong>真实码克样本</strong></p><p>正文 A & 新</p>",
		},
		{
			name: "real MkDown table cell selection keeps canonical attributes", fixture: "Real-MkDown-Structure.mh",
			keep: []string{`<td colSpan="1" rowSpan="1" width="auto">值 & 新</td>`, "<p><br></p>"},
			run: func(t *testing.T, source []byte) ([]byte, error) {
				block := Parse(source).Blocks[7]
				return ReplaceSelection(source, block, len("值 & "), len("值 & 保留"), "新")
			},
			wantFromSource: func(source []byte) string {
				return strings.Replace(string(source), `<td colSpan="1" rowSpan="1" width="auto">值 & 保留</td>`, `<td colSpan="1" rowSpan="1" width="auto">值 & 新</td>`, 1)
			},
		},
		{
			name: "real MkDown paragraph split keeps the styled neighbor", fixture: "Real-MkDown.mh",
			keep: []string{"<strong>真实</strong>", "<strong>码克样本</strong>"},
			run: func(t *testing.T, source []byte) ([]byte, error) {
				return SplitParagraph(source, Parse(source).Blocks[0], 6, "\n")
			},
			want: "<p><strong>真实</strong></p>\n<p><strong>码克样本</strong></p><p>正文 A & B</p>",
		},
		{
			name: "real MkDown paragraph split then merge restores every byte", fixture: "Real-MkDown.mh",
			run: func(t *testing.T, source []byte) ([]byte, error) {
				split, err := SplitParagraph(source, Parse(source).Blocks[0], 6, "\n")
				if err != nil {
					return nil, err
				}
				return MergeParagraphs(split, Parse(split), 0, 1)
			},
			wantFromSource: func(source []byte) string { return string(source) },
		},
		{
			name: "real MkDown list split keeps ordered siblings", fixture: "Real-MkDown-Structure.mh",
			keep: []string{"<li>第二项</li>", "<li></li>"},
			run: func(t *testing.T, source []byte) ([]byte, error) {
				return SplitListItem(source, Parse(source).Blocks[1], 6, "\n")
			},
			wantFromSource: func(source []byte) string {
				return strings.Replace(string(source), "<li>第一项</li>", "<li>第一</li>\n<li>项</li>", 1)
			},
		},
		{
			name: "real MkDown list split then merge restores every byte", fixture: "Real-MkDown-Structure.mh",
			run: func(t *testing.T, source []byte) ([]byte, error) {
				split, err := SplitListItem(source, Parse(source).Blocks[1], 6, "\n")
				if err != nil {
					return nil, err
				}
				return MergeListItems(split, Parse(split), 1, 2)
			},
			wantFromSource: func(source []byte) string { return string(source) },
		},
		{
			name: "real MkDown last table cell Tab adds only canonical single-span cells", fixture: "Real-MkDown-Structure.mh",
			keep: []string{`colSpan="1"`, `rowSpan="1"`, `width="auto"`, "<p><br></p>"},
			run: func(t *testing.T, source []byte) ([]byte, error) {
				next, _, err := InsertTableRow(source, Parse(source), 7, "\n")
				return next, err
			},
			wantFromSource: func(source []byte) string {
				return strings.Replace(string(source), "</tr></tbody></table>", "</tr>\n<tr><td colSpan=\"1\" rowSpan=\"1\" width=\"auto\"><br></td><td colSpan=\"1\" rowSpan=\"1\" width=\"auto\"><br></td></tr></tbody></table>", 1)
			},
		},
		{
			name:   "image insertion keeps entity comments and unknown tail",
			source: `<!--pre keep--><p class='note'>A &amp; B</p><!--post keep--><x-unknown>raw</x-unknown>`,
			keep:   []string{"<!--pre keep-->", "<!--post keep-->", "A &amp;", "<x-unknown>raw</x-unknown>"},
			run: func(t *testing.T, source []byte) ([]byte, error) {
				leaf := leafWithText(t, Parse(source), "A & B")
				return InsertImage(source, leaf, len("A & "), image)
			},
			want: `<!--pre keep--><p class='note'>A &amp; <img src="` + image + `" alt="">B</p><!--post keep--><x-unknown>raw</x-unknown>`,
		},
		{
			name:   "selection crossing style boundaries keeps wrapping markup",
			source: `<p>前<strong>粗体</strong><s>删除</s>后</p>`,
			keep:   []string{"<strong>", "</strong>", "<s>", "</s>"},
			run: func(t *testing.T, source []byte) ([]byte, error) {
				return ReplaceSelection(source, Parse(source).Blocks[0], len("前粗"), len("前粗体删"), "新")
			},
			want: `<p>前<strong>粗新</strong><s>除</s>后</p>`,
		},
		{
			name:   "unsafe attribute leaf stays read-only",
			source: `<p onclick="never()">unsafe</p>`,
			run: func(t *testing.T, source []byte) ([]byte, error) {
				return ReplaceLeaf(source, leafWithText(t, Parse(source), "unsafe"), "changed")
			},
			wantErr: ErrReadOnlyLeaf,
		},
		{
			name:   "complex merged table refuses a structural row insertion",
			source: `<table><tbody><tr><td colspan="2">A</td></tr></tbody></table>`,
			run: func(t *testing.T, source []byte) ([]byte, error) {
				next, _, err := InsertTableRow(source, Parse(source), 0, "\n")
				return next, err
			},
			wantErr: ErrReadOnlyLeaf,
		},
		{
			name:   "cross read-only island block selection is refused",
			source: `<p>first</p><x-unknown>keep</x-unknown><p>second</p>`,
			run: func(t *testing.T, source []byte) ([]byte, error) {
				document := Parse(source)
				return ReplaceBlockSelection(source, document, 0, 1, 2, 1, "X")
			},
			wantErr: ErrReadOnlyLeaf,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := load(t, test.fixture, test.source)
			result, err := test.run(t, source)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("error = %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := test.want
			if test.wantFromSource != nil {
				want = test.wantFromSource(source)
			}
			if string(result) != want {
				t.Fatalf("result = %q, want %q", result, want)
			}
			for _, marker := range test.keep {
				if !strings.Contains(want, marker) {
					t.Fatalf("untouched marker %q is missing from %q", marker, want)
				}
			}
		})
	}
}

func TestSplitParagraphPreservesBOMCRLFEntitiesAndNeighbors(t *testing.T) {
	source := append([]byte{0xef, 0xbb, 0xbf}, []byte("<p class='keep'>A &amp; Body</p>\r\n<!--tail-->\r\n")...)
	block := Parse(source).Blocks[0]
	result, err := SplitParagraph(source, block, len("A &"), "\r\n")
	want := append([]byte{0xef, 0xbb, 0xbf}, []byte("<p class='keep'>A &amp;</p>\r\n<p> Body</p>\r\n<!--tail-->\r\n")...)
	if err != nil || !bytes.Equal(result, want) {
		t.Fatalf("paragraph split=%q, %v", result, err)
	}
	parsed := Parse(result)
	if len(parsed.Blocks) != 2 || parsed.Blocks[0].Visible != "A &" || parsed.Blocks[1].Visible != " Body" {
		t.Fatalf("split blocks=%+v", parsed.Blocks)
	}
	if _, err := SplitParagraph(result, block, 0, "\r\n"); !errors.Is(err, ErrStaleLeaf) {
		t.Fatalf("stale split accepted: %v", err)
	}
}

func TestSplitParagraphCreatesEditableEmptyBlocks(t *testing.T) {
	for _, test := range []struct {
		source, want string
		caret        int
	}{
		{"<p>Body</p>", "<p>Body</p>\n<p><br></p>", len("Body")},
		{"<p>Body</p>", "<p><br></p>\n<p>Body</p>", 0},
		{"<p><br></p>", "<p><br></p>\n<p><br></p>", 0},
	} {
		source := []byte(test.source)
		result, err := SplitParagraph(source, Parse(source).Blocks[0], test.caret, "\n")
		if err != nil || string(result) != test.want {
			t.Errorf("split %q at %d = %q, %v; want %q", source, test.caret, result, err, test.want)
		}
		for _, block := range Parse(result).Blocks {
			if block.ReadOnly || len(block.Leaves) != 1 || !block.Leaves[0].Editable {
				t.Fatalf("split produced noneditable block: %+v", block)
			}
		}
	}
}

func TestSplitParagraphRejectsAmbiguousMarkupAndRuneBoundary(t *testing.T) {
	for _, source := range []string{
		"<p>before<!--hidden-->after</p>",
		"<p onclick='run()'>unsafe</p>",
		"<p><a href='javascript:alert(1)'>unsafe</a></p>",
	} {
		block := Parse([]byte(source)).Blocks[0]
		if _, err := SplitParagraph([]byte(source), block, 1, "\n"); !errors.Is(err, ErrReadOnlyLeaf) {
			t.Errorf("ambiguous split %q = %v", source, err)
		}
	}
	source := []byte("<p>中文</p>")
	if _, err := SplitParagraph(source, Parse(source).Blocks[0], 1, "\n"); !errors.Is(err, ErrReadOnlyLeaf) {
		t.Fatalf("split inside UTF-8 rune=%v", err)
	}
}
