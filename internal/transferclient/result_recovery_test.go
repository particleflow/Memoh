package transferclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/deliverymeta"
)

var recoveryTestLimits = QueueLimits{Jobs: 1, Bytes: 1 << 20, JournalBytes: 1 << 20, Workers: 1}

func resultRecoveryFixture(t *testing.T, origin string) (func(string) string, deliverymeta.ResultRecovery, []byte, string) {
	t.Helper()
	getenv, closed, write := producerFixture(t, origin)
	writeProducerContextFixture(t, getenv, closed.Plan)
	data := make([]byte, 131109)
	for i := range data {
		data[i] = byte(i*19 + i/17)
	} //nolint:gosec // Deliberate deterministic synthetic bytes.
	write("source", "explicit-result.png", data)
	cfg, err := OpenHostConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	root, binding := cfg.sourcePath, cfg.Binding
	_ = cfg.Close()
	attempt, _ := deliverymeta.AttemptDirectory(closed.Task.Attempt)
	identity := map[string]any{"attempt": closed.Task.Attempt, "generation": closed.Task.Generation, "request_hash": strings.Repeat("c", 64), "pi_session_id": strings.Repeat("d", 64), "workspace_id": "original-workspace", "pane_id": "original-pane", "terminal_id": "original-terminal"}
	completion := map[string]any{}
	for k, v := range identity {
		completion[k] = v
	}
	completion["version"], completion["result"] = 1, map[string]any{"outcome": "succeeded"}
	completionRaw, _ := json.Marshal(completion)
	identity["version"], identity["started"], identity["exited"], identity["exit_code"] = 1, true, true, 0
	identity["completion"] = map[string]any{"name": "result.json", "sha256": digest(completionRaw), "size": len(completionRaw)}
	receipt, _ := json.Marshal(identity)
	for name, raw := range map[string][]byte{"result.json": completionRaw, "receipt.json": receipt} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(attempt), name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	request := deliverymeta.ResultRecovery{Version: 1, Plan: closed.Plan, SourceRef: closed.Artifact.SourceRef, PhysicalName: "explicit-result.png", SHA256: digest(data), SizeBytes: int64(len(data)), HostBinding: binding, TargetFingerprint: strings.Repeat("e", 64), OperationFingerprint: strings.Repeat("f", 64), RequestHash: strings.Repeat("c", 64), ContextHash: deliverymeta.PlanHash(closed.Plan), Receipt: receipt, RemoteOutcome: "absent"}
	return getenv, request, data, root
}

