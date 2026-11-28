package workspace

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image/png"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
)

const maxAssetBytes = 8 << 20

var (
	ErrAssetTooLarge        = errors.New("local image is too large")
	ErrAssetContentConflict = errors.New("image asset already exists with different content")
)

func PNGAssetPath(data []byte) string {
	digest := sha256.Sum256(data)
	return "assets/" + hex.EncodeToString(digest[:]) + ".png"
}

func WritePNGAsset(documentPath, savedLocation string, data []byte) (string, error) {
	return writePNGAsset(documentPath, savedLocation, data, nil)
}

func writePNGAsset(documentPath, savedLocation string, data []byte, beforeOpen func(string)) (string, error) {
	if len(data) > maxAssetBytes {
		return "", ErrAssetTooLarge
	}
	configuration, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	if configuration.Width <= 0 || configuration.Height <= 0 || configuration.Width > 4096 || configuration.Height > 4096 || configuration.Width > (8<<20)/configuration.Height {
		return "", ErrAssetTooLarge
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		return "", err
	}
	location, err := CanonicalLocation(documentPath)
	if err != nil || savedLocation == "" || location != savedLocation {
		return "", ErrOutsideWorkspace
	}
	documentInfo, err := os.Lstat(documentPath)
	if err != nil || !documentInfo.Mode().IsRegular() {
		return "", ErrOutsideWorkspace
	}
	directory := filepath.Dir(savedLocation)
	directoryInfo, err := os.Lstat(directory)
	if err != nil || !directoryInfo.IsDir() || directoryInfo.Mode()&os.ModeSymlink != 0 {
		return "", ErrOutsideWorkspace
	}
	if beforeOpen != nil {
		beforeOpen("document")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return "", ErrOutsideWorkspace
	}
	defer root.Close()
	opened, err := root.Open(".")
	if err != nil {
		return "", ErrOutsideWorkspace
	}
	openedInfo, err := opened.Stat()
	opened.Close()
	if err != nil || !os.SameFile(directoryInfo, openedInfo) {
		return "", ErrOutsideWorkspace
	}
	currentInfo, err := root.Lstat(filepath.Base(documentPath))
	if err != nil || !os.SameFile(documentInfo, currentInfo) {
		return "", ErrOutsideWorkspace
	}
	if err := root.Mkdir("assets", 0755); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	assetsInfo, err := root.Lstat("assets")
	if err != nil || !assetsInfo.IsDir() || assetsInfo.Mode()&os.ModeSymlink != 0 {
		return "", ErrOutsideWorkspace
	}
	if beforeOpen != nil {
		beforeOpen("assets")
	}
	assets, err := root.OpenRoot("assets")
	if err != nil {
		return "", ErrOutsideWorkspace
	}
	defer assets.Close()
	opened, err = assets.Open(".")
	if err != nil {
		return "", ErrOutsideWorkspace
	}
	openedInfo, err = opened.Stat()
	opened.Close()
	if err != nil || !os.SameFile(assetsInfo, openedInfo) {
		return "", ErrOutsideWorkspace
	}
	relative := PNGAssetPath(data)
	name := strings.TrimPrefix(relative, "assets/")
	readExisting := func() (string, error) {
		info, err := assets.Lstat(name)
		if err != nil || !info.Mode().IsRegular() {
			return "", ErrOutsideWorkspace
		}
		if info.Size() != int64(len(data)) {
			return "", ErrAssetContentConflict
		}
		existing, err := assets.ReadFile(name)
		if err != nil {
			return "", ErrOutsideWorkspace
		}
		if !bytes.Equal(existing, data) {
			return "", ErrAssetContentConflict
		}
		return relative, nil
	}
	file, err := assets.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if errors.Is(err, os.ErrExist) {
		return readExisting()
	}
	if err != nil {
		return "", err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		assets.Remove(name)
		return "", err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		assets.Remove(name)
		return "", err
	}
	if err := file.Close(); err != nil {
		assets.Remove(name)
		return "", err
	}
	return relative, nil
}

func ReadAsset(workspaceRoot, documentPath, savedLocation, relative string) ([]byte, error) {
	return readAsset(workspaceRoot, documentPath, savedLocation, relative, nil)
}

func readAsset(workspaceRoot, documentPath, savedLocation, relative string, beforeOpen func()) ([]byte, error) {
	if relative == "" || path.IsAbs(relative) || path.Clean(relative) != relative || strings.ContainsAny(relative, "\\:") {
		return nil, ErrOutsideWorkspace
	}
	for _, character := range relative {
		if unicode.IsControl(character) {
			return nil, ErrOutsideWorkspace
		}
	}
	currentLocation, err := CanonicalLocation(documentPath)
	if err != nil || savedLocation == "" || currentLocation != savedLocation {
		return nil, ErrOutsideWorkspace
	}
	info, err := os.Lstat(documentPath)
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrOutsideWorkspace
	}
	documentDirectory := filepath.Dir(savedLocation)
	if !filepath.IsAbs(documentDirectory) {
		return nil, ErrOutsideWorkspace
	}
	boundary := documentDirectory
	if workspaceRoot != "" {
		canonicalRoot, err := filepath.EvalSymlinks(workspaceRoot)
		if err != nil {
			return nil, err
		}
		if within, err := filepath.Rel(canonicalRoot, documentDirectory); err == nil && within != ".." && !strings.HasPrefix(within, ".."+string(filepath.Separator)) {
			boundary = canonicalRoot
		}
	}
	target := filepath.Join(documentDirectory, filepath.FromSlash(relative))
	confined, err := filepath.Rel(boundary, target)
	if err != nil || confined == ".." || strings.HasPrefix(confined, ".."+string(filepath.Separator)) {
		return nil, ErrOutsideWorkspace
	}
	parts, err := workspaceComponents(confined)
	if err != nil {
		return nil, ErrOutsideWorkspace
	}
	boundaryInfo, err := os.Lstat(boundary)
	if err != nil || !boundaryInfo.IsDir() || boundaryInfo.Mode()&os.ModeSymlink != 0 {
		return nil, ErrOutsideWorkspace
	}
	if beforeOpen != nil {
		beforeOpen()
	}
	root, err := os.OpenRoot(boundary)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	opened, err := root.Open(".")
	if err != nil {
		return nil, ErrOutsideWorkspace
	}
	openedInfo, err := opened.Stat()
	opened.Close()
	if err != nil || !os.SameFile(boundaryInfo, openedInfo) {
		return nil, ErrOutsideWorkspace
	}
	for depth := 1; depth <= len(parts); depth++ {
		component := filepath.Join(parts[:depth]...)
		info, err := root.Lstat(component)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || depth < len(parts) && !info.IsDir() || depth == len(parts) && !info.Mode().IsRegular() {
			return nil, ErrOutsideWorkspace
		}
		if depth == len(parts) && info.Size() > maxAssetBytes {
			return nil, ErrAssetTooLarge
		}
	}
	file, err := root.Open(confined)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxAssetBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxAssetBytes {
		return nil, ErrAssetTooLarge
	}
	return data, nil
}
