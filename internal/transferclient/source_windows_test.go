//go:build windows

package transferclient

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/felinics/memoh/internal/deliverymeta"
)

// Only synthetic temporary fixtures are changed; production code never edits ACLs.
func makeSourceFixtureShared(t *testing.T, root string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsNativeTaskSourceBoundary(t *testing.T) {
	path := nativeFixtureRoot(t, true)
	root, guard, err := openTaskSourceRoot(path)
	if err != nil {
		t.Fatal("OS-authorized source rejected for non-private ACL", err)
	}
	defer func() { closeQueueGuard(guard); _ = root.Close() }()
	if err := checkQueueRoot(root); err == nil {
		t.Fatal("shared source accepted as private queue")
	}
	if err := os.WriteFile(filepath.Join(path, "result-one"), []byte("native-source-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := openSourceFile(root, "result-one", os.O_RDONLY, false)
	if err != nil {
		t.Fatal(err)
	}
	if writer, err := os.OpenFile(filepath.Join(path, "result-one"), os.O_RDWR, 0); err == nil {
		_ = writer.Close()
		t.Fatal("open artifact allowed concurrent writer")
	}
	_ = f.Close()
	name := deliverymeta.ManifestPrefix + digest([]byte("native-manifest"))
	if err := os.WriteFile(filepath.Join(path, name), []byte("native-manifest"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err = openSourceFile(root, name, os.O_RDONLY, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := removeSourceManifest(root, deliverymeta.ManifestPrefix+digest([]byte("other-manifest")), f); err == nil {
		t.Fatal("manifest handle/name mismatch accepted")
	}
	if err := os.Rename(filepath.Join(path, name), filepath.Join(path, "renamed")); err == nil {
		t.Fatal("pinned manifest could be renamed")
	}
	if err := removeSourceManifest(root, name, f); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if _, err := root.Lstat(name); !os.IsNotExist(err) {
		t.Fatal("verified manifest not consumed", err)
	}
	if data, err := os.ReadFile(filepath.Join(path, "result-one")); err != nil || string(data) != "native-source-bytes" {
		t.Fatal("artifact modified", err)
	}
	if err := syncSourceDir(root); err != nil {
		t.Fatal("native source metadata flush", err)
	}
}

func TestWindowsNativeTaskSourceProcesses(t *testing.T) {
	TestTaskSourceProcessRecovery(t)
}

func TestWindowsNativeTaskSourceScope(t *testing.T) {
	t.Run("manifest-feedback", TestTaskSourceManifestAndFeedbackScope)
	t.Run("file-capabilities", TestTaskSourceFileCapabilities)
	t.Run("configuration", TestProducerConfigurationCannotChangeBetweenCheckAndAdmission)
}

func TestWindowsNativeTaskSourceRevocation(t *testing.T) {
	if os.Getenv("MEMOH_REVOKED_NATIVE_SOURCE_CHILD") == "1" {
		calls := 0
		transport := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
			calls++
			return nil, ErrTransport
		}}
		status, err := AdvanceProducer(t.Context(), os.Getenv, "fixture", taskSourceLimits, &http.Client{Transport: transport}, time.Now())
		if err != nil || status.Job.Phase != "blocked" || status.Job.ErrorCode != "source_changed" || status.Job.ReceiptID != "" || calls != 0 {
			t.Fatal("revoked native source not blocked before network", status.Job.Phase, status.Job.ErrorCode, err)
		}
		raw, _ := json.Marshal(sourceProcessResult{PID: os.Getpid(), Status: status})
		_, _ = os.Stdout.Write(append(raw, '\n'))
		return
	}
	getenv, closed, _ := producerFixture(t, "https://fixture.invalid")
	raw, _ := json.Marshal(closed)
	registered, err := EnqueueProducer(t.Context(), getenv, "fixture", taskSourceLimits, bytes.NewReader(raw), nil, time.Now())
	if err != nil || !registered.Registered || registered.Admitted {
		t.Fatal("registration", err)
	}
	cfg, err := OpenHostConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfg.sourcePath, closed.Artifact.SourceRef)
	_ = cfg.Close()
	original, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	oldACL, _, err := original.DACL()
	if err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := user.User.Sid.String()
	denied, err := windows.SecurityDescriptorFromString("D:P(D;;FR;;;" + sid + ")(A;;FA;;;" + sid + ")")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := denied.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, oldACL, nil); err != nil {
			t.Error("restore synthetic fixture ACL", err)
		}
	}()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestWindowsNativeTaskSourceRevocation$") //nolint:gosec // Current test executable and fixed action; synthetic private fixture only.
	cmd.Env = append(os.Environ(), "MEMOH_REVOKED_NATIVE_SOURCE_CHILD=1", "HERDR_PLUGIN_CONFIG_DIR="+getenv("HERDR_PLUGIN_CONFIG_DIR"), "HERDR_PLUGIN_STATE_DIR="+getenv("HERDR_PLUGIN_STATE_DIR"))
	out, err := cmd.CombinedOutput()
	line, _, _ := bytes.Cut(out, []byte{'\n'})
	var result sourceProcessResult
	if err != nil || json.Unmarshal(line, &result) != nil {
		t.Fatalf("native revoke child: %v %s", err, out)
	}
	if result.PID == os.Getpid() || result.Status.Job.Task != registered.Job.Task || result.Status.Job.HostBinding != registered.Job.HostBinding || result.Status.Job.DeliveryID != registered.Job.DeliveryID {
		t.Fatal("revocation changed original identity")
	}
}
