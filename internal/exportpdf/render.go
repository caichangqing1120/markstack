package exportpdf

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"net/url"
	"runtime"
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
	pageWidth          = 210.0
	pageHeight         = 297.0
	maxPDFBytes        = 64 << 20
	pngInterlaceOffset = 28
)

func RenderMarkdown(source []byte, loadImage func(string) ([]byte, error), font []byte) ([]byte, error) {
	return RenderMarkdownWithOptions(source, loadImage, font, Options{})
}

func RenderMarkdownWithOptions(source []byte, loadImage func(string) ([]byte, error), font []byte, options Options) ([]byte, error) {
	safe, vectors, err := exporthtml.MarkdownForPDF(source, loadImage)
	if err != nil {
		return nil, err
	}
	return renderDocument(safe, font, vectors, options)
}

func RenderRichHTML(source []byte, loadImage func(string) ([]byte, error), font []byte) ([]byte, error) {
	return RenderRichHTMLWithOptions(source, loadImage, font, Options{})
}

func RenderRichHTMLWithOptions(source []byte, loadImage func(string) ([]byte, error), font []byte, options Options) ([]byte, error) {
	safe, err := exporthtml.RenderRichHTML(source, loadImage)
	if err != nil {
		return nil, err
	}
	return renderDocument(safe, font, nil, options)
}

type pageRenderer struct {
	pdfLayout
	pdf           *fpdf.Fpdf
	images        pdfImages
	adam7Images   map[string]registeredImage
	vectors       []pdfVector
	outlineLevels []int
	toc           *tableOfContents
	leftInset     float64
}

func (printer *pageRenderer) contentX() float64     { return printer.margin + printer.leftInset }
func (printer *pageRenderer) contentWidth() float64 { return max(1, printer.width()-printer.leftInset) }

type registeredImage struct {
	name    string
	size    image.Point
	options fpdf.ImageOptions
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
	flow       *inlineFlow
}

type inlineStyle struct {
	bold, italic, code           bool
	underline, highlight, strike bool
	vertical                     int
	color                        [3]uint8
	link                         string
}

type inlineGlyph struct {
	value        rune
	style        inlineStyle
	vector       *pdfVector
	width, scale float64
}

type inlineFlow struct {
	printer        *pageRenderer
	line           []inlineGlyph
	width          float64
	lastBreak      int
	content        bool
	x, limit, size float64
	lines          [][]inlineGlyph
	measureOnly    bool
	onFirstLine    func()
}

func render(safeHTML, font []byte) ([]byte, error) {
	return renderDocument(safeHTML, font, nil, Options{})
}

