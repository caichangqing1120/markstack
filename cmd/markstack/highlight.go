package main

import (
	"strings"

	"github.com/vivaui/viva/components"
)

func markdownSourceRuns(line string, palette colors, runs []components.CodeEditorRun) []components.CodeEditorRun {
	runs = runs[:0]
	leading := len(line) - len(strings.TrimLeft(line, " "))
	trimmed := line[leading:]
	if leading <= 3 {
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			return append(runs, components.CodeEditorRun{Start: leading, End: len(line), Color: palette.muted})
		}
		marker := 0
		for marker < len(trimmed) && marker < 6 && trimmed[marker] == '#' {
			marker++
		}
		if marker > 0 && marker < len(trimmed) && trimmed[marker] == ' ' {
			return append(runs, components.CodeEditorRun{Start: leading, End: len(line), Color: palette.accent})
		}
	}
	position := leading
	if strings.HasPrefix(trimmed, "> ") || strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "+ ") {
		runs = append(runs, components.CodeEditorRun{Start: leading, End: leading + 1, Color: palette.muted})
		position = leading + 1
	}
	for position < len(line) {
		switch {
		case line[position] == '`':
			if end := strings.IndexByte(line[position+1:], '`'); end >= 0 {
				end += position + 2
				runs = append(runs, components.CodeEditorRun{Start: position, End: end, Color: palette.accent})
				position = end
				continue
			}
		case line[position] == '[':
			if labelEnd := strings.Index(line[position:], "]("); labelEnd >= 0 {
				urlStart := position + labelEnd + 2
				if close := strings.IndexByte(line[urlStart:], ')'); close >= 0 {
					url := line[urlStart : urlStart+close]
					if strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "mailto:") {
						end := urlStart + close + 1
						runs = append(runs, components.CodeEditorRun{Start: position, End: end, Color: palette.accent})
						position = end
						continue
					}
				}
			}
		case strings.HasPrefix(line[position:], "**") || strings.HasPrefix(line[position:], "~~"):
			runs = append(runs, components.CodeEditorRun{Start: position, End: position + 2, Color: palette.muted})
			position += 2
			continue
		}
		position++
	}
	return runs
}
