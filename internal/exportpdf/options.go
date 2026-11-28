package exportpdf

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"strings"

	htmlnode "golang.org/x/net/html"

	"markstack/internal/exporthtml"
)

type Options struct {
	Title             string
	Header            bool
	FooterPageNumbers bool
	Preset            string
}

type pdfLayout struct {
	margin, bodySize, headingScale, lineScale float64
}

func layoutForPreset(preset string) pdfLayout {
	switch preset {
	case "compact":
		return pdfLayout{margin: 13, bodySize: 10, headingScale: .9, lineScale: .85}
	case "academic":
		return pdfLayout{margin: 25, bodySize: 12, headingScale: 1.08, lineScale: 1.2}
	default:
		return pdfLayout{margin: 18, bodySize: 11, headingScale: 1, lineScale: 1}
	}
}

func (layout pdfLayout) width() float64  { return pageWidth - 2*layout.margin }
func (layout pdfLayout) height() float64 { return pageHeight - 2*layout.margin }
func (layout pdfLayout) bottom() float64 { return pageHeight - layout.margin }

func (layout pdfLayout) inlineMetrics(size float64) (up, down float64) {
	up = size * 4.05 / 11
	down = max(size*1.95/11, size/layout.bodySize*6*layout.lineScale-up)
	return up, down
}

func (printer *pageRenderer) runningText(options Options, source []byte, main *htmlnode.Node, vectors []exporthtml.Vector) {
	pdf := printer.pdf
	if options.Header {
		title := strings.Join(strings.Fields(strings.Map(func(value rune) rune {
			if value > 0xFFFF {
				return '\uFFFD'
			}
			return value
		}, options.Title)), " ")
		if title != "" {
			pdf.SetHeaderFunc(func() {
				if pdf.PageNo() == 1 {
					return
				}
				pdf.SetFont("Unicode", "", 9)
				pdf.SetTextColor(99, 110, 117)
				pdf.Text(printer.margin, printer.margin/2, printer.fitHeader(title))
			})
		}
	}
	if options.FooterPageNumbers {
		alias := "{nb}"
		// fpdf replaces aliases throughout every page stream, not just in the footer.
		if bytes.Contains(source, []byte(alias)) || strings.Contains(options.Title, alias) || strings.Contains(visible(main), alias) {
			alias = "{nb:" + rand.Text() + "}"
		} else {
			for _, vector := range vectors {
				if strings.Contains(vector.Body, alias) {
					alias = "{nb:" + rand.Text() + "}"
					break
				}
			}
		}
		pdf.AliasNbPages(alias)
		pdf.SetFooterFunc(func() {
			pdf.SetFont("Unicode", "", 9)
			pdf.SetTextColor(99, 110, 117)
			prefix := fmt.Sprintf("%d / ", pdf.PageNo())
			x := (pageWidth - pdf.GetStringWidth(prefix+"99")) / 2
			pdf.Text(x, pageHeight-printer.margin/2, prefix+alias)
		})
	}
}

func (printer *pageRenderer) fitHeader(title string) string {
	if printer.pdf.GetStringWidth(title) <= printer.width() {
		return title
	}
	const ellipsis = "…"
	remaining := printer.width() - printer.pdf.GetStringWidth(ellipsis)
	for index, value := range title {
		remaining -= printer.pdf.GetStringWidth(string(value))
		if remaining < 0 {
			return title[:index] + ellipsis
		}
	}
	return title
}
