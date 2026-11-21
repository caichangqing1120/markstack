package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"markstack/internal/richedit"
)

type SearchOptions struct {
	Query         string
	CaseSensitive bool
	Regex         bool
	MarkdownOnly  bool
	MaxResults    int
}

type SearchHit struct {
	Path        string
	Relative    string
	FileName    bool
	Line        int
	Column      int
	Preview     string
	ByteOffset  int
	MatchBytes  int
	Rich        bool
	RichBlock   int
	RichStart   int
	RichEnd     int
	Fingerprint [sha256.Size]byte
}

type SearchSummary struct {
	Emitted   int
	Truncated bool
}

var errStopSearch = errors.New("stop workspace search")

func Search(ctx context.Context, root string, options SearchOptions, emit func(SearchHit) bool) (SearchSummary, error) {
	var summary SearchSummary
	if err := ctx.Err(); err != nil {
		return summary, err
	}
	if strings.TrimSpace(options.Query) == "" || emit == nil {
		return summary, errors.New("search query and result handler are required")
	}
	pattern := options.Query
	if !options.Regex {
		pattern = regexp.QuoteMeta(options.Query)
	}
	if !options.CaseSensitive {
		pattern = "(?i)" + pattern
	}
	matcher, err := regexp.Compile(pattern)
	if err != nil {
		return summary, fmt.Errorf("invalid search expression: %w", err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return summary, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return summary, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return summary, err
	}
	if !info.IsDir() {
		return summary, fmt.Errorf("not a directory: %s", root)
	}
	rootHandle, err := os.OpenRoot(root)
	if err != nil {
		return summary, err
	}
	defer rootHandle.Close()
	limit := options.MaxResults
	if limit <= 0 {
		limit = 500
	}
	offer := func(hit SearchHit) error {
		if summary.Emitted >= limit {
			summary.Truncated = true
			return errStopSearch
		}
		if !emit(hit) {
			return errStopSearch
		}
		summary.Emitted++
		return nil
	}
	err = fs.WalkDir(rootHandle.FS(), ".", func(relative string, entry fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if walkErr != nil {
			if replaced, err := rootHandle.Lstat(relative); err == nil && replaced.Mode()&os.ModeSymlink != 0 {
				return nil
			}
			return walkErr
		}
		if relative == "." {
			return nil
		}
		path := filepath.Join(root, filepath.FromSlash(relative))
		name := entry.Name()
		if entry.IsDir() {
			if strings.HasPrefix(name, ".") || excludedSearchDir(name) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") || entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() || options.MarkdownOnly && !IsMarkdown(path) {
			return nil
		}
		info, err := rootHandle.Lstat(relative)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > MaxDocumentSize {
			return nil
		}
		raw, err := rootHandle.ReadFile(relative)
		if err != nil {
			return err
		}
		if !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
			return nil
		}
		content := bytes.TrimPrefix(raw, utf8BOM)
		relative = filepath.FromSlash(relative)
		fingerprint := sha256.Sum256(raw)
		rich := KindOf(path) == RichHTMLKind
		if found := matcher.FindStringIndex(name); found != nil {
			if err := offer(SearchHit{Path: path, Relative: relative, FileName: true, Column: utf8.RuneCountInString(name[:found[0]]) + 1, Fingerprint: fingerprint, Rich: rich}); err != nil {
				return err
			}
		}
		if rich {
			document, err := richedit.ParseContext(ctx, content)
			if err != nil {
				return err
			}
			for blockIndex, block := range document.Blocks {
				if err := ctx.Err(); err != nil {
					return err
				}
				for cursor := 0; cursor <= len(block.Visible); {
					found := matcher.FindStringIndex(block.Visible[cursor:])
					if found == nil {
						break
					}
					start, end := cursor+found[0], cursor+found[1]
					hit := SearchHit{
						Path: path, Relative: relative, Rich: true, RichBlock: blockIndex, RichStart: start, RichEnd: end,
						Line: bytes.Count(content[:block.Start], []byte{'\n'}) + 1, Column: utf8.RuneCountInString(block.Visible[:start]) + 1,
						Preview: searchPreview([]byte(block.Visible), start), ByteOffset: block.Start, Fingerprint: fingerprint,
					}
					if err := offer(hit); err != nil {
						return err
					}
					if end > start {
						cursor = end
					} else if end < len(block.Visible) {
						_, size := utf8.DecodeRuneInString(block.Visible[end:])
						cursor = end + size
					} else {
						break
					}
				}
			}
			return nil
		}
		offset := 0
		for lineIndex, rawLine := range bytes.Split(content, []byte{'\n'}) {
			if err := ctx.Err(); err != nil {
				return err
			}
			line := bytes.TrimSuffix(rawLine, []byte{'\r'})
			if found := matcher.FindIndex(line); found != nil {
				hit := SearchHit{
					Path: path, Relative: relative, Line: lineIndex + 1,
					Column:  utf8.RuneCount(line[:found[0]]) + 1,
					Preview: searchPreview(line, found[0]), ByteOffset: offset + found[0], MatchBytes: found[1] - found[0], Fingerprint: fingerprint,
				}
				if err := offer(hit); err != nil {
					return err
				}
			}
			offset += len(rawLine) + 1
		}
		return nil
	})
	if errors.Is(err, errStopSearch) {
		return summary, nil
	}
	return summary, err
}

func excludedSearchDir(name string) bool {
	switch strings.ToLower(name) {
	case "node_modules", "vendor", "dist", "build", "target", "coverage", "out":
		return true
	default:
		return false
	}
}

func searchPreview(line []byte, match int) string {
	if utf8.RuneCount(line) <= 160 {
		return string(line)
	}
	runes := []rune(string(line))
	position := utf8.RuneCount(line[:match])
	start := max(0, position-40)
	end := min(len(runes), start+160)
	preview := string(runes[start:end])
	if start > 0 {
		preview = "…" + preview
	}
	if end < len(runes) {
		preview += "…"
	}
	return preview
}
