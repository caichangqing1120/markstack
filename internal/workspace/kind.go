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
	case ".txt", ".log", ".csv", ".tsv":
		return PlainKind
	}
	if CodeLanguage(path) != "" {
		return CodeKind
	}
	return UnsupportedKind
}
