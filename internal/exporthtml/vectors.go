package exporthtml

import (
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"

	"markstack/internal/diagramrender"
	"markstack/internal/mathrender"
	"markstack/internal/vectorrender"
)

type Vector struct {
	Scene  vectorrender.Scene
	Label  string
	Inline bool
	Body   string
	Math   bool
}

type vectorNode struct {
	ast.BaseInline
	index int
}

var kindVector = ast.NewNodeKind("MarkStackVector")

func (*vectorNode) Kind() ast.NodeKind                 { return kindVector }
func (node *vectorNode) Dump(source []byte, level int) { ast.DumpHelper(node, source, level, nil, nil) }

type vectorRenderer struct{}

func (vectorRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(kindVector, func(writer util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		_, err := fmt.Fprintf(writer, `<markstack-vector data-index="%d"></markstack-vector>`, node.(*vectorNode).index)
		return ast.WalkSkipChildren, err
	})
}

var markdownParser = goldmark.New(goldmark.WithExtensions(extension.GFM, mathrender.Markdown{}),
	goldmark.WithRendererOptions(renderer.WithNodeRenderers(util.Prioritized(vectorRenderer{}, 100))))

func collectVectors(root ast.Node, source []byte, limit int) ([]Vector, error) {
	var vectors []Vector
	remaining := limit
	var visit func(ast.Node, bool) error
	visit = func(parent ast.Node, alt bool) error {
		for node := parent.FirstChild(); node != nil; {
			next := node.NextSibling()
			var vector Vector
			var err error
			switch typed := node.(type) {
			case *mathrender.Inline:
				if alt {
					parent.ReplaceChild(parent, node, ast.NewString(typed.Range.Value(source)))
					node = next
					continue
				}
				vector = Vector{Label: string(typed.Range.Value(source)), Inline: true, Body: typed.Body(source), Math: true}
				vector.Scene, _ = mathrender.Layout(vector.Body, 16, nil)
			case *mathrender.Display:
				vector = Vector{Label: string(source[typed.Start:typed.End]), Body: typed.Body(source), Math: true}
				vector.Scene, _ = mathrender.Layout(vector.Body, 21, nil)
			case *ast.FencedCodeBlock:
				if strings.EqualFold(string(typed.Language(source)), "mermaid") {
					vector.Body = string(typed.Lines().Value(source))
					vector.Scene, err = diagramrender.Layout(vector.Body, nil)
					if err == nil {
						vector.Label = "Mermaid 图表"
					}
				}
			}
			if vector.Label != "" {
				remaining -= len(vector.Scene.Ops)*80 + len(vector.Label) + len(vector.Body) + 128
				if remaining < 0 {
					return ErrTooLarge
				}
				parent.ReplaceChild(parent, node, &vectorNode{index: len(vectors)})
				vectors = append(vectors, vector)
			} else if err := visit(node, alt || node.Kind() == ast.KindImage); err != nil {
				return err
			}
			node = next
		}
		return nil
	}
	if err := visit(root, false); err != nil {
		return nil, err
	}
	return vectors, nil
}
