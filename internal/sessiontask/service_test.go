package sessiontask

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	thread "github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/workdir"
)

type fixture struct {
	sessions                    map[string]thread.Thread
	workdirs                    map[string]workdir.Workdir
	targets                     map[string]agenvo.Target
	operations                  map[string]dbstore.ComputerOperationRecord
	rows                        []dbstore.SessionTaskCandidate
	service                     *Service
	root, child, grandchild, op string
	primary                     string
}

func (f *fixture) GetPrimaryMount(context.Context, string) (dbstore.BotRemoteRuntimeBindingRecord, error) {
	if f.primary == "" {
		return dbstore.BotRemoteRuntimeBindingRecord{}, db.ErrNotFound
	}
	return dbstore.BotRemoteRuntimeBindingRecord{ID: f.primary, BotID: "bot"}, nil
}

func (f *fixture) Get(_ context.Context, id string) (thread.Thread, error) {
	v, ok := f.sessions[id]
	if !ok {
		return v, ErrUnavailable
	}
	return v, nil
}

type dirs struct{ f *fixture }

func (d dirs) Get(_ context.Context, bot, id string) (workdir.Workdir, error) {
	v, ok := d.f.workdirs[id]
	if !ok || v.BotID != bot {
		return v, ErrUnavailable
	}
	return v, nil
}

func (f *fixture) ResolveTarget(_ context.Context, s agenvo.Scope) (agenvo.Target, error) {
	v, ok := f.targets[s.TargetID]
	if !ok || s.BotID != "bot" || s.UserID != "user" {
		return v, ErrUnavailable
	}
	return v, nil
}

func (f *fixture) ReadComputerOperation(_ context.Context, r dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error) {
	v, ok := f.operations[r.ID]
	if !ok || v.BotID != r.BotID || v.UserID != r.UserID || v.SessionID != r.SessionID {
		return v, ErrUnavailable
	}
	return v, nil
}

func (f *fixture) ListSessionTaskCandidates(_ context.Context, q dbstore.SessionTaskFilter) ([]dbstore.SessionTaskCandidate, error) {
	rows := append([]dbstore.SessionTaskCandidate{}, f.rows...)
	less := func(a, b dbstore.SessionTaskCandidate) bool {
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		return a.ID < b.ID
	}
	sort.Slice(rows, func(i, j int) bool { return less(rows[j], rows[i]) })
	var out []dbstore.SessionTaskCandidate
	for _, r := range rows {
		if r.CreatedAt.After(q.SnapshotAt) || (q.ID != "" && (q.ID != r.ID || q.Type != r.Type)) || (q.Before.ID != "" && !less(r, q.Before)) {
			continue
		}
		out = append(out, r)
		if len(out) == int(q.Limit) {
			break
		}
	}
	return out, nil
}

func newFixture() *fixture {
	f := &fixture{sessions: map[string]thread.Thread{}, workdirs: map[string]workdir.Workdir{}, targets: map[string]agenvo.Target{"pc-a": {DeviceID: "synthetic-a"}, "pc-b": {DeviceID: "synthetic-b"}}, operations: map[string]dbstore.ComputerOperationRecord{}}
	f.root, f.child, f.grandchild = uuid.NewString(), uuid.NewString(), uuid.NewString()
	now := time.Now().Add(-time.Minute).UTC()
	for i, id := range []string{f.root, f.child, f.grandchild} {
		v := thread.Thread{ID: id, BotID: "bot", CreatedByUserID: "user", Type: Subagent, CreatedAt: now, UpdatedAt: now, Metadata: map[string]any{"reasoning": "SECRET"}, Title: "SECRET"}
		switch i {
		case 0:
			v.Type = "chat"
		case 1:
			v.ParentThreadID = f.root
		default:
			v.ParentThreadID = f.child
		}
		f.sessions[id] = v
		if i > 0 {
			f.rows = append(f.rows, dbstore.SessionTaskCandidate{Type: Subagent, ID: id, SessionID: id, CreatedAt: now})
		}
	}
	f.op = f.addOperation(f.grandchild, "pc-a", now)
	f.service = NewService(f, f, dirs{f}, f, "synthetic-cursor-secret")
	return f
}

