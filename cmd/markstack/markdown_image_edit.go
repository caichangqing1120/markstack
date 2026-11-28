package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/text"
	"markstack/internal/markdownmedia"
	"markstack/internal/session"
	"markstack/internal/workspace"
)

type imageRenameEdit struct {
	before, after [32]byte
	documentPath  string
	from, to      string
	file          os.FileInfo
}

func markdownImageScale(title string) float32 {
	return float32(markdownmedia.WidthPercent(title)) / 100
}

func (state *model) activeImageDocument() *session.Document {
	current, inspector := state.session.Current(), &state.imageInspector
	if current == nil || current.Kind != workspace.MarkdownKind || !inspector.open || inspector.ref.Destination == "" || inspector.documentID != current.ID || inspector.path != current.Path || inspector.source != current.Text() || current.Composing() {
		return nil
	}
	return current
}

func (state *model) setMarkdownImageWidth(percent int) error {
	current := state.activeImageDocument()
	if current == nil || percent != 50 && percent != 75 && percent != 100 {
		return errors.New("图片引用已变化")
	}
	inspector := &state.imageInspector
	if markdownmedia.WidthPercent(inspector.ref.Title) == percent {
		return nil
	}
	patch := inspector.ref.SetWidth(current.Text(), percent)
	return state.changeImageSource(current, []markdownmedia.Patch{patch}, inspector.ref.Destination, nil)
}

func (state *model) renameMarkdownImage(name string) error {
	current := state.activeImageDocument()
	if current == nil {
		return errors.New("图片引用已变化")
	}
	from := state.imageInspector.ref.Destination
	to, err := workspace.ImageAssetName(from, name)
	if err != nil || from == to {
		return err
	}
	if err := current.VerifyDisk(); err != nil {
		return err
	}
	if state.session.Root != "" && current.Path != "" {
		rootPath := state.session.Root
		if resolved, err := filepath.EvalSymlinks(rootPath); err == nil {
			rootPath = resolved
		}
		docPath := current.Path
		if resolved, err := filepath.EvalSymlinks(docPath); err == nil {
			docPath = resolved
		}
		if docRel, relErr := filepath.Rel(rootPath, docPath); relErr == nil && docRel != ".." && !strings.HasPrefix(docRel, ".."+string(filepath.Separator)) {
			docSlash := filepath.ToSlash(docRel)
			targetRel := workspace.RelativeReferencePath(docSlash, from)
			if targetRel != "" {
				others, incomplete, scanErr := markdownmedia.SharedImageReferences(context.Background(), rootPath, docSlash, targetRel)
				if scanErr != nil || incomplete {
					return errors.New("工作区图片引用检查未完成，暂无法重命名")
				}
				if len(others) > 0 {
					return fmt.Errorf("该图片同时被 %s 引用，暂不支持直接重命名", others[0])
				}
			}
		}
	}
	var patches []markdownmedia.Patch
	for _, reference := range markdownmedia.ImageReferences([]byte(current.Text())) {
		if reference.Destination == from {
			patches = append(patches, reference.Rename(to))
		}
	}
	if len(patches) == 0 {
		return errors.New("未找到可更新的图片引用")
	}
	edit := imageRenameEdit{documentPath: current.Path, from: from, to: to}
	return state.changeImageSource(current, patches, to, &edit)
}

