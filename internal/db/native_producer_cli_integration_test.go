//go:build integration

package db_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"io/fs"
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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/computertask"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/handlers"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
	"github.com/felinics/memoh/internal/team"
	"github.com/felinics/memoh/internal/toolcontext"
)

// Real production CLI processes, filesystem queue, TLS, NAS handlers, registry
// verifier and NOBYPASSRLS PostgreSQL. Linux paths test the producer's directory
// contract; Windows command quoting/NTFS are separate acceptance boundaries.
func TestNativeProducerCLIUsesPinnedDirectoryPairAndRecoversUnknownCommit(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("isolated Linux CLI and real NAS fixture; native Windows acceptance is separate")
	}
	f := newNativeHerdrFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	profile := map[string]any{
		"instance_id": "synthetic-native-instance", "session": "synthetic", "work_dir": `C:\Synthetic`,
		"bash_path": `C:\Synthetic\Git\bin\bash.exe`, "herdr_cli": `C:\Synthetic\herdr.exe`, "herdr_config_root": `C:\Synthetic\herdr`,
		"agent":    agenvo.AgentProfile{ExecutionMode: "pi-json-v1", Provider: "synthetic", Model: "synthetic", ConfigDir: `C:\Synthetic\Pi`},
		"producer": map[string]string{"program_path": `C:\Synthetic\producer.exe`, "config_dir": `C:\Synthetic\native-config`, "state_dir": `C:\Synthetic\native-state`},
	}
	f.config, _ = json.Marshal(map[string]any{"herdr": profile})
	f.exec(t, "UPDATE user_runtimes SET provider_config=$1 WHERE id=$2", []byte(f.config), f.runtime)
	f.exec(t, "UPDATE bot_sessions SET created_by_user_id=$1 WHERE id=$2", f.owner, f.session)
	target, err := agenvo.DecodeNativeTarget(f.runtime, f.config)
	if err != nil {
		t.Fatal(err)
	}
	id := computertask.DeliveryIdentity{Binding: toolcontext.ComputerBinding{BotID: f.bot, UserID: f.owner, SessionID: f.session, TargetID: f.mount, RunID: "native-producer-original-run"}, OperationID: uuid.NewString(), DeviceID: f.runtime, InstanceID: target.InstanceID, Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32)}
	root := t.TempDir()
	write := func(path string, raw []byte) {
		t.Helper()
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	type locations struct{ config, state, source string }
	pair := func(name string) locations {
		t.Helper()
		base := filepath.Join(root, name)
		p := locations{filepath.Join(base, "config"), filepath.Join(base, "state"), filepath.Join(base, "source")}
		for _, path := range []string{p.config, p.state, p.source} {
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		return p
	}
	native, legacy := pair("native"), pair("legacy-agenvo")
	data := []byte("native producer content remains authoritative")
	sum := sha256.Sum256(data)
	closed := deliverymeta.Closed{Plan: deliverymeta.Plan{Version: 1, Task: deliverymeta.Task{OperationID: id.OperationID, BotID: f.bot, UserID: f.owner, SessionID: f.session, TargetID: f.mount, RunID: id.Binding.RunID, Attempt: id.Attempt, Generation: id.Generation, RootHash: deliverymeta.RootHash(native.source)}, Artifact: deliverymeta.Artifact{SourceRef: "native-result", Mime: "text/plain"}}, Closed: true, SHA256: hex.EncodeToString(sum[:]), SizeBytes: int64(len(data))}
	details, _ := json.Marshal(computertask.Details{Kind: "herdr_pi_task", Batch: &computertask.BatchDetails{Progress: agenvo.BatchProgress{Attempt: id.Attempt, Generation: id.Generation, Producer: &closed.Plan}}, Phase: "execution-completed"})
	op := f.operation(f.mount)
	op.ID, op.RunID, op.TargetFingerprint, op.Details = id.OperationID, id.Binding.RunID, agenvo.TargetFingerprint(target), details
	op, won, err := f.store.ClaimComputerOperation(ctx, op)
	if err != nil || !won {
		t.Fatal("original native operation", won, err)
	}
	op.State = "completed"
	op, err = f.store.SaveComputerOperation(ctx, op)
	if err != nil {
		t.Fatal(err)
	}
	// Real transactions use a restricted application role and the current team.
	rls := rlsConn(t, f.pool, teamMigrationDSN(t))
	var role string
	if err := rls.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		t.Fatal(err)
	}
	poolConfig := f.pool.Config().Copy()
	poolConfig.ConnConfig.RuntimeParams["role"] = role
	poolConfig.ConnConfig.RuntimeParams["memoh.team_id"] = team.DefaultTeamID
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store, err := postgresstore.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	verifier := newRegisteredTransferVerifier(ctx, t, store, id)
	log := slog.New(slog.DiscardHandler)
	originals := filepath.Join(root, "nas-originals")
	assets := media.NewService(nil, localfs.NewOriginals(originals))
	assets.SetRequirePersistence(true)
	if err := assets.EnableUploads(originals); err != nil { //nolint:contextcheck // Initializer only; real requests supply their own contexts.
		t.Fatal(err)
	}
	defer func() { _ = assets.Uploads().Close() }()
	messages := handlers.NewMessageHandler(log, nil, nil, nil, nil)
	messages.SetMediaService(assets)
	handler, err := handlers.NewDeviceComputerTransferHandler(ctx, computertask.NewPostgresDeliveryService(store, assets), messages, verifier, log)
	if err != nil {
		t.Fatal(err)
	}
	defer handler.Close()
	endpoint := server.NewServer(log, ":0", "synthetic-native-producer", handler) //nolint:contextcheck // Actual HTTP requests carry their contexts.
	var creates, commits, resolves atomic.Int64
	entered, finished := make(chan int, 1), make(chan struct{})
	https := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/media/uploads") {
			creates.Add(1)
		}
		if strings.HasSuffix(r.URL.Path, "/resolve") {
			resolves.Add(1)
		}
		if strings.HasSuffix(r.URL.Path, "/commit") && commits.Add(1) == 1 {
			defer close(finished)
			response := httptest.NewRecorder()
			endpoint.ServeHTTP(response, r)
			// HTTP 200 can acknowledge pending asynchronous association. Place
			// this fault only after the authoritative PG entry is committed.
			code := response.Code
			if code == http.StatusOK {
				wait, stop := context.WithTimeout(r.Context(), 5*time.Second)
				defer stop()
				tick := time.NewTicker(10 * time.Millisecond)
				defer tick.Stop()
				for {
					entry, err := store.ReadComputerDeliveryEntry(wait, op, closed.DeliveryID())
					var result struct {
						Outcome string `json:"outcome"`
					}
					if err == nil && json.Unmarshal(entry.Data, &result) == nil && result.Outcome == "committed" {
						break
					}
					select {
					case <-tick.C:
					case <-wait.Done():
						code = http.StatusInternalServerError
					}
					if code != http.StatusOK {
						break
					}
				}
			}
			entered <- code
			<-r.Context().Done() // Kill the actual CLI after NAS commit but before its acknowledgement.
			return
		}
		endpoint.ServeHTTP(w, r)
	}))
	defer https.Close()
	cert := filepath.Join(root, "synthetic-ca.pem")
	write(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: https.Certificate().Raw}))
	for p, credential := range map[locations]string{native: verifier.token, legacy: "synthetic-legacy-device"} {
		settings, _ := json.Marshal(map[string]any{"version": 1, "origin": https.URL, "source_root": p.source})
		write(filepath.Join(p.config, "transfer.json"), settings)
		write(filepath.Join(p.config, "device.credential"), []byte(credential))
	}
	binary := filepath.Join(root, "transferqueue")
	build := exec.CommandContext(ctx, "go", "build", "-buildvcs=false", "-o", binary, "../../cmd/transferqueue") //nolint:gosec // Fixed local source and isolated output.
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build production CLI: %v %s", err, out)
	}
	command := func(p locations, action, delivery string, input []byte) *exec.Cmd {
		cmd := exec.CommandContext(ctx, binary, "-action", action, "-delivery", delivery) //nolint:gosec // Built production binary and fixed test arguments; no shell.
		cmd.Env = append(os.Environ(), "HERDR_PLUGIN_CONFIG_DIR="+p.config, "HERDR_PLUGIN_STATE_DIR="+p.state, "SSL_CERT_FILE="+cert)
		cmd.Stdin = bytes.NewReader(input)
		return cmd
	}
	checkPrivate := func(output, stderr []byte) {
		t.Helper()
		for _, secret := range []string{verifier.token, "synthetic-legacy-device", native.source, legacy.source, https.URL} {
			if bytes.Contains(output, []byte(secret)) || bytes.Contains(stderr, []byte(secret)) {
				t.Fatal("CLI leaked private fixture configuration")
			}
		}
	}
	invoke := func(p locations, action, delivery string, input []byte) map[string]json.RawMessage {
		t.Helper()
		cmd := command(p, action, delivery, input)
		var out, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &stderr
		err := cmd.Run()
		checkPrivate(out.Bytes(), stderr.Bytes())
		if err != nil {
			t.Fatalf("production CLI %s: %v %s %s", action, err, out.String(), stderr.String())
		}
		var result map[string]json.RawMessage
		if json.Unmarshal(out.Bytes(), &result) != nil {
			t.Fatal("CLI did not emit one JSON result")
		}
		return result
	}
	// Seed an actual legacy queue, then freeze every existing file's bytes.
	old := closed
	old.Task.OperationID, old.Task.RootHash = uuid.NewString(), deliverymeta.RootHash(legacy.source)
	old.Artifact.SourceRef = "legacy-result"
	write(filepath.Join(legacy.source, old.Artifact.SourceRef), data)
	oldRaw, _ := json.Marshal(old)
	if out := invoke(legacy, "enqueue-result", "", oldRaw); string(out["registered"]) != "true" {
		t.Fatal("legacy fixture was not registered")
	}
	oldBytes := nativeProducerTreeDigest(t, filepath.Dir(legacy.config))
	write(filepath.Join(native.source, closed.Artifact.SourceRef), data)
	attempt, ok := deliverymeta.AttemptDirectory(id.Attempt)
	if !ok {
		t.Fatal("invalid attempt")
	}
	attemptPath := filepath.Join(native.source, filepath.FromSlash(attempt))
	if err := os.MkdirAll(attemptPath, 0o700); err != nil {
		t.Fatal(err)
	}
	plan, _ := json.Marshal(closed.Plan)
	write(filepath.Join(attemptPath, deliverymeta.ProducerContextName), plan)
	raw, _ := json.Marshal(closed)
	manifest := filepath.Join(native.source, closed.ManifestName())
	write(manifest, raw)
	cmd := command(native, "collect-result", "", nil)
	var unknownOutput, unknownError bytes.Buffer
	cmd.Stdout, cmd.Stderr = &unknownOutput, &unknownError
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-entered:
		if code != http.StatusOK {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatal("real NAS commit failed", code)
		}
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("collect did not reach real NAS commit: %s %s", unknownOutput.String(), unknownError.String())
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("collect acknowledgement was not interrupted")
	}
	checkPrivate(unknownOutput.Bytes(), unknownError.Bytes())
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("interrupted collector request did not finish")
	}
	readCommitted := func() computertask.DeliveryRequest {
		t.Helper()
		entry, err := store.ReadComputerDeliveryEntry(ctx, op, closed.DeliveryID())
		var result struct {
			Outcome   string                       `json:"outcome"`
			Reference computertask.DeliveryRequest `json:"reference"`
		}
		if err != nil || json.Unmarshal(entry.Data, &result) != nil || result.Outcome != "committed" {
			t.Fatal("NAS authoritative committed reference absent", err)
		}
		return result.Reference
	}
	ref := computertask.DeliveryRequest{Version: 1, Direction: "upload", ContentHash: closed.SHA256, SizeBytes: closed.SizeBytes, Mime: closed.Artifact.Mime}
	if readCommitted() != ref {
		t.Fatal("committed reference differs from original file")
	}
	uncertain := invoke(native, "inspect", closed.DeliveryID(), nil)
	if string(uncertain["associated"]) != "false" || creates.Load() != 1 || commits.Load() != 1 {
		t.Fatal("client invented acknowledgement or duplicated upload")
	}
	// Re-present exactly the same closed manifest after uncertain collection.
	// Fresh production processes must resolve the original ID, not rerun work.
	for range 3 {
		write(manifest, raw)
		out := invoke(native, "collect-result", "", nil)
		if string(out["associated"]) != "true" || string(out["delivery_id"]) != `"`+closed.DeliveryID()+`"` {
			t.Fatal("restarted collector did not recover original delivery", out)
		}
		if readCommitted() != ref || creates.Load() != 1 || commits.Load() != 1 || resolves.Load() != 1 {
			t.Fatal("repeat unknown collection duplicated NAS mutation", creates.Load(), commits.Load(), resolves.Load())
		}
		if !reflect.DeepEqual(oldBytes, nativeProducerTreeDigest(t, filepath.Dir(legacy.config))) {
			t.Fatal("native collector modified legacy config, source or queue bytes")
		}
	}
	current, err := store.ReadComputerOperation(ctx, op)
	if err != nil || current.ID != id.OperationID || current.RunID != id.Binding.RunID || current.TargetFingerprint != op.TargetFingerprint || current.State != "completed" {
		t.Fatal("collection changed business identity/state", err)
	}
	var operations int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM computer_operations WHERE bot_id=$1", f.bot).Scan(&operations); err != nil || operations != 1 {
		t.Fatal("collection created a new business operation", operations, err)
	}
	reader, asset, err := assets.OpenSeekable(ctx, f.bot, ref.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	got, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(got, data) || asset.ContentHash != ref.ContentHash {
		t.Fatal("NAS content differs from committed reference", err)
	}
}

func nativeProducerTreeDigest(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	files := make(map[string][32]byte)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path) //nolint:gosec // Enumerated files under the isolated synthetic fixture only.
		if err != nil {
			return err
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[name] = sha256.Sum256(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
