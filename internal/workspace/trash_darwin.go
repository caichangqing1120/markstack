//go:build darwin && cgo

package workspace

/*
#cgo LDFLAGS: -framework Foundation
#include <stdlib.h>
char *markstack_trash(const char *path, char **failure);
*/
import "C"

import (
	"fmt"
	"os"
	"unsafe"
)

func Trash(root, relative string) (TrashResult, error) {
	parent, name, absolute, err := workspaceParent(root, relative)
	if err != nil {
		return TrashResult{}, err
	}
	defer parent.Close()
	info, err := parent.Lstat(name)
	if err != nil {
		return TrashResult{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() && !info.IsDir() {
		return TrashResult{}, ErrOutsideWorkspace
	}
	current, err := os.Lstat(absolute)
	if err != nil || !os.SameFile(info, current) {
		return TrashResult{}, ErrOutsideWorkspace
	}
	path := C.CString(absolute)
	defer C.free(unsafe.Pointer(path))
	var failure *C.char
	result := C.markstack_trash(path, &failure)
	if failure != nil {
		defer C.free(unsafe.Pointer(failure))
	}
	if result == nil {
		if failure != nil {
			return TrashResult{}, fmt.Errorf("move to Trash: %s", C.GoString(failure))
		}
		return TrashResult{}, ErrTrashUnsupported
	}
	defer C.free(unsafe.Pointer(result))
	return TrashResult{Original: absolute, Trashed: C.GoString(result)}, nil
}

func TrashAvailable() bool { return true }
