package richedit

import (
	"bytes"
	"crypto/sha256"
	"strconv"
	"strings"

	htmltoken "golang.org/x/net/html"
)

func CanSplitListItem(block Block) bool { return canSplitBlock(block, ListItem) }

func SplitListItem(source []byte, block Block, visibleCaret int, newline string) ([]byte, error) {
	return splitEditableBlock(source, block, visibleCaret, newline, ListItem, "li")
}

func MergeListItems(source []byte, document Document, firstIndex, secondIndex int) ([]byte, error) {
	return mergeEditableBlocks(source, document, firstIndex, secondIndex, ListItem, "li")
}

func ExitEmptyListItem(source []byte, document Document, index int, newline string) ([]byte, int, error) {
	if index < 0 || index >= len(document.Blocks) || newline != "\n" && newline != "\r\n" {
		return nil, 0, ErrReadOnlyLeaf
	}
	block := document.Blocks[index]
	if len(block.Leaves) == 0 {
		return nil, 0, ErrReadOnlyLeaf
	}
	if block.Leaves[0].sourceDigest != sha256.Sum256(source) {
		return nil, 0, ErrStaleLeaf
	}
	if block.Kind != ListItem || block.ReadOnly || block.Visible != "" || len(block.Leaves) != 1 ||
		block.Start < 0 || block.End > len(source) || block.OpenEnd < block.Start || block.CloseStart < block.OpenEnd ||
		!strings.EqualFold(string(source[block.Start:block.OpenEnd]), "<li>") {
		return nil, 0, ErrReadOnlyLeaf
	}
	leaf := block.Leaves[0]
	raw := source[block.OpenEnd:block.CloseStart]
	if !leaf.Editable || leaf.ImageSource != "" || leaf.Start != block.OpenEnd || leaf.End != block.CloseStart ||
		len(raw) > 0 && !bytes.EqualFold(raw, []byte("<br>")) && !bytes.EqualFold(raw, []byte("<br/>")) {
		return nil, 0, ErrReadOnlyLeaf
	}
	container, err := listContainerFor(source, block.Start)
	if err != nil || container == nil || container.end < block.End || container.ordinal < 1 || container.ordinal > container.items {
		return nil, 0, ErrReadOnlyLeaf
	}
	previousEnd := block.End
	for following := 1; following <= container.items-container.ordinal; following++ {
		if index+following >= len(document.Blocks) {
			return nil, 0, ErrReadOnlyLeaf
		}
		next := document.Blocks[index+following]
		if next.Kind != ListItem || next.ReadOnly || next.Start < previousEnd || next.End > container.closeStart ||
			strings.TrimSpace(string(source[previousEnd:next.Start])) != "" {
			return nil, 0, ErrReadOnlyLeaf
		}
		previousEnd = next.End
	}
	if strings.TrimSpace(string(source[previousEnd:container.closeStart])) != "" {
		return nil, 0, ErrReadOnlyLeaf
	}
	if container.ordinal > 1 {
		if index == 0 || document.Blocks[index-1].Kind != ListItem || document.Blocks[index-1].End > block.Start ||
			strings.TrimSpace(string(source[document.Blocks[index-1].End:block.Start])) != "" {
			return nil, 0, ErrReadOnlyLeaf
		}
	}
	var result []byte
	if container.items == 1 {
		if strings.TrimSpace(string(source[container.openEnd:block.Start])) != "" {
			return nil, 0, ErrReadOnlyLeaf
		}
		result = append(result, source[:container.start]...)
		result = append(result, "<p><br></p>"...)
		result = append(result, source[container.end:]...)
	} else if container.ordinal == container.items {
		result = append(result, source[:block.Start]...)
		result = append(result, source[block.End:container.end]...)
		result = append(result, newline...)
		result = append(result, "<p><br></p>"...)
		result = append(result, source[container.end:]...)
	} else {
		rightOpen, err := continuationListOpen(source, container, document.Blocks[index+1].Number)
		if err != nil {
			return nil, 0, err
		}
		if container.ordinal == 1 {
			if strings.TrimSpace(string(source[container.openEnd:block.Start])) != "" {
				return nil, 0, ErrReadOnlyLeaf
			}
			result = append(result, source[:container.start]...)
		} else {
			result = append(result, source[:block.Start]...)
			result = append(result, source[container.closeStart:container.end]...)
			result = append(result, newline...)
		}
		result = append(result, "<p><br></p>"...)
		result = append(result, newline...)
		result = append(result, rightOpen...)
		result = append(result, source[block.End:]...)
	}
	parsed := Parse(result)
	if len(parsed.Blocks) != len(document.Blocks) || index >= len(parsed.Blocks) || parsed.Blocks[index].Kind != Paragraph ||
		parsed.Blocks[index].ReadOnly || parsed.Blocks[index].Visible != "" || len(parsed.Blocks[index].Leaves) != 1 || !parsed.Blocks[index].Leaves[0].Editable {
		return nil, 0, ErrReadOnlyLeaf
	}
	for blockIndex := range document.Blocks {
		if blockIndex == index {
			continue
		}
		before, after := document.Blocks[blockIndex], parsed.Blocks[blockIndex]
		if before.Kind != after.Kind || before.Visible != after.Visible || before.Number != after.Number {
			return nil, 0, ErrReadOnlyLeaf
		}
	}
	return result, index, nil
}

