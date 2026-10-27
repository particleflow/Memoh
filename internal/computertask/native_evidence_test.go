package computertask

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/toolcontext"
)

// The retired writer bounded saved input receipts to this historical size.
const historicalInputReceiptLimit = 64

type evidenceTransport struct {
	base           *followupTransport
	status, output string
	seq            *uint64
	readFailure    bool
	afterRead      func()
}

func (f *evidenceTransport) call(ctx context.Context, scope agenvo.Scope, target agenvo.Target, method string, params map[string]any) (agenvo.Receipt, error) {
	raw, err := f.base.call(ctx, scope, target, method, params)
	if err != nil {
		return raw, err
	}
	if method == "agent.get" {
		a := map[string]any{"name": f.base.base.agentName, "agent": "pi", "workspace_id": "w-synthetic", "pane_id": "p-agent", "terminal_id": "t-agent", "agent_status": f.status, "interactive_ready": true}
		if f.base.session != nil {
			a["agent_session"] = f.base.session
		}
		if f.seq != nil {
			a["completion_seq"] = *f.seq
		}
		return envelope(map[string]any{"session": "synthetic", "result": map[string]any{"agent": a}}), nil
	}
	if method == "agent.read" {
		if f.afterRead != nil {
			f.afterRead()
		}
		if f.readFailure {
			return agenvo.Receipt{}, errors.New("PRIVATE lost read response")
		}
		return envelope(map[string]any{"session": "synthetic", "output": f.output}), nil
	}
	return raw, nil
}

type evidenceAssets struct {
	data                   map[string]string
	ingests, opens         int
	afterIngest, afterOpen func()
	corrupt                bool
}

func (a *evidenceAssets) Ingest(_ context.Context, in media.IngestInput) (media.Asset, error) {
	raw, err := io.ReadAll(in.Reader)
	if err != nil {
		return media.Asset{}, err
	}
	a.ingests++
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	a.data[hash] = string(raw)
	if a.afterIngest != nil {
		a.afterIngest()
	}
	return media.Asset{BotID: in.BotID, ContentHash: hash}, nil
}

func (a *evidenceAssets) Open(_ context.Context, bot, hash string) (io.ReadCloser, media.Asset, error) {
	a.opens++
	data, ok := a.data[hash]
	if !ok {
		return nil, media.Asset{}, errors.New("PRIVATE missing synthetic object")
	}
	if a.corrupt {
		data = "different bytes"
	}
	if a.afterOpen != nil {
		a.afterOpen()
	}
	return io.NopCloser(strings.NewReader(data)), media.Asset{BotID: bot, ContentHash: hash}, nil
}

func evidenceFixture(t *testing.T) (*Source, *fixtureStore, *evidenceTransport, *evidenceAssets, Result) {
	t.Helper()
	source, store, base, initial := followupFixture(t, true)
	seq := uint64(5)
	f := &evidenceTransport{base: base, status: "done", output: "old round output", seq: &seq}
	a := &evidenceAssets{data: map[string]string{}}
	source.service.relay = agenvo.NewService(store, permissionChecker{}, f.call)
	source.service.assets = a
	return source, store, f, a, initial
}

