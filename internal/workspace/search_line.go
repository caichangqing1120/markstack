package workspace

import (
	"context"
	"regexp"
	"regexp/syntax"
	"unicode/utf8"
)

const (
	searchRegexWindow  = 256 << 10
	searchRegexOverlap = 4 << 10
)

func boundedSearchRegexWidth(pattern string) int {
	expression, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return -1
	}
	return boundedSearchExpressionWidth(expression)
}

func boundedSearchExpressionWidth(expression *syntax.Regexp) int {
	switch expression.Op {
	case syntax.OpNoMatch, syntax.OpEmptyMatch:
		return 0
	case syntax.OpLiteral:
		if len(expression.Rune) <= searchRegexOverlap/utf8.UTFMax {
			return len(expression.Rune) * utf8.UTFMax
		}
	case syntax.OpCharClass, syntax.OpAnyCharNotNL, syntax.OpAnyChar:
		return utf8.UTFMax
	case syntax.OpCapture, syntax.OpQuest:
		return boundedSearchExpressionWidth(expression.Sub[0])
	case syntax.OpRepeat:
		width := boundedSearchExpressionWidth(expression.Sub[0])
		if expression.Max >= 0 && width >= 0 && (width == 0 || expression.Max <= searchRegexOverlap/width) {
			return expression.Max * width
		}
	case syntax.OpConcat, syntax.OpAlternate:
		width := 0
		for _, child := range expression.Sub {
			part := boundedSearchExpressionWidth(child)
			if part < 0 {
				return -1
			}
			if expression.Op == syntax.OpAlternate {
				width = max(width, part)
			} else {
				width += part
			}
			if width > searchRegexOverlap {
				return -1
			}
		}
		return width
	}
	return -1
}

func findBoundedSearchLine(ctx context.Context, matcher *regexp.Regexp, line []byte, overlap int) ([]int, error) {
	nextRune := func(index int) int {
		index = min(index, len(line))
		for index < len(line) && !utf8.RuneStart(line[index]) {
			index++
		}
		return index
	}
	for start := 0; ; {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		next := nextRune(start + searchRegexWindow)
		end := nextRune(next + overlap)
		found := matcher.FindIndex(line[start:end])
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if found != nil && (end == len(line) || start+found[0] < next) {
			found[0] += start
			found[1] += start
			return found, nil
		}
		if end == len(line) {
			return nil, nil
		}
		start = next
	}
}
