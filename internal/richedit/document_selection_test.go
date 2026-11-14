package richedit

import (
	"bytes"
	"errors"
	"testing"
)

func TestReplaceBlockSelectionMergesPlainParagraphsWithoutRewritingNeighbors(t *testing.T) {
	source := append([]byte{0xef, 0xbb, 0xbf}, []byte("<p class='one'>第一 &amp; 尾</p>\r\n<p>第二 段</p>\r\n<!--keep-->\r\n<p>尾部</p>")...)
	document := Parse(source)
	if len(document.Blocks) != 3 || document.Blocks[0].Visible != "第一 & 尾" || document.Blocks[1].Visible != "第二 段" {
		t.Fatalf("source blocks=%+v", document.Blocks)
	}
	result, err := ReplaceBlockSelection(source, document, 0, len("第一 &"), 1, len("第二"), "新")
	want := append([]byte{0xef, 0xbb, 0xbf}, []byte("<p class='one'>第一 &amp;新 段</p>\r\n<!--keep-->\r\n<p>尾部</p>")...)
	if err != nil || !bytes.Equal(result, want) {
		t.Fatalf("cross-paragraph patch=%q, %v", result, err)
	}
	if _, err := ReplaceBlockSelection(result, document, 0, len("第一 &"), 1, len("第二"), "stale"); !errors.Is(err, ErrStaleLeaf) {
		t.Fatalf("stale cross-paragraph patch=%v", err)
	}
}

func TestReplaceBlockSelectionRejectsUnknownAndAmbiguousStructure(t *testing.T) {
	for _, source := range [][]byte{
		[]byte("<p>first</p><x-unknown>keep</x-unknown><p>second</p>"),
		[]byte("<p>first</p><!--hidden--><p>second</p>"),
		[]byte("<p><strong>first</strong></p><p>second</p>"),
		[]byte("<p>first</p><p><em>second</em></p>"),
	} {
		document := Parse(source)
		last := len(document.Blocks) - 1
		if _, err := ReplaceBlockSelection(source, document, 0, 1, last, 1, "X"); !errors.Is(err, ErrReadOnlyLeaf) {
			t.Fatalf("ambiguous source %q accepted: %v", source, err)
		}
	}
}

func TestReplaceBlockSelectionRemovesEmptyBreakPlaceholder(t *testing.T) {
	for _, test := range []struct {
		source, want string
	}{
		{"<p>Body</p>\n<p><br></p>", "<p>Body</p>"},
		{"<p><br></p>\n<p>Tail</p>", "<p>Tail</p>"},
		{"<p><br></p>\n<p><br></p>", "<p><br></p>"},
	} {
		source := []byte(test.source)
		document := Parse(source)
		result, err := ReplaceBlockSelection(source, document, 0, len(document.Blocks[0].Visible), 1, 0, "")
		if err != nil || string(result) != test.want {
			t.Errorf("merge empty %q = %q, %v; want %q", source, result, err, test.want)
		}
	}
}
