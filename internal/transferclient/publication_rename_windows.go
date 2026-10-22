//go:build windows

package transferclient

import (
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

func renamePublication(source *os.File, destination *os.Root, name string) error {
	if len(name) != 71 {
		return ErrInvalid
	} // SHA-256 plus the fixed .object suffix.
	dir, err := guardQueueRoot(destination)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	wide, err := windows.UTF16FromString(name)
	if err != nil {
		return ErrInvalid
	}
	var layout struct {
		Replace uint32
		Root    windows.Handle
		Length  uint32
		Name    [1]uint16
	}
	offset := int(unsafe.Offsetof(layout.Name))
	buffer := make([]byte, offset+len(wide)*2)
	info := (*struct {
		Replace uint32
		Root    windows.Handle
		Length  uint32
		Name    [1]uint16
	})(unsafe.Pointer(&buffer[0])) //nolint:gosec // Native FILE_RENAME_INFORMATION layout with a bounded UTF-16 trailing name.
	info.Root, info.Length = windows.Handle(dir.Fd()), uint32((len(wide)-1)*2)
	for i, v := range wide {
		buffer[offset+2*i], buffer[offset+2*i+1] = byte(v), byte(v>>8)
	}
	// ReplaceIfExists remains FALSE. Bind the target to its verified directory
	// handle; never MoveFileEx, POSIX replacement, a path reopen, or a copy fallback.
	err = windows.NtSetInformationFile(windows.Handle(source.Fd()), &windows.IO_STATUS_BLOCK{}, &buffer[0], uint32(len(buffer)), windows.FileRenameInformation)
	runtime.KeepAlive(source)
	runtime.KeepAlive(dir)
	if err != nil {
		return nativeError("publish-rename", err)
	}
	return nil
}