func (f *fixture) addOperation(session, target string, at time.Time) string {
	id := uuid.NewString()
	f.operations[id] = dbstore.ComputerOperationRecord{ID: id, BotID: "bot", UserID: "user", SessionID: session, TargetID: target, TargetFingerprint: agenvo.TargetFingerprint(f.targets[target]), State: "dispatching", CreatedAt: at, UpdatedAt: at, Details: []byte(`{"kind":"herdr_pi_task","output":"SECRET","artifact_hash":"SECRET","observation":{"reasoning":"SECRET"},"pi_result":"succeeded"}`)}
	f.rows = append(f.rows, dbstore.SessionTaskCandidate{Type: ComputerOperation, ID: id, SessionID: session, CreatedAt: at})
	return id
}

func TestParentAggregatesOwnedDescendantsWithoutChangingOwnership(t *testing.T) {
	f := newFixture()
	direct := f.addOperation(f.root, "pc-b", time.Now().Add(-time.Second))
	p, err := f.service.List(t.Context(), "bot", "user", f.root, 100, "")
	if err != nil || len(p.Items) != 4 {
		t.Fatalf("page=%+v err=%v", p, err)
	}
	v, err := f.service.Get(t.Context(), "bot", "user", f.root, ComputerOperation, f.op)
	if err != nil || v.SessionID != f.grandchild || v.ParentSessionID != f.root {
		t.Fatalf("detail=%+v err=%v", v, err)
	}
	if _, err = f.service.Get(t.Context(), "bot", "user", f.child, ComputerOperation, direct); !errors.Is(err, ErrUnavailable) {
		t.Fatal("child read parent receipt")
	}
}

func TestScopeAndRevocation(t *testing.T) {
	for _, scenario := range []string{"user", "bot", "parent", "child_user", "child_bot", "child_deleted", "foreign_session", "pc_mismatch", "pc_grant_revoked", "root_grant_revoked", "registration_changed", "child_broadens", "cycle", "receipt_user", "receipt_bot"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture()
			bot, user, parent := "bot", "user", f.root
			r := f.operations[f.op]
			child := f.sessions[f.child]
			root := f.sessions[f.root]
			switch scenario {
			case "user":
				user = "other"
			case "bot":
				bot = "other"
			case "parent":
				parent = uuid.NewString()
			case "child_user":
				child.CreatedByUserID = "other"
			case "child_bot":
				child.BotID = "other"
			case "child_deleted":
				delete(f.sessions, f.grandchild)
			case "foreign_session":
				child.ParentThreadID = ""
			case "pc_mismatch":
				root.WorkdirID = "w"
				f.workdirs["w"] = workdir.Workdir{ID: "w", BotID: "bot", WorkspaceTargetID: "pc-b"}
			case "pc_grant_revoked":
				delete(f.targets, "pc-a")
			case "root_grant_revoked":
				root.WorkdirID = "w"
				f.workdirs["w"] = workdir.Workdir{ID: "w", BotID: "bot", WorkspaceTargetID: "pc-b"}
				delete(f.targets, "pc-b")
			case "registration_changed":
				f.targets["pc-a"] = agenvo.Target{DeviceID: "changed"}
			case "child_broadens":
				root.WorkdirID = "w"
				child.WorkdirID = "native"
				f.workdirs["w"] = workdir.Workdir{ID: "w", BotID: "bot", WorkspaceTargetID: "pc-a"}
				f.workdirs["native"] = workdir.Workdir{ID: "native", BotID: "bot", WorkspaceTargetID: "native"}
			case "cycle":
				child.ParentThreadID = f.grandchild
			case "receipt_user":
				r.UserID = "other"
			case "receipt_bot":
				r.BotID = "other"
			}
			f.operations[f.op] = r
			f.sessions[f.child] = child
			f.sessions[f.root] = root
			if _, err := f.service.Get(t.Context(), bot, user, parent, ComputerOperation, f.op); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("detail allowed: %v", err)
			}
			p, err := f.service.List(t.Context(), bot, user, parent, 100, "")
			if err != nil && !errors.Is(err, ErrUnavailable) {
				t.Fatal(err)
			}
			for _, v := range p.Items {
				if v.TaskID == f.op {
					t.Fatal("revoked item in list")
				}
			}
		})
	}
}

