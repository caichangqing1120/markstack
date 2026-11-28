package exportpdf

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestPDFRunningHeaderAndFooter(t *testing.T) {
	const title = "导出测试标题"
	font := testFont(t)
	for _, format := range []struct {
		name, body string
		render     func([]byte, func(string) ([]byte, error), []byte, Options) ([]byte, error)
	}{
		{"markdown", strings.Repeat("Body {nb}.\n\n", 76), RenderMarkdownWithOptions},
		{"rich HTML", strings.Repeat("<p>Body {nb}.</p>", 76), RenderRichHTMLWithOptions},
	} {
		t.Run(format.name, func(t *testing.T) {
			data, err := format.render([]byte(format.body), nil, font, Options{Title: title, Header: true, FooterPageNumbers: true})
			if err != nil {
				t.Fatal(err)
			}
			pages := inspectPDF(t, data).pageTexts
			if len(pages) < 2 {
				t.Fatalf("expected multiple pages, got %d", len(pages))
			}
			for index, texts := range pages {
				footer := fmt.Sprintf("%d / %d", index+1, len(pages))
				if slices.Contains(texts, title) != (index > 0) || !slices.Contains(texts, footer) || !slices.Contains(texts, "Body {nb}.") {
					t.Fatalf("wrong running content on page %d: %q", index+1, texts)
				}
			}
		})
	}
}

func TestPDFPresetsChangeTypographyAndPagination(t *testing.T) {
	font := testFont(t)
	const paragraphs = 126
	source := []byte("# Heading\n\n" + strings.Repeat("Body text.\n\n", paragraphs))
	pageCounts := make(map[string]int)
	for _, preset := range []string{"", "standard", "compact", "academic"} {
		data, err := RenderMarkdownWithOptions(source, nil, font, Options{Preset: preset})
		if err != nil {
			t.Fatal(err)
		}
		pdf := inspectPDF(t, data)
		pageCounts[preset] = len(pdf.pages)
		if strings.Count(strings.Join(slices.Concat(pdf.pageTexts...), " "), "Body text.") != paragraphs {
			t.Fatalf("preset %q lost body text", preset)
		}
	}
	if pageCounts[""] != pageCounts["standard"] || pageCounts["compact"] >= pageCounts["standard"] || pageCounts["academic"] <= pageCounts["standard"] {
		t.Fatalf("unexpected pagination: %+v", pageCounts)
	}
}

func TestPDFInlineTOCUsesFinalPagesAndHeadingDestinations(t *testing.T) {
	var source strings.Builder
	source.WriteString("# Start\n\nBefore contents.\n\n[tOc]\n\nAfter contents.\n\n")
	titles := []string{"Start"}
	for index := 1; index <= 58; index++ {
		title := fmt.Sprintf("Section %03d", index)
		fmt.Fprintf(&source, "%s %s\n\n", strings.Repeat("#", 2+(index-1)%5), title)
		titles = append(titles, title)
	}
	source.WriteString(strings.Repeat("Filler.\n\n", 30) + "## Energy $E=mc^2$\n")
	titles = append(titles, "Energy E=mc^2")
	data, err := RenderMarkdownWithOptions([]byte(source.String()), nil, testFont(t), Options{Preset: "academic"})
	if err != nil {
		t.Fatal(err)
	}
	pdf := inspectPDF(t, data)
	outlines := make(map[string]int)
	var collect func([]pdfOutlineEntry)
	collect = func(entries []pdfOutlineEntry) {
		for _, entry := range entries {
			outlines[entry.title] = entry.page
			collect(entry.children)
		}
	}
	collect(parsePDFOutlines(t, data))
	if len(outlines) != len(titles) || outlines[titles[len(titles)-1]] < 3 {
		t.Fatalf("unexpected outline or layout: %+v", outlines)
	}
	if got := pdf.pageTexts[0]; !slices.Equal(got[:min(4, len(got))], []string{"Start", "Before contents.", "目录 / Table of Contents", "Start"}) {
		t.Fatalf("contents were not inserted inline: %q", got[:min(4, len(got))])
	}
	annotation := regexp.MustCompile(`/Subtype /Link /Rect \[([\d.]+) ([\d.]+) [\d.]+ [\d.]+\] /Border \[0 0 0\] /Dest \[(\d+) 0 R /XYZ 0 [\d.]+ null\]`)
	entry := 0
	var previousIndent float64
	for index, texts := range pdf.pageTexts {
		links := annotation.FindAllStringSubmatch(pdf.objects[3+2*index].dictionary, -1)
		if len(links) == 0 {
			continue
		}
		printed := texts[len(texts)-len(links):]
		for offset, link := range links {
			var object int
			var indent float64
			fmt.Sscan(link[3], &object)
			fmt.Sscan(link[1], &indent)
			if entry >= len(titles) {
				t.Fatal("extra TOC links")
			}
			title := titles[entry]
			page := outlines[title]
			if object != 1+2*page || printed[offset] != fmt.Sprint(page) || !slices.Contains(texts, title) {
				t.Errorf("TOC %q: destination object %d, printed %q, want page %d", title, object, printed[offset], page)
			}
			if title != "Energy E=mc^2" && !slices.Contains(pdf.pageTexts[page-1], title) {
				t.Errorf("heading %q missing on page %d", title, page)
			}
			if entry > 0 && entry <= 5 && indent <= previousIndent {
				t.Errorf("heading %q not indented deeper than its parent", title)
			}
			previousIndent = indent
			entry++
		}
	}
	if entry != len(titles) || len(annotation.FindAllStringSubmatch(pdf.objects[5].dictionary, -1)) == 0 {
		t.Fatalf("got %d TOC links for %d headings or no TOC continuation", entry, len(titles))
	}
}

func TestPDFTOCOnlyActivatesStandaloneMarkdownParagraphs(t *testing.T) {
	source := "# Heading\n\nBefore [TOC]\n\n[TOC]\nand after\n\n`[TOC]`\n\n\\[TOC]\n\n> [TOC]\n\n```\n[TOC]\n```\n\n<p>[TOC]</p>\n"
	font := testFont(t)
	for _, sample := range []struct {
		body   string
		render func([]byte, func(string) ([]byte, error), []byte) ([]byte, error)
	}{
		{source, RenderMarkdown},
		{"<h1>Heading</h1><p>[TOC]</p>", RenderRichHTML},
	} {
		data, err := sample.render([]byte(sample.body), nil, font)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "/Subtype /Link") {
			t.Fatalf("generated TOC links for literal marker in %q", sample.body)
		}
	}
}
