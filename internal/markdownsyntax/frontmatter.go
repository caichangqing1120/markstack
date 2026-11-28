package markdownsyntax

import (
	"strings"
	"unicode"
)

type FrontMatter struct {
	Start, BodyStart, BodyEnd, End int
}

func Metadata(source string) (FrontMatter, bool) {
	start := 0
	if strings.HasPrefix(source, "\ufeff") {
		start = len("\ufeff")
	}
	first, _, ok := strings.Cut(source[start:], "\n")
	if !ok || strings.TrimRight(first, " \t\r") != "---" {
		return FrontMatter{}, false
	}
	body := start + len(first) + 1
	for cursor := body; cursor < len(source); {
		line, _, more := strings.Cut(source[cursor:], "\n")
		if strings.TrimRight(line, " \t\r") == "---" {
			return FrontMatter{Start: start, BodyStart: body, BodyEnd: cursor, End: cursor + len(strings.TrimSuffix(line, "\r"))}, yamlMapping(source[body:cursor])
		}
		if !more {
			break
		}
		cursor += len(line) + 1
	}
	return FrontMatter{}, false
}

func StripMetadata(source []byte) []byte {
	if metadata, ok := Metadata(string(source)); ok {
		source = source[metadata.End:]
	}
	return source
}

func yamlMapping(body string) bool {
	type flowEntry struct {
		open  rune
		value bool
	}
	var flow []flowEntry
	var quote rune
	hasKey, blockScalar, rootFlow := false, false, false
	for raw := range strings.SplitSeq(body, "\n") {
		line := strings.TrimSuffix(raw, "\r")
		if strings.ContainsRune(line, '\r') {
			return false
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent < len(line) && line[indent] == '\t' {
			return false
		}
		if blockScalar && indent > 0 {
			continue
		}
		if len(flow) == 0 && quote == 0 {
			if rootFlow {
				return false
			}
			if indent == 0 || strings.HasPrefix(trimmed, "- ") {
				blockScalar = false
			}
			sequence := strings.HasPrefix(trimmed, "- ") || trimmed == "-"
			if sequence && indent > 0 {
				trimmed = strings.TrimSpace(trimmed[1:])
			}
			separator := yamlKeyEnd(trimmed)
			if indent == 0 && !hasKey && strings.HasPrefix(trimmed, "{") {
				rootFlow, hasKey, separator = true, true, -1
			} else if indent == 0 {
				if separator < 1 || sequence || strings.ContainsRune("[{|>!&*%@`", rune(trimmed[0])) {
					return false
				}
				hasKey = true
			} else if !hasKey {
				return false
			}
			if separator > 0 {
				trimmed = strings.TrimSpace(trimmed[separator+1:])
			}
			if trimmed == "" {
				continue
			}
			if trimmed[0] == '|' || trimmed[0] == '>' {
				options, _, _ := strings.Cut(trimmed[1:], "#")
				options = strings.TrimSpace(options)
				if len(options) > 2 || strings.Trim(options, "+-123456789") != "" {
					return false
				}
				blockScalar = true
				continue
			}
			if !strings.ContainsRune("[{\"'", rune(trimmed[0])) {
				if yamlKeyEnd(trimmed) >= 0 || strings.ContainsRune("]},%@`", rune(trimmed[0])) {
					return false
				}
				continue
			}
		}
		wordStart, complete := true, false
		for index := 0; index < len(trimmed); index++ {
			character := rune(trimmed[index])
			if quote != 0 {
				if character == '\\' && quote == '"' {
					index++
				} else if character == quote {
					if quote == '\'' && index+1 < len(trimmed) && trimmed[index+1] == '\'' {
						index++
					} else {
						quote, complete = 0, len(flow) == 0
					}
				}
				continue
			}
			if character == '#' && wordStart {
				break
			}
			if complete && !unicode.IsSpace(character) {
				return false
			}
			switch character {
			case '\'', '"':
				if wordStart {
					quote = character
				}
				if len(flow) > 0 {
					flow[len(flow)-1].value = true
				}
			case '[', '{':
				if len(flow) > 0 {
					flow[len(flow)-1].value = true
				}
				flow = append(flow, flowEntry{open: character})
			case ']', '}':
				if len(flow) == 0 || flow[len(flow)-1].open != character-2 {
					return false
				}
				flow = flow[:len(flow)-1]
				complete = len(flow) == 0
			case ',':
				if len(flow) == 0 || !flow[len(flow)-1].value {
					return false
				}
				flow[len(flow)-1].value = false
			default:
				if len(flow) > 0 && !unicode.IsSpace(character) {
					flow[len(flow)-1].value = true
				}
			}
			wordStart = unicode.IsSpace(character) || strings.ContainsRune("[{,:", character)
		}
	}
	return hasKey && len(flow) == 0 && quote == 0
}

func yamlKeyEnd(line string) int {
	var quote byte
	for index := 0; index < len(line); index++ {
		character := line[index]
		if index == 0 && (character == '\'' || character == '"') {
			quote = character
		} else if quote != 0 {
			if character == '\\' && quote == '"' {
				index++
			} else if character == quote {
				quote = 0
			}
		} else if character == '#' && (index == 0 || line[index-1] == ' ') {
			return -1
		} else if character == ':' && (index+1 == len(line) || line[index+1] == ' ' || line[index+1] == '\t') {
			return index
		}
	}
	return -1
}
