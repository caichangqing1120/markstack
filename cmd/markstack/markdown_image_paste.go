package main

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"os"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"

	"markstack/internal/imagehost"
	"markstack/internal/session"
	"markstack/internal/workspace"
)

var (
	imagePasteActionID  = controlTree.ID(page, "image-paste-action")
	imagePasteDiscardID = controlTree.ID(page, "image-paste-discard")
)

type pendingImagePaste struct {
	document   *session.Document
	editor     *text.Editor
	path       string
	source     string
	start, end int
	image      []byte
	remoteURL  string
	feedback   string
	scan       panelScan[string]
}

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
	if state.imageHost.paste != nil {
		return errors.New(state.local("请先处理上一次粘贴的图片", "Resolve the previous image paste first"))
	}
	if err := state.verifyMarkdownImagePaste(document, start, end); err != nil {
		return err
	}
	image, err := app.ClipboardImagePNG()
	if err != nil {
		return err
	}
	if _, err := decodeRichImage(image); err != nil {
		return err
	}
	if cfg := state.imageHost.cfg; cfg.Mode == imagehost.ModeHTTP || cfg.Mode == imagehost.ModeS3 {
		state.startImagePaste(cfg, document, start, end, image)
		return nil
	}
	return pasteLocalMarkdownImage(document, start, end, image)
}

func (state *model) verifyMarkdownImagePaste(document *session.Document, start, end int) error {
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
	return document.VerifyDisk()
}

