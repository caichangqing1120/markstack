package session

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"unicode/utf8"
)

const maxPreferencesBytes = 4 << 10

const (
	MinFontSize     = 12
	DefaultFontSize = 16
	MaxFontSize     = 32
)

var (
	ErrCorruptPreferences    = errors.New("preferences are corrupt")
	ErrUnsafePreferencesPath = errors.New("preferences path is unsafe")
)

type Preferences struct {
	Version          int    `json:"version"`
	Dark             bool   `json:"dark"`
	AutoSave         bool   `json:"autoSave"`
	TypewriterMode   bool   `json:"typewriterMode,omitempty"`
	FocusMode        bool   `json:"focusMode,omitempty"`
	SourceSize       int    `json:"sourceSize"`
	ReadingSize      int    `json:"readingSize"`
	LineSpacing      string `json:"lineSpacing,omitempty"`
	EditorWidth      string `json:"editorWidth,omitempty"`
	ReadingTheme     string `json:"readingTheme"`
	Language         string `json:"language"`
	UITextSize       string `json:"uiTextSize"`
	ShowTabNumbers   bool   `json:"showTabNumbers"`
	AutoCheckUpdates bool   `json:"autoCheckUpdates"`
	PDFHeader        bool   `json:"pdf_header"`
	PDFFooter        bool   `json:"pdf_footer"`
	PDFPreset        string `json:"pdf_preset"`
}

func DefaultPreferences() Preferences {
	return Preferences{Version: 1, SourceSize: DefaultFontSize, ReadingSize: DefaultFontSize, LineSpacing: "standard", EditorWidth: "standard", ReadingTheme: "standard", Language: "system", UITextSize: "medium", PDFPreset: "standard"}
}

func validReadingTheme(theme string) bool {
	switch theme {
	case "standard", "soft", "github", "parchment", "academic", "nord":
		return true
	}
	return false
}

type PreferencesStore struct{ Path string }

func (store PreferencesStore) Directory() (string, error) {
	root, _, err := store.openRoot(true, nil)
	if err != nil {
		return "", err
	}
	defer root.Close()
	return root.Name(), nil
}

func (store PreferencesStore) Load() (Preferences, error) {
	root, name, err := store.openRoot(false, nil)
	if err != nil || root == nil {
		return DefaultPreferences(), err
	}
	defer root.Close()
	return readPreferences(root, name)
}

func (store PreferencesStore) Save(state Preferences) error { return store.save(state, nil) }

func (store PreferencesStore) save(state Preferences, beforeOpen func()) error {
	if !validPreferences(state) {
		return ErrCorruptPreferences
	}
	data, err := json.Marshal(state)
	if err != nil || len(data) > maxPreferencesBytes {
		return ErrCorruptPreferences
	}
	root, name, err := store.openRoot(true, beforeOpen)
	if err != nil {
		return err
	}
	defer root.Close()
	if _, err := readPreferences(root, name); err != nil {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temporary := ".preferences-" + hex.EncodeToString(nonce[:])
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
		if !info.Mode().IsRegular() || !privatePreferencesMode(info.Mode()) {
			return ErrUnsafePreferencesPath
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := root.Rename(temporary, name); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	return syncRecoveryDirectory(root)
}

func (store PreferencesStore) openRoot(create bool, beforeOpen func()) (*os.Root, string, error) {
	if store.Path == "" {
		return nil, "", ErrUnsafePreferencesPath
	}
	absolute, err := filepath.Abs(store.Path)
	if err != nil {
		return nil, "", err
	}
	directory, name := filepath.Dir(absolute), filepath.Base(absolute)
	if name == "." || name == string(filepath.Separator) {
		return nil, "", ErrUnsafePreferencesPath
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
	if !info.IsDir() || !privatePreferencesMode(info.Mode()) {
		return nil, "", ErrUnsafePreferencesPath
	}
	if beforeOpen != nil {
		beforeOpen()
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, "", ErrUnsafePreferencesPath
	}
	opened, err := root.Open(".")
	if err != nil {
		root.Close()
		return nil, "", ErrUnsafePreferencesPath
	}
	openedInfo, err := opened.Stat()
	opened.Close()
	if err != nil || !os.SameFile(info, openedInfo) {
		root.Close()
		return nil, "", ErrUnsafePreferencesPath
	}
	return root, name, nil
}

func readPreferences(root *os.Root, name string) (Preferences, error) {
	fallback := DefaultPreferences()
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return fallback, nil
	}
	if err != nil {
		return fallback, err
	}
	if !info.Mode().IsRegular() || !privatePreferencesMode(info.Mode()) {
		return fallback, ErrUnsafePreferencesPath
	}
	if info.Size() > maxPreferencesBytes {
		return fallback, ErrCorruptPreferences
	}
	file, err := root.Open(name)
	if err != nil {
		return fallback, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return fallback, ErrUnsafePreferencesPath
	}
	data, err := io.ReadAll(io.LimitReader(file, maxPreferencesBytes+1))
	if err != nil {
		return fallback, err
	}
	if len(data) > maxPreferencesBytes || !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return fallback, ErrCorruptPreferences
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	state := Preferences{Language: fallback.Language, UITextSize: fallback.UITextSize, PDFPreset: fallback.PDFPreset}
	if err := decoder.Decode(&state); err != nil || decoder.Decode(new(any)) != io.EOF {
		return fallback, ErrCorruptPreferences
	}
	if state.LineSpacing == "" {
		state.LineSpacing = fallback.LineSpacing
	}
	if state.EditorWidth == "" {
		state.EditorWidth = fallback.EditorWidth
	}
	if !validPreferences(state) {
		return fallback, ErrCorruptPreferences
	}
	return state, nil
}

func validPreferences(state Preferences) bool {
	return state.Version == 1 && validFontSize(state.SourceSize) && validFontSize(state.ReadingSize) &&
		validReadingTheme(state.ReadingTheme) &&
		(state.LineSpacing == "standard" || state.LineSpacing == "compact" || state.LineSpacing == "relaxed") &&
		(state.EditorWidth == "standard" || state.EditorWidth == "wide" || state.EditorWidth == "full") &&
		(state.Language == "system" || state.Language == "zh-CN" || state.Language == "en") &&
		(state.UITextSize == "small" || state.UITextSize == "medium" || state.UITextSize == "large") &&
		(state.PDFPreset == "standard" || state.PDFPreset == "compact" || state.PDFPreset == "academic")
}

func privatePreferencesMode(mode os.FileMode) bool {
	return runtime.GOOS == "windows" || mode.Perm()&0077 == 0
}

func (state Preferences) Valid() bool { return validPreferences(state) }

func validFontSize(size int) bool { return size >= MinFontSize && size <= MaxFontSize }
