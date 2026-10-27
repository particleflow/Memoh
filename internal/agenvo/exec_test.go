package agenvo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The fixture models the public Agenvo MCP/native outcome envelopes. It does
// not claim to run Herdr or PowerShell; real Windows acceptance is separate.
type fixture struct {
	t              *testing.T
	target         Target
	grants         map[Scope]bool
	labels         map[string]string
	panes          map[string]string
	calls          []string
	runs           int
	nonce          string
	output         string
	exit           int
	truncated      bool
	submitErr      bool
	nativeRejected bool
	neverComplete  bool
	afterSubmit    func()
}

func newFixture(t *testing.T) (*fixture, *Executor) {
	f := &fixture{t: t, target: Target{NativeRuntimeID: "runtime", ConnectionID: "runtime", DeviceID: "device", InstanceID: "windows", Session: "default", WorkDir: `C:\synthetic`, BashPath: `C:\Synthetic\Git\bin\bash.exe`}, grants: map[Scope]bool{{"bot-a", "mount-a", "user"}: true, {"bot-b", "mount-b", "user"}: true}, labels: map[string]string{}, panes: map[string]string{}, output: "synthetic result"}
	c := NewClient(func(_ context.Context, scope Scope) (Target, error) {
		if !f.grants[scope] {
			return Target{}, errors.New("permission denied")
		}
		return f.target, nil
	}, f.call)
	e := NewExecutor(c)
	e.pollInterval = time.Millisecond
	return f, e
}

func (f *fixture) call(_ context.Context, _ Scope, target Target, method string, params map[string]any) (Receipt, error) {
	f.t.Helper()
	if target != f.target || target.NativeRuntimeID == "" {
		f.t.Fatal("native target changed")
	}

	f.calls = append(f.calls, method)
	if params["session"] != "default" {
		f.t.Fatal("native session changed")
	}
	var result any
	switch method {
	case "workspace.list":
		workspaces := []map[string]string{}
		for id := range f.labels {
			workspaces = append(workspaces, map[string]string{"workspace_id": id})
		}
		result = map[string]any{"workspaces": workspaces}
	case "workspace.create":
		id := fmt.Sprintf("w%d", len(f.labels)+1)
		pane := "p" + id
		label, _ := params["label"].(string)
		f.labels[id], f.panes[pane] = label, id
		result = map[string]any{"workspace": map[string]string{"workspace_id": id, "label": label}, "root_pane": map[string]string{"pane_id": pane}}
	case "pane.run":
		f.runs++
		if f.panes[fmt.Sprint(params["paneId"])] == "" {
			f.t.Fatal("command sent to an unowned pane")
		}
		command, _ := params["command"].(string)
		bootstrap := regexp.MustCompile(`printf %s ([A-Za-z0-9+/=]+) \| base64`).FindStringSubmatch(command)
		if len(bootstrap) != 2 {
			f.t.Fatal("missing encoded Bash script")
		}
		decoded, err := base64.StdEncoding.DecodeString(bootstrap[1])
		if err != nil {
			f.t.Fatal(err)
		}
		script := string(decoded)
		match := regexp.MustCompile(`MEMOH_([0-9a-f]{32}):`).FindStringSubmatch(script)
		if len(match) != 2 {
			f.t.Fatal("missing command completion nonce")
		}
		f.nonce = match[1]
		if f.afterSubmit != nil {
			f.afterSubmit()
		}
		if f.submitErr {
			return Receipt{}, ErrUnknown
		}
		if f.nativeRejected {
			return fixtureReceipt(map[string]any{"execution": "rejected", "error": map[string]string{"code": "native_error", "message": "PRIVATE"}}), ErrRejected
		}
		result = map[string]any{"accepted": true}
	case "pane.read":
		if f.panes[fmt.Sprint(params["paneId"])] == "" {
			f.t.Fatal("read another Bot's pane")
		}
		output := "PS C:\\synthetic> idle\n"
		if !f.neverComplete {
			payload, _ := json.Marshal(map[string]any{"output_base64": base64.StdEncoding.EncodeToString([]byte(f.output)), "exit_code": f.exit, "truncated": f.truncated})
			output += "MEMOH_" + f.nonce + ":" + base64.StdEncoding.EncodeToString(payload) + ":END_" + f.nonce + "\r\n"
		}
		return fixtureReceipt(map[string]any{"execution": "accepted", "requestId": "synthetic-request", "result": map[string]any{"output": output, "session": "default"}}), nil

	case "workspace.close":
		f.t.Fatal("automatic close is forbidden without atomic generation matching")
	default:
		f.t.Fatalf("unexpected native method %s", method)
	}
	return fixtureReceipt(map[string]any{"execution": "accepted", "requestId": "synthetic-request", "result": map[string]any{"result": result, "session": "default"}}), nil
}

func fixtureReceipt(value any) Receipt {
	raw, _ := json.Marshal(value)
	var receipt Receipt
	_ = json.Unmarshal(raw, &receipt)
	return receipt
}

func request() Request {
	return Request{Scope: Scope{"bot-a", "mount-a", "user"}, Command: "printf 'synthetic\\n'", Timeout: time.Second}
}