func TestResultRecoveryProcessInterruptionAndSameID(t *testing.T) {
	if stage := os.Getenv("MEMOH_RESULT_RECOVERY_CHILD"); stage != "" {
		var request deliverymeta.ResultRecovery
		if json.Unmarshal([]byte(os.Getenv("MEMOH_RESULT_RECOVERY_REQUEST")), &request) != nil {
			os.Exit(71)
		}
		client, calls := filenameNoHTTP()
		_, _ = recoverResultSource(t.Context(), os.Getenv, "fixture", recoveryTestLimits, request, client, func(actual string) {
			if actual == stage && calls.Load() == 0 {
				os.Exit(81)
			}
		})
		os.Exit(72)
	}
	for _, stage := range []string{"recovery_validated", "registration_synced"} {
		t.Run(stage, func(t *testing.T) {
			getenv, request, data, root := resultRecoveryFixture(t, "https://fixture.invalid")
			filePlan, _ := request.FilePlan()
			encoded, _ := json.Marshal(request)
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestResultRecoveryProcessInterruptionAndSameID$") //nolint:gosec // Exact current test child and synthetic fixture only.
			cmd.Env = append(os.Environ(), "MEMOH_RESULT_RECOVERY_CHILD="+stage, "MEMOH_RESULT_RECOVERY_REQUEST="+string(encoded), "HERDR_PLUGIN_CONFIG_DIR="+getenv("HERDR_PLUGIN_CONFIG_DIR"), "HERDR_PLUGIN_STATE_DIR="+getenv("HERDR_PLUGIN_STATE_DIR"))
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 81 {
				t.Fatalf("interrupted stage %s: %v %s", stage, err, out)
			}
			client, calls := filenameNoHTTP()
			result, err := RecoverResultSource(t.Context(), getenv, "fixture", recoveryTestLimits, request, client)
			if err != nil || !result.Queue.Registered || result.Queue.Recovery == nil || result.Queue.Version != 6 || result.Queue.Job.DeliveryID != filePlan.DeliveryID() || result.Queue.Job.HostBinding != request.HostBinding {
				t.Fatal("same-ID restart mapping", err)
			}
			again, err := RecoverResultSource(t.Context(), getenv, "fixture", recoveryTestLimits, request, client)
			if err != nil || !sameResultMapping(again.Queue.Recovery, result.Queue.Recovery) || again.Queue.Job != result.Queue.Job {
				t.Fatal("repeat is not idempotent", err)
			}
			q, err := OpenHerdrQueue(getenv, "fixture", recoveryTestLimits)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = q.Close() }()
			cfg, err := OpenHostConfig(getenv)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cfg.Close() }()
			q.openSource, q.openRecoverySource = cfg.OpenSource, cfg.openResultMapping
			r, journal, err := q.load(filePlan.DeliveryID())
			if err != nil {
				t.Fatal(err)
			}
			r, err = q.prepareRegistered(t.Context(), r, journal, time.Now())
			_ = journal.Close()
			if err != nil || !r.Admitted || r.Recovery == nil {
				t.Fatal("mapped original indexing", err)
			}
			snapshot, closeSnapshot, err := q.openSnapshot(r)
			if err != nil {
				t.Fatal(err)
			}
			actual := make([]byte, len(data))
			err = snapshot.ReadAt(t.Context(), r.Job.SnapshotID, r.Job.LocatorRevision, r.Job.Spec, actual, 0)
			closeSnapshot()
			if err != nil || !bytes.Equal(actual, data) || calls.Load() != 0 {
				t.Fatal("original bytes/hash or network boundary", err)
			}
			// Force journal compaction with the mapped record, then reopen it.
			_, journal, err = q.load(r.Job.DeliveryID)
			if err != nil {
				t.Fatal(err)
			}
			if err := q.compactActiveJournal(journal, r); err != nil {
				t.Fatal("mapped checkpoint", err)
			}
			_ = journal.Close()
			reopened, err := q.Inspect(r.Job.DeliveryID)
			if err != nil || !sameResultMapping(reopened.Recovery, r.Recovery) {
				t.Fatal("compaction discarded mapping", err)
			}
			for _, name := range []string{r.Job.DeliveryID + ".snapshot", filePlan.ManifestName()} {
				path := filepath.Join(q.root.Name(), name)
				if name == filePlan.ManifestName() {
					path = filepath.Join(root, name)
				}
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("recovery copied payload or manufactured manifest", err)
				}
			}
			t.Logf("stage=%s; real child exit=81; same source_ref=%s deliveryID=%s; %dB sha256=%s; mapping retained through restart/index/compaction; HTTP=0 Pi=0", stage, request.SourceRef, filePlan.DeliveryID(), len(data), request.SHA256)
		})
	}
}

