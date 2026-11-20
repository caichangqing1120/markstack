package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/text"

	"markstack/internal/workspace"
)

var ErrPathRequired = errors.New("save as path required")
var ErrCompositionPending = errors.New("finish text composition before saving")

type Decision uint8

const (
	Save Decision = iota + 1
	Discard
	Cancel
)

type Action uint8

const (
	CloseTab Action = iota + 1
	SwitchWorkspace
	Quit
)

type Document struct {
	ID               int
	Path             string
	Title            string
	Kind             workspace.DocumentKind
	Editor           *text.Editor
	saved            string
	snapshot         workspace.Snapshot
	recoveryConflict bool
	recoveryExists   bool
	recoveryBaseline [32]byte
	recoveryLocation string
}

func (document *Document) Text() string { return document.Editor.String() }
func (document *Document) Dirty() bool  { return document.Editor.CommittedString() != document.saved }
func (document *Document) Composing() bool {
	_, composing := document.Editor.Composition()
	return composing
}
func (document *Document) SavedLocation() string { return document.snapshot.Location() }
func (document *Document) VerifyDisk() error     { return workspace.Verify(document.snapshot) }
func (document *Document) Newline() string {
	if document.snapshot.Newline == "\r\n" {
		return "\r\n"
	}
	return "\n"
}

func (document *Document) FitsSavedSize(size int) bool {
	if document.snapshot.BOM {
		size += 3
	}
	return size <= workspace.MaxDocumentSize
}

func (document *Document) Replace(body string) {
	document.Editor.SetSelection(text.Range{Start: 0, End: document.Editor.RuneCount()})
	document.Editor.Insert(body)
}

func (document *Document) EditRange(start, end int, replacement string) error {
	value := document.Text()
	if start < 0 || end < start || end > len(value) || !utf8.ValidString(value[:start]) || !utf8.ValidString(value[:end]) || !utf8.ValidString(replacement) {
		return errors.New("invalid UTF-8 edit range")
	}
	replacement = strings.ReplaceAll(strings.ReplaceAll(replacement, "\r\n", "\n"), "\r", "\n")
	if document.Newline() == "\r\n" {
		replacement = strings.ReplaceAll(replacement, "\n", "\r\n")
	}
	document.Editor.SetSelection(text.Range{
		Start: utf8.RuneCountInString(value[:start]), End: utf8.RuneCountInString(value[:end]),
	})
	document.Editor.Insert(replacement)
	return nil
}

type Confirmation struct {
	Action   Action
	Document *Document
}

type transition struct {
	action               Action
	index                int
	root                 string
	nodes                []workspace.Node
	approved             map[int]bool
	pendingCompositions  map[*Document]bool
	projectedComposition func(*Document) bool
}

type Session struct {
	Root    string
	Nodes   []workspace.Node
	Tabs    []*Document
	Active  int
	nextID  int
	pending *transition
}

func New() *Session {
	session := &Session{}
	session.NewDocument()
	return session
}

func (session *Session) Current() *Document {
	if session.Active < 0 || session.Active >= len(session.Tabs) {
		return nil
	}
	return session.Tabs[session.Active]
}

func (session *Session) NewDocument() *Document {
	session.nextID++
	document := &Document{ID: session.nextID, Title: fmt.Sprintf("未命名-%d.md", session.nextID), Kind: workspace.MarkdownKind, Editor: text.NewEditor("")}
	session.Tabs = append(session.Tabs, document)
	session.Active = len(session.Tabs) - 1
	return document
}

func (session *Session) NewRichHTML() *Document {
	document := session.NewDocument()
	document.Title = fmt.Sprintf("未命名-%d.mh", document.ID)
	document.Kind = workspace.RichHTMLKind
	document.Editor = text.NewEditor("<p><br></p>")
	return document
}

func (session *Session) NewPlainText() *Document {
	document := session.NewDocument()
	document.Title = fmt.Sprintf("未命名-%d.txt", document.ID)
	document.Kind = workspace.PlainKind
	return document
}

