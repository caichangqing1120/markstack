package markdownmedia

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"golang.org/x/net/html"

	"markstack/internal/workspace"
)

type AuditedImage struct {
	Relative  string
	Bytes     int64
	Documents []string
	info      os.FileInfo
}

func (image AuditedImage) Trashable() bool {
	return len(image.Documents) == 0 && workspace.ValidTrashPath(filepath.FromSlash(image.Relative))
}

type AuditResult struct {
	Referenced, Orphans []AuditedImage
	TotalBytes          int64
	Incomplete          bool
	root                os.FileInfo
}

var errAuditLimit = errors.New("image audit reference limit reached")

func Audit(ctx context.Context, root string) (AuditResult, error) {
	info, err := os.Stat(root)
	if err != nil {
		return AuditResult{}, err
	}
	result := AuditResult{root: info}
	var images []AuditedImage
	references := make(map[string][]string)
	referencesLeft := 100_000
	incomplete, err := workspace.ScanReferenceFiles(ctx, root, true, func(file workspace.ReferenceFile) error {
		if file.Source == nil {
			if imagePath(file.Relative) && insideAssets(file.Relative) {
				images = append(images, AuditedImage{Relative: file.Relative, Bytes: file.Info.Size(), info: file.Info})
			}
			return nil
		}
		record := func(candidate string) error {
			if !imagePath(candidate) || !insideAssets(candidate) {
				return nil
			}
			key := strings.ToLower(candidate)
			files := references[key]
			if len(files) == 0 || files[len(files)-1] != file.Relative {
				referencesLeft--
				if referencesLeft < 0 {
					return errAuditLimit
				}
				references[key] = append(files, file.Relative)
			}
			return nil
		}
		if workspace.KindOf(file.Relative) == workspace.RichHTMLKind {
			return scanHTMLBytes(ctx, file.Relative, file.Source, record)
		}
		return workspace.WalkMarkdownReferences(ctx, file.Source, func(node ast.Node) error {
			switch node := node.(type) {
			case *ast.Image:
				return record(workspace.MarkdownReferencePath(file.Relative, node.Destination))
			case *ast.Link:
				return record(workspace.MarkdownReferencePath(file.Relative, node.Destination))
			case *ast.RawHTML:
				return scanHTMLImages(ctx, file.Relative, node.Segments, file.Source, record)
			case *ast.HTMLBlock:
				return scanHTMLImages(ctx, file.Relative, node.Lines(), file.Source, record)
			}
			return nil
		})
	})
	if errors.Is(err, errAuditLimit) {
		incomplete, err = true, nil
	}
	if err != nil {
		return AuditResult{}, err
	}
	current, err := os.Stat(root)
	if err != nil || !os.SameFile(info, current) {
		return AuditResult{}, workspace.ErrConflict
	}
	result.Incomplete = incomplete
	for _, image := range images {
		result.TotalBytes += image.Bytes
		image.Documents = references[strings.ToLower(image.Relative)]
		if len(image.Documents) == 0 {
			result.Orphans = append(result.Orphans, image)
		} else {
			sort.Strings(image.Documents)
			result.Referenced = append(result.Referenced, image)
		}
	}
	byPath := func(images []AuditedImage) {
		sort.Slice(images, func(left, right int) bool { return images[left].Relative < images[right].Relative })
	}
	byPath(result.Referenced)
	byPath(result.Orphans)
	return result, nil
}

func (result AuditResult) ConfirmedOrphans(previous AuditResult, selected map[string]bool) ([]AuditedImage, error) {
	if result.Incomplete || previous.Incomplete || result.root == nil || previous.root == nil || !os.SameFile(result.root, previous.root) {
		return nil, workspace.ErrConflict
	}
	confirmed := make([]AuditedImage, 0, len(selected))
	current := make(map[string]AuditedImage, len(result.Orphans))
	for _, image := range result.Orphans {
		current[image.Relative] = image
	}
	for _, image := range previous.Orphans {
		if !selected[image.Relative] {
			continue
		}
		next, ok := current[image.Relative]
		if !ok || !image.Trashable() || !workspace.SameFileRevision(image.info, next.info) {
			return nil, workspace.ErrConflict
		}
		confirmed = append(confirmed, next)
	}
	if len(confirmed) != len(selected) {
		return nil, workspace.ErrConflict
	}
	return confirmed, nil
}