func TestResultRecoveryConflictsUnknownAndPermissions(t *testing.T) {
	for _, mode := range []string{"filename", "hash", "unknown-hash", "traversal", "root", "binding", "context", "receipt", "generation", "unknown-upload", "unknown-local-create", "existing-manifest", "revoked-source", "symlink", "hardlink"} {
		t.Run(mode, func(t *testing.T) {
			if runtime.GOOS == "windows" && (mode == "revoked-source" || mode == "symlink") {
				t.Skip("Unix chmod/symlink fixture; Windows ACL/reparse acceptance is separate")
			}
			getenv, request, _, root := resultRecoveryFixture(t, "https://fixture.invalid")
			client, calls := filenameNoHTTP()
			var before []byte
			if mode == "filename" || mode == "hash" || mode == "unknown-local-create" {
				result, err := RecoverResultSource(t.Context(), getenv, "fixture", recoveryTestLimits, request, client)
				if err != nil {
					t.Fatal(err)
				}
				q, err := OpenHerdrQueue(getenv, "fixture", recoveryTestLimits)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = q.Close() }()
				if mode == "unknown-local-create" {
					r, journal, err := q.load(result.Queue.Job.DeliveryID)
					if err != nil {
						t.Fatal(err)
					}
					r.Job.Phase = "create_unknown"
					err = q.save(journal, r)
					_ = journal.Close()
					if err != nil {
						t.Fatal(err)
					}
				}
				before, err = os.ReadFile(filepath.Join(q.root.Name(), queueName(result.Queue.Job.DeliveryID+".journal")))
				if err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "filename":
				request.PhysicalName = "different.png"
			case "hash":
				request.SHA256 = strings.Repeat("0", 64)
			case "unknown-hash":
				request.SHA256 = ""
			case "traversal":
				request.PhysicalName = "../explicit-result.png"
			case "root":
				request.Plan.Task.RootHash = strings.Repeat("0", 64)
			case "binding":
				request.HostBinding = strings.Repeat("0", 64)
			case "context":
				request.ContextHash = strings.Repeat("0", 64)
			case "receipt":
				request.Receipt = []byte(`{"started":true}`)
			case "generation":
				request.Plan.Task.Generation = strings.Repeat("c", 32)
			case "unknown-upload":
				request.RemoteOutcome = "unknown"
			case "existing-manifest":
				p, _ := request.FilePlan()
				if err := os.WriteFile(filepath.Join(root, p.ManifestName()), []byte("unknown prior closure"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "revoked-source":
				if os.Geteuid() == 0 {
					t.Skip("ordinary identity required; no identity substitution")
				}
				path := filepath.Join(root, request.PhysicalName)
				if err := os.Chmod(path, 0); err != nil {
					t.Fatal(err)
				}
				defer func() { _ = os.Chmod(path, 0o600) }()
			case "symlink":
				path := filepath.Join(root, request.PhysicalName)
				if err := os.Rename(path, path+"-original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+"-original", path); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(filepath.Join(root, request.PhysicalName), filepath.Join(root, "alias.png")); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := RecoverResultSource(t.Context(), getenv, "fixture", recoveryTestLimits, request, client); err == nil {
				t.Fatal("unsafe recovery accepted")
			}
			if calls.Load() != 0 {
				t.Fatal("rejected recovery reached HTTP")
			}
			if before != nil {
				p, _ := request.FilePlan()
				q, err := OpenHerdrQueue(getenv, "fixture", recoveryTestLimits)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = q.Close() }()
				after, err := os.ReadFile(filepath.Join(q.root.Name(), queueName(p.DeliveryID()+".journal")))
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("conflict/unknown modified original journal", err)
				}
			}
		})
	}
}

