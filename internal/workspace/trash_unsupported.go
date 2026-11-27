//go:build (darwin && !cgo) || (!darwin && !windows && !linux)

package workspace

func Trash(string, string) (TrashResult, error) {
	return TrashResult{}, ErrTrashUnsupported
}

func TrashAvailable() bool { return false }
