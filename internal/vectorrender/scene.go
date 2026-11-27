package vectorrender

import (
	"fmt"
	"html"
	"strings"
	"unicode/utf8"
)

type Point struct{ X, Y float64 }

type Kind uint8

const (
	Text Kind = iota
	Line
	Rect
	Polygon
)

type Op struct {
	Kind                  Kind
	At                    Point
	Width, Height, Radius float64
	Size                  float64
	Text                  string
	Points                []Point
	Dashed, Mono, Label   bool
}

type Scene struct {
	Width, Height, Baseline float64
	Ops                     []Op
}

type Measure func(string, float64) float64

func Approximate(value string, size float64) float64 {
	var units float64
	for _, character := range value {
		switch {
		case strings.ContainsRune("il.,:;'!|", character):
			units += .32
		case strings.ContainsRune("MWmw@∑∏", character):
			units += .9
		case character >= 0x2e80:
			units++
		default:
			units += .64
		}
	}
	return units * size
}

func ValidLabel(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if character < ' ' || character == 0x7f || character == 0xfffe || character == 0xffff || character > 0xffff {
			return false
		}
	}
	return true
}

func (scene Scene) SVG(label string, inline bool) string {
	var output strings.Builder
	class, align := "markstack-vector-block", ""
	if inline {
		class = "markstack-vector-inline"
		align = fmt.Sprintf(` style="vertical-align:-%.3fpx"`, scene.Height-scene.Baseline)
	}
	fmt.Fprintf(&output, `<svg xmlns="http://www.w3.org/2000/svg" class="%s" role="img" aria-label="%s" viewBox="0 0 %.3f %.3f" width="%.3f" height="%.3f"%s>`, class, html.EscapeString(label), scene.Width, scene.Height, scene.Width, scene.Height, align)
	for _, op := range scene.Ops {
		switch op.Kind {
		case Text:
			if op.Label {
				fmt.Fprintf(&output, `<rect x="%.3f" y="%.3f" width="%.3f" height="%.3f" rx="2" fill="#fff"/>`, op.At.X-3, op.At.Y-op.Size, op.Width+6, op.Size+4)
			}
			family := "sans-serif"
			if op.Mono {
				family = "monospace"
			}
			fmt.Fprintf(&output, `<text x="%.3f" y="%.3f" font-family="%s" font-size="%.3f" fill="#24292f">%s</text>`, op.At.X, op.At.Y, family, op.Size, html.EscapeString(op.Text))
		case Rect:
			fmt.Fprintf(&output, `<rect x="%.3f" y="%.3f" width="%.3f" height="%.3f" rx="%.3f" fill="#eaf2fa" stroke="#576d84" stroke-width="%.3f"/>`, op.At.X, op.At.Y, op.Width, op.Height, op.Radius, op.Size)
		case Line, Polygon:
			if op.Kind == Polygon {
				fill := "#576d84"
				if op.Size > 0 {
					fill = "#eaf2fa"
				}
				fmt.Fprintf(&output, `<polygon fill="%s"`, fill)
			} else {
				output.WriteString(`<polyline fill="none"`)
			}
			if op.Dashed {
				output.WriteString(` stroke-dasharray="5 4"`)
			}
			if op.Size > 0 {
				fmt.Fprintf(&output, ` stroke="#576d84" stroke-width="%.3f" stroke-linejoin="round"`, op.Size)
			}
			output.WriteString(` points="`)
			for _, point := range op.Points {
				fmt.Fprintf(&output, "%.3f,%.3f ", point.X, point.Y)
			}
			output.WriteString(`"/>`)
		}
	}
	output.WriteString(`</svg>`)
	return output.String()
}
