package exportpdf

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/go-pdf/fpdf"
	htmlnode "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"markstack/internal/exporthtml"
)

var (
	ErrFontRequired = errors.New("PDF export requires a Unicode TrueType font")
	ErrTooLarge     = errors.New("PDF export is too large")
	ErrLayout       = errors.New("PDF content cannot fit on a page")
)

const (
	pageWidth    = 210.0
	pageHeight   = 297.0
	pageMargin   = 18.0
	contentWidth = pageWidth - 2*pageMargin
	maxPDFBytes  = 64 << 20
)

func RenderMarkdown(source []byte, loadImage func(string) ([]byte, error), font []byte) ([]byte, error) {
	safe, err := exporthtml.RenderMarkdown(source, loadImage)
	if err != nil {
		return nil, err
	}
	return render(safe, font)
}

func RenderRichHTML(source []byte, loadImage func(string) ([]byte, error), font []byte) ([]byte, error) {
	safe, err := exporthtml.RenderRichHTML(source, loadImage)
	if err != nil {
		return nil, err
	}
	return render(safe, font)
}

type pageRenderer struct {
	pdf    *fpdf.Fpdf
	images int
}

type preparedImage struct {
	name          string
	width, height float64
	options       fpdf.ImageOptions
}

type tableCell struct {
	value      string
	textHeight float64
	images     []preparedImage
}

type inlineStyle struct {
	bold, italic, code bool
	link               string
}

type inlineGlyph struct {
	value rune
	style inlineStyle
}

type inlineFlow struct {
	printer   *pageRenderer
	line      []inlineGlyph
	width     float64
	lastBreak int
	content   bool
}

func render(safeHTML, font []byte) ([]byte, error) {
	if len(font) == 0 {
		return nil, ErrFontRequired
	}
	document, err := htmlnode.Parse(bytes.NewReader(safeHTML))
	if err != nil {
		return nil, err
	}
	main := findElement(document, atom.Main)
	if main == nil {
		return nil, errors.New("sanitized HTML has no main element")
	}
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(pageMargin, pageMargin, pageMargin)
	pdf.SetAutoPageBreak(true, pageMargin)
	pdf.SetTitle("墨栈导出", true)
	pdf.SetCreator("MarkStack", true)
	pdf.AddUTF8FontFromBytes("Unicode", "", font)
	if err := pdf.Error(); err != nil {
		return nil, fmt.Errorf("PDF font: %w", err)
	}
	pdf.AddPage()
	printer := pageRenderer{pdf: pdf}
	for child := main.FirstChild; child != nil; child = child.NextSibling {
		if err := printer.block(child, 0); err != nil {
			return nil, err
		}
	}
	for sibling := main.NextSibling; sibling != nil; sibling = sibling.NextSibling {
		if sibling.Type == htmlnode.ElementNode && sibling.DataAtom == atom.Section {
			if err := printer.block(sibling, 0); err != nil {
				return nil, err
			}
		}
	}
	var output bytes.Buffer
	if err := pdf.Output(&output); err != nil {
		return nil, err
	}
	if output.Len() > maxPDFBytes {
		return nil, ErrTooLarge
	}
	return output.Bytes(), nil
}

func findElement(node *htmlnode.Node, kind atom.Atom) *htmlnode.Node {
	if node.Type == htmlnode.ElementNode && node.DataAtom == kind {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findElement(child, kind); found != nil {
			return found
		}
	}
	return nil
}

func visible(node *htmlnode.Node) string {
	if node.Type == htmlnode.TextNode {
		return node.Data
	}
	if node.DataAtom == atom.Br {
		return "\n"
	}
	var text strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		text.WriteString(visible(child))
	}
	return text.String()
}

func (printer *pageRenderer) text(value string, size, lineHeight, after float64) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	printer.pdf.SetFont("Unicode", "", size)
	printer.pdf.SetTextColor(41, 50, 56)
	printer.pdf.SetX(pageMargin)
	printer.pdf.MultiCell(contentWidth, lineHeight, value, "", "L", false)
	printer.pdf.Ln(after)
}

