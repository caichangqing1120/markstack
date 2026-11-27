package markdownedit

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

var (
	ErrStaleBlock    = errors.New("Markdown block is stale")
	ErrReadOnlyBlock = errors.New("Markdown block requires source editing")
)

type Kind string

const (
	Heading    Kind = "heading"
	Paragraph  Kind = "paragraph"
	ListItem   Kind = "list-item"
	Quote      Kind = "quote"
	FencedCode Kind = "fenced-code"
	Table      Kind = "table"
	Raw        Kind = "raw"
)

type Block struct {
	Kind      Kind
	Start     int
	End       int
	Visible   string
	Editable  bool
	Comment   bool
	Code      CodeBody
	Cells     []Cell
	Task      bool
	Checked   bool
	TaskStart int
	List      *ListMarker
	source    [32]byte
	styled    bool
	textFrom  int
	textTo    int
	parts     []inlinePart
}

type CodeBody struct {
	Start    int
	End      int
	Visible  string
	Info     string
	Editable bool
}

type Cell struct {
	Row      int
	Column   int
	Start    int
	End      int
	Visible  string
	Editable bool
}

type inlinePart struct {
	visibleStart int
	visibleEnd   int
	sourceStart  int
	sourceEnd    int
	rawStart     int
	rawEnd       int
	crossSafe    bool
}

var parser = goldmark.New(goldmark.WithExtensions(extension.GFM))

func Parse(source []byte) []Block {
	if len(source) == 0 {
		return nil
	}
	fingerprint := sha256.Sum256(source)
	if !utf8.Valid(source) {
		return []Block{{Kind: Raw, Start: 0, End: len(source), source: fingerprint}}
	}
	reader := text.NewReader(source)
	if bytes.HasPrefix(source, []byte("\xef\xbb\xbf")) {
		reader.Advance(3)
	}
	root := parser.Parser().Parse(reader)
	count := root.ChildCount()
	for node := root.FirstChild(); node != nil; node = node.NextSibling() {
		if list, ok := node.(*ast.List); ok {
			count += countListBlocks(source, list, -1) - 1
		}
	}
	if count == 0 {
		return nil
	}
	blocks := make([]Block, 0, count)
	for node := root.FirstChild(); node != nil; node = node.NextSibling() {
		switch typed := node.(type) {
		case *ast.Heading:
			blocks = append(blocks, simple(source, typed, Heading, fingerprint))
		case *ast.Paragraph:
			blocks = append(blocks, simple(source, typed, Paragraph, fingerprint))
		case *ast.List:
			blocks = appendList(blocks, source, typed, fingerprint, 0, -1)
		case *ast.Blockquote:
			if child := typed.FirstChild(); child != nil && child.NextSibling() == nil && isTextBlock(child) {
				blocks = append(blocks, simple(source, child, Quote, fingerprint))
			} else {
				blocks = append(blocks, readOnly(source, node, Raw, fingerprint))
			}
		case *ast.FencedCodeBlock:
			block := readOnly(source, node, FencedCode, fingerprint)
			block.Code = fencedCodeBody(source, typed)
			blocks = append(blocks, block)
		case *extast.Table:
			block := tableBlock(source, typed, fingerprint)
			block.Cells = tableCells(source, typed)
			blocks = append(blocks, block)
		case *ast.HTMLBlock:
			block := readOnly(source, typed, Raw, fingerprint)
			end := block.End
			if typed.HasClosure() {
				end = typed.ClosureLine.Stop
			}
			if typed.HTMLBlockType == ast.HTMLBlockType2 && end >= block.End && end <= len(source) {
				raw := bytes.TrimSpace(source[block.Start:end])
				if bytes.HasPrefix(raw, []byte("<!--")) && bytes.HasSuffix(raw, []byte("-->")) && bytes.Count(raw, []byte("-->")) == 1 {
					block.Comment, block.End = true, end
				}
			}
			blocks = append(blocks, block)
		default:
			blocks = append(blocks, readOnly(source, node, Raw, fingerprint))
		}
	}
	return blocks
}

