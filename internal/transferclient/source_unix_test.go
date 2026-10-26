//go:build linux || darwin

package transferclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/deliverymeta"
	"golang.org/x/sys/unix"
)

func makeSourceFixtureShared(t *testing.T, root string) {
	t.Helper()
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		mode := os.FileMode(0o660)
		if entry.IsDir() {
			mode = 0o770
		}
		return os.Chmod(path, mode)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTaskSourceRevocationNewProcess(t *testing.T) {
	if os.Getenv("MEMOH_REVOKED_SOURCE_CHILD") == "1" {
		calls := 0
		transport := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
			calls++
			return nil, ErrTransport
		}}
		status, err := AdvanceProducer(t.Context(), os.Getenv, "fixture", taskSourceLimits, &http.Client{Transport: transport}, time.Now())
		if err != nil || status.Job.Phase != "blocked" || status.Job.ErrorCode != "source_changed" || status.Job.ReceiptID != "" {
			t.Fatal("revoked source not blocked before network", status.Job.Phase, status.Job.ErrorCode, err)
		}
		if calls != 0 {
			t.Fatal("revoked source reached network")
		}
		raw, _ := json.Marshal(sourceProcessResult{PID: os.Getpid(), Status: status})
		_, _ = os.Stdout.Write(append(raw, '\n'))
		return
	}
	if os.Geteuid() == 0 {
		t.Skip("chmod revocation requires an ordinary OS identity; no identity substitution")
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
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(path, 0o600) }()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestTaskSourceRevocationNewProcess$") //nolint:gosec // Current isolated test executable and fixed test selector.
	cmd.Env = append(os.Environ(), "MEMOH_REVOKED_SOURCE_CHILD=1", "HERDR_PLUGIN_CONFIG_DIR="+getenv("HERDR_PLUGIN_CONFIG_DIR"), "HERDR_PLUGIN_STATE_DIR="+getenv("HERDR_PLUGIN_STATE_DIR"))
	out, err := cmd.CombinedOutput()
	var result sourceProcessResult
	line, _, _ := bytes.Cut(out, []byte{'\n'})
	if err != nil || json.Unmarshal(line, &result) != nil {
		t.Fatalf("revoke child: %v %s", err, out)
	}
	if result.PID == os.Getpid() || result.Status.Job.Task != registered.Job.Task || result.Status.Job.HostBinding != registered.Job.HostBinding || result.Status.Job.DeliveryID != registered.Job.DeliveryID {
		t.Fatal("revocation changed original identity")
	}
	t.Log("ordinary OS identity; new process reopens source; revoked source blocked; network_requests=0; original identity and binding retained")
}

func TestTaskSourceRejectsAliasesAndKeepsQueuePrivate(t *testing.T) {
	getenv, closed, _ := producerFixture(t, "https://fixture.invalid")
	writeProducerContextFixture(t, getenv, closed.Plan)
	cfg, err := OpenHostConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	root := cfg.sourcePath
	_ = cfg.Close()
	makeSourceFixtureShared(t, root)
	if _, guard, err := privateHostRoot(root); err == nil {
		closeQueueGuard(guard)
		t.Fatal("shared root accepted as private storage")
	}
	cfg, err = OpenHostConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cfg.Close() }()
	if f, err := cfg.OpenSource(closed.Artifact.SourceRef); err != nil {
		t.Fatal("authorized shared source rejected", err)
	} else {
		_ = f.Close()
	}
	if err := os.Symlink(filepath.Join(root, closed.Artifact.SourceRef), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if f, err := cfg.OpenSource("link"); err == nil {
		_ = f.Close()
		t.Fatal("source symlink accepted")
	}
	if err := unix.Mkfifo(filepath.Join(root, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if f, err := cfg.OpenSource("pipe"); err == nil {
		_ = f.Close()
		t.Fatal("special file accepted")
	}
	alias := filepath.Join(t.TempDir(), "source-alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	if r, g, err := openTaskSourceRoot(alias); err == nil {
		closeQueueGuard(g)
		_ = r.Close()
		t.Fatal("aliased task root accepted")
	}
	attempt, _ := deliverymeta.AttemptDirectory(closed.Task.Attempt)
	path := filepath.Join(root, filepath.FromSlash(attempt))
	if err := os.Rename(path, path+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path+"-original", path); err != nil {
		t.Fatal(err)
	}
	if _, _, _, closePlan, err := cfg.sourcePlan(closed.Task.Attempt); err == nil {
		closePlan()
		t.Fatal("attempt symlink accepted")
	}
	q, err := OpenHerdrQueue(getenv, "fixture", taskSourceLimits)
	if err != nil {
		t.Fatal(err)
	}
	queuePath := q.root.Name()
	_ = q.Close()
	if err := os.Chmod(queuePath, 0o770); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(queuePath, 0o700) }()
	if q, err := OpenHerdrQueue(getenv, "fixture", taskSourceLimits); !errors.Is(err, ErrPermission) {
		if q != nil {
			_ = q.Close()
		}
		t.Fatal("private queue protection changed", err)
	}
}
