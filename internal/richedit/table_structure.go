package richedit

import (
	"bytes"
	"crypto/sha256"
	"strings"

	htmltoken "golang.org/x/net/html"
)

func InsertTableRow(source []byte, document Document, lastIndex int, newline string) ([]byte, int, error) {
	if lastIndex < 0 || lastIndex >= len(document.Blocks) || newline != "\n" && newline != "\r\n" {
		return nil, 0, ErrReadOnlyLeaf
	}
	block := document.Blocks[lastIndex]
	if block.Kind != TableCell || block.ReadOnly || len(block.Leaves) == 0 {
		return nil, 0, ErrReadOnlyLeaf
	}
	if block.Leaves[0].sourceDigest != sha256.Sum256(source) {
		return nil, 0, ErrStaleLeaf
	}
	if lastIndex+1 < len(document.Blocks) {
		next := document.Blocks[lastIndex+1]
		if next.Kind == TableCell && next.TableStart == block.TableStart {
			return nil, 0, ErrReadOnlyLeaf
		}
	}
	rowEnd, columns, cellOpen, err := lastTableRow(source, block.TableStart)
	if err != nil || columns < 1 || columns > 64 || block.Column != columns-1 || rowEnd < block.End {
		return nil, 0, ErrReadOnlyLeaf
	}
	var inserted strings.Builder
	inserted.WriteString(newline)
	inserted.WriteString("<tr>")
	for range columns {
		inserted.WriteString(cellOpen)
		inserted.WriteString("<br></td>")
	}
	inserted.WriteString("</tr>")
	next := make([]byte, 0, len(source)+inserted.Len())
	next = append(next, source[:rowEnd]...)
	next = append(next, inserted.String()...)
	next = append(next, source[rowEnd:]...)
	parsed := Parse(next)
	if len(parsed.Blocks) != len(document.Blocks)+columns {
		return nil, 0, ErrReadOnlyLeaf
	}
	for index, before := range document.Blocks {
		mapped := index
		if index > lastIndex {
			mapped += columns
		}
		after := parsed.Blocks[mapped]
		if before.Kind != after.Kind || before.Visible != after.Visible || before.ReadOnly != after.ReadOnly {
			return nil, 0, ErrReadOnlyLeaf
		}
	}
	for column := range columns {
		cell := parsed.Blocks[lastIndex+1+column]
		if cell.Kind != TableCell || cell.ReadOnly || cell.Visible != "" || cell.Row != block.Row+1 || cell.Column != column || cell.TableStart != block.TableStart || len(cell.Leaves) != 1 || !cell.Leaves[0].Editable {
			return nil, 0, ErrReadOnlyLeaf
		}
	}
	return next, lastIndex + 1, nil
}

func lastTableRow(source []byte, tableStart int) (int, int, string, error) {
	if tableStart < 0 || tableStart >= len(source) {
		return 0, 0, "", ErrReadOnlyLeaf
	}
	tokenizer := htmltoken.NewTokenizer(bytes.NewReader(source[tableStart:]))
	var stack []string
	offset, rowEnd, columns, rowColumns := tableStart, 0, 0, 0
	tbodyCount, tbodyCloseStart, tbodyCloseEnd := 0, -1, -1
	canonicalMkDown := false
	for {
		kind := tokenizer.Next()
		if kind == htmltoken.ErrorToken {
			return 0, 0, "", ErrReadOnlyLeaf
		}
		raw := tokenizer.Raw()
		start, end := offset, offset+len(raw)
		offset = end
		switch kind {
		case htmltoken.StartTagToken:
			token := tokenizer.Token()
			tag := strings.ToLower(token.Data)
			parent := ""
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			}
			switch tag {
			case "table":
				if start != tableStart || len(stack) != 0 {
					return 0, 0, "", ErrReadOnlyLeaf
				}
				if len(token.Attr) > 0 {
					if len(token.Attr) != 1 || !strings.EqualFold(token.Attr[0].Key, "style") || token.Attr[0].Val != "width: auto;" {
						return 0, 0, "", ErrReadOnlyLeaf
					}
					canonicalMkDown = true
				}
				stack = append(stack, tag)
			case "tbody":
				if parent != "table" || len(token.Attr) != 0 || tbodyCount != 0 {
					return 0, 0, "", ErrReadOnlyLeaf
				}
				tbodyCount++
				stack = append(stack, tag)
			case "tr":
				if parent != "table" && parent != "tbody" || len(token.Attr) != 0 {
					return 0, 0, "", ErrReadOnlyLeaf
				}
				rowColumns = 0
				stack = append(stack, tag)
			case "td", "th":
				if parent != "tr" || !tableCellAttributes(token.Attr, canonicalMkDown) {
					return 0, 0, "", ErrReadOnlyLeaf
				}
				rowColumns++
				stack = append(stack, tag)
			default:
				if parent == "table" || parent == "tbody" || parent == "tr" {
					return 0, 0, "", ErrReadOnlyLeaf
				}
			}
		case htmltoken.EndTagToken:
			tag := strings.ToLower(tokenizer.Token().Data)
			if tag != "table" && tag != "tbody" && tag != "tr" && tag != "td" && tag != "th" {
				continue
			}
			if len(stack) == 0 || stack[len(stack)-1] != tag {
				return 0, 0, "", ErrReadOnlyLeaf
			}
			stack = stack[:len(stack)-1]
			switch tag {
			case "tr":
				rowEnd, columns = end, rowColumns
			case "tbody":
				tbodyCloseStart, tbodyCloseEnd = start, end
			case "table":
				boundary := start
				if tbodyCount == 1 {
					boundary = tbodyCloseStart
					if tbodyCloseEnd < 0 || strings.TrimSpace(string(source[tbodyCloseEnd:start])) != "" {
						return 0, 0, "", ErrReadOnlyLeaf
					}
				}
				if rowEnd == 0 || boundary < rowEnd || strings.TrimSpace(string(source[rowEnd:boundary])) != "" {
					return 0, 0, "", ErrReadOnlyLeaf
				}
				cellOpen := "<td>"
				if canonicalMkDown {
					cellOpen = `<td colSpan="1" rowSpan="1" width="auto">`
				}
				return rowEnd, columns, cellOpen, nil
			}
		case htmltoken.TextToken, htmltoken.CommentToken:
			if len(stack) > 0 && (stack[len(stack)-1] == "table" || stack[len(stack)-1] == "tbody" || stack[len(stack)-1] == "tr") && (kind == htmltoken.CommentToken || strings.TrimSpace(string(raw)) != "") {
				return 0, 0, "", ErrReadOnlyLeaf
			}
		}
	}
}

func tableCellAttributes(attributes []htmltoken.Attribute, canonicalMkDown bool) bool {
	if !canonicalMkDown {
		return len(attributes) == 0
	}
	if len(attributes) != 3 {
		return false
	}
	seen := make(map[string]bool, 3)
	for _, attribute := range attributes {
		key := strings.ToLower(attribute.Key)
		if seen[key] {
			return false
		}
		seen[key] = true
		switch key {
		case "colspan", "rowspan":
			if attribute.Val != "1" {
				return false
			}
		case "width":
			if attribute.Val != "auto" {
				return false
			}
		default:
			return false
		}
	}
	return seen["colspan"] && seen["rowspan"] && seen["width"]
}
