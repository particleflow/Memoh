package computertask

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/felinics/memoh/internal/agenvo"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/mcp"
	"github.com/felinics/memoh/internal/toolcontext"
)

type fixtureStore struct {
	mu           sync.Mutex
	records      map[string]dbstore.ComputerOperationRecord
	mounts       map[string]dbstore.BotRemoteRuntimeBindingRecord
	revoked      bool
	failRevision *int64
	onClaim      func()
}

func (s *fixtureStore) ClaimComputerOperation(_ context.Context, r dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, old := range s.records {
		if old.BotID == r.BotID && old.SessionID == r.SessionID && old.UserID == r.UserID && ((old.RunID == r.RunID && old.Fingerprint == r.Fingerprint) || (old.TargetID == r.TargetID && (old.State == "unknown" || old.State == "submitted" || old.State == "dispatching"))) {
			return old, false, nil
		}
	}
	if s.onClaim != nil {
		s.onClaim()
	}
	r.State = "dispatching"
	s.records[r.ID] = r
	return r, true, nil
}

func (s *fixtureStore) ReadComputerOperation(_ context.Context, r dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.records[r.ID]
	if !ok || old.BotID != r.BotID || old.SessionID != r.SessionID || old.UserID != r.UserID {
		return r, ErrDenied
	}
	return old, nil
}

func (s *fixtureStore) SaveComputerOperation(_ context.Context, r dbstore.ComputerOperationRecord) (dbstore.ComputerOperationRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failRevision != nil && r.Revision == *s.failRevision {
		return r, errors.New("synthetic persistence failure")
	}
	if s.records[r.ID].Revision != r.Revision {
		return r, errors.New("concurrent update")
	}
	r.Revision++
	s.records[r.ID] = r
	return r, nil
}

func (s *fixtureStore) GetMount(_ context.Context, bot, target string) (dbstore.BotRemoteRuntimeBindingRecord, error) {
	r, ok := s.mounts[target]
	if !ok || r.BotID != bot {
		return r, ErrDenied
	}
	r.RuntimeRevoked = s.revoked
	return r, nil
}

func (s *fixtureStore) ListMounts(ctx context.Context, bot string) ([]dbstore.BotRemoteRuntimeBindingRecord, error) {
	var out []dbstore.BotRemoteRuntimeBindingRecord
	for id := range s.mounts {
		r, e := s.GetMount(ctx, bot, id)
		if e == nil {
			out = append(out, r)
		}
	}
	return out, nil
}

func (*fixtureStore) OwnedHTTPMCPConnectionBot(context.Context, string, string) (string, error) {
	return "bot-a", nil
}

func (*fixtureStore) CreateAgenvoRuntime(context.Context, string, string, dbstore.JSON) (dbstore.UserRuntimeRecord, error) {
	return dbstore.UserRuntimeRecord{}, errors.New("registration not part of task execution")
}

type permissionChecker struct{}

func (permissionChecker) HasBotPermission(_ context.Context, bot, user, _ string) (bool, error) {
	return bot == "bot-a" && user == "user-a", nil
}

type transportFixture struct {
	t           *testing.T
	calls       []string
	runs        int
	nonce       string
	lost        bool
	agentName   string
	promptFault string
	cwd         string
}

