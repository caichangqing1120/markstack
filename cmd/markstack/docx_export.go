package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/app"

	"markstack/internal/exportdocx"
	"markstack/internal/workspace"
)

func (state *model) exportActiveDOCX() {
	state.exportDOCXWithPicker(app.SaveFile)
}

func (state *model) exportDOCXWithPicker(pick func(app.SaveFileOptions) (string, error)) {
	current := state.session.Current()
	if current == nil || current.Kind != workspace.MarkdownKind && current.Kind != workspace.RichHTMLKind {
		state.message = "当前文档不支持 DOCX 导出"
		return
	}
	if state.documentComposing(current) {
		state.message = "请先完成输入法组合，再导出 DOCX"
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
	render := exportdocx.RenderMarkdown
	if current.Kind == workspace.RichHTMLKind {
		render = exportdocx.RenderRichHTML
	}
	data, err := render([]byte(current.Editor.CommittedString()), loadImage)
	if err != nil {
		state.message = err.Error()
		return
	}
	name := strings.TrimSuffix(current.Title, filepath.Ext(current.Title)) + ".docx"
	path, err := pick(app.SaveFileOptions{Title: "导出 DOCX", SuggestedName: name, DefaultExtension: "docx"})
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
	if err := workspace.WriteNewDOCX(path, data); err != nil {
		state.message = exportDestinationErrorText(err)
		return
	}
	state.message = "已导出 DOCX: " + filepath.Base(path)
}
