package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/app"

	"markstack/internal/exportpdf"
	"markstack/internal/workspace"
)

func (state *model) exportActivePDF() {
	state.exportPDFWithPicker(app.SaveFile)
}

func (state *model) exportPDFWithPicker(pick func(app.SaveFileOptions) (string, error)) {
	current := state.session.Current()
	if current == nil || current.Kind != workspace.MarkdownKind && current.Kind != workspace.RichHTMLKind {
		state.message = "当前文档不支持 PDF 导出"
		return
	}
	if state.documentComposing(current) {
		state.message = "请先完成输入法组合，再导出 PDF"
		return
	}
	if current.Path != "" {
		if err := current.VerifyDisk(); err != nil {
			state.message = err.Error()
			return
		}
	}
	font, err := loadPDFFont()
	if err != nil {
		state.message = err.Error()
		return
	}
	loadImage := func(relative string) ([]byte, error) {
		if current.Path == "" {
			return nil, os.ErrNotExist
		}
		return workspace.ReadAsset(state.session.Root, current.Path, current.SavedLocation(), relative)
	}
	source := []byte(current.Editor.CommittedString())
	var data []byte
	if current.Kind == workspace.RichHTMLKind {
		data, err = exportpdf.RenderRichHTML(source, loadImage, font)
	} else {
		data, err = exportpdf.RenderMarkdown(source, loadImage, font)
	}
	if err != nil {
		state.message = err.Error()
		return
	}
	name := strings.TrimSuffix(current.Title, filepath.Ext(current.Title)) + ".pdf"
	path, err := pick(app.SaveFileOptions{Title: "导出 PDF", SuggestedName: name, DefaultExtension: "pdf"})
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
	if err := workspace.WriteNewPDF(path, data); err != nil {
		state.message = exportDestinationErrorText(err)
		return
	}
	state.message = "已导出 PDF: " + filepath.Base(path)
}

func loadPDFFont() ([]byte, error) {
	path := os.Getenv("MARKSTACK_PDF_FONT")
	if path == "" {
		return exportpdf.BundledFont()
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("PDF 字体不可用: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > 32<<20 {
		return nil, errors.New("PDF 字体不是有效的 TrueType 文件")
	}
	return os.ReadFile(path)
}
