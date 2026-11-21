package exportpdf

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const bundledFontDigest = "26305be85f5378dc7d53f3dfa6c852a7e46621b226423400c1821aca9fc01c41"

//go:embed font/NotoSansSC-Regular.ttf.gz
var compressedFont []byte

//go:embed font/OFL.txt
var bundledFontLicense string

func BundledFont() ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(compressedFont))
	if err != nil {
		return nil, fmt.Errorf("PDF font archive: %w", err)
	}
	font, readErr := io.ReadAll(io.LimitReader(reader, 32<<20+1))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, fmt.Errorf("PDF font archive: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("PDF font archive: %w", closeErr)
	}
	if len(font) == 0 || len(font) > 32<<20 {
		return nil, ErrTooLarge
	}
	if actual := fmt.Sprintf("%x", sha256.Sum256(font)); actual != bundledFontDigest {
		return nil, errors.New("bundled PDF font checksum mismatch")
	}
	return font, nil
}

func FontLicense() string { return bundledFontLicense }

func completeTrueTypeFont(font []byte) bool {
	const headerSize, entrySize = 12, 16
	if len(font) < headerSize {
		return false
	}
	switch binary.BigEndian.Uint32(font[:4]) {
	case 0x00010000, 0x74727565:
	default:
		return false
	}
	count := int(binary.BigEndian.Uint16(font[4:6]))
	if count == 0 || count > (len(font)-headerSize)/entrySize {
		return false
	}
	directoryEnd := headerSize + count*entrySize
	tables := make(map[string][]byte, count)
	for index := range count {
		entry := font[headerSize+index*entrySize:][:entrySize]
		tag := string(entry[:4])
		start := uint64(binary.BigEndian.Uint32(entry[8:12]))
		size := uint64(binary.BigEndian.Uint32(entry[12:16]))
		end := start + size
		if _, duplicate := tables[tag]; duplicate || end > uint64(len(font)) || size > 0 && start < uint64(directoryEnd) {
			return false
		}
		tables[tag] = font[start:end]
	}
	for tag, minimum := range map[string]int{"name": 6, "head": 54, "hhea": 36, "hmtx": 4, "maxp": 6, "cmap": 4, "post": 16, "loca": 2} {
		if len(tables[tag]) < minimum {
			return false
		}
	}
	glyphs, present := tables["glyf"]
	if !present {
		return false
	}
	glyphCount := int(binary.BigEndian.Uint16(tables["maxp"][4:6]))
	metricCount := int(binary.BigEndian.Uint16(tables["hhea"][34:36]))
	if metricCount == 0 || metricCount > glyphCount || len(tables["hmtx"]) < 2*(glyphCount+metricCount) {
		return false
	}
	if !completePDFCharacterMap(tables["cmap"]) {
		return false
	}
	var locationSize int
	switch binary.BigEndian.Uint16(tables["head"][50:52]) {
	case 0:
		locationSize = 2
	case 1:
		locationSize = 4
	default:
		return false
	}
	locations := tables["loca"]
	if len(locations) < (glyphCount+1)*locationSize {
		return false
	}
	var previous uint64
	for index := range glyphCount + 1 {
		record := locations[index*locationSize:]
		var offset uint64
		if locationSize == 2 {
			offset = uint64(binary.BigEndian.Uint16(record)) * 2
		} else {
			offset = uint64(binary.BigEndian.Uint32(record))
		}
		if offset < previous || offset > uint64(len(glyphs)) {
			return false
		}
		previous = offset
	}
	return true
}

func completePDFCharacterMap(table []byte) bool {
	count := int(binary.BigEndian.Uint16(table[2:4]))
	recordsEnd := 4 + 8*count
	if recordsEnd > len(table) {
		return false
	}
	for position := 4; position < recordsEnd; position += 8 {
		record := table[position : position+8]
		platform := binary.BigEndian.Uint16(record[:2])
		encoding := binary.BigEndian.Uint16(record[2:4])
		if platform != 0 && (platform != 3 || encoding != 1) {
			continue
		}
		start := uint64(binary.BigEndian.Uint32(record[4:8]))
		if start < uint64(recordsEnd) || start+2 > uint64(len(table)) {
			return false
		}
		mapping := table[start:]
		if binary.BigEndian.Uint16(mapping[:2]) != 4 {
			continue
		}
		if len(mapping) < 16 {
			return false
		}
		size := int(binary.BigEndian.Uint16(mapping[2:4]))
		segmentBytes := int(binary.BigEndian.Uint16(mapping[6:8]))
		return segmentBytes > 0 && segmentBytes%2 == 0 && size >= 16+4*segmentBytes && size <= len(mapping)
	}
	return false
}
