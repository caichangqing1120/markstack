package richedit

import (
	"bytes"
	"errors"
	"testing"
)

func TestSplitStyledParagraphPreservesBOMCRLFAttributesAndUnknownTail(t *testing.T) {
	source := append([]byte{0xef, 0xbb, 0xbf}, []byte("<p data-k='a'>前文 <strong class='keep'>粗体</strong> 后文</p>\r\n<!--tail-->\r\n")...)
	block := Parse(source).Blocks[0]
	if !CanSplitParagraph(block) {
		t.Fatal("styled paragraph was not considered a safe split candidate")
	}
	result, err := SplitParagraph(source, block, len("前文 粗"), "\r\n")
	want := append([]byte{0xef, 0xbb, 0xbf}, []byte("<p data-k='a'>前文 <strong class='keep'>粗</strong></p>\r\n<p><strong class='keep'>体</strong> 后文</p>\r\n<!--tail-->\r\n")...)
	if err != nil || !bytes.Equal(result, want) {
		t.Fatalf("styled split=%q, %v; want %q", result, err, want)
	}
	if _, err := SplitParagraph(result, block, len("前文 粗"), "\r\n"); !errors.Is(err, ErrStaleLeaf) {
		t.Fatalf("stale styled split=%v", err)
	}
}

func TestSplitNestedEmphasisAndSafeLinkPreservesWrappers(t *testing.T) {
	for _, test := range []struct {
		source, want string
		caret        int
	}{
		{`<p><strong><em>abcd</em></strong></p>`, "<p><strong><em>ab</em></strong></p>\n<p><strong><em>cd</em></strong></p>", 2},
		{`<p><a href='https://example.org?a=1&amp;b=2'>Link</a></p>`, "<p><a href='https://example.org?a=1&amp;b=2'>Li</a></p>\n<p><a href='https://example.org?a=1&amp;b=2'>nk</a></p>", 2},
	} {
		result, err := SplitParagraph([]byte(test.source), Parse([]byte(test.source)).Blocks[0], test.caret, "\n")
		if err != nil || string(result) != test.want {
			t.Errorf("split %q=%q, %v; want %q", test.source, result, err, test.want)
		}
	}
}

func TestSplitStyledParagraphKeepsUnchangedEntitySpelling(t *testing.T) {
	source := []byte(`<p>A &amp; <strong>bold</strong> tail</p>`)
	result, err := SplitParagraph(source, Parse(source).Blocks[0], len("A & bo"), "\n")
	want := "<p>A &amp; <strong>bo</strong></p>\n<p><strong>ld</strong> tail</p>"
	if err != nil || string(result) != want {
		t.Fatalf("styled entity split=%q, %v; want %q", result, err, want)
	}
}

func TestSplitStyledParagraphRejectsCommentUnsafeLinkAndEntityInterior(t *testing.T) {
	for _, source := range []string{
		`<p>before<!--hidden-->after</p>`,
		`<p><a href='javascript:alert(1)'>unsafe</a></p>`,
		`<p><strong onclick='bad()'>unsafe</strong></p>`,
		`<p><x-unknown>island</x-unknown></p>`,
	} {
		block := Parse([]byte(source)).Blocks[0]
		if _, err := SplitParagraph([]byte(source), block, 2, "\n"); !errors.Is(err, ErrReadOnlyLeaf) {
			t.Errorf("unsafe split %q=%v", source, err)
		}
	}
	source := []byte(`<p><strong>&NotEqualTilde;</strong></p>`)
	if _, err := SplitParagraph(source, Parse(source).Blocks[0], len("≂"), "\n"); !errors.Is(err, ErrReadOnlyLeaf) {
		t.Fatalf("split inside two-codepoint entity=%v", err)
	}
}

func TestSplitStyledParagraphAtLeafBoundariesAvoidsEmptyWrappers(t *testing.T) {
	for _, test := range []struct {
		source, want string
		caret        int
	}{
		{`<p>before <strong>bold</strong> after</p>`, "<p>before </p>\n<p><strong>bold</strong> after</p>", len("before ")},
		{`<p>before <strong>bold</strong> after</p>`, "<p>before <strong>bold</strong></p>\n<p> after</p>", len("before bold")},
		{`<p><strong>bold</strong><em>italic</em></p>`, "<p><strong>bold</strong></p>\n<p><em>italic</em></p>", len("bold")},
		{`<p><strong>bold</strong></p>`, "<p><br></p>\n<p><strong>bold</strong></p>", 0},
		{`<p><strong>bold</strong></p>`, "<p><strong>bold</strong></p>\n<p><br></p>", len("bold")},
	} {
		source := []byte(test.source)
		result, err := SplitParagraph(source, Parse(source).Blocks[0], test.caret, "\n")
		if err != nil || string(result) != test.want {
			t.Errorf("boundary %q at %d = %q, %v; want %q", test.source, test.caret, result, err, test.want)
		}
	}
}

func TestSplitStyledParagraphRejectsCommentInBoundaryGap(t *testing.T) {
	source := []byte(`<p>before<!--keep--><strong>bold</strong></p>`)
	if _, err := SplitParagraph(source, Parse(source).Blocks[0], len("before"), "\n"); !errors.Is(err, ErrReadOnlyLeaf) {
		t.Fatalf("comment boundary split=%v", err)
	}
}
