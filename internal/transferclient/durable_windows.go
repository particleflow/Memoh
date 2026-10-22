//go:build windows

package transferclient

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func queuePlatformReady() error { return nil }

// Windows has no previously admitted queue schema. Hash logical names to avoid
// case folding, reserved DOS devices, ADS and trailing-dot aliases. Constants
// such as locator/striped locks retain their fixed names. Never decode a path ID.
func queueName(name string) string {
	for _, suffix := range []string{".journal", ".snapshot", ".tree", ".part", ".admission"} {
		if strings.HasSuffix(name, suffix) {
			return fmt.Sprintf("%x%s", sha256.Sum256([]byte(strings.TrimSuffix(name, suffix))), suffix)
		}
	}
	return name
}

func nativeError(stage string, err error) error {
	var status windows.NTStatus
	if errors.As(err, &status) {
		err = status.Errno()
	}
	if errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return ErrBusy
	}
	// Stage and OS error only; never source/config paths or remote payloads.
	return fmt.Errorf("%w: windows %s: %w", ErrStore, stage, err)
}

func privateDescriptor() (*windows.SECURITY_DESCRIPTOR, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, nativeError("token", err)
	}
	// Applied only at creation; never SetSecurityInfo on an existing object.
	sid := user.User.Sid.String()
	return windows.SecurityDescriptorFromString("O:" + sid + "D:P(A;OICI;FA;;;" + sid + ")(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
}

func checkPrivateHandle(h windows.Handle) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nativeError("token", err)
	}
	sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return nativeError("security", err)
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || !owner.Equals(user.User.Sid) {
		return ErrStore
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || acl.AceCount == 0 {
		return ErrStore
	}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(acl, i, &ace) != nil || ace == nil || ace.Header.AceSize < 16 {
			return ErrStore
		}
		// Reject callback/object/conditional ACEs we do not implement, even if the
		// current token happens to have access. Deny ACEs cannot broaden access.
		if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return ErrStore
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)) //nolint:gosec // GetAce returns an OS-validated ACE; header and SID length are checked.
		if !sid.IsValid() || sid.Len()+8 > int(ace.Header.AceSize) {
			return ErrStore
		}
		text := sid.String()
		if !sid.Equals(user.User.Sid) && text != "S-1-5-18" && text != "S-1-5-32-544" {
			return ErrStore
		}
	}
	runtime.KeepAlive(sd)
	return nil
}

// NtCreateFile's RootDirectory binds a single validated component to an already
// open directory (no string re-resolution or junction traversal). This is the
// same native primitive used by Go's os.Root Windows implementation.
func nativeAt(root *os.Root, name string, access, share, disposition, options uint32, create bool) (*os.File, error) {
	if name != "." && (filepath.Base(name) != name || strings.ContainsAny(name, `\/:`) || name == "..") {
		return nil, ErrInvalid
	}
	parent, err := root.Open(".")
	if err != nil {
		return nil, ErrStore
	}
	defer func() { _ = parent.Close() }()
	// NT paths represent the root handle itself with an empty object name.
	// Go os.Root uses the same dot-to-empty conversion in OBJECT_ATTRIBUTES.init.
	objectName := name
	if objectName == "." {
		objectName = ""
	}
	object, err := windows.NewNTUnicodeString(objectName)
	if err != nil {
		return nil, ErrInvalid
	}
	oa := windows.OBJECT_ATTRIBUTES{RootDirectory: windows.Handle(parent.Fd()), ObjectName: object, Attributes: windows.OBJ_DONT_REPARSE}
	oa.Length = uint32(unsafe.Sizeof(oa))
	if create {
		oa.SecurityDescriptor, err = privateDescriptor()
		if err != nil {
			return nil, err
		}
	}
	var handle windows.Handle
	err = windows.NtCreateFile(&handle, access|windows.SYNCHRONIZE, &oa, &windows.IO_STATUS_BLOCK{}, nil, windows.FILE_ATTRIBUTE_NORMAL, share, disposition, options|windows.FILE_SYNCHRONOUS_IO_NONALERT|windows.FILE_OPEN_REPARSE_POINT, 0, 0)
	runtime.KeepAlive(oa)
	if err != nil {
		return nil, nativeError("open", err)
	}
	return os.NewFile(uintptr(handle), name), nil
}

