package workspace

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

var ErrInvalidExport = errors.New("invalid HTML export")
var ErrInvalidPDFExport = errors.New("invalid PDF export")

func WriteNew(path string, data []byte) error {
	if path == "" || len(data) > 32<<20 || !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return ErrInvalidExport
	}
	extension := strings.ToLower(filepath.Ext(path))
	if extension != ".html" && extension != ".htm" {
		return fmt.Errorf("%w: choose an .html target", ErrInvalidExport)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(absolute); err == nil {
		return ErrConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeAtomicNew(absolute, data, 0644)
}

func WriteNewPDF(path string, data []byte) error {
	if path == "" || len(data) > 64<<20 || !bytes.HasPrefix(data, []byte("%PDF-")) || !bytes.HasSuffix(bytes.TrimSpace(data), []byte("%%EOF")) {
		return ErrInvalidPDFExport
	}
	if strings.ToLower(filepath.Ext(path)) != ".pdf" {
		return fmt.Errorf("%w: choose a .pdf target", ErrInvalidPDFExport)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(absolute); err == nil {
		return ErrConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeAtomicNew(absolute, data, 0644)
}
