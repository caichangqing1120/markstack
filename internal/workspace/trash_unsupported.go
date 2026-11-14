//go:build !darwin || !cgo

package workspace

func Trash(string, string) (TrashResult, error) {
	return TrashResult{}, ErrTrashUnsupported
}

func TrashAvailable() bool { return false }
