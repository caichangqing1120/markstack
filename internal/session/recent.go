package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"markstack/internal/workspace"
)

const (
	recentVersion  = 1
	maxRecentPaths = 8
	maxRecentBytes = 16 << 10
)

var (
	ErrCorruptRecent    = errors.New("recent history is corrupt")
	ErrUnsafeRecentPath = errors.New("recent history path is unsafe")
)

type RecentState struct {
	Version    int      `json:"version"`
	Workspaces []string `json:"workspaces"`
	Files      []string `json:"files"`
}

func (state *RecentState) AddWorkspace(path string) {
	if validRecentPath(path) {
		state.Version = recentVersion
		state.Workspaces = addRecentPath(state.Workspaces, path)
	}
}

func (state *RecentState) AddFile(path string) {
	if validRecentPath(path) && workspace.KindOf(path) != workspace.UnsupportedKind {
		state.Version = recentVersion
		state.Files = addRecentPath(state.Files, path)
	}
}

func (state *RecentState) Clear() {
	state.Version = recentVersion
	state.Workspaces = nil
	state.Files = nil
}

func addRecentPath(paths []string, path string) []string {
	next := make([]string, 0, min(maxRecentPaths, len(paths)+1))
	next = append(next, path)
	for _, candidate := range paths {
		if candidate != path && len(next) < maxRecentPaths {
			next = append(next, candidate)
		}
	}
	return next
}

func validRecentPath(path string) bool {
	return path != "" && len(path) <= 4096 && filepath.IsAbs(path) && filepath.Clean(path) == path && utf8.ValidString(path) && !strings.ContainsRune(path, 0)
}

func validateRecent(state RecentState) error {
	if state.Version != recentVersion || len(state.Workspaces) > maxRecentPaths || len(state.Files) > maxRecentPaths {
		return ErrCorruptRecent
	}
	for _, paths := range [][]string{state.Workspaces, state.Files} {
		seen := make(map[string]bool, len(paths))
		for _, path := range paths {
			if !validRecentPath(path) || seen[path] {
				return ErrCorruptRecent
			}
			seen[path] = true
		}
	}
	for _, path := range state.Files {
		if workspace.KindOf(path) == workspace.UnsupportedKind {
			return ErrCorruptRecent
		}
	}
	return nil
}

type RecentStore struct{ Path string }

func (store RecentStore) Load() (RecentState, error) {
	root, name, err := store.openRoot(false)
	if err != nil || root == nil {
		return RecentState{Version: recentVersion}, err
	}
	defer root.Close()
	return readRecent(root, name)
}

func (store RecentStore) Save(state RecentState) error {
	if state.Version == 0 {
		state.Version = recentVersion
	}
	if err := validateRecent(state); err != nil {
		return err
	}
	populated := len(state.Workspaces)+len(state.Files) > 0
	root, name, err := store.openRoot(populated)
	if err != nil || root == nil {
		return err
	}
	defer root.Close()
	if _, err := readRecent(root, name); err != nil {
		return err
	}
	if !populated {
		if err := root.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return syncRecoveryDirectory(root)
	}
	data, err := json.Marshal(state)
	if err != nil || len(data) > maxRecentBytes {
		return ErrCorruptRecent
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temporary := ".recent-" + hex.EncodeToString(nonce[:])
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
			return ErrUnsafeRecentPath
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := root.Rename(temporary, name); err != nil {
		return err
	}
	return syncRecoveryDirectory(root)
}

func (store RecentStore) openRoot(create bool) (*os.Root, string, error) {
	root, name, err := (RecoveryStore{Path: store.Path}).openRoot(create)
	if errors.Is(err, ErrUnsafeRecoveryPath) {
		return nil, "", ErrUnsafeRecentPath
	}
	return root, name, err
}

func readRecent(root *os.Root, name string) (RecentState, error) {
	empty := RecentState{Version: recentVersion}
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return empty, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return empty, ErrUnsafeRecentPath
	}
	if info.Size() > maxRecentBytes {
		return empty, ErrCorruptRecent
	}
	file, err := root.Open(name)
	if err != nil {
		return empty, fmt.Errorf("%w: %v", ErrUnsafeRecentPath, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxRecentBytes+1))
	if err != nil {
		return empty, err
	}
	if len(data) > maxRecentBytes {
		return empty, ErrCorruptRecent
	}
	var state RecentState
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil || decoder.Decode(new(any)) != io.EOF || validateRecent(state) != nil {
		return empty, ErrCorruptRecent
	}
	return state, nil
}
