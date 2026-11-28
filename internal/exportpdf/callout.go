package exportpdf

import (
	htmlnode "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"markstack/internal/markdownsyntax"
)

func (printer *pageRenderer) callout(node *htmlnode.Node) (bool, error) {
	alert, ok := markdownsyntax.AlertClass(htmlClass(node))
	if !ok {
		return false, nil
	}
	pdf := printer.pdf
	if pdf.GetY()+12 > printer.bottom() {
		pdf.AddPage()
	}
	firstPage, firstY := pdf.PageNo(), pdf.GetY()
	accentX, inset := printer.contentX()+1, printer.leftInset
	printer.leftInset = min(inset+7, printer.width()/2)
	defer func() { printer.leftInset = inset }()
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.DataAtom == atom.P && htmlClass(child) == "callout-title" {
			flow := inlineFlow{printer: printer, x: printer.contentX(), limit: printer.contentWidth(), size: printer.bodySize}
			pdf.SetFont("Unicode", "", flow.size)
			if err := flow.collect(child, inlineStyle{color: alert.RGB, bold: true}); err != nil {
				return true, err
			}
			flow.flush(false)
		} else if err := printer.block(child, 0); err != nil {
			return true, err
		}
	}
	lastPage, lastY := pdf.PageNo(), pdf.GetY()
	pdf.SetDrawColor(int(alert.RGB[0]), int(alert.RGB[1]), int(alert.RGB[2]))
	pdf.SetLineWidth(1.1)
	for page := firstPage; page <= lastPage; page++ {
		pdf.SetPage(page)
		top, bottom := printer.margin, printer.bottom()
		if page == firstPage {
			top = firstY
		}
		if page == lastPage {
			bottom = lastY
		}
		if top < bottom {
			pdf.Line(accentX, top, accentX, bottom)
		}
	}
	pdf.SetPage(lastPage)
	pdf.SetXY(accentX-1, lastY+3)
	return true, pdf.Error()
}

func htmlClass(node *htmlnode.Node) string {
	for _, attr := range node.Attr {
		if attr.Key == "class" {
			return attr.Val
		}
	}
	return ""
}
