package sessiontask

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/agenvo"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/toolcontext"
	"github.com/felinics/memoh/internal/workdir"
)

type oldRegistration struct {
	row                         dbstore.BotRemoteRuntimeBindingRecord
	denied                      bool
	mountReads, credentialReads int
}

func TestRetiredComputerOperationHistorySurvivesDeletedBinding(t *testing.T) {
	for _, snapshot := range []string{"legacy", "null", "snapshot"} {
		for _, selected := range []string{"unbound", "metadata", "workdir"} {
			t.Run(selected+"/"+snapshot, func(t *testing.T) {
				f := newFixture()
				r := f.operations[f.op]
				r.State, r.Revision, r.RunID = "unknown", 9, "original-run"
				switch snapshot {
				case "snapshot":
					r.Details, _ = json.Marshal(map[string]any{"kind": "herdr_pi_task", "target_snapshot": f.targets[r.TargetID], "private_history": "SECRET"})
				case "null":
					r.Details = []byte(`{"kind":"herdr_pi_task","target_snapshot":null,"private_history":"SECRET"}`)
				}
				f.operations[f.op] = r
				other := f.addOperation(f.grandchild, "pc-b", time.Now().Add(-time.Minute))
				root := f.sessions[f.root]
				switch selected {
				case "metadata":
					root.Metadata["workspace_target_id"] = r.TargetID
				case "workdir":
					root.WorkdirID = "retained-workdir"
					f.workdirs[root.WorkdirID] = workdir.Workdir{ID: root.WorkdirID, BotID: "bot", WorkspaceTargetID: r.TargetID}
				}
				f.sessions[f.root] = root
				before, _ := json.Marshal(f.operations)
				delete(f.targets, r.TargetID)
				got, err := f.service.Get(t.Context(), "bot", "user", f.root, ComputerOperation, f.op)
				if err != nil || got.TaskID != r.ID || got.Status != "unknown" || got.ProcessState != "unknown" || got.BusinessState != "unknown" || got.Revision != "9" || got.ContentAvailable || got.AttachmentsAvailable {
					t.Fatalf("deleted binding hid or changed original history: %+v %v", got, err)
				}
				page, err := f.service.List(t.Context(), "bot", "user", f.root, 100, "")
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, item := range page.Items {
					found = found || item.TaskID == f.op
					if selected != "unbound" && item.TaskID == other {
						t.Error("deleted binding broadened selected PC history")
					}
				}
				if !found {
					t.Error("list lost original operation after binding deletion")
				}
				after, _ := json.Marshal(f.operations)
				if string(before) != string(after) {
					t.Error("historical projection wrote original operation")
				}
			})
		}
	}
}

func (s *oldRegistration) GetMount(_ context.Context, bot, id string) (dbstore.BotRemoteRuntimeBindingRecord, error) {
	s.mountReads++
	if s.denied || s.row.BotID != bot || s.row.ID != id {
		return dbstore.BotRemoteRuntimeBindingRecord{}, errors.New("revoked")
	}
	return s.row, nil
}

func (s *oldRegistration) OwnedHTTPMCPConnectionBot(_ context.Context, owner, connection string) (string, error) {
	s.credentialReads++
	if s.denied || owner != "owner" || connection != "11111111-1111-4111-8111-111111111111" {
		return "", errors.New("revoked")
	}
	return "owner-bot", nil
}

type oldPermission struct{ denied bool }

func (p *oldPermission) HasBotPermission(_ context.Context, bot, user, permission string) (bool, error) {
	return !p.denied && bot == "bot" && user == "user" && permission == "workspace_exec", nil
}

func TestOldIdentityTaskProjectionRequiresCurrentAuthorization(t *testing.T) {
	f := newFixture()
	target := agenvo.Target{ConnectionID: "11111111-1111-4111-8111-111111111111", DeviceID: "old-device", InstanceID: "old-instance", Session: "synthetic", WorkDir: `C:\Synthetic`, BashPath: `C:\Synthetic\Git\bin\bash.exe`}
	raw, _ := json.Marshal(target)
	old := &oldRegistration{row: dbstore.BotRemoteRuntimeBindingRecord{ID: "pc-a", BotID: "bot", RuntimeID: "old-runtime", RuntimeUserID: "owner", BotOwnerUserID: "owner", RuntimeProvider: agenvo.Provider, RuntimeProviderConfig: raw}}
	row := f.operations[f.op]
	row.TargetFingerprint = agenvo.TargetFingerprint(target)
	row.State = "unknown"
	f.operations[f.op] = row
	permission := &oldPermission{}
	f.service = NewService(f, f, dirs{f}, agenvo.NewService(old, permission, nil), "synthetic-cursor-secret")
	task, err := f.service.Get(t.Context(), "bot", "user", f.root, ComputerOperation, f.op)
	if err != nil || task.Status != "unknown" {
		t.Fatalf("old task: %+v %v", task, err)
	}
	old.denied = true
	if got, err := f.service.Get(t.Context(), "bot", "user", f.root, ComputerOperation, f.op); err != nil || got.Status != "unknown" {
		t.Fatal("retired registration hid retained history", err)
	}
	if old.mountReads != 0 || old.credentialReads != 0 {
		t.Fatal("historical read resolved a live registration or credential")
	}
	permission.denied = true
	if _, err := f.service.Get(t.Context(), "bot", "user", f.root, ComputerOperation, f.op); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Bot permission revocation: %v", err)
	}
}