func pasteLocalMarkdownImage(document *session.Document, start, end int, image []byte) error {
	source := document.Text()
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

func (state *model) startImagePaste(cfg imagehost.Config, document *session.Document, start, end int, image []byte) {
	bitmap := bytes.Clone(image)
	pending := &pendingImagePaste{
		document: document, editor: document.Editor, path: document.Path,
		source: document.Text(), start: start, end: end, image: bitmap,
		feedback: state.local("正在上传剪贴板图片…", "Uploading clipboard image…"),
	}
	state.imageHost.paste = pending
	upload := state.imageHost.upload
	if upload == nil {
		upload = imagehost.Upload
	}
	pending.scan.start(state.invalidator.Invalidate, func(ctx context.Context) (string, error) {
		return upload(ctx, cfg, "image.png", bitmap, "image/png")
	})
}

func (state *model) drainImagePaste() {
	pending := state.imageHost.paste
	if pending == nil {
		return
	}
	result, ready := pending.scan.receive()
	if !ready {
		return
	}
	if result.err == nil {
		result.err = imagehost.ValidateURL(result.data)
	}
	if result.err != nil {
		pending.feedback = state.imageHostErrorText(result.err)
		return
	}
	pending.remoteURL = result.data
	if !pending.originalPosition(state.session.Current()) || state.mode == readMode || state.settingsOpen || state.session.Pending() != nil || !state.pendingCompositionFocus().IsZero() {
		pending.feedback = state.local("图片已上传，尚未插入文档", "Image uploaded but not yet inserted")
		return
	}
	state.applyImagePaste()
}

func (pending *pendingImagePaste) originalPosition(document *session.Document) bool {
	return document != nil && document == pending.document && document.Editor == pending.editor && document.Path == pending.path && document.Editor.CommittedString() == pending.source
}

func (state *model) applyImagePaste() {
	pending := state.imageHost.paste
	document := state.session.Current()
	if pending == nil || pending.scan.ready != nil || document == nil || !state.pendingCompositionFocus().IsZero() {
		return
	}
	start, end := pending.start, pending.end
	if !pending.originalPosition(document) {
		if state.mode != editMode && state.mode != splitMode {
			return
		}
		start = byteOffsetOfRune(document.Text(), document.Editor.Caret())
		end = start
	}
	if err := state.verifyMarkdownImagePaste(document, start, end); err != nil {
		pending.feedback = err.Error()
		return
	}
	if pending.remoteURL == "" {
		if err := pasteLocalMarkdownImage(document, start, end, pending.image); err != nil {
			pending.feedback = state.local("无法保存本地图片：", "Couldn't save local image: ") + err.Error()
			return
		}
	} else {
		markup := "![image](<" + pending.remoteURL + ">)"
		if !document.FitsSavedSize(len(document.Text()) - (end - start) + len(markup)) {
			pending.feedback = state.local("插入图片后文档超过 16 MiB", "Inserting the image would exceed the 16 MiB document limit")
			return
		}
		document.Editor.BreakUndoGroup()
		err := document.EditRange(start, end, markup)
		document.Editor.BreakUndoGroup()
		if err != nil {
			pending.feedback = state.local("无法在此处插入图片", "Couldn't insert the image here")
			return
		}
	}
	state.clearImagePaste()
	state.message = ""
	state.rich, state.selection = renderedEditor{}, renderedSelection{}
	if state.mode == renderedMode || state.mode == readMode {
		state.mode = editMode
	}
	state.focusRequest = sourceID
}

func (state *model) clearImagePaste() {
	if pending := state.imageHost.paste; pending != nil {
		pending.scan.stop()
		state.imageHost.paste = nil
	}
}

func (state *model) paintImagePasteBanner(canvas *gfx.Canvas, theme ui.Theme, palette colors, bounds ui.Rect) ui.Rect {
	pending := state.imageHost.paste
	if pending == nil {
		return bounds
	}
	document := state.session.Current()
	original := pending.originalPosition(document)
	ready := pending.scan.ready == nil
	canInsert := document != nil && document.Path != "" && document.Kind == workspace.MarkdownKind && !document.ReadOnly() &&
		(original || state.mode == editMode || state.mode == splitMode) && state.pendingCompositionFocus().IsZero()
	local := state.local
	action := local("保存到本地", "Save Locally")
	if pending.remoteURL != "" {
		action = local("插入已上传图片", "Insert Uploaded Image")
	}
	if !original {
		if pending.remoteURL == "" {
			action = local("保存到此处", "Save Locally Here")
		} else {
			action = local("插入到此处", "Insert Here")
		}
	}
	label := pending.feedback
	dismiss := local("放弃", "Discard")
	if !ready {
		dismiss = local("取消", "Cancel")
	} else if !canInsert {
		label += local(" · 需在已保存的 Markdown 源码中插入", " · Open a saved Markdown source to insert")
	}
	height, labelWidth, actionsY := float32(36), bounds.Width-260, bounds.Y+4
	if bounds.Width < 580 && ready {
		height, labelWidth, actionsY = 64, bounds.Width-24, bounds.Y+32
	} else if !ready {
		labelWidth = bounds.Width - 94
	}
	canvas.Fill(ui.Rect{X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: height}, palette.bar)
	canvas.Fill(ui.Rect{X: bounds.X, Y: bounds.Y + height - 1, Width: bounds.Width, Height: 1}, palette.border)
	view := settingsView{state: state, canvas: canvas, theme: theme, palette: palette}
	view.label(label, ui.Rect{X: bounds.X + 12, Y: bounds.Y + 2, Width: labelWidth, Height: 30}, 12, gfx.WeightRegular, palette.ink)
	dismissBounds := ui.Rect{X: bounds.X + bounds.Width - 79, Y: actionsY, Width: 67, Height: 27}
	if ready {
		view.button(imagePasteActionID, action, action, ui.Rect{X: dismissBounds.X - 159, Y: actionsY, Width: 152, Height: 27}, true, canInsert, state.applyImagePaste)
	}
	view.button(imagePasteDiscardID, dismiss, dismiss, dismissBounds, false, true, state.clearImagePaste)
	bounds.Y += height
	bounds.Height = max(0, bounds.Height-height)
	return bounds
}

func writeClipboardPNGAsset(document *session.Document, image []byte) error {
	_, err := workspace.WritePNGAsset(document.Path, document.SavedLocation(), image)
	if errors.Is(err, workspace.ErrAssetContentConflict) {
		return errors.New("同名图片文件已存在，内容不同")
	}
	return err
}