func storedEvidence(t *testing.T, store *fixtureStore, id string) Details {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	var d Details
	if err := json.Unmarshal(store.records[id].Details, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func assertSampleOnly(t *testing.T, r Result) {
	t.Helper()
	if r.State != "unknown" || r.ProcessStopped || r.Observation == nil || r.Observation.TaskSuccessVerified {
		t.Fatal("sample became a completion receipt")
	}
	for _, sample := range r.NativeEvidence.Observations {
		if sample.Association != "sample_only" || sample.ResultOwnership != "unknown" {
			t.Fatal("sample claims input ownership")
		}
	}
	for _, input := range r.Followups {
		if input.State != "accepted" || input.BusinessOutcome != "unknown" {
			t.Fatal("sampling settled an input")
		}
	}
}

func TestNativeEvidenceRetainsSamplesExportsAndRestart(t *testing.T) {
	source, store, f, a, initial := evidenceFixture(t)
	client := standardClient(t, source, "pc-a", false)
	args := map[string]any{"operation_id": initial.ID}
	old := parseResult(t, invoke(t, client, "computer_task_result", args, false))
	oldID := old.NativeEvidence.LatestObservationID
	oldExport := parseResult(t, invoke(t, client, "computer_task_artifact", args, false))
	// Historical input receipts are fixture data, never newly dispatched prompts.
	row := store.records[initial.ID]
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(row.Details, &fields)
	fields["followups"], _ = json.Marshal([]FollowupRecord{{RequestID: "historical-input-a", Progress: agenvo.AgentFollowupProgress{State: "accepted"}}, {RequestID: "historical-input-b", Progress: agenvo.AgentFollowupProgress{State: "accepted"}}})
	row.Details, _ = json.Marshal(fields)
	store.records[row.ID] = row

	f.status, f.output = "working", "next round in progress"
	next := parseResult(t, invoke(t, client, "computer_task_result", args, false))
	assertSampleOnly(t, next)
	if next.NativeEvidence.Count != 2 || len(old.NativeEvidence.Observations[0].Inputs) != 0 || len(next.NativeEvidence.Observations[0].Inputs) != 2 || old.NativeEvidence.Observations[0].Status != "done" || next.NativeEvidence.Observations[0].Status != "working" || next.ArtifactHash != oldExport.ArtifactHash {
		t.Fatal("old/new rounds or export association conflated")
	}
	newExport := parseResult(t, invoke(t, client, "computer_task_artifact", args, false))
	if newExport.ArtifactHash == oldExport.ArtifactHash || newExport.NativeEvidence.ExportCount != 2 || a.ingests != 2 {
		t.Fatal("distinct samples did not retain distinct exports")
	}
	duplicate := parseResult(t, invoke(t, client, "computer_task_result", args, false))
	if duplicate.NativeEvidence.Count != 2 || duplicate.NativeEvidence.RecordingState != "duplicate" {
		t.Fatal("identical sample not deduplicated")
	}
	oldArgs := map[string]any{"operation_id": initial.ID, "observation_id": oldID}
	replay := parseResult(t, invoke(t, client, "computer_task_artifact", oldArgs, false))
	if replay.ArtifactHash != oldExport.ArtifactHash || replay.NativeEvidence.LatestObservationID != next.NativeEvidence.LatestObservationID || replay.NativeEvidence.ExportCount != 2 || a.ingests != 2 || a.opens != 2 {
		t.Fatal("old export overwritten, reread or duplicated")
	}
	for _, e := range replay.NativeEvidence.Exports {
		if e.Availability != "not_rechecked" || e.ResultOwnership != "unknown" || e.BytesVerifiedAt == "" {
			t.Fatal("historical reference claims fresh availability or ownership")
		}
	}
	before := observationSnapshot(t, store)
	restored := &fixtureStore{records: map[string]dbstore.ComputerOperationRecord{}, mounts: store.mounts}
	if err := json.Unmarshal(before, &restored.records); err != nil {
		t.Fatal(err)
	}
	restarted := NewSource(NewService(restored, restored, agenvo.NewService(restored, permissionChecker{}, f.call), a, source.service.authorize))
	client = standardClient(t, restarted, "pc-a", false)
	calls := slices.Clone(f.base.methods)
	saved := parseResult(t, invoke(t, client, "computer_task_get", args, false))
	invoke(t, client, "computer_task_artifact", oldArgs, false)
	if saved.NativeEvidence.Count != 2 || saved.NativeEvidence.ExportCount != 2 || saved.NativeEvidence.LatestObservationID != next.NativeEvidence.LatestObservationID || saved.NativeEvidence.LatestExportObservationID != oldID {
		t.Fatal("restart lost history")
	}
	if !bytes.Equal(before, observationSnapshot(t, restored)) || !slices.Equal(calls, f.base.methods) || a.ingests != 2 {
		t.Fatal("restart replay changed persisted history or contacted provider")
	}
	for _, method := range f.base.methods {
		if method != "agent.get" && method != "agent.read" {
			t.Fatalf("rebuilt Agent via %s", method)
		}
	}
	private := storedEvidence(t, restored, initial.ID)
	if private.NativeEvidence.Observations[0].Session.Value != syntheticAgentSession("original").Value || private.NativeEvidence.Observations[0].Observation.Output != "old round output" {
		t.Fatal("private session/output not durable")
	}
}

func TestNativeEvidenceSequenceGapsSessionsAndTruncationAreOnlySamples(t *testing.T) {
	source, store, f, _, initial := evidenceFixture(t)
	client := standardClient(t, source, "pc-a", false)
	args := map[string]any{"operation_id": initial.ID}
	// No event stream is consumed. Backward/gapped sequence samples must not
	// infer missing rounds, repair history, or assign any input's result.
	// This synthetic Go transport supplies the integer directly. The decimal
	// string preserves the received value without additional rounding here;
	// it cannot recover rounding by Agenvo's upstream JSON.parse(stdout), nor
	// establish end-to-end uint64 precision above JavaScript's safe range.
	for _, seq := range []uint64{5, 3, 9007199254740993} {
		f.seq = &seq
		wire := invoke(t, client, "computer_task_result", args, false)
		if strings.Contains(string(wire), "PRIVATE") {
			t.Fatal("private session metadata leaked")
		}
		assertSampleOnly(t, parseResult(t, wire))
	}
	saved := storedEvidence(t, store, initial.ID)
	if len(saved.NativeEvidence.Observations) != 3 || saved.NativeEvidence.Observations[2].Observation.CompletionSeq != "9007199254740993" {
		t.Fatal("sequence reordered, rounded or used to infer a round")
	}
	f.readFailure = true
	failed := parseResult(t, invoke(t, client, "computer_task_result", args, true))
	if failed.NativeEvidence.Count != 3 || failed.NativeEvidence.RecordingState != "observation_unavailable" {
		t.Fatal("lost read created evidence")
	}
	f.readFailure = false
	f.base.session = syntheticAgentSession("new-session")
	changed := parseResult(t, invoke(t, client, "computer_task_result", args, false))
	if changed.Observation.SessionRelation != "changed" || changed.NativeEvidence.Count != 4 {
		t.Fatal("changed session hidden by matching Agent name")
	}
	f.base.session = nil
	f.seq = nil
	f.output = strings.Repeat("文", 6000)
	b := toolcontext.ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "thread-a", RunID: "read-run", TargetID: "pc-a"}
	ctx, err := toolcontext.WithComputerBinding(t.Context(), b)
	if err != nil {
		t.Fatal(err)
	}
	// The existing gateway may prune large terminal text. Verify byte truncation
	// at the real service boundary, and retrieve history through the compact API.
	missing, err := source.service.observe(ctx, b, initial.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	page := parseResult(t, invoke(t, client, "computer_task_get", map[string]any{"operation_id": initial.ID, "history_offset": 4}, false))
	if page.NativeEvidence.Observations[0].SessionRelation != "unavailable" || !page.NativeEvidence.Observations[0].Truncated {
		t.Fatal("compact history lost metadata behind large latest output")
	}
	if missing.Observation.SessionRelation != "unavailable" || missing.Observation.CompletionSeq != "" || !missing.Truncated || !missing.Observation.Truncated || len(missing.Output) > 16384 || !utf8.ValidString(missing.Output) {
		t.Fatal("missing metadata or truncation misrepresented")
	}
	if slices.Contains(f.base.methods, "agent.prompt") {
		t.Fatal("sampling redispatched")
	}
	if storedEvidence(t, store, initial.ID).Agent.AgentSession.Value != syntheticAgentSession("original").Value {
		t.Fatal("sampling rebound original session")
	}
}

func TestNativeEvidenceCapacityPreservesOldEvidence(t *testing.T) {
	source, store, f, a, initial := evidenceFixture(t)
	source.service.WithNativeObservationLimit(1)
	client := standardClient(t, source, "pc-a", false)
	args := map[string]any{"operation_id": initial.ID}
	old := parseResult(t, invoke(t, client, "computer_task_result", args, false))
	before := observationSnapshot(t, store)
	f.output = "new sample over capacity"
	full := parseResult(t, invoke(t, client, "computer_task_result", args, true))
	if !full.NativeEvidence.Full || full.NativeEvidence.RecordingState != "capacity_reached" || full.Output != old.Output || !bytes.Equal(before, observationSnapshot(t, store)) {
		t.Fatal("full ledger evicted evidence or silently replaced latest view")
	}
	invoke(t, client, "computer_task_artifact", args, false)
	if a.ingests != 1 {
		t.Fatal("capacity blocked old export")
	}
	source.service.WithNativeObservationLimit(2)
	expanded := parseResult(t, invoke(t, client, "computer_task_result", args, false))
	if expanded.NativeEvidence.Count != 2 || expanded.NativeEvidence.Limit != 2 {
		t.Fatal("configured expansion did not retain both samples")
	}
	source.service.WithNativeObservationLimit(1)
	duplicate := parseResult(t, invoke(t, client, "computer_task_result", args, false))
	if duplicate.NativeEvidence.Count != 2 || duplicate.NativeEvidence.Limit != 1 || !duplicate.NativeEvidence.Full {
		t.Fatal("lowering capacity deleted evidence")
	}
}

func TestNativeEvidenceLegacyHashIsNotRelabeled(t *testing.T) {
	source, store, _, a, initial := evidenceFixture(t)
	r := store.records[initial.ID]
	var d Details
	_ = json.Unmarshal(r.Details, &d)
	d.Output = "surviving latest view"
	d.Observation = &agenvo.AgentObservation{Output: d.Output, Status: "done"}
	d.ArtifactHash = strings.Repeat("a", 64)
	r.Details, _ = json.Marshal(d)
	store.records[r.ID] = r
	client := standardClient(t, source, "pc-a", false)
	got := parseResult(t, invoke(t, client, "computer_task_result", map[string]any{"operation_id": r.ID}, false))
	history := got.NativeEvidence
	if history.Count != 2 {
		t.Fatal("lost imported view")
	}
	legacy := parseResult(t, invoke(t, client, "computer_task_get", map[string]any{"operation_id": r.ID, "history_offset": 0}, false))
	history = legacy.NativeEvidence
	if history.Count != 2 || history.Observations[0].Origin != "legacy_latest_view" || history.Observations[0].FirstObservedAt != "" || history.Exports[0].ObservationID != "" || history.Exports[0].BytesVerifiedAt != "" || history.Exports[0].ArtifactHash != d.ArtifactHash {
		t.Fatal("legacy hash assigned to unproven output/time")
	}
	exported := parseResult(t, invoke(t, client, "computer_task_artifact", map[string]any{"operation_id": r.ID, "observation_id": history.Observations[0].ID}, false))
	if exported.NativeEvidence.ExportCount != 2 || a.ingests != 1 || exported.NativeEvidence.Exports[0].ObservationID != "" {
		t.Fatal("verified legacy view overwrote unassigned hash")
	}
}

func TestNativeEvidenceAuthorityRevocationAndExportVerification(t *testing.T) {
	for _, change := range []string{"before", "after_read", "after_ingest", "after_open", "corrupt_bytes"} {
		t.Run(change, func(t *testing.T) {
			source, store, f, a, initial := evidenceFixture(t)
			client := standardClient(t, source, "pc-a", false)
			args := map[string]any{"operation_id": initial.ID}
			invoke(t, client, "computer_task_result", args, false)
			before := observationSnapshot(t, store)
			method := "computer_task_artifact"
			switch change {
			case "before":
				store.revoked = true
			case "after_read":
				method = "computer_task_result"
				f.output = "must not persist"
				f.afterRead = func() { store.revoked = true }
			case "after_ingest":
				a.afterIngest = func() { store.revoked = true }
			case "after_open":
				a.afterOpen = func() { store.revoked = true }
			case "corrupt_bytes":
				a.corrupt = true
			}
			wire := invoke(t, client, method, args, true)
			if strings.Contains(string(wire), "artifact_hash") || strings.Contains(string(wire), "native_evidence") || strings.Contains(string(wire), "PRIVATE") || !bytes.Equal(before, observationSnapshot(t, store)) {
				t.Fatal("failed/revoked operation exposed or replaced evidence")
			}
		})
	}
}

func TestNativeEvidenceReusesScopeForHistoryAndExports(t *testing.T) {
	for _, change := range []string{"user", "bot", "session", "selected_pc", "nas", "grant_removed"} {
		t.Run(change, func(t *testing.T) {
			source, store, f, _, initial := evidenceFixture(t)
			client := standardClient(t, source, "pc-a", false)
			sampled := parseResult(t, invoke(t, client, "computer_task_result", map[string]any{"operation_id": initial.ID}, false))
			b := toolcontext.ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "thread-a", RunID: "next-run", TargetID: "pc-a"}
			switch change {
			case "user":
				b.UserID = "user-b"
			case "bot":
				b.BotID = "bot-b"
			case "session":
				b.SessionID = "thread-b"
			case "selected_pc":
				b.TargetID = "pc-b"
			case "nas":
				b.TargetID = "native"
			case "grant_removed":
				delete(store.mounts, "pc-a")
			}
			ctx, err := toolcontext.WithComputerBinding(t.Context(), b)
			if err != nil {
				t.Fatal(err)
			}
			calls := slices.Clone(f.base.methods)
			history, readErr := source.service.observe(ctx, b, initial.ID, false)
			exported, exportErr := source.service.artifactForObservation(ctx, b, initial.ID, sampled.NativeEvidence.LatestObservationID)
			if change == "nas" {
				if readErr != nil || exportErr != nil || history.NativeEvidence == nil || exported.ArtifactHash == "" {
					t.Fatal("existing NAS grant not reused")
				}
			} else if readErr == nil || exportErr == nil || history.ID != "" || exported.ID != "" {
				t.Fatal("scope escaped")
			}
			if !slices.Equal(calls, f.base.methods) {
				t.Fatal("history/export contacted Agent")
			}
		})
	}
}

func TestNativeEvidenceCASRetainsConcurrentMetadataAndLostAcknowledgement(t *testing.T) {
	for _, fault := range []string{"conflict", "committed_response_lost", "save_failed"} {
		t.Run(fault, func(t *testing.T) {
			source, store, f, _, initial := evidenceFixture(t)
			client := standardClient(t, source, "pc-a", false)
			args := map[string]any{"operation_id": initial.ID}
			invoke(t, client, "computer_task_result", args, false)
			injected := false
			source.service.store = followupFaultStore{ComputerOperationStore: store, save: func(ctx context.Context, r dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error) {
				if fault == "save_failed" {
					return r, errors.New("PRIVATE save failed")
				}
				if !injected {
					injected = true
					if fault == "conflict" {
						current, err := store.ReadComputerOperation(ctx, r)
						if err != nil {
							t.Fatal(err)
						}
						var fields map[string]json.RawMessage
						_ = json.Unmarshal(current.Details, &fields)
						fields["future_metadata"] = json.RawMessage(`{"keep":"concurrent"}`)
						fields["delivery_ledger"] = json.RawMessage(`{"keep":"delivery"}`)
						fields["followups"] = json.RawMessage(`[{"request_id":"synthetic-receipt","progress":{"state":"accepted"}}]`)
						current.CancelRequested = true
						current.Details, _ = json.Marshal(fields)
						if _, err = store.SaveComputerOperation(ctx, current); err != nil {
							t.Fatal(err)
						}
					} else {
						if _, err := store.SaveComputerOperation(ctx, r); err != nil {
							t.Fatal(err)
						}
						return r, errors.New("PRIVATE response lost after commit")
					}
				}
				return store.SaveComputerOperation(ctx, r)
			}}
			f.output = "next sample"
			before := observationSnapshot(t, store)
			wire := invoke(t, client, "computer_task_result", args, fault == "save_failed")
			if strings.Contains(string(wire), "PRIVATE") {
				t.Fatal("private error leak")
			}
			d := storedEvidence(t, store, initial.ID)
			if fault == "save_failed" {
				if !bytes.Equal(before, observationSnapshot(t, store)) {
					t.Fatal("failed save changed evidence")
				}
			} else if len(d.NativeEvidence.Observations) != 2 {
				t.Fatal("conflict/lost ack lost or duplicated sample")
			}
			if fault == "conflict" {
				var fields map[string]json.RawMessage
				_ = json.Unmarshal(store.records[initial.ID].Details, &fields)
				if string(fields["future_metadata"]) != `{"keep":"concurrent"}` || string(fields["delivery_ledger"]) != `{"keep":"delivery"}` || len(d.Followups) != 1 || !store.records[initial.ID].CancelRequested {
					t.Fatal("CAS retry erased other ledger metadata")
				}
				if len(d.NativeEvidence.Observations[1].Inputs) != 0 {
					t.Fatal("later input retroactively attributed to earlier read")
				}
			}
			if !slices.Equal(f.base.methods, []string{"agent.get", "agent.read", "agent.get", "agent.read"}) {
				t.Fatal("CAS retry repeated provider RPC")
			}
		})
	}
}

func TestNativeEvidenceCompactPagesWithAllInputReceipts(t *testing.T) {
	source, store, f, _, initial := evidenceFixture(t)
	r := store.records[initial.ID]
	var d Details
	_ = json.Unmarshal(r.Details, &d)
	for range historicalInputReceiptLimit {
		d.Followups = append(d.Followups, FollowupRecord{RequestID: uuid.NewString(), Progress: agenvo.AgentFollowupProgress{State: "accepted"}})
	}
	r.Details, _ = json.Marshal(d)
	store.records[r.ID] = r
	b := toolcontext.ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "thread-a", RunID: "read-run", TargetID: "pc-a"}
	ctx, err := toolcontext.WithComputerBinding(t.Context(), b)
	if err != nil {
		t.Fatal(err)
	}
	f.output = strings.Repeat("large output ", 1500)
	if _, err = source.service.observe(ctx, b, r.ID, true); err != nil {
		t.Fatal(err)
	}
	f.output = "another sample"
	if _, err = source.service.observe(ctx, b, r.ID, true); err != nil {
		t.Fatal(err)
	}
	client := standardClient(t, source, "pc-a", false)
	calls := slices.Clone(f.base.methods)
	for offset := range 2 {
		wire := invoke(t, client, "computer_task_get", map[string]any{"operation_id": r.ID, "history_offset": offset}, false)
		got := parseResult(t, wire)
		if got.NativeEvidence == nil || got.NativeEvidence.Count != 2 || got.NativeEvidence.HistoryOffset != offset || len(got.NativeEvidence.Observations) != 1 || len(got.NativeEvidence.Observations[0].Inputs) != historicalInputReceiptLimit || got.Output != "" || got.Observation != nil {
			t.Fatal("bounded page lost receipts or exposed raw terminal output")
		}
		if offset == 0 && (got.NativeEvidence.NextHistoryOffset == nil || *got.NativeEvidence.NextHistoryOffset != 1) {
			t.Fatal("missing next page")
		}
	}
	if !slices.Equal(calls, f.base.methods) {
		t.Fatal("history page polled Agent")
	}
	for _, offset := range []int{-1, 2} {
		invoke(t, client, "computer_task_get", map[string]any{"operation_id": r.ID, "history_offset": offset}, true)
	}
	invoke(t, client, "computer_task_artifact", map[string]any{"operation_id": r.ID, "observation_id": uuid.NewString()}, true)
}

