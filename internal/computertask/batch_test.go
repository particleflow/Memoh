package computertask

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/felinics/memoh/internal/computertarget"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/mcp"
	"github.com/felinics/memoh/internal/toolcontext"
)

// batchQueue is the in-memory ComputerTaskBatchQueue. It models the durable
// rules the SQL store enforces: create-or-return identity, one writer per batch
// row, and a CAS-guarded result envelope.
type batchQueue struct {
	mu      sync.Mutex
	records map[string]dbstore.ComputerTaskBatchRecord
	order   []string
	changes int
}

func newBatchQueue() *batchQueue {
	return &batchQueue{records: map[string]dbstore.ComputerTaskBatchRecord{}}
}

func (q *batchQueue) ClaimComputerTaskBatch(_ context.Context, rec dbstore.ComputerTaskBatchRecord) (dbstore.ComputerTaskBatchRecord, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, existing := range q.records {
		if existing.BotID == rec.BotID && existing.SessionID == rec.SessionID && existing.UserID == rec.UserID && existing.RunID == rec.RunID && existing.Fingerprint == rec.Fingerprint {
			return existing, false, nil
		}
	}
	q.records[rec.ID] = rec
	q.order = append(q.order, rec.ID)
	return rec, true, nil
}

func (q *batchQueue) ReadComputerTaskBatch(_ context.Context, id, bot, session, user string) (dbstore.ComputerTaskBatchRecord, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	rec, ok := q.records[id]
	if !ok || rec.BotID != bot || rec.SessionID != session || rec.UserID != user {
		return dbstore.ComputerTaskBatchRecord{}, ErrDenied
	}
	return rec, nil
}

func (q *batchQueue) ListSessionComputerTaskBatches(_ context.Context, bot, session, user string, limit int32) ([]dbstore.ComputerTaskBatchRecord, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := []dbstore.ComputerTaskBatchRecord{}
	for i := len(q.order) - 1; i >= 0 && len(out) < int(limit); i-- {
		if rec := q.records[q.order[i]]; rec.BotID == bot && rec.SessionID == session && rec.UserID == user {
			out = append(out, rec)
		}
	}
	return out, nil
}

func (q *batchQueue) ListActiveComputerTaskBatches(context.Context) ([]dbstore.ComputerTaskBatchRecord, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := []dbstore.ComputerTaskBatchRecord{}
	for _, id := range q.order {
		if rec := q.records[id]; rec.State != BatchCompleted {
			out = append(out, rec)
		}
	}
	return out, nil
}

func (q *batchQueue) ReadComputerTaskBatchDelivery(_ context.Context, id string) ([]byte, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	rec, ok := q.records[id]
	if !ok {
		return nil, ErrDenied
	}
	if len(rec.Delivery) == 0 {
		return []byte("{}"), nil
	}
	return rec.Delivery, nil
}

func (q *batchQueue) SaveComputerTaskBatchDelivery(_ context.Context, id string, expected, delivery []byte) ([]byte, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	rec, ok := q.records[id]
	if !ok || normalizeJSON(rec.Delivery) != normalizeJSON(expected) {
		return nil, ErrDenied
	}
	rec.Delivery = delivery
	q.records[id] = rec
	return delivery, nil
}

func (q *batchQueue) ChangeComputerTaskBatch(_ context.Context, id string, mutate func(*dbstore.ComputerTaskBatchRecord) error) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	rec, ok := q.records[id]
	if !ok {
		return ErrDenied
	}
	next := rec
	if err := mutate(&next); err != nil {
		return err
	}
	q.records[id] = next
	q.changes++
	return nil
}

func normalizeJSON(raw []byte) string {
	if len(raw) == 0 {
		return "{}"
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return string(raw)
	}
	out, _ := json.Marshal(value)
	return string(out)
}

// batchFixture drives the real MCP tool surface, the real service and the
// synthetic native transport. Only the queue is in-memory.
type batchFixture struct {
	t      *testing.T
	source *Source
	store  *fixtureStore
	queue  *batchQueue
	remote *batchTransport
}