func (printer *pageRenderer) block(node *htmlnode.Node, depth int) error {
	if node.Type != htmlnode.ElementNode {
		return nil
	}
	switch node.DataAtom {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		sizes := []float64{24, 19, 16, 14, 12, 11}
		size := sizes[int(node.Data[1]-'1')]
		lineHeight := size * 0.48
		if next := node.NextSibling; next != nil && next.Type == htmlnode.ElementNode && next.DataAtom == atom.Pre && next.FirstChild != nil {
			if printer.pdf.GetY()+3+lineHeight+2+5 > pageHeight-pageMargin {
				printer.pdf.AddPage()
			}
		}
		printer.pdf.Ln(3)
		printer.text(visible(node), size, lineHeight, 2)
	case atom.P:
		if err := printer.paragraph(node); err != nil {
			return err
		}
	case atom.Ul, atom.Ol:
		number := 1
		if node.DataAtom == atom.Ol {
			for _, attribute := range node.Attr {
				if attribute.Key == "start" {
					if start, err := strconv.Atoi(attribute.Val); err == nil && start > 0 {
						number = start
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if child.DataAtom != atom.Li {
				continue
			}
			prefix := "• "
			if node.DataAtom == atom.Ol {
				prefix = strconv.Itoa(number) + ". "
				number++
			}
			printer.pdf.SetFont("Unicode", "", 11)
			printer.pdf.SetX(pageMargin + float64(depth)*6)
			printer.pdf.MultiCell(contentWidth-float64(depth)*6, 6, prefix+strings.TrimSpace(visible(child)), "", "L", false)
		}
		printer.pdf.Ln(3)
	case atom.Blockquote:
		printer.pdf.SetFont("Unicode", "", 11)
		printer.pdf.SetTextColor(88, 100, 106)
		firstPage, firstY := printer.pdf.PageNo(), printer.pdf.GetY()
		printer.pdf.SetX(pageMargin + 7)
		printer.pdf.MultiCell(contentWidth-7, 6, strings.TrimSpace(visible(node)), "", "L", false)
		lastPage, lastY := printer.pdf.PageNo(), printer.pdf.GetY()
		printer.pdf.SetDrawColor(118, 162, 182)
		printer.pdf.SetLineWidth(0.7)
		for page := firstPage; page <= lastPage; page++ {
			printer.pdf.SetPage(page)
			top, bottom := pageMargin, pageHeight-pageMargin
			if page == firstPage {
				top = firstY
			}
			if page == lastPage {
				bottom = lastY
			}
			printer.pdf.Line(pageMargin+1, top, pageMargin+1, bottom)
		}
		printer.pdf.SetPage(lastPage)
		printer.pdf.SetXY(pageMargin, lastY)
		printer.pdf.Ln(3)
	case atom.Pre:
		printer.pdf.SetFont("Unicode", "", 9)
		printer.pdf.SetFillColor(243, 245, 246)
		printer.pdf.SetX(pageMargin)
		printer.pdf.MultiCell(contentWidth, 5, strings.TrimRight(visible(node), "\n"), "", "L", true)
		printer.pdf.Ln(3)
	case atom.Table:
		return printer.table(node)
	case atom.Img:
		return printer.image(node)
	case atom.Hr:
		printer.pdf.Ln(3)
		y := printer.pdf.GetY()
		printer.pdf.SetDrawColor(220, 226, 230)
		printer.pdf.SetLineWidth(0.2)
		printer.pdf.Line(pageMargin, y, pageWidth-pageMargin, y)
		printer.pdf.Ln(5)
	default:
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := printer.block(child, depth); err != nil {
				return err
			}
		}
	}
	return printer.pdf.Error()
}

func (printer *pageRenderer) paragraph(node *htmlnode.Node) error {
	plain := true
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != htmlnode.TextNode && child.DataAtom != atom.Br {
			plain = false
			break
		}
	}
	if plain {
		printer.text(visible(node), 11, 6, 3)
		return printer.pdf.Error()
	}
	printer.pdf.SetFont("Unicode", "", 11)
	flow := inlineFlow{printer: printer}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if err := flow.collect(child, inlineStyle{}); err != nil {
			return err
		}
	}
	flow.flush(false)
	if flow.content {
		printer.pdf.Ln(3)
	}
	return printer.pdf.Error()
}

