package exportpdf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func TestRenderPDFImageResourceAliases(t *testing.T) {
	makePNG := func(last color.NRGBA, compression png.CompressionLevel) []byte {
		bitmap := image.NewNRGBA(image.Rect(0, 0, 32, 24))
		for row := range 24 {
			for column := range 32 {
				bitmap.SetNRGBA(column, row, color.NRGBA{R: 90, G: 180, B: 44, A: 190})
			}
		}
		bitmap.SetNRGBA(31, 23, last)
		var buffer bytes.Buffer
		encoder := png.Encoder{CompressionLevel: compression}
		if err := encoder.Encode(&buffer, bitmap); err != nil {
			t.Fatal(err)
		}
		return buffer.Bytes()
	}
	firstColor := color.NRGBA{R: 255, A: 64}
	first := makePNG(firstColor, png.DefaultCompression)
	second := makePNG(color.NRGBA{B: 255, A: 64}, png.DefaultCompression)
	alias := pngWithNote(first)
	recompressed := makePNG(firstColor, png.BestCompression)
	if bytes.Equal(first, recompressed) {
		t.Fatal("different compression did not change the PNG stream")
	}
	if string(first[37:41]) != "IDAT" {
		t.Fatal("fixture must begin image data immediately after IHDR")
	}
	chunkEnd := 41 + int(binary.BigEndian.Uint32(first[33:37]))
	split := appendPNGChunk(bytes.Clone(first[:33]), "IDAT", first[41:42])
	split = appendPNGChunk(split, "IDAT", first[42:chunkEnd])
	split = append(split, first[chunkEnd+4:]...)
	bitmap := image.NewPaletted(image.Rect(0, 0, 20, 16), color.Palette{color.Black, color.White})
	bitmap.SetColorIndex(1, 1, 1)
	var gifAsset, pngAsset, jpegAsset bytes.Buffer
	if err := gif.Encode(&gifAsset, bitmap, nil); err != nil {
		t.Fatal(err)
	}
	widerGIF := bytes.Clone(gifAsset.Bytes())
	binary.LittleEndian.PutUint16(widerGIF[6:8], 21)
	decoded, err := gif.Decode(bytes.NewReader(gifAsset.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(&pngAsset, decoded); err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(&jpegAsset, bitmap, nil); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		paths []string
		data  [][]byte
		want  []int
	}{
		{"png-alias-and-distinct", []string{"first.png", "same.png", "alias.png", "recompressed.png", "split.png", "second.png"}, [][]byte{first, first, alias, recompressed, split, second}, []int{0, 0, 0, 0, 0, 1}},
		{"converted-gif", []string{"converted.png", "first.gif", "same.gif"}, [][]byte{pngAsset.Bytes(), gifAsset.Bytes(), gifAsset.Bytes()}, []int{0, 0, 0}},
		{"gif-canvas-and-png-alias", []string{"alias.png", "wider.gif", "first.gif"}, [][]byte{pngWithNote(pngAsset.Bytes()), widerGIF, gifAsset.Bytes()}, []int{0, 0, 0}},
		{"jpeg", []string{"first.jpg", "same.jpg", "separate.png"}, [][]byte{jpegAsset.Bytes(), jpegAsset.Bytes(), pngAsset.Bytes()}, []int{0, 0, 1}},
	}
	font := testFont(t)
	for _, fixture := range tests {
		t.Run(fixture.name, func(t *testing.T) {
			assets := make(map[string][]byte, len(fixture.paths))
			var source strings.Builder
			source.WriteString("<p>Before images</p><table><tr><td>")
			for index, path := range fixture.paths {
				if index == 1 {
					source.WriteString("</td><td>Neighbor</td></tr></table>")
				}
				assets[path] = fixture.data[index]
				fmt.Fprintf(&source, "<p><img src=%q alt=\"image\"></p>", path)
			}
			source.WriteString("<p>After images</p>")
			result, err := RenderRichHTML([]byte(source.String()), func(path string) ([]byte, error) {
				return assets[path], nil
			}, font)
			if err != nil {
				t.Fatal(err)
			}
			inspected := inspectPDF(t, result)
			var names []string
			for _, page := range inspected.images {
				for _, placement := range page {
					names = append(names, placement.name)
				}
			}
			if len(names) != len(fixture.want) {
				t.Fatalf("got %d image placements, want %d", len(names), len(fixture.want))
			}
			for index, name := range names {
				for previous := 0; previous < index; previous++ {
					if (names[previous] == name) != (fixture.want[previous] == fixture.want[index]) {
						t.Errorf("unexpected resource identity at placements %d and %d", previous, index)
					}
				}
			}
		})
	}
}

func pngWithNote(original []byte) []byte {
	updated := appendPNGChunk(bytes.Clone(original[:33]), "tEXt", []byte("Description\x00a metadata-only alias"))
	return append(updated, original[33:]...)
}

func appendPNGChunk(destination []byte, kind string, data []byte) []byte {
	destination = binary.BigEndian.AppendUint32(destination, uint32(len(data)))
	offset := len(destination)
	destination = append(destination, kind...)
	destination = append(destination, data...)
	return binary.BigEndian.AppendUint32(destination, crc32.ChecksumIEEE(destination[offset:]))
}