func TestSelectedPCInheritedThroughChildren(t *testing.T) {
	f := newFixture()
	root := f.sessions[f.root]
	root.WorkdirID = "w"
	f.sessions[f.root] = root
	f.workdirs["w"] = workdir.Workdir{ID: "w", BotID: "bot", WorkspaceTargetID: "pc-a"}
	other := f.addOperation(f.child, "pc-b", time.Now().Add(-time.Second))
	if _, err := f.service.Get(t.Context(), "bot", "user", f.child, ComputerOperation, f.op); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Get(t.Context(), "bot", "user", f.child, ComputerOperation, other); !errors.Is(err, ErrUnavailable) {
		t.Fatal("child root escaped PC scope")
	}
}

func TestUnboundSelectionAndPrimaryNeverBecomeImplicitNAS(t *testing.T) {
	for _, mode := range []string{"id", "snapshot", "primary", "malformed", "conflicting", "workdir_precedence"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture()
			root := f.sessions[f.root]
			switch mode {
			case "id":
				root.Metadata["workspace_target_id"] = "pc-b"
			case "snapshot":
				root.Metadata["workspace_target"] = map[string]any{"target_id": "pc-b"}
			case "primary":
				f.primary = "pc-b"
			case "malformed":
				root.Metadata["workspace_target_id"] = 42
			case "conflicting":
				root.Metadata["workspace_target_id"] = "native"
				root.Metadata["workspace_target"] = map[string]any{"target_id": "pc-b"}
			case "workdir_precedence":
				root.WorkdirID = "w"
				root.Metadata["workspace_target_id"] = "native"
				f.workdirs["w"] = workdir.Workdir{ID: "w", BotID: "bot", WorkspaceTargetID: "pc-b"}
			}
			f.sessions[f.root] = root
			if _, err := f.service.Get(t.Context(), "bot", "user", f.root, ComputerOperation, f.op); !errors.Is(err, ErrUnavailable) {
				t.Fatal("broadened unbound scope", err)
			}
		})
	}
	f := newFixture()
	f.primary = "pc-b"
	root := f.sessions[f.root]
	root.Metadata["workspace_target_id"] = "native"
	f.sessions[f.root] = root
	if _, err := f.service.Get(t.Context(), "bot", "user", f.root, ComputerOperation, f.op); err != nil {
		t.Fatal("explicit NAS selection should use current Bot grants", err)
	}
}