func (session *Session) Open(path string) (*Document, error) {
	if err := session.OpenMany([]string{path}); err != nil {
		return nil, err
	}
	return session.Current(), nil
}

func (session *Session) OpenMany(paths []string) error {
	if len(paths) == 0 {
		return errors.New("no documents to open")
	}
	if session.pending != nil {
		return errors.New("another document transition is pending")
	}
	exact := make(map[string]*Document, len(session.Tabs)+len(paths))
	known := make(map[string]*Document, len(session.Tabs)+len(paths))
	for _, document := range session.Tabs {
		if document.Path != "" {
			exact[document.Path] = document
			if location := document.snapshot.Location(); location != "" {
				known[location] = document
			}
		}
	}
	var additions []*Document
	var last *Document
	for _, candidate := range paths {
		path, err := filepath.Abs(candidate)
		if err != nil {
			return err
		}
		kind := workspace.KindOf(path)
		if kind == workspace.UnsupportedKind {
			return fmt.Errorf("unsupported document: %s", path)
		}
		if document := exact[path]; document != nil {
			last = document
			continue
		}
		location, err := workspace.CanonicalLocation(path)
		if err != nil {
			return err
		}
		if document := known[location]; document != nil {
			last = document
			continue
		}
		snapshot, err := workspace.Load(path)
		if err != nil {
			return err
		}
		editor := text.NewEditor(snapshot.Text)
		editor.MoveTo(0, false)
		document := &Document{Path: path, Title: filepath.Base(path), Kind: kind, Editor: editor, saved: snapshot.Text, snapshot: snapshot}
		exact[path] = document
		known[snapshot.Location()] = document
		additions = append(additions, document)
		last = document
	}
	for _, document := range additions {
		session.nextID++
		document.ID = session.nextID
		session.Tabs = append(session.Tabs, document)
	}
	for index, document := range session.Tabs {
		if document == last {
			session.Active = index
			break
		}
	}
	return nil
}

// Manifest snapshots tab positions without storing any document body.
func (session *Session) Manifest() SessionManifest {
	manifest := SessionManifest{Version: manifestVersion, Root: session.Root}
	for _, document := range session.Tabs {
		if document.Path == "" && !document.Dirty() {
			continue
		}
		caret := max(0, min(document.Editor.Caret(), utf8.RuneCountInString(document.Editor.CommittedString())))
		manifest.Tabs = append(manifest.Tabs, SessionTab{ID: document.ID, Path: document.Path, Caret: caret, Dirty: document.Dirty()})
		if session.Current() == document {
			manifest.ActiveID = document.ID
		}
	}
	return manifest
}

