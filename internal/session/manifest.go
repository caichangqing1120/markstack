package session

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"unicode/utf8"

	"markstack/internal/workspace"
)

const (
	manifestVersion  = 1
	maxManifestBytes = 64 << 10
	maxManifestTabs  = 32
)

var (
	ErrCorruptManifest    = errors.New("session manifest is corrupt")
	ErrUnsafeManifestPath = errors.New("session manifest path is unsafe")
)

// SessionTab names one tab without storing its document body.
type SessionTab struct {
	ID    int    `json:"id"`
	Path  string `json:"path,omitempty"`
	Caret int    `json:"caret"`
	Dirty bool   `json:"dirty,omitempty"`
}

// SessionManifest keeps tab order and focus separately from dirty recovery content.
type SessionManifest struct {
	Version  int          `json:"version"`
	Root     string       `json:"root,omitempty"`
	ActiveID int          `json:"activeId,omitempty"`
	Tabs     []SessionTab `json:"tabs"`
}

type SessionStore struct{ Path string }

func (store SessionStore) Load() (SessionManifest, error) {
	root, name, err := store.openRoot(false)
	if err != nil || root == nil {
		return SessionManifest{Version: manifestVersion}, err
	}
	defer root.Close()
	return readManifest(root, name)
}

func (store SessionStore) Save(manifest SessionManifest) error {
	if manifest.Version == 0 {
		manifest.Version = manifestVersion
	}
	if err := validateManifest(manifest); err != nil {
		return err
	}
	populated := manifest.Root != "" || len(manifest.Tabs) > 0
	root, name, err := store.openRoot(populated)
	if err != nil || root == nil {
		return err
	}
	defer root.Close()
	if _, err := readManifest(root, name); err != nil {
		return err
	}
	if !populated {
		if err := root.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return syncRecoveryDirectory(root)
	}
	data, err := json.Marshal(manifest)
	if err != nil || len(data) > maxManifestBytes {
		return ErrCorruptManifest
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temporary := ".session-" + hex.EncodeToString(nonce[:])
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
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return ErrUnsafeManifestPath
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := root.Rename(temporary, name); err != nil {
		return err
	}
	return syncRecoveryDirectory(root)
}

func (store SessionStore) openRoot(create bool) (*os.Root, string, error) {
	root, name, err := (RecoveryStore{Path: store.Path}).openRoot(create)
	if errors.Is(err, ErrUnsafeRecoveryPath) {
		return nil, "", ErrUnsafeManifestPath
	}
	return root, name, err
}

func readManifest(root *os.Root, name string) (SessionManifest, error) {
	empty := SessionManifest{Version: manifestVersion}
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return empty, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return empty, ErrUnsafeManifestPath
	}
	if info.Size() > maxManifestBytes {
		return empty, ErrCorruptManifest
	}
	file, err := root.Open(name)
	if err != nil {
		return empty, ErrUnsafeManifestPath
	}
	defer file.Close()
	if current, err := root.Lstat(name); err != nil || !current.Mode().IsRegular() {
		return empty, ErrUnsafeManifestPath
	}
	data, err := io.ReadAll(io.LimitReader(file, maxManifestBytes+1))
	if err != nil {
		return empty, err
	}
	if len(data) > maxManifestBytes || !utf8.Valid(data) {
		return empty, ErrCorruptManifest
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest SessionManifest
	if err := decoder.Decode(&manifest); err != nil || decoder.Decode(new(any)) != io.EOF || validateManifest(manifest) != nil {
		return empty, ErrCorruptManifest
	}
	return manifest, nil
}

func validateManifest(manifest SessionManifest) error {
	if manifest.Version != manifestVersion || len(manifest.Tabs) > maxManifestTabs || manifest.Root != "" && !validRecentPath(manifest.Root) {
		return ErrCorruptManifest
	}
	seenIDs := make(map[int]bool, len(manifest.Tabs))
	seenPaths := make(map[string]bool, len(manifest.Tabs))
	active := manifest.ActiveID == 0
	for _, tab := range manifest.Tabs {
		if tab.ID <= 0 || tab.Caret < 0 || seenIDs[tab.ID] || tab.Path == "" && !tab.Dirty {
			return ErrCorruptManifest
		}
		seenIDs[tab.ID] = true
		active = active || tab.ID == manifest.ActiveID
		if tab.Path == "" {
			continue
		}
		if !validRecentPath(tab.Path) || workspace.KindOf(tab.Path) == workspace.UnsupportedKind || seenPaths[tab.Path] {
			return ErrCorruptManifest
		}
		seenPaths[tab.Path] = true
	}
	if !active {
		return ErrCorruptManifest
	}
	return nil
}
