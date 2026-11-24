package session

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/text"

	"markstack/internal/workspace"
)

const (
	recoveryVersion  = 1
	maxRecoveryBytes = 64 << 20
	maxRecoveryDraft = 32
	maxDraftBytes    = 16 << 20
)

var (
	ErrCorruptRecovery    = errors.New("invalid recovery journal")
	ErrUnsafeRecoveryPath = errors.New("unsafe recovery journal path")
)

type RecoveryDraft struct {
	ID           int               `json:"id"`
	Path         string            `json:"path,omitempty"`
	Title        string            `json:"title"`
	Body         string            `json:"body"`
	Saved        string            `json:"saved"`
	BOM          bool              `json:"bom,omitempty"`
	Newline      string            `json:"newline"`
	DiskExists   bool              `json:"diskExists,omitempty"`
	DiskSHA256   [sha256.Size]byte `json:"diskSha256"`
	DiskLocation string            `json:"diskLocation,omitempty"`
}

type RecoverySnapshot struct {
	Version  int             `json:"version"`
	Root     string          `json:"root,omitempty"`
	ActiveID int             `json:"activeId,omitempty"`
	Drafts   []RecoveryDraft `json:"drafts"`
}

type RecoveryStore struct{ Path string }

func (store RecoveryStore) Load() (RecoverySnapshot, error) {
	root, name, err := store.openRoot(false)
	if err != nil || root == nil {
		return RecoverySnapshot{Version: recoveryVersion}, err
	}
	defer root.Close()
	snapshot, _, err := readRecovery(root, name)
	return snapshot, err
}

