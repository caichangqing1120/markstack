package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

const MaxDocumentSize = 16 << 20

type Node struct {
	Path  string
	Name  string
	Depth int
	Dir   bool
}

func IsMarkdown(path string) bool {
	return KindOf(path) == MarkdownKind
}

func Scan(root string) ([]Node, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", root)
	}
	var nodes []Node
	var walk func(string, int) error
	walk = func(dir string, depth int) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		slices.SortFunc(entries, func(left, right os.DirEntry) int {
			if left.IsDir() != right.IsDir() {
				if left.IsDir() {
					return -1
				}
				return 1
			}
			return strings.Compare(strings.ToLower(left.Name()), strings.ToLower(right.Name()))
		})
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".") || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			if !entry.IsDir() && KindOf(path) == UnsupportedKind {
				continue
			}
			nodes = append(nodes, Node{Path: path, Name: entry.Name(), Depth: depth, Dir: entry.IsDir()})
			if entry.IsDir() {
				if err := walk(path, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(root, 1); err != nil {
		return nil, err
	}
	return nodes, nil
}

func Read(path string) (string, error) {
	snapshot, err := Load(path)
	return snapshot.Text, err
}

func Save(path, body string) error {
	if path == "" || !IsMarkdown(path) {
		return fmt.Errorf("choose a Markdown file path: %s", path)
	}
	if !utf8.ValidString(body) || strings.IndexByte(body, 0) >= 0 {
		return errors.New("document is not UTF-8 text")
	}
	mode := os.FileMode(0644)
	exists := false
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("cannot replace a non-regular file: %s", path)
		}
		exists = true
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if !exists {
		return writeAtomicNew(path, []byte(body), mode)
	}
	return writeAtomic(path, []byte(body), mode)
}
