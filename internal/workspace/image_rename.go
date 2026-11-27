package workspace

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
)

func ImageAssetName(relative, name string) (string, error) {
	if !isImageAssetPath(relative) || !portableImageName(name) {
		return "", ErrOutsideWorkspace
	}
	extension := path.Ext(relative)
	if path.Ext(name) == "" {
		name += extension
	} else if !strings.EqualFold(path.Ext(name), extension) {
		return "", errors.New("重命名不能更改图片扩展名")
	}
	return path.Join(path.Dir(relative), name), nil
}

func portableImageName(name string) bool {
	if ValidateName(name) != nil || len(name) > 240 || strings.HasSuffix(name, ".") {
		return false
	}
	for _, character := range name {
		if !unicode.IsLetter(character) && !unicode.IsNumber(character) && !unicode.IsMark(character) && !strings.ContainsRune("-_.", character) {
			return false
		}
	}
	base, _, _ := strings.Cut(strings.ToUpper(name), ".")
	switch base {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "COM¹", "COM²", "COM³", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9", "LPT¹", "LPT²", "LPT³":
		return false
	}
	return true
}

func isImageAssetPath(relative string) bool {
	if !strings.HasPrefix(relative, "assets/") || path.Clean(relative) != relative || strings.ContainsAny(relative, "\\:") {
		return false
	}
	_, err := workspaceComponents(filepath.FromSlash(relative))
	return err == nil
}

func RenameImageAsset(workspaceRoot, documentPath, savedLocation, from, to string, expected os.FileInfo) (os.FileInfo, error) {
	if !isImageAssetPath(from) || !isImageAssetPath(to) || path.Dir(from) != path.Dir(to) || !strings.EqualFold(path.Ext(from), path.Ext(to)) {
		return nil, ErrOutsideWorkspace
	}
	location, err := CanonicalLocation(documentPath)
	if err != nil || savedLocation == "" || location != savedLocation {
		return nil, ErrOutsideWorkspace
	}
	directory := filepath.Dir(location)
	boundary := directory
	if workspaceRoot != "" {
		boundary, err = filepath.EvalSymlinks(workspaceRoot)
		if err != nil {
			return nil, err
		}
	}
	relative, err := filepath.Rel(boundary, filepath.Join(directory, filepath.FromSlash(from)))
	if err != nil {
		return nil, ErrOutsideWorkspace
	}
	parent, name, _, err := workspaceParent(boundary, relative)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	info, err := parent.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || expected != nil && !os.SameFile(expected, info) {
		return nil, ErrOutsideWorkspace
	}
	newName := path.Base(to)
	if name == newName {
		return info, nil
	}
	if err := parent.Link(name, newName); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, ErrRenameTargetExists
		}
		return nil, err
	}
	linked, err := parent.Lstat(newName)
	if err != nil || !os.SameFile(info, linked) {
		parent.Remove(newName)
		return nil, ErrOutsideWorkspace
	}
	if err := parent.Remove(name); err != nil {
		parent.Remove(newName)
		return nil, err
	}
	return info, nil
}
