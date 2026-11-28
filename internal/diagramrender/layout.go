package diagramrender

import (
	"errors"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"markstack/internal/vectorrender"
)

var ErrUnsupported = errors.New("unsupported or malformed Mermaid")

const labelSize = 14

type point = vectorrender.Point

type node struct {
	label string
	shape byte
	box   vectorrender.Op
}

type edge struct {
	from, to int
	label    string
	dashed   bool
}

type diagram struct {
	ids     map[string]int
	nodes   []node
	edges   []edge
	measure vectorrender.Measure
}

func Layout(source string, measure vectorrender.Measure) (vectorrender.Scene, error) {
	if len(source) > 32768 || !utf8.ValidString(source) {
		return vectorrender.Scene{}, ErrUnsupported
	}
	var lines []string
	for line := range strings.SplitSeq(source, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "%%") {
			lines = append(lines, line)
		}
	}
	if len(lines) < 2 || len(lines) > 257 {
		return vectorrender.Scene{}, ErrUnsupported
	}
	if measure == nil {
		measure = vectorrender.Approximate
	}
	graph := diagram{ids: make(map[string]int), measure: measure}
	if lines[0] == "sequenceDiagram" {
		for _, line := range lines[1:] {
			if !graph.sequence(line) {
				return vectorrender.Scene{}, ErrUnsupported
			}
		}
		return graph.sequenceLayout(), nil
	}
	header := strings.Fields(strings.TrimSuffix(lines[0], ";"))
	if len(header) != 2 || header[0] != "flowchart" && header[0] != "graph" ||
		header[1] != "TD" && header[1] != "TB" && header[1] != "LR" && header[1] != "RL" {
		return vectorrender.Scene{}, ErrUnsupported
	}
	for _, line := range lines[1:] {
		if !graph.flow(line) {
			return vectorrender.Scene{}, ErrUnsupported
		}
	}
	return graph.flowLayout(header[1]), nil
}

func identifier(source string) (string, string) {
	end := 0
	for end < len(source) {
		character, length := utf8.DecodeRuneInString(source[end:])
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) && character != '_' {
			break
		}
		end += length
	}
	return source[:end], strings.TrimSpace(source[end:])
}

func labelValue(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) && len(value) >= 2 {
		value = value[1 : len(value)-1]
	}
	return value, len([]rune(value)) <= 100 && vectorrender.ValidLabel(value)
}

func (graph *diagram) declare(id, label string, shape byte) int {
	if id == "" || len(id) > 100 {
		return -1
	}
	if index, ok := graph.ids[id]; ok {
		if shape != 0 {
			graph.nodes[index] = node{label: label, shape: shape}
		}
		return index
	}
	if len(graph.nodes) == 64 {
		return -1
	}
	if label == "" {
		label = id
	}
	if !vectorrender.ValidLabel(label) {
		return -1
	}
	index := len(graph.nodes)
	graph.ids[id] = index
	graph.nodes = append(graph.nodes, node{label: label, shape: shape})
	return index
}

func (graph *diagram) flowNode(source string) (int, string) {
	id, rest := identifier(source)
	if id == "" {
		return -1, rest
	}
	switch id {
	case "subgraph", "end", "classDef", "class", "style", "linkStyle", "click", "direction":
		return -1, rest
	}
	if rest == "" {
		return graph.declare(id, "", 0), rest
	}
	var close byte
	switch rest[0] {
	case '[':
		close = ']'
	case '(':
		close = ')'
	case '{':
		close = '}'
	default:
		return graph.declare(id, "", 0), rest
	}
	quoted := false
	for index := 1; index < len(rest); index++ {
		if rest[index] == '"' {
			quoted = !quoted
		}
		if rest[index] == close && !quoted {
			label, ok := labelValue(rest[1:index])
			if !ok || label == "" {
				break
			}
			return graph.declare(id, label, rest[0]), strings.TrimSpace(rest[index+1:])
		}
	}
	return -1, rest
}

