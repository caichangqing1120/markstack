package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/app"

	"markstack/internal/exporthtml"
	"markstack/internal/workspace"
)

func (state *model) exportActiveHTML() {
	state.exportHTMLWithPicker(app.SaveFile)
}

func (state *model) exportHTMLWithPicker(pick func(app.SaveFileOptions) (string, error)) {
	current := state.session.Current()
	if current == nil || current.Kind != workspace.MarkdownKind && current.Kind != workspace.RichHTMLKind {
		state.message = "当前文档不支持 HTML 导出"
		return
	}
	if state.documentComposing(current) {
		state.message = "请先完成输入法组合，再导出 HTML"
		return
	}
	if current.Path != "" {
		if err := current.VerifyDisk(); err != nil {
			state.message = err.Error()
			return
		}
	}
	loadImage := func(relative string) ([]byte, error) {
		if current.Path == "" {
			return nil, os.ErrNotExist
		}
		return workspace.ReadAsset(state.session.Root, current.Path, current.SavedLocation(), relative)
	}
	source := []byte(current.Editor.CommittedString())
	var data []byte
	var err error
	if current.Kind == workspace.RichHTMLKind {
		data, err = exporthtml.RenderRichHTML(source, loadImage)
	} else {
		data, err = exporthtml.RenderMarkdown(source, loadImage)
	}
	if err != nil {
		state.message = err.Error()
		return
	}
	name := strings.TrimSuffix(current.Title, filepath.Ext(current.Title)) + ".html"
	path, err := pick(app.SaveFileOptions{Title: "导出 HTML", SuggestedName: name, DefaultExtension: "html"})
	if errors.Is(err, app.ErrCanceled) {
		return
	}
	if err != nil {
		state.message = err.Error()
		return
	}
	if current.Path != "" {
		if err := current.VerifyDisk(); err != nil {
			state.message = err.Error()
			return
		}
	}
	if err := workspace.WriteNew(path, data); err != nil {
		state.message = exportDestinationErrorText(err)
		return
	}
	state.message = "已导出 HTML: " + filepath.Base(path)
}

func exportDestinationErrorText(err error) string {
	if errors.Is(err, workspace.ErrConflict) {
		return "导出目标已存在，请选择其他文件名"
	}
	return err.Error()
}

func (state *model) richComposing() bool {
	_, composing := state.rich.input.Composition()
	return composing
}

func (state *model) htmlComposing() bool {
	_, composing := state.html.input.Composition()
	return composing
}