func TestExecuteRequiresNonceBoundCompletionAndKeepsExitCode(t *testing.T) {
	f, e := newFixture(t)
	f.exit, f.truncated = 7, true
	r, err := e.Execute(t.Context(), request())
	if err != nil || r.State != "completed" || r.ExitCode == nil || *r.ExitCode != 7 || !r.Truncated || r.Output != "synthetic result" || !r.CleanupRequired {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	if f.runs != 1 {
		t.Fatalf("command submissions=%d", f.runs)
	}
}

func TestExecuteRejectsCrossBotMountBeforeAnyRemoteCall(t *testing.T) {
	f, e := newFixture(t)
	req := request()
	req.Scope = Scope{"bot-b", "mount-a", "user"}
	r, err := e.Execute(t.Context(), req)
	if err == nil || r.State != "not_started" || len(f.calls) != 0 {
		t.Fatalf("result=%+v err=%v calls=%v", r, err, f.calls)
	}
}

func TestExecuteRechecksGrantAfterSubmission(t *testing.T) {
	f, e := newFixture(t)
	f.afterSubmit = func() { delete(f.grants, request().Scope) }
	r, err := e.Execute(t.Context(), request())
	if err == nil || r.State != "unknown" || r.ExitCode != nil || !r.CleanupRequired {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	for _, method := range f.calls {
		if method == "pane.read" || method == "workspace.close" {
			t.Fatal("revoked grant used again")
		}
	}
}

func TestExecuteLostSubmissionResponseNeverRetriesOrReportsSuccess(t *testing.T) {
	f, e := newFixture(t)
	f.submitErr = true
	r, err := e.Execute(t.Context(), request())
	if !errors.Is(err, ErrUnknown) || r.State != "unknown" || r.ExitCode != nil || f.runs != 1 || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatalf("result=%+v err=%v submissions=%d", r, err, f.runs)
	}
}

func TestExecuteNativeRejectionRetainsUnknownOutcome(t *testing.T) {
	f, e := newFixture(t)
	f.nativeRejected = true
	r, err := e.Execute(t.Context(), request())
	if !errors.Is(err, ErrRejected) || r.State != "rejected" || r.ExitCode != nil || f.runs != 1 || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatalf("result=%+v err=%v", r, err)
	}
}

func TestExecuteCancellationRetainsUncertainWorkspace(t *testing.T) {
	f, e := newFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f.afterSubmit = cancel
	r, err := e.Execute(ctx, request())
	if !errors.Is(err, context.Canceled) || r.State != "unknown" || !r.CleanupRequired || r.ExitCode != nil || f.runs != 1 {
		t.Fatalf("result=%+v err=%v", r, err)
	}
}

func TestExecuteRetainsWorkspaceWithoutUnsafeClose(t *testing.T) {
	f, e := newFixture(t)
	r, err := e.Execute(t.Context(), request())
	if err != nil || !r.CleanupRequired {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	for _, method := range f.calls {
		if method == "workspace.close" {
			t.Fatal("foreign workspace closed")
		}
	}
}

func TestExecuteIdlePromptIsNotCompletion(t *testing.T) {
	f, e := newFixture(t)
	f.neverComplete = true
	req := request()
	req.Timeout = 10 * time.Millisecond
	r, err := e.Execute(t.Context(), req)
	if !errors.Is(err, context.DeadlineExceeded) || r.State != "unknown" || r.ExitCode != nil || f.runs != 1 {
		t.Fatalf("result=%+v err=%v", r, err)
	}
}

func TestUnsupportedExecOptionsNeverReachRelay(t *testing.T) {
	for _, change := range []func(*Request){func(r *Request) { r.Stdin = []byte("x") }, func(r *Request) { r.Env = []string{"X=Y"} }, func(r *Request) { r.PTY = true }, func(r *Request) { r.Background = true }} {
		f, e := newFixture(t)
		req := request()
		change(&req)
		r, err := e.Execute(t.Context(), req)
		if !errors.Is(err, ErrUnsupported) || r.State != "not_started" || len(f.calls) != 0 {
			t.Fatalf("result=%+v err=%v", r, err)
		}
	}
}

func TestUnavailableTargetHasNoNativeFallback(t *testing.T) {
	f, e := newFixture(t)
	e.client.callNative = func(context.Context, Scope, Target, string, map[string]any) (Receipt, error) {
		return Receipt{}, errors.Join(ErrUnknown, errors.New("offline PRIVATE"))
	}
	r, err := e.Execute(t.Context(), request())
	if !errors.Is(err, ErrUnknown) || r.State != "not_started" || f.runs != 0 {
		t.Fatalf("result=%+v err=%v", r, err)
	}
}

func TestCompletionRejectsMalformedOrAmbiguousEvidence(t *testing.T) {
	valid := base64.StdEncoding.EncodeToString([]byte(`{"exit_code":0,"output_base64":"b2s=","truncated":false}`))
	for _, snapshot := range []string{"MEMOH_n:%%%:END_n", "MEMOH_n:" + base64.StdEncoding.EncodeToString([]byte(`{"output_base64":"b2s="}`)) + ":END_n", "MEMOH_n:" + valid + ":END_n\nMEMOH_n:" + valid + ":END_n"} {
		if _, _, err := parseCompletion(snapshot, "n"); !errors.Is(err, ErrUnknown) {
			t.Fatalf("accepted ambiguous evidence: %v", err)
		}
	}
	if _, found, err := parseCompletion("MEMOH_other:"+valid+":END_other", "n"); err != nil || found {
		t.Fatalf("accepted another operation: found=%v err=%v", found, err)
	}
}
