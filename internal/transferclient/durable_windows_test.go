//go:build windows

package transferclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func nativeFixtureRoot(t *testing.T, broad bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "private-state")
	sd, err := privateDescriptor()
	if err != nil {
		t.Fatal(err)
	}
	if broad {
		user, err := windows.GetCurrentProcessToken().GetTokenUser()
		if err != nil {
			t.Fatal(err)
		}
		sd, err = windows.SecurityDescriptorFromString("O:" + user.User.Sid.String() + "D:P(A;OICI;FA;;;WD)")
		if err != nil {
			t.Fatal(err)
		}
	}
	sa := windows.SecurityAttributes{SecurityDescriptor: sd}
	sa.Length = uint32(unsafe.Sizeof(sa))
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.CreateDirectory(ptr, &sa); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func nativeLimits() QueueLimits {
	return QueueLimits{Jobs: 8, Bytes: 256 << 20, JournalBytes: 1 << 20, Workers: 1}
}

func nativeQueue(t *testing.T, path string) *DiskQueue {
	t.Helper()
	q, err := OpenHerdrQueue(func(string) string { return path }, "revision-1", nativeLimits())
	if err != nil {
		t.Fatal("native queue open:", err)
	}
	t.Cleanup(func() { _ = q.Close() })
	return q
}

