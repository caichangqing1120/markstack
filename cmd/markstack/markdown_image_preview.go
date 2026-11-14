package main

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"

	"markstack/internal/markdownmedia"
	"markstack/internal/session"
	"markstack/internal/workspace"
)

type markdownPreviewImage struct {
	bitmap gfx.Bitmap
	alt    string
}

type inlinePreviewPart struct {
	text   string
	bitmap gfx.Bitmap
	alt    string
}

func (state *model) markdownPreviewImages(current *session.Document) map[int]markdownPreviewImage {
	source := current.Text()
	if state.markdownMediaID == current.ID && state.markdownMediaPath == current.Path && state.markdownMediaSource == source && state.markdownMediaBlocks != nil {
		return state.markdownMediaBlocks
	}
	state.markdownMediaID = current.ID
	state.markdownMediaPath = current.Path
	state.markdownMediaSource = source
	state.markdownMediaBlocks = make(map[int]markdownPreviewImage)
	state.markdownInlineBlocks = make(map[int][]inlinePreviewPart)
	loaded := make(map[string]gfx.Bitmap)
	load := func(destination string) (gfx.Bitmap, bool) {
		if current.Path == "" {
			return gfx.Bitmap{}, false
		}
		if bitmap, ok := loaded[destination]; ok {
			return bitmap, true
		}
		data, err := workspace.ReadAsset(state.session.Root, current.Path, current.SavedLocation(), destination)
		if err != nil {
			return gfx.Bitmap{}, false
		}
		bitmap, err := decodeRichImage(data)
		if err != nil {
			return gfx.Bitmap{}, false
		}
		loaded[destination] = bitmap
		return bitmap, true
	}
	for _, candidate := range markdownmedia.StandaloneImages([]byte(source)) {
		for index, block := range state.parsed.Blocks {
			if block.Kind != markdown.Paragraph || block.Start != candidate.Start || block.End != candidate.End {
				continue
			}
			if bitmap, ok := load(candidate.Destination); ok {
				state.markdownMediaBlocks[index] = markdownPreviewImage{bitmap: bitmap, alt: candidate.Alt}
			}
			break
		}
	}
	for _, candidate := range markdownmedia.MixedParagraphs([]byte(source)) {
		for index, block := range state.parsed.Blocks {
			if block.Kind != markdown.Paragraph || block.Start != candidate.Start || block.End != candidate.End {
				continue
			}
			parts := make([]inlinePreviewPart, 0, len(candidate.Parts))
			for _, part := range candidate.Parts {
				if part.Destination == "" {
					parts = append(parts, inlinePreviewPart{text: part.Text})
					continue
				}
				if bitmap, ok := load(part.Destination); ok {
					parts = append(parts, inlinePreviewPart{bitmap: bitmap, alt: part.Alt})
				} else {
					alt := part.Alt
					if alt == "" {
						alt = "图"
					}
					parts = append(parts, inlinePreviewPart{text: "▧ 图片不可用: " + alt, alt: alt})
				}
			}
			state.markdownInlineBlocks[index] = parts
			break
		}
	}
	return state.markdownMediaBlocks
}
