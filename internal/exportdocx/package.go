package exportdocx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
)

const (
	maxOutput = 64 << 20
	wordNS    = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	packageNS = "http://schemas.openxmlformats.org/package/2006/"
	relsNS    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	drawingNS = "http://schemas.openxmlformats.org/drawingml/2006/"
	pageWidth = 8640
)

var ErrTooLarge = errors.New("DOCX export is too large")

type limitedBuffer struct {
	data bytes.Buffer
	err  error
}

func (out *limitedBuffer) Write(payload []byte) (int, error) {
	if out.err == nil && len(payload) > maxOutput-out.data.Len() {
		out.err = ErrTooLarge
	}
	if out.err != nil {
		return 0, out.err
	}
	return out.data.Write(payload)
}

func (out *limitedBuffer) write(value string) {
	if out.err != nil {
		return
	}
	if len(value) > maxOutput-out.data.Len() {
		out.err = ErrTooLarge
		return
	}
	out.data.WriteString(value)
}

func (out *limitedBuffer) escape(value string) {
	if out.err == nil {
		out.err = xml.EscapeText(out, []byte(strings.Map(func(character rune) rune {
			if character < 0x20 && character != '\t' && character != '\n' && character != '\r' || character == 0xfffe || character == 0xffff {
				return -1
			}
			return character
		}, value)))
	}
}

type part struct {
	name string
	data []byte
}

func (doc *document) pack() ([]byte, error) {
	numbering := renderNumbering(doc.lists)
	if err := errors.Join(doc.body.err, doc.rels.err, numbering.err); err != nil {
		return nil, err
	}
	parts := []part{
		{"[Content_Types].xml", []byte(contentTypes)},
		{"_rels/.rels", []byte(packageRelationships)},
		{"word/document.xml", doc.body.data.Bytes()},
		{"word/styles.xml", []byte(renderStyles())},
		{"word/numbering.xml", numbering.data.Bytes()},
		{"word/_rels/document.xml.rels", doc.rels.data.Bytes()},
	}
	parts = append(parts, doc.media...)
	remaining := maxOutput
	for _, item := range parts {
		if len(item.data) > remaining {
			return nil, ErrTooLarge
		}
		remaining -= len(item.data)
	}
	var output limitedBuffer
	archive := zip.NewWriter(&output)
	for _, item := range parts {
		method := uint16(zip.Deflate)
		if strings.HasPrefix(item.name, "word/media/") {
			method = zip.Store
		}
		entry, err := archive.CreateHeader(&zip.FileHeader{Name: item.name, Method: method})
		if err != nil {
			return nil, err
		}
		if _, err := entry.Write(item.data); err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return output.data.Bytes(), nil
}

const contentTypes = xml.Header + `<Types xmlns="` + packageNS + `content-types">` +
	`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
	`<Default Extension="xml" ContentType="application/xml"/>` +
	`<Default Extension="png" ContentType="image/png"/><Default Extension="jpg" ContentType="image/jpeg"/><Default Extension="gif" ContentType="image/gif"/>` +
	`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
	`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
	`<Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/></Types>`

const packageRelationships = xml.Header + `<Relationships xmlns="` + packageNS + `relationships">` +
	`<Relationship Id="rId1" Type="` + relsNS + `/officeDocument" Target="word/document.xml"/></Relationships>`

func (doc *document) relationship(kind, target string, external bool) int {
	doc.relationID++
	fmt.Fprintf(&doc.rels, `<Relationship Id="rId%d" Type="%s/%s" Target="`, doc.relationID, relsNS, kind)
	doc.rels.escape(target)
	doc.rels.write(`"`)
	if external {
		doc.rels.write(` TargetMode="External"`)
	}
	doc.rels.write(`/>`)
	return doc.relationID
}