func nativeSource(t *testing.T, data []byte) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "closed-output")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path) //nolint:gosec // Fixed fixture basename under TempDir.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestWindowsNativePrivacyAndPathBinding(t *testing.T) {
	path := nativeFixtureRoot(t, true)
	if _, err := OpenHerdrQueue(func(string) string { return path }, "revision-1", nativeLimits()); !errors.Is(err, ErrStore) {
		t.Fatal("broad DACL accepted", err)
	}
	if _, err := os.Stat(filepath.Join(path, "deliveries-v1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("rejected root was modified")
	}
	path = nativeFixtureRoot(t, false)
	q := nativeQueue(t, path)
	if err := syncQueueDir(q.root); err != nil {
		t.Fatal("NTFS directory flush:", err)
	}
	if err := queueFreeSpace(q.root, 1); err != nil {
		t.Fatal("free space:", err)
	}
	if err := os.Rename(filepath.Join(path, "deliveries-v1"), filepath.Join(path, "moved")); err == nil {
		t.Fatal("open queue identity could be renamed")
	}
	if queueName("CON.journal") == queueName("con.journal") || queueName("A.journal") == queueName("a.journal") {
		t.Fatal("case aliases collide")
	}
	for _, id := range []string{"CON", "con", "A", "a"} {
		j := newJob([]byte("native"))
		j.DeliveryID, j.SnapshotID = id, id
		if err := q.Admit(t.Context(), "download", j, nil); err != nil {
			t.Fatal("logical device/case ID", id, err)
		}
		if _, err := q.Inspect(id); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWindowsNativeSnapshotSharingAndSourceGuard(t *testing.T) {
	path := nativeFixtureRoot(t, false)
	q := nativeQueue(t, path)
	data := []byte("immutable native snapshot fixture")
	j := newJob(data)
	j.SnapshotID = j.DeliveryID
	source := nativeSource(t, data)
	if err := q.Admit(t.Context(), "upload", j, source); err != nil {
		t.Fatal(err)
	}
	r, err := q.Inspect(j.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, release, err := q.openSnapshot(r)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	actual := make([]byte, len(data))
	if err := snapshot.ReadAt(t.Context(), j.SnapshotID, j.LocatorRevision, j.Spec, actual, 0); err != nil || !bytes.Equal(actual, data) {
		t.Fatal("native proof read", err)
	}
	sealed := snapshot.(sealedSnapshot)
	f := sealed.data.(*os.File)
	// Remove only the fixture's readonly attribute to prove sharing, not the
	// attribute, rejects another writer. The opened handle still pins identity.
	if err := f.Chmod(0o600); err != nil {
		t.Fatal(err)
	}
	writer, err := os.OpenFile(filepath.Join(path, "deliveries-v1", queueName(j.DeliveryID+".snapshot")), os.O_WRONLY, 0o600) //nolint:gosec // Private fixture namespace and derived logical name.
	if err == nil {
		_ = writer.Close()
		t.Fatal("snapshot accepted concurrent writer")
	}
	if err := f.Chmod(0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(path, "deliveries-v1", queueName(j.DeliveryID+".snapshot")), filepath.Join(path, "moved-snapshot")); err == nil {
		t.Fatal("snapshot renamed while reading")
	}
	// An already-open producer writer prevents freezing; close and retry the
	// same reserved ID using its authorized read handle.
	other := nativeQueue(t, nativeFixtureRoot(t, false))
	sourcePath := filepath.Join(t.TempDir(), "producer")
	writable, err := os.Create(sourcePath) //nolint:gosec // Fixed fixture basename under TempDir.
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writable.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := other.Admit(t.Context(), "upload", j, writable); !errors.Is(err, ErrBusy) {
		t.Fatal("live source writer accepted", err)
	}
	_ = writable.Close()
	closed, err := os.Open(sourcePath) //nolint:gosec // Fixed fixture basename under TempDir.
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = closed.Close() }()
	if err := other.Admit(t.Context(), "upload", j, closed); err != nil {
		t.Fatal("closed source resume", err)
	}
}

func TestWindowsNativeCrossProcessLocks(t *testing.T) {
	if path := os.Getenv("MEMOH_NATIVE_LOCK_FIXTURE"); path != "" {
		q := nativeQueue(t, path)
		f, err := q.lock("delivery-lock")
		if os.Getenv("MEMOH_NATIVE_LOCK_CRASH") == "1" {
			if err != nil {
				t.Fatal(err)
			}
			_ = f
			os.Exit(29)
		}
		if !errors.Is(err, ErrBusy) {
			if f != nil {
				unlockClose(f)
			}
			t.Fatal("cross-process exclusion failed", err)
		}
		return
	}
	path := nativeFixtureRoot(t, false)
	q := nativeQueue(t, path)
	f, err := q.lock("delivery-lock")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestWindowsNativeCrossProcessLocks$") //nolint:gosec // Current test executable with fixed arguments.
	cmd.Env = append(os.Environ(), "MEMOH_NATIVE_LOCK_FIXTURE="+path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("lock child: %v %s", err, out)
	}
	unlockClose(f)
	cmd = exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestWindowsNativeCrossProcessLocks$") //nolint:gosec // Current test executable with fixed arguments.
	cmd.Env = append(os.Environ(), "MEMOH_NATIVE_LOCK_FIXTURE="+path, "MEMOH_NATIVE_LOCK_CRASH=1")
	err = cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 29 {
		t.Fatal("crash child", err)
	}
	// The OS may release terminated-process locks asynchronously; bounded retry.
	for attempt := 0; attempt < 20; attempt++ {
		f, err = q.lock("delivery-lock")
		if err == nil {
			unlockClose(f)
			return
		}
		if !errors.Is(err, ErrBusy) {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("crashed owner lock not released")
}

func TestWindowsNativeAdmissionCancelAndRetire(t *testing.T) {
	path := nativeFixtureRoot(t, false)
	q := nativeQueue(t, path)
	data := bytes.Repeat([]byte("x"), 65<<20)
	j := newJob(data)
	j.SnapshotID = j.DeliveryID
	source := nativeSource(t, data)
	if err := q.Admit(t.Context(), "upload", j, source); !errors.Is(err, ErrAdmissionPending) {
		t.Fatal("bounded copy", err)
	}
	_ = q.Close()
	q = nativeQueue(t, path)
	if err := q.Admit(t.Context(), "upload", j, source); err != nil {
		t.Fatal("native resume", err)
	}
	// Confirm private readonly files can retire by verified handle without an
	// ACL edit or leaking a permanent quota reservation.
	if err := q.RequestCancel(j.DeliveryID); err != nil {
		t.Fatal(err)
	}
	r, journal, err := q.load(j.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	r.Job.Phase = "canceled"
	if err := q.save(journal, r); err != nil {
		t.Fatal(err)
	}
	_ = journal.Close()
	if err := q.Retire(j.DeliveryID); err != nil {
		t.Fatal("readonly retirement", err)
	}
	if _, err := q.root.Lstat(queueName(j.DeliveryID + ".admission")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("progress leaked")
	}
	j.DeliveryID, j.SnapshotID = "partial", "partial"
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := q.Admit(ctx, "upload", j, source); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := q.RequestCancel(j.DeliveryID); err != nil {
		t.Fatal(err)
	}
	if err := q.Retire(j.DeliveryID); err != nil {
		t.Fatal("partial retirement", err)
	}
}

func TestWindowsNativeTLSUploadOutboxAndDownload(t *testing.T) {
	path := nativeFixtureRoot(t, false)
	q := nativeQueue(t, path)
	data := []byte("native TLS fixture")
	j := newJob(data)
	j.SnapshotID = j.DeliveryID
	if err := q.Admit(t.Context(), "upload", j, nativeSource(t, data)); err != nil {
		t.Fatal(err)
	}
	fixture, client := newFixture(t)
	fixture.setDrop("PATCH /bots/bot-1/media/uploads/receipt-1")
	committed := false
	binding := &AssociationBinding{
		Commit: func(context.Context, string, Reference, io.Reader) error {
			committed = true
			return context.DeadlineExceeded
		},
		Resolve: func(context.Context, string, Reference, bool) (AssociationOutcome, error) {
			if !committed {
				t.Fatal("resolve before commit")
			}
			return OutcomeCommitted, nil
		},
	}
	for attempt := 0; attempt < 30; attempt++ {
		_ = q.Close()
		q = nativeQueue(t, path)
		r, err := q.Inspect(j.DeliveryID)
		if err != nil {
			t.Fatal(err)
		}
		if r.Associated {
			break
		}
		now := time.Now()
		if r.Job.NextAttempt.After(now) {
			now = r.Job.NextAttempt
		}
		if r.AssociationDue.After(now) {
			now = r.AssociationDue
		}
		if committed && !r.Job.CancelRequested {
			if err := q.RequestCancel(j.DeliveryID); err != nil {
				t.Fatal(err)
			}
		}
		if err := q.RunOne(t.Context(), j.DeliveryID, client, binding, now); err != nil {
			t.Fatal(err)
		}
	}
	r, err := q.Inspect(j.DeliveryID)
	if err != nil || !r.Associated || !r.Job.CancelRequested || !bytes.Equal(fixtureReady(fixture), data) {
		t.Fatal("ambiguous committed result lost", r, err)
	}
	if fixture.count("POST /bots/bot-1/media/uploads") != 1 {
		t.Fatal("created duplicate receipt")
	}
	if err := q.Retire(j.DeliveryID); err != nil {
		t.Fatal(err)
	}
	// NAS-to-PC Range resumes against a TLS loopback fixture. Only the verified
	// staging reader reaches the explicitly synthetic association binding.
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != syntheticAuth {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("ETag", `"`+digest(data)+`"`)
		http.ServeContent(w, r, "fixture", time.Time{}, bytes.NewReader(data))
	}))
	defer server.Close()
	download, err := New(server.URL, server.Client(), func(context.Context) (string, error) { return syntheticAuth, nil })
	if err != nil {
		t.Fatal(err)
	}
	j.DeliveryID, j.SnapshotID = "download", "download"
	if err := q.Admit(t.Context(), "download", j, nil); err != nil {
		t.Fatal(err)
	}
	part, err := q.file(j.DeliveryID+".part", os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data[:4]); err != nil {
		t.Fatal(err)
	}
	if err := part.Sync(); err != nil {
		t.Fatal(err)
	}
	_ = part.Close()
	r, journal, err := q.load(j.DeliveryID)
	if err != nil {
		t.Fatal(err)
	}
	r.Job.Offset = 4
	if err := q.save(journal, r); err != nil {
		t.Fatal(err)
	}
	_ = journal.Close()
	var delivered []byte
	binding = &AssociationBinding{Commit: func(_ context.Context, _ string, _ Reference, reader io.Reader) error {
		delivered, _ = io.ReadAll(reader)
		return nil
	}, Resolve: func(context.Context, string, Reference, bool) (AssociationOutcome, error) { return OutcomeAbsent, nil }}
	for i := 0; i < 2; i++ {
		if err := q.RunOne(t.Context(), j.DeliveryID, download, binding, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(delivered, data) {
		t.Fatal("Range/hash delivery differs")
	}
}

func TestWindowsNativeRejectsLinksAndTraversal(t *testing.T) {
	path := nativeFixtureRoot(t, false)
	q := nativeQueue(t, path)
	for _, name := range []string{"../outside", `a\b`, "stream:ads"} {
		f, err := nativeAt(q.root, name, windows.GENERIC_READ, windows.FILE_SHARE_READ, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE, false)
		if f != nil {
			_ = f.Close()
		}
		if !errors.Is(err, ErrInvalid) {
			t.Fatal("unsafe component accepted", name, err)
		}
	}
	f, err := q.file("fixture", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	original := filepath.Join(path, "deliveries-v1", "fixture")
	alias := filepath.Join(path, "deliveries-v1", "alias")
	if err := os.Link(original, alias); err != nil {
		t.Fatal("NTFS hardlink fixture", err)
	}
	if linked, err := q.file("alias", os.O_RDONLY, 0o600); err == nil {
		_ = linked.Close()
		t.Fatal("hardlink accepted")
	}
	t.Run("reparse", func(t *testing.T) {
		link := filepath.Join(path, "deliveries-v1", "symlink")
		if err := os.Symlink(original, link); err != nil {
			t.Skipf("OS does not permit fixture symlink creation; no privilege change: %v", err)
		}
		if linked, err := q.file("symlink", os.O_RDONLY, 0o600); err == nil {
			_ = linked.Close()
			t.Fatal("reparse file accepted")
		}
	})
}