func (graph *diagram) flow(rest string) bool {
	for rest != "" {
		from, tail := graph.flowNode(rest)
		if from < 0 {
			return false
		}
		rest = tail
		for strings.HasPrefix(rest, "-->") || strings.HasPrefix(rest, "-.->") {
			dashed := rest[0:2] == "-."
			length := 3
			if dashed {
				length = 4
			}
			rest = strings.TrimSpace(rest[length:])
			var label string
			if strings.HasPrefix(rest, "|") {
				value, after, found := strings.Cut(rest[1:], "|")
				var valid bool
				label, valid = labelValue(value)
				if !found || !valid {
					return false
				}
				rest = strings.TrimSpace(after)
			}
			to, after := graph.flowNode(rest)
			if to < 0 || len(graph.edges) == 256 {
				return false
			}
			graph.edges = append(graph.edges, edge{from, to, label, dashed})
			from, rest = to, after
		}
		if rest == "" {
			return true
		}
		if rest[0] != ';' {
			return false
		}
		rest = strings.TrimSpace(rest[1:])
	}
	return true
}

func (graph *diagram) sequence(line string) bool {
	word, tail := identifier(line)
	if word == "participant" && len(line) > len(word) && (line[len(word)] == ' ' || line[len(word)] == '\t') {
		id, rest := identifier(tail)
		if rest == "" {
			return graph.declare(id, "", '(') >= 0
		}
		alias, label := identifier(rest)
		value, valid := labelValue(label)
		return alias == "as" && valid && value != "" && graph.declare(id, value, '(') >= 0
	}
	actors, rawLabel, found := strings.Cut(line, ":")
	label, valid := labelValue(rawLabel)
	if !found || !valid || len(graph.edges) == 256 {
		return false
	}
	from, rest := identifier(actors)
	dashed := strings.HasPrefix(rest, "-->>")
	if dashed {
		rest = rest[4:]
	} else if strings.HasPrefix(rest, "->>") {
		rest = rest[3:]
	} else {
		return false
	}
	to, extra := identifier(strings.TrimSpace(rest))
	if extra != "" {
		return false
	}
	start, end := graph.declare(from, "", 0), graph.declare(to, "", 0)
	if start < 0 || end < 0 {
		return false
	}
	graph.edges = append(graph.edges, edge{start, end, label, dashed})
	return true
}

func (graph *diagram) layers() ([][]int, []int) {
	outgoing := make([][]int, len(graph.nodes))
	for index, edge := range graph.edges {
		outgoing[edge.from] = append(outgoing[edge.from], index)
	}
	state := make([]byte, len(graph.nodes))
	back := make([]bool, len(graph.edges))
	var order []int
	var visit func(int)
	visit = func(from int) {
		state[from] = 1
		for _, index := range outgoing[from] {
			to := graph.edges[index].to
			if state[to] == 1 {
				back[index] = true
			} else if state[to] == 0 {
				visit(to)
			}
		}
		state[from] = 2
		order = append(order, from)
	}
	for index := range graph.nodes {
		if state[index] == 0 {
			visit(index)
		}
	}
	ranks := make([]int, len(graph.nodes))
	for index := len(order) - 1; index >= 0; index-- {
		from := order[index]
		for _, id := range outgoing[from] {
			if !back[id] {
				to := graph.edges[id].to
				ranks[to] = max(ranks[to], ranks[from]+1)
			}
		}
	}
	var layers [][]int
	for index, rank := range ranks {
		for len(layers) <= rank {
			layers = append(layers, nil)
		}
		layers[rank] = append(layers[rank], index)
	}
	return layers, ranks
}

