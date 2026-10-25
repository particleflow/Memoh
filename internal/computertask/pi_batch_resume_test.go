package computertask

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/toolcontext"
)

type resumeOperationStore struct {
	*nativeTaskStore
	mu                sync.Mutex
	faultPhase        string
	commitBeforeError bool
	paused            bool
	admitted          []string
	reads             atomic.Int32
	arrived           chan int64
	release           chan struct{}
	onSaved           func(string)
}

func (s *resumeOperationStore) ReadComputerOperation(ctx context.Context, key dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error) {
	r, err := s.nativeTaskStore.ReadComputerOperation(ctx, key)
	if err == nil && s.arrived != nil && s.reads.Add(1) <= 2 {
		s.arrived <- r.Revision
		select {
		case <-s.release:
		case <-ctx.Done():
			return r, ctx.Err()
		}
	}
	return r, err
}

func (s *resumeOperationStore) SaveComputerOperation(ctx context.Context, r dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error) {
	return s.save(ctx, r, false)
}

func (s *resumeOperationStore) SaveComputerOperationWithAdmission(ctx context.Context, r dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error) {
	return s.save(ctx, r, true)
}

func (s *resumeOperationStore) save(ctx context.Context, r dbstore.ComputerOperationRecord, admission bool) (dbstore.ComputerOperationRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var details Details
	if err := json.Unmarshal(r.Details, &details); err != nil || details.Batch == nil {
		return r, ErrInvalid
	}
	phase := details.Batch.Progress.Phase
	if admission && s.paused {
		return r, db.ErrComputerAdmissionPaused
	}
	if s.faultPhase != "" && phase == s.faultPhase && !s.commitBeforeError {
		return r, errors.New("synthetic save rejected before commit")
	}
	next, err := s.nativeTaskStore.SaveComputerOperation(ctx, r)
	if err == nil && admission {
		s.admitted = append(s.admitted, phase)
	}
	if err == nil && s.onSaved != nil {
		s.onSaved(phase)
	}
	if err == nil && s.faultPhase != "" && phase == s.faultPhase {
		return next, errors.New("synthetic lost save acknowledgement")
	}
	return next, err
}

type batchResumeFixture struct {
	t            *testing.T
	base         *nativeTaskFixture
	store        *resumeOperationStore
	mu           sync.Mutex
	calls        map[string]int
	label        string
	mode         string
	lostMethod   string
	tabCreated   bool
	op           dbstore.ComputerOperationRecord
	progress     agenvo.BatchProgress
	input        Submit
	policyDenied atomic.Bool
}

func newBatchResumeFixture(t *testing.T) (*batchResumeFixture, context.Context) {
	t.Helper()
	return newBatchResumeFixtureWithInput(t, Submit{Kind: "herdr_pi_task", Task: "synthetic original batch instruction"})
}

func newBatchResumeFixtureWithInput(t *testing.T, input Submit) (*batchResumeFixture, context.Context) {
	t.Helper()
	base, _, ctx := newNativeTaskFixture(t)
	f := &batchResumeFixture{t: t, base: base, store: &resumeOperationStore{nativeTaskStore: base.store}, calls: make(map[string]int), input: input}
	first, err := f.service().submit(ctx, base.binding, f.input)
	if !errors.Is(err, agenvo.ErrUnknown) || first.State != "unknown" || first.ID == "" {
		t.Fatalf("original lost create receipt: state=%s err=%v", first.State, err)
	}
	f.op, f.progress = f.record(first.ID)
	if f.progress.Phase != "batch_creating_workspace" || f.progress.WorkspaceID != "" || f.progress.ControlPane != "" || f.progress.PaneID != "" || f.calls["workspace.create"] != 1 || base.batch.runs != 0 {
		t.Fatal("fixture did not retain exact pre-dispatch unknown workspace creation")
	}
	return f, ctx
}

func (f *batchResumeFixture) service() *Service {
	relay := agenvo.NewService(f.base.store, f.base, func(context.Context, string, string, string, map[string]any) (json.RawMessage, error) {
		f.t.Error("native resume reached old MCP/OAuth transport")
		return nil, agenvo.ErrUnavailable
	})
	relay.SetNativeCaller(f.invoke)
	return NewService(f.store, f.base.store, relay, f.base.assets, func(context.Context, toolcontext.ComputerBinding, string, string, string) error {
		if f.policyDenied.Load() {
			return ErrDenied
		}
		return nil
	})
}

