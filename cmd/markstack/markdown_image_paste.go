package main

import (
	"errors"
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
	event.Apply(current.Editor)
	return app.TextStateOf(current.Editor), nil
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
	if _, err := workspace.WritePNGAsset(document.Path, document.SavedLocation(), image); err != nil {
		return err
	}
	if err := document.VerifyDisk(); err != nil {
		return err
	}
	document.Editor.BreakUndoGroup()
	defer document.Editor.BreakUndoGroup()
	return document.EditRange(start, end, markup)
}