func (graph *diagram) flowLayout(direction string) vectorrender.Scene {
	horizontal := direction == "LR" || direction == "RL"
	layers, ranks := graph.layers()
	const margin, separation = 24.0, 28.0
	mainGap := 64.0
	for _, edge := range graph.edges {
		if horizontal {
			mainGap = max(mainGap, graph.measure(edge.label, labelSize)+30)
		}
	}
	main, cross := make([]float64, len(layers)), make([]float64, len(layers))
	mainSize, crossSize := 2*margin, 0.0
	for rank, layer := range layers {
		for _, index := range layer {
			item := &graph.nodes[index]
			width, height := max(78, graph.measure(item.label, labelSize)+26), 42.0
			if item.shape == '{' {
				width, height = width*1.65, 66
			}
			item.box = vectorrender.Op{Kind: vectorrender.Rect, Width: width, Height: height, Size: 1.4}
			if horizontal {
				width, height = height, width
			}
			main[rank] = max(main[rank], height)
			cross[rank] += width + separation
		}
		cross[rank] -= separation
		mainSize += main[rank]
		crossSize = max(crossSize, cross[rank])
	}
	mainSize += float64(len(layers)-1) * mainGap
	for _, edge := range graph.edges {
		if !horizontal {
			crossSize = max(crossSize, graph.measure(edge.label, labelSize)+24)
		}
	}
	crossSize += 2 * margin
	mainAt := margin
	for rank, layer := range layers {
		crossAt := (crossSize - cross[rank]) / 2
		for _, index := range layer {
			box := &graph.nodes[index].box
			if horizontal {
				box.At = point{X: mainAt + (main[rank]-box.Width)/2, Y: crossAt}
				crossAt += box.Height + separation
				if direction == "RL" {
					box.At.X = mainSize - box.At.X - box.Width
				}
			} else {
				box.At = point{X: crossAt, Y: mainAt + (main[rank]-box.Height)/2}
				crossAt += box.Width + separation
			}
		}
		mainAt += main[rank] + mainGap
	}
	scene := vectorrender.Scene{Width: crossSize, Height: mainSize}
	if horizontal {
		scene.Width, scene.Height = mainSize, crossSize
	}
	var labels []vectorrender.Op
	backLane := 0.0
	for _, edge := range graph.edges {
		from, to := graph.nodes[edge.from].box, graph.nodes[edge.to].box
		a, b := center(from), center(to)
		var points []point
		var at point
		if ranks[edge.to] != ranks[edge.from]+1 {
			backLane += 17
			if horizontal {
				lane := crossSize - margin + backLane
				a.Y += from.Height / 2
				b.Y += to.Height / 2
				if edge.from == edge.to {
					a.X -= from.Width * .25
					b.X += to.Width * .25
					if graph.nodes[edge.from].shape == '{' {
						a.Y -= from.Height * .25
						b.Y -= to.Height * .25
					}
				}
				points = []point{a, {X: a.X, Y: lane}, {X: b.X, Y: lane}, b}
				at = point{X: (a.X + b.X) / 2, Y: lane}
				scene.Height = max(scene.Height, lane+margin)
			} else {
				lane := crossSize - margin + backLane
				a.X += from.Width / 2
				b.X += to.Width / 2
				if edge.from == edge.to {
					a.Y -= from.Height * .25
					b.Y += to.Height * .25
					if graph.nodes[edge.from].shape == '{' {
						a.X -= from.Width * .25
						b.X -= to.Width * .25
					}
				}
				points = []point{a, {X: lane, Y: a.Y}, {X: lane, Y: b.Y}, b}
				at = point{X: lane, Y: (a.Y + b.Y) / 2}
				scene.Width = max(scene.Width, lane+graph.measure(edge.label, labelSize)/2+margin)
			}
		} else if horizontal {
			sign := math.Copysign(1, b.X-a.X)
			a.X += sign * from.Width / 2
			b.X -= sign * to.Width / 2
			mid := (a.X + b.X) / 2
			points = []point{a, {X: mid, Y: a.Y}, {X: mid, Y: b.Y}, b}
			at = point{X: mid, Y: (a.Y + b.Y) / 2}
		} else {
			a.Y += from.Height / 2
			b.Y -= to.Height / 2
			mid := (a.Y + b.Y) / 2
			points = []point{a, {X: a.X, Y: mid}, {X: b.X, Y: mid}, b}
			at = point{X: (a.X + b.X) / 2, Y: mid}
		}
		arrow(&scene, edge.dashed, points...)
		if edge.label != "" {
			text := graph.text(edge.label, at, true)
			text.At.X = max(6, min(text.At.X, scene.Width-text.Width-6))
			labels = append(labels, text)
		}
	}
	for _, item := range graph.nodes {
		graph.drawNode(&scene, item)
	}
	scene.Ops = append(scene.Ops, labels...)
	return scene
}

