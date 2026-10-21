package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/agent/toolexec"
	"github.com/felinics/memoh/internal/agenvo"
	workspacepkg "github.com/felinics/memoh/internal/workspace"
	"github.com/felinics/memoh/internal/workspace/bridge"
)

// Exercise the public tool decoder, target routing and real Agenvo executor.
// Only the external MCP transport is synthetic; no Windows process is started.
type agenvoToolFixture struct {
	containerTestTargetProvider
	t           *testing.T
	calls       []string
	cwd         string
	nonce       string
	nativeCalls int
}

func (f *agenvoToolFixture) MCPClient(context.Context, string) (*bridge.Client, error) {
	f.nativeCalls++
	return nil, errors.New("native fallback forbidden")
}

func (f *agenvoToolFixture) call(_ context.Context, connection, tool string, args map[string]any) (json.RawMessage, error) {
	f.t.Helper()
	if connection != "synthetic-connection" || tool != "execute" {
		f.t.Fatal("wrong MCP destination")
	}
	code, _ := args["code"].(string)
	if !strings.HasPrefix(code, "return await call(") || !strings.HasSuffix(code, ");") {
		f.t.Fatal("invalid relay call")
	}
	var arguments []json.RawMessage
	if err := json.Unmarshal([]byte("["+strings.TrimSuffix(strings.TrimPrefix(code, "return await call("), ");")+"]"), &arguments); err != nil || len(arguments) != 3 {
		f.t.Fatalf("invalid arguments: %v", err)
	}
	var method string
	var params map[string]any
	if json.Unmarshal(arguments[1], &method) != nil || json.Unmarshal(arguments[2], &params) != nil {
		f.t.Fatal("invalid native arguments")
	}
	if params["session"] != "synthetic-session" {
		f.t.Fatal("wrong native session")
	}
	f.calls = append(f.calls, method)
	var result any
	switch method {
	case "workspace.list":
		result = map[string]any{"workspaces": []any{}}
	case "workspace.create":
		f.cwd, _ = params["cwd"].(string)
		label, _ := params["label"].(string)
		f.nonce = strings.TrimPrefix(label, "memoh-")
		result = map[string]any{"workspace": map[string]string{"workspace_id": "synthetic-workspace", "label": label}, "root_pane": map[string]string{"pane_id": "synthetic-pane"}}
	case "pane.run":
		if params["paneId"] != "synthetic-pane" {
			f.t.Fatal("wrong pane")
		}
		result = map[string]any{"output": ""}
	case "pane.read":
		if params["paneId"] != "synthetic-pane" {
			f.t.Fatal("wrong pane")
		}
		payload, _ := json.Marshal(map[string]any{"output_base64": base64.StdEncoding.EncodeToString([]byte("synthetic cwd accepted\n")), "exit_code": 0, "truncated": false})
		return agenvoToolReceipt(map[string]any{"output": "\r\nMEMOH_" + f.nonce + ":" + base64.StdEncoding.EncodeToString(payload) + ":END_" + f.nonce + "\r\n", "session": "synthetic-session"}), nil
	default:
		f.t.Fatalf("unexpected operation: %s", method)
	}
	return agenvoToolReceipt(map[string]any{"result": result, "session": "synthetic-session"}), nil
}

func agenvoToolReceipt(result any) json.RawMessage {
	receipt, _ := json.Marshal(map[string]any{"execution": "accepted", "requestId": "synthetic-request", "result": result})
	raw, _ := json.Marshal(map[string]any{"content": []map[string]string{{"type": "text", "text": string(receipt)}}, "isError": false})
	return raw
}