func newBatchFixture(t *testing.T) *batchFixture {
	t.Helper()
	source, store, remote, _ := setupBatch(t)
	f := &batchFixture{t: t, source: source, store: store, queue: newBatchQueue(), remote: remote}
	// The scan rebuilds each batch's own persisted conversation authority; the
	// fixture mirrors the production rebuild's first guard: the host binding
	// must already be attached to the scan context, or the rebuild refuses.
	f.source.service.WithComputerBatches(f.queue, func(ctx context.Context, b toolcontext.ComputerBinding) (context.Context, error) {
		if current, ok := toolcontext.HostComputerBinding(ctx); !ok || current != b {
			return nil, ErrDenied
		}
		return ctx, nil
	})
	return f
}

func (f *batchFixture) call(tool string, args map[string]any, wantError bool) json.RawMessage {
	f.t.Helper()
	return invoke(f.t, standardClient(f.t, f.source, "pc-a", false), tool, args, wantError)
}

func (f *batchFixture) submit(items ...map[string]any) BatchSummary {
	f.t.Helper()
	raw := f.call("computer_task_batch_submit", map[string]any{"target_id": "pc-a", "items": items}, false)
	var body struct {
		Computer BatchSummary `json:"computer"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		f.t.Fatal(err)
	}
	if body.Computer.ID == "" {
		f.t.Fatalf("batch not created: %s", raw)
	}
	return body.Computer
}

func (f *batchFixture) status(id string) BatchSummary {
	f.t.Helper()
	raw := f.call("computer_task_batch_get", map[string]any{"batch_id": id}, false)
	var body struct {
		Batches []BatchSummary `json:"batches"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		f.t.Fatal(err)
	}
	if len(body.Batches) != 1 {
		f.t.Fatalf("batch read returned %d rows: %s", len(body.Batches), raw)
	}
	return body.Batches[0]
}

// scan runs the host-side advance pass once, exactly as the result scan does.
func (f *batchFixture) scan() {
	f.t.Helper()
	if err := f.source.ScanBatches(context.Background()); err != nil {
		f.t.Fatal(err)
	}
}

func (f *batchFixture) record(id string) dbstore.ComputerTaskBatchRecord {
	f.t.Helper()
	rec, err := f.queue.ReadComputerTaskBatch(context.Background(), id, "bot-a", "thread-a", "user-a")
	if err != nil {
		f.t.Fatal(err)
	}
	return rec
}

func (f *batchFixture) items(id string) []BatchItem {
	f.t.Helper()
	items, err := DecodeBatchItems(f.record(id).Items)
	if err != nil {
		f.t.Fatalf("stored items: %v", err)
	}
	return items
}

func batchItem(task string) map[string]any {
	return map[string]any{"kind": "herdr_pi_task", "task": task, "model": "synthetic-model"}
}

// failedPiResult is the immutable outcome a failed invocation saves. It carries
// no output and no preflight code, which is the plain "Pi exited non-zero" case.
func failedPiResult() computertarget.PiJSONResult {
	return computertarget.PiJSONResult{Outcome: "failed"}
}

// settle drives the real receipt reader until the batch leaves the running
// state, bounded so a stuck fixture fails instead of hanging.
func (f *batchFixture) settle(id string) BatchSummary {
	f.t.Helper()
	for range 60 {
		if state := f.status(id); state.State == BatchCompleted {
			return state
		}
		f.scan()
	}
	return f.status(id)
}

func TestBatchSubmitQueuesEveryItemAndReplayReturnsTheSameBatch(t *testing.T) {
	f := newBatchFixture(t)
	items := []map[string]any{batchItem("compute first"), batchItem("compute second"), batchItem("compute third")}
	created := f.submit(items...)
	if created.Total != 3 || created.Queued != 3 || created.State != BatchRunning || created.TargetID != "pc-a" || !created.Created {
		t.Fatalf("created batch=%+v", created)
	}
	if f.remote.runs != 0 {
		t.Fatal("submission dispatched work before any scan")
	}
	replay := f.submit(items...)
	if replay.ID != created.ID || replay.Created || replay.Total != 3 {
		t.Fatalf("replay minted a second batch: %+v", replay)
	}
	f.queue.mu.Lock()
	count := len(f.queue.records)
	f.queue.mu.Unlock()
	if count != 1 {
		t.Fatalf("batches stored=%d", count)
	}
	read := f.status(created.ID)
	if read.Total != 3 || read.Queued != 3 || read.Head == nil || read.Head.Position != 0 || read.Head.Task != "compute first" {
		t.Fatalf("queued view=%+v head=%+v", read, read.Head)
	}
}

