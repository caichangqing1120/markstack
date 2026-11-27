package session

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"markstack/internal/workspace"
)

var ErrDirtyDocument = errors.New("save or discard open changes before moving to Trash")

func (session *Session) RenameNode(oldPath, newName string) error {
	if session.pending != nil {
		return errors.New("another document transition is pending")
	}
	if session.Root == "" {
		return workspace.ErrOutsideWorkspace
	}
	if err := workspace.ValidateName(newName); err != nil {
		return err
	}
	root, err := filepath.EvalSymlinks(session.Root)
	if err != nil {
		return err
	}
	if info, err := os.Lstat(session.Root); err != nil || !info.Mode().IsDir() {
		return workspace.ErrOutsideWorkspace
	}
	oldLocation, err := workspace.CanonicalLocation(oldPath)
	if err != nil {
		return err
	}
	relative, within := relativePath(root, oldLocation)
	if !within || relative == "." {
		return workspace.ErrOutsideWorkspace
	}
	info, err := os.Lstat(oldLocation)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() && !info.IsDir() {
		return workspace.ErrOutsideWorkspace
	}
	targetRelative := filepath.Join(filepath.Dir(relative), newName)
	if relative == targetRelative {
		return nil
	}
	target := filepath.Join(root, targetRelative)
	if !info.IsDir() && workspace.KindOf(oldLocation) != workspace.KindOf(target) {
		return errors.New("cannot rename a document to a different format")
	}
	type affectedTab struct {
		document *Document
		path     string
	}
	var affected []affectedTab
	for _, document := range session.Tabs {
		if document.Path == "" {
			continue
		}
		location, err := workspace.CanonicalLocation(document.Path)
		if err != nil {
			return err
		}
		suffix, inside := relativePath(oldLocation, location)
		if !inside || !info.IsDir() && suffix != "." {
			continue
		}
		if err := workspace.Verify(document.snapshot); err != nil {
			return err
		}
		newPath := target
		if suffix != "." {
			newPath = filepath.Join(target, suffix)
		}
		affected = append(affected, affectedTab{document: document, path: newPath})
	}
	if err := workspace.Rename(root, relative, targetRelative); err != nil {
		return err
	}
	var postError error
	for _, tab := range affected {
		original := tab.document.snapshot
		next, err := workspace.LoadView(tab.path)
		tab.document.Path, tab.document.Title = tab.path, filepath.Base(tab.path)
		if err != nil || next.Preview != original.Preview || !bytes.Equal(next.Bytes, original.Bytes) || original.Preview && !original.Revision().SameFile(next.Revision()) {
			tab.document.snapshot = workspace.Snapshot{Path: tab.path, BOM: original.BOM, Newline: original.Newline, Preview: original.Preview}
			tab.document.recoveryConflict = true
			tab.document.recoveryExists = true
			tab.document.recoveryBaseline = sha256.Sum256(original.Bytes)
			tab.document.recoveryLocation = original.Location()
			postError = workspace.ErrConflict
			continue
		}
		if !strings.ContainsAny(next.Text, "\r\n") && (original.Newline == "\n" || original.Newline == "\r\n") {
			// Line-free bytes cannot reveal the newline; keep the preference this tab already knew.
			next.Newline = original.Newline
		}
		tab.document.snapshot = next
		tab.document.recoveryConflict = false
		tab.document.recoveryLocation = ""
	}
	nodes, err := workspace.Scan(root)
	if err == nil {
		session.Nodes = nodes
	}
	if postError != nil {
		return postError
	}
	return err
}

func relativePath(root, path string) (string, bool) {
	relative, err := filepath.Rel(root, path)
	return relative, err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func (session *Session) TrashNode(path string) (workspace.TrashResult, error) {
	if session.pending != nil || session.Root == "" {
		return workspace.TrashResult{}, workspace.ErrOutsideWorkspace
	}
	root, err := filepath.EvalSymlinks(session.Root)
	if err != nil {
		return workspace.TrashResult{}, err
	}
	if info, err := os.Lstat(session.Root); err != nil || !info.Mode().IsDir() {
		return workspace.TrashResult{}, workspace.ErrOutsideWorkspace
	}
	location, err := workspace.CanonicalLocation(path)
	if err != nil {
		return workspace.TrashResult{}, err
	}
	relative, inside := relativePath(root, location)
	if !inside || relative == "." {
		return workspace.TrashResult{}, workspace.ErrOutsideWorkspace
	}
	info, err := os.Lstat(location)
	if err != nil {
		return workspace.TrashResult{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() && !info.IsDir() {
		return workspace.TrashResult{}, workspace.ErrOutsideWorkspace
	}
	var affected []int
	for index, document := range session.Tabs {
		if document.Path == "" {
			continue
		}
		documentLocation, err := workspace.CanonicalLocation(document.Path)
		if err != nil {
			return workspace.TrashResult{}, err
		}
		suffix, child := relativePath(location, documentLocation)
		if !child || !info.IsDir() && suffix != "." {
			continue
		}
		if document.Dirty() {
			return workspace.TrashResult{}, ErrDirtyDocument
		}
		if err := workspace.Verify(document.snapshot); err != nil {
			return workspace.TrashResult{}, err
		}
		affected = append(affected, index)
	}
	result, err := workspace.Trash(root, relative)
	if err != nil {
		return workspace.TrashResult{}, err
	}
	for index := len(affected) - 1; index >= 0; index-- {
		session.close(affected[index])
	}
	if nodes, err := workspace.Scan(root); err == nil {
		session.Nodes = nodes
	}
	return result, nil
}
