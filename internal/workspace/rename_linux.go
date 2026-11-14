//go:build linux

package workspace

import (
	"os"

	"golang.org/x/sys/unix"
)

func renameNoReplace(oldDir *os.File, oldName string, newDir *os.File, newName string) error {
	return unix.Renameat2(int(oldDir.Fd()), oldName, int(newDir.Fd()), newName, unix.RENAME_NOREPLACE)
}

func RenameAvailable() bool { return true }
