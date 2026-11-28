package markdownmedia

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const widthTitle = "markstack-width="

type ImageReference struct {
	Start, End, LabelEnd, DestinationStart, DestinationEnd int
	Destination, Title                                     string
}

type Patch struct {
	Start, End int
	Text       string
}

type imageOffsetParser struct{ parser.InlineParser }

func (capture imageOffsetParser) Parse(parent ast.Node, reader text.Reader, context parser.Context) ast.Node {
	_, before := reader.Position()
	node := capture.InlineParser.Parse(parent, reader, context)
	if image, ok := node.(*ast.Image); ok {
		_, after := reader.Position()
		image.SetAttributeString("markstack-offsets", [2]int{before.Start + 1, after.Start})
	}
	return node
}

var offsetImageParser = newOffsetImageParser()

func newOffsetImageParser() goldmark.Markdown {
	inline := parser.DefaultInlineParsers()
	for index, entry := range inline {
		if entry.Value == parser.NewLinkParser() {
			inline[index] = util.Prioritized(imageOffsetParser{parser.NewLinkParser()}, entry.Priority)
			break
		}
	}
	return goldmark.New(goldmark.WithExtensions(extension.GFM), goldmark.WithParser(parser.NewParser(
		parser.WithBlockParsers(parser.DefaultBlockParsers()...),
		parser.WithParagraphTransformers(parser.DefaultParagraphTransformers()...),
		parser.WithInlineParsers(inline...),
	)))
}

func ImageReferences(source []byte) []ImageReference {
	root := offsetImageParser.Parser().Parse(text.NewReader(source))
	var references []ImageReference
	_ = ast.Walk(root, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		image, imageOK := node.(*ast.Image)
		if !entering || !imageOK {
			return ast.WalkContinue, nil
		}
		attribute, _ := image.AttributeString("markstack-offsets")
		bounds, ok := attribute.([2]int)
		start, labelEnd, end := image.Pos(), bounds[0], bounds[1]
		if !ok || start < 0 || start >= labelEnd || labelEnd > end || end > len(source) {
			return ast.WalkContinue, nil
		}
		ref := ImageReference{Start: start, End: end, LabelEnd: labelEnd, Destination: string(image.Destination), Title: string(image.Title)}
		if image.Reference == nil && labelEnd < end && source[labelEnd] == '(' {
			remaining := source[labelEnd+1 : end]
			trimmed := bytes.TrimLeft(remaining, " \t\r\n")
			offset := end - len(trimmed)
			if len(trimmed) > 0 && trimmed[0] == '<' {
				trimmed, offset = trimmed[1:], offset+1
			}
			if bytes.HasPrefix(trimmed, image.Destination) {
				ref.DestinationStart, ref.DestinationEnd = offset, offset+len(image.Destination)
			}
		}
		references = append(references, ref)
		return ast.WalkContinue, nil
	})
	return references
}

func (ref ImageReference) Rename(destination string) Patch {
	if ref.DestinationEnd > ref.DestinationStart {
		return Patch{Start: ref.DestinationStart, End: ref.DestinationEnd, Text: destination}
	}
	return Patch{Start: ref.LabelEnd, End: ref.End, Text: imageSuffix(destination, ref.Title)}
}

func WidthPercent(title string) int {
	for _, percent := range []int{50, 75, 100} {
		marker := fmt.Sprintf("%s%d%%", widthTitle, percent)
		if title == marker || strings.HasSuffix(title, " "+marker) {
			return percent
		}
	}
	return 100
}

func (ref ImageReference) SetWidth(source string, percent int) Patch {
	title := ref.Title
	marker := ""
	for _, value := range []int{50, 75, 100} {
		candidate := fmt.Sprintf("%s%d%%", widthTitle, value)
		if title == candidate {
			marker = candidate
			title = ""
			break
		} else if trimmed, found := strings.CutSuffix(title, " "+candidate); found {
			marker, title = " "+candidate, trimmed
			break
		}
	}
	annotation := ""
	if percent != 100 {
		annotation = fmt.Sprintf("%s%d%%", widthTitle, percent)
		if title != "" {
			annotation = " " + annotation
		}
	}
	if ref.DestinationEnd > ref.DestinationStart {
		end := ref.DestinationEnd
		if end < ref.End && source[end] == '>' {
			end++
		}
		if ref.Title == "" {
			return Patch{Start: end, End: end, Text: ` "` + annotation + `"`}
		}
		beforeClosing := end + len(strings.TrimRight(source[end:ref.End-1], " \t\r\n")) - 1
		if beforeClosing > end && strings.HasSuffix(source[end:beforeClosing], ref.Title) {
			if title == "" && annotation == "" {
				return Patch{Start: end, End: beforeClosing + 1}
			}
			return Patch{Start: beforeClosing - len(marker), End: beforeClosing, Text: annotation}
		}
		return Patch{Start: end, End: ref.End, Text: imageSuffix("", title+annotation)[1:]}
	}
	return Patch{Start: ref.LabelEnd, End: ref.End, Text: imageSuffix(ref.Destination, title+annotation)}
}

func imageSuffix(destination, title string) string {
	if strings.ContainsAny(destination, " \t()") {
		destination = "<" + destination + ">"
	}
	if title != "" {
		title = ` "` + strings.ReplaceAll(title, `"`, `\"`) + `"`
	}
	return "(" + destination + title + ")"
}