func (f *batchResumeFixture) record(id string) (dbstore.ComputerOperationRecord, agenvo.BatchProgress) {
	f.t.Helper()
	f.base.store.mu.Lock()
	r := f.base.store.records[id]
	f.base.store.mu.Unlock()
	var d Details
	if json.Unmarshal(r.Details, &d) != nil || d.Batch == nil {
		f.t.Fatal("missing original durable batch")
	}
	return r, d.Batch.Progress
}

func (f *batchResumeFixture) restart() *Service {
	f.t.Helper()
	f.base.store.mu.Lock()
	raw, err := json.Marshal(f.base.store.records)
	f.base.store.mu.Unlock()
	if err != nil {
		f.t.Fatal(err)
	}
	reopened := &fixtureStore{mounts: f.base.store.mounts}
	if err := json.Unmarshal(raw, &reopened.records); err != nil {
		f.t.Fatal(err)
	}
	f.base.store = &nativeTaskStore{fixtureStore: reopened, t: f.t}
	f.base.batch.store = reopened
	f.store.nativeTaskStore = f.base.store
	return f.service()
}

func (f *batchResumeFixture) invoke(ctx context.Context, scope agenvo.Scope, target agenvo.Target, method string, params map[string]any) (agenvo.Receipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[method]++
	if method == "workspace.create" {
		f.label, _ = params["label"].(string)
		return agenvo.Receipt{}, agenvo.ErrUnknown // The workspace exists; only its reply was lost.
	}
	count := 1
	if f.tabCreated {
		count = 2
	}
	ws := map[string]any{"workspace_id": "w-test", "label": f.label, "active_tab_id": "control-tab", "tab_count": count, "pane_count": count}
	cwd := f.base.profile.WorkDir
	if f.mode == "wrong-cwd" {
		cwd = `C:\Different`
	}
	control := map[string]any{"pane_id": "control", "terminal_id": "control-terminal", "workspace_id": "w-test", "tab_id": "control-tab", "cwd": cwd}
	job := map[string]any{"pane_id": "job", "terminal_id": "job-terminal", "workspace_id": "w-test", "tab_id": "job-tab", "cwd": cwd}
	if f.mode == "replaced-control" && f.calls["pane.get"] > 1 {
		control["terminal_id"] = "replacement-terminal"
	}
	var body map[string]any
	switch method {
	case "workspace.list":
		workspaces := []any{ws}
		if f.mode == "ambiguous-label" {
			workspaces = append(workspaces, map[string]any{"workspace_id": "other-workspace", "label": f.label, "active_tab_id": "other-tab", "tab_count": 1, "pane_count": 1})
		}
		body = map[string]any{"type": "workspace_list", "workspaces": workspaces}
	case "workspace.get":
		if f.mode == "changed-topology" && f.calls[method] > 1 {
			ws["active_tab_id"] = "replacement-tab"
		}
		body = map[string]any{"type": "workspace_info", "workspace": ws}
	case "pane.list":
		panes := []any{control}
		if f.tabCreated {
			panes = append(panes, job)
		}
		body = map[string]any{"type": "pane_list", "panes": panes}
	case "pane.get":
		pane := control
		if params["paneId"] == "job" {
			pane = job
			if f.mode == "replaced-job" {
				pane["terminal_id"] = "replacement-job-terminal"
			}
		}
		body = map[string]any{"type": "pane_info", "pane": pane}
	case "agent.list":
		agents := []any{}
		if f.mode == "existing-agent" || f.mode == "agent-after-tab" && f.tabCreated {
			agents = append(agents, map[string]any{"agent": "already-owned", "workspace_id": "w-test", "pane_id": "control", "agent_status": "working"})
		}
		body = map[string]any{"type": "agent_list", "agents": agents}
	case "tab.create":
		_, progress := f.record(f.op.ID)
		if progress.Phase != "batch_creating_tab" || progress.ControlPane != "control" {
			f.t.Error("tab creation preceded durable admission")
		}
		f.tabCreated = true
		body = map[string]any{"type": "tab_created", "root_pane": job, "tab": map[string]any{"tab_id": "job-tab", "workspace_id": "w-test", "label": params["label"]}}
	case "pane.run":
		// Existing simulator checks the real generated Bash script against the
		// original persisted attempt/session and counts actual Pi submissions.
		r, err := f.base.invoke(ctx, scope, target, method, params)
		if f.lostMethod == method {
			return agenvo.Receipt{}, agenvo.ErrUnknown
		}
		return r, err
	default:
		f.t.Errorf("unexpected resume method %s", method)
		return agenvo.Receipt{}, agenvo.ErrUnsupported
	}
	if f.lostMethod == method {
		return agenvo.Receipt{}, agenvo.ErrUnknown
	}
	raw, _ := json.Marshal(map[string]any{"session": target.Session, "result": body})
	return agenvo.Receipt{Execution: "accepted", Result: raw}, nil
}

