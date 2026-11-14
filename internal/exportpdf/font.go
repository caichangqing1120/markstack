package exportpdf

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"io"
)

const bundledFontDigest = "26305be85f5378dc7d53f3dfa6c852a7e46621b226423400c1821aca9fc01c41"

//go:embed font/NotoSansSC-Regular.ttf.gz
var compressedFont []byte

//go:embed font/OFL.txt
var bundledFontLicense string

func BundledFont() ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(compressedFont))
	if err != nil {
		return nil, fmt.Errorf("PDF font archive: %w", err)
	}
	font, readErr := io.ReadAll(io.LimitReader(reader, 32<<20+1))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, fmt.Errorf("PDF font archive: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("PDF font archive: %w", closeErr)
	}
	if len(font) == 0 || len(font) > 32<<20 {
		return nil, ErrTooLarge
	}
	if actual := fmt.Sprintf("%x", sha256.Sum256(font)); actual != bundledFontDigest {
		return nil, errors.New("bundled PDF font checksum mismatch")
	}
	return font, nil
}

func FontLicense() string { return bundledFontLicense }