func (flow *inlineFlow) collect(node *htmlnode.Node, style inlineStyle) error {
	if node.Type == htmlnode.TextNode {
		for _, value := range node.Data {
			flow.add(value, style)
		}
		return nil
	}
	if node.DataAtom == atom.Br {
		flow.flush(true)
		return nil
	}
	if node.DataAtom == atom.Img {
		flow.flush(false)
		flow.content = true
		return flow.printer.image(node)
	}
	switch node.DataAtom {
	case atom.Strong:
		style.bold = true
	case atom.Em:
		style.italic = true
	case atom.Code:
		style.code = true
	case atom.A:
		for _, attribute := range node.Attr {
			if attribute.Key == "href" && safePDFLink(attribute.Val) {
				style.link = attribute.Val
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if err := flow.collect(child, style); err != nil {
			return err
		}
	}
	return nil
}

func (flow *inlineFlow) add(value rune, style inlineStyle) {
	if value == '\n' {
		flow.flush(true)
		return
	}
	if unicode.IsSpace(value) && len(flow.line) == 0 {
		return
	}
	width := flow.printer.pdf.GetStringWidth(string(value))
	for len(flow.line) > 0 && flow.width+width > contentWidth {
		split := flow.lastBreak
		if split == 0 {
			split = len(flow.line)
		}
		flow.draw(flow.line[:split])
		flow.line = flow.line[split:]
		for len(flow.line) > 0 && unicode.IsSpace(flow.line[0].value) {
			flow.line = flow.line[1:]
		}
		flow.remeasure()
	}
	flow.line = append(flow.line, inlineGlyph{value: value, style: style})
	flow.width += width
	if unicode.IsSpace(value) || unicode.Is(unicode.Han, value) {
		flow.lastBreak = len(flow.line)
	}
	flow.content = true
}

func (flow *inlineFlow) remeasure() {
	flow.width, flow.lastBreak = 0, 0
	for index, glyph := range flow.line {
		flow.width += flow.printer.pdf.GetStringWidth(string(glyph.value))
		if unicode.IsSpace(glyph.value) || unicode.Is(unicode.Han, glyph.value) {
			flow.lastBreak = index + 1
		}
	}
}

func (flow *inlineFlow) flush(force bool) {
	if len(flow.line) > 0 {
		flow.draw(flow.line)
		flow.line = flow.line[:0]
		flow.width, flow.lastBreak = 0, 0
	} else if force {
		flow.printer.pdf.Ln(6)
	}
	if force {
		flow.content = true
	}
}

func (flow *inlineFlow) draw(line []inlineGlyph) {
	for len(line) > 0 && unicode.IsSpace(line[len(line)-1].value) {
		line = line[:len(line)-1]
	}
	if flow.printer.pdf.GetY()+6 > pageHeight-pageMargin {
		flow.printer.pdf.AddPage()
	}
	flow.printer.pdf.SetX(pageMargin)
	for start := 0; start < len(line); {
		end := start + 1
		for end < len(line) && line[end].style == line[start].style {
			end++
		}
		var value strings.Builder
		for _, glyph := range line[start:end] {
			value.WriteRune(glyph.value)
		}
		flow.printer.segment(value.String(), line[start].style)
		start = end
	}
	flow.printer.pdf.Ln(6)
}

func (printer *pageRenderer) segment(value string, style inlineStyle) {
	printer.pdf.SetFont("Unicode", "", 11)
	printer.pdf.SetTextColor(41, 50, 56)
	width := printer.pdf.GetStringWidth(value)
	if style.code {
		printer.pdf.SetTextColor(80, 90, 97)
		printer.pdf.SetFillColor(243, 245, 246)
		printer.pdf.Rect(printer.pdf.GetX(), printer.pdf.GetY(), width, 6, "F")
	}
	if style.link != "" {
		printer.pdf.SetTextColor(32, 119, 154)
	}
	if style.bold {
		printer.pdf.SetDrawColor(41, 50, 56)
		printer.pdf.SetLineWidth(0.1)
		printer.pdf.SetTextRenderingMode(2)
	}
	if style.italic {
		x, y := printer.pdf.GetXY()
		printer.pdf.TransformBegin()
		printer.pdf.TransformSkewX(10, x, y)
	}
	printer.pdf.CellFormat(width, 6, value, "", 0, "", false, 0, style.link)
	if style.italic {
		printer.pdf.TransformEnd()
	}
	if style.bold {
		printer.pdf.SetTextRenderingMode(0)
	}
}

func safePDFLink(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https", "http":
		return parsed.Host != ""
	case "mailto":
		return parsed.Opaque != "" || parsed.Path != ""
	default:
		return false
	}
}

func (printer *pageRenderer) table(node *htmlnode.Node) error {
	var rows []*htmlnode.Node
	var collect func(*htmlnode.Node)
	collect = func(current *htmlnode.Node) {
		if current.DataAtom == atom.Tr {
			rows = append(rows, current)
			return
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			collect(child)
		}
	}
	collect(node)
	columns := 0
	for _, row := range rows {
		count := 0
		for cell := row.FirstChild; cell != nil; cell = cell.NextSibling {
			if cell.DataAtom == atom.Th || cell.DataAtom == atom.Td {
				count++
			}
		}
		columns = max(columns, count)
	}
	if columns == 0 {
		return nil
	}
	width := contentWidth / float64(columns)
	printer.pdf.SetFont("Unicode", "", 10)
	printer.pdf.SetTextColor(41, 50, 56)
	printer.pdf.SetDrawColor(220, 226, 230)
	printer.pdf.SetLineWidth(0.2)
	for _, row := range rows {
		var cells []tableCell
		height := 8.0
		for cell := row.FirstChild; cell != nil; cell = cell.NextSibling {
			if cell.DataAtom == atom.Th || cell.DataAtom == atom.Td {
				entry := tableCell{value: strings.TrimSpace(visible(cell))}
				if entry.value != "" {
					entry.textHeight = float64(len(printer.pdf.SplitText(entry.value, width-4))) * 5
				}
				var imageNodes []*htmlnode.Node
				collectImages(cell, &imageNodes)
				if len(imageNodes) > 8 {
					return ErrLayout
				}
				for _, imageNode := range imageNodes {
					prepared, err := printer.prepareImage(imageNode, width-4, 80)
					if err != nil {
						return err
					}
					entry.images = append(entry.images, prepared)
				}
				cellHeight := entry.textHeight + 4
				for _, image := range entry.images {
					cellHeight += image.height + 2
				}
				cells = append(cells, entry)
				height = max(height, cellHeight)
			}
		}
		if height > pageHeight-2*pageMargin {
			return ErrLayout
		}
		if printer.pdf.GetY()+height > pageHeight-pageMargin {
			printer.pdf.AddPage()
			printer.pdf.SetFont("Unicode", "", 10)
		}
		y := printer.pdf.GetY()
		for column, entry := range cells {
			x := pageMargin + float64(column)*width
			printer.pdf.Rect(x, y, width, height, "D")
			if entry.value != "" {
				printer.pdf.SetXY(x+2, y+2)
				printer.pdf.MultiCell(width-4, 5, entry.value, "", "L", false)
			}
			imageY := y + 2 + entry.textHeight
			for _, image := range entry.images {
				printer.pdf.ImageOptions(image.name, x+2, imageY, image.width, image.height, false, image.options, 0, "")
				imageY += image.height + 2
			}
		}
		printer.pdf.SetY(y + height)
	}
	printer.pdf.Ln(4)
	return printer.pdf.Error()
}

func collectImages(node *htmlnode.Node, images *[]*htmlnode.Node) {
	if node.DataAtom == atom.Img {
		*images = append(*images, node)
		return
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		collectImages(child, images)
	}
}

func (printer *pageRenderer) prepareImage(node *htmlnode.Node, maxWidth, maxHeight float64) (preparedImage, error) {
	if maxWidth <= 0 || maxHeight <= 0 {
		return preparedImage{}, ErrLayout
	}
	var source string
	for _, attribute := range node.Attr {
		if attribute.Key == "src" {
			source = attribute.Val
		}
	}
	parts := strings.SplitN(source, ",", 2)
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "data:image/") || !strings.HasSuffix(parts[0], ";base64") || len(parts[1]) > 12<<20 {
		return preparedImage{}, errors.New("invalid sanitized PDF image")
	}
	data, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil || len(data) > 8<<20 {
		return preparedImage{}, errors.New("invalid sanitized PDF image")
	}
	configuration, kind, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || configuration.Width <= 0 || configuration.Height <= 0 || configuration.Width > 4096 || configuration.Height > 4096 || kind != "png" && kind != "jpeg" && kind != "gif" {
		return preparedImage{}, errors.New("invalid sanitized PDF image")
	}
	if kind == "gif" {
		decoded, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return preparedImage{}, err
		}
		var converted bytes.Buffer
		if err := png.Encode(&converted, decoded); err != nil {
			return preparedImage{}, err
		}
		data, kind = converted.Bytes(), "png"
	}
	width := min(maxWidth, float64(configuration.Width)*0.2646)
	height := width * float64(configuration.Height) / float64(configuration.Width)
	if height > maxHeight {
		height = maxHeight
		width = height * float64(configuration.Width) / float64(configuration.Height)
	}
	printer.images++
	name := fmt.Sprintf("image-%d", printer.images)
	options := fpdf.ImageOptions{ImageType: strings.ToUpper(kind), ReadDpi: false}
	printer.pdf.RegisterImageOptionsReader(name, options, bytes.NewReader(data))
	if err := printer.pdf.Error(); err != nil {
		return preparedImage{}, err
	}
	return preparedImage{name: name, width: width, height: height, options: options}, nil
}

func (printer *pageRenderer) image(node *htmlnode.Node) error {
	prepared, err := printer.prepareImage(node, contentWidth, pageHeight-2*pageMargin)
	if err != nil {
		return err
	}
	if printer.pdf.GetY()+prepared.height+4 > pageHeight-pageMargin {
		printer.pdf.AddPage()
	}
	printer.pdf.ImageOptions(prepared.name, pageMargin, printer.pdf.GetY(), prepared.width, prepared.height, false, prepared.options, 0, "")
	printer.pdf.SetY(printer.pdf.GetY() + prepared.height + 4)
	return printer.pdf.Error()
}