func (f *batchResumeFixture) resumeInput() Submit {
	in := f.input
	in.ResumeOperationID = f.op.ID
	return in
}

func (f *batchResumeFixture) assertIdentityAndCounts(t *testing.T, tabs, runs int) {
	t.Helper()
	r, p := f.record(f.op.ID)
	if r.ID != f.op.ID || r.RunID != f.op.RunID || r.TargetID != f.op.TargetID || r.Fingerprint != f.op.Fingerprint || r.TargetFingerprint != f.op.TargetFingerprint || p.Attempt != f.progress.Attempt || p.Generation != f.progress.Generation || p.RequestHash != f.progress.RequestHash || p.PiSessionID != f.progress.PiSessionID || len(f.base.store.records) != 1 {
		t.Error("resume replaced original operation/run/attempt/session identity")
	}
	if f.calls["workspace.create"] != 1 || f.calls["tab.create"] != tabs || f.base.batch.runs != runs {
		t.Errorf("side effect counts: original workspace=%d, resumed tabs=%d Pi=%d; want 1/%d/%d", f.calls["workspace.create"], f.calls["tab.create"], f.base.batch.runs, tabs, runs)
	}
}

func TestNativeBatchExplicitResumeKeepsOriginalIdentityAfterRestart(t *testing.T) {
	f, _ := newBatchResumeFixture(t)
	later := f.base.binding
	later.RunID = "explicit-recovery-run"
	ctx, err := toolcontext.WithComputerBinding(t.Context(), later)
	if err != nil {
		t.Fatal(err)
	}
	// Reopen serialized ledger rows; retain no Service or active-turn state.
	service := f.restart()
	before := len(f.calls)
	if got, err := service.submit(ctx, later, f.input); err != nil || got.ID != f.op.ID || got.State != "unknown" || len(f.calls) != before {
		t.Fatal("ordinary duplicate resumed a task without explicit operation ID", err)
	}
	got, err := service.submit(ctx, later, f.resumeInput())
	if err != nil || got.ID != f.op.ID || got.State != "submitted" || got.PiResult != nil || got.ProcessStopped {
		t.Fatalf("explicit resume manufactured completion or failed: state=%s err=%v", got.State, err)
	}
	f.assertIdentityAndCounts(t, 1, 1)
	if len(f.store.admitted) != 2 || f.store.admitted[0] != "batch_creating_tab" || f.store.admitted[1] != "batch_submitting" {
		t.Fatal("resume skipped admission at a mutation boundary", f.store.admitted)
	}
	_, _ = f.service().submit(ctx, later, f.resumeInput())
	f.assertIdentityAndCounts(t, 1, 1)
}

func TestNativeBatchExplicitResumeConcurrentCallersReserveOnce(t *testing.T) {
	f, bound := newBatchResumeFixture(t)
	f.store.arrived, f.store.release = make(chan int64, 2), make(chan struct{})
	ctx, cancel := context.WithTimeout(bound, 5*time.Second)
	var wg sync.WaitGroup
	var release sync.Once
	defer func() { cancel(); release.Do(func() { close(f.store.release) }); wg.Wait() }()
	type outcome struct {
		result Result
		err    error
	}
	results := make(chan outcome, 2)
	for range 2 {
		service := f.service()
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := service.submit(ctx, f.base.binding, f.resumeInput())
			results <- outcome{got, err}
		}()
	}
	for range 2 {
		select {
		case revision := <-f.store.arrived:
			if revision != f.op.Revision {
				t.Fatal("callers did not load the same original revision")
			}
		case <-ctx.Done():
			t.Fatal("concurrent resume did not reach initial read barrier")
		}
	}
	release.Do(func() { close(f.store.release) })
	wg.Wait()
	close(results)
	accepted := false
	for got := range results {
		if got.result.ID != f.op.ID {
			t.Error("concurrent resume changed operation ID")
		}
		accepted = accepted || got.err == nil && got.result.State == "submitted"
	}
	if !accepted {
		t.Error("neither concurrent caller completed the one permitted submission")
	}
	f.assertIdentityAndCounts(t, 1, 1)
}

