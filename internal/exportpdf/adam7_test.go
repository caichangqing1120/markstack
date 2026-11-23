package exportpdf

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"regexp"
	"strings"
	"testing"

	"github.com/go-pdf/fpdf"
	htmlnode "golang.org/x/net/html"
)

func TestPrepareAdam7ImageReusesRegistration(t *testing.T) {
	asset := adam7TestPNG(t, image.NewGray(image.Rect(0, 0, 192, 96)), 8, 0)
	source := "data:image/png;base64," + base64.StdEncoding.EncodeToString(asset)
	first := &htmlnode.Node{Attr: []htmlnode.Attribute{{Key: "src", Val: source}}}
	repeated := &htmlnode.Node{Attr: []htmlnode.Attribute{{Key: "src", Val: strings.Clone(source)}}}
	printer := pageRenderer{pdf: fpdf.New("P", "mm", "A4", "")}
	initial, err := printer.prepareImage(first, 40, 50)
	if err != nil || initial.width != 40 || initial.height != 20 {
		t.Fatalf("initial image: %+v, %v", initial, err)
	}
	allocations := testing.AllocsPerRun(5, func() {
		prepared, err := printer.prepareImage(repeated, 30, 10)
		if err != nil || prepared.name != initial.name || prepared.width != 20 || prepared.height != 10 {
			t.Fatalf("reused image: %+v, %v", prepared, err)
		}
	})
	if allocations > 2 {
		t.Fatalf("repeated image took %.0f allocations; expected reuse without decoding or encoding", allocations)
	}
}

