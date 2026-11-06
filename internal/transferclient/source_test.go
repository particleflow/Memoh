package transferclient

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/deliverymeta"
)

var taskSourceLimits = QueueLimits{Jobs: 2, Bytes: 1, JournalBytes: 1 << 20, Workers: 1}

type sourceProcessResult struct {
	PID    int         `json:"pid"`
	Status QueueStatus `json:"status"`
	Error  string      `json:"error,omitempty"`
}

func readSourceFixture(rootPath, name string) ([]byte, error) {
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return root.ReadFile(name)
}

// Real processes/storage/bytes over a loopback TLS protocol fixture. This is
// neither the production Server/PG nor a Windows SMB acceptance claim.
func TestTaskSourceProcessRecovery(t *testing.T) {
	if mode := os.Getenv("MEMOH_TASK_SOURCE_CHILD"); mode != "" {
		cfg, err := OpenHostConfig(os.Getenv)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = cfg.Close() }()
		if mode == "register-crash" {
			names, err := cfg.producerManifests()
			if err != nil || len(names) != 1 {
				t.Fatal("closed manifest", err)
			}
			raw, err := cfg.readProducerManifest(names[0], "fixture")
			if err != nil {
				t.Fatal(err)
			}
			action, _, err := ProducerAdmission(bytes.NewReader(raw), "fixture")
			if err != nil {
				t.Fatal(err)
			}
			action.Job.HostBinding = cfg.Binding
			q, err := OpenHerdrQueue(os.Getenv, "fixture", taskSourceLimits)
			if err != nil {
				t.Fatal(err)
			}
			q.journalHook = func(stage string) {
				if stage == "registration_synced" {
					os.Exit(74) // Lose the registration acknowledgment, keep source metadata.
				}
			}
			_, _ = q.RegisterUpload(t.Context(), action.Job, action.SourceRef, cfg.OpenSource)
			t.Fatal("registration crash hook not reached")
		}
		der, err := base64.StdEncoding.DecodeString(os.Getenv("MEMOH_TASK_SOURCE_CA"))
		if err != nil {
			t.Fatal(err)
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatal(err)
		}
		roots := x509.NewCertPool()
		roots.AddCert(cert)
		transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
		defer transport.CloseIdleConnections()
		status, collectErr := CollectProducer(t.Context(), os.Getenv, "fixture", taskSourceLimits, &http.Client{Transport: transport}, time.Now().Add(time.Hour))
		out := sourceProcessResult{PID: os.Getpid(), Status: status}
		if collectErr != nil {
			if !errors.Is(collectErr, ErrNoWork) {
				t.Fatal(collectErr)
			}
			out.Error = "no_work"
		}
		if status.Job.DeliveryID != "" && PublishProducerFeedback(t.Context(), os.Getenv, "fixture", taskSourceLimits, status.Job, time.Now()) != nil {
			t.Fatal("feedback unavailable")
		}
		raw, _ := json.Marshal(out)
		_, _ = os.Stdout.Write(append(raw, '\n'))
		return
	}
	f := &fixture{calls: map[string]int{}}
	var closed deliverymeta.Closed
	var commits atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := "/internal/computer-transfers/" + closed.Task.OperationID + "/" + closed.DeliveryID()
		if r.Header.Get("Authorization") != "Memoh-Transfer fixture-stable-device" || !strings.HasPrefix(r.URL.Path, prefix+"/") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if strings.Contains(r.URL.Path, "/internal/computer-deliveries/") {
			if strings.HasSuffix(r.URL.Path, "/commit") {
				commits.Add(1)
				w.WriteHeader(499) // The same task must resolve this lost acknowledgment.
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"version": 3, "revision": "1", "outcome": "committed"})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/limits") {
			_ = json.NewEncoder(w).Encode(Limits{MaxUploadBytes: 1 << 20, ResumableEnabled: true, ChunkMaxBytes: 65536})
			return
		}
		r.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
		r.Header.Set("Authorization", syntheticAuth)
		f.serve(w, r)
	}))
	defer server.Close()
	getenv, c, write := producerFixture(t, server.URL)
	closed = c
	data := make([]byte, 2*65536+37)
	for i := range data {
		data[i] = byte((i*73 + i/257) % 256)
	}
	closed.SizeBytes, closed.SHA256 = int64(len(data)), digest(data)
	write("source", closed.Artifact.SourceRef, data)
	writeProducerContextFixture(t, getenv, closed.Plan)
	raw, _ := json.Marshal(closed)
	write("source", closed.ManifestName(), raw)
	cfg, err := OpenHostConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	root, binding := cfg.sourcePath, cfg.Binding
	_ = cfg.Close()
	// The executable workspace retains its OS permissions; no root is made read-only.
	makeSourceFixtureShared(t, root)
	configRoot := getenv("HERDR_PLUGIN_CONFIG_DIR")
	configBytes, err := readSourceFixture(configRoot, "transfer.json")
	if err != nil {
		t.Fatal(err)
	}
	run := func(mode string) sourceProcessResult {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestTaskSourceProcessRecovery$") //nolint:gosec // Current test executable, fixed selector and isolated synthetic roots.
		cmd.Env = append(os.Environ(), "MEMOH_TASK_SOURCE_CHILD="+mode, "MEMOH_TASK_SOURCE_CA="+base64.StdEncoding.EncodeToString(server.Certificate().Raw), "HERDR_PLUGIN_CONFIG_DIR="+getenv("HERDR_PLUGIN_CONFIG_DIR"), "HERDR_PLUGIN_STATE_DIR="+getenv("HERDR_PLUGIN_STATE_DIR"))
		out, err := cmd.CombinedOutput()
		if mode == "register-crash" {
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 74 {
				t.Fatalf("crash hook: %v %s", err, out)
			}
			return sourceProcessResult{PID: cmd.Process.Pid}
		}
		var result sourceProcessResult
		line, _, _ := bytes.Cut(out, []byte{'\n'})
		if err != nil || json.Unmarshal(line, &result) != nil {
			t.Fatalf("child: %v %s", err, out)
		}
		return result
	}
	crashed := run("register-crash")
	if kept, err := readSourceFixture(root, closed.ManifestName()); err != nil || !bytes.Equal(kept, raw) {
		t.Fatal("registration death consumed manifest", err)
	}
	f.setDrop("PATCH /bots/" + closed.Task.BotID + "/media/uploads/receipt-1")
	first := run("collect")
	if !first.Status.Admitted || first.Status.Job.ReceiptID != "receipt-1" || first.Status.Job.Offset != 0 || fixtureReceipt(f).Offset != 65536 {
		t.Fatal("lost PATCH not pending", first.Status.Job.Phase, first.Status.Job.Offset)
	}
	if _, err := os.Stat(filepath.Join(root, closed.ManifestName())); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("registered manifest not consumed", err)
	}
	// Recreate only the original metadata to model a lost unlink acknowledgment.
	write("source", closed.ManifestName(), raw)
	var recovered sourceProcessResult
	for range 4 {
		recovered = run("collect")
		if recovered.Status.Associated {
			break
		}
	}
	if !recovered.Status.Associated || first.PID == recovered.PID || crashed.PID == first.PID || recovered.Status.Job.DeliveryID != closed.DeliveryID() || recovered.Status.Job.HostBinding != binding || recovered.Status.Job.Task != first.Status.Job.Task || recovered.Status.Job.OperationID != closed.Task.OperationID || recovered.Status.Job.ReceiptID != first.Status.Job.ReceiptID || commits.Load() != 1 || !bytes.Equal(fixtureReady(f), data) {
		t.Fatal("original identity/bytes not recovered", recovered.Status.Job.Phase, recovered.Status.AssociationState)
	}
	create := f.count("POST /bots/" + closed.Task.BotID + "/media/uploads")
	patch := f.count("PATCH /bots/" + closed.Task.BotID + "/media/uploads/receipt-1")
	complete := f.count("POST /bots/" + closed.Task.BotID + "/media/uploads/receipt-1/complete")
	if create != 1 || patch != 3 || complete != 1 || run("collect").Error != "no_work" || commits.Load() != 1 {
		t.Fatal("duplicate consumption/create/association", create, patch, complete)
	}
	unchanged, err := readSourceFixture(configRoot, "transfer.json")
	source, sourceErr := readSourceFixture(root, closed.Artifact.SourceRef)
	if err != nil || sourceErr != nil || !bytes.Equal(configBytes, unchanged) || !bytes.Equal(source, data) {
		t.Fatal("source or original binding modified")
	}
	feedback := readProducerFeedbackFixture(t, getenv, closed.Plan)
	if len(feedback.Files) != 1 || feedback.Files[0].DeliveryID != closed.DeliveryID() {
		t.Fatal("shared-source advisory lost its original declaration")
	}
	_ = filepath.WalkDir(getenv("HERDR_PLUGIN_STATE_DIR"), func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(entry.Name(), ".snapshot") || strings.HasSuffix(entry.Name(), ".part") {
			t.Fatal("artifact copied into private queue")
		}
		return nil
	})
	t.Logf("actual_bytes=%d sha256=%s create=%d patch=%d complete=%d commit=%d original_identity=true new_processes=true source_preserved=true config_bytes_unchanged=true snapshot_copies=0", len(data), digest(data), create, patch, complete, commits.Load())
}

