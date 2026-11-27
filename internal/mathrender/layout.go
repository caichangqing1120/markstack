package mathrender

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"markstack/internal/vectorrender"
)

var ErrUnsupported = errors.New("unsupported or malformed LaTeX")

const maxFormula = 4096

type expression struct {
	kind         string
	value        string
	children     []expression
	upper, lower *expression
	rows         [][]expression
}

type parser struct {
	source string
	pos    int
	failed bool
}

var commands = map[string]string{
	"alpha": "α", "beta": "β", "gamma": "γ", "delta": "δ", "epsilon": "ϵ", "varepsilon": "ε", "zeta": "ζ",
	"eta": "η", "theta": "θ", "vartheta": "ϑ", "iota": "ι", "kappa": "κ", "lambda": "λ", "mu": "μ", "nu": "ν",
	"xi": "ξ", "omicron": "ο", "pi": "π", "varpi": "ϖ", "rho": "ρ", "varrho": "ϱ", "sigma": "σ", "varsigma": "ς",
	"tau": "τ", "upsilon": "υ", "phi": "ϕ", "varphi": "φ", "chi": "χ", "psi": "ψ", "omega": "ω",
	"Alpha": "Α", "Beta": "Β", "Gamma": "Γ", "Delta": "Δ", "Epsilon": "Ε", "Zeta": "Ζ", "Eta": "Η", "Theta": "Θ",
	"Iota": "Ι", "Kappa": "Κ", "Lambda": "Λ", "Mu": "Μ", "Nu": "Ν", "Xi": "Ξ", "Omicron": "Ο", "Pi": "Π",
	"Rho": "Ρ", "Sigma": "Σ", "Tau": "Τ", "Upsilon": "Υ", "Phi": "Φ", "Chi": "Χ", "Psi": "Ψ", "Omega": "Ω",
	"sum": "∑", "int": "∫", "prod": "∏", "le": "≤", "leq": "≤", "ge": "≥", "geq": "≥", "ne": "≠", "neq": "≠",
	"approx": "≈", "infty": "∞", "to": "→", "rightarrow": "→", "times": "×", "cdot": "·", "pm": "±", "ldots": "…",
	"sin": "sin", "cos": "cos", "log": "log", "lim": "lim", "ln": "ln", "exp": "exp", "partial": "∂", "in": "∈",
	"{": "{", "}": "}", "$": "$", "_": "_", "%": "%", "#": "#", "&": "&", "|": "‖", ",": " ", ";": "  ", " ": " ",
}

func Layout(source string, size float64, measure vectorrender.Measure) (vectorrender.Scene, error) {
	if len(source) > maxFormula || size <= 0 || !utf8.ValidString(source) {
		return vectorrender.Scene{}, ErrUnsupported
	}
	parse := parser{source: source}
	root := parse.row(0, "")
	if parse.failed || parse.pos != len(source) || len(root.children) == 0 {
		return vectorrender.Scene{}, ErrUnsupported
	}
	if measure == nil {
		measure = vectorrender.Approximate
	}
	box := place(root, size, measure)
	scene := vectorrender.Scene{Width: box.width + 2, Height: box.up + box.down + 2, Baseline: box.up + 1}
	box.position(&scene.Ops, 1, scene.Baseline)
	return scene, nil
}

func (parse *parser) rest() string { return parse.source[parse.pos:] }

func (parse *parser) take(prefix string) bool {
	if !strings.HasPrefix(parse.rest(), prefix) {
		return false
	}
	parse.pos += len(prefix)
	return true
}

func (parse *parser) skip() {
	for parse.pos < len(parse.source) && strings.ContainsRune(" \t\r\n", rune(parse.source[parse.pos])) {
		parse.pos++
	}
}

func (parse *parser) row(depth int, stop string) expression {
	result := expression{kind: "row"}
	for !parse.failed {
		parse.skip()
		remaining := parse.rest()
		if remaining == "" || stop == "}" && remaining[0] == '}' || stop == `\right` && strings.HasPrefix(remaining, stop) ||
			stop == "matrix" && (remaining[0] == '&' || strings.HasPrefix(remaining, `\\`) || strings.HasPrefix(remaining, `\end`)) {
			break
		}
		atom := parse.atom(depth)
		for !parse.failed {
			parse.skip()
			var target **expression
			switch {
			case parse.take("^"):
				target = &atom.upper
			case parse.take("_"):
				target = &atom.lower
			default:
			}
			if target == nil {
				break
			}
			if *target != nil {
				parse.failed = true
				break
			}
			argument := parse.atom(depth + 1)
			*target = &argument
		}
		result.children = append(result.children, atom)
	}
	return result
}