func fencedCodeBody(source []byte, node *ast.FencedCodeBlock) CodeBody {
	position := node.Pos()
	if position < 0 || position >= len(source) || position > 0 && source[position-1] != '\n' && !(position == 3 && bytes.Equal(source[:3], []byte{0xef, 0xbb, 0xbf})) {
		return CodeBody{}
	}
	marker := source[position]
	if marker != '`' && marker != '~' {
		return CodeBody{}
	}
	breakOffset := bytes.IndexByte(source[position:], '\n')
	if breakOffset < 0 {
		return CodeBody{}
	}
	lineEnd := position + breakOffset
	fenceLength := 0
	for position+fenceLength < lineEnd && source[position+fenceLength] == marker {
		fenceLength++
	}
	if fenceLength < 3 {
		return CodeBody{}
	}
	info := ""
	if node.Info != nil {
		segment := node.Info.Segment
		if segment.Start < position+fenceLength || segment.Stop > lineEnd || segment.Start > segment.Stop {
			return CodeBody{}
		}
		info = string(segment.Value(source))
	}
	newline := "\n"
	if lineEnd > position && source[lineEnd-1] == '\r' {
		newline = "\r\n"
	}
	start := lineEnd + 1
	for cursor := start; cursor < len(source); {
		end := len(source)
		hasBreak := false
		if offset := bytes.IndexByte(source[cursor:], '\n'); offset >= 0 {
			end, hasBreak = cursor+offset, true
		}
		line := source[cursor:end]
		if bytes.HasSuffix(line, []byte{'\r'}) {
			line = line[:len(line)-1]
			if newline != "\r\n" || !hasBreak {
				return CodeBody{}
			}
		} else if hasBreak && newline == "\r\n" {
			return CodeBody{}
		}
		closingLength := 0
		for closingLength < len(line) && line[closingLength] == marker {
			closingLength++
		}
		if closingLength >= fenceLength && len(bytes.Trim(line[closingLength:], " \t")) == 0 {
			body := source[start:cursor]
			if len(body) > 0 {
				if !bytes.HasSuffix(body, []byte(newline)) {
					return CodeBody{}
				}
				body = body[:len(body)-len(newline)]
			}
			for index := 0; index < node.Lines().Len(); index++ {
				segment := node.Lines().At(index)
				if segment.Start < start || segment.Stop > cursor || segment.Padding != 0 {
					return CodeBody{}
				}
			}
			return CodeBody{Start: start, End: cursor, Visible: strings.ReplaceAll(string(body), "\r\n", "\n"), Info: info, Editable: true}
		}
		if !hasBreak {
			break
		}
		cursor = end + 1
	}
	return CodeBody{}
}

func tableBlock(source []byte, table *extast.Table, fingerprint [32]byte) Block {
	block := readOnly(source, table, Table, fingerprint)
	first, last := table.FirstChild(), table.LastChild()
	if first == nil || last == nil {
		return block
	}
	start, end := first.Pos(), last.Pos()
	if start < 0 || end < start || end >= len(source) {
		return block
	}
	lineStart := bytes.LastIndexByte(source[:start], '\n') + 1
	if lineStart == 0 && start >= 3 && bytes.HasPrefix(source, []byte("\xef\xbb\xbf")) {
		lineStart = 3
	}
	if start-lineStart <= 3 && len(bytes.Trim(source[lineStart:start], " ")) == 0 {
		start = lineStart
	}
	if first == last {
		separator := bytes.IndexByte(source[end:], '\n')
		if separator < 0 {
			return block
		}
		end += separator + 1
	}
	if newline := bytes.IndexByte(source[end:], '\n'); newline >= 0 {
		end += newline
	} else {
		end = len(source)
	}
	if end > start && source[end-1] == '\r' {
		end--
	}
	block.Start, block.End = start, end
	return block
}

func tableCells(source []byte, table *extast.Table) []Cell {
	var cells []Cell
	row := 0
	for rowNode := table.FirstChild(); rowNode != nil; rowNode = rowNode.NextSibling() {
		column := 0
		for node := rowNode.FirstChild(); node != nil; node = node.NextSibling() {
			cell := Cell{Row: row, Column: column}
			column++
			if node.Lines().Len() != 1 {
				cells = append(cells, cell)
				continue
			}
			segment := node.Lines().At(0)
			if segment.Start < 0 || segment.Stop < segment.Start || segment.Stop > len(source) {
				cells = append(cells, cell)
				continue
			}
			cell.Start, cell.End = segment.Start, segment.Stop
			raw := source[cell.Start:cell.End]
			cell.Visible = string(raw)
			if bytes.ContainsAny(raw, "\\|&\r\n") {
				cells = append(cells, cell)
				continue
			}
			position := cell.Start
			for child := node.FirstChild(); child != nil; child = child.NextSibling() {
				textNode, ok := child.(*ast.Text)
				if !ok || textNode.IsRaw() || textNode.SoftLineBreak() || textNode.HardLineBreak() || textNode.Segment.Padding != 0 || textNode.Segment.Start != position {
					position = -1
					break
				}
				position = textNode.Segment.Stop
			}
			cell.Editable = position == cell.End
			cells = append(cells, cell)
		}
		row++
	}
	return cells
}