func (graph *diagram) sequenceLayout() vectorrender.Scene {
	const margin = 24.0
	widths := make([]float64, len(graph.nodes))
	spacing := 66.0
	for index, item := range graph.nodes {
		widths[index] = max(88, graph.measure(item.label, labelSize)+26)
	}
	maxSelf := 0.0
	for _, message := range graph.edges {
		width := graph.measure(message.label, labelSize)
		if message.from == message.to {
			maxSelf = max(maxSelf, width+20)
		} else {
			distance := max(message.to-message.from, message.from-message.to)
			spacing = max(spacing, (width+24)/float64(distance)-88)
		}
	}
	spacing = max(spacing, maxSelf)
	left, bottom := margin, 92+float64(len(graph.edges))*52
	scene := vectorrender.Scene{Height: bottom + margin}
	for index := range graph.nodes {
		item := &graph.nodes[index]
		item.shape = '('
		item.box = vectorrender.Op{Kind: vectorrender.Rect, At: point{X: left, Y: margin}, Width: widths[index], Height: 38, Size: 1.4}
		middle := center(item.box).X
		scene.Ops = append(scene.Ops, vectorrender.Op{Kind: vectorrender.Line, Size: 1.2, Dashed: true, Points: []point{{X: middle, Y: margin + 38}, {X: middle, Y: bottom}}})
		graph.drawNode(&scene, *item)
		left += widths[index] + spacing
	}
	scene.Width = left - spacing + margin
	for index, message := range graph.edges {
		from, to := center(graph.nodes[message.from].box).X, center(graph.nodes[message.to].box).X
		y := 99 + float64(index)*52
		if message.from == message.to {
			width := max(28, graph.measure(message.label, labelSize)+12)
			arrow(&scene, message.dashed, point{X: from, Y: y}, point{X: from + width, Y: y}, point{X: from + width, Y: y + 18}, point{X: from, Y: y + 18})
			to = from + width
			scene.Width = max(scene.Width, to+margin)
		} else {
			arrow(&scene, message.dashed, point{X: from, Y: y}, point{X: to, Y: y})
		}
		if message.label != "" {
			scene.Ops = append(scene.Ops, graph.text(message.label, point{X: (from + to) / 2, Y: y - 12}, false))
		}
	}
	return scene
}

func center(box vectorrender.Op) point {
	return point{X: box.At.X + box.Width/2, Y: box.At.Y + box.Height/2}
}

func (graph *diagram) drawNode(scene *vectorrender.Scene, item node) {
	box := item.box
	middle := center(box)
	switch item.shape {
	case '(':
		box.Radius = 8
	case '{':
		box.Kind = vectorrender.Polygon
		box.Points = []point{{X: box.At.X, Y: middle.Y}, {X: middle.X, Y: box.At.Y}, {X: box.At.X + box.Width, Y: middle.Y}, {X: middle.X, Y: box.At.Y + box.Height}}
	}
	scene.Ops = append(scene.Ops, box, graph.text(item.label, middle, false))
}

func (graph *diagram) text(value string, at point, label bool) vectorrender.Op {
	width := graph.measure(value, labelSize)
	return vectorrender.Op{Kind: vectorrender.Text, At: point{X: at.X - width/2, Y: at.Y + labelSize*.33}, Size: labelSize, Width: width, Text: value, Label: label}
}

func arrow(scene *vectorrender.Scene, dashed bool, points ...point) {
	start, end := points[len(points)-2], points[len(points)-1]
	length := math.Hypot(end.X-start.X, end.Y-start.Y)
	if length == 0 {
		return
	}
	ux, uy := (end.X-start.X)/length, (end.Y-start.Y)/length
	base := point{X: end.X - ux*8, Y: end.Y - uy*8}
	points[len(points)-1] = base
	scene.Ops = append(scene.Ops, vectorrender.Op{Kind: vectorrender.Line, Size: 1.45, Points: points, Dashed: dashed},
		vectorrender.Op{Kind: vectorrender.Polygon, Points: []point{end, {X: base.X - uy*4, Y: base.Y + ux*4}, {X: base.X + uy*4, Y: base.Y - ux*4}}})
}