func TestBatchSubmitRejectsBeforeCreatingAnything(t *testing.T) {
	f := newBatchFixture(t)
	cases := map[string]map[string]any{
		"no target":     {"items": []any{batchItem("compute")}},
		"empty items":   {"target_id": "pc-a", "items": []any{}},
		"foreign kind":  {"target_id": "pc-a", "items": []any{map[string]any{"kind": "foreground_bash", "task": "printf 42", "model": "synthetic-model"}}},
		"missing model": {"target_id": "pc-a", "items": []any{map[string]any{"kind": "herdr_pi_task", "task": "compute"}}},
		"stale model":   {"target_id": "pc-a", "items": []any{map[string]any{"kind": "herdr_pi_task", "task": "compute", "model": "gone/from-catalog"}}},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			f.call("computer_task_batch_submit", args, true)
			f.queue.mu.Lock()
			defer f.queue.mu.Unlock()
			if len(f.queue.records) != 0 {
				t.Fatal("rejected request created a batch")
			}
		})
	}
	if f.remote.runs != 0 {
		t.Fatal("rejected request dispatched work")
	}
}

func TestBatchRunsStrictlyInOrderOneItemAtATime(t *testing.T) {
	f := newBatchFixture(t)
	created := f.submit(batchItem("compute first"), batchItem("compute second"), batchItem("compute third"))
	f.scan()
	items := f.items(created.ID)
	if items[0].State != BatchItemInFlight || items[1].State != BatchItemQueued || items[2].State != BatchItemQueued {
		t.Fatalf("first pass states=%s %s %s", items[0].State, items[1].State, items[2].State)
	}
	if f.remote.runs != 1 {
		t.Fatalf("dispatched %d items in one pass", f.remote.runs)
	}
	// The lane is occupied, so another pass must not dispatch item #2.
	f.scan()
	items = f.items(created.ID)
	if f.remote.runs != 1 || items[1].State != BatchItemQueued {
		t.Fatalf("second pass overtook the lane: runs=%d second=%s", f.remote.runs, items[1].State)
	}
}

func TestBatchSettlesEachItemBeforeAdvancing(t *testing.T) {
	f := newBatchFixture(t)
	created := f.submit(batchItem("compute first"), batchItem("compute second"))
	final := f.settle(created.ID)
	if final.State != BatchCompleted || final.Total != 2 || final.Succeeded != 2 {
		t.Fatalf("final=%+v", final)
	}
	if f.remote.runs != 2 {
		t.Fatalf("runs=%d, want one per item", f.remote.runs)
	}
	items := f.items(created.ID)
	if items[0].OperationID == "" || items[1].OperationID == "" || items[0].OperationID == items[1].OperationID {
		t.Fatalf("items did not each claim their own receipt: %+v", items)
	}
}

func TestBatchItemFailureDoesNotStopLaterItems(t *testing.T) {
	f := newBatchFixture(t)
	created := f.submit(batchItem("compute first"), batchItem("compute second"))
	f.scan()
	// Force the first item to a terminal failed outcome the way the single-task
	// path saves one: an immutable parsed result on a released slot.
	f.store.mu.Lock()
	for id, rec := range f.store.records {
		var d Details
		if json.Unmarshal(rec.Details, &d) != nil || d.BatchOwner != created.ID {
			continue
		}
		d.Phase, d.Output = "pi_failed", ""
		failed := failedPiResult()
		d.Batch.Result = &failed
		rec.State = "completed"
		rec.Details, _ = json.Marshal(d)
		f.store.records[id] = rec
		break
	}
	f.store.mu.Unlock()
	final := f.settle(created.ID)
	if final.State != BatchCompleted || final.Total != 2 || final.Succeeded != 1 || final.Failed != 1 {
		t.Fatalf("failure isolation=%+v", final)
	}
	if f.remote.runs != 2 {
		t.Fatalf("later item did not run: runs=%d", f.remote.runs)
	}
	if f.items(created.ID)[0].Error == "" {
		t.Fatal("failed item kept no machine cause")
	}
}