// RestoreManifest merges clean disk tabs with already restored dirty drafts.
func (session *Session) RestoreManifest(manifest SessionManifest) ([]string, error) {
	if err := validateManifest(manifest); err != nil {
		return nil, err
	}
	recovered := make(map[int]*Document)
	recoveredLocations := make(map[string]*Document)
	previousActive := session.Current()
	for _, document := range session.Tabs {
		if !document.Dirty() {
			continue
		}
		recovered[document.ID] = document
		if location := document.recoveryLocation; location != "" {
			recoveredLocations[location] = document
		} else if location := document.snapshot.Location(); location != "" {
			recoveredLocations[location] = document
		}
	}
	tabs := make([]*Document, 0, len(manifest.Tabs)+len(recovered))
	used := make(map[*Document]bool, len(recovered))
	locations := make(map[string]bool, len(manifest.Tabs))
	carets := make(map[*Document]int, len(recovered))
	var warnings []string
	active, nextID := -1, 0
	for _, entry := range manifest.Tabs {
		location := ""
		if entry.Path != "" {
			if canonical, err := workspace.CanonicalLocation(entry.Path); err == nil {
				location = canonical
			}
		}
		if location != "" && locations[location] {
			warnings = append(warnings, entry.Path)
			continue
		}
		document := recovered[entry.ID]
		if document == nil && location != "" {
			document = recoveredLocations[location]
		}
		if document == nil && entry.Dirty {
			warnings = append(warnings, entry.Path)
			continue
		}
		if document == nil {
			loaded, err := workspace.Load(entry.Path)
			if err != nil {
				warnings = append(warnings, entry.Path)
				continue
			}
			document = &Document{
				ID: entry.ID, Path: entry.Path, Title: filepath.Base(entry.Path), Kind: workspace.KindOf(entry.Path),
				Editor: text.NewEditor(loaded.Text), saved: loaded.Text, snapshot: loaded,
			}
			location = loaded.Location()
		}
		if used[document] {
			warnings = append(warnings, entry.Path)
			continue
		}
		used[document] = true
		if location != "" {
			locations[location] = true
		}
		carets[document] = max(0, min(entry.Caret, document.Editor.RuneCount()))
		tabs = append(tabs, document)
		if entry.ID == manifest.ActiveID {
			active = len(tabs) - 1
		}
		nextID = max(nextID, document.ID)
	}
	for _, document := range session.Tabs {
		if !document.Dirty() || used[document] {
			continue
		}
		tabs = append(tabs, document)
		nextID = max(nextID, document.ID)
	}
	if active < 0 {
		for index, document := range tabs {
			if document == previousActive {
				active = index
				break
			}
		}
	}
	if active < 0 {
		active = 0
	}
	root, nodes := manifest.Root, []workspace.Node(nil)
	if session.Root != "" && len(recovered) > 0 {
		root = session.Root
	}
	if root != "" {
		requested := root
		canonical, err := filepath.EvalSymlinks(root)
		if err == nil {
			root = canonical
			nodes, err = workspace.Scan(root)
		}
		if err != nil {
			warnings = append(warnings, requested)
			root = ""
		}
	}
	if len(tabs) == 0 {
		nextID++
		tabs = append(tabs, &Document{ID: nextID, Title: fmt.Sprintf("未命名-%d.md", nextID), Kind: workspace.MarkdownKind, Editor: text.NewEditor("")})
	}
	for document, caret := range carets {
		document.Editor.MoveTo(caret, false)
	}
	session.Root, session.Nodes = root, nodes
	session.Tabs, session.Active, session.nextID = tabs, active, nextID
	session.pending = nil
	return warnings, nil
}

