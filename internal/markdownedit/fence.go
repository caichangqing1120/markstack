package markdownedit

import (
	"bytes"
	"crypto/sha256"
	"strings"
)

func FenceLanguage(info string) string {
	language, _, _ := strings.Cut(strings.TrimSpace(info), " ")
	language, _, _ = strings.Cut(language, "\t")
	return language
}

func PatchFenceLanguage(source []byte, block Block, language string) (SourcePatch, error) {
	if sha256.Sum256(source) != block.source {
		return SourcePatch{}, ErrStaleBlock
	}
	if block.Kind != FencedCode || !block.Code.Editable || block.Code.Math || len(language) > 64 {
		return SourcePatch{}, ErrReadOnlyBlock
	}
	for _, character := range language {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || strings.ContainsRune("_+.#-", character)) {
			return SourcePatch{}, ErrReadOnlyBlock
		}
	}
	lineEnd := block.Code.Start - len(lineEndingBefore(source, block.Code.Start))
	lineStart := bytes.LastIndexByte(source[:lineEnd], '\n') + 1
	if lineStart == 0 && bytes.HasPrefix(source, []byte("\xef\xbb\xbf")) {
		lineStart = 3
	}
	marker := source[lineStart]
	start := lineStart
	for start < lineEnd && source[start] == marker {
		start++
	}
	for start < lineEnd && (source[start] == ' ' || source[start] == '\t') {
		start++
	}
	end := start
	for end < lineEnd && source[end] != ' ' && source[end] != '\t' {
		end++
	}
	patch := SourcePatch{Start: start, End: end, Text: language}
	delta := len(language) - (end - start)
	for _, updated := range Parse(patch.apply(source)) {
		if updated.Kind == FencedCode && updated.Code.Editable && updated.Code.Start == block.Code.Start+delta &&
			updated.Code.End == block.Code.End+delta && updated.Code.Visible == block.Code.Visible && FenceLanguage(updated.Code.Info) == language {
			return patch, nil
		}
	}
	return SourcePatch{}, ErrReadOnlyBlock
}