func taskBlock(source []byte, node ast.Node, fingerprint [32]byte) Block {
	block := readOnly(source, node, ListItem, fingerprint)
	if node.Lines().Len() == 0 {
		return block
	}
	checkbox, ok := node.FirstChild().(*extast.TaskCheckBox)
	if !ok {
		return block
	}
	start := node.Lines().At(0).Start
	if start < 0 || start+3 > len(source) || source[start] != '[' || source[start+2] != ']' || !bytes.ContainsRune([]byte(" xX"), rune(source[start+1])) {
		return block
	}
	block.Task, block.Checked, block.TaskStart = true, checkbox.IsChecked, start
	if node.Lines().Len() != 1 {
		return block
	}
	if checkbox.NextSibling() == nil {
		end := nextLine(source, start)
		end -= len(lineEndingBefore(source, end))
		block.Start, block.End, block.Editable = end, end, true
		return block
	}
	label, ok := checkbox.NextSibling().(*ast.Text)
	if !ok || label.IsRaw() || label.SoftLineBreak() || label.HardLineBreak() || label.Segment.Padding != 0 {
		return block
	}
	segment := label.Segment
	for sibling := label.NextSibling(); sibling != nil; sibling = sibling.NextSibling() {
		literal, ok := sibling.(*ast.Text)
		if !ok || literal.IsRaw() || literal.SoftLineBreak() || literal.HardLineBreak() || literal.Segment.Padding != 0 || literal.Segment.Start != segment.Stop || literal.Segment.Stop <= literal.Segment.Start || literal.Segment.Stop > len(source) {
			return block
		}
		segment.Stop = literal.Segment.Stop
	}
	if segment.Start < 4 || segment.Stop > len(source) || segment.Start >= segment.Stop || bytes.ContainsAny(source[segment.Start:segment.Stop], "\\&\r\n") {
		return block
	}
	if start+3 >= segment.Start || len(bytes.Trim(source[start+3:segment.Start], " \t")) != 0 {
		return block
	}
	block.Start, block.End = segment.Start, segment.Stop
	block.Visible, block.Editable = string(source[segment.Start:segment.Stop]), true
	return block
}

func isTextBlock(node ast.Node) bool {
	switch node.(type) {
	case *ast.Paragraph, *ast.TextBlock:
		return true
	default:
		return false
	}
}

func simple(source []byte, node ast.Node, kind Kind, fingerprint [32]byte) Block {
	block := readOnly(source, node, kind, fingerprint)
	if node.Lines().Len() != 1 {
		return block
	}
	if kind == Heading {
		lineStart := bytes.LastIndexByte(source[:block.Start], '\n') + 1
		prefix := bytes.TrimSpace(source[lineStart:block.Start])
		if len(prefix) == 0 || len(prefix) > 6 || len(bytes.Trim(prefix, "#")) != 0 {
			return block
		}
	}
	if kind == Paragraph {
		child := node.FirstChild()
		if child != nil && child.NextSibling() == nil {
			var label ast.Node
			switch typed := child.(type) {
			case *ast.Emphasis:
				if typed.Level == 1 || typed.Level == 2 {
					label = typed.FirstChild()
				}
			case *ast.Link:
				label = typed.FirstChild()
			}
			if inline, ok := label.(*ast.Text); ok && inline.NextSibling() == nil && !inline.IsRaw() && !inline.SoftLineBreak() && !inline.HardLineBreak() && inline.Segment.Padding == 0 {
				segment := inline.Segment
				if segment.Start >= block.Start && segment.Stop <= block.End && segment.Start < segment.Stop && !bytes.ContainsAny(source[segment.Start:segment.Stop], "\\&\r\n") {
					block.Visible = string(source[segment.Start:segment.Stop])
					block.Editable, block.styled = true, true
					block.textFrom, block.textTo = segment.Start, segment.Stop
					return block
				}
			}
		}
	}
	start, end := -1, -1
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		inline, ok := child.(*ast.Text)
		if !ok {
			if kind == Paragraph || kind == Heading {
				mixed := mixedInline(source, node, block)
				if kind == Heading {
					for _, part := range mixed.parts {
						if !part.crossSafe {
							return block
						}
					}
				}
				return mixed
			}
			return block
		}
		if inline.SoftLineBreak() || inline.HardLineBreak() || inline.IsRaw() {
			return block
		}
		segment := inline.Segment
		if segment.Padding != 0 || segment.Start < 0 || segment.Stop > len(source) || segment.Start >= segment.Stop {
			return block
		}
		if start < 0 {
			start = segment.Start
		} else if segment.Start < end || len(bytes.Trim(source[end:segment.Start], " \t")) != 0 {
			return block
		}
		end = segment.Stop
	}
	if start < 0 {
		return block
	}
	visible := source[start:end]
	if bytes.ContainsAny(visible, "\\&\r\n") {
		return block
	}
	block.Start, block.End = start, end
	block.Visible, block.Editable = string(visible), true
	return block
}

