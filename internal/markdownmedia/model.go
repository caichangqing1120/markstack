package markdownmedia

import (
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

type Image struct {
	Start, End  int
	Alt         string
	Destination string
}

type Part struct {
	Text, Alt, Destination string
}

type Paragraph struct {
	Start, End int
	Parts      []Part
}

func MixedParagraphs(source []byte) []Paragraph {
	if !utf8.Valid(source) || len(source) > 16<<20 {
		return nil
	}
	root := imageParser.Parser().Parse(text.NewReader(source))
	var paragraphs []Paragraph
	for node := root.FirstChild(); node != nil; node = node.NextSibling() {
		paragraph, ok := node.(*ast.Paragraph)
		if !ok || paragraph.Lines().Len() != 1 {
			continue
		}
		segment := paragraph.Lines().At(0)
		if segment.Padding != 0 || segment.Start < 0 || segment.Stop > len(source) || segment.Start >= segment.Stop || segment.Stop-segment.Start > 32<<10 {
			continue
		}
		candidate := Paragraph{Start: segment.Start, End: segment.Stop}
		images, valid := 0, true
		for child := paragraph.FirstChild(); child != nil; child = child.NextSibling() {
			switch typed := child.(type) {
			case *ast.Text:
				part := typed.Segment
				if typed.IsRaw() || typed.SoftLineBreak() || typed.HardLineBreak() || part.Padding != 0 || part.Start < segment.Start || part.Stop > segment.Stop || part.Start >= part.Stop {
					valid = false
					break
				}
				value := string(part.Value(source))
				if strings.IndexByte(value, '\\') >= 0 {
					valid = false
					break
				}
				if len(candidate.Parts) > 0 && candidate.Parts[len(candidate.Parts)-1].Destination == "" {
					candidate.Parts[len(candidate.Parts)-1].Text += value
				} else {
					candidate.Parts = append(candidate.Parts, Part{Text: value})
				}
			case *ast.Image:
				if len(typed.Destination) == 0 {
					valid = false
					break
				}
				for alt := typed.FirstChild(); alt != nil; alt = alt.NextSibling() {
					if _, ok := alt.(*ast.Text); !ok {
						valid = false
						break
					}
				}
				if !valid {
					break
				}
				candidate.Parts = append(candidate.Parts, Part{Alt: string(typed.Text(source)), Destination: string(typed.Destination)})
				images++
			default:
				valid = false
			}
			if !valid || len(candidate.Parts) > 64 {
				break
			}
		}
		if valid && images > 0 && len(candidate.Parts) > 1 {
			paragraphs = append(paragraphs, candidate)
		}
	}
	return paragraphs
}

var imageParser = goldmark.New(goldmark.WithExtensions(extension.GFM))

func StandaloneImages(source []byte) []Image {
	if !utf8.Valid(source) {
		return nil
	}
	root := imageParser.Parser().Parse(text.NewReader(source))
	var images []Image
	for node := root.FirstChild(); node != nil; node = node.NextSibling() {
		paragraph, ok := node.(*ast.Paragraph)
		if !ok || paragraph.Lines().Len() != 1 {
			continue
		}
		image, ok := paragraph.FirstChild().(*ast.Image)
		if !ok || image.NextSibling() != nil {
			continue
		}
		segment := paragraph.Lines().At(0)
		if segment.Padding != 0 || segment.Start < 0 || segment.Stop > len(source) || segment.Start >= segment.Stop {
			continue
		}
		images = append(images, Image{
			Start: segment.Start, End: segment.Stop,
			Alt: string(image.Text(source)), Destination: string(image.Destination),
		})
	}
	return images
}
