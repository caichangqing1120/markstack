package exportpdf

import (
	"strconv"

	"github.com/go-pdf/fpdf"
	htmlnode "golang.org/x/net/html"
	"markstack/internal/diagramrender"
	"markstack/internal/exporthtml"
	"markstack/internal/mathrender"
	"markstack/internal/vectorrender"
)

const vectorScale = 25.4 / 96

type pdfVector struct {
	scene  vectorrender.Scene
	inline bool
	body   string
}

func (printer *pageRenderer) prepareVectors(vectors []exporthtml.Vector) {
	printer.vectors = make([]pdfVector, len(vectors))
	measure := func(value string, size float64) float64 {
		printer.pdf.SetFontSize(size * .75)
		return printer.pdf.GetStringWidth(value) / vectorScale
	}
	for index, vector := range vectors {
		if len(vector.Scene.Ops) == 0 {
			continue
		}
		scene := vector.Scene
		if vector.Math {
			size := 21 * printer.bodySize / 11
			if vector.Inline {
				size = printer.bodySize / .75
			}
			if layout, err := mathrender.Layout(vector.Body, size, measure); err == nil {
				scene = layout
			}
		} else if layout, err := diagramrender.Layout(vector.Body, measure); err == nil {
			scene = layout
		}
		printer.vectors[index] = pdfVector{scene, vector.Inline, vector.Body}
	}
	printer.pdf.SetFontSize(printer.bodySize)
}

func (printer *pageRenderer) vector(node *htmlnode.Node) *pdfVector {
	if node.Data != "markstack-vector" {
		return nil
	}
	for _, attr := range node.Attr {
		if attr.Key == "data-index" {
			index, err := strconv.Atoi(attr.Val)
			if err == nil && index >= 0 && index < len(printer.vectors) && len(printer.vectors[index].scene.Ops) > 0 {
				return &printer.vectors[index]
			}
		}
	}
	return nil
}

func (printer *pageRenderer) hasVector(node *htmlnode.Node) bool {
	if printer.vector(node) != nil {
		return true
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if printer.hasVector(child) {
			return true
		}
	}
	return false
}

func (vector *pdfVector) fit(width, height float64) float64 {
	return min(vectorScale, width/vector.scene.Width, height/vector.scene.Height)
}

func (printer *pageRenderer) vectorBlock(vector *pdfVector) {
	scale := vector.fit(printer.contentWidth(), printer.height()-6)
	height := vector.scene.Height * scale
	if printer.pdf.GetY()+height > printer.bottom() {
		printer.pdf.AddPage()
	}
	printer.drawVector(vector.scene, printer.contentX()+(printer.contentWidth()-vector.scene.Width*scale)/2, printer.pdf.GetY(), scale)
	printer.pdf.Ln(height + 3)
}

func (printer *pageRenderer) drawVector(scene vectorrender.Scene, x, y, scale float64) {
	pdf := printer.pdf
	for _, op := range scene.Ops {
		left, top := x+op.At.X*scale, y+op.At.Y*scale
		width, height := op.Width*scale, op.Height*scale
		pdf.SetDrawColor(87, 109, 132)
		pdf.SetFillColor(234, 242, 250)
		switch op.Kind {
		case vectorrender.Text:
			if op.Label {
				pdf.SetFillColor(255, 255, 255)
				pdf.RoundedRect(left-3*scale, top-op.Size*scale, width+6*scale, (op.Size+4)*scale, 2*scale, "1234", "F")
			}
			pdf.SetTextColor(36, 41, 47)
			pdf.SetFontSize(op.Size * scale * 72 / 25.4)
			pdf.Text(left, top, op.Text)
		case vectorrender.Rect:
			pdf.SetLineWidth(op.Size * scale)
			if op.Radius > 0 {
				pdf.RoundedRect(left, top, width, height, op.Radius*scale, "1234", "DF")
			} else {
				pdf.Rect(left, top, width, height, "DF")
			}
		case vectorrender.Polygon:
			style := "F"
			if op.Size > 0 {
				pdf.SetLineWidth(op.Size * scale)
				style = "DF"
			} else {
				pdf.SetFillColor(87, 109, 132)
			}
			var points []fpdf.PointType
			for _, point := range op.Points {
				points = append(points, fpdf.PointType{X: x + point.X*scale, Y: y + point.Y*scale})
			}
			pdf.Polygon(points, style)
		case vectorrender.Line:
			pdf.SetLineWidth(op.Size * scale)
			if op.Dashed {
				pdf.SetDashPattern([]float64{5 * scale, 4 * scale}, 0)
			}
			for index := 1; index < len(op.Points); index++ {
				from, to := op.Points[index-1], op.Points[index]
				pdf.Line(x+from.X*scale, y+from.Y*scale, x+to.X*scale, y+to.Y*scale)
			}
			if op.Dashed {
				pdf.SetDashPattern(nil, 0)
			}
		}
	}
	pdf.SetFontSize(printer.bodySize)
}
