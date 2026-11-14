package richedit

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestInsertImagePreservesEntityAndUnknownTail(t *testing.T) {
	source := []byte(`<p class="note">A &amp; B</p><!--keep--><x-unknown>raw</x-unknown>`)
	parsed := Parse(source)
	image := "assets/" + strings.Repeat("a", 64) + ".png"
	result, err := InsertImage(source, parsed.Blocks[0].Leaves[0], len("A & "), image)
	want := `<p class="note">A &amp; <img src="` + image + `" alt="">B</p><!--keep--><x-unknown>raw</x-unknown>`
	if err != nil || string(result) != want {
		t.Fatalf("insert image = %q, %v; want %q", result, err, want)
	}
	blocks := Parse(result).Blocks
	if len(blocks) < 2 || blocks[0].ReadOnly || len(blocks[0].Leaves) != 3 || !blocks[0].Leaves[1].SafeAsset || blocks[0].Leaves[1].ImageSource != image {
		t.Fatalf("inserted image is not a safe native leaf: %+v", blocks)
	}
	if !bytes.Equal(source[len(source)-len(`<!--keep--><x-unknown>raw</x-unknown>`):], result[len(result)-len(`<!--keep--><x-unknown>raw</x-unknown>`):]) {
		t.Fatal("unselected unknown tail changed")
	}
}

func TestInsertImageRejectsEntityInteriorStaleAndReadOnly(t *testing.T) {
	source := []byte(`<p>A &NotEqualTilde; B</p><script>no</script>`)
	parsed := Parse(source)
	image := "assets/" + strings.Repeat("b", 64) + ".png"
	leaf := parsed.Blocks[0].Leaves[0]
	if _, err := InsertImage(source, leaf, len("A ≂"), image); !errors.Is(err, ErrReadOnlyLeaf) {
		t.Fatalf("split entity = %v", err)
	}
	if _, err := InsertImage(append([]byte(nil), source...), leaf, 2, "../outside.png"); !errors.Is(err, ErrReadOnlyLeaf) {
		t.Fatalf("unsafe URL = %v", err)
	}
	if _, err := InsertImage(append([]byte(" "), source...), leaf, 2, image); !errors.Is(err, ErrStaleLeaf) {
		t.Fatalf("stale source = %v", err)
	}
	unknown := parsed.Blocks[len(parsed.Blocks)-1].Leaves
	if len(unknown) > 0 {
		if _, err := InsertImage(source, unknown[0], 0, image); !errors.Is(err, ErrReadOnlyLeaf) {
			t.Fatalf("read-only leaf = %v", err)
		}
	}
}

func TestImageAtParagraphEndHasEditableTrailingLeaf(t *testing.T) {
	source := []byte(`<p>A<img src="assets/p.png" alt=""></p>`)
	block := Parse(source).Blocks[0]
	if block.ReadOnly || len(block.Leaves) != 3 || block.Leaves[2].Start != block.CloseStart || block.Leaves[2].End != block.CloseStart || !block.Leaves[2].Editable {
		t.Fatalf("image at end has no trailing caret: %+v", block)
	}
	updated, err := ReplaceLeaf(source, block.Leaves[2], "Z")
	if err != nil || string(updated) != `<p>A<img src="assets/p.png" alt="">Z</p>` {
		t.Fatalf("typing after image = %q, %v", updated, err)
	}
}

func TestInsertImageReplacesEmptyParagraphBreak(t *testing.T) {
	source := []byte("<p><br></p>\r\n<!--keep-->")
	image := "assets/" + strings.Repeat("c", 64) + ".png"
	block := Parse(source).Blocks[0]
	updated, err := InsertImage(source, block.Leaves[0], 0, image)
	want := `<p><img src="` + image + `" alt=""></p>` + "\r\n<!--keep-->"
	if err != nil || string(updated) != want {
		t.Fatalf("paste into empty paragraph = %q, %v", updated, err)
	}
	parsed := Parse(updated).Blocks[0]
	if parsed.ReadOnly || len(parsed.Leaves) != 2 || !parsed.Leaves[1].Editable {
		t.Fatalf("empty image paragraph has no trailing edit point: %+v", parsed)
	}
}
