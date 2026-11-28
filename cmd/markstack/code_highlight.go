package main

import (
	"strings"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"

	"markstack/internal/workspace"
)

const maxCodeHighlightBytes = 1 << 20

type codeHighlightCache struct {
	documentID int
	source     string
	language   string
	dark       bool
	palette    colors
	lineText   []string
	lines      [][]components.CodeEditorRun
}

func codeLanguage(path string) string {
	return workspace.CodeLanguage(path)
}

var regexLexer = chroma.MustNewLexer(&chroma.Config{Name: "Regular expression"}, func() chroma.Rules {
	return chroma.Rules{"root": {
		{Pattern: `\\.`, Type: chroma.LiteralStringEscape},
		{Pattern: `\[(?:\\.|[^\]\\])*\]`, Type: chroma.LiteralStringRegex},
		{Pattern: `\{\d+(?:,\d*)?\}`, Type: chroma.LiteralNumber},
		{Pattern: `[-^$.*+?()|]`, Type: chroma.Keyword},
		{Pattern: `.`, Type: chroma.Text},
	}}
})

func codeLexer(language string) chroma.Lexer {
	switch language {
	case "sqlite", "plsql":
		return lexers.Get("sql")
	case "less":
		return lexers.Get("scss")
	case "regex":
		return regexLexer
	}
	return lexers.Get(language)
}

func (cache *codeHighlightCache) Prepare(documentID int, source, language string, palette colors, dark bool) {
	if cache.documentID == documentID && cache.source == source && cache.language == language && cache.dark == dark && cache.palette == palette {
		return
	}
	cache.documentID, cache.source, cache.language, cache.dark, cache.palette = documentID, source, language, dark, palette
	cache.lineText, cache.lines = nil, nil
	if language == "" || len(source) > maxCodeHighlightBytes || !utf8.ValidString(source) {
		return
	}
	lexer := codeLexer(workspace.NormalizeCodeLanguage(language))
	if lexer == nil {
		return
	}
	iterator, err := lexer.Tokenise(&chroma.TokeniseOptions{State: "root", EnsureLF: false}, source)
	if err != nil {
		return
	}
	cache.lineText = strings.Split(source, "\n")
	cache.lines = make([][]components.CodeEditorRun, len(cache.lineText))
	defer func() {
		if recover() != nil {
			cache.lineText, cache.lines = nil, nil
		}
	}()
	offset, lineIndex, lineStart := 0, 0, 0
	for token := iterator(); token != chroma.EOF; token = iterator() {
		if !strings.HasPrefix(source[offset:], token.Value) {
			cache.lineText, cache.lines = nil, nil
			return
		}
		color := codeTokenColor(token.Type, palette, dark)
		for part := 0; part < len(token.Value); {
			end := len(token.Value)
			if newline := strings.IndexByte(token.Value[part:], '\n'); newline >= 0 {
				end = part + newline
			}
			if end > part && lineIndex < len(cache.lines) {
				startByte, endByte := offset+part, offset+end
				if startByte < len(source) && !utf8.RuneStart(source[startByte]) || endByte < len(source) && !utf8.RuneStart(source[endByte]) {
					cache.lineText, cache.lines = nil, nil
					return
				}
				cache.lines[lineIndex] = appendCodeRun(cache.lines[lineIndex], startByte-lineStart, endByte-lineStart, color)
			}
			if end == len(token.Value) {
				break
			}
			lineIndex++
			lineStart = offset + end + 1
			part = end + 1
		}
		offset += len(token.Value)
	}
	if offset != len(source) {
		cache.lineText, cache.lines = nil, nil
	}
}

func appendCodeRun(runs []components.CodeEditorRun, start, end int, color ui.Color) []components.CodeEditorRun {
	if len(runs) > 0 && runs[len(runs)-1].End == start && runs[len(runs)-1].Color == color {
		runs[len(runs)-1].End = end
		return runs
	}
	return append(runs, components.CodeEditorRun{Start: start, End: end, Color: color})
}

func codeTokenColor(kind chroma.TokenType, palette colors, dark bool) ui.Color {
	switch markdownCodeClass(kind) {
	case components.CodeComment:
		return palette.muted
	case components.CodeString:
		if dark {
			return ui.RGB(160, 213, 169)
		}
		return ui.RGB(31, 130, 87)
	case components.CodeNumber:
		if dark {
			return ui.RGB(236, 178, 115)
		}
		return ui.RGB(168, 91, 39)
	case components.CodeKeyword:
		return palette.accent
	case components.CodeFunction:
		if dark {
			return ui.RGB(161, 194, 231)
		}
		return ui.RGB(49, 103, 161)
	default:
		return palette.ink
	}
}

