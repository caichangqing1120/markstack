package markdownmedia

import (
	"reflect"
	"strings"
	"testing"
)

func TestMixedParagraphsKeepUnicodeTextAndTwoImagesInSourceOrder(t *testing.T) {
	line := "前文 ![图一](assets/一.png) 中间 ![图二](assets/二.png) 后文"
	source := "# 标题\r\n\r\n" + line + "\r\n\r\n尾段"
	paragraphs := MixedParagraphs([]byte(source))
	want := []Paragraph{{
		Start: strings.Index(source, line), End: strings.Index(source, line) + len(line),
		Parts: []Part{
			{Text: "前文 "}, {Alt: "图一", Destination: "assets/一.png"},
			{Text: " 中间 "}, {Alt: "图二", Destination: "assets/二.png"}, {Text: " 后文"},
		},
	}}
	if !reflect.DeepEqual(paragraphs, want) {
		t.Fatalf("mixed paragraphs=%+v, want %+v", paragraphs, want)
	}
}

func TestMixedParagraphsRetainRemoteDestinationForSafeFallback(t *testing.T) {
	source := []byte("before ![remote](https://example.org/x.png) after\n\n![one](assets/a.png) ![two](assets/b.png)\n")
	paragraphs := MixedParagraphs(source)
	if len(paragraphs) != 2 || len(paragraphs[0].Parts) != 3 || paragraphs[0].Parts[1].Destination != "https://example.org/x.png" || len(paragraphs[1].Parts) != 3 {
		t.Fatalf("remote/two-image paragraphs=%+v", paragraphs)
	}
}

func TestMixedParagraphsRefuseStyledEscapedLinksAndMultiline(t *testing.T) {
	for _, source := range []string{
		`before **bold** ![image](assets/p.png) after`,
		`before [![image](assets/p.png)](https://example.org) after`,
		`before \!literal ![image](assets/p.png) after`,
		`before ![**styled**](assets/p.png) after`,
		"before\n![image](assets/p.png) after",
	} {
		if got := MixedParagraphs([]byte(source)); len(got) != 0 {
			t.Errorf("unsupported %q became a mixed bitmap: %+v", source, got)
		}
	}
	if got := MixedParagraphs([]byte{0xff, '!', '[', ']'}); len(got) != 0 {
		t.Fatalf("invalid UTF-8 became media: %+v", got)
	}
}

func TestMixedParagraphsBoundSingleParagraphLayoutInput(t *testing.T) {
	text := strings.Repeat("中", 11<<10)
	source := []byte(text + " ![图](assets/p.png) 后文")
	if got := MixedParagraphs(source); len(got) != 0 {
		t.Fatalf("unbounded mixed paragraph entered native row layout: %d parts", len(got[0].Parts))
	}
	image := " ![图](assets/p.png)"
	exact := []byte(strings.Repeat("a", 32<<10-len(image)) + image)
	paragraphs := MixedParagraphs(exact)
	if len(paragraphs) != 1 || paragraphs[0].End-paragraphs[0].Start != 32<<10 || len(paragraphs[0].Parts) != 2 || paragraphs[0].Parts[1].Destination != "assets/p.png" {
		t.Fatalf("paragraph exactly at the 32 KiB budget = %+v", paragraphs)
	}
	over := []byte(strings.Repeat("a", 32<<10-len(image)+1) + image)
	if got := MixedParagraphs(over); len(got) != 0 {
		t.Fatalf("paragraph one byte over the 32 KiB budget entered native row layout: %+v", got)
	}
}