func TestNativeProfileChangedTaskProjectionPreservesHistory(t *testing.T) {
	f := newFixture()
	current := f.targets[f.operations[f.op].TargetID]
	current.NativeRuntimeID = "synthetic-runtime"
	current.ConnectionID = current.NativeRuntimeID
	current.InstanceID = "synthetic-instance"
	old := current
	old.Agent.ConfigDir = `C:\Synthetic\original`
	current.Agent.ConfigDir = `C:\Synthetic\changed`
	r := f.operations[f.op]
	r.TargetFingerprint = agenvo.TargetFingerprint(old)
	r.State = "unknown"
	r.Details, _ = json.Marshal(map[string]any{"kind": "herdr_pi_task", "target_snapshot": old})
	f.operations[f.op] = r
	f.targets[r.TargetID] = current
	before, _ := json.Marshal(f.operations)
	got, err := f.service.Get(t.Context(), "bot", "user", f.root, ComputerOperation, f.op)
	if err != nil || got.Status != "unknown" {
		t.Fatalf("history after profile change: %+v %v", got, err)
	}
	if _, err = f.service.Get(t.Context(), "bot", "other", f.root, ComputerOperation, f.op); err == nil {
		t.Fatal("foreign user read")
	}
	delete(f.targets, r.TargetID)
	if got, err = f.service.Get(t.Context(), "bot", "user", f.root, ComputerOperation, f.op); err != nil || got.Status != "unknown" {
		t.Fatal("deleted native target hid history", err)
	}
	f.historyDenied = true
	if _, err = f.service.Get(t.Context(), "bot", "user", f.root, ComputerOperation, f.op); !errors.Is(err, ErrUnavailable) {
		t.Fatal("revoked Bot permission allowed history", err)
	}
	after, _ := json.Marshal(f.operations)
	if string(before) != string(after) {
		t.Fatal("history modified")
	}
}

func TestRetiredOperationHistoryStillChecksOriginalScopeAndSnapshot(t *testing.T) {
	for _, scenario := range []string{"user", "bot", "session", "selected-context", "delegation", "permission", "empty-fingerprint", "mismatched-snapshot", "empty-snapshot", "malformed-snapshot"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture()
			r := f.operations[f.op]
			r.State = "unknown"
			bot, user, parent := "bot", "user", f.root
			ctx := t.Context()
			switch scenario {
			case "user":
				user = "other"
			case "bot":
				bot = "other"
			case "session":
				parent = f.child
				r.SessionID = f.root
			case "selected-context":
				ctx = toolcontext.WithComputerScope(ctx, "pc-b")
			case "delegation":
				ctx = toolcontext.WithDelegationScope(ctx, &toolcontext.DelegationScope{Target: func(context.Context, string, string, string) error { return ErrUnavailable }})
			case "permission":
				f.historyDenied = true
			case "empty-fingerprint":
				r.TargetFingerprint = ""
			case "mismatched-snapshot":
				r.Details, _ = json.Marshal(map[string]any{"kind": "herdr_pi_task", "target_snapshot": agenvo.Target{DeviceID: "another-device"}})
			case "empty-snapshot":
				r.Details = []byte(`{"kind":"herdr_pi_task","target_snapshot":{}}`)
			case "malformed-snapshot":
				r.Details = []byte(`{"kind":"herdr_pi_task","target_snapshot":"not-a-target"}`)
			}
			f.operations[f.op] = r
			delete(f.targets, "pc-a")
			before, _ := json.Marshal(f.operations)
			if _, err := f.service.Get(ctx, bot, user, parent, ComputerOperation, f.op); !errors.Is(err, ErrUnavailable) {
				t.Error("retired historical detail escaped original authorization", err)
			}
			page, err := f.service.List(ctx, bot, user, parent, 100, "")
			if err != nil && !errors.Is(err, ErrUnavailable) {
				t.Fatal(err)
			}
			for _, item := range page.Items {
				if item.TaskID == f.op {
					t.Error("retired historical list exposed denied operation")
				}
			}
			after, _ := json.Marshal(f.operations)
			if string(before) != string(after) {
				t.Error("denied history read changed unknown operation")
			}
		})
	}
}

func TestDeletedWorkdirDoesNotMoveHistoryToCurrentPrimary(t *testing.T) {
	f := newFixture()
	r := f.operations[f.op]
	r.State = "unknown"
	f.operations[f.op] = r
	root := f.sessions[f.root]
	root.WorkdirID = "retired-workdir"
	f.workdirs[root.WorkdirID] = workdir.Workdir{ID: root.WorkdirID, BotID: "bot", WorkspaceTargetID: r.TargetID}
	f.sessions[f.root] = root
	if _, err := f.service.Get(t.Context(), "bot", "user", f.root, ComputerOperation, f.op); err != nil {
		t.Fatal(err)
	}
	// Model the persisted result of runtime/mount -> workdir cascade and
	// session.workdir_id SET NULL. A later default is not the historical target.
	delete(f.targets, r.TargetID)
	delete(f.workdirs, root.WorkdirID)
	root.WorkdirID = ""
	f.sessions[f.root] = root
	f.primary = "pc-b"
	before, _ := json.Marshal(f.operations)
	got, err := f.service.Get(t.Context(), "bot", "user", f.root, ComputerOperation, f.op)
	if err != nil || got.TargetID != "pc-a" || got.Status != "unknown" {
		t.Errorf("current primary replaced original history target: %+v %v", got, err)
	}
	page, err := f.service.List(t.Context(), "bot", "user", f.root, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range page.Items {
		found = found || item.TaskID == f.op
	}
	if !found || f.primaryReads != 0 {
		t.Error("history consulted current primary instead of original receipt", f.primaryReads)
	}
	after, _ := json.Marshal(f.operations)
	if string(before) != string(after) {
		t.Error("history read rewrote original unknown task")
	}
}
