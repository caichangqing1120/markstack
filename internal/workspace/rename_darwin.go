//go:build darwin

package workspace

import (
	"os"

	"golang.org/x/sys/unix"
)

func renameNoReplace(oldDir *os.File, oldName string, newDir *os.File, newName string) error {
	return unix.RenameatxNp(int(oldDir.Fd()), oldName, int(newDir.Fd()), newName, unix.RENAME_EXCL)
}

func RenameAvailable() bool { return true }
