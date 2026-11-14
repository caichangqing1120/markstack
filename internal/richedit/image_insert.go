package richedit

import (
	"bytes"
	"crypto/sha256"
	"strings"
	"unicode/utf8"
)

func InsertImage(source []byte, leaf Leaf, visibleCaret int, relative string) ([]byte, error) {
	if leaf.sourceDigest != sha256.Sum256(source) {
		return nil, ErrStaleLeaf
	}
	if !leaf.Editable || leaf.ImageSource != "" || leaf.Start < 0 || leaf.End > len(source) || leaf.Start > leaf.End ||
		visibleCaret < 0 || visibleCaret > len(leaf.Visible) || !utf8.ValidString(leaf.Visible[:visibleCaret]) || len(relative) != len("assets/")+64+len(".png") ||
		!strings.HasPrefix(relative, "assets/") || !strings.HasSuffix(relative, ".png") {
		return nil, ErrReadOnlyLeaf
	}
	for _, character := range relative[len("assets/") : len(relative)-len(".png")] {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return nil, ErrReadOnlyLeaf
		}
	}
	raw := source[leaf.Start:leaf.End]
	position, tail := leaf.Start, leaf.Start
	if leaf.Visible == "" && bytes.HasPrefix(bytes.ToLower(raw), []byte("<br")) {
		tail = leaf.End
	} else {
		rawOffset, ok := rawOffsetAtVisible(string(raw), leaf.Visible, visibleCaret)
		if !ok {
			return nil, ErrReadOnlyLeaf
		}
		position += rawOffset
		tail = position
	}
	image := `<img src="` + relative + `" alt="">`
	result := make([]byte, 0, len(source)-(tail-position)+len(image))
	result = append(result, source[:position]...)
	result = append(result, image...)
	result = append(result, source[tail:]...)
	for _, block := range Parse(result).Blocks {
		if block.ReadOnly {
			continue
		}
		for _, candidate := range block.Leaves {
			if candidate.Start == position && candidate.ImageSource == relative && candidate.SafeAsset {
				return result, nil
			}
		}
	}
	return nil, ErrReadOnlyLeaf
}