func mixedInline(source []byte, node ast.Node, block Block) Block {
	visible := make([]byte, 0, block.End-block.Start)
	var parts []inlinePart
	var collect func(ast.Node) bool
	collect = func(current ast.Node) bool {
		switch typed := current.(type) {
		case *ast.Text:
			segment := typed.Segment
			if typed.IsRaw() || typed.SoftLineBreak() || typed.HardLineBreak() || segment.Padding != 0 || segment.Start < block.Start || segment.Stop > block.End || segment.Start >= segment.Stop || bytes.ContainsAny(source[segment.Start:segment.Stop], "\\&\r\n") {
				return false
			}
			start := len(visible)
			visible = append(visible, source[segment.Start:segment.Stop]...)
			parts = append(parts, inlinePart{visibleStart: start, visibleEnd: len(visible), sourceStart: segment.Start, sourceEnd: segment.Stop})
			return true
		case *ast.Emphasis:
			if typed.Level != 1 && typed.Level != 2 {
				return false
			}
		case *ast.Link:
		case *extast.Strikethrough:
		default:
			return false
		}
		for child := current.FirstChild(); child != nil; child = child.NextSibling() {
			if !collect(child) {
				return false
			}
		}
		return true
	}
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		first := len(parts)
		if !collect(child) {
			return block
		}
		if len(parts) == first+1 {
			markInlineGroup(source, block, child, &parts[first])
		}
	}
	if len(parts) == 0 || len(visible) == 0 {
		return block
	}
	block.Visible, block.Editable, block.styled, block.parts = string(visible), true, true, parts
	return block
}

func markInlineGroup(source []byte, block Block, node ast.Node, part *inlinePart) {
	part.rawStart, part.rawEnd = part.sourceStart, part.sourceEnd
	switch typed := node.(type) {
	case *ast.Text:
		part.crossSafe = true
		return
	case *ast.Emphasis:
		if _, ok := typed.FirstChild().(*ast.Text); !ok || typed.FirstChild().NextSibling() != nil {
			return
		}
		marker := typed.Level
		if marker != 1 && marker != 2 {
			return
		}
		part.rawStart, part.rawEnd = part.sourceStart-marker, part.sourceEnd+marker
		if part.rawStart < block.Start || part.rawEnd > block.End {
			return
		}
		open := source[part.rawStart:part.sourceStart]
		close := source[part.sourceEnd:part.rawEnd]
		part.crossSafe = bytes.Equal(open, close) && (bytes.Equal(open, []byte("*")) || bytes.Equal(open, []byte("**")) || bytes.Equal(open, []byte("_")) || bytes.Equal(open, []byte("__")))
	case *extast.Strikethrough:
		if _, ok := typed.FirstChild().(*ast.Text); !ok || typed.FirstChild().NextSibling() != nil {
			return
		}
		part.rawStart, part.rawEnd = part.sourceStart-2, part.sourceEnd+2
		part.crossSafe = part.rawStart >= block.Start && part.rawEnd <= block.End && bytes.Equal(source[part.rawStart:part.sourceStart], []byte("~~")) && bytes.Equal(source[part.sourceEnd:part.rawEnd], []byte("~~"))
	}
}