func TestNativeEvidenceExportRetainsObservationArrivingDuringIngest(t *testing.T) {
	source, store, f, a, initial := evidenceFixture(t)
	client := standardClient(t, source, "pc-a", false)
	args := map[string]any{"operation_id": initial.ID}
	old := parseResult(t, invoke(t, client, "computer_task_result", args, false))
	b := toolcontext.ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "thread-a", RunID: "read-run", TargetID: "pc-a"}
	ctx, err := toolcontext.WithComputerBinding(t.Context(), b)
	if err != nil {
		t.Fatal(err)
	}
	a.afterIngest = func() {
		f.output = "new output while old export is pending"
		if _, err := source.service.observe(ctx, b, initial.ID, true); err != nil {
			t.Fatal(err)
		}
	}
	exported := parseResult(t, invoke(t, client, "computer_task_artifact", args, false))
	saved := storedEvidence(t, store, initial.ID)
	if exported.NativeEvidence.Count != 2 || exported.NativeEvidence.Exports[0].ObservationID != old.NativeEvidence.LatestObservationID || exported.NativeEvidence.LatestObservationID == old.NativeEvidence.LatestObservationID || saved.Output != f.output || len(saved.NativeEvidence.Exports) != 1 {
		t.Fatal("export erased newer sample or rebound its bytes")
	}
	a.afterIngest = nil
	before := observationSnapshot(t, store)
	invoke(t, client, "computer_task_artifact", map[string]any{"operation_id": initial.ID, "observation_id": old.NativeEvidence.LatestObservationID}, false)
	if !bytes.Equal(before, observationSnapshot(t, store)) || a.ingests != 1 {
		t.Fatal("repeat export changed history")
	}
}