func (parse *parser) group(depth int) expression {
	parse.skip()
	if !parse.take("{") {
		parse.failed = true
		return expression{}
	}
	result := parse.row(depth, "}")
	if !parse.take("}") {
		parse.failed = true
	}
	return result
}

func (parse *parser) command() string {
	start := parse.pos
	for parse.pos < len(parse.source) {
		character := parse.source[parse.pos]
		if character < 'a' || character > 'z' {
			if character < 'A' || character > 'Z' {
				break
			}
		}
		parse.pos++
	}
	if parse.pos == start && start < len(parse.source) {
		_, length := utf8.DecodeRuneInString(parse.rest())
		parse.pos += length
	}
	return parse.source[start:parse.pos]
}

func (parse *parser) delimiter() expression {
	parse.skip()
	if parse.take("\\") {
		if value := commands[parse.command()]; value == "{" || value == "}" || value == "‖" {
			return expression{value: value}
		}
	} else if rest := parse.rest(); rest != "" && strings.ContainsRune("()[]|.", rune(rest[0])) {
		parse.pos++
		if rest[0] != '.' {
			return expression{value: rest[:1]}
		}
		return expression{}
	}
	parse.failed = true
	return expression{}
}

func (parse *parser) atom(depth int) expression {
	parse.skip()
	if depth > 24 || parse.rest() == "" {
		parse.failed = true
		return expression{}
	}
	if strings.HasPrefix(parse.rest(), "{") {
		return parse.group(depth + 1)
	}
	if parse.take("\\") {
		command := parse.command()
		switch command {
		case "frac":
			return expression{kind: command, children: []expression{parse.group(depth + 1), parse.group(depth + 1)}}
		case "sqrt":
			return expression{kind: command, children: []expression{parse.group(depth + 1)}}
		case "left":
			left := parse.delimiter()
			body := parse.row(depth+1, `\right`)
			if !parse.take(`\right`) {
				parse.failed = true
			}
			return expression{kind: "delimiters", children: []expression{left, body, parse.delimiter()}}
		case "begin":
			return parse.matrix(depth + 1)
		default:
			if value, ok := commands[command]; ok {
				return expression{value: value}
			}
		}
	} else {
		value, length := utf8.DecodeRuneInString(parse.rest())
		if value <= 0xffff && !unicode.IsControl(value) && value != 0xfffe && value != 0xffff && !strings.ContainsRune("}$^_&#%", value) {
			parse.pos += length
			if value == '-' {
				value = '−'
			}
			return expression{value: string(value)}
		}
	}
	parse.failed = true
	return expression{}
}

func (parse *parser) matrix(depth int) expression {
	var environment string
	for _, name := range []string{"matrix", "pmatrix", "bmatrix"} {
		if parse.take("{" + name + "}") {
			environment = name
			break
		}
	}
	result := expression{kind: "matrix"}
	for environment != "" && !parse.failed && len(result.rows) < 16 {
		var row []expression
		for len(row) < 16 {
			row = append(row, parse.row(depth, "matrix"))
			if !parse.take("&") {
				break
			}
		}
		if len(result.rows) > 0 && len(row) != len(result.rows[0]) {
			break
		}
		result.rows = append(result.rows, row)
		if parse.take(`\end{` + environment + `}`) {
			switch environment {
			case "pmatrix":
				return expression{kind: "delimiters", children: []expression{{value: "("}, result, {value: ")"}}}
			case "bmatrix":
				return expression{kind: "delimiters", children: []expression{{value: "["}, result, {value: "]"}}}
			default:
				return result
			}
		}
		if !parse.take(`\\`) {
			break
		}
	}
	parse.failed = true
	return expression{}
}

type box struct {
	width, up, down float64
	position        func(*[]vectorrender.Op, float64, float64)
}

func place(expr expression, size float64, measure vectorrender.Measure) box {
	base := placeBase(expr, size, measure)
	if expr.upper == nil && expr.lower == nil {
		return base
	}
	upper, lower := box{}, box{}
	upperY, lowerY := -max(size*.57, base.up-size*.27), max(size*.33, base.down+size*.12)
	if expr.upper != nil {
		upper = place(*expr.upper, size*.68, measure)
	}
	if expr.lower != nil {
		lower = place(*expr.lower, size*.68, measure)
	}
	x := base.width + size*.05
	return box{x + max(upper.width, lower.width), max(base.up, upper.up-upperY), max(base.down, lowerY+lower.down), func(ops *[]vectorrender.Op, left, baseline float64) {
		base.position(ops, left, baseline)
		if upper.position != nil {
			upper.position(ops, left+x, baseline+upperY)
		}
		if lower.position != nil {
			lower.position(ops, left+x, baseline+lowerY)
		}
	}}
}

