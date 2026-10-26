//go:build linux || darwin

package transferclient

import (
	"os"

	"github.com/felinics/memoh/internal/deliverymeta"
	"golang.org/x/sys/unix"
)

func guardSourceRoot(root *os.Root) (*os.File, error) {
	f, err := root.Open(".")
	if err != nil {
		return nil, ErrBinding
	}
	info, err := f.Stat()
	if err != nil || !info.IsDir() {
		_ = f.Close()
		return nil, ErrBinding
	}
	return f, nil
}

func openSourceFile(root *os.Root, name string, flags int, deleting bool) (*os.File, error) {
	if !sourceLeaf(name) || deleting && !sourceManifestName(name) || flags != os.O_RDONLY && (flags != os.O_CREATE|os.O_RDWR || name != deliverymeta.ProducerFeedbackName) {
		return nil, ErrInvalid
	}
	parent, err := root.Open(".")
	if err != nil {
		return nil, ErrBinding
	}
	defer func() { _ = parent.Close() }()
	fd, err := unix.Openat(int(parent.Fd()), name, flags|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o600) //nolint:gosec // Existing pinned root descriptor; one validated component; special files cannot block before Fstat.
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Nlink != 1 || stat.Mode&unix.S_IFMT != unix.S_IFREG {
		_ = f.Close()
		return nil, ErrStore
	}
	return f, nil
}

func openSourceChild(parent *os.Root, name string) (*os.Root, *os.File, error) {
	if !sourceLeaf(name) {
		return nil, nil, ErrInvalid
	}
	info, err := parent.Lstat(name)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, ErrBinding
	}
	root, err := parent.OpenRoot(name)
	if err != nil {
		return nil, nil, ErrBinding
	}
	guard, err := guardSourceRoot(root)
	if err == nil {
		actual, e := guard.Stat()
		current, currentErr := parent.Lstat(name)
		if e != nil || currentErr != nil || !current.IsDir() || !os.SameFile(info, actual) || !os.SameFile(info, current) {
			err = ErrBinding
		}
	}
	if err != nil {
		closeQueueGuard(guard)
		_ = root.Close()
		return nil, nil, err
	}
	return root, guard, nil
}

func removeSourceManifest(root *os.Root, name string, f *os.File) error {
	if !sourceManifestName(name) {
		return ErrInvalid
	}
	info, err := root.Lstat(name)
	actual, statErr := f.Stat()
	if err != nil || statErr != nil || !os.SameFile(info, actual) || !info.Mode().IsRegular() {
		return ErrProducerManifest
	}
	return root.Remove(name)
}

func syncSourceDir(root *os.Root) error {
	f, err := guardSourceRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return f.Sync()
}