func TestResultRecoveryExistingReceiptGetOnly(t *testing.T) {
	for _, denial := range []bool{false, true} {
		t.Run(map[bool]string{false: "observed", true: "revoked"}[denial], func(t *testing.T) {
			var request deliverymeta.ResultRecovery
			gets, mutations := 0, 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					mutations++
					w.WriteHeader(500)
					return
				}
				gets++
				if denial {
					w.WriteHeader(403)
					return
				}
				p, _ := request.FilePlan()
				if r.URL.Path != "/internal/computer-transfers/"+p.Task.OperationID+"/"+p.DeliveryID()+"/bots/"+p.Task.BotID+"/media/uploads/original-receipt" || r.Header.Get("Authorization") != "Memoh-Transfer fixture-stable-device" {
					t.Error("original receipt scope/identity lost")
					w.WriteHeader(403)
					return
				}
				_ = json.NewEncoder(w).Encode(Receipt{ID: "original-receipt", State: "uploading", Mime: p.Artifact.Mime, SizeBytes: request.SizeBytes, Offset: 17, ExpiresAt: time.Now().Add(time.Hour)})
			}))
			defer server.Close()
			getenv, req, _, _ := resultRecoveryFixture(t, server.URL)
			request = req
			registered, err := RecoverResultSource(t.Context(), getenv, "fixture", recoveryTestLimits, request, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			q, err := OpenHerdrQueue(getenv, "fixture", recoveryTestLimits)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = q.Close() }()
			r, journal, err := q.load(registered.Queue.Job.DeliveryID)
			if err != nil {
				t.Fatal(err)
			}
			r.Job.Phase, r.Job.ReceiptID, r.Job.Offset, r.Job.ChunkBytes = "uploading", "original-receipt", 17, 65536
			if err := q.save(journal, r); err != nil {
				t.Fatal(err)
			}
			_ = journal.Close()
			path := filepath.Join(q.root.Name(), queueName(r.Job.DeliveryID+".journal"))
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			request.RemoteOutcome, request.KnownReceipt = "existing", "original-receipt"
			observed, err := RecoverResultSource(t.Context(), getenv, "fixture", recoveryTestLimits, request, server.Client())
			if denial && err == nil || !denial && (err != nil || observed.ReceiptObserved == nil || observed.ReceiptObserved.Offset != 17) {
				t.Fatal("receipt readback", err)
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil || !bytes.Equal(before, after) || gets != 1 || mutations != 0 {
				t.Fatal("receipt reconciliation wrote state or mutated transport", readErr, gets, mutations)
			}
			// Existing GET middleware would reserve an absent NAS entry: block
			// this case locally rather than calling an apparently read-only GET.
			request.RemoteOutcome, request.KnownReceipt = "absent", ""
			if _, err := RecoverResultSource(t.Context(), getenv, "fixture", recoveryTestLimits, request, server.Client()); err == nil || gets != 1 {
				t.Fatal("empty ledger caused unsafe GET")
			}
			t.Log("original receipt GET=1 Create/PATCH/Complete/Commit/Resolve=0; journal bytes unchanged; absent NAS binding stops before GET")
		})
	}
}

func TestResultRecoveryReopenRevalidatesProofAndSource(t *testing.T) {
	for _, changed := range []string{"producer-context.json", "receipt.json", "result.json", "source-bytes", "source-revoked"} {
		t.Run(changed, func(t *testing.T) {
			if changed == "source-revoked" && (os.Geteuid() == 0 || runtime.GOOS == "windows") {
				t.Skip("ordinary identity required")
			}
			getenv, request, _, root := resultRecoveryFixture(t, "https://fixture.invalid")
			client, calls := filenameNoHTTP()
			registered, err := RecoverResultSource(t.Context(), getenv, "fixture", recoveryTestLimits, request, client)
			if err != nil {
				t.Fatal(err)
			}
			attempt, _ := deliverymeta.AttemptDirectory(request.Plan.Task.Attempt)
			path := filepath.Join(root, filepath.FromSlash(attempt), changed)
			if strings.HasPrefix(changed, "source-") {
				path = filepath.Join(root, request.PhysicalName)
			}
			if changed == "source-revoked" {
				if err := os.Chmod(path, 0); err != nil {
					t.Fatal(err)
				}
				defer func() { _ = os.Chmod(path, 0o600) }()
			} else {
				if err := os.WriteFile(path, []byte("changed after durable mapping"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			// Entirely fresh configured invocation reopens proofs and original
			// source before existing worker indexing or any HTTP can start.
			status, err := InvokeConfiguredHerdr(t.Context(), getenv, "fixture", recoveryTestLimits, ConfiguredAction{Kind: "advance", DeliveryID: registered.Queue.Job.DeliveryID}, client, time.Now())
			if err != nil || status.Job.Phase != "blocked" || status.Job.ErrorCode != "source_changed" || status.Job.DeliveryID != registered.Queue.Job.DeliveryID || status.Job.HostBinding != request.HostBinding || calls.Load() != 0 {
				t.Fatal("resumed mapping bypassed changed source/proof", status.Job.Phase, status.Job.ErrorCode, err)
			}
		})
	}
}
