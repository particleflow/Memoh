//go:build windows

package transferclient

import (
	"os"

	"golang.org/x/sys/windows"
)

func queuePlatformReady() error { return ErrBinding }

func checkQueueRoot(*os.Root) error { return ErrBinding }

func checkQueueFile(f *os.File) error {
	var info windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info) != nil || info.NumberOfLinks != 1 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return ErrStore
	}
	return nil
}

func lockQueueFile(f *os.File) error {
	if windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 0xffffffff, 0xffffffff, &windows.Overlapped{}) != nil {
		return ErrBusy
	}
	return nil
}

func unlockClose(f *os.File) {
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 0xffffffff, 0xffffffff, &windows.Overlapped{})
	_ = f.Close()
}

// Windows file Sync/LockFileEx alone do not establish durable directory
// creation or a private DACL. No reviewed native host binding exists yet.
// Refuse admission before any HTTP call; Linux tests are not Windows proof.
func syncQueueDir(*os.Root) error { return ErrBinding }

func queueFreeSpace(*os.Root, int64) error { return ErrBinding }