func TestProjectionNeverLeaksAndStatesStayHonest(t *testing.T) {
	for _, state := range []string{"dispatching", "unknown", "submitted", "completed", "rejected", "not_started", "SECRET"} {
		f := newFixture()
		r := f.operations[f.op]
		r.State = state
		r.CancelRequested = true
		r.Revision = 9007199254740993
		f.operations[f.op] = r
		p, err := f.service.List(t.Context(), "bot", "user", f.root, 100, "")
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(p)
		for _, forbidden := range []string{"SECRET", "reasoning", "artifact_hash", "output", "http", "succeeded", "queued", "running"} {
			if strings.Contains(string(raw), forbidden) {
				t.Fatalf("leaked %s: %s", forbidden, raw)
			}
		}
		v, err := f.service.Get(t.Context(), "bot", "user", f.root, ComputerOperation, f.op)
		if err != nil {
			t.Fatal(err)
		}
		if !*v.CancelRequested || v.BusinessState != "unknown" || v.PiResult != "unknown" || v.ContentAvailable || v.AttachmentsAvailable || v.Revision != "9007199254740993" {
			t.Fatalf("dishonest projection: %+v", v)
		}
		if (state == "dispatching" || state == "completed" || state == "SECRET") && (v.Status != "unknown" || v.ProcessState != "unknown") {
			t.Fatal(v)
		}
	}
	f := newFixture()
	r := f.operations[f.op]
	r.State = "completed"
	r.Details = []byte(`{"kind":"foreground_bash","exit_code":0,"output":"SECRET"}`)
	f.operations[f.op] = r
	v, err := f.service.Get(t.Context(), "bot", "user", f.root, ComputerOperation, f.op)
	if err != nil || v.Status != "process_exited" || v.ProcessState != "exited" || v.BusinessState != "unknown" || v.PiResult != "not_applicable" {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestPiBatchReceiptsRemainOutsideFrozenSummaryContract(t *testing.T) {
	f := newFixture()
	r := f.operations[f.op]
	r.State = "completed"
	r.Revision = 7
	r.CancelRequested = true
	// PR #7 persists exit evidence, output refs and a parsed Pi result. None
	// of these extend this API's approved scalar projection implicitly.
	r.Details = []byte(`{"kind":"herdr_pi_task","exit_code":0,"phase":"pi_succeeded","output":"SECRET",
		"batch":{"progress":{"request_id":"SECRET","pi_session_id":"SECRET"},
		"receipt":{"exited":true,"started":true,"exit_code":0,"stdout":{"name":"stdout.jsonl","sha256":"SECRET","size":42}},
		"files":[{"ready_hash":"SECRET","chunks":[{"hash":"SECRET","size":42}]}],
		"result":{"outcome":"succeeded","output":"SECRET","reasoning":"SECRET"}}}`)
	f.operations[f.op] = r
	page, err := f.service.List(t.Context(), "bot", "user", f.root, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := f.service.Get(t.Context(), "bot", "user", f.root, ComputerOperation, f.op)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{page, detail} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"SECRET", "succeeded", "batch", "exit_code", "reasoning", "ready_hash", "stdout", "chunks"} {
			if strings.Contains(string(raw), forbidden) {
				t.Fatalf("Pi batch detail leaked %q", forbidden)
			}
		}
	}
	if detail.Status != "unknown" || detail.ProcessState != "unknown" || detail.PiResult != "unknown" || detail.BusinessState != "unknown" || detail.ContentAvailable || detail.AttachmentsAvailable || !*detail.CancelRequested {
		t.Fatalf("frozen summary contract changed: %+v", detail)
	}
	after := f.operations[f.op]
	if after.State != r.State || after.Revision != r.Revision || after.CancelRequested != r.CancelRequested || string(after.Details) != string(r.Details) {
		t.Fatal("summary read modified a Pi batch receipt")
	}
}

func TestCursorStableAcrossLateStateChangesAndRevocation(t *testing.T) {
	f := newFixture()
	first, err := f.service.List(t.Context(), "bot", "user", f.root, 1, "")
	if err != nil || first.NextCursor == "" {
		t.Fatal(first, err)
	}
	seen := map[string]bool{first.Items[0].TaskID: true}
	late := f.addOperation(f.root, "pc-b", time.Now().Add(time.Second))
	r := f.operations[f.op]
	r.State = "submitted"
	r.UpdatedAt = time.Now()
	f.operations[f.op] = r
	cur := first.NextCursor
	for cur != "" {
		p, e := f.service.List(t.Context(), "bot", "user", f.root, 1, cur)
		if e != nil {
			t.Fatal(e)
		}
		for _, v := range p.Items {
			if seen[v.TaskID] || v.TaskID == late {
				t.Fatal("duplicate or late insert")
			}
			seen[v.TaskID] = true
			if v.TaskID == f.op && v.Status != "submitted" {
				t.Fatal("status was frozen")
			}
		}
		cur = p.NextCursor
	}
	if len(seen) != 3 {
		t.Fatal(seen)
	}
	for _, raw := range []string{"invalid", first.NextCursor + "!", strings.Repeat("x", 2049)} {
		if _, err = f.service.List(t.Context(), "bot", "user", f.root, 1, raw); !errors.Is(err, ErrCursor) {
			t.Fatal(err)
		}
	}
	if _, err = f.service.List(t.Context(), "bot", "user", f.child, 1, first.NextCursor); !errors.Is(err, ErrCursor) {
		t.Fatal("cross-session cursor accepted")
	}
	delete(f.targets, "pc-a")
	p, err := f.service.List(t.Context(), "bot", "user", f.root, 100, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range p.Items {
		if v.TaskID == f.op {
			t.Fatal("cursor retained revoked grant")
		}
	}
}

func TestBoundedFilteredPagesStillAdvance(t *testing.T) {
	f := newFixture()
	f.rows = nil
	for i := 0; i < scanBudget+2; i++ {
		f.addOperation(f.root, "pc-a", time.Now().Add(-time.Hour-time.Duration(i)*time.Second))
	}
	delete(f.targets, "pc-a")
	p, err := f.service.List(t.Context(), "bot", "user", f.root, 20, "")
	if err != nil || len(p.Items) != 0 || p.NextCursor == "" {
		t.Fatal(p, err)
	}
	p, err = f.service.List(t.Context(), "bot", "user", f.root, 20, p.NextCursor)
	if err != nil || len(p.Items) != 0 || p.NextCursor != "" {
		t.Fatal(p, err)
	}
}