func markdownCodeRuns(language, line string, dst []components.MarkdownCodeRun) []components.MarkdownCodeRun {
	dst = dst[:0]
	fields := strings.Fields(language)
	if len(fields) == 0 || len(line) > maxCodeHighlightBytes || !utf8.ValidString(line) {
		return dst
	}
	canonical := workspace.NormalizeCodeLanguage(fields[0])
	lexer := codeLexer(canonical)
	if lexer == nil {
		return dst
	}
	source := line + "\n"
	iterator, err := lexer.Tokenise(&chroma.TokeniseOptions{State: "root", EnsureLF: false}, source)
	if err != nil {
		return dst
	}
	offset := 0
	goCode := canonical == "go"
	inFunctionDeclaration, inPackageDeclaration := false, false
	for token := iterator(); token != chroma.EOF; token = iterator() {
		if !strings.HasPrefix(source[offset:], token.Value) {
			return dst[:0]
		}
		tokenEnd := offset + len(token.Value)
		end := min(tokenEnd, len(line))
		if offset < len(line) && !utf8.RuneStart(line[offset]) || end < len(line) && !utf8.RuneStart(line[end]) {
			return dst[:0]
		}
		if goCode && token.Type.InCategory(chroma.Keyword) {
			switch token.Value {
			case "func":
				inFunctionDeclaration = true
			case "package":
				inPackageDeclaration = true
			}
		}
		class := markdownCodeClass(token.Type)
		if goCode && class == components.CodeFunction && (inFunctionDeclaration || inPackageDeclaration) {
			class = 0
			inPackageDeclaration = false
		}
		if token.Value == "{" {
			inFunctionDeclaration = false
		}
		if class != 0 && end > offset {
			if len(dst) > 0 && dst[len(dst)-1].End == offset && dst[len(dst)-1].Class == class {
				dst[len(dst)-1].End = end
			} else {
				dst = append(dst, components.MarkdownCodeRun{Start: offset, End: end, Class: class})
			}
		}
		offset = tokenEnd
	}
	if offset < len(line) {
		return dst[:0]
	}
	return dst
}

func (appearance markdownAppearance) applyCodeLook(look *components.MarkdownLook, dark bool) {
	markdownCodeLook(look, dark, appearance.palette)
	if appearance.curated {
		look.Code[components.CodeKeyword] = appearance.palette.accent
		if !dark {
			look.Code[components.CodeString] = ui.RGB(158, 30, 29)
		}
	}
}

func markdownCodeClass(kind chroma.TokenType) components.MarkdownCodeClass {
	switch {
	case kind.InCategory(chroma.Comment), kind == chroma.GenericDeleted:
		return components.CodeComment
	case kind.InCategory(chroma.LiteralString), kind == chroma.GenericInserted:
		return components.CodeString
	case kind.InCategory(chroma.LiteralNumber):
		return components.CodeNumber
	case kind.InCategory(chroma.Keyword), kind == chroma.NameTag, kind == chroma.GenericHeading, kind == chroma.GenericSubheading:
		return components.CodeKeyword
	case kind.InCategory(chroma.NameFunction), kind == chroma.NameAttribute, kind == chroma.NameDecorator, kind == chroma.NameBuiltin:
		return components.CodeFunction
	default:
		return 0
	}
}

func markdownCodeLook(look *components.MarkdownLook, dark bool, palette colors) {
	look.Code[components.CodeComment] = palette.muted
	if dark {
		look.Code[components.CodeString] = ui.RGB(225, 160, 150)
		look.Code[components.CodeNumber] = ui.RGB(205, 176, 119)
		look.Code[components.CodeKeyword] = ui.RGB(204, 139, 205)
		look.Code[components.CodeFunction] = ui.RGB(161, 194, 231)
		return
	}
	look.Code[components.CodeString] = ui.RGB(196, 26, 22)
	look.Code[components.CodeNumber] = ui.RGB(111, 91, 155)
	look.Code[components.CodeKeyword] = ui.RGB(166, 38, 164)
	look.Code[components.CodeFunction] = ui.RGB(35, 87, 157)
}

func (cache *codeHighlightCache) Runs(lineIndex int, line string, dst []components.CodeEditorRun) []components.CodeEditorRun {
	dst = dst[:0]
	if lineIndex < 0 || lineIndex >= len(cache.lines) || line != cache.lineText[lineIndex] {
		return dst
	}
	return append(dst, cache.lines[lineIndex]...)
}
