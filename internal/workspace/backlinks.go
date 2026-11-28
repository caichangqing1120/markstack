package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const referenceResultLimit = 500

var errReferenceLimit = errors.New("workspace reference scan limit reached")

type ReferenceFile struct {
	Relative string
	Info     os.FileInfo
	Source   []byte
}

func ScanReferenceFiles(ctx context.Context, directory string, audit bool, visit func(ReferenceFile) error) (incomplete bool, err error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return false, err
	}
	defer root.Close()
	entriesLeft, documentsLeft, bytesLeft := 50_000, 4_000, int64(128<<20)
	var walk func(string, int) error
	walk = func(relative string, depth int) error {
		if depth > 64 {
			return errReferenceLimit
		}
		folder, err := root.Open(relative)
		if err != nil {
			return err
		}
		defer folder.Close()
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			entries, readErr := folder.ReadDir(128)
			for _, entry := range entries {
				if err := ctx.Err(); err != nil {
					return err
				}
				entriesLeft--
				if entriesLeft < 0 {
					return errReferenceLimit
				}
				name := entry.Name()
				if strings.EqualFold(name, ".git") || !audit && (strings.HasPrefix(name, ".") || excludedSearchDir(name)) {
					continue
				}
				child := filepath.Join(relative, name)
				if entry.Type()&os.ModeSymlink != 0 {
					if audit {
						incomplete = true
					}
					continue
				}
				if entry.IsDir() {
					if err := walk(child, depth+1); err != nil {
						return err
					}
					continue
				}
				if !entry.Type().IsRegular() {
					continue
				}
				markdown := IsMarkdown(child)
				rich := audit && KindOf(child) == RichHTMLKind
				if !markdown && !audit {
					continue
				}
				info, err := root.Lstat(child)
				if err != nil {
					return err
				}
				if !info.Mode().IsRegular() {
					incomplete = true
					continue
				}
				file := ReferenceFile{Relative: filepath.ToSlash(child), Info: info}
				if markdown || rich {
					if info.Size() > MaxDocumentSize {
						incomplete = true
						continue
					}
					documentsLeft--
					if documentsLeft < 0 || info.Size() > bytesLeft {
						return errReferenceLimit
					}
					raw, err := readReferenceSource(root, child, info)
					if err != nil {
						return err
					}
					bytesLeft -= int64(len(raw))
					if !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
						incomplete = true
						continue
					}
					file.Source = raw
				}
				if err := visit(file); err != nil {
					return err
				}
			}
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			if readErr != nil {
				return readErr
			}
		}
	}
	err = walk(".", 0)
	if errors.Is(err, errReferenceLimit) {
		return true, nil
	}
	return incomplete, err
}

func readReferenceSource(root *os.Root, relative string, expected os.FileInfo) ([]byte, error) {
	file, err := root.Open(relative)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !SameFileRevision(expected, opened) {
		return nil, ErrConflict
	}
	raw, err := io.ReadAll(io.LimitReader(file, expected.Size()+1))
	if err != nil {
		return nil, err
	}
	current, err := root.Lstat(relative)
	if err != nil || int64(len(raw)) != expected.Size() || !SameFileRevision(expected, current) {
		return nil, ErrConflict
	}
	return raw, nil
}

var referenceParser = goldmark.New(goldmark.WithExtensions(extension.GFM))

func WalkMarkdownReferences(ctx context.Context, source []byte, visit func(ast.Node) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	parsed := referenceParser.Parser().Parse(text.NewReader(source))
	return ast.Walk(parsed, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if err := ctx.Err(); err != nil {
			return ast.WalkStop, err
		}
		return ast.WalkContinue, visit(node)
	})
}

func MarkdownReferencePath(relative string, destination []byte) string {
	destination = util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(destination)))
	return RelativeReferencePath(relative, string(destination))
}

func RelativeReferencePath(relative, destination string) string {
	value, err := url.Parse(strings.ReplaceAll(destination, `\`, "/"))
	if err != nil || value.Scheme != "" || value.Host != "" || value.User != nil || value.Opaque != "" {
		return ""
	}
	decoded := strings.ReplaceAll(value.Path, `\`, "/")
	if decoded == "" || strings.HasPrefix(decoded, "/") || strings.ContainsRune(decoded, ':') || strings.ContainsFunc(decoded, unicode.IsControl) {
		return ""
	}
	resolved := path.Join(path.Dir(relative), decoded)
	if resolved == "." || resolved == ".." || strings.HasPrefix(resolved, "../") || !filepath.IsLocal(filepath.FromSlash(resolved)) {
		return ""
	}
	return resolved
}

type Backlink struct {
	Relative    string
	Line        int
	ByteOffset  int
	Context     string
	Fingerprint [sha256.Size]byte
}

func Backlinks(ctx context.Context, root, target string) ([]Backlink, bool, error) {
	relative, err := filepath.Rel(root, target)
	if err != nil || !filepath.IsLocal(relative) || !IsMarkdown(relative) {
		return nil, false, ErrOutsideWorkspace
	}
	relative = filepath.ToSlash(relative)
	var links []Backlink
	incomplete, err := ScanReferenceFiles(ctx, root, false, func(file ReferenceFile) error {
		source := bytes.TrimPrefix(file.Source, utf8BOM)
		var fingerprint [sha256.Size]byte
		first := len(links)
		lineStart, lineNumber := 0, 1
		return WalkMarkdownReferences(ctx, source, func(node ast.Node) error {
			link, ok := node.(*ast.Link)
			if !ok || !sameReferencePath(root, relative, MarkdownReferencePath(file.Relative, link.Destination)) {
				return nil
			}
			if len(links) == referenceResultLimit {
				return errReferenceLimit
			}
			offset := link.Pos()
			if offset < lineStart || offset >= len(source) {
				return nil
			}
			for {
				advance := bytes.IndexByte(source[lineStart:offset], '\n')
				if advance < 0 {
					break
				}
				lineStart += advance + 1
				lineNumber++
			}
			end := bytes.IndexByte(source[offset:], '\n')
			if end < 0 {
				end = len(source) - offset
			}
			if len(links) == first {
				fingerprint = sha256.Sum256(file.Source)
			}
			line := bytes.TrimSuffix(source[lineStart:offset+end], []byte{'\r'})
			links = append(links, Backlink{file.Relative, lineNumber, offset, searchPreview(line, offset-lineStart), fingerprint})
			return nil
		})
	})
	sort.SliceStable(links, func(left, right int) bool { return links[left].Relative < links[right].Relative })
	return links, incomplete, err
}

func sameReferencePath(root, target, candidate string) bool {
	if candidate == target {
		return true
	}
	if runtime.GOOS == "linux" || !strings.EqualFold(candidate, target) {
		return false
	}
	actual, err := os.Lstat(filepath.Join(root, filepath.FromSlash(candidate)))
	if err != nil || !actual.Mode().IsRegular() {
		return false
	}
	expected, err := os.Lstat(filepath.Join(root, filepath.FromSlash(target)))
	return err == nil && os.SameFile(actual, expected)
}
