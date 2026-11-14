package richedit

import (
	"bytes"
	"errors"
	"testing"
)

func TestExitLastEmptyOrderedListItemPreservesBOMCRLFAndTail(t *testing.T) {
	source := append([]byte{0xef, 0xbb, 0xbf}, []byte(`<ol start="9"><li>First</li><li><br></li></ol>`+"\r\n<!--keep-->\r\n")...)
	parsed := Parse(source)
	result, index, err := ExitEmptyListItem(source, parsed, 1, "\r\n")
	want := append([]byte{0xef, 0xbb, 0xbf}, []byte(`<ol start="9"><li>First</li></ol>`+"\r\n<p><br></p>\r\n<!--keep-->\r\n")...)
	if err != nil || index != 1 || !bytes.Equal(result, want) {
		t.Fatalf("empty ordered exit=%q index=%d err=%v; want %q", result, index, err, want)
	}
	blocks := Parse(result).Blocks
	if len(blocks) != 2 || blocks[0].Kind != ListItem || blocks[0].Number != 9 || blocks[1].Kind != Paragraph || blocks[1].Visible != "" || !blocks[1].Leaves[0].Editable {
		t.Fatalf("exited list blocks=%+v", blocks)
	}
	if _, _, err := ExitEmptyListItem(result, parsed, 1, "\r\n"); !errors.Is(err, ErrStaleLeaf) {
		t.Fatalf("stale list exit=%v", err)
	}
}

func TestExitOnlyEmptyListItemRemovesContainer(t *testing.T) {
	for _, source := range []string{`<ul><li><br></li></ul>`, `<ol start="5"><li><br></li></ol>`} {
		result, index, err := ExitEmptyListItem([]byte(source), Parse([]byte(source)), 0, "\n")
		if err != nil || index != 0 || string(result) != `<p><br></p>` {
			t.Errorf("only empty list %q = %q index=%d err=%v", source, result, index, err)
		}
	}
}

func TestExitMiddleEmptyListItemSplitsContainersAndKeepsOrderedNumbers(t *testing.T) {
	for _, test := range []struct {
		source, want string
		index        int
	}{
		{`<ul><li>one</li><li><br></li><li>three</li></ul>`, "<ul><li>one</li></ul>\n<p><br></p>\n<ul><li>three</li></ul>", 1},
		{`<ol start="9"><li>one</li><li><br></li><li>three</li></ol>`, "<ol start=\"9\"><li>one</li></ol>\n<p><br></p>\n<ol start=\"11\"><li>three</li></ol>", 1},
		{`<ul><li><br></li><li>after</li></ul>`, "<p><br></p>\n<ul><li>after</li></ul>", 0},
	} {
		source := []byte(test.source)
		parsed := Parse(source)
		result, index, err := ExitEmptyListItem(source, parsed, test.index, "\n")
		if err != nil || index != test.index || string(result) != test.want {
			t.Errorf("middle list exit %q = %q index=%d err=%v; want %q", test.source, result, index, err, test.want)
		}
		if err == nil {
			blocks := Parse(result).Blocks
			if len(blocks) != len(parsed.Blocks) || blocks[index].Kind != Paragraph {
				t.Errorf("middle exit blocks=%+v", blocks)
			}
			if test.source[1] == 'o' && blocks[len(blocks)-1].Number != 11 {
				t.Errorf("following ordered number=%d", blocks[len(blocks)-1].Number)
			}
		}
	}
}

func TestExitListItemRefusesMiddleOrUnsafeStructure(t *testing.T) {
	for _, test := range []struct {
		source string
		index  int
	}{
		{`<ul><li>one</li><li data-id="keep"><br></li></ul>`, 1},
		{`<ol class="keep" start="9"><li>one</li><li><br></li><li>three</li></ol>`, 1},
		{`<ul><li>one</li><!--keep--><li><br></li></ul>`, 1},
		{`<ul><li>one</li><li><br></li><!--keep--></ul>`, 1},
		{`<ul><li>one</li><li><img src="assets/p.png"></li></ul>`, 1},
		{`<ul><li><ul><li><br></li></ul></li></ul>`, 1},
	} {
		source := []byte(test.source)
		parsed := Parse(source)
		if test.index >= len(parsed.Blocks) {
			continue
		}
		if _, _, err := ExitEmptyListItem(source, parsed, test.index, "\n"); !errors.Is(err, ErrReadOnlyLeaf) {
			t.Errorf("unsafe list exit %q=%v", source, err)
		}
	}
}
