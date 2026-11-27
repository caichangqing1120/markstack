package markdownedit

import (
	"errors"
	"strings"
	"testing"
)

func TestM1TablePatches(test *testing.T) {
	const header = "| H | J |"
	const delimiter = "| --- | --- |"
	const first = "| α | 一 |"
	const last = "| β | 二 |"
	const blank = "|   |   |"
	for _, entry := range []struct {
		name      string
		action    TableAction
		row       int
		want      []string
		targetRow int
		targetCol int
	}{
		{"above-header", RowAbove, 0, []string{blank, delimiter, header, first, last}, 0, 1},
		{"above-middle", RowAbove, 1, []string{header, delimiter, blank, first, last}, 1, 1},
		{"below-last", RowBelow, 2, []string{header, delimiter, first, last, blank}, 3, 1},
		{"delete-header", RowDelete, 0, []string{first, delimiter, last}, 0, 1},
		{"delete-last", RowDelete, 2, []string{header, delimiter, first}, 1, 1},
		{"column-left", ColumnLeft, 1, []string{"| H |   | J |", "| --- | --- | --- |", "| α |   | 一 |", "| β |   | 二 |"}, 1, 1},
		{"column-right", ColumnRight, 1, []string{"| H | J |   |", "| --- | --- | --- |", "| α | 一 |   |", "| β | 二 |   |"}, 1, 2},
		{"column-delete", ColumnDelete, 1, []string{"| H |", "| --- |", "| α |", "| β |"}, 1, 0},
		{"align-left", AlignLeft, 1, []string{header, "| --- | :--- |", first, last}, 1, 1},
		{"align-center", AlignCenter, 1, []string{header, "| --- | :---: |", first, last}, 1, 1},
		{"align-right", AlignRight, 1, []string{header, "| --- | ---: |", first, last}, 1, 1},
		{"delete-table", TableDelete, 1, nil, 0, 0},
	} {
		for _, ending := range []string{"\n", "\r\n"} {
			test.Run(entry.name+ending, func(test *testing.T) {
				prefix := "\xef\xbb\xbf# title" + ending + ending
				suffix := ending + "*untouched*\n"
				source := []byte(prefix + strings.Join([]string{header, delimiter, first, last}, ending) + ending + suffix)
				blocks := Parse(source)
				patch, err := PatchTable(source, blocks[1], entry.row, 1, entry.action)
				if err != nil {
					test.Fatal(err)
				}
				want := prefix + suffix
				if entry.want != nil {
					want = prefix + strings.Join(entry.want, ending) + ending + suffix
				}
				if got := string(patch.apply(source)); got != want {
					test.Fatalf("patch=%q, want %q", got, want)
				}
				if entry.want != nil && (!patch.Cell.Editable || patch.Cell.Row != entry.targetRow || patch.Cell.Column != entry.targetCol || want[patch.Cell.Start:patch.Cell.End] != patch.Cell.Visible) {
					test.Fatalf("target=%+v", patch.Cell)
				}
			})
		}
	}
}

func TestM1TableBoundaryAndStaleSource(test *testing.T) {
	for _, action := range []TableAction{TableDelete, RowDelete, ColumnDelete} {
		source := []byte("|   |\r\n| --- |")
		block := Parse(source)[0]
		patch, err := PatchTable(source, block, 0, 0, action)
		if err != nil || len(patch.apply(source)) != 0 {
			test.Fatalf("empty table deletion %d=%+v, %v", action, patch, err)
		}
		if _, err := PatchTable(append(source, '\n'), block, 0, 0, action); !errors.Is(err, ErrStaleBlock) {
			test.Fatalf("stale table edit=%v", err)
		}
	}
	for _, value := range []string{"| a | b |\n| --- | --- |\n| c |", "| a |\n| --- |\n| **bold** |"} {
		source := []byte(value)
		if CanEditTable(source, Parse(source)[0]) {
			test.Fatalf("non-rectangular or styled table considered simple: %q", value)
		}
	}
}

func TestM1ListIndentAndPromotion(test *testing.T) {
	for _, entry := range []struct {
		before, after string
		index         int
		outdent       bool
	}{
		{"- A\n- B\n- C\n", "- A\n  - B\n- C\n", 1, false},
		{"9. A\n10. B\n11. C\n", "9. A\n\n   10. B\n11. C\n", 1, false},
		{"- [ ] A\n- [x] B\n  - C\n", "- [ ] A\n  - [x] B\n    - C\n", 1, false},
		{"- A\n  - B\n  - C\n- D", "- A\n  - C\n- B\n- D", 1, true},
		{"- A\n  - B\n  - C", "- A\n  - C\n- B", 1, true},
		{"- A\n  - B\n    - C\n    - D\n", "- A\n  - B\n    - D\n  - C\n", 2, true},
	} {
		test.Run(entry.before, func(test *testing.T) {
			crlf := func(value string) string { return "\xef\xbb\xbf" + strings.ReplaceAll(value, "\n", "\r\n") }
			source := []byte(crlf(entry.before))
			block := Parse(source)[entry.index]
			patch, caret, err := PatchListIndent(source, block, entry.outdent)
			if err != nil {
				test.Fatalf("list patch failed: %+v: %v", block, err)
			}
			if next := string(patch.apply(source)); next != crlf(entry.after) || !strings.HasPrefix(next[caret:], block.Visible) {
				test.Fatalf("list = %q, caret=%d; want %q", next, caret, crlf(entry.after))
			}
		})
	}
}

func TestM1EmptyListsAndUnsafeIndent(test *testing.T) {
	for _, value := range []string{"- ", "1. ", "- [ ] ", "- [x] "} {
		source := []byte("- previous\r\n" + value + "\r\n")
		blocks := Parse(source)
		patch, err := ExitEmptyList(source, blocks[1])
		if err != nil || string(patch.apply(source)) != "- previous\r\n\r\n\r\n" {
			test.Fatalf("empty %q: %+v, %v", value, patch, err)
		}
	}
	for _, value := range []string{"- first\r\n", "- previous\n\n- multi\n  continuation\n"} {
		source := []byte(value)
		blocks := Parse(source)
		if _, _, err := PatchListIndent(source, blocks[len(blocks)-1], false); !errors.Is(err, ErrReadOnlyBlock) {
			test.Fatalf("unsafe indent of %q was not refused: %v", value, err)
		}
	}
}

func TestM1FenceLanguageOnlyReplacesTheLanguageToken(test *testing.T) {
	source := []byte("\xef\xbb\xbf~~~  go  linenos\r\nfmt.Println(\"你好\")\r\n~~~\r\n\r\nTail\n")
	block := Parse(source)[0]
	patch, err := PatchFenceLanguage(source, block, "c++")
	if err != nil || string(patch.apply(source)) != strings.Replace(string(source), "go  linenos", "c++  linenos", 1) {
		test.Fatalf("fence language = %+v, %v", patch, err)
	}
	for _, invalid := range []string{"python\n- new block", "`go`", "", "go ln"} {
		if _, err := PatchFenceLanguage(source, block, invalid); !errors.Is(err, ErrReadOnlyBlock) {
			test.Errorf("language %q was not refused: %v", invalid, err)
		}
	}
	plain := []byte("```\r\n```\r\n")
	if patch, err := PatchFenceLanguage(plain, Parse(plain)[0], "python"); err != nil || string(patch.apply(plain)) != "```python\r\n```\r\n" {
		test.Fatalf("language on empty fence = %+v, %v", patch, err)
	}
}