func TestBatchCancelOnlyAffectsUndispatchedItems(t *testing.T) {
	f := newBatchFixture(t)
	created := f.submit(batchItem("compute first"), batchItem("compute second"), batchItem("compute third"))
	f.scan()
	raw := f.call("computer_task_batch_cancel", map[string]any{"batch_id": created.ID}, false)
	var body struct {
		Computer BatchSummary `json:"computer"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.Computer.InFlight != 1 || body.Computer.Canceled != 2 || body.Computer.State != BatchRunning {
		t.Fatalf("cancel result=%+v", body.Computer)
	}
	if f.remote.runs != 1 {
		t.Fatalf("cancel dispatched run %d", f.remote.runs)
	}
	final := f.settle(created.ID)
	if final.State != BatchCompleted || final.Canceled != 2 || final.Total != 3 {
		t.Fatalf("final=%+v", final)
	}
	if f.remote.runs != 1 {
		t.Fatalf("cancelled items ran: runs=%d", f.remote.runs)
	}
}

func TestBatchScopeIsPerConversation(t *testing.T) {
	f := newBatchFixture(t)
	created := f.submit(batchItem("compute first"))
	foreign := toolcontext.ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "thread-other", TargetID: "pc-a", RunID: "run-other"}
	ctx, err := toolcontext.WithComputerBinding(t.Context(), foreign)
	if err != nil {
		t.Fatal(err)
	}
	session := mcp.ToolSessionContext{BotID: foreign.BotID, UserID: foreign.UserID, SessionID: foreign.SessionID, RunID: foreign.RunID, RunContext: ctx, RequireActiveRun: true, RuntimeActive: true}
	if _, err := f.source.CallTool(ctx, session, "computer_task_batch_get", map[string]any{"batch_id": created.ID}); err == nil {
		t.Fatal("foreign conversation read the batch")
	}
	if _, err := f.source.CallTool(ctx, session, "computer_task_batch_cancel", map[string]any{"batch_id": created.ID}); err == nil {
		t.Fatal("foreign conversation cancelled the batch")
	}
	if f.status(created.ID).Canceled != 0 {
		t.Fatal("foreign cancel changed the batch")
	}
}

func TestBatchDispatchWithoutAuthorityLeavesItemsQueued(t *testing.T) {
	f := newBatchFixture(t)
	created := f.submit(batchItem("compute first"))
	// A scan that cannot rebuild the conversation authority must not dispatch on
	// its own identity; the item simply stays queued.
	f.source.service.WithComputerBatches(f.queue, func(context.Context, toolcontext.ComputerBinding) (context.Context, error) {
		return nil, ErrDenied
	})
	f.scan()
	if f.remote.runs != 0 {
		t.Fatal("unbound scan dispatched work")
	}
	if state := f.status(created.ID); state.Queued != 1 || state.State != BatchRunning {
		t.Fatalf("batch advanced without authority: %+v", state)
	}
}

func TestBatchBlockedReportsTheHoldingOperation(t *testing.T) {
	f := newBatchFixture(t)
	created := f.submit(batchItem("compute first"))
	f.scan()
	items := f.items(created.ID)
	if items[0].OperationID == "" || items[0].State != BatchItemInFlight {
		t.Fatalf("dispatch did not record the receipt: %+v", items[0])
	}
	// A freshly dispatched item has not resolved yet; nothing behind it may run.
	state := f.status(created.ID)
	if state.InFlight != 1 || state.Queued != 0 || state.BlockedOn != "" {
		t.Fatalf("in-flight item before an unresolved read: %+v", state)
	}
}

// TestBatchUnknownOutcomeHoldsTheSlot: an item whose dispatch outcome is unknown
// and whose receipt never becomes readable keeps the batch blocked on that
// operation. Nothing behind it may run, and the batch never re-dispatches the
// unknown attempt.
func TestBatchUnknownOutcomeHoldsTheSlot(t *testing.T) {
	f := newBatchFixture(t)
	created := f.submit(batchItem("compute first"), batchItem("compute second"))
	f.remote.lost, f.remote.missing = true, true
	f.scan()
	items := f.items(created.ID)
	if items[0].State != BatchItemInFlight || items[0].OperationID == "" {
		t.Fatalf("unknown dispatch did not hold the slot: %+v", items[0])
	}
	if state := f.status(created.ID); state.State != BatchBlocked || state.BlockedOn != items[0].OperationID {
		t.Fatalf("batch state=%+v blocked_on=%q", state, state.BlockedOn)
	}
	for range 3 {
		f.scan()
	}
	if f.remote.runs != 1 {
		t.Fatalf("unknown attempt was re-dispatched or the lane advanced: runs=%d", f.remote.runs)
	}
	if items := f.items(created.ID); items[1].State != BatchItemQueued {
		t.Fatalf("item behind the unknown attempt advanced: %s", items[1].State)
	}
}

// TestBatchPausedAdmissionDefersWithoutConsuming: a dispatched item whose
// admission gate is closed stays queued instead of failing, so the queue is
// never consumed by a temporary condition.
func TestBatchPausedAdmissionDefersWithoutConsuming(t *testing.T) {
	f := newBatchFixture(t)
	created := f.submit(batchItem("compute first"))
	authorize := f.source.service.authorize
	f.source.service.authorize = func(context.Context, toolcontext.ComputerBinding, string, string, string) error {
		return fmt.Errorf("synthetic pause: %w", ErrPreflightUnavailable)
	}
	t.Cleanup(func() { f.source.service.authorize = authorize })
	f.scan()
	final := f.status(created.ID)
	if final.Queued != 1 || final.Failed != 0 || final.State != BatchRunning {
		t.Fatalf("temporary gate consumed the item: %+v", final)
	}
	if f.remote.runs != 0 {
		t.Fatalf("deferred item dispatched: runs=%d", f.remote.runs)
	}
	f.source.service.authorize = authorize
	f.scan()
	if items := f.items(created.ID); items[0].State != BatchItemInFlight {
		t.Fatalf("gate reopening did not dispatch: %s", items[0].State)
	}
}

// TestBatchStaleTransitionDoesNotRevertAConcurrentCancel: a transition computed
// from a stale snapshot must fail its write instead of reverting a cancel that
// committed while the scan was advancing.
func TestBatchStaleTransitionDoesNotRevertAConcurrentCancel(t *testing.T) {
	f := newBatchFixture(t)
	created := f.submit(batchItem("compute first"), batchItem("compute second"))
	stale := f.record(created.ID)
	f.call("computer_task_batch_cancel", map[string]any{"batch_id": created.ID}, false)
	items, err := DecodeBatchItems(stale.Items)
	if err != nil {
		t.Fatal(err)
	}
	items[0].State = BatchItemInFlight
	if err := f.source.service.persistBatchItems(context.Background(), stale, items); err == nil {
		t.Fatal("stale transition overwrote a concurrent cancel")
	}
	if state := f.status(created.ID); state.Canceled != 2 || state.State != BatchCompleted {
		t.Fatalf("cancel was reverted: %+v", state)
	}
}

func TestBatchUnavailableWithoutAQueue(t *testing.T) {
	source, _, _, _ := setupBatch(t)
	client := standardClient(t, source, "pc-a", false)
	raw := invoke(t, client, "computer_task_batch_submit", map[string]any{"target_id": "pc-a", "items": []any{batchItem("compute")}}, true)
	if !strings.Contains(string(raw), "batches_unavailable") {
		t.Fatalf("missing unavailable fact: %s", raw)
	}
	// The batch tools stay listed, and the single-task path is unaffected.
	single := parseResult(t, invoke(t, client, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "model": "synthetic-model", "task": "compute 42"}, false))
	if single.ID == "" {
		t.Fatal("single-task path broke without a batch queue")
	}
}