func (state *model) changeImageSource(current *session.Document, patches []markdownmedia.Patch, destination string, rename *imageRenameEdit) error {
	before := current.Text()
	sort.Slice(patches, func(first, second int) bool { return patches[first].Start < patches[second].Start })
	start, end := patches[0].Start, patches[len(patches)-1].End
	var replacement strings.Builder
	position := start
	imageStart := state.imageInspector.ref.Start
	for _, patch := range patches {
		if patch.Start < position || patch.End < patch.Start || patch.End > len(before) {
			return errors.New("无效的图片引用区间")
		}
		replacement.WriteString(before[position:patch.Start])
		replacement.WriteString(patch.Text)
		position = patch.End
		if patch.End <= state.imageInspector.ref.Start {
			imageStart += len(patch.Text) - (patch.End - patch.Start)
		}
	}
	after := before[:start] + replacement.String() + before[end:]
	if !current.FitsSavedSize(len(after)) {
		return errors.New("更新图片后文档超过 16 MiB")
	}
	var updated markdownmedia.ImageReference
	for _, ref := range markdownmedia.ImageReferences([]byte(after)) {
		if ref.Start == imageStart && ref.Destination == destination {
			updated = ref
			break
		}
	}
	if updated.Destination == "" {
		return errors.New("无法验证更新后的图片引用")
	}
	selection := current.Editor.Selection()
	if current.Editor.Caret() == selection.Start {
		selection.Start, selection.End = selection.End, selection.Start
	}
	if rename != nil {
		file, err := workspace.RenameImageAsset(state.session.Root, current.Path, current.SavedLocation(), rename.from, rename.to, nil)
		if err != nil {
			return err
		}
		rename.file = file
	}
	caret := min(selection.End, utf8.RuneCountInString(after))
	err := current.EditRange(start, end, replacement.String(), session.RangeEditSelection{Before: selection, After: text.Range{Start: caret, End: caret}})
	if err != nil {
		if rename != nil {
			_, rollback := workspace.RenameImageAsset(state.session.Root, current.Path, current.SavedLocation(), rename.to, rename.from, rename.file)
			return errors.Join(err, rollback)
		}
		return err
	}
	if rename != nil {
		rename.before, rename.after = sha256.Sum256([]byte(before)), sha256.Sum256([]byte(after))
		if state.imageRenames == nil {
			state.imageRenames = make(map[int][]imageRenameEdit)
		}
		state.imageRenames[current.ID] = appendRenderedHistory(state.imageRenames[current.ID], *rename)
		state.recordPendingImageRename(current.ID, *rename)
	}
	state.moveInactiveRenderedHistory(current, before, 1, true)
	state.imageInspector.source, state.imageInspector.ref = current.Text(), updated
	state.invalidator.Invalidate()
	return nil
}

func (state *model) stepMarkdownEdit(current *session.Document, undo bool) bool {
	move, restore := current.Editor.Redo, current.Editor.Undo
	if undo {
		move, restore = current.Editor.Undo, current.Editor.Redo
	}
	edits := state.imageRenames[current.ID]
	if len(edits) == 0 {
		return move()
	}
	before := sha256.Sum256([]byte(current.Text()))
	if !move() {
		return false
	}
	after := sha256.Sum256([]byte(current.Text()))
	for index := len(edits) - 1; index >= 0; index-- {
		edit := edits[index]
		have, want, from, to := edit.before, edit.after, edit.from, edit.to
		if undo {
			have, want, from, to = want, have, to, from
		}
		if before != have || after != want || current.Path != edit.documentPath {
			continue
		}
		file, err := workspace.RenameImageAsset(state.session.Root, current.Path, current.SavedLocation(), from, to, edit.file)
		if err != nil {
			restore()
			state.message = err.Error()
			return false
		}
		state.recordPendingImageRename(current.ID, imageRenameEdit{
			documentPath: current.Path,
			from:         from,
			to:           to,
			file:         file,
		})
		break
	}
	return true
}

func (state *model) recordPendingImageRename(documentID int, edit imageRenameEdit) {
	if state.pendingImageRenames == nil {
		state.pendingImageRenames = make(map[int][]imageRenameEdit)
	}
	pending := state.pendingImageRenames[documentID]
	if n := len(pending); n > 0 {
		last := pending[n-1]
		if last.documentPath == edit.documentPath && last.from == edit.to && last.to == edit.from {
			pending = pending[:n-1]
			if len(pending) == 0 {
				delete(state.pendingImageRenames, documentID)
			} else {
				state.pendingImageRenames[documentID] = pending
			}
			return
		}
	}
	state.pendingImageRenames[documentID] = append(pending, edit)
}

func (state *model) commitPendingImageRenames(documentID int) {
	delete(state.pendingImageRenames, documentID)
}

func (state *model) rollbackPendingImageRenames(document *session.Document) {
	if document == nil {
		return
	}
	pending := state.pendingImageRenames[document.ID]
	delete(state.pendingImageRenames, document.ID)
	delete(state.imageRenames, document.ID)
	for index := len(pending) - 1; index >= 0; index-- {
		edit := pending[index]
		if edit.documentPath == "" || document.Path != edit.documentPath {
			continue
		}
		_, _ = workspace.RenameImageAsset(state.session.Root, document.Path, document.SavedLocation(), edit.to, edit.from, edit.file)
	}
}
