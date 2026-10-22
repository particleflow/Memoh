//go:build integration && windows

package db_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"golang.org/x/sys/windows"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/computertask"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/handlers"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/server"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
	"github.com/felinics/memoh/internal/team"
	"github.com/felinics/memoh/internal/toolcontext"
	"github.com/felinics/memoh/internal/transferclient"
)

func nativeDeliveryDatabase(t *testing.T) (*postgresstore.Store, computertask.DeliveryIdentity, context.Context) {
	t.Helper()
	if os.Getenv("TEST_POSTGRES_DSN") == "" {
		t.Fatal("native combination requires an isolated PostgreSQL DSN; no skip")
	}
	ownerPool := freshMigratedDB(t)
	config := ownerPool.Config().Copy()
	config.ConnConfig.RuntimeParams["memoh.team_id"] = team.DefaultTeamID
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store, err := postgresstore.New(pool)
	if err != nil {
		t.Fatal(err)
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(t.Context(), sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	owner, user, bot, session, connection := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec("INSERT INTO users(id) VALUES($1),($2)", owner, user)
	exec("INSERT INTO team_members(user_id) VALUES($1),($2)", owner, user)
	exec("INSERT INTO bots(id,owner_user_id,name) VALUES($1,$2,'native-delivery-fixture')", bot, owner)
	exec("INSERT INTO bot_sessions(id,bot_id,created_by_user_id) VALUES($1,$2,$3)", session, bot, user)
	exec(`INSERT INTO bot_user_grants(bot_id,subject_type,user_id,permissions) VALUES($1,'user',$2,'["chat","workspace_read","workspace_write","workspace_exec"]')`, bot, user)
	exec("INSERT INTO mcp_connections(id,bot_id,name,type) VALUES($1,$2,'native-fixture','http')", connection, bot)
	target := agenvo.Target{ConnectionID: connection, DeviceID: "synthetic-device", InstanceID: "synthetic-instance", Session: "fixture", WorkDir: `C:\Synthetic`, BashPath: `C:\Synthetic\Git\bin\bash.exe`, Agent: agenvo.AgentProfile{ExecutionMode: "pi-json-v1", Provider: "synthetic", Model: "synthetic", ConfigDir: `C:\Synthetic\config`}}
	raw, _ := json.Marshal(target)
	runtime, err := store.CreateAgenvoRuntime(t.Context(), owner, "native-fixture", raw)
	if err != nil {
		t.Fatal(err)
	}
	mount, err := store.CreateOrUpdateMount(t.Context(), bot, runtime.ID)
	if err != nil {
		t.Fatal(err)
	}
	id := computertask.DeliveryIdentity{Binding: toolcontext.ComputerBinding{BotID: bot, UserID: user, SessionID: session, TargetID: mount.ID, RunID: "synthetic-native-run"}, OperationID: uuid.NewString(), DeviceID: target.DeviceID, InstanceID: target.InstanceID, Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32)}
	details, _ := json.Marshal(computertask.Details{Kind: "herdr_pi_task", Batch: &computertask.BatchDetails{Progress: agenvo.BatchProgress{Attempt: id.Attempt, Generation: id.Generation}}, Phase: "submitted"})
	if _, claimed, err := store.ClaimComputerOperation(t.Context(), dbstore.ComputerOperationRecord{ID: id.OperationID, BotID: bot, UserID: user, SessionID: session, TargetID: mount.ID, RunID: id.Binding.RunID, Fingerprint: "synthetic-native-fingerprint", TargetFingerprint: agenvo.TargetFingerprint(target), Details: details}); err != nil || !claimed {
		t.Fatal("claim", claimed, err)
	}
	rls := rlsConn(t, pool, teamMigrationDSN(t))
	var role string
	if err := rls.QueryRow(t.Context(), "SELECT current_user").Scan(&role); err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["role"] = role
	restricted, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restricted.Close)
	store, err = postgresstore.New(restricted)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := toolcontext.WithComputerBinding(t.Context(), id.Binding)
	if err != nil {
		t.Fatal(err)
	}
	bound, err = computertask.WithDeliveryIdentity(bound, id)
	if err != nil {
		t.Fatal(err)
	}
	return store, id, bound
}