func TestRenderAdam7PNG(t *testing.T) {
	font := testFont(t)
	bounds := image.Rect(0, 0, 17, 15)
	gray := image.NewGray(bounds)
	rgba := image.NewNRGBA(bounds)
	rgb16 := image.NewNRGBA64(bounds)
	rgba16 := image.NewNRGBA64(bounds)
	palette := color.Palette{color.NRGBA{R: 35, A: 0}, color.NRGBA{R: 199, G: 52, A: 93}, color.NRGBA{B: 170, A: 0}, color.NRGBA{B: 221, A: 255}}
	indexed := image.NewPaletted(bounds, palette)
	indexedControl := image.NewNRGBA(bounds)
	for row := range bounds.Dy() {
		for column := range bounds.Dx() {
			value := uint8(11*column + 7*row)
			gray.SetGray(column, row, color.Gray{Y: value})
			rgba.SetNRGBA(column, row, color.NRGBA{R: value, G: 255 - value, B: uint8(9 * column), A: uint8(17 * row)})
			pixel16 := color.NRGBA64{R: 257*uint16(value) + 13, G: uint16(4027 * column), B: uint16(4333 * row), A: 65535}
			rgb16.SetNRGBA64(column, row, pixel16)
			pixel16.A = 32769
			rgba16.SetNRGBA64(column, row, pixel16)
			index := (column + row) % len(palette)
			indexed.SetColorIndex(column, row, uint8(index))
			indexedControl.SetNRGBA(column, row, palette[index].(color.NRGBA))
		}
	}
	fixtures := []struct {
		name            string
		bitmap, control image.Image
		depth, kind     byte
		alpha           bool
	}{
		{"gray8", gray, gray, 8, 0, false},
		{"rgba8", rgba, rgba, 8, 6, true},
		{"palette-alpha", indexed, indexedControl, 8, 3, true},
		{"rgb16", rgb16, rgb16, 16, 2, false},
		{"rgba16", rgba16, rgba16, 16, 6, true},
	}
	for _, fixture := range fixtures {
		asset := adam7TestPNG(t, fixture.bitmap, fixture.depth, fixture.kind)
		var control bytes.Buffer
		if err := png.Encode(&control, fixture.control); err != nil {
			t.Fatal(err)
		}
		assertSamePNGTestPixels(t, asset, control.Bytes())
		for _, document := range []struct {
			kind, source string
			render       func([]byte, func(string) ([]byte, error), []byte) ([]byte, error)
		}{
			{"markdown", "# Images\n\n![First](sample.png)\n\n![Second](sample.png)\n\nDocument tail.\n", RenderMarkdown},
			{"rich-table", `<p>Images</p><table><tr><td><img src="sample.png" alt="First"></td><td>Neighbor</td></tr></table><p><img src="sample.png" alt="Second"></p><p>Document tail.</p>`, RenderRichHTML},
		} {
			t.Run(fixture.name+"/"+document.kind, func(t *testing.T) {
				source := []byte(document.source)
				original := bytes.Clone(asset)
				export := func(data []byte) ([]byte, error) {
					return document.render(source, func(path string) ([]byte, error) {
						if path != "sample.png" {
							t.Fatalf("unexpected asset path %q", path)
						}
						return data, nil
					}, font)
				}
				interlaced, err := export(asset)
				if string(source) != document.source || !bytes.Equal(asset, original) {
					t.Fatal("export changed source or asset bytes")
				}
				if fixture.depth == 16 && fixture.alpha {
					if err == nil || !strings.Contains(err.Error(), "16-bit transparency") || len(interlaced) != 0 {
						t.Fatalf("expected explicit refusal of Adam7 16-bit transparency, got %d bytes, %v", len(interlaced), err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				nonInterlaced, err := export(control.Bytes())
				if err != nil {
					t.Fatal(err)
				}
				actual := adam7PDFImageObjects(t, interlaced, bounds.Size(), fixture.depth, fixture.alpha, fixture.kind == 0)
				expected := adam7PDFImageObjects(t, nonInterlaced, bounds.Size(), fixture.depth, fixture.alpha, fixture.kind == 0)
				for index := range actual {
					if !bytes.Equal(actual[index], expected[index]) {
						t.Fatalf("PDF image stream %d differs from the non-interlaced control", index)
					}
				}
				label := fixture.name + "-" + document.kind
				savePDFArtifact(t, "Adam7-"+label+".pdf", interlaced)
				savePDFArtifact(t, "Adam7-"+label+"-control.pdf", nonInterlaced)
			})
		}
	}
	malformed := adam7TestPNG(t, gray, 8, 0)
	malformed[len(malformed)-1] ^= 1
	if _, err := png.Decode(bytes.NewReader(malformed)); err == nil {
		t.Fatal("bad fixture: malformed PNG passed decoding")
	}
	result, err := RenderMarkdown([]byte("![Broken](bad.png)\n\nDocument tail.\n"), func(string) ([]byte, error) {
		return malformed, nil
	}, font)
	if err != nil {
		t.Fatal(err)
	}
	inspected := inspectPDF(t, result)
	if len(inspected.images) != 1 || len(inspected.images[0]) != 0 || !strings.Contains(strings.Join(inspected.pageTexts[0], ""), "Document tail.") {
		t.Fatal("malformed Adam7 image was embedded or prevented text export")
	}
}

func adam7TestPNG(t *testing.T, bitmap image.Image, depth, kind byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	passes := [][4]int{{0, 0, 8, 8}, {4, 0, 8, 8}, {0, 4, 4, 8}, {2, 0, 4, 4}, {0, 2, 2, 4}, {1, 0, 2, 2}, {0, 1, 1, 2}}
	for _, pass := range passes {
		for row := pass[1]; row < bitmap.Bounds().Dy(); row += pass[3] {
			line := []byte{0}
			for column := pass[0]; column < bitmap.Bounds().Dx(); column += pass[2] {
				switch bitmap := bitmap.(type) {
				case *image.Gray:
					line = append(line, bitmap.GrayAt(column, row).Y)
				case *image.NRGBA:
					pixel := bitmap.NRGBAAt(column, row)
					line = append(line, pixel.R, pixel.G, pixel.B, pixel.A)
				case *image.Paletted:
					line = append(line, bitmap.ColorIndexAt(column, row))
				case *image.NRGBA64:
					pixel := bitmap.NRGBA64At(column, row)
					line = binary.BigEndian.AppendUint16(line, pixel.R)
					line = binary.BigEndian.AppendUint16(line, pixel.G)
					line = binary.BigEndian.AppendUint16(line, pixel.B)
					if kind == 6 {
						line = binary.BigEndian.AppendUint16(line, pixel.A)
					}
				default:
					t.Fatalf("unhandled test fixture %T", bitmap)
				}
			}
			if _, err := writer.Write(line); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	header := binary.BigEndian.AppendUint32(nil, uint32(bitmap.Bounds().Dx()))
	header = binary.BigEndian.AppendUint32(header, uint32(bitmap.Bounds().Dy()))
	header = append(header, depth, kind, 0, 0, 1)
	data := appendPNGChunk([]byte("\x89PNG\r\n\x1a\n"), "IHDR", header)
	if indexed, ok := bitmap.(*image.Paletted); ok {
		var rgb, alpha []byte
		for _, entry := range indexed.Palette {
			pixel := entry.(color.NRGBA)
			rgb = append(rgb, pixel.R, pixel.G, pixel.B)
			alpha = append(alpha, pixel.A)
		}
		data = appendPNGChunk(data, "PLTE", rgb)
		data = appendPNGChunk(data, "tRNS", alpha)
	}
	data = appendPNGChunk(data, "IDAT", compressed.Bytes())
	return appendPNGChunk(data, "IEND", nil)
}

func assertSamePNGTestPixels(t *testing.T, first, second []byte) {
	t.Helper()
	actual, err := png.Decode(bytes.NewReader(first))
	if err != nil {
		t.Fatal(err)
	}
	expected, err := png.Decode(bytes.NewReader(second))
	if err != nil {
		t.Fatal(err)
	}
	if actual.Bounds() != expected.Bounds() {
		t.Fatal("mismatched PNG bounds")
	}
	for row := range actual.Bounds().Dy() {
		for column := range actual.Bounds().Dx() {
			red, green, blue, alpha := actual.At(column, row).RGBA()
			wantRed, wantGreen, wantBlue, wantAlpha := expected.At(column, row).RGBA()
			if red != wantRed || green != wantGreen || blue != wantBlue || alpha != wantAlpha {
				t.Fatalf("decoded PNG pixels differ at (%d, %d)", column, row)
			}
		}
	}
}

func adam7PDFImageObjects(t *testing.T, data []byte, size image.Point, depth byte, alpha, gray bool) [][]byte {
	t.Helper()
	inspected := inspectPDF(t, data)
	if len(inspected.images) != 1 || len(inspected.images[0]) != 2 || inspected.images[0][0].name != inspected.images[0][1].name {
		t.Fatalf("expected two placements of a single image resource, got %+v", inspected.images)
	}
	name := inspected.images[0][0].name
	resource := regexp.MustCompile(`/` + regexp.QuoteMeta(name) + `\s+(\d+)\s+0\s+R\b`)
	maskRef := regexp.MustCompile(`/SMask\s+(\d+)\s+0\s+R\b`)
	var imageID, imageCount int
	for _, object := range inspected.objects {
		if strings.Contains(object.dictionary, "/Subtype /Image") {
			imageCount++
		}
		for _, resources := range pdfXObjectDict.FindAllStringSubmatch(object.dictionary, -1) {
			if match := resource.FindStringSubmatch(resources[1]); match != nil {
				fmt.Sscanf(match[1], "%d", &imageID)
			}
		}
	}
	verify := func(id int, bits byte, channels int, space string) []byte {
		object := inspected.objects[id]
		for _, entry := range []string{"/Subtype /Image", fmt.Sprintf("/Width %d\n", size.X), fmt.Sprintf("/Height %d\n", size.Y), "/ColorSpace /" + space + "\n", fmt.Sprintf("/BitsPerComponent %d\n", bits), fmt.Sprintf("/Predictor 15 /Colors %d /BitsPerComponent %d /Columns %d", channels, bits, size.X)} {
			if !strings.Contains(object.dictionary, entry) {
				t.Fatalf("image resource %d missing %q: %s", id, entry, object.dictionary)
			}
		}
		if want := size.Y * (1 + size.X*channels*int(bits)/8); len(object.decoded) != want {
			t.Fatalf("image resource %d decoded length %d, want %d", id, len(object.decoded), want)
		}
		return object.decoded
	}
	channels, space := 3, "DeviceRGB"
	if gray {
		channels, space = 1, "DeviceGray"
	}
	streams := [][]byte{verify(imageID, depth, channels, space)}
	mask := maskRef.FindStringSubmatch(inspected.objects[imageID].dictionary)
	if alpha {
		if imageCount != 2 || mask == nil {
			t.Fatal("expected one color image and its soft mask")
		}
		var maskID int
		fmt.Sscanf(mask[1], "%d", &maskID)
		streams = append(streams, verify(maskID, 8, 1, "DeviceGray"))
	} else if imageCount != 1 || mask != nil {
		t.Fatal("opaque image should not have a soft mask or duplicate resources")
	}
	return streams
}
