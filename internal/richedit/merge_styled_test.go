package richedit

import (
	"bytes"
	"errors"
	"testing"
)

func TestMergeStyledParagraphsPreservesWrappersAndUnknownTail(t *testing.T) {
	source := append([]byte{0xef, 0xbb, 0xbf}, []byte("<p data-id='keep'>before <strong>bo</strong></p>\r\n<p><strong>ld</strong> after</p>\r\n<!--tail-->\r\n")...)
	parsed := Parse(source)
	result, err := MergeParagraphs(source, parsed, 0, 1)
	want := append([]byte{0xef, 0xbb, 0xbf}, []byte("<p data-id='keep'>before <strong>bold</strong> after</p>\r\n<!--tail-->\r\n")...)
	if err != nil || !bytes.Equal(result, want) {
		t.Fatalf("styled merge=%q, %v; want %q", result, err, want)
	}
	if _, err := MergeParagraphs(result, parsed, 0, 1); !errors.Is(err, ErrStaleLeaf) {
		t.Fatalf("stale styled merge=%v", err)
	}
}

func TestMergeStyledParagraphsExactlyReversesSplit(t *testing.T) {
	for _, original := range []string{
		`<p>before <strong>bold</strong> after</p>`,
		`<p><strong><em>abcd</em></strong></p>`,
		`<p><a href='https://example.org?a=1&amp;b=2'>Link</a></p>`,
	} {
		caret := 2
		if original == `<p>before <strong>bold</strong> after</p>` {
			caret = len("before bo")
		}
		source := []byte(original)
		split, err := SplitParagraph(source, Parse(source).Blocks[0], caret, "\n")
		if err != nil {
			t.Fatal(err)
		}
		merged, err := MergeParagraphs(split, Parse(split), 0, 1)
		if err != nil || !bytes.Equal(merged, source) {
			t.Errorf("split→merge %q = %q, %v", original, merged, err)
		}
	}
}

func TestMergeStyledParagraphsAllowsDistinctStylesAndEmptySides(t *testing.T) {
	for _, test := range []struct{ source, want string }{
		{`<p><strong>ab</strong></p>` + "\n" + `<p><em>cd</em></p>`, `<p><strong>ab</strong><em>cd</em></p>`},
		{`<p><strong>bold</strong></p>` + "\n" + `<p><br></p>`, `<p><strong>bold</strong></p>`},
		{`<p><br></p>` + "\n" + `<p><strong>bold</strong></p>`, `<p><strong>bold</strong></p>`},
		{`<p><strong class='a'>ab</strong></p>` + "\n" + `<p><strong class='b'>cd</strong></p>`, `<p><strong class='a'>ab</strong><strong class='b'>cd</strong></p>`},
	} {
		source := []byte(test.source)
		result, err := MergeParagraphs(source, Parse(source), 0, 1)
		if err != nil || string(result) != test.want {
			t.Errorf("merge %q = %q, %v; want %q", test.source, result, err, test.want)
		}
	}
}

func TestMergeStyledParagraphsRefusesAttributeLossAndReadOnlyGap(t *testing.T) {
	for _, source := range []string{
		`<p>before</p>` + "\n" + `<p class='other'>after</p>`,
		`<p>before</p><!--keep--><p>after</p>`,
		`<p>before<img src='assets/p.png'></p>` + "\n" + `<p>after</p>`,
		`<p><a href='javascript:bad'>unsafe</a></p>` + "\n" + `<p>after</p>`,
	} {
		value := []byte(source)
		parsed := Parse(value)
		if len(parsed.Blocks) < 2 {
			continue
		}
		if _, err := MergeParagraphs(value, parsed, 0, 1); !errors.Is(err, ErrReadOnlyLeaf) {
			t.Errorf("unsafe styled merge %q=%v", source, err)
		}
	}
}