func readOnly(source []byte, node ast.Node, kind Kind, fingerprint [32]byte) Block {
	start, end := len(source), 0
	var visit func(ast.Node)
	visit = func(current ast.Node) {
		if current.Type() == ast.TypeBlock {
			for index := 0; index < current.Lines().Len(); index++ {
				segment := current.Lines().At(index)
				start = min(start, segment.Start)
				end = max(end, segment.Stop)
			}
		}
		for child := current.FirstChild(); child != nil; child = child.NextSibling() {
			visit(child)
		}
	}
	visit(node)
	if start >= end {
		start = max(0, min(node.Pos(), len(source)-1))
		end = min(len(source), start+1)
	}
	return Block{Kind: kind, Start: start, End: end, source: fingerprint}
}

func Replace(source []byte, block Block, visibleText string) ([]byte, error) {
	if !utf8.ValidString(visibleText) || bytes.ContainsAny([]byte(visibleText), "\r\n") {
		return nil, ErrReadOnlyBlock
	}
	next, err := PatchRaw(source, block, visibleText)
	if err != nil {
		if errors.Is(err, ErrReadOnlyBlock) && block.styled && len(block.parts) > 1 {
			start, end, newEnd := visibleDiff(block.Visible, visibleText)
			if start < end {
				return ReplaceSelection(source, block, start, end, visibleText[start:newEnd])
			}
		}
		return nil, err
	}
	end := block.Start + len(visibleText)
	if block.styled {
		end = block.End + len(visibleText) - len(block.Visible)
	}
	for _, candidate := range Parse(next) {
		if candidate.Kind == block.Kind && candidate.Start == block.Start && candidate.End == end && candidate.Visible == visibleText && candidate.Editable && candidate.styled == block.styled && candidate.Task == block.Task && candidate.Checked == block.Checked {
			return next, nil
		}
	}
	return nil, ErrReadOnlyBlock
}

func ReplaceCode(source []byte, block Block, visible string) ([]byte, error) {
	if sha256.Sum256(source) != block.source {
		return nil, ErrStaleBlock
	}
	code := block.Code
	if block.Kind != FencedCode || !code.Editable || code.Start < 1 || code.End < code.Start || code.End > len(source) || !utf8.ValidString(visible) || strings.ContainsAny(visible, "\r\x00") {
		return nil, ErrReadOnlyBlock
	}
	if visible == code.Visible {
		return append([]byte(nil), source...), nil
	}
	newline := "\n"
	if code.Start >= 2 && source[code.Start-2] == '\r' {
		newline = "\r\n"
	}
	body := strings.ReplaceAll(visible, "\n", newline)
	if visible != "" {
		body += newline
	}
	next := make([]byte, 0, len(source)-code.End+code.Start+len(body))
	next = append(next, source[:code.Start]...)
	next = append(next, body...)
	next = append(next, source[code.End:]...)
	before := Parse(source)
	after := Parse(next)
	if len(before) != len(after) {
		return nil, ErrReadOnlyBlock
	}
	for index, candidate := range before {
		if candidate.Kind == FencedCode && candidate.Code.Start == code.Start {
			updated := after[index]
			if updated.Kind == FencedCode && updated.Code.Editable && updated.Code.Start == code.Start && updated.Code.End == code.Start+len(body) && updated.Code.Info == code.Info && updated.Code.Visible == visible {
				return next, nil
			}
			break
		}
	}
	return nil, ErrReadOnlyBlock
}

func ReplaceCell(source []byte, table Block, row, column int, visible string) ([]byte, error) {
	if sha256.Sum256(source) != table.source {
		return nil, ErrStaleBlock
	}
	if table.Kind != Table || row < 0 || column < 0 || !utf8.ValidString(visible) || strings.ContainsAny(visible, "|\\\r\n") {
		return nil, ErrReadOnlyBlock
	}
	index := -1
	for candidate := range table.Cells {
		if table.Cells[candidate].Row == row && table.Cells[candidate].Column == column {
			index = candidate
			break
		}
	}
	if index < 0 {
		return nil, ErrReadOnlyBlock
	}
	cell := table.Cells[index]
	if !cell.Editable || cell.Start < 0 || cell.End < cell.Start || cell.End > len(source) || string(source[cell.Start:cell.End]) != cell.Visible {
		return nil, ErrReadOnlyBlock
	}
	next := make([]byte, 0, len(source)-cell.End+cell.Start+len(visible))
	next = append(next, source[:cell.Start]...)
	next = append(next, visible...)
	next = append(next, source[cell.End:]...)
	rightPadding := 0
	for cell.End+rightPadding < len(source) && (source[cell.End+rightPadding] == ' ' || source[cell.End+rightPadding] == '\t') {
		rightPadding++
	}
	for _, candidate := range Parse(next) {
		if candidate.Kind != Table || len(candidate.Cells) != len(table.Cells) {
			continue
		}
		valid := true
		for position, updated := range candidate.Cells {
			original := table.Cells[position]
			if updated.Row != original.Row || updated.Column != original.Column {
				valid = false
				break
			}
			if position == index {
				validRange := updated.Start == cell.Start && updated.End == cell.Start+len(visible)
				if visible == "" {
					validRange = updated.Start == updated.End && updated.Start >= cell.Start && updated.Start <= cell.Start+rightPadding
				}
				if !updated.Editable || !validRange || updated.Visible != visible {
					valid = false
					break
				}
			} else if updated.Visible != original.Visible {
				valid = false
				break
			}
		}
		if valid {
			return next, nil
		}
	}
	return nil, ErrReadOnlyBlock
}

