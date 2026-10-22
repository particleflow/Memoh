//go:build linux || darwin

package transferclient

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func queuePlatformReady() error { return nil }

func checkQueueRoot(root *os.Root) error {
	f, err := root.Open(".")
	if err != nil {
		return ErrStore
	}
	defer func() { _ = f.Close() }()
	var stat unix.Stat_t
	if unix.Fstat(int(f.Fd()), &stat) != nil || stat.Uid != uint32(os.Getuid()) || stat.Mode&0o077 != 0 { //nolint:gosec // OS descriptors and Unix UIDs have native int/uid_t range.
		return ErrStore
	}
	return nil
}

func checkQueueFile(f *os.File) error {
	var stat unix.Stat_t
	if unix.Fstat(int(f.Fd()), &stat) != nil || stat.Nlink != 1 || stat.Uid != uint32(os.Getuid()) || stat.Mode&0o077 != 0 || stat.Mode&unix.S_IFMT != unix.S_IFREG { //nolint:gosec // OS descriptors and Unix UIDs have native int/uid_t range.
		return ErrStore
	}
	return nil
}

func lockQueueFile(f *os.File) error {
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil { //nolint:gosec // Fd originates from an open Unix file descriptor.
		if errors.Is(err, unix.EWOULDBLOCK) {
			return ErrBusy
		}
		return ErrStore
	}
	return nil
}

func unlockClose(f *os.File) {
	_ = unix.Flock(int(f.Fd()), unix.LOCK_UN) //nolint:gosec // Fd originates from an open Unix file descriptor.
	_ = f.Close()
}

func syncQueueDir(root *os.Root) error {
	f, err := root.Open(".")
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.IsDir() {
		return ErrStore
	}
	return f.Sync()
}

func queueFreeSpace(root *os.Root, bytes int64) error {
	f, err := root.Open(".")
	if err != nil {
		return ErrStore
	}
	defer func() { _ = f.Close() }()
	var stat unix.Statfs_t
	if unix.Fstatfs(int(f.Fd()), &stat) != nil || stat.Bsize <= 0 { //nolint:gosec // Fd originates from an open Unix file descriptor.
		return ErrStore
	}
	// Reserve a metadata margin; ENOSPC during later writes still fails closed.
	if bytes <= 0 {
		return ErrInvalid
	}
	if uint64(bytes)+(16<<20) > stat.Bavail*uint64(stat.Bsize) {
		return ErrCapacity
	}
	return nil
}
