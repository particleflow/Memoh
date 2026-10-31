//go:build linux

package workspace

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"

	ctr "github.com/felinics/memoh/internal/container"
)

// cleanupBotRunDirectory is deliberately narrower than workspace data cleanup:
// it removes one empty, canonical UUID directory under the configured run root.
// Every ancestor is opened without following symlinks and pinned by descriptor;
// AT_REMOVEDIR atomically rejects files, symlinks and non-empty directories.
func (m *Manager) cleanupBotRunDirectory(botID string) error {
	id, err := uuid.Parse(botID)
	if err != nil || id == uuid.Nil || id.String() != botID {
		return fmt.Errorf("%w: runtime directory requires a canonical Bot UUID", ctr.ErrInvalidArgument)
	}
	root, err := filepath.Abs(m.dataRoot())
	if err != nil {
		return fmt.Errorf("resolve runtime directory root: %w", err)
	}
	if root == string(filepath.Separator) {
		return fmt.Errorf("%w: runtime directory requires a dedicated data root", ctr.ErrInvalidArgument)
	}

	fd, err := openRuntimeDirectoryRoot(root)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open runtime directory root: %w", err)
	}
	defer func() { _ = unix.Close(fd) }()

	run, err := unix.Openat(fd, "run", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open Bot runtime directory parent: %w", err)
	}
	defer func() { _ = unix.Close(run) }()
	if err := unix.Unlinkat(run, botID, unix.AT_REMOVEDIR); err != nil && !errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("remove empty Bot runtime directory: %w", err)
	}
	return nil
}

func openRuntimeDirectoryRoot(root string) (int, error) {
	flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC
	fd, err := unix.Open(string(filepath.Separator), flags, 0)
	if err != nil {
		return -1, err
	}
	for _, component := range strings.Split(strings.TrimPrefix(root, string(filepath.Separator)), string(filepath.Separator)) {
		next, err := unix.Openat(fd, component, flags, 0)
		_ = unix.Close(fd)
		if err != nil {
			return -1, err
		}
		fd = next
	}
	return fd, nil
}