func TestTaskSourceManifestAndFeedbackScope(t *testing.T) {
	for _, kind := range []string{"missing-context", "wrong-operation", "wrong-attempt", "wrong-root", "undeclared", "replacement", "feedback-binding", "feedback-link"} {
		t.Run(kind, func(t *testing.T) {
			getenv, closed, write := producerFixture(t, "https://fixture.invalid")
			plan := closed.Plan
			switch kind {
			case "wrong-operation":
				plan.Task.OperationID = plan.Task.BotID
			case "wrong-attempt":
				plan.Task.Attempt = strings.Repeat("c", 32)
			case "wrong-root":
				plan.Task.RootHash = strings.Repeat("0", 64)
			case "undeclared":
				plan.Artifact.SourceRef = "other-result"
			}
			if kind != "missing-context" {
				writeProducerContextFixture(t, getenv, plan)
			}
			raw, _ := json.Marshal(closed)
			write("source", closed.ManifestName(), raw)
			cfg, err := OpenHostConfig(getenv)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cfg.Close() }()
			switch {
			case strings.HasPrefix(kind, "feedback-"):
				action, _, _ := ProducerAdmission(bytes.NewReader(raw), "fixture")
				action.Job.HostBinding = cfg.Binding
				if kind == "feedback-binding" {
					action.Job.HostBinding = strings.Repeat("0", 64)
				} else {
					rel, _ := deliverymeta.AttemptDirectory(plan.Task.Attempt)
					if err := os.Link(filepath.Join(cfg.sourcePath, closed.Artifact.SourceRef), filepath.Join(cfg.sourcePath, filepath.FromSlash(rel), deliverymeta.ProducerFeedbackName)); err != nil {
						t.Fatal(err)
					}
				}
				if err := PublishProducerFeedback(t.Context(), getenv, "fixture", taskSourceLimits, action.Job, time.Now()); !errors.Is(err, ErrProducerFeedback) {
					t.Fatal("foreign feedback accepted", err)
				}
			case kind == "replacement":
				if _, err := cfg.readProducerManifest(closed.ManifestName(), "fixture"); err != nil {
					t.Fatal(err)
				}
				changed := append(bytes.Clone(raw), '\n')
				write("source", closed.ManifestName(), changed)
				if err := cfg.consumeSourceManifest(closed.ManifestName(), raw); !errors.Is(err, ErrProducerManifest) {
					t.Fatal("replacement consumed", err)
				}
			default:
				if _, err := cfg.readProducerManifest(closed.ManifestName(), "fixture"); !errors.Is(err, ErrProducerManifest) {
					t.Fatal("unbound manifest accepted", err)
				}
				if err := cfg.consumeSourceManifest(closed.ManifestName(), raw); !errors.Is(err, ErrProducerManifest) {
					t.Fatal("unbound manifest consumed", err)
				}
			}
			kept, err := readSourceFixture(cfg.sourcePath, closed.Artifact.SourceRef)
			if err != nil || string(kept) != "fixture" {
				t.Fatal("original artifact changed", err)
			}
			if _, err := os.Stat(filepath.Join(cfg.sourcePath, closed.ManifestName())); err != nil {
				t.Fatal("unconsumed evidence missing", err)
			}
		})
	}
}

