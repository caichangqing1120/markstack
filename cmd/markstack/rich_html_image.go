package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/vivaui/viva/gfx"

	"markstack/internal/workspace"
)

var errRichImageTooLarge = errors.New("HTML image dimensions are too large")

func decodeRichImage(data []byte) (gfx.Bitmap, error) {
	configuration, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return gfx.Bitmap{}, err
	}
	if configuration.Width <= 0 || configuration.Height <= 0 || configuration.Width > 4096 || configuration.Height > 4096 || configuration.Width > (8<<20)/configuration.Height {
		return gfx.Bitmap{}, errRichImageTooLarge
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return gfx.Bitmap{}, err
	}
	width, height := configuration.Width, configuration.Height
	pixels := make([]byte, width*height*4)
	bounds := decoded.Bounds()
	for row := range height {
		for column := range width {
			red, green, blue, alpha := decoded.At(bounds.Min.X+column, bounds.Min.Y+row).RGBA()
			offset := row*width*4 + column*4
			pixels[offset] = uint8(blue >> 8)
			pixels[offset+1] = uint8(green >> 8)
			pixels[offset+2] = uint8(red >> 8)
			pixels[offset+3] = uint8(alpha >> 8)
		}
	}
	digest := sha256.Sum256(data)
	id := binary.LittleEndian.Uint64(digest[:8])
	if id == 0 {
		id = 1
	}
	return gfx.Bitmap{Pix: pixels, Width: width, Height: height, Stride: width * 4, ID: id}, nil
}

func (state *model) richHTMLBitmap(relative string) (gfx.Bitmap, bool) {
	current := state.session.Current()
	if current == nil || current.Path == "" {
		return gfx.Bitmap{}, false
	}
	key := current.Path + "\x00" + relative
	if bitmap, ok := state.html.images[key]; ok {
		return bitmap, true
	}
	data, err := workspace.ReadAsset(state.session.Root, current.Path, current.SavedLocation(), relative)
	if err != nil {
		return gfx.Bitmap{}, false
	}
	bitmap, err := decodeRichImage(data)
	if err != nil {
		return gfx.Bitmap{}, false
	}
	if state.html.images == nil {
		state.html.images = make(map[string]gfx.Bitmap)
	}
	state.html.images[key] = bitmap
	return bitmap, true
}
