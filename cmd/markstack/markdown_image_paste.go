package main

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"io"
	"os"
	"unicode/utf8"

	"github.com/vivaui/viva/app"

	"markstack/internal/session"
	"markstack/internal/workspace"
)

func (state *model) onMarkdownSourceTextEvent(event app.TextEvent) (app.TextState, error) {
	current := state.session.Current()
	if current == nil {
		return app.TextState{}, nil
	}
	before := current.Editor.CommittedString()
	historyTraversal := event.Kind == app.TextCommandEvent && (event.Command == app.TextUndo || event.Command == app.TextRedo)
	defer func() {
		if !historyTraversal && current.Editor.CommittedString() != before {
			state.rich = renderedEditor{}
			state.selection = renderedSelection{}
		}
	}()
	if event.Kind == app.TextPasteNoText {
		selection := current.Editor.Selection()
		value := current.Text()
		start := byteOffsetOfRune(value, selection.Start)
		end := byteOffsetOfRune(value, selection.End)
		if err := state.pasteMarkdownClipboardImage(current, start, end); err != nil {
			state.message = err.Error()
		} else {
			state.message = ""
		}
		state.invalidator.Invalidate()
		result := app.TextStateOf(current.Editor)
		result.Redraw = true
		return result, nil
	}
	if historyTraversal && !current.Composing() && len(state.imageRenames[current.ID]) > 0 {
		state.stepMarkdownEdit(current, event.Command == app.TextUndo)
		return app.TextStateOf(current.Editor), nil
	}
	event.Apply(current.Editor)
	return app.TextStateOf(current.Editor), nil
}

func (state *model) pickMarkdownImage(document *session.Document) (string, error) {
	if document.Path == "" {
		return "", errors.New("请先保存 Markdown 文件，再插入图片")
	}
	if err := document.VerifyDisk(); err != nil {
		return "", err
	}
	paths, err := app.OpenFiles(app.OpenFileOptions{Title: "插入本地图片", Filters: []app.FileFilter{{Name: "图片", Pattern: "*.png;*.jpg;*.jpeg;*.gif"}}})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	info, err := os.Stat(paths[0])
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return "", workspace.ErrAssetTooLarge
	}
	file, err := os.Open(paths[0])
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (8<<20)+1))
	if err != nil {
		return "", err
	}
	if len(data) > 8<<20 {
		return "", workspace.ErrAssetTooLarge
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > 4096 || config.Height > 4096 || config.Width > (8<<20)/config.Height {
		return "", workspace.ErrAssetTooLarge
	}
	if format != "png" {
		bitmap, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return "", err
		}
		var output bytes.Buffer
		if err := png.Encode(&output, bitmap); err != nil {
			return "", err
		}
		data = output.Bytes()
	}
	return workspace.WritePNGAsset(document.Path, document.SavedLocation(), data)
}

func (state *model) pasteMarkdownClipboardImage(document *session.Document, start, end int) error {
	if document == nil || document != state.session.Current() || document.Kind != workspace.MarkdownKind {
		return errors.New("当前文档不是 Markdown")
	}
	if document.Path == "" {
		return errors.New("请先保存 Markdown 文件，再粘贴图片")
	}
	source := document.Text()
	if start < 0 || end < start || end > len(source) || !utf8.ValidString(source[:start]) || !utf8.ValidString(source[:end]) {
		return errors.New("图片插入位置无效")
	}
	if _, composing := document.Editor.Composition(); composing {
		return errors.New("请先完成输入法组合，再粘贴图片")
	}
	if err := document.VerifyDisk(); err != nil {
		return err
	}
	image, err := app.ClipboardImagePNG()
	if err != nil {
		return err
	}
	if _, err := decodeRichImage(image); err != nil {
		return err
	}
	markup := "![图片](" + workspace.PNGAssetPath(image) + ")"
	if !document.FitsSavedSize(len(source) - (end - start) + len(markup)) {
		return errors.New("插入图片后文档超过 16 MiB")
	}
	if err := writeClipboardPNGAsset(document, image); err != nil {
		return err
	}
	if err := document.VerifyDisk(); err != nil {
		return err
	}
	document.Editor.BreakUndoGroup()
	defer document.Editor.BreakUndoGroup()
	return document.EditRange(start, end, markup)
}

func writeClipboardPNGAsset(document *session.Document, image []byte) error {
	_, err := workspace.WritePNGAsset(document.Path, document.SavedLocation(), image)
	if errors.Is(err, workspace.ErrAssetContentConflict) {
		return errors.New("同名图片文件已存在，内容不同")
	}
	return err
}
