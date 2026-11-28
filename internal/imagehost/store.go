package imagehost

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

const maxConfigBytes = 32 << 10

var (
	ErrUnsafeStore  = errors.New("image host configuration path or permissions are unsafe")
	ErrCorruptStore = errors.New("image host configuration is invalid or too large")
	ErrReadStore    = errors.New("could not read the private image host configuration")
	ErrWriteStore   = errors.New("could not write the private image host configuration")
)

type ConfigStore struct{ Path string }

func DefaultStore() (ConfigStore, error) {
	if path := os.Getenv("MARKSTACK_IMAGEHOST_PATH"); path != "" {
		return ConfigStore{Path: path}, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return ConfigStore{}, ErrReadStore
	}
	return ConfigStore{Path: filepath.Join(base, "MarkStack", "imagehost.json")}, nil
}

func (store ConfigStore) Load() (Config, error) {
	root, name, err := store.openRoot(false)
	if err != nil || root == nil {
		return DefaultConfig(), err
	}
	defer root.Close()
	return readConfig(root, name)
}

func (store ConfigStore) Save(cfg Config) error {
	cfg = cfg.normalized()
	if err := cfg.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil || len(data) > maxConfigBytes || !utf8.Valid(data) {
		return ErrCorruptStore
	}
	root, name, err := store.openRoot(true)
	if err != nil {
		return err
	}
	defer root.Close()
	if _, err := readConfig(root, name); err != nil {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return ErrWriteStore
	}
	temporary := ".imagehost-" + hex.EncodeToString(nonce[:])
	file, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return ErrWriteStore
	}
	defer root.Remove(temporary)
	writeErr := writePrivateConfig(file, data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return ErrWriteStore
	}
	if info, err := root.Lstat(name); err == nil {
		if !info.Mode().IsRegular() || !privateMode(info.Mode()) {
			return ErrUnsafeStore
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return ErrWriteStore
	}
	if err := root.Rename(temporary, name); err != nil {
		return ErrWriteStore
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := root.Open(".")
	if err != nil {
		return ErrWriteStore
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return ErrWriteStore
	}
	return nil
}

func writePrivateConfig(file *os.File, data []byte) error {
	if err := file.Chmod(0600); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

func (store ConfigStore) openRoot(create bool) (*os.Root, string, error) {
	if store.Path == "" {
		return nil, "", ErrUnsafeStore
	}
	absolute, err := filepath.Abs(store.Path)
	if err != nil {
		return nil, "", ErrUnsafeStore
	}
	directory, name := filepath.Dir(absolute), filepath.Base(absolute)
	if directory == absolute || !filepath.IsLocal(name) {
		return nil, "", ErrUnsafeStore
	}
	if create {
		if err := os.MkdirAll(directory, 0700); err != nil {
			return nil, "", ErrWriteStore
		}
	}
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) && !create {
		return nil, name, nil
	}
	if err != nil {
		return nil, "", ErrReadStore
	}
	if !info.IsDir() || !privateMode(info.Mode()) {
		return nil, "", ErrUnsafeStore
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, "", ErrUnsafeStore
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) || !privateMode(opened.Mode()) {
		root.Close()
		return nil, "", ErrUnsafeStore
	}
	return root, name, nil
}

func readConfig(root *os.Root, name string) (Config, error) {
	fallback := DefaultConfig()
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return fallback, nil
	}
	if err != nil {
		return fallback, ErrReadStore
	}
	if !info.Mode().IsRegular() || !privateMode(info.Mode()) {
		return fallback, ErrUnsafeStore
	}
	if info.Size() > maxConfigBytes {
		return fallback, ErrCorruptStore
	}
	file, err := root.Open(name)
	if err != nil {
		return fallback, ErrReadStore
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || !privateMode(opened.Mode()) {
		return fallback, ErrUnsafeStore
	}
	data, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return fallback, ErrReadStore
	}
	if len(data) > maxConfigBytes || !utf8.Valid(data) || !bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		return fallback, ErrCorruptStore
	}
	cfg := DefaultConfig()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil || decoder.Decode(new(any)) != io.EOF || cfg.Validate() != nil {
		return fallback, ErrCorruptStore
	}
	return cfg.normalized(), nil
}

func privateMode(mode os.FileMode) bool {
	return runtime.GOOS == "windows" || mode.Perm()&0077 == 0
}
