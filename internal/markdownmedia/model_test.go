package markdownmedia

import (
	"strings"
	"testing"
)

func TestStandaloneImagesUseASTAndExactUTF8SourceIntervals(t *testing.T) {
	source := "# Heading\r\n\r\n![中文图](assets/图.png)\r\n\r\ntext ![inline](assets/a.png)\r\n\r\n\\![escaped](assets/no.png)\r\n\r\n![remote](https://example.org/p.png)\r\n\r\n![traversal](../outside.png)\r\n\r\n![again](assets/a.png)\r\n"
	images := StandaloneImages([]byte(source))
	if len(images) != 4 {
		t.Fatalf("standalone image count=%d: %+v", len(images), images)
	}
	for index, expected := range []struct{ raw, alt, destination string }{
		{"![中文图](assets/图.png)", "中文图", "assets/图.png"},
		{"![remote](https://example.org/p.png)", "remote", "https://example.org/p.png"},
		{"![traversal](../outside.png)", "traversal", "../outside.png"},
		{"![again](assets/a.png)", "again", "assets/a.png"},
	} {
		image := images[index]
		if image.Alt != expected.alt || image.Destination != expected.destination || image.Start != strings.Index(source, expected.raw) || image.End != image.Start+len(expected.raw) {
			t.Fatalf("image %d=%+v, want %q", index, image, expected.raw)
		}
	}
}

func TestStandaloneImagesRejectAmbiguousNestedAndMalformedParagraphs(t *testing.T) {
	source := []byte("![**styled**](assets/a.png) text\n\n```md\n![code](assets/b.png)\n```\n\n![broken](assets/c.png\n\n![one](assets/1.png) ![two](assets/2.png)\n")
	if images := StandaloneImages(source); len(images) != 0 {
		t.Fatalf("ambiguous image paragraph became a bitmap: %+v", images)
	}
	if images := StandaloneImages([]byte{0xff, '!', '[', ']'}); len(images) != 0 {
		t.Fatalf("invalid UTF-8 image source=%+v", images)
	}
}
