//go:build (darwin && !cgo) || (!darwin && !windows && !linux)

package workspace

import "os"

func Trash(string, string, ...os.FileInfo) (TrashResult, error) {
	return TrashResult{}, ErrTrashUnsupported
}

func TrashAvailable() bool { return false }
