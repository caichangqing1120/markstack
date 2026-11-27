//go:build !darwin && !linux && !windows

package workspace

import "os"

func renameNoReplace(_ *os.File, _ string, _ *os.File, _ string) error {
	return ErrUnsupportedRename
}

func RenameAvailable() bool { return false }