func TestNativeBatchExplicitResumeRejectsChangedIdentityAndRemoteEvidence(t *testing.T) {
	for _, mode := range []string{"task", "target", "owner", "profile", "ambiguous-label", "wrong-cwd", "changed-topology", "replaced-control", "existing-agent"} {
		t.Run(mode, func(t *testing.T) {
			f, ctx := newBatchResumeFixture(t)
			in, binding := f.resumeInput(), f.base.binding
			switch mode {
			case "task":
				in.Task += " changed"
			case "target":
				in.TargetID = uuid.NewString()
			case "owner":
				binding.UserID = uuid.NewString()
				ctx, _ = toolcontext.WithComputerBinding(t.Context(), binding)
			case "profile":
				mount := f.base.store.mounts[binding.TargetID]
				profile := f.base.profile
				profile.HerdrConfigRoot = `C:\Changed\herdr`
				mount.RuntimeProviderConfig, _ = json.Marshal(map[string]any{"herdr": profile})
				f.base.store.mounts[binding.TargetID] = mount
			default:
				f.mode = mode
			}
			if _, err := f.service().submit(ctx, binding, in); err == nil {
				t.Error("resume accepted changed scope or ambiguous evidence")
			}
			f.assertIdentityAndCounts(t, 0, 0)
		})
	}
}

func TestNativeBatchExplicitResumeStopsAtUncertainMutationAndSave(t *testing.T) {
	for _, tc := range []struct {
		name, phase, lost string
		after             bool
		tabs, runs        int
		blocksRetry       bool
	}{
		{name: "workspace-save-before", phase: "batch_workspace_adopted"},
		{name: "workspace-save-after", phase: "batch_workspace_adopted", after: true},
		{name: "tab-admission-before", phase: "batch_creating_tab"},
		{name: "tab-admission-after", phase: "batch_creating_tab", after: true, blocksRetry: true},
		{name: "tab-reply-lost", lost: "tab.create", tabs: 1, blocksRetry: true},
		{name: "tab-save-before", phase: "batch_tab_adopted", tabs: 1, blocksRetry: true},
		{name: "tab-save-after", phase: "batch_tab_adopted", after: true, tabs: 1},
		{name: "submit-admission-before", phase: "batch_submitting", tabs: 1},
		{name: "submit-admission-after", phase: "batch_submitting", after: true, tabs: 1, blocksRetry: true},
		{name: "run-reply-lost", lost: "pane.run", tabs: 1, runs: 1, blocksRetry: true},
		{name: "submitted-save-before", phase: "batch_submitted", tabs: 1, runs: 1, blocksRetry: true},
		{name: "submitted-save-after", phase: "batch_submitted", after: true, tabs: 1, runs: 1, blocksRetry: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, ctx := newBatchResumeFixture(t)
			f.store.faultPhase, f.store.commitBeforeError, f.lostMethod = tc.phase, tc.after, tc.lost
			if _, err := f.service().submit(ctx, f.base.binding, f.resumeInput()); err == nil {
				t.Error("uncertain resume did not stop")
			}
			f.assertIdentityAndCounts(t, tc.tabs, tc.runs)
			if tc.blocksRetry {
				f.store.faultPhase, f.lostMethod = "", ""
				_, _ = f.service().submit(ctx, f.base.binding, f.resumeInput())
				f.assertIdentityAndCounts(t, tc.tabs, tc.runs)
			}
		})
	}
}

func TestNativeBatchExplicitResumeAdoptedCheckpointsSurviveRestart(t *testing.T) {
	for _, phase := range []string{"batch_workspace_adopted", "batch_tab_adopted"} {
		t.Run(phase, func(t *testing.T) {
			f, ctx := newBatchResumeFixture(t)
			f.store.faultPhase, f.store.commitBeforeError = phase, true
			if _, err := f.service().submit(ctx, f.base.binding, f.resumeInput()); err == nil {
				t.Fatal("lost checkpoint acknowledgement was ignored")
			}
			f.store.faultPhase = ""
			got, err := f.restart().submit(ctx, f.base.binding, f.resumeInput())
			if err != nil || got.State != "submitted" {
				t.Fatal("durable adopted checkpoint could not continue", got.State, err)
			}
			f.assertIdentityAndCounts(t, 1, 1)
		})
	}
}

func TestNativeBatchExplicitResumeHonorsAdmission(t *testing.T) {
	f, ctx := newBatchResumeFixture(t)
	f.store.paused = true
	if _, err := f.service().submit(ctx, f.base.binding, f.resumeInput()); !errors.Is(err, db.ErrComputerAdmissionPaused) {
		t.Error("paused admission allowed new native work", err)
	}
	f.assertIdentityAndCounts(t, 0, 0)
}

