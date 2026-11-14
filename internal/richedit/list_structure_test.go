package richedit

import (
	"bytes"
	"errors"
	"testing"
)

func TestSplitAndMergeOrderedListItemRestoresBOMCRLF(t *testing.T) {
	source := append([]byte{0xef, 0xbb, 0xbf}, []byte(`<ol start="9"><li>AlphaBeta</li><li>Tail</li></ol>`+"\r\n<!--keep-->\r\n")...)
	document := Parse(source)
	if len(document.Blocks) != 2 || document.Blocks[0].Number != 9 || document.Blocks[1].Number != 10 || !CanSplitListItem(document.Blocks[0]) {
		t.Fatalf("ordered baseline=%+v", document.Blocks)
	}
	split, err := SplitListItem(source, document.Blocks[0], len("Alpha"), "\r\n")
	want := append([]byte{0xef, 0xbb, 0xbf}, []byte(`<ol start="9"><li>Alpha</li>`+"\r\n"+`<li>Beta</li><li>Tail</li></ol>`+"\r\n<!--keep-->\r\n")...)
	if err != nil || !bytes.Equal(split, want) {
		t.Fatalf("ordered list split=%q, %v; want %q", split, err, want)
	}
	blocks := Parse(split).Blocks
	if len(blocks) != 3 || blocks[0].Number != 9 || blocks[1].Number != 10 || blocks[2].Number != 11 || blocks[0].Visible != "Alpha" || blocks[1].Visible != "Beta" {
		t.Fatalf("ordered split blocks=%+v", blocks)
	}
	merged, err := MergeListItems(split, Parse(split), 0, 1)
	if err != nil || !bytes.Equal(merged, source) {
		t.Fatalf("ordered split→merge=%q, %v", merged, err)
	}
	if _, err := SplitListItem(split, document.Blocks[0], 2, "\r\n"); !errors.Is(err, ErrStaleLeaf) {
		t.Fatalf("stale ordered list split=%v", err)
	}
}

func TestSplitStyledListItemAndEmptyTail(t *testing.T) {
	for _, test := range []struct {
		source, want string
		caret        int
	}{
		{`<ul><li>before <strong>bold</strong></li></ul>`, "<ul><li>before <strong>bo</strong></li>\n<li><strong>ld</strong></li></ul>", len("before bo")},
		{`<ul><li>one</li></ul>`, "<ul><li>one</li>\n<li><br></li></ul>", len("one")},
	} {
		source := []byte(test.source)
		result, err := SplitListItem(source, Parse(source).Blocks[0], test.caret, "\n")
		if err != nil || string(result) != test.want {
			t.Errorf("split li %q=%q, %v; want %q", test.source, result, err, test.want)
		}
	}
}

func TestListItemStructureRefusesUnknownGapAndAttributeLoss(t *testing.T) {
	for _, source := range []string{
		`<ul><li>first</li><!--keep--><li>second</li></ul>`,
		`<ul><li>first</li><li data-id="keep">second</li></ul>`,
		`<ul><li>first<img src="assets/p.png"></li><li>second</li></ul>`,
		`<ul><li>first</li></ul><ul><li>second</li></ul>`,
	} {
		value := []byte(source)
		parsed := Parse(value)
		if len(parsed.Blocks) < 2 {
			t.Fatalf("unsafe fixture has too few blocks: %q", source)
		}
		if _, err := MergeListItems(value, parsed, 0, 1); !errors.Is(err, ErrReadOnlyLeaf) {
			t.Errorf("unsafe list merge %q=%v", source, err)
		}
	}
	source := []byte(`<ul><li>before<!--keep-->after</li></ul>`)
	if _, err := SplitListItem(source, Parse(source).Blocks[0], 2, "\n"); !errors.Is(err, ErrReadOnlyLeaf) {
		t.Fatalf("commented list split=%v", err)
	}
}