func (f *transportFixture) call(_ context.Context, owner, connection, tool string, args map[string]any) (json.RawMessage, error) {
	f.t.Helper()
	if owner != "owner" || connection != "11111111-1111-4111-8111-111111111111" || tool != "execute" {
		f.t.Fatal("wrong transport identity")
	}
	code, _ := args["code"].(string)
	var parts []json.RawMessage
	if json.Unmarshal([]byte("["+strings.TrimSuffix(strings.TrimPrefix(code, "return await call("), ");")+"]"), &parts) != nil || len(parts) != 3 {
		f.t.Fatal("not fixed relay invocation")
	}
	var method string
	var params map[string]any
	var target map[string]string
	_ = json.Unmarshal(parts[0], &target)
	_ = json.Unmarshal(parts[1], &method)
	_ = json.Unmarshal(parts[2], &params)
	if target["deviceId"] != "device-a" {
		f.t.Fatal("cross-device dispatch")
	}
	f.calls = append(f.calls, method)
	var body any
	switch method {
	case "workspace.list":
		body = map[string]any{"workspaces": []any{}}
	case "workspace.create":
		f.cwd, _ = params["cwd"].(string)
		body = map[string]any{"workspace": map[string]any{"workspace_id": "w-synthetic", "label": params["label"]}, "root_pane": map[string]string{"pane_id": "p-synthetic"}}
	case "pane.run":
		f.runs++
		if f.lost {
			return nil, errors.New("synthetic lost response")
		}
		command, _ := params["command"].(string)
		encoded := regexp.MustCompile(`printf %s ([A-Za-z0-9+/=]+) \| base64`).FindStringSubmatch(command)
		if len(encoded) != 2 {
			f.t.Fatal("missing Bash bootstrap")
		}
		script, _ := base64.StdEncoding.DecodeString(encoded[1])
		match := regexp.MustCompile(`MEMOH_([0-9a-f]{32}):`).FindSubmatch(script)
		if len(match) != 2 {
			f.t.Fatal("missing nonce")
		}
		f.nonce = string(match[1])
		body = map[string]any{}
	case "pane.read":
		frame, _ := json.Marshal(map[string]any{"output_base64": base64.StdEncoding.EncodeToString([]byte("42\n")), "exit_code": 0, "truncated": false})
		return envelope(map[string]any{"output": "MEMOH_" + f.nonce + ":" + base64.StdEncoding.EncodeToString(frame) + ":END_" + f.nonce + "\r\n", "session": "synthetic"}), nil
	case "tab.create":
		body = map[string]any{"root_pane": map[string]string{"workspace_id": "w-synthetic", "pane_id": "p-agent", "terminal_id": "t-agent"}}
	case "agent.start":
		f.agentName = params["name"].(string)
		return receipt("starting", map[string]any{"session": "synthetic", "name": f.agentName, "query": "agent.get"}), nil
	case "agent.get":
		body = map[string]any{"agent": map[string]any{"name": f.agentName, "agent": "pi", "workspace_id": "w-synthetic", "pane_id": "p-agent", "terminal_id": "t-agent", "agent_status": "done", "interactive_ready": true, "completion_seq": 1}}
	case "agent.prompt":
		switch f.promptFault {
		case "rejected":
			return envelope(map[string]any{"nativeError": map[string]any{"execution": "rejected", "error": map[string]any{"code": "native_error", "native": map[string]string{"code": "agent_not_found"}}}}), nil
		case "wrong-type":
			return envelope(map[string]any{"id": "cli:agent:prompt", "session": "synthetic", "result": map[string]string{"type": "ok"}}), nil
		case "missing":
			return envelope(map[string]string{"session": "synthetic"}), nil
		}

		return envelope(map[string]any{"id": "cli:agent:prompt", "session": "synthetic", "result": map[string]any{"type": "agent_prompted", "agent": map[string]any{"name": f.agentName, "agent": "pi", "workspace_id": "w-synthetic", "pane_id": "p-agent", "terminal_id": "t-agent", "agent_status": "done"}}}), nil
	case "agent.read":
		return envelope(map[string]any{"session": "synthetic", "output": "unverified Pi output"}), nil
	default:
		f.t.Fatalf("unexpected method %s", method)
	}
	return envelope(map[string]any{"result": body, "session": "synthetic"}), nil
}

func envelope(result any) json.RawMessage {
	return receipt("accepted", result)
}

func receipt(execution string, result any) json.RawMessage {
	body, _ := json.Marshal(map[string]any{"execution": execution, "requestId": "synthetic-request", "result": result})
	raw, _ := json.Marshal(map[string]any{"content": []map[string]string{{"type": "text", "text": string(body)}}})
	return raw
}

func TestStandardMCPConfiguredHerdrTaskUsesFixedOperationsWithoutRawSearch(t *testing.T) {
	source, store, f := setup(t)
	record := store.mounts["pc-a"]
	var target agenvo.Target
	_ = json.Unmarshal(record.RuntimeProviderConfig, &target)
	target.Agent = agenvo.AgentProfile{Provider: "synthetic-magpie", Model: "synthetic-model", ConfigDir: `C:\Synthetic\Pi`}
	record.RuntimeProviderConfig, _ = json.Marshal(target)
	store.mounts["pc-a"] = record
	c := standardClient(t, source, "pc-a", false)
	r := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "compute 42"}, false))
	if r.State != "submitted" || r.Phase != "prompt_submitted" || r.ExitCode != nil {
		t.Fatalf("Agent submission=%+v", r)
	}
	observed := parseResult(t, invoke(t, c, "computer_task_result", map[string]any{"operation_id": r.ID}, false))
	if observed.State != "unknown" || observed.Observation == nil || observed.Observation.TaskSuccessVerified || observed.Output != "unverified Pi output" {
		t.Fatalf("unverified observation=%+v", observed)
	}
	again := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "retry with different intent"}, true))
	if again.ID != r.ID || again.BlockedByOperation != r.ID {
		t.Fatal("Agent prompt resubmitted")
	}
	starts, prompts := 0, 0
	for _, method := range f.calls {
		if method == "agent.start" {
			starts++
		}
		if method == "agent.prompt" {
			prompts++
		}
		if method == "search" || method == "workspace.close" {
			t.Fatal("unexpected raw discovery/cleanup")
		}
	}
	if starts != 1 || prompts != 1 || f.runs != 0 {
		t.Fatalf("Agent procedure calls=%v", f.calls)
	}
}