func continuationListOpen(source []byte, container *listContainer, number int) ([]byte, error) {
	opening := source[container.start:container.openEnd]
	if container.tag == "ul" {
		return append([]byte(nil), opening...), nil
	}
	if container.tag != "ol" || number <= 0 || number > 1000000 {
		return nil, ErrReadOnlyLeaf
	}
	tokenizer := htmltoken.NewTokenizer(bytes.NewReader(opening))
	if tokenizer.Next() != htmltoken.StartTagToken {
		return nil, ErrReadOnlyLeaf
	}
	for _, attribute := range tokenizer.Token().Attr {
		if !strings.EqualFold(attribute.Key, "start") {
			return nil, ErrReadOnlyLeaf
		}
	}
	return []byte(`<ol start="` + strconv.Itoa(number) + `">`), nil
}

type listContainer struct {
	tag                             string
	start, openEnd, closeStart, end int
	items, ordinal                  int
}

type listTokenFrame struct {
	tag  string
	list *listContainer
}

func listContainerFor(source []byte, itemStart int) (*listContainer, error) {
	tokenizer := htmltoken.NewTokenizer(bytes.NewReader(source))
	var stack []listTokenFrame
	var target *listContainer
	offset := 0
	for {
		kind := tokenizer.Next()
		if kind == htmltoken.ErrorToken {
			return nil, ErrReadOnlyLeaf
		}
		raw := tokenizer.Raw()
		start, end := offset, offset+len(raw)
		switch kind {
		case htmltoken.StartTagToken:
			token := tokenizer.Token()
			tag := strings.ToLower(token.Data)
			frame := listTokenFrame{tag: tag}
			if tag == "ul" || tag == "ol" {
				frame.list = &listContainer{tag: tag, start: start, openEnd: end}
			} else if tag == "li" {
				if len(stack) == 0 || stack[len(stack)-1].list == nil {
					return nil, ErrReadOnlyLeaf
				}
				parent := stack[len(stack)-1].list
				parent.items++
				if start == itemStart {
					for _, ancestor := range stack {
						if ancestor.tag == "li" {
							return nil, ErrReadOnlyLeaf
						}
					}
					target = parent
					target.ordinal = parent.items
				}
			}
			if tag != "br" && tag != "img" && tag != "hr" && tag != "input" && tag != "meta" && tag != "link" {
				stack = append(stack, frame)
			}
		case htmltoken.SelfClosingTagToken:
		case htmltoken.EndTagToken:
			token := tokenizer.Token()
			tag := strings.ToLower(token.Data)
			if len(stack) == 0 || stack[len(stack)-1].tag != tag {
				return nil, ErrReadOnlyLeaf
			}
			frame := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if target != nil && frame.list == target {
				target.closeStart, target.end = start, end
				return target, nil
			}
		}
		offset = end
	}
}