func TestNativeBatchExplicitResumeDoesNotApplyToLegacyAgenvo(t *testing.T) {
	source, store, transport, _ := setupBatch(t)
	source.service.relay = agenvo.NewService(store, permissionChecker{}, func(ctx context.Context, owner, connection, tool string, args map[string]any) (json.RawMessage, error) {
		raw, err := transport.call(ctx, owner, connection, tool, args)
		if len(transport.calls) == 1 && transport.calls[0] == "workspace.create" {
			return nil, agenvo.ErrUnknown
		}
		return raw, err
	})
	binding := toolcontext.ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "thread-a", TargetID: "pc-a", RunID: "legacy-original-run"}
	ctx, err := toolcontext.WithComputerBinding(t.Context(), binding)
	if err != nil {
		t.Fatal(err)
	}
	in := Submit{Kind: "herdr_pi_task", Task: "legacy synthetic instruction"}
	first, err := source.service.submit(ctx, binding, in)
	if !errors.Is(err, agenvo.ErrUnknown) || first.State != "unknown" {
		t.Fatal("legacy unknown fixture", err)
	}
	before := store.records[first.ID]
	in.ResumeOperationID = first.ID
	if _, err := source.service.submit(ctx, binding, in); err == nil {
		t.Error("legacy Agenvo entered native-only continuation")
	}
	after := store.records[first.ID]
	if len(transport.calls) != 1 || transport.runs != 0 || before.Revision != after.Revision || string(before.Details) != string(after.Details) {
		t.Error("native resume modified or replayed legacy Agenvo operation")
	}
}

func TestNativeBatchExplicitResumeRechecksNewTabBeforeInput(t *testing.T) {
	for _, mode := range []string{"replaced-job", "agent-after-tab"} {
		t.Run(mode, func(t *testing.T) {
			f, ctx := newBatchResumeFixture(t)
			f.mode = mode
			if _, err := f.service().submit(ctx, f.base.binding, f.resumeInput()); err == nil {
				t.Error("resume sent input after new tab ownership changed")
			}
			f.assertIdentityAndCounts(t, 1, 0)
			_, _ = f.restart().submit(ctx, f.base.binding, f.resumeInput())
			f.assertIdentityAndCounts(t, 1, 0)
		})
	}
}

func TestNativeBatchExplicitResumeRechecksAuthorityBeforeEachInput(t *testing.T) {
	for _, phase := range []string{"batch_workspace_adopted", "batch_tab_adopted"} {
		for _, denial := range []string{"permission-revoked", "policy-denied", "context-canceled"} {
			t.Run(phase+"/"+denial, func(t *testing.T) {
				f, bound := newBatchResumeFixture(t)
				ctx, cancel := context.WithCancel(bound)
				defer cancel()
				f.store.onSaved = func(saved string) {
					if saved != phase {
						return
					}
					switch denial {
					case "permission-revoked":
						f.base.denied = true
					case "policy-denied":
						f.policyDenied.Store(true)
					case "context-canceled":
						cancel()
					}
				}
				if _, err := f.service().submit(ctx, f.base.binding, f.resumeInput()); err == nil {
					t.Error("resume continued after current authority was lost")
				}
				tabs := 0
				if phase == "batch_tab_adopted" {
					tabs = 1
				}
				f.assertIdentityAndCounts(t, tabs, 0)
				if len(f.store.admitted) != tabs {
					t.Error("denied continuation reserved a new native input", f.store.admitted)
				}
			})
		}
	}
}

func resumeJSONMap(t *testing.T, raw []byte) map[string]json.RawMessage {
	t.Helper()
	var value map[string]json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		t.Fatal("invalid test JSON object", err)
	}
	return value
}

