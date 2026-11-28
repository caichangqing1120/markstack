package exportpdf

import (
	"strconv"
	"strings"

	htmlnode "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type tocHeading struct {
	title           string
	level, id, page int
}

type tocNumber struct {
	heading *tocHeading
	page    int
	y       float64
}

type tableOfContents struct {
	headings []*tocHeading
	byNode   map[*htmlnode.Node]*tocHeading
	numbers  []tocNumber
}

func headingLevel(node *htmlnode.Node) int {
	switch node.DataAtom {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		return int(node.Data[1] - '0')
	}
	return 0
}

func (printer *pageRenderer) headingSize(level int) float64 {
	return [...]float64{24, 19, 16, 14, 12, 11}[level-1] * printer.headingScale
}

func (printer *pageRenderer) headingHeight(level int) float64 {
	return printer.headingSize(level) * .48 * printer.lineScale
}

func (printer *pageRenderer) heading(node *htmlnode.Node) error {
	level := headingLevel(node)
	title := printer.headingOutlineTitle(node)
	if err := validatePDFText(title); err != nil {
		return err
	}
	size, lineHeight := printer.headingSize(level), printer.headingHeight(level)
	printer.pdf.Ln(3)
	mark := func() {
		printer.addHeadingBookmark(level, title)
		if printer.toc != nil {
			if entry := printer.toc.byNode[node]; entry != nil {
				entry.page = printer.pdf.PageNo()
				printer.pdf.SetLink(entry.id, printer.pdf.GetY(), entry.page)
			}
		}
	}
	if printer.hasVector(node) {
		flow := inlineFlow{printer: printer, x: printer.margin, limit: printer.width(), size: size, onFirstLine: mark}
		return flow.render(node, "", 2)
	}
	if title != "" && printer.pdf.GetY()+lineHeight > printer.bottom() {
		printer.pdf.AddPage()
	}
	mark()
	return printer.text(visible(node), size, lineHeight, 2)
}

func (printer *pageRenderer) indexTOC(main *htmlnode.Node) error {
	for node := main.FirstChild; node != nil; node = node.NextSibling {
		if node.Type == htmlnode.ElementNode && node.Data == "markstack-toc" {
			printer.toc = &tableOfContents{byNode: make(map[*htmlnode.Node]*tocHeading)}
			break
		}
	}
	if printer.toc == nil {
		return nil
	}
	for node := main.FirstChild; node != nil; node = node.NextSibling {
		if level := headingLevel(node); level != 0 {
			title := printer.headingOutlineTitle(node)
			if title == "" {
				continue
			}
			if err := validatePDFText(title); err != nil {
				return err
			}
			entry := &tocHeading{title: title, level: level, id: printer.pdf.AddLink()}
			printer.toc.headings = append(printer.toc.headings, entry)
			printer.toc.byNode[node] = entry
		}
	}
	return nil
}

func (printer *pageRenderer) drawTOC() error {
	pdf := printer.pdf
	rowHeight := 6 * printer.lineScale
	titleHeight := rowHeight + 3
	if pdf.GetY()+titleHeight+rowHeight > printer.bottom() {
		pdf.AddPage()
	}
	pdf.SetFont("Unicode", "", printer.bodySize+3)
	pdf.SetTextColor(41, 50, 56)
	const title = "目录 / Table of Contents"
	pdf.Text(printer.margin, pdf.GetY()+titleHeight-3, title)
	pdf.Ln(titleHeight)
	if len(printer.toc.headings) == 0 {
		return printer.text("无标题 / No headings", printer.bodySize, rowHeight, 3)
	}
	size := printer.bodySize - 1
	pdf.SetFont("Unicode", "", size)
	pdf.SetDrawColor(163, 174, 180)
	pdf.SetLineWidth(.18)
	right := pageWidth - printer.margin
	const numberWidth = 18.0
	for _, heading := range printer.toc.headings {
		x := printer.margin + float64(heading.level-1)*4
		flow := inlineFlow{printer: printer, limit: right - x - numberWidth, size: size, measureOnly: true}
		for _, value := range heading.title {
			flow.add(value, inlineStyle{})
		}
		flow.flush(false)
		for index, line := range flow.lines {
			if pdf.GetY()+rowHeight > printer.bottom() {
				pdf.AddPage()
			}
			top := pdf.GetY()
			y := top + rowHeight*.73
			var label strings.Builder
			for _, glyph := range line {
				label.WriteRune(glyph.value)
			}
			value := label.String()
			pdf.SetTextColor(41, 68, 83)
			pdf.Text(x, y, value)
			if index == len(flow.lines)-1 {
				pdf.SetDashPattern([]float64{.35, 1}, 0)
				pdf.Line(x+pdf.GetStringWidth(value)+2, y-.55, right-numberWidth+2, y-.55)
				pdf.SetDashPattern(nil, 0)
				printer.toc.numbers = append(printer.toc.numbers, tocNumber{heading: heading, page: pdf.PageNo(), y: y})
			}
			pdf.Link(x, top, right-x, rowHeight, heading.id)
			pdf.Ln(rowHeight)
		}
	}
	pdf.Ln(3)
	return pdf.Error()
}

func (printer *pageRenderer) finishTOC() error {
	if printer.toc == nil || len(printer.toc.numbers) == 0 {
		return nil
	}
	pdf := printer.pdf
	lastPage := pdf.PageNo()
	defer pdf.SetPage(lastPage)
	right, size := pageWidth-printer.margin, printer.bodySize-1
	lastNumberPage := 0
	for _, number := range printer.toc.numbers {
		if number.heading.page == 0 {
			return ErrLayout
		}
		if lastNumberPage != number.page {
			lastNumberPage = number.page
			pdf.SetPage(number.page)
			pdf.SetFontSize(size)
		}
		pdf.SetTextColor(99, 110, 117)
		value := strconv.Itoa(number.heading.page)
		pdf.Text(right-pdf.GetStringWidth(value), number.y, value)
	}
	return pdf.Error()
}