func (session *Session) Save(index int, path string) error {
	if index < 0 || index >= len(session.Tabs) {
		return errors.New("document is not open")
	}
	document := session.Tabs[index]
	if session.hasPendingComposition(document) {
		return ErrCompositionPending
	}
	if path == "" {
		path = document.Path
	}
	if path == "" {
		return ErrPathRequired
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if kind := workspace.KindOf(path); kind == workspace.UnsupportedKind || kind != document.Kind {
		return fmt.Errorf("cannot save %s as a different document format: %s", document.Title, path)
	}
	if document.recoveryConflict && document.snapshot.SameLocation(path) {
		return workspace.ErrConflict
	}
	for otherIndex, other := range session.Tabs {
		if otherIndex != index && other.Path == path {
			return errors.New("file is already open in another tab")
		}
	}
	snapshot := document.snapshot
	if !snapshot.SameLocation(path) {
		snapshot = workspace.Snapshot{Path: path, BOM: document.snapshot.BOM, Newline: document.snapshot.Newline}
		if _, err := os.Lstat(path); err == nil {
			return workspace.ErrConflict
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	next, err := workspace.Write(snapshot, snapshot.Encode(document.Text()))
	if err != nil {
		return err
	}
	if snapshot.Newline == "\r\n" && !strings.ContainsAny(next.Text, "\r\n") {
		next.Newline = snapshot.Newline
	}
	document.snapshot = next
	document.recoveryConflict = false
	document.recoveryExists = false
	document.recoveryBaseline = [32]byte{}
	document.recoveryLocation = ""
	document.Path = path
	document.Title = filepath.Base(path)
	document.Kind = workspace.KindOf(path)
	document.saved = document.Text()
	if session.Root != "" {
		if nodes, err := workspace.Scan(session.Root); err == nil {
			session.Nodes = nodes
		}
	}
	return nil
}

func (session *Session) hasPendingComposition(document *Document) bool {
	return document.Composing() || session.pending != nil && session.pending.projectedComposition != nil && session.pending.projectedComposition(document)
}

func (session *Session) Pending() *Confirmation {
	if session.pending == nil {
		return nil
	}
	for _, document := range session.Tabs {
		if !document.Dirty() && !session.hasPendingComposition(document) && !session.pending.pendingCompositions[document] || session.pending.approved[document.ID] {
			continue
		}
		if session.pending.action != CloseTab || session.Tabs[session.pending.index] == document {
			return &Confirmation{Action: session.pending.action, Document: document}
		}
	}
	return nil
}

func (session *Session) RequestClose(index int) bool {
	if index < 0 || index >= len(session.Tabs) || session.pending != nil {
		return false
	}
	if session.Tabs[index].Dirty() || session.Tabs[index].Composing() {
		session.pending = &transition{action: CloseTab, index: index, approved: make(map[int]bool)}
		return false
	}
	session.close(index)
	return true
}

func (session *Session) close(index int) {
	session.Tabs = append(session.Tabs[:index], session.Tabs[index+1:]...)
	if session.Active >= len(session.Tabs) {
		session.Active = len(session.Tabs) - 1
	} else if session.Active > index {
		session.Active--
	}
	if len(session.Tabs) == 0 {
		session.NewDocument()
	}
}

func (session *Session) RequestWorkspace(root string) (bool, error) {
	if session.pending != nil {
		return false, errors.New("another transition is pending")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return false, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return false, err
	}
	nodes, err := workspace.Scan(root)
	if err != nil {
		return false, err
	}
	if root == session.Root {
		session.Nodes = nodes
		return true, nil
	}
	transition := &transition{action: SwitchWorkspace, root: root, nodes: nodes, approved: make(map[int]bool)}
	session.pending = transition
	if session.Pending() != nil {
		return false, nil
	}
	session.apply()
	return true, nil
}

func (session *Session) RequestQuit() bool {
	return session.RequestQuitWithPending(nil)
}

func (session *Session) RequestQuitWithPending(projectedComposition func(*Document) bool) bool {
	if session.pending != nil {
		return false
	}
	next := &transition{action: Quit, approved: make(map[int]bool), projectedComposition: projectedComposition}
	if projectedComposition != nil {
		next.pendingCompositions = make(map[*Document]bool)
		for _, document := range session.Tabs {
			if projectedComposition(document) {
				next.pendingCompositions[document] = true
			}
		}
	}
	session.pending = next
	if session.Pending() != nil {
		return false
	}
	session.pending = nil
	return true
}

func (session *Session) Resolve(decision Decision, saveAsPath string) (bool, error) {
	confirmation := session.Pending()
	if confirmation == nil {
		return false, errors.New("no confirmation is pending")
	}
	if decision == Cancel {
		session.pending = nil
		return false, nil
	}
	if decision == Save {
		index := session.indexOf(confirmation.Document)
		if err := session.Save(index, saveAsPath); err != nil {
			return false, err
		}
	} else if decision != Discard {
		return false, errors.New("invalid confirmation choice")
	}
	session.pending.approved[confirmation.Document.ID] = true
	if session.Pending() != nil {
		return false, nil
	}
	session.apply()
	return true, nil
}

func (session *Session) indexOf(document *Document) int {
	for index, candidate := range session.Tabs {
		if candidate == document {
			return index
		}
	}
	return -1
}

func (session *Session) apply() {
	transition := session.pending
	session.pending = nil
	switch transition.action {
	case CloseTab:
		session.close(transition.index)
	case SwitchWorkspace:
		session.Root = transition.root
		session.Nodes = transition.nodes
		session.Tabs = nil
		session.nextID = 0
		session.NewDocument()
	}
}