func nativeDeliveryPrivateRoot(t *testing.T) string {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := user.User.Sid.String()
	sd, err := windows.SecurityDescriptorFromString("O:" + sid + "D:P(A;OICI;FA;;;" + sid + ")(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "private-native-state")
	sa := windows.SecurityAttributes{SecurityDescriptor: sd}
	sa.Length = uint32(unsafe.Sizeof(sa))
	ptr, err := windows.UTF16PtrFromString(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.CreateDirectory(ptr, &sa); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

type nativeLostPublication struct {
	*transferclient.NativePublication
	lose bool
}

func (p *nativeLostPublication) Publish(ctx context.Context, ref transferclient.Reference, source io.Reader) (transferclient.AssociationOutcome, error) {
	out, err := p.NativePublication.Publish(ctx, ref, source)
	if err == nil && out == transferclient.OutcomeCommitted && p.lose {
		p.lose = false
		return "", context.DeadlineExceeded
	}
	return out, err
}

// One Windows process executes the actual NTFS destination, HTTPS handlers,
// finite asynchronous verifier and PostgreSQL transactions together. Only the
// incoming execution authenticator is synthetic. Originals are preseeded fixture
// bytes: Unix-only upload admission is intentionally not exercised on Windows.
func TestWindowsNativePostgresDelivery(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "revoke-during-scan"}[revoke], func(t *testing.T) { nativePostgresDelivery(t, revoke) })
	}
}

