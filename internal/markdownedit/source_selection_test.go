package markdownedit

import (
	"errors"
	"testing"
)

func TestVisibleRangeAtSourcePreservesLiteralIdentity(test *testing.T) {
	for _, scenario := range []struct {
		name      string
		source    string
		start     int
		end       int
		wantStart int
		wantEnd   int
		refused   bool
	}{
		{"plain-unicode", "a\u4e2d\u6587Z\n", 1, 7, 1, 7, false},
		{"plain-end", "abcd\n", 4, 4, 4, 4, false},
		{"bold-unicode", "**a\u4e2d\u6587Z**\n", 3, 9, 1, 7, false},
		{"bold-label-start", "**a\u4e2d\u6587Z**\n", 2, 2, 0, 0, false},
		{"bold-label-end", "**a\u4e2d\u6587Z**\n", 10, 10, 8, 8, false},
		{"mixed-unicode", "keep **a\u4e2d\u6587Z** tail\n", 8, 14, 6, 12, false},
		{"heading", "### abcd\n", 5, 7, 1, 3, false},
		{"link-label", "[abcd](url)\n", 2, 4, 1, 3, false},
		{"mixed-link", "pre [ab](url) tail\n", 5, 7, 4, 6, false},
		{"nested-leaf", "**ab *cd* ef** tail\n", 6, 8, 3, 5, false},
		{"plain-after-style", "**ab** tail\n", 6, 7, 2, 3, false},
		{"ambiguous-style-boundary", "**ab** tail\n", 6, 6, 0, 0, true},
		{"bold-opening", "**abcd**\n", 0, 2, 0, 0, true},
		{"bold-closing", "**abcd**\n", 6, 8, 0, 0, true},
		{"whole-style-syntax", "**abcd**\n", 0, 8, 0, 0, true},
		{"link-destination", "[ab](url) tail\n", 5, 8, 0, 0, true},
		{"cross-style-syntax", "**ab** cd\n", 3, 8, 0, 0, true},
		{"inside-unicode-rune", "**a\u4e2d\u6587Z**\n", 4, 9, 0, 0, true},
		{"heading-marker", "### abcd\n", 1, 5, 0, 0, true},
		{"outside-source", "abcd\n", 2, 99, 0, 0, true},
		{"reversed-byte-range", "abcd\n", 3, 1, 0, 0, true},
		{"inline-code-text", "ab `cd`\n", 4, 6, 3, 5, false},
		{"readonly-code", "ab ` cd `\n", 0, 1, 0, 0, true},
	} {
		test.Run(scenario.name, func(context *testing.T) {
			source := []byte(scenario.source)
			blocks := Parse(source)
			if len(blocks) != 1 {
				context.Fatal("fixture does not have one source block")
			}
			start, end, err := VisibleRangeAtSource(source, blocks[0], scenario.start, scenario.end)
			if string(source) != scenario.source {
				context.Fatal("source-to-visible mapping mutated source bytes")
			}
			if scenario.refused {
				if !errors.Is(err, ErrReadOnlyBlock) {
					context.Fatalf("unsafe source range error = %v, want readonly refusal", err)
				}
				return
			}
			if err != nil || start != scenario.wantStart || end != scenario.wantEnd {
				context.Fatalf("visible source range = %d..%d, %v, want %d..%d", start, end, err, scenario.wantStart, scenario.wantEnd)
			}
		})
	}
}

func TestVisibleRangeAtSourceRejectsStaleAndInvalidBlocks(test *testing.T) {
	source := []byte("**abcd**\n")
	block := Parse(source)[0]
	if _, _, err := VisibleRangeAtSource([]byte("**abce**\n"), block, 3, 5); !errors.Is(err, ErrStaleBlock) {
		test.Fatalf("changed source error = %v", err)
	}
	for _, bounds := range [][2]int{{-1, 8}, {0, 99}, {8, 0}} {
		invalid := block
		invalid.Start, invalid.End = bounds[0], bounds[1]
		if _, _, err := VisibleRangeAtSource(source, invalid, 3, 5); !errors.Is(err, ErrStaleBlock) {
			test.Fatalf("invalid block bounds %v error = %v", bounds, err)
		}
	}
	block.Visible = "different"
	if _, _, err := VisibleRangeAtSource(source, block, 3, 5); !errors.Is(err, ErrReadOnlyBlock) {
		test.Fatalf("mismatched visible identity error = %v", err)
	}
}