func (store RecoveryStore) Save(snapshot RecoverySnapshot) error {
	if snapshot.Version == 0 {
		snapshot.Version = recoveryVersion
	}
	if err := validateRecovery(snapshot); err != nil {
		return err
	}
	root, name, err := store.openRoot(len(snapshot.Drafts) > 0)
	if err != nil || root == nil {
		return err
	}
	defer root.Close()
	if _, _, err := readRecovery(root, name); err != nil {
		return err
	}
	if len(snapshot.Drafts) == 0 {
		if err := root.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return syncRecoveryDirectory(root)
	}
	if recoveryTextExceedsWireLimit(snapshot.Drafts, maxRecoveryBytes) {
		return fmt.Errorf("%w: journal exceeds size limit", ErrCorruptRecovery)
	}
	var journal bytes.Buffer
	encoder := json.NewEncoder(&journal)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(snapshot); err != nil {
		return fmt.Errorf("%w: %v", ErrCorruptRecovery, err)
	}
	data := bytes.TrimSuffix(journal.Bytes(), []byte("\n"))
	if len(data) > maxRecoveryBytes {
		return fmt.Errorf("%w: journal exceeds size limit", ErrCorruptRecovery)
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temporary := ".recovery-" + hex.EncodeToString(nonce[:])
	file, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(temporary)
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if info, err := root.Lstat(name); err == nil {
		if !info.Mode().IsRegular() {
			return ErrUnsafeRecoveryPath
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := root.Rename(temporary, name); err != nil {
		return err
	}
	return syncRecoveryDirectory(root)
}

func recoveryTextExceedsWireLimit(drafts []RecoveryDraft, limit int) bool {
	remaining := limit
	for _, draft := range drafts {
		remaining -= len(draft.Body) + len(draft.Saved)
	}
	if remaining < 0 {
		return true
	}
	if limit-remaining <= limit/6 {
		return false
	}
	if limit-remaining <= limit/2 && !recoveryTextHasLongEscapes(drafts) {
		return false
	}
	for _, draft := range drafts {
		for _, value := range [...]string{draft.Body, draft.Saved} {
			for _, symbol := range value {
				switch symbol {
				case '"', '\\', '\b', '\f', '\n', '\r', '\t':
					remaining--
				case '\u2028', '\u2029':
					remaining -= 3
				default:
					if symbol < ' ' {
						remaining -= 5
					}
				}
				if remaining < 0 {
					return true
				}
			}
		}
	}
	return false
}

func recoveryTextHasLongEscapes(drafts []RecoveryDraft) bool {
	const controls = "\x00\x01\x02\x03\x04\x05\x06\x07\x0b\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f"
	for _, draft := range drafts {
		if strings.ContainsAny(draft.Body, controls) || strings.ContainsAny(draft.Saved, controls) {
			return true
		}
	}
	return false
}

func (store RecoveryStore) openRoot(create bool) (*os.Root, string, error) {
	if store.Path == "" {
		return nil, "", ErrUnsafeRecoveryPath
	}
	absolute, err := filepath.Abs(store.Path)
	if err != nil {
		return nil, "", err
	}
	directory, name := filepath.Dir(absolute), filepath.Base(absolute)
	if name == "." || name == string(filepath.Separator) {
		return nil, "", ErrUnsafeRecoveryPath
	}
	if create {
		if err := os.MkdirAll(directory, 0700); err != nil {
			return nil, "", err
		}
	}
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) && !create {
		return nil, name, nil
	}
	if err != nil {
		return nil, "", err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, "", ErrUnsafeRecoveryPath
	}
	root, err := os.OpenRoot(directory)
	return root, name, err
}

func readRecovery(root *os.Root, name string) (RecoverySnapshot, bool, error) {
	empty := RecoverySnapshot{Version: recoveryVersion}
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return empty, false, nil
	}
	if err != nil {
		return empty, false, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return empty, false, ErrUnsafeRecoveryPath
	}
	if info.Size() > maxRecoveryBytes {
		return empty, false, ErrCorruptRecovery
	}
	file, err := root.Open(name)
	if err != nil {
		return empty, false, err
	}
	defer file.Close()
	if current, err := root.Lstat(name); err != nil || !current.Mode().IsRegular() {
		return empty, false, ErrUnsafeRecoveryPath
	}
	data, err := readRecoveryBytes(file, info.Size())
	if err != nil {
		return empty, false, err
	}
	if len(data) > maxRecoveryBytes || !utf8.Valid(data) {
		return empty, false, ErrCorruptRecovery
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var snapshot RecoverySnapshot
	if err := decoder.Decode(&snapshot); err != nil || decoder.Decode(new(any)) != io.EOF {
		return empty, false, ErrCorruptRecovery
	}
	if err := validateRecovery(snapshot); err != nil {
		return empty, false, err
	}
	return snapshot, true, nil
}

func readRecoveryBytes(file io.Reader, size int64) ([]byte, error) {
	const limit = maxRecoveryBytes + 1
	journal := make([]byte, 0, min(int(size)+bytes.MinRead, limit))
	var extra [1]byte
	for len(journal) < limit {
		space := journal[len(journal):cap(journal)]
		if len(space) == 0 {
			space = extra[:]
		}
		count, err := file.Read(space)
		if err != nil && err != io.EOF {
			return nil, err
		}
		if len(journal)+count > cap(journal) {
			grown := make([]byte, len(journal), min(2*cap(journal), limit))
			copy(grown, journal)
			journal = append(grown, extra[0])
		} else {
			journal = journal[:len(journal)+count]
		}
		if err == io.EOF {
			break
		}
	}
	return journal, nil
}

func validateRecovery(snapshot RecoverySnapshot) error {
	if snapshot.Version != recoveryVersion || len(snapshot.Drafts) > maxRecoveryDraft || snapshot.Root != "" && !filepath.IsAbs(snapshot.Root) {
		return ErrCorruptRecovery
	}
	seen := make(map[int]bool, len(snapshot.Drafts))
	seenPaths := make(map[string]bool, len(snapshot.Drafts))
	active := snapshot.ActiveID == 0
	total := 0
	for _, draft := range snapshot.Drafts {
		total += len(draft.Body) + len(draft.Saved)
		if !validRecoveryDraft(draft) || seen[draft.ID] || total > maxRecoveryBytes {
			return ErrCorruptRecovery
		}
		seen[draft.ID] = true
		if draft.Path != "" {
			if seenPaths[draft.DiskLocation] {
				return ErrCorruptRecovery
			}
			seenPaths[draft.DiskLocation] = true
		}
		active = active || snapshot.ActiveID == draft.ID
	}
	if !active {
		return ErrCorruptRecovery
	}
	return nil
}

func validRecoveryDraft(draft RecoveryDraft) bool {
	if draft.ID <= 0 || draft.Title == "" || draft.Body == draft.Saved || len(draft.Body) > maxDraftBytes || len(draft.Saved) > maxDraftBytes || !utf8.ValidString(draft.Body) || !utf8.ValidString(draft.Saved) || strings.IndexByte(draft.Body, 0) >= 0 || strings.IndexByte(draft.Saved, 0) >= 0 {
		return false
	}
	if draft.Newline != "\n" && draft.Newline != "\r\n" {
		return false
	}
	if draft.Path == "" {
		return !draft.DiskExists && draft.DiskLocation == ""
	}
	return filepath.IsAbs(draft.Path) && draft.DiskExists && filepath.IsAbs(draft.DiskLocation)
}

func syncRecoveryDirectory(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func (session *Session) RecoverySnapshot() RecoverySnapshot {
	snapshot := RecoverySnapshot{Version: recoveryVersion, Root: session.Root}
	for _, document := range session.Tabs {
		if !document.Dirty() {
			continue
		}
		draft := RecoveryDraft{
			ID: document.ID, Path: document.Path, Title: document.Title,
			Body: document.Editor.CommittedString(), Saved: document.saved, BOM: document.snapshot.BOM, Newline: document.Newline(),
		}
		if document.Path != "" {
			if document.recoveryConflict {
				draft.DiskExists, draft.DiskSHA256, draft.DiskLocation = document.recoveryExists, document.recoveryBaseline, document.recoveryLocation
			} else if document.snapshot.Exists() {
				draft.DiskExists, draft.DiskSHA256, draft.DiskLocation = true, sha256.Sum256(document.snapshot.Bytes), document.snapshot.Location()
			}
		}
		snapshot.Drafts = append(snapshot.Drafts, draft)
		if session.Current() == document {
			snapshot.ActiveID = document.ID
		}
	}
	if snapshot.ActiveID == 0 && len(snapshot.Drafts) > 0 {
		snapshot.ActiveID = snapshot.Drafts[0].ID
	}
	return snapshot
}

func (session *Session) RestoreRecovery(snapshot RecoverySnapshot) error {
	if err := validateRecovery(snapshot); err != nil {
		return err
	}
	if len(snapshot.Drafts) == 0 {
		return nil
	}
	tabs := make([]*Document, 0, len(snapshot.Drafts))
	active, nextID := 0, 0
	for index, draft := range snapshot.Drafts {
		editor := text.NewEditor(draft.Body)
		editor.MoveTo(0, false)
		kindPath := draft.Path
		if kindPath == "" {
			kindPath = draft.Title
		}
		kind := workspace.KindOf(kindPath)
		if kind == workspace.UnsupportedKind {
			return ErrCorruptRecovery
		}
		document := &Document{
			ID: draft.ID, Path: draft.Path, Title: draft.Title, Kind: kind, Editor: editor, saved: draft.Saved,
			snapshot:       workspace.Snapshot{Path: draft.Path, BOM: draft.BOM, Newline: draft.Newline},
			recoveryExists: draft.DiskExists, recoveryBaseline: draft.DiskSHA256, recoveryLocation: draft.DiskLocation,
		}
		if draft.Path != "" {
			location, locationErr := workspace.CanonicalLocation(draft.Path)
			if locationErr == nil && draft.DiskExists && location == draft.DiskLocation {
				current, err := workspace.Load(draft.Path)
				if err == nil && sha256.Sum256(current.Bytes) == draft.DiskSHA256 && current.Location() == draft.DiskLocation {
					if current.Text != draft.Saved || current.BOM != draft.BOM || current.Newline != draft.Newline {
						return ErrCorruptRecovery
					}
					document.snapshot = current
				} else {
					document.recoveryConflict = true
				}
			} else {
				document.recoveryConflict = true
			}
		}
		tabs = append(tabs, document)
		if draft.ID == snapshot.ActiveID {
			active = index
		}
		nextID = max(nextID, draft.ID)
	}
	var nodes []workspace.Node
	root := snapshot.Root
	if root != "" {
		if info, err := os.Lstat(root); err == nil && info.Mode().IsDir() {
			nodes, err = workspace.Scan(root)
			if err != nil {
				root = ""
			}
		} else {
			root = ""
		}
	}
	session.Root, session.Nodes = root, nodes
	session.Tabs, session.Active, session.nextID = tabs, active, nextID
	session.pending = nil
	return nil
}
