//go:build unix

package media

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func openUploadFile(root *os.Root, name string, flags int, mode os.FileMode) (*os.File, error) {
	f, err := root.OpenFile(name, flags|unix.O_NOFOLLOW|unix.O_NONBLOCK, mode)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	stat, ok := info.Sys().(*unix.Stat_t)
	// os uses syscall.Stat_t on some platforms; fstat avoids type assumptions.
	if !ok {
		stat = &unix.Stat_t{}
		err = unix.Fstat(int(f.Fd()), stat) //nolint:gosec // G115: an OS file descriptor fits int.
	}
	if err != nil || !info.Mode().IsRegular() || stat.Nlink != 1 {
		_ = f.Close()
		return nil, ErrUploadInvalid
	}
	return f, nil
}

func (s *UploadStore) lock(ctx context.Context, id string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stripe := 0
	if id != "admission" {
		if !validUploadID(id) {
			return nil, ErrUploadNotFound
		}
		hash := sha256.Sum256([]byte(id))
		stripe = 1 + int(hash[0])%64
	}
	f, err := openUploadFile(s.root, fmt.Sprintf("lock-%02d", stripe), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil { //nolint:gosec // G115: an OS file descriptor fits int.
		_ = f.Close()
		if err == unix.EWOULDBLOCK || err == unix.EAGAIN {
			return nil, ErrUploadBusy
		}
		return nil, err
	}
	return func() { _ = f.Close() }, nil // Closing the descriptor releases flock.
}
