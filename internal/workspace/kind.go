package workspace

import (
	"path/filepath"
	"strings"
)

type DocumentKind uint8

const (
	UnsupportedKind DocumentKind = iota
	MarkdownKind
	RichHTMLKind
	CodeKind
	PlainKind
)

func KindOf(path string) DocumentKind {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mdx":
		return MarkdownKind
	case ".mh":
		return RichHTMLKind
	case ".go", ".js", ".mjs", ".cjs", ".jsx", ".ts", ".mts", ".cts", ".tsx", ".json", ".yml", ".yaml", ".html", ".htm", ".svg", ".css", ".py", ".sql", ".sh", ".bash", ".zsh":
		return CodeKind
	case ".txt", ".log", ".csv", ".tsv":
		return PlainKind
	default:
		return UnsupportedKind
	}
}
