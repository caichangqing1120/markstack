package exportpdf

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestRenderRejectsIncompleteTrueTypeFont(t *testing.T) {
	font, err := BundledFont()
	if err != nil {
		t.Fatal(err)
	}
	missingName := bytes.Clone(font)
	copy(fontTableEntry(t, missingName, "name")[:4], "TEST")
	shortPost := bytes.Clone(font)
	binary.BigEndian.PutUint32(fontTableEntry(t, shortPost, "post")[12:16], 8)
	shortMetrics := bytes.Clone(font)
	binary.BigEndian.PutUint32(fontTableEntry(t, shortMetrics, "hmtx")[12:16], 4)
	shortMapping := bytes.Clone(font)
	binary.BigEndian.PutUint32(fontTableEntry(t, shortMapping, "cmap")[12:16], 4)
	truncatedGlyphs := bytes.Clone(font)
	binary.BigEndian.PutUint32(fontTableEntry(t, truncatedGlyphs, "glyf")[12:16], 1)
	invalidLocations := bytes.Clone(font)
	locationEntry := fontTableEntry(t, invalidLocations, "loca")
	locationStart := binary.BigEndian.Uint32(locationEntry[8:12])
	locationEnd := locationStart + binary.BigEndian.Uint32(locationEntry[12:16])
	clear(invalidLocations[locationEnd-4 : locationEnd])
	for _, fixture := range []struct {
		name string
		font []byte
	}{
		{"short-file", []byte("no")},
		{"not-truetype", []byte("ordinary text in a .ttf file")},
		{"truncated-directory", bytes.Clone(font[:12])},
		{"truncated-tables", bytes.Clone(font[:len(font)/2])},
		{"missing-name", missingName},
		{"short-post", shortPost},
		{"truncated-metrics", shortMetrics},
		{"truncated-mapping", shortMapping},
		{"truncated-glyph-data", truncatedGlyphs},
		{"unordered-glyph-locations", invalidLocations},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			data, err := RenderMarkdown([]byte("# Sample\n\n中文\n"), nil, fixture.font)
			if len(data) != 0 || err == nil {
				t.Fatalf("expected an error and no PDF for an incomplete font, got %d bytes and %v", len(data), err)
			}
		})
	}
}

func fontTableEntry(t *testing.T, font []byte, tag string) []byte {
	t.Helper()
	const headerSize, entrySize = 12, 16
	for index := range int(binary.BigEndian.Uint16(font[4:6])) {
		offset := headerSize + index*entrySize
		entry := font[offset : offset+entrySize]
		if string(entry[:4]) == tag {
			return entry
		}
	}
	t.Fatalf("bundled font has no %q table", tag)
	return nil
}