func TestNativeEvidenceHistoryPagePreservesCanceledOperationFacts(t *testing.T) {
	source, _, f, _, initial := evidenceFixture(t)
	client := standardClient(t, source, "pc-a", false)
	args := map[string]any{"operation_id": initial.ID}
	invoke(t, client, "computer_task_result", args, false)
	calls := slices.Clone(f.base.methods)
	invoke(t, client, "computer_task_cancel", args, false)
	ordinaryWire := invoke(t, client, "computer_task_get", args, false)
	pageWire := invoke(t, client, "computer_task_get", map[string]any{"operation_id": initial.ID, "history_offset": 0}, false)
	ordinary, page := parseResult(t, ordinaryWire), parseResult(t, pageWire)
	if !ordinary.CancelRequested || !ordinary.CleanupRequired || ordinary.ProcessStopped || ordinary.State != "unknown" {
		t.Fatal("fixture did not retain cancellation intent without proving a stop")
	}
	// Inspect the actual JSON: an omitted summary field must not silently
	// become an explicit false fact about the original operation.
	facts := func(wire json.RawMessage) string {
		var value struct {
			Computer map[string]json.RawMessage `json:"computer"`
		}
		if err := json.Unmarshal(wire, &value); err != nil {
			t.Fatal(err)
		}
		selected := map[string]json.RawMessage{}
		for _, key := range []string{"state", "phase", "cancel_requested", "cleanup_required", "process_stopped", "truncated", "native_agent_name", "workspace_id", "pane_id"} {
			if raw, ok := value.Computer[key]; ok {
				selected[key] = raw
			}
		}
		raw, err := json.Marshal(selected)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	if facts(pageWire) != facts(ordinaryWire) {
		t.Fatalf("history operation facts = %s; ordinary get = %s", facts(pageWire), facts(ordinaryWire))
	}
	if page.NativeEvidence == nil || page.NativeEvidence.Count != 1 || page.Output != "" || page.Observation != nil || len(page.Followups) != 0 {
		t.Fatal("history page lost evidence or included unbounded latest-view content")
	}
	if !slices.Equal(calls, f.base.methods) || slices.Contains(f.base.methods, "agent.prompt") {
		t.Fatal("cancel/history lookup contacted or redispatched the Agent")
	}
}
