package session

import (
	"errors"
	"fmt"

	"github.com/vivaui/viva/text"
)

type Template uint8

const (
	TemplateNote Template = iota + 1
	TemplateProposal
)

func (session *Session) NewFromTemplate(kind Template) (*Document, error) {
	if session.pending != nil {
		return nil, errors.New("another document transition is pending")
	}
	var title, body string
	switch kind {
	case TemplateNote:
		title = "笔记"
		body = "# 笔记\n\n## 摘要\n\n## 正文\n\n## 参考\n\n"
	case TemplateProposal:
		title = "方案"
		body = "# 方案\n\n## 背景\n\n## 目标\n\n## 实施步骤\n\n## 验收标准\n\n"
	default:
		return nil, fmt.Errorf("unsupported template: %d", kind)
	}
	document := session.NewDocument()
	document.Title = fmt.Sprintf("%s-%d.md", title, document.ID)
	document.Editor = text.NewEditor(body)
	document.Editor.SetSelection(text.Range{Start: 2, End: 4})
	return document, nil
}