func renderDocument(safeHTML, font []byte, vectors []exporthtml.Vector, options Options) ([]byte, error) {
	if len(font) == 0 {
		return nil, ErrFontRequired
	}
	if !completeTrueTypeFont(font) {
		return nil, errors.New("PDF 字体不是完整的 TrueType 文件")
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
	printer := pageRenderer{pdf: pdf, pdfLayout: layoutForPreset(options.Preset)}
	pdf.SetMargins(printer.margin, printer.margin, printer.margin)
	pdf.SetAutoPageBreak(true, printer.margin)
	pdf.SetTitle("墨栈导出", true)
	pdf.SetCreator("MarkStack", true)
	if err := callPDF("PDF TrueType 字体载入失败", func() error {
		pdf.AddUTF8FontFromBytes("Unicode", "", font)
		pdf.SetFont("Unicode", "", printer.bodySize)
		return pdf.Error()
	}); err != nil {
		return nil, err
	}
	printer.runningText(options, safeHTML, main, vectors)
	pdf.AddPage()
	printer.prepareVectors(vectors)
	if err := printer.indexTOC(main); err != nil {
		return nil, err
	}
	for child := main.FirstChild; child != nil; child = child.NextSibling {
		if err := printer.block(child, 0); err != nil {
			return nil, err
		}
	}
	for sibling := main.NextSibling; sibling != nil; sibling = sibling.NextSibling {
		if sibling.Type == htmlnode.ElementNode && sibling.DataAtom == atom.Section {
			printer.keepRawHTMLHeading(sibling)
			if err := printer.block(sibling, 0); err != nil {
				return nil, err
			}
		}
	}
	if err := printer.finishTOC(); err != nil {
		return nil, err
	}
	var output bytes.Buffer
	if err := callPDF("PDF 写出失败", func() error { return pdf.Output(&output) }); err != nil {
		return nil, err
	}
	if output.Len() > maxPDFBytes {
		return nil, ErrTooLarge
	}
	return output.Bytes(), nil
}

func callPDF(stage string, operation func() error) (err error) {
	defer func() {
		if value := recover(); value != nil {
			if runtimeErr, ok := value.(runtime.Error); ok {
				err = fmt.Errorf("%s: %w", stage, runtimeErr)
				return
			}
			panic(value)
		}
	}()
	if err := operation(); err != nil {
		return fmt.Errorf("%s: %w", stage, err)
	}
	return nil
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

func validatePDFText(value string) error {
	for _, character := range value {
		if character > 0xFFFF {
			return fmt.Errorf("PDF 导出不支持字符 U+%04X", character)
		}
	}
	return nil
}

func (printer *pageRenderer) multiCell(width, lineHeight float64, value string, fill bool) error {
	if err := validatePDFText(value); err != nil {
		return err
	}
	printer.pdf.MultiCell(width, lineHeight, value, "", "L", fill)
	return printer.pdf.Error()
}

func (printer *pageRenderer) text(value string, size, lineHeight, after float64) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return printer.pdf.Error()
	}
	printer.pdf.SetFont("Unicode", "", size)
	printer.pdf.SetTextColor(41, 50, 56)
	printer.pdf.SetX(printer.contentX())
	if err := printer.multiCell(printer.contentWidth(), lineHeight, value, false); err != nil {
		return err
	}
	printer.pdf.Ln(after)
	return printer.pdf.Error()
}

func (printer *pageRenderer) keepRawHTMLHeading(section *htmlnode.Node) {
	for _, attribute := range section.Attr {
		if attribute.Key != "class" || attribute.Val != "unsupported" {
			continue
		}
		heading := section.FirstChild
		if heading == nil || heading.DataAtom != atom.H2 || strings.TrimSpace(visible(heading)) == "" {
			return
		}
		body := heading.NextSibling
		if body != nil && body.DataAtom == atom.Pre && body.FirstChild != nil && printer.pdf.GetY()+3+printer.headingHeight(2)+2+5*printer.lineScale > printer.bottom() {
			printer.pdf.AddPage()
		}
		return
	}
}

func (printer *pageRenderer) headingOutlineTitle(node *htmlnode.Node) string {
	var parts strings.Builder
	var collect func(*htmlnode.Node)
	collect = func(current *htmlnode.Node) {
		if current == nil {
			return
		}
		if vector := printer.vector(current); vector != nil {
			if body := strings.TrimSpace(vector.body); body != "" {
				if parts.Len() > 0 {
					parts.WriteByte(' ')
				}
				parts.WriteString(body)
				parts.WriteByte(' ')
			}
			return
		}
		if current.Type == htmlnode.TextNode {
			parts.WriteString(current.Data)
			return
		}
		if current.DataAtom == atom.Br {
			parts.WriteByte(' ')
			return
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			collect(child)
		}
	}
	collect(node)
	return strings.Join(strings.Fields(parts.String()), " ")
}

func (printer *pageRenderer) addHeadingBookmark(headingLevel int, title string) {
	if title == "" {
		return
	}
	for len(printer.outlineLevels) > 0 && printer.outlineLevels[len(printer.outlineLevels)-1] >= headingLevel {
		printer.outlineLevels = printer.outlineLevels[:len(printer.outlineLevels)-1]
	}
	bookmarkLevel := len(printer.outlineLevels)
	printer.outlineLevels = append(printer.outlineLevels, headingLevel)
	printer.pdf.Bookmark(title, bookmarkLevel, -1)
}