func handlePath(h windows.Handle) (string, error) {
	buf := make([]uint16, 32768)
	n, err := windows.GetFinalPathNameByHandle(h, &buf[0], 32768, 0)
	if err != nil || n == 0 || n >= 32768 {
		return "", ErrStore
	}
	path := strings.TrimPrefix(windows.UTF16ToString(buf[:n]), `\\?\`)
	// No UNC, device paths, redirected drives, ReFS, FAT or remote share queues.
	if len(path) < 3 || path[1:3] != `:\` {
		return "", ErrStore
	}
	drive, err := windows.UTF16PtrFromString(path[:3])
	if err != nil || windows.GetDriveType(drive) != windows.DRIVE_FIXED {
		return "", ErrStore
	}
	return path, nil
}

func checkRootHandle(f *os.File, path string) error {
	h := windows.Handle(f.Fd())
	var info windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(h, &info) != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return ErrStore
	}
	actual, err := handlePath(h)
	if err != nil || !strings.EqualFold(actual, path) {
		return ErrStore
	}
	var fs [32]uint16
	if windows.GetVolumeInformationByHandle(h, nil, 0, nil, nil, nil, &fs[0], uint32(len(fs))) != nil || windows.UTF16ToString(fs[:]) != "NTFS" {
		return ErrStore
	}
	return checkPrivateHandle(h)
}

func guardQueueRoot(root *os.Root) (*os.File, error) {
	f, err := nativeAt(root, ".", windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, windows.FILE_OPEN, windows.FILE_DIRECTORY_FILE, false)
	if err != nil {
		return nil, err
	}
	if err := checkRootHandle(f, root.Name()); err != nil {
		_ = f.Close()
		return nil, err
	}
	// Keep this no-delete-share handle for the entire finite queue invocation.
	return f, nil
}

func checkQueueRoot(root *os.Root) error {
	f, err := guardQueueRoot(root)
	if err == nil {
		_ = f.Close()
	}
	return err
}

func makeQueueDir(root *os.Root, name string) error {
	f, err := nativeAt(root, name, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, windows.FILE_OPEN_IF, windows.FILE_DIRECTORY_FILE, true)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return checkRootHandle(f, filepath.Join(root.Name(), name))
}

func openQueueFile(root *os.Root, name string, flags int, _ os.FileMode) (*os.File, error) {
	access, disposition := uint32(windows.GENERIC_READ|windows.FILE_WRITE_ATTRIBUTES), uint32(windows.FILE_OPEN)
	if flags&os.O_RDWR != 0 {
		access |= windows.GENERIC_WRITE
	}
	if flags&os.O_CREATE != 0 {
		disposition = windows.FILE_OPEN_IF
		if flags&os.O_EXCL != 0 {
			disposition = windows.FILE_CREATE
		}
	}
	share := uint32(windows.FILE_SHARE_READ)
	// Journal admission reads may overlap another job's append. Its first frame
	// is immutable; job locks own journal writes. Lock handles must be shareable.
	if strings.HasSuffix(name, ".journal") || strings.HasSuffix(name, ".lock") {
		share |= windows.FILE_SHARE_WRITE
	}
	return nativeAt(root, name, access, share, disposition, windows.FILE_NON_DIRECTORY_FILE, flags&os.O_CREATE != 0)
}

func checkQueueFile(f *os.File) error {
	h := windows.Handle(f.Fd())
	var info windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(h, &info) != nil || info.NumberOfLinks != 1 || info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		return ErrStore
	}
	kind, err := windows.GetFileType(h)
	if err != nil || kind != windows.FILE_TYPE_DISK {
		return ErrStore
	}
	return checkPrivateHandle(h)
}

func queueFileIdentity(f *os.File) (string, error) {
	var id struct {
		Volume uint64
		ID     [16]byte
	}
	if windows.GetFileInformationByHandleEx(windows.Handle(f.Fd()), windows.FileIdInfo, (*byte)(unsafe.Pointer(&id)), uint32(unsafe.Sizeof(id))) != nil { //nolint:gosec // Exact fixed FILE_ID_INFO layout passed to the Windows API.
		return "", ErrStore
	}
	info, err := f.Stat()
	if err != nil {
		return "", ErrStore
	}
	return fmt.Sprintf("%x:%x:%d", id.Volume, id.ID, info.Size()), nil
}

func lockQueueFile(f *os.File) error {
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 0xffffffff, 0xffffffff, &windows.Overlapped{}); err != nil {
		return nativeError("lock", err)
	}
	return nil
}

func unlockClose(f *os.File) {
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 0xffffffff, 0xffffffff, &windows.Overlapped{})
	_ = f.Close()
}

// Flush a verified writable NTFS directory handle, not a volume. No elevated
// privileges, ignored unsupported error or POSIX-fsync emulation. MS-FSA
// 2.1.5.7 documents NTFS directory metadata flush; the Windows runner must also
// execute this path. Hardware that lies about flush completion is out of scope.
func syncQueueDir(root *os.Root) error {
	f, err := guardQueueRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := windows.FlushFileBuffers(windows.Handle(f.Fd())); err != nil {
		return nativeError("flush-directory", err)
	}
	return nil
}

func queueFreeSpace(root *os.Root, bytes int64) error {
	if bytes <= 0 {
		return ErrInvalid
	}
	f, err := guardQueueRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	path, err := handlePath(windows.Handle(f.Fd()))
	if err != nil {
		return err
	}
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return ErrStore
	}
	var free uint64
	if err := windows.GetDiskFreeSpaceEx(ptr, &free, nil, nil); err != nil {
		return nativeError("free-space", err)
	}
	if uint64(bytes)+(16<<20) > free {
		return ErrCapacity
	}
	return nil
}

func removeQueueFile(root *os.Root, name string) error {
	if _, err := root.Lstat(name); errors.Is(err, os.ErrNotExist) {
		return os.ErrNotExist
	}
	f, err := nativeAt(root, name, windows.DELETE|windows.READ_CONTROL|windows.FILE_READ_ATTRIBUTES, 0, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE, false)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := checkQueueFile(f); err != nil {
		return err
	}
	// Delete only our private artifact through the verified handle; do not change
	// its ACL or clear readonly attributes by a separately resolved pathname.
	flags := uint32(windows.FILE_DISPOSITION_DELETE | windows.FILE_DISPOSITION_IGNORE_READONLY_ATTRIBUTE)
	if err := windows.SetFileInformationByHandle(windows.Handle(f.Fd()), windows.FileDispositionInfoEx, (*byte)(unsafe.Pointer(&flags)), 4); err != nil { //nolint:gosec // Exact 32-bit disposition structure passed to the Windows API.
		return nativeError("delete", err)
	}
	return nil
}

var reopenFile = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReOpenFile")

func guardQueueSource(source *os.File) (*os.File, func(), error) {
	// ReOpenFile binds the producer's already-authorized handle, without reopening
	// an untrusted source path. Withhold WRITE/DELETE sharing, including existing
	// writers/mappings. Producers must close writable handles before admission.
	h, _, err := reopenFile.Call(source.Fd(), uintptr(windows.GENERIC_READ), uintptr(windows.FILE_SHARE_READ), uintptr(windows.FILE_FLAG_OPEN_REPARSE_POINT))
	runtime.KeepAlive(source)
	if windows.Handle(h) == windows.InvalidHandle {
		return nil, nil, nativeError("source-open", err)
	}
	f := os.NewFile(h, "authorized-source")
	var info windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(windows.Handle(h), &info) != nil || info.NumberOfLinks != 1 || info.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
		_ = f.Close()
		return nil, nil, ErrSnapshot
	}
	return f, func() { _ = f.Close() }, nil
}
