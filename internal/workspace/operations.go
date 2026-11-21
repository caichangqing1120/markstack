package workspace

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	ErrOutsideWorkspace   = errors.New("path is outside the workspace")
	ErrUnsupportedRename  = errors.New("safe rename is not available on this platform")
	ErrRenameTargetExists = fmt.Errorf("rename target already exists: %w", ErrConflict)
	ErrTrashUnsupported   = errors.New("system Trash is not available on this platform")
)

type TrashResult struct {
	Original string
	Trashed  string
}

func CreateMarkdown(root, relative string, body []byte) (Snapshot, error) {
	if !IsMarkdown(relative) || len(body) > MaxDocumentSize || !utf8.Valid(body) || bytes.IndexByte(body, 0) >= 0 {
		return Snapshot{}, ErrReadOnlyEncoding
	}
	parent, name, absolute, err := workspaceParent(root, relative)
	if err != nil {
		return Snapshot{}, err
	}
	defer parent.Close()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return Snapshot{}, err
	}
	temporary := ".markstack-" + hex.EncodeToString(nonce[:])
	file, err := parent.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return Snapshot{}, err
	}
	defer parent.Remove(temporary)
	if _, err := file.Write(body); err != nil {
		file.Close()
		return Snapshot{}, err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return Snapshot{}, err
	}
	if err := file.Close(); err != nil {
		return Snapshot{}, err
	}
	if err := parent.Link(temporary, name); err != nil {
		if errors.Is(err, os.ErrExist) {
			return Snapshot{}, ErrConflict
		}
		return Snapshot{}, err
	}
	info, err := parent.Lstat(name)
	if err != nil {
		return Snapshot{}, err
	}
	value := body
	bom := bytes.HasPrefix(body, utf8BOM)
	if bom {
		value = body[len(utf8BOM):]
	}
	newline := "\n"
	if bytes.Contains(value, []byte("\r\n")) {
		newline = "\r\n"
	}
	return Snapshot{
		Path: absolute, Bytes: append([]byte(nil), body...), Text: string(value), BOM: bom, Newline: newline,
		Mode: info.Mode().Perm(), exists: true, fingerprint: sha256.Sum256(body), info: info, location: absolute,
	}, nil
}

func CreateFolder(root, relative string) error {
	parent, name, _, err := workspaceParent(root, relative)
	if err != nil {
		return err
	}
	defer parent.Close()
	if err := parent.Mkdir(name, 0755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return ErrConflict
		}
		return err
	}
	return nil
}

func Rename(root, oldRelative, newRelative string) error {
	if oldRelative == newRelative {
		return nil
	}
	oldParent, oldName, _, err := workspaceParent(root, oldRelative)
	if err != nil {
		return err
	}
	defer oldParent.Close()
	newParent, newName, _, err := workspaceParent(root, newRelative)
	if err != nil {
		return err
	}
	defer newParent.Close()
	info, err := oldParent.Lstat(oldName)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() && !info.IsDir() {
		return ErrOutsideWorkspace
	}
	if _, err := newParent.Lstat(newName); err == nil {
		return ErrRenameTargetExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	oldDir, err := oldParent.Open(".")
	if err != nil {
		return err
	}
	defer oldDir.Close()
	newDir, err := newParent.Open(".")
	if err != nil {
		return err
	}
	defer newDir.Close()
	if err := renameNoReplace(oldDir, oldName, newDir, newName); err != nil {
		if errors.Is(err, os.ErrExist) {
			return ErrRenameTargetExists
		}
		return err
	}
	return nil
}

func workspaceParent(root, relative string) (*os.Root, string, string, error) {
	parts, err := workspaceComponents(relative)
	if err != nil {
		return nil, "", "", err
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, "", "", err
	}
	rootHandle, err := os.OpenRoot(canonical)
	if err != nil {
		return nil, "", "", err
	}
	defer rootHandle.Close()
	for depth := 1; depth < len(parts); depth++ {
		ancestor := filepath.Join(parts[:depth]...)
		info, err := rootHandle.Lstat(ancestor)
		if err != nil {
			return nil, "", "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, "", "", ErrOutsideWorkspace
		}
	}
	parent, err := rootHandle.OpenRoot(filepath.Dir(relative))
	if err != nil {
		return nil, "", "", err
	}
	return parent, parts[len(parts)-1], filepath.Join(canonical, relative), nil
}

func workspaceComponents(relative string) ([]string, error) {
	if relative == "" || filepath.IsAbs(relative) || filepath.Clean(relative) != relative || strings.Contains(relative, "\\") {
		return nil, ErrOutsideWorkspace
	}
	parts := strings.Split(relative, string(filepath.Separator))
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".") || excludedSearchDir(part) {
			return nil, ErrOutsideWorkspace
		}
		for _, character := range part {
			if unicode.IsControl(character) {
				return nil, fmt.Errorf("%w: control character in name", ErrOutsideWorkspace)
			}
		}
	}
	return parts, nil
}

func ValidateName(name string) error {
	parts, err := workspaceComponents(name)
	if err != nil {
		return err
	}
	if len(parts) != 1 {
		return ErrOutsideWorkspace
	}
	return nil
}
