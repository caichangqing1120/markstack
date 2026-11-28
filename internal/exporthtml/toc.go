package exporthtml

import (
	"bytes"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/util"
)

type pdfContentsNode struct{ ast.BaseBlock }

var kindPDFContents = ast.NewNodeKind("MarkStackPDFContents")

func (*pdfContentsNode) Kind() ast.NodeKind { return kindPDFContents }

func (node *pdfContentsNode) Dump(source []byte, level int) {
	ast.DumpHelper(node, source, level, nil, nil)
}

func markPDFContents(root ast.Node, source []byte) {
	for node := root.FirstChild(); node != nil; {
		next := node.NextSibling()
		if node.Kind() == ast.KindParagraph && node.Lines().Len() == 1 && bytes.EqualFold(bytes.TrimSpace(node.Lines().Value(source)), []byte("[TOC]")) {
			root.ReplaceChild(root, node, &pdfContentsNode{})
		}
		node = next
	}
}

func renderPDFContents(writer util.BufWriter, _ []byte, _ ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	_, err := writer.WriteString("<markstack-toc></markstack-toc>")
	return ast.WalkSkipChildren, err
}
