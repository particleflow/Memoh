//go:build integration

package db_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/computertask"
	memohdb "github.com/felinics/memoh/internal/db"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/handlers"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
	"github.com/felinics/memoh/internal/team"
	"github.com/felinics/memoh/internal/toolcontext"
	"github.com/felinics/memoh/internal/transferclient"
)

// Tests ONLY the 3ca receipt gate against real PostgreSQL, registry and handlers.
// Linux source/proof fixtures do not exercise Windows paths or native owner
// preparation/dispatch (already separate contracts). No field operation is used.
func TestResultRecoveryReceiptPostgresHandlerBoundary(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("isolated Linux private-journal fixture; Windows acceptance is separate")
	}
	f := newNativeHerdrFixture(t)
	ctx := t.Context()
	profile := agenvo.NativeHerdrProfile{InstanceID: "synthetic-native-instance", Session: "synthetic", WorkDir: `C:\Synthetic`, BashPath: `C:\Synthetic\Git\bin\bash.exe`, HerdrCLI: `C:\Synthetic\herdr.exe`, HerdrConfigRoot: `C:\Synthetic\herdr`, Agent: agenvo.AgentProfile{ExecutionMode: "pi-json-v1", Provider: "synthetic", Model: "synthetic", ConfigDir: `C:\Synthetic\Pi`}}
	f.config, _ = json.Marshal(map[string]any{"herdr": profile})
	f.exec(t, "UPDATE user_runtimes SET provider_config=$1 WHERE id=$2", []byte(f.config), f.runtime)
	f.exec(t, "UPDATE bot_sessions SET created_by_user_id=$1 WHERE id=$2", f.owner, f.session)
	target, err := agenvo.DecodeNativeTarget(f.runtime, f.config)
	if err != nil {
		t.Fatal(err)
	}
	rls := rlsConn(t, f.pool, teamMigrationDSN(t))
	var role string
	var super, bypass bool
	if err := rls.QueryRow(ctx, "SELECT current_user,rolsuper,rolbypassrls FROM pg_roles WHERE rolname=current_user").Scan(&role, &super, &bypass); err != nil || super || bypass {
		t.Fatal("handler role must be NOSUPERUSER NOBYPASSRLS", err)
	}
	config := f.pool.Config().Copy()
	config.ConnConfig.RuntimeParams["role"] = role
	config.ConnConfig.RuntimeParams["memoh.team_id"] = team.DefaultTeamID
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store, err := postgresstore.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	id := computertask.DeliveryIdentity{Binding: toolcontext.ComputerBinding{BotID: f.bot, UserID: f.owner, SessionID: f.session, TargetID: f.mount, RunID: "original-synthetic-run"}, DeviceID: target.DeviceID, InstanceID: target.InstanceID, Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32)}
	verifier := newRegisteredTransferVerifier(ctx, t, store, id)
	root := t.TempDir()
	originals := filepath.Join(root, "nas-originals")
	assets := media.NewService(nil, localfs.NewOriginals(originals))
	assets.SetRequirePersistence(true)
	if err := assets.EnableUploads(originals); err != nil { //nolint:contextcheck // Initializer only; real handler requests supply their own contexts.
		t.Fatal(err)
	}
	defer func() { _ = assets.Uploads().Close() }()
	base := computertask.NewPostgresDeliveryService(store, assets)
	log := slog.New(slog.DiscardHandler)
	messages := handlers.NewMessageHandler(log, nil, nil, nil, nil)
	messages.SetMediaService(assets)
	handler, err := handlers.NewDeviceComputerTransferHandler(ctx, base, messages, verifier, log)
	if err != nil {
		t.Fatal(err)
	}
	defer handler.Close()
	endpoint := server.NewServer(log, ":0", "synthetic-recovery-only", handler) //nolint:contextcheck // Actual requests carry their own contexts.
	var calls, gets, mutations atomic.Int64
	var denialLeaked atomic.Bool
	https := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/media/uploads/") {
			gets.Add(1)
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			mutations.Add(1)
		}
		response := httptest.NewRecorder()
		endpoint.ServeHTTP(response, r)
		if response.Code >= 400 {
			for _, private := range []string{verifier.token, `"size_bytes"`, `"offset"`, `"sha256"`} {
				if bytes.Contains(response.Body.Bytes(), []byte(private)) {
					denialLeaked.Store(true)
				}
			}
		}
		for key, values := range response.Header() {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(response.Code)
		_, _ = w.Write(response.Body.Bytes())
	}))
	defer https.Close()
	limits := transferclient.QueueLimits{Jobs: 1, Bytes: 1 << 20, JournalBytes: 1 << 20, Workers: 1}
	data := []byte("original synthetic file; no payload upload is needed for receipt readback")
	sum := func(raw []byte) string { hash := sha256.Sum256(raw); return hex.EncodeToString(hash[:]) }
	write := func(path string, raw []byte) {
		t.Helper()
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	type task struct {
		op       dbstore.ComputerOperationRecord
		request  deliverymeta.ResultRecovery
		getenv   func(string) string
		state    string
		queue    transferclient.QueueStatus
		revision string
		limits   transferclient.QueueLimits
	}
	newTask := func(t *testing.T, revisions ...string) task {
		t.Helper()
		revision, queueLimits := "fixture", limits
		if len(revisions) != 0 {
			revision, queueLimits = revisions[0], transferclient.DefaultHostQueueLimits()
		}
		dir := t.TempDir()
		config, state, source := filepath.Join(dir, "config"), filepath.Join(dir, "state"), filepath.Join(dir, "source")
		for _, path := range []string{config, state, source} {
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		settings, _ := json.Marshal(map[string]any{"version": 1, "origin": https.URL, "source_root": source})
		write(filepath.Join(config, "transfer.json"), settings)
		write(filepath.Join(config, "device.credential"), []byte(verifier.token))
		op := f.operation(f.mount)
		op.RunID = id.Binding.RunID
		op.Fingerprint = sum([]byte("synthetic original task " + op.ID))
		op.TargetFingerprint = agenvo.TargetFingerprint(target)
		plan := deliverymeta.Plan{Version: 1, Task: deliverymeta.Task{OperationID: op.ID, BotID: f.bot, UserID: f.owner, SessionID: f.session, TargetID: f.mount, RunID: op.RunID, Attempt: id.Attempt, Generation: id.Generation, RootHash: deliverymeta.RootHash(source)}, Artifact: deliverymeta.Artifact{SourceRef: "legacy_result", Mime: "application/octet-stream"}}
		progress := agenvo.BatchProgress{Producer: &plan, LayoutVersion: 1, CompletionVersion: 1, Attempt: id.Attempt, Generation: id.Generation, RequestHash: sum([]byte("original batch request")), PiSessionID: sum([]byte("original Pi session")), WorkspaceID: "original-workspace", PaneID: "original-pane", TerminalID: "original-terminal", ControlPane: "original-control", ControlTerminal: "original-control-terminal"}
		stdout := agenvo.BatchFile{Name: "stdout.jsonl", Hash: sum(nil), Size: 0}
		stderr := agenvo.BatchFile{Name: "stderr.txt", Hash: sum(nil), Size: 0}
		result := agenvo.PiJSONResult{Outcome: "succeeded", StopReason: "stop", Records: 8}
		completion, _ := json.Marshal(agenvo.BatchCompletion{Version: 1, Attempt: id.Attempt, Generation: id.Generation, RequestHash: progress.RequestHash, PiSessionID: progress.PiSessionID, WorkspaceID: progress.WorkspaceID, PaneID: progress.PaneID, TerminalID: progress.TerminalID, Stdout: stdout, Result: result})
		receipt := agenvo.BatchReceipt{Version: 1, Attempt: id.Attempt, Generation: id.Generation, RequestHash: progress.RequestHash, PiSessionID: progress.PiSessionID, WorkspaceID: progress.WorkspaceID, PaneID: progress.PaneID, TerminalID: progress.TerminalID, Started: true, Exited: true, Stdout: stdout, Stderr: stderr, Completion: &agenvo.BatchFile{Name: "result.json", Hash: sum(completion), Size: int64(len(completion))}}
		if _, err := agenvo.ParseBatchCompletion(completion, progress, receipt); err != nil {
			t.Fatal(err)
		}
		op.Details, _ = json.Marshal(computertask.Details{Kind: "herdr_pi_task", Phase: "execution-completed", Batch: &computertask.BatchDetails{Progress: progress, Receipt: &receipt, Result: &result}})
		var won bool
		op, won, err = store.ClaimComputerOperation(ctx, op)
		if err != nil || !won {
			t.Fatal("original isolated operation", err)
		}
		op.State = "completed"
		op, err = store.SaveComputerOperation(ctx, op)
		if err != nil {
			t.Fatal(err)
		}
		attempt, _ := deliverymeta.AttemptDirectory(id.Attempt)
		attemptDir := filepath.Join(source, filepath.FromSlash(attempt))
		if err := os.MkdirAll(attemptDir, 0o700); err != nil {
			t.Fatal(err)
		}
		planRaw, _ := json.Marshal(plan)
		receiptRaw, _ := json.Marshal(receipt)
		write(filepath.Join(attemptDir, deliverymeta.ProducerContextName), planRaw)
		write(filepath.Join(attemptDir, "receipt.json"), receiptRaw)
		write(filepath.Join(attemptDir, "result.json"), completion)
		write(filepath.Join(source, "explicit.bin"), data)
		request := deliverymeta.ResultRecovery{Version: 1, Plan: plan, SourceRef: plan.Artifact.SourceRef, PhysicalName: "explicit.bin", SHA256: sum(data), SizeBytes: int64(len(data)), HostBinding: sum(settings), TargetFingerprint: op.TargetFingerprint, RequestHash: progress.RequestHash, OperationFingerprint: op.Fingerprint, ContextHash: sum(planRaw), Receipt: receiptRaw, RemoteOutcome: "absent"}
		getenv := func(key string) string {
			if key == "HERDR_PLUGIN_CONFIG_DIR" {
				return config
			}
			if key == "HERDR_PLUGIN_STATE_DIR" {
				return state
			}
			return ""
		}
		registered, err := transferclient.RecoverResultSource(ctx, getenv, revision, queueLimits, request, https.Client())
		if err != nil || registered.Queue.Version != 6 {
			t.Fatal("original-ID journal v6", err)
		}
		return task{op: op, request: request, getenv: getenv, state: state, queue: registered.Queue, revision: revision, limits: queueLimits}
	}
	projection := func(t *testing.T, item task) deliverymeta.ResultRecovery {
		t.Helper()
		request := item.request
		entry, err := store.ReadComputerDeliveryEntry(ctx, item.op, item.queue.Job.DeliveryID)
		if errors.Is(err, memohdb.ErrNotFound) {
			request.RemoteOutcome, request.KnownReceipt = "absent", ""
			return request
		}
		if err != nil {
			t.Fatal(err)
		}
		var row struct {
			Transfer *struct {
				UploadID string `json:"upload_id"`
				Creating bool   `json:"creating"`
			} `json:"transfer"`
		}
		if json.Unmarshal(entry.Data, &row) != nil {
			t.Fatal("invalid real ledger projection")
		}
		request.RemoteOutcome, request.KnownReceipt = "unknown", ""
		if row.Transfer != nil && row.Transfer.UploadID != "" {
			request.RemoteOutcome, request.KnownReceipt = "existing", row.Transfer.UploadID
		}
		return request
	}
	snapshot := func(t *testing.T, item task) [2]string {
		t.Helper()
		var out [2]string
		// xmin/ctid also detect an otherwise invisible no-op SQL UPDATE.
		if err := f.conn.QueryRow(ctx, `SELECT jsonb_build_object('row',to_jsonb(o),'xmin',o.xmin::text,'ctid',o.ctid::text)::text FROM computer_operations o WHERE id=$1`, item.op.ID).Scan(&out[0]); err != nil {
			t.Fatal(err)
		}
		if err := f.conn.QueryRow(ctx, `SELECT coalesce(jsonb_agg(jsonb_build_object('row',to_jsonb(e),'xmin',e.xmin::text,'ctid',e.ctid::text) ORDER BY delivery_id),'[]'::jsonb)::text FROM computer_delivery_entries e WHERE operation_id=$1`, item.op.ID).Scan(&out[1]); err != nil {
			t.Fatal(err)
		}
		return out
	}
	seedHistoricalReceipt := func(t *testing.T, item task) {
		t.Helper()
		record := item.queue
		record.Job.Phase, record.Job.ReceiptID, record.Job.ChunkBytes = "uploading", "synthetic-orphan-receipt", 65536
		// Synthetic lost-NAS-binding fixture only: append a valid v6 record to
		// this NEW private journal. No original frame/record is edited or removed.
		body, _ := json.Marshal(record)
		hash := sha256.Sum256(body)
		frame := make([]byte, 36+len(body))
		binary.BigEndian.PutUint32(frame[:4], uint32(len(body))) //nolint:gosec // Existing bounded v6 fixture record, smaller than the 16 KiB frame limit.
		copy(frame[4:36], hash[:])
		copy(frame[36:], body)
		file, err := os.OpenFile(filepath.Join(item.state, "deliveries-v1", record.Job.DeliveryID+".journal"), os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(frame); err != nil {
			t.Fatal(err)
		}
		if err := file.Sync(); err != nil {
			t.Fatal(err)
		}
		_ = file.Close()
		q, err := transferclient.OpenHerdrQueue(item.getenv, "fixture", limits)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := q.Inspect(record.Job.DeliveryID)
		_ = q.Close()
		if err != nil || actual.Version != 6 || actual.Job.ReceiptID != record.Job.ReceiptID {
			t.Fatal("historical v6 receipt fixture failed decoder", err)
		}
	}
	checkRecovery := func(t *testing.T, item task, request deliverymeta.ResultRecovery, wantGets int64, wantError bool) {
		t.Helper()
		before := snapshot(t, item)
		queueBefore := nativeProducerTreeDigest(t, item.state)
		mediaBefore := nativeProducerTreeDigest(t, originals)
		oldCalls, oldGets, oldMutations := calls.Load(), gets.Load(), mutations.Load()
		observed, err := transferclient.RecoverResultSource(ctx, item.getenv, "fixture", limits, request, https.Client())
		if (err != nil) != wantError || !wantError && (observed.ReceiptObserved == nil || observed.ReceiptObserved.ID != request.KnownReceipt) {
			t.Fatal("recovery observation", err)
		}
		if calls.Load()-oldCalls != wantGets || gets.Load()-oldGets != wantGets || mutations.Load() != oldMutations {
			t.Fatal("unexpected receipt/Create/PATCH/Complete/Commit/Resolve invocation")
		}
		after := snapshot(t, item)
		queueAfter, mediaAfter := nativeProducerTreeDigest(t, item.state), nativeProducerTreeDigest(t, originals)
		if after != before || !reflect.DeepEqual(queueBefore, queueAfter) || !reflect.DeepEqual(mediaBefore, mediaAfter) {
			t.Errorf("changed components: operation=%t ledger=%t journal=%t media=%t", after[0] != before[0], after[1] != before[1], !reflect.DeepEqual(queueBefore, queueAfter), !reflect.DeepEqual(mediaBefore, mediaAfter))
			if after[0] != before[0] {
				var old, next struct {
					Row  map[string]json.RawMessage `json:"row"`
					Xmin string                     `json:"xmin"`
					CTID string                     `json:"ctid"`
				}
				_ = json.Unmarshal([]byte(before[0]), &old)
				_ = json.Unmarshal([]byte(after[0]), &next)
				for key, value := range old.Row {
					if !bytes.Equal(value, next.Row[key]) {
						t.Log("changed operation column", key)
					}
				}
				t.Logf("operation xmin changed=%t ctid changed=%t", old.Xmin != next.Xmin, old.CTID != next.CTID)
			}
			for name, hash := range mediaAfter {
				if mediaBefore[name] != hash {
					t.Log("changed synthetic media fixture file", name)
				}
			}
			t.Fatal("readback/refusal updated operation, ledger, original v6 journal or media files")
		}
		if wantError && (observed.ReceiptObserved != nil || denialLeaked.Load()) {
			t.Fatal("denial exposed receipt, byte metadata or credential")
		}
		if wantError && wantGets == 1 {
			var status *transferclient.HTTPError
			if !errors.As(err, &status) || (status.Status != 401 && status.Status != 403) {
				t.Fatal("authorization denial did not fail closed", err)
			}
			t.Logf("real handler authorization HTTP=%d; receipt/byte metadata absent", status.Status)
		}
		t.Logf("receipt_GET=%d other_HTTP=0; original operation+ledger (including xmin/ctid), journal v6, media bytes unchanged", wantGets)
	}
	t.Run("missing_binding_stops_before_GET", func(t *testing.T) {
		item := newTask(t)
		seedHistoricalReceipt(t, item)
		request := projection(t, item)
		if request.RemoteOutcome != "absent" {
			t.Fatal("fixture not absent")
		}
		checkRecovery(t, item, request, 0, true)
	})
	t.Run("unknown_create_stops_before_GET", func(t *testing.T) {
		item := newTask(t)
		seedHistoricalReceipt(t, item)
		identity := id
		identity.OperationID = item.op.ID
		principal := pgTransferPrincipal(identity, item.queue.Job.DeliveryID, computertask.DeliveryRequest{Version: 1, Direction: "upload", ContentHash: item.request.SHA256, SizeBytes: item.request.SizeBytes, Mime: item.request.Plan.Artifact.Mime})
		principal.Device = verifier.device
		principal.CredentialID = verifier.device.CredentialID
		bound, err := computertask.BindVerifiedTransfer(ctx, principal)
		if err != nil {
			t.Fatal(err)
		}
		if err := base.ReserveTransfer(bound); err != nil {
			t.Fatal(err)
		}
		if _, err := base.ClaimTransferUpload(bound); err != nil {
			t.Fatal(err)
		}
		request := projection(t, item)
		if request.RemoteOutcome != "unknown" {
			t.Fatal("real PG create fence lost")
		}
		checkRecovery(t, item, request, 0, true)
	})
	t.Run("control_direct_GET_on_absent_binding_reserves_PG", func(t *testing.T) {
		item := newTask(t)
		before := snapshot(t, item)
		queueBefore := nativeProducerTreeDigest(t, item.state)
		oldCalls, oldGets, oldMutations := calls.Load(), gets.Load(), mutations.Load()
		client, err := transferclient.NewDeviceTask(https.URL, item.queue.Job, "upload", item.queue.Job.Task, https.Client(), func(context.Context) (string, error) { return "Memoh-Transfer " + verifier.token, nil })
		if err != nil {
			t.Fatal(err)
		}
		defer client.CloseIdleConnections()
		_, err = client.Get(ctx, f.bot, "synthetic-orphan-receipt")
		var denied *transferclient.HTTPError
		if !errors.As(err, &denied) || denied.Status != 403 {
			t.Fatal("direct absent-binding GET control", err)
		}
		after := snapshot(t, item)
		entry, err := store.ReadComputerDeliveryEntry(ctx, item.op, item.queue.Job.DeliveryID)
		if err != nil {
			t.Fatal("actual ReserveTransfer did not persist", err)
		}
		var reserved struct {
			Outcome  string `json:"outcome"`
			Transfer *struct {
				UploadID string `json:"upload_id"`
			} `json:"transfer"`
		}
		if json.Unmarshal(entry.Data, &reserved) != nil || reserved.Outcome != "reserved" || reserved.Transfer == nil || reserved.Transfer.UploadID != "" || before == after || before[1] != "[]" || after[1] == "[]" {
			t.Fatal("GET ReserveTransfer side effect not established")
		}
		if calls.Load()-oldCalls != 1 || gets.Load()-oldGets != 1 || mutations.Load() != oldMutations || !reflect.DeepEqual(queueBefore, nativeProducerTreeDigest(t, item.state)) {
			t.Fatal("control issued a mutation request or touched journal")
		}
		t.Log("negative control: direct receipt GET=1 HTTP=403 yet actual PG creates reserved ledger row and changes operation revision/xmin; no Create/Commit/Resolve; recovery cases must stop before this GET")
	})
	t.Run("cli_cold_ready_and_cached_proof_report_get_scope", func(t *testing.T) {
		item := newTask(t, "native-profile-v1")
		created, err := transferclient.InvokeConfiguredHerdr(ctx, item.getenv, item.revision, item.limits, transferclient.ConfiguredAction{Kind: "advance", DeliveryID: item.queue.Job.DeliveryID}, https.Client(), time.Now())
		if err != nil || created.Job.ReceiptID == "" || created.Version != 6 {
			t.Fatal("original real handler receipt setup", err)
		}
		request := projection(t, item)
		if request.RemoteOutcome != "existing" || request.KnownReceipt != created.Job.ReceiptID {
			t.Fatal("original receipt not bound")
		}
		if _, err := assets.Uploads().Append(ctx, f.owner, f.bot, request.KnownReceipt, 0, int64(len(data)), request.SHA256, bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
		if _, err := assets.Uploads().Complete(ctx, f.owner, f.bot, request.KnownReceipt); err != nil {
			t.Fatal(err)
		}
		// Build only this changed CLI, once; run it against the real PG/handler
		// fixture. No image/distribution build or old test matrix is repeated.
		binary := filepath.Join(t.TempDir(), "transferqueue")
		build := exec.CommandContext(ctx, "go", "build", "-buildvcs=false", "-o", binary, "../../cmd/transferqueue") //nolint:gosec // Fixed current source and isolated test output.
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("changed CLI build: %v %s", err, out)
		}
		certificate := filepath.Join(t.TempDir(), "synthetic-ca.pem")
		write(certificate, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: https.Certificate().Raw}))
		invoke := func(input deliverymeta.ResultRecovery, wantExit int) map[string]string {
			t.Helper()
			raw, _ := json.Marshal(input)
			cmd := exec.CommandContext(ctx, binary, "-action", "recover-result") //nolint:gosec // Built local CLI and fixed explicit entry.
			cmd.Env = append(os.Environ(), "HERDR_PLUGIN_CONFIG_DIR="+item.getenv("HERDR_PLUGIN_CONFIG_DIR"), "HERDR_PLUGIN_STATE_DIR="+item.state, "SSL_CERT_FILE="+certificate)
			cmd.Stdin = bytes.NewReader(raw)
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			exit := 0
			if err != nil {
				var exited *exec.ExitError
				if !errors.As(err, &exited) {
					t.Fatal(err)
				}
				exit = exited.ExitCode()
			}
			for _, private := range []string{verifier.token, item.request.PhysicalName, item.state, https.URL} {
				if bytes.Contains(stdout.Bytes(), []byte(private)) || bytes.Contains(stderr.Bytes(), []byte(private)) {
					t.Fatal("CLI status exposed private fixture metadata")
				}
			}
			var output map[string]string
			if exit != wantExit || stderr.Len() != 0 || json.Unmarshal(stdout.Bytes(), &output) != nil {
				t.Fatal("CLI status/exit", exit, stdout.String(), stderr.String())
			}
			return output
		}
		for _, phase := range []string{"cold-ready", "cached-proof"} {
			before := snapshot(t, item)
			queueBefore := nativeProducerTreeDigest(t, item.state)
			mediaBefore := nativeProducerTreeDigest(t, originals)
			oldCalls, oldGets, oldMutations := calls.Load(), gets.Load(), mutations.Load()
			output := invoke(request, 0)
			if output["state"] != "receipt_observed" || output["recovery_scope"] != "receipt_get_only" || output["server_effects"] != "ready_proof_may_be_recorded" {
				t.Fatal("CLI claimed server read-only or omitted possible proof write", output)
			}
			changed := snapshot(t, item) != before
			if changed != (phase == "cold-ready") || calls.Load()-oldCalls != 1 || gets.Load()-oldGets != 1 || mutations.Load() != oldMutations || !reflect.DeepEqual(queueBefore, nativeProducerTreeDigest(t, item.state)) || !reflect.DeepEqual(mediaBefore, nativeProducerTreeDigest(t, originals)) {
				t.Fatal("scope label does not match measured client/server effects", phase)
			}
			entry, err := store.ReadComputerDeliveryEntry(ctx, item.op, item.queue.Job.DeliveryID)
			if err != nil {
				t.Fatal(err)
			}
			var bound struct {
				Transfer struct {
					UploadID string        `json:"upload_id"`
					Ready    *media.Upload `json:"ready"`
				} `json:"transfer"`
			}
			if json.Unmarshal(entry.Data, &bound) != nil || bound.Transfer.UploadID != request.KnownReceipt || bound.Transfer.Ready == nil || bound.Transfer.Ready.ContentHash != request.SHA256 || bound.Transfer.Ready.SizeBytes != request.SizeBytes {
				t.Fatal("original ready proof/receipt lost")
			}
			t.Logf("actual CLI %s: receipt_get_only + ready_proof_may_be_recorded; GET=1 client mutations=0; PG proof changed=%t; original id/proof/journal v6 unchanged", phase, changed)
		}
		// New status is emitted only after the existing gate. A missing binding
		// cannot obtain this observation or trigger a GET under the new label.
		absent := request
		absent.RemoteOutcome, absent.KnownReceipt = "absent", ""
		before := snapshot(t, item)
		queueBefore := nativeProducerTreeDigest(t, item.state)
		oldCalls := calls.Load()
		output := invoke(absent, 1)
		if output["state"] != "recovery_blocked" || output["recovery_scope"] != "" || output["server_effects"] != "" || calls.Load() != oldCalls || snapshot(t, item) != before || !reflect.DeepEqual(queueBefore, nativeProducerTreeDigest(t, item.state)) {
			t.Fatal("status correction bypassed original binding gate")
		}
	})
	t.Run("existing_and_conflicting_receipt_and_revocation", func(t *testing.T) {
		item := newTask(t)
		// One finite real worker setup reaches Create and durably retains its
		// original receipt. It returns before PATCH or completion; no file bytes
		// are uploaded. Every assertion below starts AFTER this setup baseline.
		created, err := transferclient.InvokeConfiguredHerdr(ctx, item.getenv, "fixture", limits, transferclient.ConfiguredAction{Kind: "advance", DeliveryID: item.queue.Job.DeliveryID}, https.Client(), time.Now())
		if err != nil || created.Job.ReceiptID == "" || created.Version != 6 {
			t.Fatal("real handler receipt setup", err)
		}
		request := projection(t, item)
		if request.RemoteOutcome != "existing" || request.KnownReceipt != created.Job.ReceiptID {
			t.Fatal("PG/local original receipt mismatch")
		}
		wrong := request
		wrong.KnownReceipt = "conflicting-receipt"
		checkRecovery(t, item, wrong, 0, true)
		// The first UploadStore read creates one fixed, empty coordination lock.
		// Classify it explicitly as setup, not a ledger/receipt mutation or a
		// product failure, then freeze every media file for the measured GETs.
		mediaBeforeLock := nativeProducerTreeDigest(t, originals)
		if _, err := assets.Uploads().Get(ctx, f.owner, f.bot, request.KnownReceipt); err != nil {
			t.Fatal(err)
		}
		mediaAfterLock := nativeProducerTreeDigest(t, originals)
		lockHash := sha256.Sum256([]byte(request.KnownReceipt))
		lockName := filepath.Join(".uploads", fmt.Sprintf("lock-%02d", 1+int(lockHash[0])%64))
		for name, hash := range mediaBeforeLock {
			if mediaAfterLock[name] != hash {
				t.Fatal("receipt changed while initializing read lock")
			}
		}
		for name, hash := range mediaAfterLock {
			if _, exists := mediaBeforeLock[name]; !exists && (name != lockName || hash != sha256.Sum256(nil)) {
				t.Fatal("unexpected media change in read-lock setup")
			}
		}
		t.Log("initial media read materialized at most one 0-byte fixed coordination lock; existing receipt/payload bytes unchanged; subsequent GET snapshots include that lock")
		checkRecovery(t, item, request, 1, false)
		// Independent cold-ready control: simulate the existing fault window
		// after Media completion but before ready proof reached the PG ledger.
		// These are fixture-only local Media methods, not HTTP upload/recovery.
		if _, err := assets.Uploads().Append(ctx, f.owner, f.bot, request.KnownReceipt, 0, int64(len(data)), request.SHA256, bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
		if _, err := assets.Uploads().Complete(ctx, f.owner, f.bot, request.KnownReceipt); err != nil {
			t.Fatal(err)
		}
		coldBefore := snapshot(t, item)
		journalBefore := nativeProducerTreeDigest(t, item.state)
		mediaBefore := nativeProducerTreeDigest(t, originals)
		entryBefore, err := store.ReadComputerDeliveryEntry(ctx, item.op, item.queue.Job.DeliveryID)
		if err != nil {
			t.Fatal(err)
		}
		oldCalls, oldGets, oldMutations := calls.Load(), gets.Load(), mutations.Load()
		observed, err := transferclient.RecoverResultSource(ctx, item.getenv, "fixture", limits, request, https.Client())
		if err != nil || observed.ReceiptObserved == nil || observed.ReceiptObserved.State != "ready" {
			t.Fatal("cold-ready control", err)
		}
		entryAfter, err := store.ReadComputerDeliveryEntry(ctx, item.op, item.queue.Job.DeliveryID)
		if err != nil {
			t.Fatal(err)
		}
		var old, next struct {
			Reference json.RawMessage `json:"reference"`
			Scope     string          `json:"scope"`
			Outcome   string          `json:"outcome"`
			Transfer  struct {
				UploadID string        `json:"upload_id"`
				Ready    *media.Upload `json:"ready"`
			} `json:"transfer"`
		}
		if json.Unmarshal(entryBefore.Data, &old) != nil || json.Unmarshal(entryAfter.Data, &next) != nil || old.Transfer.Ready != nil || next.Transfer.Ready == nil || next.Transfer.Ready.ID != request.KnownReceipt || next.Transfer.Ready.ContentHash != request.SHA256 || next.Transfer.Ready.SizeBytes != request.SizeBytes || old.Transfer.UploadID != next.Transfer.UploadID || !bytes.Equal(old.Reference, next.Reference) || old.Scope != next.Scope || old.Outcome != next.Outcome {
			t.Fatal("cold-ready GET changed more than the original ready proof")
		}
		if calls.Load()-oldCalls != 1 || gets.Load()-oldGets != 1 || mutations.Load() != oldMutations || coldBefore == snapshot(t, item) || !reflect.DeepEqual(journalBefore, nativeProducerTreeDigest(t, item.state)) || !reflect.DeepEqual(mediaBefore, nativeProducerTreeDigest(t, originals)) {
			t.Fatal("cold-ready side-effect classification failed")
		}
		t.Log("cold-ready control: recovery GET=1 and HTTP mutations=0, but real handler RecordTransferReady changes original PG ledger/operation; original scope/reference/receipt and private journal unchanged; GET-only is NOT universally server-write-free")
		checkRecovery(t, item, request, 1, false) // Cached ready proof is read-only again.
		// Actual account revocation, checked by the registry and transactional
		// authorization; no synthetic verifier boolean or auth context bypass.
		f.exec(t, "UPDATE users SET is_active=false WHERE id=$1", f.owner)
		checkRecovery(t, item, request, 1, true)
		f.exec(t, "UPDATE users SET is_active=true WHERE id=$1", f.owner)
		verifier.revoke(ctx, t)
		checkRecovery(t, item, request, 1, true)
	})
}