func placeBase(expr expression, size float64, measure vectorrender.Measure) box {
	switch expr.kind {
	case "row":
		parts := make([]box, len(expr.children))
		result := box{up: size * .8, down: size * .23}
		for index, child := range expr.children {
			part := place(child, size, measure)
			parts[index] = part
			result.width += part.width
			result.up, result.down = max(result.up, part.up), max(result.down, part.down)
		}
		result.position = func(ops *[]vectorrender.Op, x, y float64) {
			for _, part := range parts {
				part.position(ops, x, y)
				x += part.width
			}
		}
		return result
	case "frac":
		above := place(expr.children[0], size*.83, measure)
		below := place(expr.children[1], size*.83, measure)
		width := max(above.width, below.width) + size*.32
		axis, gap := -size*.28, size*.16
		return box{width, -axis + gap + above.down + above.up, axis + gap + below.up + below.down, func(ops *[]vectorrender.Op, x, y float64) {
			above.position(ops, x+(width-above.width)/2, y+axis-gap-above.down)
			below.position(ops, x+(width-below.width)/2, y+axis+gap+below.up)
			line(ops, max(.6, size*.065), vectorrender.Point{X: x, Y: y + axis}, vectorrender.Point{X: x + width, Y: y + axis})
		}}
	case "sqrt":
		body := place(expr.children[0], size, measure)
		lead, top := size*.7, -body.up-size*.12
		return box{lead + body.width + size*.12, -top + 1, body.down, func(ops *[]vectorrender.Op, x, y float64) {
			body.position(ops, x+lead, y)
			line(ops, max(.6, size*.065), vectorrender.Point{X: x, Y: y - size*.18}, vectorrender.Point{X: x + lead*.28, Y: y - size*.28}, vectorrender.Point{X: x + lead*.55, Y: y + body.down}, vectorrender.Point{X: x + lead*.92, Y: y + top}, vectorrender.Point{X: x + lead + body.width + size*.1, Y: y + top})
		}}
	case "matrix":
		return placeMatrix(expr.rows, size, measure)
	case "delimiters":
		body := place(expr.children[1], size, measure)
		fontSize := max(size, (body.up+body.down)*.88)
		left, right := place(expr.children[0], fontSize, measure), place(expr.children[2], fontSize, measure)
		shift := (body.down - body.up + left.up - left.down) / 2
		return box{left.width + body.width + right.width, max(body.up, left.up-shift), max(body.down, left.down+shift), func(ops *[]vectorrender.Op, x, y float64) {
			left.position(ops, x, y+shift)
			body.position(ops, x+left.width, y)
			right.position(ops, x+left.width+body.width, y+shift)
		}}
	default:
		if strings.Contains("∑∫∏", expr.value) && expr.value != "" {
			size *= 1.25
		}
		width := measure(expr.value, size)
		return box{width, size * .8, size * .23, func(ops *[]vectorrender.Op, x, y float64) {
			if expr.value != "" {
				*ops = append(*ops, vectorrender.Op{Kind: vectorrender.Text, At: vectorrender.Point{X: x, Y: y}, Width: width, Size: size, Text: expr.value})
			}
		}}
	}
}

func placeMatrix(rows [][]expression, size float64, measure vectorrender.Measure) box {
	columnWidths := make([]float64, len(rows[0]))
	type matrixRow struct {
		cells    []box
		up, down float64
	}
	var layout []matrixRow
	var height, width float64
	gap := size * .36
	for _, row := range rows {
		var line matrixRow
		for column, cell := range row {
			value := place(cell, size*.88, measure)
			line.cells = append(line.cells, value)
			line.up, line.down = max(line.up, value.up), max(line.down, value.down)
			columnWidths[column] = max(columnWidths[column], value.width)
		}
		layout = append(layout, line)
		height += line.up + line.down + gap
	}
	for _, column := range columnWidths {
		width += column + gap
	}
	up := (height-gap)/2 + size*.25
	return box{width - gap, up, height - gap - up, func(ops *[]vectorrender.Op, x, y float64) {
		rowTop := y - up
		for _, row := range layout {
			left := x
			for column, cell := range row.cells {
				cell.position(ops, left+(columnWidths[column]-cell.width)/2, rowTop+row.up)
				left += columnWidths[column] + gap
			}
			rowTop += row.up + row.down + gap
		}
	}}
}

func line(ops *[]vectorrender.Op, width float64, points ...vectorrender.Point) {
	*ops = append(*ops, vectorrender.Op{Kind: vectorrender.Line, Size: width, Points: points})
}