func TestTaskSourceFileCapabilities(t *testing.T) {
	getenv, closed, _ := producerFixture(t, "https://fixture.invalid")
	cfg, err := OpenHostConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cfg.Close() }()
	f, err := cfg.OpenSource(closed.Artifact.SourceRef)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte("x"), 0); err == nil {
		t.Fatal("artifact handle writable")
	}
	_ = f.Close()
	for _, name := range []string{"../outside", `a\b`, "stream:ads", ".", "..", "/absolute"} {
		if f, err := openSourceFile(cfg.sources, name, os.O_RDONLY, false); err == nil {
			_ = f.Close()
			t.Fatal("unsafe source component", name)
		}
	}
	for _, mode := range []struct {
		flags int
		del   bool
	}{{os.O_RDWR, false}, {os.O_CREATE | os.O_RDWR, false}, {os.O_RDONLY, true}} {
		if f, err := openSourceFile(cfg.sources, closed.Artifact.SourceRef, mode.flags, mode.del); err == nil {
			_ = f.Close()
			t.Fatal("artifact granted mutation capability")
		}
	}
	if err := os.Link(filepath.Join(cfg.sourcePath, closed.Artifact.SourceRef), filepath.Join(cfg.sourcePath, "alias")); err != nil {
		t.Fatal(err)
	}
	if f, err := cfg.OpenSource("alias"); err == nil {
		_ = f.Close()
		t.Fatal("source with multiple hard links accepted")
	}
	if f, err := cfg.OpenSource(closed.Artifact.SourceRef); err == nil {
		_ = f.Close()
		t.Fatal("multiply-linked original accepted")
	}
}