func nativePostgresDelivery(t *testing.T, revoke bool) {
	t.Helper()
	store, id, ctx := nativeDeliveryDatabase(t)
	data := []byte("native NTFS and real PostgreSQL delivery")
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	mediaRoot := t.TempDir()
	dir := filepath.Join(mediaRoot, id.Binding.BotID, hash[:2])
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, hash+".txt"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	assets := media.NewService(nil, localfs.NewOriginals(mediaRoot))
	assets.SetRequirePersistence(true)
	log := slog.New(slog.DiscardHandler)
	gated := &pgGatedDeliveryAssets{Service: assets, started: make(chan struct{}, 8), release: make(chan struct{})}
	request := computertask.DeliveryRequest{Version: 1, Direction: "download", ContentHash: hash, SizeBytes: int64(len(data)), Mime: "text/plain"}
	principal := pgTransferPrincipal(id, "native-pg-download", request)
	verifier := &pgTransferVerifier{principal: principal, denied: make(chan struct{}, 1)}
	messages := handlers.NewMessageHandler(log, nil, nil, nil, nil)
	messages.SetMediaService(assets)
	h, err := handlers.NewComputerTransferHandler(ctx, computertask.NewPostgresDeliveryService(store, gated), messages, verifier, log)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	e := echo.New()
	e.HTTPErrorHandler = server.NewHTTPErrorHandler(log) //nolint:contextcheck // Requests provide their own contexts.
	h.Register(e)                                        //nolint:contextcheck // Registers callbacks; each HTTP request supplies its own context.
	var drop atomic.Bool
	https := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/commit") && drop.Swap(false) {
			e.ServeHTTP(httptest.NewRecorder(), r)
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		e.ServeHTTP(w, r)
	}))
	defer https.Close()
	client, err := transferclient.NewTask(https.URL, id.OperationID, principal.DeliveryID, https.Client(), func(context.Context) (string, error) { return "Memoh-Transfer isolated-transfer-fixture", nil })
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	root := nativeDeliveryPrivateRoot(t)
	openQueue := func() *transferclient.DiskQueue {
		q, err := transferclient.OpenHerdrQueue(func(string) string { return root }, "native-pg-fixture-v1", transferclient.QueueLimits{Jobs: 4, Bytes: 8 << 20, JournalBytes: 1 << 20, Workers: 1})
		if err != nil {
			t.Fatal(err)
		}
		return q
	}
	queue := openQueue()
	defer func() { _ = queue.Close() }()
	job := transferclient.Job{Version: 1, DeliveryID: "native-pg-download", SnapshotID: "native-pg-download", BotID: id.Binding.BotID, OperationID: id.OperationID, LocatorRevision: "native-pg-fixture-v1", Phase: "queued", Spec: transferclient.Spec{SizeBytes: int64(len(data)), SHA256: hash, Mime: "text/plain"}}
	if err := queue.Admit(ctx, "download", job, nil); err != nil {
		t.Fatal(err)
	}
	ref := transferclient.Reference{DeliveryID: job.DeliveryID, OperationID: job.OperationID, BotID: job.BotID, ContentHash: hash, SizeBytes: int64(len(data)), Mime: "text/plain"}
	openPublication := func() *transferclient.NativePublication {
		p, err := queue.OpenNativePublication(func(ctx context.Context, candidate transferclient.Reference) error {
			if candidate != ref {
				return transferclient.ErrAuthorization
			}
			return verifier.Revalidate(ctx, principal)
		})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	publication := openPublication()
	defer func() { _ = publication.Close() }()
	step := func(binding *transferclient.AssociationBinding) transferclient.QueueStatus {
		r, err := queue.Inspect(job.DeliveryID)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		if r.AssociationDue.After(now) {
			now = r.AssociationDue
		}
		if err := queue.RunOne(ctx, job.DeliveryID, client, binding, now); err != nil {
			t.Fatal(err)
		}
		r, err = queue.Inspect(job.DeliveryID)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if status := step(nil); status.Job.Phase != "ready" {
		t.Fatalf("real Media Range/hash: %+v", status)
	}
	status := step(client.DeliveryBinding(&nativeLostPublication{NativePublication: publication, lose: true}))
	if status.Associated || status.AssociationState != transferclient.AssociationUnknown {
		t.Fatalf("native publish acknowledgment loss: %+v", status)
	}
	_ = publication.Close()
	_ = queue.Close()
	queue = openQueue()
	publication = openPublication()
	if err := queue.RequestCancel(job.DeliveryID); err != nil {
		t.Fatal(err)
	}
	drop.Store(true)
	status = step(client.DeliveryBinding(publication))
	if status.Associated || status.Job.Phase == "canceled" || status.AssociationState != transferclient.AssociationUnknown {
		t.Fatalf("lost async admission/cancel must stay unknown: %+v", status)
	}
	select {
	case <-gated.started:
	case <-time.After(5 * time.Second):
		t.Fatal("real asynchronous verification did not start")
	}
	op := dbstore.ComputerOperationRecord{ID: id.OperationID, BotID: id.Binding.BotID, UserID: id.Binding.UserID, SessionID: id.Binding.SessionID}
	current, err := store.ReadComputerOperation(ctx, op)
	if err != nil {
		t.Fatal(err)
	}
	current.State = "completed"
	if _, err := store.SaveComputerOperation(ctx, current); err != nil {
		t.Fatal(err)
	}
	verifier.revoked.Store(revoke)
	close(gated.release)
	if revoke {
		select {
		case <-verifier.denied:
		case <-time.After(5 * time.Second):
			t.Fatal("worker did not recheck revoked principal after scan")
		}
		status = step(client.DeliveryBinding(publication))
		if status.Associated || !status.AssociationBlocked || status.Job.Phase != "ready" {
			t.Fatal("revocation fabricated delivery/cancellation", status)
		}
		current, err = store.ReadComputerOperation(ctx, op)
		var details struct {
			Ledger struct {
				Entries map[string]struct {
					Outcome string `json:"outcome"`
				} `json:"entries"`
			} `json:"delivery_ledger"`
		}
		if err != nil || json.Unmarshal(current.Details, &details) != nil || details.Ledger.Entries[job.DeliveryID].Outcome != "pending" {
			t.Fatal("revoked scan committed its result", err)
		}
		return
	}
	for range 12 {
		status = step(client.DeliveryBinding(publication))
		if status.Associated {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !status.Associated || !status.Job.CancelRequested || status.Job.Phase == "canceled" {
		t.Fatalf("published NTFS winner lost: %+v", status)
	}
	current, err = store.ReadComputerOperation(ctx, op)
	if err != nil || current.State != "completed" || !strings.Contains(string(current.Details), "delivery_ledger") {
		t.Fatal("execution CAS lost delivery", err)
	}
	reader, err := publication.OpenPublished(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(got) != string(data) {
		t.Fatal("published bytes differ", err)
	}
}