func (printer *pageRenderer) block(node *htmlnode.Node, depth int) error {
	if node.Type != htmlnode.ElementNode {
		return nil
	}
	if vector := printer.vector(node); vector != nil {
		printer.vectorBlock(vector)
		return printer.pdf.Error()
	}
	if node.Data == "markstack-toc" && printer.toc != nil {
		return printer.drawTOC()
	}
	switch node.DataAtom {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		return printer.heading(node)
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
			if printer.hasVector(child) || hasExtendedStyle(child) {
				inset := float64(depth) * 6
				if err := printer.formatted(child, printer.contentX()+inset, max(1, printer.contentWidth()-inset), printer.bodySize, prefix, 0); err != nil {
					return err
				}
				continue
			}
			printer.pdf.SetFont("Unicode", "", printer.bodySize)
			printer.pdf.SetX(printer.contentX() + float64(depth)*6)
			if err := printer.multiCell(max(1, printer.contentWidth()-float64(depth)*6), 6*printer.lineScale, prefix+strings.TrimSpace(visible(child)), false); err != nil {
				return err
			}
		}
		printer.pdf.Ln(3)
	case atom.Blockquote:
		printer.pdf.SetFont("Unicode", "", printer.bodySize)
		printer.pdf.SetTextColor(88, 100, 106)
		firstPage, firstY := printer.pdf.PageNo(), printer.pdf.GetY()
		printer.pdf.SetX(printer.contentX() + 7)
		var err error
		if printer.hasVector(node) || hasExtendedStyle(node) {
			err = printer.formatted(node, printer.contentX()+7, max(1, printer.contentWidth()-7), printer.bodySize, "", 0)
		} else {
			err = printer.multiCell(max(1, printer.contentWidth()-7), 6*printer.lineScale, strings.TrimSpace(visible(node)), false)
		}
		if err != nil {
			return err
		}
		lastPage, lastY := printer.pdf.PageNo(), printer.pdf.GetY()
		printer.pdf.SetDrawColor(118, 162, 182)
		printer.pdf.SetLineWidth(0.7)
		for page := firstPage; page <= lastPage; page++ {
			printer.pdf.SetPage(page)
			top, bottom := printer.margin, printer.bottom()
			if page == firstPage {
				top = firstY
			}
			if page == lastPage {
				bottom = lastY
			}
			if bottom > top {
				printer.pdf.Line(printer.contentX()+1, top, printer.contentX()+1, bottom)
			}
		}
		printer.pdf.SetPage(lastPage)
		printer.pdf.SetXY(printer.contentX(), lastY)
		printer.pdf.Ln(3)
	case atom.Pre:
		printer.pdf.SetFont("Unicode", "", printer.bodySize-2)
		printer.pdf.SetFillColor(243, 245, 246)
		printer.pdf.SetX(printer.contentX())
		if err := printer.multiCell(printer.contentWidth(), 5*printer.lineScale, strings.TrimRight(visible(node), "\n"), true); err != nil {
			return err
		}
		printer.pdf.Ln(3)
	case atom.Table:
		return printer.table(node)
	case atom.Div:
		if handled, err := printer.callout(node); handled {
			return err
		}
		return printer.blocks(node, depth)
	case atom.Img:
		return printer.image(node)
	case atom.Hr:
		if printer.pdf.GetY()+8 > printer.bottom() {
			printer.pdf.AddPage()
		}
		printer.pdf.Ln(3)
		y := printer.pdf.GetY()
		printer.pdf.SetDrawColor(220, 226, 230)
		printer.pdf.SetLineWidth(0.2)
		printer.pdf.Line(printer.contentX(), y, pageWidth-printer.margin, y)
		printer.pdf.Ln(5)
	default:
		return printer.blocks(node, depth)
	}
	return printer.pdf.Error()
}

