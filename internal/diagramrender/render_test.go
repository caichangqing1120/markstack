package diagramrender

import (
	"strings"
	"testing"

	"markstack/internal/vectorrender"
)

func TestFlowchartDirectionsAndShapes(t *testing.T) {
	for _, direction := range []string{"TD", "TB", "LR", "RL"} {
		t.Run(direction, func(t *testing.T) {
			scene, err := Layout("flowchart "+direction+"\nA[开始] -->|yes| B(Check) -.-> C{Decision}\nC --> A", nil)
			if err != nil {
				t.Fatal(err)
			}
			var nodes []vectorrender.Op
			var arrows, lines, dashed int
			for _, op := range scene.Ops {
				switch {
				case op.Kind == vectorrender.Rect || op.Kind == vectorrender.Polygon && op.Size > 0:
					nodes = append(nodes, op)
				case op.Kind == vectorrender.Polygon:
					arrows++
				case op.Kind == vectorrender.Line:
					lines++
					if op.Dashed {
						dashed++
					}
				}
			}
			if len(nodes) != 3 || nodes[0].Radius != 0 || nodes[1].Radius == 0 || nodes[2].Kind != vectorrender.Polygon || arrows != 3 || lines != 3 || dashed != 1 {
				t.Fatalf("nodes=%v arrows=%d lines=%d dashed=%d", nodes, arrows, lines, dashed)
			}
			first, last := nodes[0].At, nodes[2].At
			if direction == "LR" && first.X >= last.X || direction == "RL" && first.X <= last.X || (direction == "TD" || direction == "TB") && first.Y >= last.Y {
				t.Fatalf("wrong rank direction: %v → %v", first, last)
			}
			if !strings.Contains(scene.SVG("Flow", false), ">yes</text>") {
				t.Fatal("edge label missing")
			}
		})
	}
}

func TestSequenceAndFallback(t *testing.T) {
	input := "sequenceDiagram\nparticipant A as 客户端\nparticipant B as Server\nA->>B: request\nB-->>A: <script>alert(1)</script>\nA->>A: retry"
	scene, err := Layout(input, nil)
	if err != nil || scene.Height < 200 {
		t.Fatalf("sequence: %+v %v", scene, err)
	}
	svg := scene.SVG("Sequence", false)
	if !strings.Contains(svg, ">客户端</text>") || strings.Contains(svg, "<script>") || !strings.Contains(svg, "&lt;script&gt;") || strings.Count(svg, "stroke-dasharray") != 3 {
		t.Fatalf("missing lifelines or escaped labels: %s", svg)
	}
	for _, input := range []string{"pie\nA : 1", "gantt\ntitle example", "flowchart LR\nA -->|broken B", "graph TD\nA[broken", "sequenceDiagram\nA->B: unsupported", "graph LR\nsubgraph X"} {
		if scene, err := Layout(input, nil); err == nil || len(scene.Ops) != 0 {
			t.Errorf("unexpected rendering of %q", input)
		}
	}
}