func setup(t *testing.T) (*Source, *fixtureStore, *transportFixture) {
	t.Helper()
	store := &fixtureStore{records: map[string]dbstore.ComputerOperationRecord{}, mounts: map[string]dbstore.BotRemoteRuntimeBindingRecord{}}
	for _, name := range []string{"a", "b"} {
		target := agenvo.Target{ConnectionID: "11111111-1111-4111-8111-111111111111", DeviceID: "device-" + name, InstanceID: "herdr-" + name, Session: "synthetic", WorkDir: `C:\Synthetic`, BashPath: `C:\Synthetic\Git\bin\bash.exe`}
		raw, _ := json.Marshal(target)
		store.mounts["pc-"+name] = dbstore.BotRemoteRuntimeBindingRecord{ID: "pc-" + name, BotID: "bot-a", RuntimeName: "PC-" + name, RuntimeUserID: "owner", BotOwnerUserID: "owner", RuntimeProvider: "agenvo", RuntimeProviderConfig: raw}
	}
	f := &transportFixture{t: t}
	relay := agenvo.NewService(store, permissionChecker{}, f.call)
	service := NewService(store, store, relay, nil, func(context.Context, toolcontext.ComputerBinding, string, string, string) error { return nil })
	return NewSource(service), store, f
}

type testHeaders struct{ base http.RoundTripper }

func (h testHeaders) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("X-Memoh-Session-Id", "forged-session")
	r.Header.Set("X-Memoh-Workspace-Target-Id", "pc-b")
	return h.base.RoundTrip(r)
}

