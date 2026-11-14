package main

import (
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/markdown"
)

func outlineOf(source string) []outlineEntry {
	var entries []outlineEntry
	var parsed markdown.Document
	parsed.Parse(source)
	byteOffset, runeOffset := 0, 0
	for index := range parsed.Blocks {
		block := &parsed.Blocks[index]
		if block.Kind != markdown.Heading {
			continue
		}
		runeOffset += utf8.RuneCountInString(source[byteOffset:block.Start])
		byteOffset = block.Start
		var title strings.Builder
		for _, span := range parsed.Spans(block) {
			title.WriteString(span.Text)
		}
		entries = append(entries, outlineEntry{Title: title.String(), Level: block.Level, Rune: runeOffset})
	}
	return entries
}
