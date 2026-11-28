package exportdocx

import (
	"bytes"
	"fmt"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const cellMargin = 90

func (doc *document) table(table *html.Node, ctx blockContext) {
	type row struct {
		cells  []*html.Node
		header bool
	}
	var rows []row
	columns := 0
	addRow := func(node *html.Node, header bool) {
		item := row{header: header}
		for cell := node.FirstChild; cell != nil; cell = cell.NextSibling {
			if cell.DataAtom == atom.Td || cell.DataAtom == atom.Th {
				item.cells = append(item.cells, cell)
			}
		}
		if len(item.cells) > 0 {
			columns = max(columns, len(item.cells))
			rows = append(rows, item)
		}
	}
	for node := table.FirstChild; node != nil; node = node.NextSibling {
		switch node.DataAtom {
		case atom.Tr:
			addRow(node, false)
		case atom.Thead, atom.Tbody, atom.Tfoot:
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				if child.DataAtom == atom.Tr {
					addRow(child, node.DataAtom == atom.Thead)
				}
			}
		}
	}
	if columns == 0 {
		return
	}
	out := &doc.body
	width := ctx.imageWidth()
	fmt.Fprintf(out, `<w:tbl><w:tblPr><w:tblW w:w="%d" w:type="dxa"/>`, width)
	if indent := ctx.width - width; indent > 0 {
		fmt.Fprintf(out, `<w:tblInd w:w="%d" w:type="dxa"/>`, indent)
	}
	out.write(`<w:tblBorders>`)
	for _, edge := range []string{"top", "left", "bottom", "right", "insideH", "insideV"} {
		fmt.Fprintf(out, `<w:%s w:val="single" w:sz="4" w:color="DCE2E6"/>`, edge)
	}
	out.write(`</w:tblBorders><w:tblLayout w:type="fixed"/><w:tblCellMar>`)
	for _, edge := range []string{"top", "left", "bottom", "right"} {
		fmt.Fprintf(out, `<w:%s w:w="%d" w:type="dxa"/>`, edge, cellMargin)
	}
	out.write(`</w:tblCellMar></w:tblPr><w:tblGrid>`)
	columnWidth := func(column int) int { return (column+1)*width/columns - column*width/columns }
	for column := range columns {
		fmt.Fprintf(out, `<w:gridCol w:w="%d"/>`, columnWidth(column))
	}
	out.write(`</w:tblGrid>`)
	for _, item := range rows {
		out.write(`<w:tr>`)
		if item.header {
			out.write(`<w:trPr><w:tblHeader/></w:trPr>`)
		}
		for column := range columns {
			if out.err != nil || doc.rels.err != nil {
				return
			}
			cellWidth := columnWidth(column)
			fmt.Fprintf(out, `<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/>`, cellWidth)
			if item.header || column < len(item.cells) && item.cells[column].DataAtom == atom.Th {
				out.write(`<w:shd w:val="clear" w:color="auto" w:fill="EDF2F5"/>`)
			}
			out.write(`</w:tcPr>`)
			if column < len(item.cells) {
				doc.blocks(item.cells[column], blockContext{width: max(1, cellWidth-2*cellMargin)})
			}
			if !bytes.HasSuffix(out.data.Bytes(), []byte(`</w:p>`)) {
				out.write(`<w:p/>`)
			}
			out.write(`</w:tc>`)
		}
		out.write(`</w:tr>`)
	}
	out.write(`</w:tbl>`)
}