func (result AuditResult) Trash(root string, image AuditedImage) (workspace.TrashResult, error) {
	info, err := os.Stat(root)
	if err != nil || result.Incomplete || result.root == nil || !os.SameFile(result.root, info) || image.info == nil || !image.Trashable() {
		return workspace.TrashResult{}, workspace.ErrConflict
	}
	return workspace.Trash(root, filepath.FromSlash(image.Relative), image.info)
}

func insideAssets(relative string) bool {
	for part := range strings.SplitSeq(path.Dir(relative), "/") {
		if strings.EqualFold(part, "assets") {
			return true
		}
	}
	return false
}

func imagePath(relative string) bool {
	switch strings.ToLower(path.Ext(relative)) {
	case ".png", ".apng", ".jpg", ".jpeg", ".jfif", ".gif", ".svg", ".svgz", ".webp", ".avif", ".heif", ".heic", ".bmp", ".ico", ".tif", ".tiff", ".jxl":
		return true
	default:
		return false
	}
}

func scanHTMLImages(ctx context.Context, relative string, lines *text.Segments, source []byte, record func(string) error) error {
	var body bytes.Buffer
	for index := 0; index < lines.Len(); index++ {
		line := lines.At(index)
		body.Write(line.Value(source))
	}
	return scanHTMLBytes(ctx, relative, body.Bytes(), record)
}

func scanHTMLBytes(ctx context.Context, relative string, source []byte, record func(string) error) error {
	tokenizer := html.NewTokenizer(bytes.NewReader(source))
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch tokenizer.Next() {
		case html.ErrorToken:
			if errors.Is(tokenizer.Err(), io.EOF) {
				return nil
			}
			return tokenizer.Err()
		case html.StartTagToken, html.SelfClosingTagToken:
			name, more := tokenizer.TagName()
			if !bytes.Equal(name, []byte("img")) && !bytes.Equal(name, []byte("source")) {
				continue
			}
			for more {
				key, value, next := tokenizer.TagAttr()
				more = next
				switch string(key) {
				case "src":
					if err := record(workspace.RelativeReferencePath(relative, string(value))); err != nil {
						return err
					}
				case "srcset":
					for candidate := range strings.SplitSeq(string(value), ",") {
						candidate = strings.TrimSpace(candidate)
						if end := strings.IndexAny(candidate, " \t\r\n\f"); end >= 0 {
							candidate = candidate[:end]
						}
						if err := record(workspace.RelativeReferencePath(relative, candidate)); err != nil {
							return err
						}
					}
				}
			}
		}
	}
}

func SharedImageReferences(ctx context.Context, root, currentDocRel, targetRel string) (others []string, incomplete bool, err error) {
	want := strings.ToLower(path.Clean(filepath.ToSlash(targetRel)))
	current := path.Clean(filepath.ToSlash(currentDocRel))
	seen := make(map[string]bool)
	incomplete, err = workspace.ScanReferenceFiles(ctx, root, true, func(file workspace.ReferenceFile) error {
		if file.Source == nil {
			return nil
		}
		docRel := path.Clean(filepath.ToSlash(file.Relative))
		if strings.EqualFold(docRel, current) {
			return nil
		}
		record := func(candidate string) error {
			if candidate == "" {
				return nil
			}
			if strings.ToLower(path.Clean(filepath.ToSlash(candidate))) == want && !seen[docRel] {
				seen[docRel] = true
				others = append(others, docRel)
			}
			return nil
		}
		if workspace.KindOf(file.Relative) == workspace.RichHTMLKind {
			return scanHTMLBytes(ctx, file.Relative, file.Source, record)
		}
		return workspace.WalkMarkdownReferences(ctx, file.Source, func(node ast.Node) error {
			switch node := node.(type) {
			case *ast.Image:
				return record(workspace.MarkdownReferencePath(file.Relative, node.Destination))
			case *ast.Link:
				return record(workspace.MarkdownReferencePath(file.Relative, node.Destination))
			case *ast.RawHTML:
				return scanHTMLImages(ctx, file.Relative, node.Segments, file.Source, record)
			case *ast.HTMLBlock:
				return scanHTMLImages(ctx, file.Relative, node.Lines(), file.Source, record)
			}
			return nil
		})
	})
	if err != nil {
		return nil, false, err
	}
	sort.Strings(others)
	return others, incomplete, nil
}
