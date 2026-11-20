package workspace

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"
)

var (
	ErrConflict         = errors.New("document changed on disk")
	ErrReadOnlyEncoding = errors.New("document encoding is not editable UTF-8")
)

var utf8BOM = []byte{0xef, 0xbb, 0xbf}

type Snapshot struct {
	Path    string
	Bytes   []byte
	Text    string
	BOM     bool
	Newline string
	Mode    os.FileMode

	exists      bool
	fingerprint [sha256.Size]byte
	info        os.FileInfo
	location    string
}

func (snapshot Snapshot) Exists() bool     { return snapshot.exists }
func (snapshot Snapshot) Location() string { return snapshot.location }

func (snapshot Snapshot) SameRevision(other Snapshot) bool {
	return snapshot.exists && other.exists && snapshot.location == other.location &&
		os.SameFile(snapshot.info, other.info) && snapshot.fingerprint == other.fingerprint
}

func Load(path string) (Snapshot, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return Snapshot{}, err
	}
	location, err := CanonicalLocation(path)
	if err != nil {
		return Snapshot{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return Snapshot{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxDocumentSize {
		return Snapshot{}, fmt.Errorf("not a supported text file: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, err
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return Snapshot{}, fmt.Errorf("%w: %s", ErrReadOnlyEncoding, path)
	}
	value := data
	bom := bytes.HasPrefix(data, utf8BOM)
	if bom {
		value = data[len(utf8BOM):]
	}
	newline := "\n"
	if bytes.Contains(value, []byte("\r\n")) {
		newline = "\r\n"
	}
	return Snapshot{
		Path: path, Bytes: data, Text: string(value), BOM: bom, Newline: newline,
		Mode: info.Mode().Perm(), exists: true, fingerprint: sha256.Sum256(data), info: info, location: location,
	}, nil
}

func (snapshot Snapshot) Encode(text string) []byte {
	if snapshot.BOM {
		data := make([]byte, 0, len(utf8BOM)+len(text))
		data = append(data, utf8BOM...)
		return append(data, text...)
	}
	return []byte(text)
}

func Write(snapshot Snapshot, next []byte) (Snapshot, error) {
	if snapshot.Path == "" || KindOf(snapshot.Path) == UnsupportedKind {
		return Snapshot{}, fmt.Errorf("choose a supported document path: %s", snapshot.Path)
	}
	if len(next) > MaxDocumentSize || !utf8.Valid(next) || bytes.IndexByte(next, 0) >= 0 {
		return Snapshot{}, ErrReadOnlyEncoding
	}
	if snapshot.exists {
		if err := Verify(snapshot); err != nil {
			return Snapshot{}, err
		}
	} else if _, err := os.Lstat(snapshot.Path); err == nil {
		return Snapshot{}, ErrConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return Snapshot{}, err
	}
	if snapshot.exists && bytes.Equal(snapshot.Bytes, next) {
		return snapshot, nil
	}
	mode := snapshot.Mode
	if mode == 0 {
		mode = 0644
	}
	var writeErr error
	if snapshot.exists {
		writeErr = writeAtomic(snapshot.Path, next, mode)
	} else {
		writeErr = writeAtomicNew(snapshot.Path, next, mode)
	}
	if writeErr != nil {
		return Snapshot{}, writeErr
	}
	return Load(snapshot.Path)
}

func Verify(snapshot Snapshot) error {
	if snapshot.Path == "" || !snapshot.exists {
		return ErrConflict
	}
	location, err := CanonicalLocation(snapshot.Path)
	if err != nil || location != snapshot.location {
		return ErrConflict
	}
	info, err := os.Lstat(snapshot.Path)
	if errors.Is(err, os.ErrNotExist) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || !os.SameFile(info, snapshot.info) {
		return ErrConflict
	}
	current, err := os.ReadFile(snapshot.Path)
	if err != nil {
		return err
	}
	if sha256.Sum256(current) != snapshot.fingerprint {
		return ErrConflict
	}
	return nil
}

func stageAtomic(path string, data []byte, mode os.FileMode) (string, error) {
	temp, err := os.CreateTemp(filepath.Dir(path), ".markstack-*")
	if err != nil {
		return "", err
	}
	name := temp.Name()
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		os.Remove(name)
		return "", err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		os.Remove(name)
		return "", err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		os.Remove(name)
		return "", err
	}
	if err := temp.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	temp, err := stageAtomic(path, data, mode)
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	if err := os.Rename(temp, path); err != nil {
		return err
	}
	return syncParentDirectory(path)
}

func writeAtomicNew(path string, data []byte, mode os.FileMode) error {
	temp, err := stageAtomic(path, data, mode)
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	if err := os.Link(temp, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return ErrConflict
		}
		return err
	}
	return syncParentDirectory(path)
}

func syncParentDirectory(path string) error {
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func (snapshot Snapshot) SameLocation(path string) bool {
	if snapshot.Path == "" {
		return false
	}
	original, err := CanonicalLocation(snapshot.Path)
	if err != nil {
		return false
	}
	candidate, err := CanonicalLocation(path)
	return err == nil && candidate == original
}

// CanonicalLocation resolves the parent directory without following the file itself.
func CanonicalLocation(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	directory, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, filepath.Base(absolute)), nil
}