func ToggleTask(source []byte, block Block) ([]byte, error) {
	if sha256.Sum256(source) != block.source || block.TaskStart < 0 || block.TaskStart+3 > len(source) {
		return nil, ErrStaleBlock
	}
	if !block.Task || source[block.TaskStart] != '[' || source[block.TaskStart+2] != ']' {
		return nil, ErrReadOnlyBlock
	}
	next := append([]byte(nil), source...)
	if block.Checked {
		next[block.TaskStart+1] = ' '
	} else {
		next[block.TaskStart+1] = 'x'
	}
	for _, candidate := range Parse(next) {
		if candidate.Kind == ListItem && candidate.Task && candidate.Checked != block.Checked && candidate.Visible == block.Visible && candidate.Start == block.Start && candidate.TaskStart == block.TaskStart {
			return next, nil
		}
	}
	return nil, ErrReadOnlyBlock
}

func PatchRaw(source []byte, block Block, visibleText string) ([]byte, error) {
	if sha256.Sum256(source) != block.source || block.Start < 0 || block.End > len(source) || block.Start >= block.End {
		return nil, ErrStaleBlock
	}
	if !block.Editable || !utf8.ValidString(visibleText) {
		return nil, ErrReadOnlyBlock
	}
	start, end := block.Start, block.End
	if block.styled {
		if len(block.parts) > 0 {
			oldStart, oldEnd, newEnd := visibleDiff(block.Visible, visibleText)
			matched := false
			for _, part := range block.parts {
				if oldStart >= part.visibleStart && oldEnd <= part.visibleEnd {
					start = part.sourceStart + oldStart - part.visibleStart
					end = part.sourceStart + oldEnd - part.visibleStart
					visibleText = visibleText[oldStart:newEnd]
					matched = true
					break
				}
			}
			if !matched {
				return nil, ErrReadOnlyBlock
			}
		} else {
			start, end = block.textFrom, block.textTo
		}
		if start < block.Start || end > block.End || start > end {
			return nil, ErrStaleBlock
		}
	}
	next := make([]byte, 0, len(source)-end+start+len(visibleText))
	next = append(next, source[:start]...)
	next = append(next, visibleText...)
	next = append(next, source[end:]...)
	return next, nil
}

func EditableRange(block Block, start, end int) bool {
	if !block.Editable || start < 0 || end < start || end > len(block.Visible) || !utf8.ValidString(block.Visible[:start]) || !utf8.ValidString(block.Visible[:end]) {
		return false
	}
	if !block.styled || len(block.parts) == 0 {
		return true
	}
	for _, part := range block.parts {
		if start >= part.visibleStart && end <= part.visibleEnd {
			return true
		}
	}
	return false
}

