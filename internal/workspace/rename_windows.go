package workspace

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func RenameAvailable() bool { return true }

type fileRenameInformation struct {
	flags      uint32
	directory  windows.Handle
	nameLength uint32
	name       [1]uint16
}

func renameNoReplace(oldDir *os.File, oldName string, newDir *os.File, newName string) error {
	name, err := windows.NewNTUnicodeString(oldName)
	if err != nil {
		return err
	}
	attributes := windows.OBJECT_ATTRIBUTES{ObjectName: name, RootDirectory: windows.Handle(oldDir.Fd()), Attributes: windows.OBJ_CASE_INSENSITIVE}
	attributes.Length = uint32(unsafe.Sizeof(attributes))
	var file windows.Handle
	var status windows.IO_STATUS_BLOCK
	err = windows.NtCreateFile(&file, windows.DELETE|windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE, &attributes, &status, nil, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.FILE_OPEN,
		windows.FILE_SYNCHRONOUS_IO_NONALERT|windows.FILE_OPEN_REPARSE_POINT, 0, 0)
	if err != nil {
		return renameWindowsError(err)
	}
	defer windows.CloseHandle(file)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(file, &info); err != nil {
		return err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return ErrOutsideWorkspace
	}
	target, err := windows.UTF16FromString(newName)
	if err != nil {
		return err
	}
	length := len(target) - 1
	const offset = int(unsafe.Offsetof(fileRenameInformation{}.name))
	buffer := make([]byte, max(offset+length*2, int(unsafe.Sizeof(fileRenameInformation{}))))
	request := (*fileRenameInformation)(unsafe.Pointer(&buffer[0]))
	request.directory, request.nameLength = windows.Handle(newDir.Fd()), uint32(length*2)
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(&buffer[offset])), length), target[:length])
	return renameWindowsError(windows.NtSetInformationFile(file, &status, &buffer[0], uint32(len(buffer)), windows.FileRenameInformation))
}

func renameWindowsError(err error) error {
	var status windows.NTStatus
	if errors.As(err, &status) {
		return status.Errno()
	}
	return err
}