func standardClient(t *testing.T, source *Source, target string, public bool) *sdkmcp.ClientSession {
	t.Helper()
	// This is the existing host tool-mount contract: an authenticated active
	// turn supplies immutable identity. HTTP headers are deliberately ignored.
	run, err := toolcontext.WithComputerBinding(t.Context(), toolcontext.ComputerBinding{BotID: "bot-a", UserID: "user-a", SessionID: "thread-a", TargetID: target, RunID: "run-a"})
	if err != nil {
		t.Fatal(err)
	}
	session := mcp.ToolSessionContext{BotID: "bot-a", SessionID: "thread-a", RunID: "run-a", RequireActiveRun: true, RuntimeActive: true, RunContext: run, PublicRequest: public}
	gateway := mcp.NewToolGatewayService(slog.New(slog.DiscardHandler), []mcp.ToolSource{source})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mcp.ServeToolMCPHTTP(w, r, slog.New(slog.DiscardHandler), gateway, nil, session)
	}))
	t.Cleanup(server.Close)
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "unmodified-standard-client", Version: "test"}, nil)
	cs, err := client.Connect(t.Context(), &sdkmcp.StreamableClientTransport{Endpoint: server.URL, HTTPClient: &http.Client{Transport: testHeaders{http.DefaultTransport}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func invoke(t *testing.T, c *sdkmcp.ClientSession, name string, args map[string]any, wantError bool) json.RawMessage {
	t.Helper()
	r, err := c.CallTool(t.Context(), &sdkmcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if r.IsError != wantError {
		t.Fatalf("%s error=%v content=%v", name, r.IsError, r.Content)
	}
	raw, _ := json.Marshal(r.StructuredContent)
	return raw
}

func parseResult(t *testing.T, raw []byte) Result {
	t.Helper()
	var v struct {
		Computer Result `json:"computer"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v.Computer
}

func TestStandardMCPTrustedPCBindingAndSingleTask(t *testing.T) {
	source, _, f := setup(t)
	c := standardClient(t, source, "pc-a", false)
	tools, err := c.ListTools(t.Context(), nil)
	if err != nil || len(tools.Tools) != 7 {
		t.Fatalf("tools=%v err=%v", tools, err)
	}
	listing := invoke(t, c, "computer_targets", map[string]any{}, false)
	if !strings.Contains(string(listing), "pc-a") || strings.Contains(string(listing), "pc-b") || strings.Contains(string(listing), "device-a") {
		t.Fatalf("inventory leak: %s", listing)
	}
	for _, target := range []string{"pc-b", "native", "missing"} {
		invoke(t, c, "computer_task_submit", map[string]any{"target_id": target, "kind": "foreground_bash", "task": "printf 42"}, true)
	}
	if len(f.calls) != 0 {
		t.Fatal("denial dispatched transport")
	}
	args := map[string]any{"kind": "foreground_bash", "task": "printf 42", "work_dir": "."}
	r := parseResult(t, invoke(t, c, "computer_task_submit", args, false))
	if r.State != "completed" || r.Output != "42\n" || r.ExitCode == nil || *r.ExitCode != 0 || f.runs != 1 {
		t.Fatalf("result=%+v runs=%d", r, f.runs)
	}
	for _, tool := range []string{"computer_task_get", "computer_task_result"} {
		got := parseResult(t, invoke(t, c, tool, map[string]any{"operation_id": r.ID}, false))
		if got.ID != r.ID || got.State != "completed" {
			t.Fatalf("receipt=%+v", got)
		}
	}
	again := parseResult(t, invoke(t, c, "computer_task_submit", args, false))
	if again.ID != r.ID || f.runs != 1 {
		t.Fatal("duplicate submission")
	}
	invoke(t, c, "computer_task_submit", map[string]any{"kind": "foreground_bash", "task": "printf 42", "method": "agent.start"}, true)
}

func TestStandardMCPUnknownIsDurableAndCancellationDoesNotKill(t *testing.T) {
	source, store, f := setup(t)
	f.lost = true
	c := standardClient(t, source, "pc-a", false)
	r := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "foreground_bash", "task": "printf 42"}, false))
	if r.State != "unknown" || r.ExitCode != nil {
		t.Fatalf("unknown=%+v", r)
	}
	for i := 0; i < 3; i++ {
		got := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "foreground_bash", "task": fmt.Sprintf("different %d", i)}, true))
		if got.ID != r.ID || got.BlockedByOperation != r.ID {
			t.Fatal("unsettled operation did not return explicit conflict")
		}
	}
	cancel := parseResult(t, invoke(t, c, "computer_task_cancel", map[string]any{"operation_id": r.ID}, false))
	if !cancel.CancelRequested || cancel.ProcessStopped || cancel.State != "unknown" || f.runs != 1 {
		t.Fatalf("cancel=%+v runs=%d", cancel, f.runs)
	}
	store.revoked = true
	for _, tool := range []string{"computer_task_get", "computer_task_result", "computer_task_cancel", "computer_task_artifact", "computer_task_logs"} {
		invoke(t, c, tool, map[string]any{"operation_id": r.ID}, true)
	}
}

func TestStandardMCPPublicHeadersCannotGrantBinding(t *testing.T) {
	source, _, f := setup(t)
	c := standardClient(t, source, "pc-a", true)
	invoke(t, c, "computer_targets", map[string]any{}, true)
	invoke(t, c, "computer_task_submit", map[string]any{"kind": "foreground_bash", "task": "printf 42"}, true)
	if len(f.calls) != 0 {
		t.Fatal("untrusted dispatch")
	}
}

func TestStandardMCPPersistenceFailureRetainsUnknownReceiptWithoutRedispatch(t *testing.T) {
	source, store, f := setup(t)
	revision := int64(1)
	store.failRevision = &revision
	c := standardClient(t, source, "pc-a", false)
	args := map[string]any{"kind": "foreground_bash", "task": "printf 42"}
	r := parseResult(t, invoke(t, c, "computer_task_submit", args, true))
	if r.ID == "" || r.State != "unknown" || r.ExitCode != nil || f.runs != 1 {
		t.Fatalf("failed receipt=%+v runs=%d", r, f.runs)
	}
	store.failRevision = nil
	got := parseResult(t, invoke(t, c, "computer_task_submit", args, false))
	if got.ID != r.ID || got.State != "unknown" || f.runs != 1 {
		t.Fatal("persistence failure allowed duplicate effect")
	}
}

func TestStandardMCPInvalidWorkdirDoesNotReserveOrDispatch(t *testing.T) {
	source, store, f := setup(t)
	c := standardClient(t, source, "pc-a", false)
	invoke(t, c, "computer_task_submit", map[string]any{"kind": "foreground_bash", "task": "printf 42", "work_dir": "../escape"}, true)
	if len(store.records) != 0 || len(f.calls) != 0 {
		t.Fatal("invalid workdir reserved or dispatched")
	}
}

func TestStandardMCPNASAmbiguityDoesNotGuess(t *testing.T) {
	source, _, f := setup(t)
	c := standardClient(t, source, "native", false)
	raw := invoke(t, c, "computer_targets", map[string]any{}, false)
	if !strings.Contains(string(raw), "pc-a") || !strings.Contains(string(raw), "pc-b") {
		t.Fatalf("missing authorized targets: %s", raw)
	}
	invoke(t, c, "computer_task_submit", map[string]any{"kind": "foreground_bash", "task": "printf 42"}, true)
	if len(f.calls) != 0 {
		t.Fatal("ambiguous dispatch")
	}
	invoke(t, c, "computer_task_submit", map[string]any{"target_id": "pc-a", "kind": "foreground_bash", "task": "printf 42"}, false)
}

func TestStandardMCPPromptFailuresHaveDurableHonestReceipts(t *testing.T) {
	for _, fault := range []string{"rejected", "wrong-type", "missing"} {
		t.Run(fault, func(t *testing.T) {
			source, store, f := setup(t)
			f.promptFault = fault
			r := store.mounts["pc-a"]
			var target agenvo.Target
			_ = json.Unmarshal(r.RuntimeProviderConfig, &target)
			target.Agent = agenvo.AgentProfile{Provider: "synthetic", Model: "synthetic", ConfigDir: `C:\Synthetic\Pi`}
			r.RuntimeProviderConfig, _ = json.Marshal(target)
			store.mounts["pc-a"] = r
			c := standardClient(t, source, "pc-a", false)
			args := map[string]any{"kind": "herdr_pi_task", "task": "synthetic"}
			got := parseResult(t, invoke(t, c, "computer_task_submit", args, true))
			want := "unknown"
			if fault == "rejected" {
				want = "rejected"
			}
			var d Details
			_ = json.Unmarshal(store.records[got.ID].Details, &d)
			if got.State != want || d.Agent.PromptSubmitted || got.ExitCode != nil {
				t.Fatalf("false submission: %+v %+v", got, d.Agent)
			}
			again := parseResult(t, invoke(t, c, "computer_task_submit", args, false))
			if again.ID != got.ID {
				t.Fatal("resubmitted uncertain prompt")
			}
			prompts := 0
			for _, call := range f.calls {
				if call == "agent.prompt" {
					prompts++
				}
			}
			if prompts != 1 {
				t.Fatalf("prompts=%d", prompts)
			}
		})
	}
}

func TestStandardMCPRegistrationChangeAfterClaimNeverDispatches(t *testing.T) {
	source, store, f := setup(t)
	store.onClaim = func() {
		r := store.mounts["pc-a"]
		var target agenvo.Target
		_ = json.Unmarshal(r.RuntimeProviderConfig, &target)
		target.WorkDir = `D:\Moved`
		r.RuntimeProviderConfig, _ = json.Marshal(target)
		store.mounts["pc-a"] = r
	}
	c := standardClient(t, source, "pc-a", false)
	got := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "foreground_bash", "task": "printf 42"}, true))
	if got.State != "not_started" || len(f.calls) != 0 {
		t.Fatalf("changed registration dispatched: %+v %v", got, f.calls)
	}
}

func TestStandardMCPRootRelativePathAndForegroundEvidence(t *testing.T) {
	for _, tt := range []struct{ root, input, want string }{{`C:\Synthetic`, "", `C:\Synthetic`}, {`C:\Synthetic`, ".", `C:\Synthetic`}, {`C:\Synthetic`, "child", `C:\Synthetic\child`}, {`D:\Moved`, "child", `D:\Moved\child`}, {`C:\Synthetic`, `C:\Synthetic`, `C:\Synthetic`}} {
		t.Run(tt.root+tt.input, func(t *testing.T) {
			source, store, f := setup(t)
			r := store.mounts["pc-a"]
			var target agenvo.Target
			_ = json.Unmarshal(r.RuntimeProviderConfig, &target)
			target.WorkDir = tt.root
			r.RuntimeProviderConfig, _ = json.Marshal(target)
			store.mounts["pc-a"] = r
			c := standardClient(t, source, "pc-a", false)
			got := parseResult(t, invoke(t, c, "computer_task_submit", map[string]any{"kind": "foreground_bash", "task": "printf 42", "work_dir": tt.input}, false))
			if f.cwd != tt.want || got.WorkspaceID != "w-synthetic" || got.PaneID == "" || got.RequestID != "synthetic-request" || !got.CleanupRequired {
				t.Fatalf("cwd=%q result=%+v", f.cwd, got)
			}
			persisted := parseResult(t, invoke(t, c, "computer_task_get", map[string]any{"operation_id": got.ID}, false))
			if persisted.WorkspaceID != got.WorkspaceID || persisted.PaneID != got.PaneID || persisted.RequestID != got.RequestID || !persisted.CleanupRequired {
				t.Fatal("foreground evidence lost")
			}
		})
	}
}