func ToggleInline(source []byte, block Block, start, end int, marker string) ([]byte, error) {
	if sha256.Sum256(source) != block.source || block.Start < 0 || block.End > len(source) {
		return nil, ErrStaleBlock
	}
	if !block.Editable || block.Kind != Paragraph || marker != "**" && marker != "*" && marker != "~~" || start < 0 || start >= end || end > len(block.Visible) || !utf8.ValidString(block.Visible[:start]) || !utf8.ValidString(block.Visible[:end]) {
		return nil, ErrReadOnlyBlock
	}
	parts := block.parts
	if len(parts) == 0 {
		from, to := block.Start, block.End
		if block.styled {
			from, to = block.textFrom, block.textTo
		}
		parts = []inlinePart{{visibleStart: 0, visibleEnd: len(block.Visible), sourceStart: from, sourceEnd: to}}
	}
	for _, part := range parts {
		if start < part.visibleStart || end > part.visibleEnd {
			continue
		}
		from := part.sourceStart + start - part.visibleStart
		to := part.sourceStart + end - part.visibleStart
		selected := block.Visible[start:end]
		replacement := marker + selected + marker
		if start == part.visibleStart && end == part.visibleEnd && from >= len(marker) && to+len(marker) <= len(source) && string(source[from-len(marker):from]) == marker && string(source[to:to+len(marker)]) == marker {
			if !(marker == "*" && from >= 2 && to+2 <= len(source) && string(source[from-2:from]) == "**" && string(source[to:to+2]) == "**") {
				from -= len(marker)
				to += len(marker)
				replacement = selected
			}
		} else if from >= 2 && to+2 <= len(source) && string(source[from-2:from]) == "**" && string(source[to:to+2]) == "**" && marker == "**" {
			return nil, ErrReadOnlyBlock
		}
		next := make([]byte, 0, len(source)+len(replacement)-(to-from))
		next = append(next, source[:from]...)
		next = append(next, replacement...)
		next = append(next, source[to:]...)
		for _, candidate := range Parse(next) {
			if candidate.Kind == block.Kind && candidate.Start == block.Start && candidate.Visible == block.Visible && candidate.Editable {
				return next, nil
			}
		}
		return nil, ErrReadOnlyBlock
	}
	return nil, ErrReadOnlyBlock
}

func LinkSelection(source []byte, block Block, start, end int, destination string) ([]byte, error) {
	if sha256.Sum256(source) != block.source || block.Start < 0 || block.End > len(source) {
		return nil, ErrStaleBlock
	}
	if !block.Editable || block.Kind != Paragraph || !SafeLinkURL(destination) || start < 0 || start >= end || end > len(block.Visible) || !utf8.ValidString(block.Visible[:start]) || !utf8.ValidString(block.Visible[:end]) {
		return nil, ErrReadOnlyBlock
	}
	parts := block.parts
	if len(parts) == 0 {
		from, to := block.Start, block.End
		if block.styled {
			from, to = block.textFrom, block.textTo
		}
		parts = []inlinePart{{visibleStart: 0, visibleEnd: len(block.Visible), sourceStart: from, sourceEnd: to}}
	}
	for _, part := range parts {
		if start < part.visibleStart || end > part.visibleEnd {
			continue
		}
		from := part.sourceStart + start - part.visibleStart
		to := part.sourceStart + end - part.visibleStart
		replacement := "[" + block.Visible[start:end] + "](" + destination + ")"
		next := make([]byte, 0, len(source)+len(replacement)-(to-from))
		next = append(next, source[:from]...)
		next = append(next, replacement...)
		next = append(next, source[to:]...)
		for _, candidate := range Parse(next) {
			if candidate.Kind == block.Kind && candidate.Start == block.Start && candidate.Visible == block.Visible && candidate.Editable {
				return next, nil
			}
		}
		return nil, ErrReadOnlyBlock
	}
	return nil, ErrReadOnlyBlock
}

func SafeLinkURL(destination string) bool {
	if destination == "" || strings.TrimSpace(destination) != destination || strings.ContainsAny(destination, "()<>[] \\ \t\r\n") {
		return false
	}
	parsed, err := url.Parse(destination)
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return parsed.Host != ""
	case "mailto":
		return parsed.Opaque != ""
	default:
		return false
	}
}

func visibleDiff(before, after string) (start, endBefore, endAfter int) {
	for start < len(before) && start < len(after) {
		original, originalSize := utf8.DecodeRuneInString(before[start:])
		replacement, replacementSize := utf8.DecodeRuneInString(after[start:])
		if original != replacement || originalSize != replacementSize {
			break
		}
		start += originalSize
	}
	endBefore, endAfter = len(before), len(after)
	for endBefore > start && endAfter > start {
		original, originalSize := utf8.DecodeLastRuneInString(before[:endBefore])
		replacement, replacementSize := utf8.DecodeLastRuneInString(after[:endAfter])
		if original != replacement || originalSize != replacementSize {
			break
		}
		endBefore -= originalSize
		endAfter -= replacementSize
	}
	return start, endBefore, endAfter
}
