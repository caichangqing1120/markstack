package richedit

import (
	"bytes"
	"errors"
	"html"
	"strings"
	"testing"
	"time"
)

func TestReplaceSelectionPreservesInlineWrappersAndUnknownNeighbors(t *testing.T) {
	source := []byte("<p>before <strong>bold</strong> after</p>\r\n<x-unknown data-x='keep'>raw</x-unknown>")
	block := Parse(source).Blocks[0]
	if block.Visible != "before bold after" {
		t.Fatalf("visible source=%q", block.Visible)
	}
	result, err := ReplaceSelection(source, block, len("before "), len("before bold a"), "新")
	want := []byte("<p>before <strong>新</strong>fter</p>\r\n<x-unknown data-x='keep'>raw</x-unknown>")
	if err != nil || !bytes.Equal(result, want) {
		t.Fatalf("cross-style patch=%q, %v", result, err)
	}
	if visible := Parse(result).Blocks[0].Visible; visible != "before 新fter" {
		t.Fatalf("patched visible=%q", visible)
	}
	if _, err := ReplaceSelection(result, block, 0, 2, "stale"); !errors.Is(err, ErrStaleLeaf) {
		t.Fatalf("stale block accepted: %v", err)
	}
	if _, err := ReplaceSelection(result, block, 0, 2, block.Visible[:2]); !errors.Is(err, ErrStaleLeaf) {
		t.Fatalf("stale no-op selection accepted: %v", err)
	}
}

func TestReplaceSelectionKeepsUntouchedEntitySpelling(t *testing.T) {
	source := append([]byte{0xef, 0xbb, 0xbf}, []byte(`<p>A &amp; <strong>bold</strong> tail</p>`)...)
	block := Parse(source).Blocks[0]
	unchanged, err := ReplaceSelection(source, block, len("A "), len("A &"), "&")
	if err != nil || !bytes.Equal(unchanged, source) {
		t.Fatalf("no-op selection changed entity spelling: %q, %v", unchanged, err)
	}
	result, err := ReplaceSelection(source, block, len("A &"), len("A & bo"), "新")
	want := append([]byte{0xef, 0xbb, 0xbf}, []byte(`<p>A &amp;新<strong>ld</strong> tail</p>`)...)
	if err != nil || !bytes.Equal(result, want) {
		t.Fatalf("entity-adjacent selection=%q, %v", result, err)
	}
}

func TestReplaceSelectionRejectsReadOnlyAndPartialRuneRanges(t *testing.T) {
	for _, source := range [][]byte{
		[]byte(`<p>safe <span>unknown</span> tail</p>`),
		[]byte(`<p>a<img src="assets/p.png" alt="image">b</p>`),
	} {
		block := Parse(source).Blocks[0]
		if _, err := ReplaceSelection(source, block, 0, len(block.Visible), "X"); !errors.Is(err, ErrReadOnlyLeaf) {
			t.Fatalf("unsafe selection accepted for %q: %v", source, err)
		}
	}
	source := []byte(`<p>中文<strong>bold</strong></p>`)
	block := Parse(source).Blocks[0]
	if _, err := ReplaceSelection(source, block, 1, len("中文bold"), "X"); !errors.Is(err, ErrReadOnlyLeaf) {
		t.Fatalf("partial UTF-8 rune accepted: %v", err)
	}
}

func TestEntityOffsetMappingPreservesNamedNumericAndAmbiguousReferences(t *testing.T) {
	for _, raw := range []string{
		"A &amp; B", "&notit; tail", "&#x1F600; 汉字", "&NotEqualTilde; x", "plain &invalid; suffix",
	} {
		visible := html.UnescapeString(raw)
		for offset := range visible {
			mapped, ok := rawOffsetAtVisible(raw, visible, offset)
			if ok && (mapped < 0 || mapped > len(raw) || html.UnescapeString(raw[:mapped])+html.UnescapeString(raw[mapped:]) != visible || len(html.UnescapeString(raw[:mapped])) != offset) {
				t.Fatalf("invalid entity boundary raw=%q visible=%q offset=%d mapped=%d", raw, visible, offset, mapped)
			}
		}
		if mapped, ok := rawOffsetAtVisible(raw, visible, len(visible)); !ok || mapped != len(raw) {
			t.Fatalf("entity end raw=%q mapped=%d ok=%v", raw, mapped, ok)
		}
	}
	for _, test := range []struct {
		raw, visible string
		target, want int
	}{
		{"A &amp; B &lt; C", "A & B < C", len("A & B <"), len("A &amp; B &lt;")},
		{"&notit;", html.UnescapeString("&notit;"), len("¬"), len("&not")},
		{"&#x1F600;X", "😀X", len("😀"), len("&#x1F600;")},
		{"&ampX", html.UnescapeString("&ampX"), len("&"), len("&amp")},
	} {
		mapped, ok := rawOffsetAtVisible(test.raw, test.visible, test.target)
		if !ok || mapped != test.want {
			t.Errorf("entity boundary raw=%q visible=%q target=%d => %d, %v; want %d", test.raw, test.visible, test.target, mapped, ok, test.want)
		}
	}
	complex := "&NotEqualTilde;X"
	visible := html.UnescapeString(complex)
	if _, ok := rawOffsetAtVisible(complex, visible, len("≂")); ok {
		t.Fatal("entity producing two codepoints was split internally")
	}
}

func TestLongEntityLeafOffsetMappingUsesBoundedAllocations(t *testing.T) {
	prefix := strings.Repeat("a", 1000000)
	raw := prefix + "&amp;" + strings.Repeat("b", 1000000)
	visible := html.UnescapeString(raw)
	started := time.Now()
	allocations := testing.AllocsPerRun(1, func() {
		if mapped, ok := rawOffsetAtVisible(raw, visible, len(prefix)+1); !ok || mapped != len(prefix)+len("&amp;") {
			t.Fatalf("long entity offset=%d, %v", mapped, ok)
		}
	})
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("long entity mapping took %v", elapsed)
	}
	if allocations > 500 {
		t.Fatalf("long entity mapping allocated %v times", allocations)
	}
}