func (printer *pageRenderer) blocks(parent *htmlnode.Node, depth int) error {
	for child := parent.FirstChild; child != nil; child = child.NextSibling {
		if err := printer.block(child, depth); err != nil {
			return err
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
		return printer.text(visible(node), printer.bodySize, 6*printer.lineScale, 3)
	}
	return printer.formatted(node, printer.contentX(), printer.contentWidth(), printer.bodySize, "", 3)
}

func (printer *pageRenderer) formatted(node *htmlnode.Node, x, width, size float64, prefix string, after float64) error {
	flow := inlineFlow{printer: printer, x: x, limit: width, size: size}
	return flow.render(node, prefix, after)
}

func (flow *inlineFlow) render(node *htmlnode.Node, prefix string, after float64) error {
	pdf := flow.printer.pdf
	pdf.SetFont("Unicode", "", flow.size)
	for _, value := range prefix {
		flow.add(value, inlineStyle{})
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if err := flow.collect(child, inlineStyle{}); err != nil {
			return err
		}
	}
	flow.flush(false)
	if flow.content {
		pdf.Ln(after)
	}
	return pdf.Error()
}

func (flow *inlineFlow) collect(node *htmlnode.Node, style inlineStyle) error {
	if vector := flow.printer.vector(node); vector != nil {
		if !vector.inline {
			flow.flush(false)
			flow.content = true
			flow.printer.vectorBlock(vector)
			return nil
		}
		scale := vector.fit(flow.limit, flow.printer.height()-8)
		flow.append(inlineGlyph{vector: vector, width: vector.scene.Width * scale, scale: scale})
		return nil
	}
	if node.Type == htmlnode.TextNode {
		if err := validatePDFText(node.Data); err != nil {
			return err
		}
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
		if flow.measureOnly {
			return nil
		}
		flow.flush(false)
		flow.content = true
		return flow.printer.image(node)
	}
	if node.DataAtom == atom.P && len(flow.line) > 0 && flow.line[len(flow.line)-1].value != ' ' {
		flow.flush(false)
	}
	switch node.DataAtom {
	case atom.Strong:
		style.bold = true
	case atom.Em:
		style.italic = true
	case atom.Code:
		style.code = true
	case atom.U:
		style.underline = true
	case atom.Mark:
		style.highlight = true
	case atom.Sub:
		style.vertical = -1
	case atom.Sup:
		style.vertical = 1
	case atom.Del:
		style.strike = true
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
	size := inlineSize(flow.size, style)
	if current, _ := flow.printer.pdf.GetFontSize(); current != size {
		flow.printer.pdf.SetFontSize(size)
	}
	flow.append(inlineGlyph{value: value, style: style, width: flow.printer.pdf.GetStringWidth(string(value))})
}

func (flow *inlineFlow) append(glyph inlineGlyph) {
	for len(flow.line) > 0 && flow.width+glyph.width > flow.limit {
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
	flow.line = append(flow.line, glyph)
	flow.width += glyph.width
	if unicode.IsSpace(glyph.value) || unicode.Is(unicode.Han, glyph.value) {
		flow.lastBreak = len(flow.line)
	}
	flow.content = true
}

func (flow *inlineFlow) remeasure() {
	flow.width, flow.lastBreak = 0, 0
	for index, glyph := range flow.line {
		flow.width += glyph.width
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
		flow.draw(nil)
	}
	if force {
		flow.content = true
	}
}

func (flow *inlineFlow) draw(line []inlineGlyph) {
	for len(line) > 0 && unicode.IsSpace(line[len(line)-1].value) {
		line = line[:len(line)-1]
	}
	if flow.measureOnly {
		flow.lines = append(flow.lines, append([]inlineGlyph(nil), line...))
		return
	}
	up, down := flow.lineMetrics(line)
	if flow.printer.pdf.GetY()+up+down > flow.printer.bottom() {
		flow.printer.pdf.AddPage()
	}
	if flow.onFirstLine != nil {
		flow.onFirstLine()
		flow.onFirstLine = nil
	}
	y := flow.printer.pdf.GetY()
	flow.drawAt(line, flow.x, y, up)
	flow.printer.pdf.SetXY(flow.x, y+up+down)
}

func (flow *inlineFlow) lineMetrics(line []inlineGlyph) (up, down float64) {
	up, down = flow.printer.inlineMetrics(flow.size)
	for _, glyph := range line {
		if glyph.vector != nil {
			scene := glyph.vector.scene
			up = max(up, scene.Baseline*glyph.scale)
			down = max(down, (scene.Height-scene.Baseline)*glyph.scale)
		}
	}
	return up, down
}

func (flow *inlineFlow) drawAt(line []inlineGlyph, x, y, up float64) {
	pdf := flow.printer.pdf
	baseline := y + up
	textUp, _ := flow.printer.inlineMetrics(flow.size)
	pdf.SetXY(x, baseline-textUp)
	for start := 0; start < len(line); {
		if glyph := line[start]; glyph.vector != nil {
			x, textY := pdf.GetXY()
			flow.printer.drawVector(glyph.vector.scene, x, baseline-glyph.vector.scene.Baseline*glyph.scale, glyph.scale)
			pdf.SetXY(x+glyph.width, textY)
			start++
			continue
		}
		end := start + 1
		for end < len(line) && line[end].vector == nil && line[end].style == line[start].style {
			end++
		}
		var value strings.Builder
		for _, glyph := range line[start:end] {
			value.WriteRune(glyph.value)
		}
		flow.printer.segment(value.String(), line[start].style, flow.size)
		start = end
	}
}

func (printer *pageRenderer) segment(value string, style inlineStyle, size float64) {
	originalX, originalY := printer.pdf.GetXY()
	fontSize := inlineSize(size, style)
	shift := 0.0
	if style.vertical != 0 {
		shift = size * .352778 * .17 * float64(style.vertical)
	}
	printer.pdf.SetXY(originalX, originalY-shift+(size-fontSize)*.13)
	printer.pdf.SetFont("Unicode", "", fontSize)
	printer.pdf.SetTextColor(41, 50, 56)
	width := printer.pdf.GetStringWidth(value)
	up, down := printer.inlineMetrics(size)
	height := up + down
	if style.highlight {
		printer.pdf.SetFillColor(255, 235, 170)
		printer.pdf.Rect(printer.pdf.GetX(), printer.pdf.GetY(), width, height, "F")
	} else if style.code {
		printer.pdf.SetTextColor(80, 90, 97)
		printer.pdf.SetFillColor(243, 245, 246)
		printer.pdf.Rect(printer.pdf.GetX(), printer.pdf.GetY(), width, height, "F")
	}
	if style.link != "" {
		printer.pdf.SetTextColor(32, 119, 154)
	}
	if style.color != ([3]uint8{}) {
		printer.pdf.SetTextColor(int(style.color[0]), int(style.color[1]), int(style.color[2]))
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
	printer.pdf.CellFormat(width, height, value, "", 0, "", false, 0, style.link)
	if style.italic {
		printer.pdf.TransformEnd()
	}
	if style.bold {
		printer.pdf.SetTextRenderingMode(0)
	}
	if style.underline || style.strike {
		printer.pdf.SetDrawColor(41, 50, 56)
		printer.pdf.SetLineWidth(.22)
		underlineY := originalY - shift + height*.80
		if style.underline {
			printer.pdf.Line(originalX, underlineY, originalX+width, underlineY)
		}
		if style.strike {
			strikeY := originalY - shift + height*.57
			printer.pdf.Line(originalX, strikeY, originalX+width, strikeY)
		}
	}
	printer.pdf.SetXY(originalX+width, originalY)
}

func inlineSize(size float64, style inlineStyle) float64 {
	if style.vertical != 0 {
		return size * .78
	}
	return size
}

func hasExtendedStyle(node *htmlnode.Node) bool {
	switch node.DataAtom {
	case atom.U, atom.Mark, atom.Sub, atom.Sup:
		return true
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if hasExtendedStyle(child) {
			return true
		}
	}
	return false
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
	width := printer.contentWidth() / float64(columns)
	size, lineHeight := printer.bodySize-1, 5*printer.lineScale
	printer.pdf.SetFont("Unicode", "", size)
	printer.pdf.SetTextColor(41, 50, 56)
	printer.pdf.SetDrawColor(220, 226, 230)
	printer.pdf.SetLineWidth(0.2)
	for _, row := range rows {
		var cells []tableCell
		height := 8.0
		for cell := row.FirstChild; cell != nil; cell = cell.NextSibling {
			if cell.DataAtom == atom.Th || cell.DataAtom == atom.Td {
				entry := tableCell{value: strings.TrimSpace(visible(cell))}
				printer.pdf.SetFontSize(size)
				if printer.hasVector(cell) || hasExtendedStyle(cell) {
					flow := &inlineFlow{printer: printer, limit: width - 4, size: size, measureOnly: true}
					if err := flow.collect(cell, inlineStyle{}); err != nil {
						return err
					}
					flow.flush(false)
					for _, line := range flow.lines {
						up, down := flow.lineMetrics(line)
						entry.textHeight += up + down
					}
					entry.flow = flow
				} else if entry.value != "" {
					if err := validatePDFText(entry.value); err != nil {
						return err
					}
					entry.textHeight = float64(len(printer.pdf.SplitText(entry.value, width-4))) * lineHeight
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
		if height > printer.height() {
			return ErrLayout
		}
		if printer.pdf.GetY()+height > printer.bottom() {
			printer.pdf.AddPage()
			printer.pdf.SetFont("Unicode", "", size)
		}
		y := printer.pdf.GetY()
		for column, entry := range cells {
			x := printer.contentX() + float64(column)*width
			printer.pdf.SetFontSize(size)
			printer.pdf.SetDrawColor(220, 226, 230)
			printer.pdf.SetLineWidth(0.2)
			printer.pdf.Rect(x, y, width, height, "D")
			if entry.flow != nil {
				lineY := y + 2
				for _, line := range entry.flow.lines {
					up, down := entry.flow.lineMetrics(line)
					entry.flow.drawAt(line, x+2, lineY, up)
					lineY += up + down
				}
			} else if entry.value != "" {
				printer.pdf.SetXY(x+2, y+2)
				printer.pdf.MultiCell(width-4, lineHeight, entry.value, "", "L", false)
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
	if registered, ok := printer.adam7Images[source]; ok {
		return registered.fit(maxWidth, maxHeight), nil
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
	size := image.Pt(configuration.Width, configuration.Height)
	resourceSize := size
	adam7 := kind == "png" && len(data) > pngInterlaceOffset && data[pngInterlaceOffset] == 1
	if kind == "gif" || adam7 {
		decoded, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return preparedImage{}, err
		}
		if adam7 {
			decoded, err = prepareAdam7Pixels(decoded)
			if err != nil {
				return preparedImage{}, err
			}
		}
		var converted bytes.Buffer
		if err := png.Encode(&converted, decoded); err != nil {
			return preparedImage{}, err
		}
		data, kind = converted.Bytes(), "png"
		resourceSize = decoded.Bounds().Size()
	}
	options := fpdf.ImageOptions{ImageType: strings.ToUpper(kind), ReadDpi: false}
	name, err := printer.images.register(printer.pdf, data, options, resourceSize)
	if err != nil {
		return preparedImage{}, err
	}
	registered := registeredImage{name: name, size: size, options: options}
	if adam7 {
		if printer.adam7Images == nil {
			printer.adam7Images = make(map[string]registeredImage)
		}
		printer.adam7Images[source] = registered
	}
	return registered.fit(maxWidth, maxHeight), nil
}

func (registered registeredImage) fit(maxWidth, maxHeight float64) preparedImage {
	width := min(maxWidth, float64(registered.size.X)*0.2646)
	height := width * float64(registered.size.Y) / float64(registered.size.X)
	if height > maxHeight {
		height = maxHeight
		width = height * float64(registered.size.X) / float64(registered.size.Y)
	}
	return preparedImage{name: registered.name, width: width, height: height, options: registered.options}
}

func prepareAdam7Pixels(decoded image.Image) (image.Image, error) {
	switch bitmap := decoded.(type) {
	case *image.NRGBA64:
		if !bitmap.Opaque() {
			return nil, errors.New("Adam7 PNG with 16-bit transparency is not supported in PDF export")
		}
	case *image.Paletted:
		if !bitmap.Opaque() {
			straight := image.NewNRGBA(bitmap.Bounds())
			var palette [256]color.NRGBA
			for index, entry := range bitmap.Palette {
				palette[index] = color.NRGBAModel.Convert(entry).(color.NRGBA)
			}
			for row := bitmap.Rect.Min.Y; row < bitmap.Rect.Max.Y; row++ {
				for column := bitmap.Rect.Min.X; column < bitmap.Rect.Max.X; column++ {
					straight.SetNRGBA(column, row, palette[bitmap.ColorIndexAt(column, row)])
				}
			}
			return straight, nil
		}
	}
	return decoded, nil
}

func (printer *pageRenderer) image(node *htmlnode.Node) error {
	prepared, err := printer.prepareImage(node, printer.contentWidth(), printer.height()-4)
	if err != nil {
		return err
	}
	if printer.pdf.GetY()+prepared.height+4 > printer.bottom() {
		printer.pdf.AddPage()
	}
	printer.pdf.ImageOptions(prepared.name, printer.contentX(), printer.pdf.GetY(), prepared.width, prepared.height, false, prepared.options, 0, "")
	printer.pdf.SetY(printer.pdf.GetY() + prepared.height + 4)
	return printer.pdf.Error()
}
