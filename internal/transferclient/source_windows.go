//go:build windows

package transferclient

import (
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/felinics/memoh/internal/deliverymeta"
	"golang.org/x/sys/windows"
)

func sourceUNC(path string) bool {
	if !strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, `\\?\`) || strings.HasPrefix(path, `\\.\`) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(path, `\\`), `\`)
	return len(parts) >= 2 && parts[0] != "" && parts[1] != "" && parts[0] != "." && parts[0] != "?"
}

func sourceFinalPath(f *os.File) (string, error) {
	buf := make([]uint16, 32768)
	n, err := windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &buf[0], uint32(len(buf)), 0)
	if err != nil || n == 0 || n >= uint32(len(buf)) {
		return "", ErrBinding
	}
	actual := windows.UTF16ToString(buf[:n])
	if strings.HasPrefix(actual, `\\?\UNC\`) {
		return `\\` + strings.TrimPrefix(actual, `\\?\UNC\`), nil
	}
	return strings.TrimPrefix(actual, `\\?\`), nil
}

// Remote sources must be the already-configured native SMB share, never a
// mapped-drive alias, WebDAV/device namespace or an offline-cached substitute.
// This queries the opened handle; it creates no connection/credential/protocol.
func checkSourceRoot(f *os.File, expected string) error {
	h := windows.Handle(f.Fd())
	var info windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(h, &info) != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return ErrBinding
	}
	actual, err := sourceFinalPath(f)
	if err != nil || !strings.EqualFold(actual, expected) {
		return ErrBinding
	}
	if sourceUNC(expected) {
		// FILE_REMOTE_PROTOCOL_INFO v2, including its protocol-specific union.
		var remote struct {
			Version, Size               uint16
			Protocol                    uint32
			Major, Minor, Revision, Pad uint16
			Flags                       uint32
			Generic                     [8]uint32
			Reserved, Specific          [16]uint32
		}
		remote.Version, remote.Size = 2, uint16(unsafe.Sizeof(remote))
		if windows.GetFileInformationByHandleEx(h, windows.FileRemoteProtocolInfo, (*byte)(unsafe.Pointer(&remote)), uint32(unsafe.Sizeof(remote))) != nil || remote.Protocol != 0x00020000 || remote.Flags&2 != 0 { //nolint:gosec // Exact documented FILE_REMOTE_PROTOCOL_INFO v2 layout, native handle only.
			return ErrBinding
		}
		return nil
	}
	// Preserve the existing local-source filesystem support. Queue checks are
	// wholly separate and still require private fixed NTFS for all their roots.
	if _, err := handlePath(h); err != nil {
		return ErrBinding
	}
	var fs [32]uint16
	if windows.GetVolumeInformationByHandle(h, nil, 0, nil, nil, nil, &fs[0], uint32(len(fs))) != nil || windows.UTF16ToString(fs[:]) != "NTFS" {
		return ErrBinding
	}
	return nil
}

func guardSourceRoot(root *os.Root) (*os.File, error) {
	f, err := nativeAt(root, ".", windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, windows.FILE_OPEN, windows.FILE_DIRECTORY_FILE, false)
	if err == nil {
		err = checkSourceRoot(f, root.Name())
	}
	if err != nil {
		if f != nil {
			_ = f.Close()
		}
		return nil, err
	}
	return f, nil
}

func openSourceChild(parent *os.Root, name string) (*os.Root, *os.File, error) {
	if !sourceLeaf(name) {
		return nil, nil, ErrInvalid
	}
	pinned, err := nativeAt(parent, name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, windows.FILE_OPEN, windows.FILE_DIRECTORY_FILE, false)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = pinned.Close() }()
	if err := checkSourceRoot(pinned, filepath.Join(parent.Name(), name)); err != nil {
		return nil, nil, err
	}
	root, err := parent.OpenRoot(name)
	if err != nil {
		return nil, nil, err
	}
	guard, err := guardSourceRoot(root)
	if err == nil {
		before, beforeErr := queueObjectIdentity(pinned)
		after, afterErr := queueObjectIdentity(guard)
		if beforeErr != nil || afterErr != nil || before != after {
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

func openSourceFile(root *os.Root, name string, flags int, deleting bool) (*os.File, error) {
	if !sourceLeaf(name) || deleting && !sourceManifestName(name) || flags != os.O_RDONLY && (flags != os.O_CREATE|os.O_RDWR || name != deliverymeta.ProducerFeedbackName) {
		return nil, ErrInvalid
	}
	access, disposition := uint32(windows.GENERIC_READ), uint32(windows.FILE_OPEN)
	if flags == os.O_CREATE|os.O_RDWR {
		access |= windows.GENERIC_WRITE
		disposition = windows.FILE_OPEN_IF
	}
	if deleting {
		access |= windows.DELETE
	}
	f, err := nativeAt(root, name, access, windows.FILE_SHARE_READ, disposition, windows.FILE_NON_DIRECTORY_FILE, false)
	if err != nil {
		return nil, err
	}
	var info windows.ByHandleFileInformation
	kind, typeErr := windows.GetFileType(windows.Handle(f.Fd()))
	if windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info) != nil || typeErr != nil || kind != windows.FILE_TYPE_DISK || info.NumberOfLinks != 1 || info.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
		_ = f.Close()
		return nil, ErrStore
	}
	return f, nil
}

func removeSourceManifest(root *os.Root, name string, f *os.File) error {
	if !sourceManifestName(name) {
		return ErrInvalid
	}
	actual, err := sourceFinalPath(f)
	if err != nil || !strings.EqualFold(actual, filepath.Join(root.Name(), name)) {
		return ErrProducerManifest
	}
	// Basic FILE_DISPOSITION_INFO respects existing ACLs/readonly attributes;
	// unlike private queue cleanup, never clear an attribute on a user source.
	remove := byte(1)
	if err := windows.SetFileInformationByHandle(windows.Handle(f.Fd()), windows.FileDispositionInfo, &remove, 1); err != nil {
		return nativeError("source-consume", err)
	}
	return nil
}

func syncSourceDir(root *os.Root) error {
	f, err := nativeAt(root, ".", windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, windows.FILE_OPEN, windows.FILE_DIRECTORY_FILE, false)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := checkSourceRoot(f, root.Name()); err != nil {
		return err
	}
	if err := windows.FlushFileBuffers(windows.Handle(f.Fd())); err != nil {
		return nativeError("source-flush-directory", err)
	}
	return nil
}
