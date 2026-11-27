package workspace

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

func TrashAvailable() bool { return true }

func Trash(root, relative string) (TrashResult, error) {
	parent, name, absolute, err := workspaceParent(root, relative)
	if err != nil {
		return TrashResult{}, err
	}
	defer parent.Close()
	entry, err := parent.Lstat(name)
	if err != nil {
		return TrashResult{}, err
	}
	if entry.Mode()&os.ModeSymlink != 0 || !entry.IsDir() && !entry.Mode().IsRegular() {
		return TrashResult{}, ErrOutsideWorkspace
	}
	data := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(data) {
		home, err := os.UserHomeDir()
		if err != nil {
			return TrashResult{}, err
		}
		data = filepath.Join(home, ".local", "share")
	}
	trash := filepath.Join(data, "Trash")
	if relative, err := filepath.Rel(absolute, trash); err == nil && filepath.IsLocal(relative) {
		return TrashResult{}, ErrOutsideWorkspace
	}
	if err := os.MkdirAll(data, 0700); err != nil {
		return TrashResult{}, err
	}
	store, err := privateTrashDirectory(trash)
	if err != nil {
		return TrashResult{}, err
	}
	defer store.Close()
	files, err := privateTrashDirectory(filepath.Join(trash, "files"))
	if err != nil {
		return TrashResult{}, err
	}
	defer files.Close()
	metadata, err := privateTrashDirectory(filepath.Join(trash, "info"))
	if err != nil {
		return TrashResult{}, err
	}
	defer metadata.Close()
	oldDir, err := parent.Open(".")
	if err != nil {
		return TrashResult{}, err
	}
	defer oldDir.Close()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return TrashResult{}, err
	}
	destination := "markstack-" + hex.EncodeToString(nonce[:])
	infoName := destination + ".trashinfo"
	infoFD, err := unix.Openat(int(metadata.Fd()), infoName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC, 0600)
	if err != nil {
		return TrashResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = unix.Unlinkat(int(metadata.Fd()), infoName, 0)
		}
	}()
	input := os.NewFile(uintptr(infoFD), infoName)
	encoded := (&url.URL{Path: absolute}).EscapedPath()
	_, writeErr := fmt.Fprintf(input, "[Trash Info]\nPath=%s\nDeletionDate=%s\n", encoded, time.Now().Format("2006-01-02T15:04:05"))
	if err := errors.Join(writeErr, input.Sync(), input.Close()); err != nil {
		return TrashResult{}, err
	}
	if current, err := parent.Lstat(name); err != nil || !os.SameFile(entry, current) {
		return TrashResult{}, ErrOutsideWorkspace
	}
	if err := renameNoReplace(oldDir, name, files, destination); err != nil {
		if errors.Is(err, unix.EXDEV) {
			return TrashResult{}, ErrTrashUnsupported
		}
		return TrashResult{}, err
	}
	committed = true
	return TrashResult{Original: absolute, Trashed: filepath.Join(trash, "files", destination)}, nil
}

func privateTrashDirectory(path string) (*os.File, error) {
	if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || int(stat.Uid) != os.Geteuid() || stat.Mode&0022 != 0 {
		_ = unix.Close(fd)
		return nil, ErrTrashUnsupported
	}
	return os.NewFile(uintptr(fd), path), nil
}