func TestNativeBatchExplicitResumePreservesHistoricalProducerAndDeliveryFields(t *testing.T) {
	f, ctx := newBatchResumeFixtureWithInput(t, Submit{Kind: "herdr_pi_task", Task: "original producer request", Artifact: &deliverymeta.Artifact{SourceRef: "result", Mime: "text/plain"}})
	r, _ := f.record(f.op.ID)
	details := resumeJSONMap(t, r.Details)
	batch := resumeJSONMap(t, details["batch"])
	progress := resumeJSONMap(t, batch["progress"])
	history := resumeJSONMap(t, progress["history"])
	producer := resumeJSONMap(t, progress["producer"])
	opaque := json.RawMessage(`{"ordinal":9007199254740993,"text":"retained 中文","future":[true,null,{"v":3}]}`)
	details["future_top"], batch["future_batch"], progress["future_progress"] = opaque, opaque, opaque
	history["future_history"], producer["future_producer"] = opaque, opaque
	progress["history"], _ = json.Marshal(history)
	progress["producer"], _ = json.Marshal(producer)
	batch["progress"], _ = json.Marshal(progress)
	details["batch"], _ = json.Marshal(batch)
	details["delivery_ledger"] = json.RawMessage(`{"version":5,"storage":"rows","future_delivery":9007199254740993}`)
	r.Details, _ = json.Marshal(details)
	r.DeliveryEntries = map[string]dbstore.ComputerDeliveryEntry{"retained_other_delivery": {DeliveryID: "retained_other_delivery", Version: 1, Ordinal: 7, Data: dbstore.JSON(opaque)}}
	f.base.store.records[r.ID] = r
	unchanged := func(raw []byte) string {
		d := resumeJSONMap(t, raw)
		b := resumeJSONMap(t, d["batch"])
		p := resumeJSONMap(t, b["progress"])
		delete(d, "phase")
		for _, key := range []string{"phase", "workspace_id", "control_pane", "control_terminal", "pane_id", "terminal_id", "request_id"} {
			delete(p, key)
		}
		b["progress"], _ = json.Marshal(p)
		d["batch"], _ = json.Marshal(b)
		out, _ := json.Marshal(d)
		return string(out)
	}
	beforeEntries, _ := json.Marshal(r.DeliveryEntries)
	got, err := f.restart().submit(ctx, f.base.binding, f.resumeInput())
	if err != nil || got.State != "submitted" {
		t.Fatal("resume with retained producer evidence failed", got.State, err)
	}
	after, _ := f.record(r.ID)
	afterEntries, _ := json.Marshal(after.DeliveryEntries)
	if unchanged(r.Details) != unchanged(after.Details) || string(beforeEntries) != string(afterEntries) {
		t.Error("resume rewrote historical fields, producer metadata or retained delivery rows")
	}
	f.assertIdentityAndCounts(t, 1, 1)
}

func TestNativeBatchExplicitResumePreservesLegacySubmitFingerprint(t *testing.T) {
	for _, tc := range []struct {
		name          string
		artifact      *deliverymeta.Artifact
		artifacts     []deliverymeta.Artifact
		encoded, hash string
	}{
		{name: "plain", encoded: `{"target_id":"pc-original","kind":"herdr_pi_task","task":"original 中文","work_dir":"."}`, hash: "e312e83235b25165c4d946246eb428f72ae01c734010ad10d379575fe80fd1fe"},
		{name: "single-artifact", artifact: &deliverymeta.Artifact{SourceRef: "result", Mime: "text/plain"}, encoded: `{"artifact":{"source_ref":"result","mime":"text/plain"},"target_id":"pc-original","kind":"herdr_pi_task","task":"original 中文","work_dir":"."}`, hash: "65d0d0071b1fa4f0000f10ac1ec9a0f5cf1f57c5f8ac12fb81bc02f4a9756a52"},
		{name: "multiple-artifacts", artifacts: []deliverymeta.Artifact{{SourceRef: "result", Mime: "text/plain"}, {SourceRef: "report", Mime: "application/pdf"}}, encoded: `{"artifacts":[{"source_ref":"result","mime":"text/plain"},{"source_ref":"report","mime":"application/pdf"}],"target_id":"pc-original","kind":"herdr_pi_task","task":"original 中文","work_dir":"."}`, hash: "9e589aa677f132b08733a61923bc102a63bb6a226d5401bb30d8e4eb7e957a8e"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := Submit{TargetID: "pc-original", Kind: "herdr_pi_task", Task: "original 中文", WorkDir: ".", Artifact: tc.artifact, Artifacts: tc.artifacts}
			raw, err := json.Marshal(in)
			if err != nil || string(raw) != tc.encoded || fingerprint(in) != tc.hash {
				t.Fatal("omitted resume_operation_id changed legacy JSON or SHA256", err)
			}
			in.ResumeOperationID = "11111111-1111-4111-8111-111111111111"
			if fingerprint(in) == tc.hash {
				t.Fatal("test did not add the explicit resume selector")
			}
			in.ResumeOperationID = ""
			if fingerprint(in) != tc.hash {
				t.Fatal("cleared resume selector did not restore original request fingerprint")
			}
		})
	}
}
