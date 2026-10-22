package localfs

import (
	"errors"
	"os"
	"syscall"

	"github.com/felinics/memoh/internal/storage"
)

func originalStamp(info os.FileInfo) (fileStamp, error) {
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || s.Nlink != 1 {
		return fileStamp{}, errors.New("verified range requires a single-linked regular original")
	}
	return fileStamp{
		device: s.Dev, inode: s.Ino, mode: uint64(s.Mode), size: info.Size(),
		mtimeSec: int64(s.Mtim.Sec), mtimeNsec: int64(s.Mtim.Nsec), ctimeSec: int64(s.Ctim.Sec), ctimeNsec: int64(s.Ctim.Nsec), //nolint:unconvert // Timespec fields are 32-bit on some Linux architectures.
	}, nil
}

func rangeFilesystem(f *os.File) error {
	var s syscall.Statfs_t
	if err := syscall.Fstatfs(int(f.Fd()), &s); err != nil { //nolint:gosec // G115: descriptor from os.File; an invalid descriptor becomes EBADF.
		return err
	}
	// Network/FUSE filesystems may expose stale change metadata. Retain the
	// full-stream verification path there; no unsafe metadata-only cache hit.
	switch int64(s.Type) & 0xffffffff { //nolint:unconvert // Normalize signed 32-bit filesystem magic values as well as 64-bit Statfs_t.
	case 0xef53, 0x9123683e, 0x58465342, 0x2fc12fc1, 0xf2f52010, 0x01021994, 0x794c7630:
		return nil // ext, btrfs, xfs, zfs, f2fs, tmpfs, overlay
	default:
		return storage.ErrVerifiedRangeUnsupported
	}
}
