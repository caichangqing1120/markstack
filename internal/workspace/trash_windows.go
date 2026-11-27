package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	comLibrary             = windows.NewLazySystemDLL("ole32.dll")
	shellLibrary           = windows.NewLazySystemDLL("shell32.dll")
	comInitialize          = comLibrary.NewProc("CoInitializeEx")
	comUninitialize        = comLibrary.NewProc("CoUninitialize")
	comCreate              = comLibrary.NewProc("CoCreateInstance")
	shellCreateItem        = shellLibrary.NewProc("SHCreateItemFromParsingName")
	fileOperationClass     = windows.GUID{Data1: 0x3ad05575, Data2: 0x8857, Data3: 0x4850, Data4: [8]byte{0x92, 0x77, 0x11, 0xb8, 0x5b, 0xdb, 0x8e, 0x09}}
	fileOperationInterface = windows.GUID{Data1: 0x947aab5f, Data2: 0x0a5c, Data3: 0x4c13, Data4: [8]byte{0xb4, 0xd6, 0x4b, 0xf7, 0x83, 0x6f, 0xc9, 0xf8}}
	shellItemInterface     = windows.GUID{Data1: 0x43826d1e, Data2: 0xe718, Data3: 0x42ee, Data4: [8]byte{0xbc, 0x55, 0xa1, 0xe2, 0x61, 0xc3, 0x7b, 0xfe}}
)

type shellObject struct{ methods *[23]uintptr }

func (object *shellObject) invoke(method int, arguments ...uintptr) uintptr {
	result, _, _ := syscall.SyscallN(object.methods[method], append([]uintptr{uintptr(unsafe.Pointer(object))}, arguments...)...)
	return result
}

func shellResult(stage string, result uintptr) error {
	if int32(result) < 0 {
		return fmt.Errorf("Recycle Bin %s: HRESULT 0x%08x", stage, uint32(result))
	}
	return nil
}

func TrashAvailable() bool { return true }

func Trash(root, relative string, expected ...os.FileInfo) (TrashResult, error) {
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
	if !matchTrashFile(info, expected) {
		return TrashResult{}, ErrConflict
	}
	volume, err := windows.UTF16PtrFromString(filepath.VolumeName(absolute) + `\`)
	if err != nil || windows.GetDriveType(volume) != windows.DRIVE_FIXED {
		return TrashResult{}, ErrTrashUnsupported
	}
	path, err := windows.UTF16PtrFromString(absolute)
	if err != nil {
		return TrashResult{}, err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	result, _, _ := comInitialize.Call(0, 2)
	if uint32(result) != 0x80010106 {
		if err := shellResult("initialize", result); err != nil {
			return TrashResult{}, err
		}
		defer comUninitialize.Call()
	}
	var operation, item *shellObject
	result, _, _ = comCreate.Call(uintptr(unsafe.Pointer(&fileOperationClass)), 0, 1, uintptr(unsafe.Pointer(&fileOperationInterface)), uintptr(unsafe.Pointer(&operation)))
	if err := shellResult("create", result); err != nil {
		return TrashResult{}, err
	}
	defer operation.invoke(2)
	result, _, _ = shellCreateItem.Call(uintptr(unsafe.Pointer(path)), 0, uintptr(unsafe.Pointer(&shellItemInterface)), uintptr(unsafe.Pointer(&item)))
	if err := shellResult("resolve item", result); err != nil {
		return TrashResult{}, err
	}
	defer item.invoke(2)
	const (
		forceRecycle = 0x00080000
		allowUndo    = 0x0040
		noErrorUI    = 0x0400
		silent       = 0x0004
	)
	const flags = forceRecycle | allowUndo | noErrorUI | silent
	if err := shellResult("set recycle policy", operation.invoke(5, flags)); err != nil {
		return TrashResult{}, err
	}
	current, err := os.Lstat(absolute)
	if err != nil || !os.SameFile(info, current) || !matchTrashFile(current, expected) {
		return TrashResult{}, ErrOutsideWorkspace
	}
	if err := shellResult("queue item", operation.invoke(18, uintptr(unsafe.Pointer(item)), 0)); err != nil {
		return TrashResult{}, err
	}
	if err := shellResult("recycle item", operation.invoke(21)); err != nil {
		return TrashResult{}, err
	}
	var cancelled int32
	result, _, _ = syscall.SyscallN(operation.methods[22], uintptr(unsafe.Pointer(operation)), uintptr(unsafe.Pointer(&cancelled)))
	if err := shellResult("verify item", result); err != nil {
		return TrashResult{}, err
	}
	if cancelled != 0 {
		return TrashResult{}, ErrTrashUnsupported
	}
	if _, err := parent.Lstat(name); !os.IsNotExist(err) {
		return TrashResult{}, ErrTrashUnsupported
	}
	return TrashResult{Original: absolute}, nil
}
