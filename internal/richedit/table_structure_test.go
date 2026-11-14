package richedit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTableCellsIdentifyTheirContainingTable(t *testing.T) {
	source := []byte("<table><tr><td>A</td></tr></table><table><tr><td>B</td></tr></table>")
	blocks := Parse(source).Blocks
	if len(blocks) != 2 || blocks[0].TableStart != 0 || blocks[1].TableStart != strings.Index(string(source)[1:], "<table>")+1 || blocks[0].TableStart == blocks[1].TableStart {
		t.Fatalf("table identities: %+v", blocks)
	}
}

func TestInsertTableRowPreservesOriginalCellsAndUnknownTail(t *testing.T) {
	source := []byte("<table><tbody><tr><th>列</th><td>值</td></tr></tbody></table>\n<!--keep-->")
	document := Parse(source)
	next, index, err := InsertTableRow(source, document, 1, "\n")
	want := "<table><tbody><tr><th>列</th><td>值</td></tr>\n<tr><td><br></td><td><br></td></tr></tbody></table>\n<!--keep-->"
	if err != nil || string(next) != want || index != 2 {
		t.Fatalf("insert table row=%q, index=%d, error=%v", next, index, err)
	}
	blocks := Parse(next).Blocks
	if len(blocks) != 4 || blocks[2].Kind != TableCell || blocks[3].Kind != TableCell || blocks[2].Visible != "" || !blocks[2].Leaves[0].Editable {
		t.Fatalf("inserted cells not editable: %+v", blocks)
	}
}

func TestRealMkDownTableAddsOnlyCanonicalSingleSpanCells(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("testdata", "Real-MkDown-Structure.mh"))
	if err != nil {
		t.Fatal(err)
	}
	document := Parse(source)
	var lastIndex int = -1
	for index, block := range document.Blocks {
		if block.Kind == TableCell {
			lastIndex = index
		}
	}
	if lastIndex < 0 || document.Blocks[lastIndex].Visible != "值 & 保留" || document.Blocks[lastIndex].ReadOnly || !document.Blocks[lastIndex].Leaves[0].Editable {
		t.Fatalf("real MkDown last cell is not editable: %+v", document.Blocks)
	}
	next, index, err := InsertTableRow(source, document, lastIndex, "\n")
	insert := "\n<tr><td colSpan=\"1\" rowSpan=\"1\" width=\"auto\"><br></td><td colSpan=\"1\" rowSpan=\"1\" width=\"auto\"><br></td></tr>"
	want := strings.Replace(string(source), "</tr></tbody></table>", "</tr>"+insert+"</tbody></table>", 1)
	if err != nil || string(next) != want || index != lastIndex+1 {
		t.Fatalf("real MkDown row=%q, index=%d, error=%v", next, index, err)
	}
	for _, block := range Parse(next).Blocks[index : index+2] {
		if block.Kind != TableCell || block.ReadOnly || len(block.Leaves) != 1 || !block.Leaves[0].Editable || block.Visible != "" {
			t.Fatalf("new MkDown cell is not editable: %+v", block)
		}
	}
}

func TestInsertTableRowPreservesBOMCRLFAndRejectsStaleOrUnsafe(t *testing.T) {
	source := []byte("\xef\xbb\xbf<table><tbody><tr><td>A</td></tr></tbody></table>\r\n<!--keep-->")
	document := Parse(source)
	next, _, err := InsertTableRow(source, document, 0, "\r\n")
	if err != nil || !strings.Contains(string(next), "</tr>\r\n<tr><td><br></td></tr>") || !strings.HasPrefix(string(next), "\xef\xbb\xbf") || !strings.HasSuffix(string(next), "\r\n<!--keep-->") {
		t.Fatalf("CRLF table insert=%q, %v", next, err)
	}
	if _, _, err := InsertTableRow(append(append([]byte(nil), source...), 'x'), document, 0, "\r\n"); !errors.Is(err, ErrStaleLeaf) {
		t.Fatalf("stale table insert=%v", err)
	}
	for _, raw := range []string{
		"<table><tbody><tr><td>A</td></tr><!--keep--></tbody></table>",
		"<table><tbody><tr><td colspan=\"2\">A</td></tr></tbody></table>",
		"<table style=\"width: auto;\"><tbody><tr><td colSpan=\"2\" rowSpan=\"1\" width=\"auto\">A</td></tr></tbody></table>",
		"<table style=\"background: url(https://example.org/a)\"><tbody><tr><td>A</td></tr></tbody></table>",
	} {
		blocks := Parse([]byte(raw))
		if _, _, err := InsertTableRow([]byte(raw), blocks, 0, "\n"); !errors.Is(err, ErrReadOnlyLeaf) {
			t.Errorf("unsafe table %q inserted: %v", raw, err)
		}
	}
}