func TestAgenvoToolExecuteWorkDir(t *testing.T) {
	denied := errors.New("synthetic permission denied")
	for _, tt := range []struct {
		name, input, bound, want, caller, root string
		offline                                bool
		wantErr                                error
	}{
		{name: "omitted", want: `C:\Synthetic`},
		{name: "explicit default", input: `C:\Synthetic`, want: `C:\Synthetic`},
		{name: "case insensitive default", input: `c:\synthetic`, want: `C:\Synthetic`},
		{name: "absolute child", input: `C:\Synthetic\child`, want: `C:\Synthetic\child`},
		{name: "cross drive rejected", input: `D:\Other`, wantErr: agenvo.ErrUnsupported},
		{name: "drive slash", input: `C:/Synthetic/child`, want: `C:\Synthetic\child`},
		{name: "UNC outside root rejected", input: `\\synthetic-host\share\child`, wantErr: agenvo.ErrUnsupported},
		{name: "bound relative default", bound: `child`, want: `C:\Synthetic\child`},
		{name: "explicit bound", bound: `C:\Synthetic\child`, input: `C:\Synthetic\child`, want: `C:\Synthetic\child`},
		{name: "explicit dot", input: ".", want: `C:\Synthetic`},
		{name: "relative child", input: "child", want: `C:\Synthetic\child`},
		{name: "explicit relative uses root not bound cwd", bound: `C:\Synthetic\bound`, input: "child", want: `C:\Synthetic\child`},
		{name: "parent relative rejected", input: `..\child`, wantErr: agenvo.ErrUnsupported},
		{name: "drive relative rejected", input: `C:child`, wantErr: agenvo.ErrUnsupported},
		{name: "root relative rejected", input: `\child`, wantErr: agenvo.ErrUnsupported},
		{name: "POSIX rejected", input: "/tmp/synthetic", wantErr: agenvo.ErrUnsupported},
		{name: "NUL rejected", input: "C:\\Synthetic\x00", wantErr: agenvo.ErrUnsupported},
		{name: "caller denied", input: `C:\Synthetic`, caller: "unauthorized", wantErr: denied},
		{name: "changed root default", root: `D:\Moved`, want: `D:\Moved`},
		{name: "changed root relative", root: `D:\Moved`, input: "child", want: `D:\Moved\child`},
		{name: "changed root stale absolute rejected", root: `D:\Moved`, input: `C:\Synthetic`, wantErr: agenvo.ErrUnsupported},
		{name: "UNC root relative", root: `\\synthetic-host\share\root`, input: "child", want: `\\synthetic-host\share\root\child`},
		{name: "offline", input: `C:\Synthetic`, offline: true, wantErr: workspacepkg.ErrRemoteRuntimeOffline},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &agenvoToolFixture{t: t}
			currentRoot := tt.root
			if currentRoot == "" {
				currentRoot = `C:\Synthetic`
			}
			info := bridge.WorkspaceInfo{Backend: bridge.WorkspaceBackendAgenvo, OS: "windows", DefaultWorkDir: `C:\Synthetic`}
			f.info = info
			executor := agenvo.NewExecutor(agenvo.NewClient(func(_ context.Context, scope agenvo.Scope) (agenvo.Target, error) {
				if scope != (agenvo.Scope{BotID: "bot-a", TargetID: "mount-a", UserID: "caller"}) {
					return agenvo.Target{}, denied
				}
				return agenvo.Target{ConnectionID: "synthetic-connection", DeviceID: "synthetic-device", InstanceID: "synthetic-instance", Session: "synthetic-session", WorkDir: currentRoot, BashPath: `C:\Synthetic\Git\bin\bash.exe`}, nil
			}, f.call))
			f.resolved = workspacepkg.ResolvedWorkspaceTarget{TargetID: "mount-a", Kind: "remote", Client: bridge.UnsupportedClient(), Info: info, Execute: func(ctx context.Context, req agenvo.Request) (agenvo.Result, error) {
				// The workspace resolver binds these IDs to the selected authorized mount.
				req.Scope.BotID, req.Scope.TargetID = "bot-a", "mount-a"
				return executor.Execute(ctx, req)
			}}
			if tt.offline {
				f.resolveErr = workspacepkg.ErrRemoteRuntimeOffline
			}
			caller := tt.caller
			if caller == "" {
				caller = "caller"
			}
			session := SessionContext{BotID: "bot-a", UserID: caller, WorkspaceTargetID: "mount-a", WorkspaceTargetKind: "remote", WorkdirPath: tt.bound}
			provider := NewContainerProvider(nil, f, nil, "/data")
			tools, err := provider.Tools(t.Context(), session)
			if err != nil {
				t.Fatal(err)
			}
			args := map[string]any{"command": "printf 'synthetic\\n'"}
			if tt.input != "" {
				args["work_dir"] = tt.input
			}
			found := false
			for _, tool := range tools {
				if tool.Name != ToolExec().String() {
					continue
				}
				found = true
				output, execErr := tool.Execute(&toolexec.ToolExecContext{Context: t.Context()}, toolexec.ArgumentsFromValue(args))
				if tt.wantErr != nil {
					if !errors.Is(execErr, tt.wantErr) || len(f.calls) != 0 {
						t.Fatalf("error=%v calls=%v, want %v and zero dispatch", execErr, f.calls, tt.wantErr)
					}
				} else {
					var result agenvo.Result
					if execErr != nil || json.Unmarshal(output.JSON, &result) != nil || result.State != "completed" || result.ExitCode == nil || *result.ExitCode != 0 || result.Output != "synthetic cwd accepted\n" {
						t.Fatalf("output=%s error=%v", output.JSON, execErr)
					}
					if f.cwd != tt.want || strings.Join(f.calls, ",") != "workspace.list,workspace.create,pane.run,pane.read" {
						t.Fatalf("cwd=%q want=%q calls=%v", f.cwd, tt.want, f.calls)
					}
				}
			}
			if !found || f.nativeCalls != 0 {
				t.Fatalf("exec found=%v native calls=%d", found, f.nativeCalls)
			}
		})
	}
}
