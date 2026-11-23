package exportpdf

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"image"
	"io"

	"github.com/go-pdf/fpdf"
)

const maxPDFImageAliases = 256

type pdfImages struct {
	groups  map[pdfImageBucket]*pdfImageGroup
	aliases map[[sha256.Size]byte]string
}

type pdfImageBucket struct {
	size    image.Point
	alpha8  bool
	content [sha256.Size]byte
}

type pdfImageGroup struct {
	first string
	names map[[sha256.Size]byte]string
}

func (images *pdfImages) register(pdf *fpdf.Fpdf, data []byte, options fpdf.ImageOptions, size image.Point) (string, error) {
	raw := sha256.Sum256(data)
	name := fmt.Sprintf("%s-%x", options.ImageType, raw)
	if pdf.GetImageInfo(name) != nil {
		return name, nil
	}
	if alias := images.aliases[raw]; alias != "" {
		return alias, nil
	}
	if options.ImageType == "JPEG" {
		return addPDFImage(pdf, name, data, options)
	}
	bucket, err := bucketPDFImage(data, size)
	if err != nil {
		return "", err
	}
	if images.groups == nil {
		images.groups = make(map[pdfImageBucket]*pdfImageGroup)
	}
	group := images.groups[bucket]
	if group == nil {
		images.groups[bucket] = &pdfImageGroup{first: name}
		return addPDFImage(pdf, name, data, options)
	}
	if group.names == nil {
		identity, err := registeredPDFImageKey(pdf.GetImageInfo(group.first))
		if err != nil {
			return "", err
		}
		group.names = map[[sha256.Size]byte]string{identity: group.first}
	}
	probe := fpdf.New("P", "mm", "A4", "")
	info := probe.RegisterImageOptionsReader("image", options, bytes.NewReader(data))
	if err := probe.Error(); err != nil {
		return "", err
	}
	identity, err := registeredPDFImageKey(info)
	if err != nil {
		return "", err
	}
	if alias := group.names[identity]; alias != "" {
		if len(images.aliases) < maxPDFImageAliases {
			if images.aliases == nil {
				images.aliases = make(map[[sha256.Size]byte]string)
			}
			images.aliases[raw] = alias
		}
		return alias, nil
	}
	group.names[identity] = name
	return addPDFImage(pdf, name, data, options)
}

func addPDFImage(pdf *fpdf.Fpdf, name string, data []byte, options fpdf.ImageOptions) (string, error) {
	pdf.RegisterImageOptionsReader(name, options, bytes.NewReader(data))
	return name, pdf.Error()
}

func registeredPDFImageKey(info *fpdf.ImageInfoType) ([sha256.Size]byte, error) {
	encoded, err := info.GobEncode()
	return sha256.Sum256(encoded), err
}

func bucketPDFImage(data []byte, size image.Point) (pdfImageBucket, error) {
	bucket := pdfImageBucket{size: size}
	// FPDF preserves these filtered RGBA bytes across its color and mask streams.
	if len(data) < 33 || data[24] != 8 || (data[25] != 4 && data[25] != 6) || data[28] != 0 {
		return bucket, nil
	}
	bucket.alpha8 = true
	stream, err := zlib.NewReader(&pngImageData{chunks: data[33:]})
	if err != nil {
		return bucket, err
	}
	defer stream.Close()
	digest := sha256.New()
	_, err = io.Copy(digest, stream)
	digest.Sum(bucket.content[:0])
	return bucket, err
}

type pngImageData struct {
	chunks []byte
	data   []byte
}

func (reader *pngImageData) Read(destination []byte) (int, error) {
	for len(reader.data) == 0 {
		if len(reader.chunks) == 0 {
			return 0, io.EOF
		}
		if len(reader.chunks) < 12 {
			return 0, io.ErrUnexpectedEOF
		}
		length := binary.BigEndian.Uint32(reader.chunks[:4])
		if uint64(length) > uint64(len(reader.chunks)-12) {
			return 0, io.ErrUnexpectedEOF
		}
		end := 8 + int(length)
		switch string(reader.chunks[4:8]) {
		case "IDAT":
			reader.data = reader.chunks[8:end]
		case "IEND":
			return 0, io.EOF
		}
		reader.chunks = reader.chunks[end+4:]
	}
	count := copy(destination, reader.data)
	reader.data = reader.data[count:]
	return count, nil
}
