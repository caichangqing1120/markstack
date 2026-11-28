package exportdocx

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"path"
	"strings"
	"unicode"
)

const (
	emuPerTwip  = 635
	emuPerPixel = 9525
	pageHeight  = 12960
)

type picture struct {
	id, width, height int
}

func (doc *document) image(source string) (result picture) {
	if decoded, err := url.PathUnescape(source); err == nil {
		source = decoded
	}
	for strings.HasPrefix(source, "./") {
		source = strings.TrimPrefix(source, "./")
	}
	if doc.loadImage == nil || !safeImagePath(source) {
		return result
	}
	if cached, exists := doc.images[source]; exists {
		return cached
	}
	defer func() { doc.images[source] = result }()
	data, err := doc.loadImage(source)
	if err != nil || len(data) > 8<<20 {
		return result
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 4096 || config.Height > 4096 || config.Width > (8<<20)/config.Height {
		return result
	}
	switch format {
	case "jpeg":
		format = "jpg"
	case "png", "gif":
	default:
		return result
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return result
	}
	if len(data) > maxOutput-doc.mediaSize {
		doc.body.err = ErrTooLarge
		return result
	}
	name := fmt.Sprintf("media/image%d.%s", len(doc.media)+1, format)
	doc.media = append(doc.media, part{"word/" + name, bytes.Clone(data)})
	doc.mediaSize += len(data)
	return picture{id: doc.relationship("image", name, false), width: config.Width, height: config.Height}
}

func safeImagePath(source string) bool {
	return source != "" && source != "." && source != ".." && !strings.HasPrefix(source, "../") && !path.IsAbs(source) &&
		path.Clean(source) == source && !strings.ContainsAny(source, `\:`) && strings.IndexFunc(source, unicode.IsControl) < 0
}

func (doc *document) draw(image picture, maxWidth int, alt string) {
	width, height := int64(image.width)*emuPerPixel, int64(image.height)*emuPerPixel
	if limit := int64(maxWidth) * emuPerTwip; width > limit {
		width, height = limit, max(1, height*limit/width)
	}
	if limit := int64(pageHeight) * emuPerTwip; height > limit {
		width, height = max(1, width*limit/height), limit
	}
	doc.drawingID++
	out := &doc.body
	fmt.Fprintf(out, `<w:r><w:drawing><wp:inline><wp:extent cx="%d" cy="%d"/><wp:docPr id="%d" name="Picture %d" descr="`, width, height, doc.drawingID, doc.drawingID)
	out.escape(alt)
	out.write(`"/><wp:cNvGraphicFramePr><a:graphicFrameLocks noChangeAspect="1"/></wp:cNvGraphicFramePr>` +
		`<a:graphic><a:graphicData uri="` + drawingNS + `picture"><pic:pic><pic:nvPicPr>`)
	fmt.Fprintf(out, `<pic:cNvPr id="%d" name="Picture %d"/><pic:cNvPicPr/></pic:nvPicPr>`, doc.drawingID, doc.drawingID)
	fmt.Fprintf(out, `<pic:blipFill><a:blip r:embed="rId%d"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill>`, image.id)
	fmt.Fprintf(out, `<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm>`, width, height)
	out.write(`<a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`)
}
