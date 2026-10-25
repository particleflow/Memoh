package nativeherdr

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/workspace/bridge"
	pb "github.com/felinics/memoh/internal/workspace/bridgepb"
)

func syntheticTarget() agenvo.Target {
	return agenvo.Target{
		NativeRuntimeID: "synthetic-runtime", HerdrCLI: `C:\Program Files\Herdr\herdr.exe`,
		HerdrConfigRoot: `C:\Synthetic User\Herdr`, WorkDir: `C:\Synthetic`, Session: "synthetic-session",
	}
}

func TestWindowsArgumentsRemainData(t *testing.T) {
	tests := []struct{ input, quoted string }{
		{"", `""`},
		{"plain", `"plain"`},
		{"two words", `"two words"`},
		{`a"b`, `"a\"b"`},
		{`C:\path\`, `"C:\path\\"`},
		{`a\"b`, `"a\\\"b"`},
		{"中文🙂\n& %PATH% $(whoami)", "\"中文🙂\n& %PATH% $(whoami)\""},
	}
	for _, tt := range tests {
		if got := quoteWindowsArg(tt.input); got != tt.quoted {
			t.Errorf("quote %q: got %q want %q", tt.input, got, tt.quoted)
		}
	}
	secret := "PRIVATE prompt; & calc.exe %PATH% `$(exit)\n中文🙂"
	input, err := cliInput(syntheticTarget(), []string{"pane", "run", "p1", secret})
	if err != nil {
		t.Fatal(err)
	}
	var payload cliPayload
	if json.Unmarshal(input, &payload) != nil || payload.Executable != syntheticTarget().HerdrCLI || !strings.Contains(payload.Arguments, secret) {
		t.Fatal("stdin payload did not preserve data")
	}
	command := encodedPowerShell()
	if len(command) >= 8191 || strings.Contains(command, secret) || strings.Contains(command, syntheticTarget().HerdrCLI) {
		t.Fatal("cmd bootstrap contains request data or exceeds cmd.exe limit")
	}
	encoded := command[strings.LastIndex(command, " ")+1:]
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw)%2 != 0 {
		t.Fatal("invalid UTF-16LE bootstrap")
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(raw[i*2:])
	}
	if string(utf16.Decode(units)) != cliPowerShell {
		t.Fatal("encoded script changed")
	}
}

func TestCLIInputRejectsUnrepresentableArguments(t *testing.T) {
	for _, argv := range [][]string{nil, {"a\x00b"}, {string([]byte{0xff})}, {strings.Repeat("🙂", maxWindowsArguments)}} {
		if _, err := cliInput(syntheticTarget(), argv); !errors.Is(err, agenvo.ErrUnsupported) {
			t.Errorf("invalid arguments accepted: %v", err)
		}
	}
}

func TestCLIInputPinsConfigSocketAndSession(t *testing.T) {
	for _, session := range []string{"default", "synthetic-session"} {
		target := syntheticTarget()
		target.Session = session
		input, err := cliInput(target, []string{"workspace", "list"})
		if err != nil {
			t.Fatal(err)
		}
		var payload cliPayload
		if err := json.Unmarshal(input, &payload); err != nil {
			t.Fatal(err)
		}
		wantSocket := target.HerdrConfigRoot + `\herdr.sock`
		if session != "default" {
			wantSocket = target.HerdrConfigRoot + `\sessions\` + session + `\herdr.sock`
		}
		if payload.Session != session || payload.SocketPath != wantSocket || payload.ConfigPath != target.HerdrConfigRoot+`\config.toml` || payload.XDGConfigHome != `C:\Synthetic User` {
			t.Fatalf("default/foreign configuration could be selected: %+v", payload)
		}
	}
	for _, root := range []string{"herdr", `C:\WrongRoot`, `C:\herdr\..`, `C:\`} {
		if validHerdrRoot(root) {
			t.Errorf("accepted incompatible release config root: %q", root)
		}
	}
	for _, root := range []string{`C:\herdr`, `D:/Synthetic/herdr/`, `C:\Synthetic User\Herdr`} {
		if !validHerdrRoot(root) {
			t.Errorf("rejected valid release config root: %q", root)
		}
	}
}

type syntheticOutput struct {
	frames []*pb.ExecOutput
	err    error
}

func (s *syntheticOutput) Recv() (*pb.ExecOutput, error) {
	if len(s.frames) == 0 {
		if s.err != nil {
			return nil, s.err
		}
		return nil, io.EOF
	}
	frame := s.frames[0]
	s.frames = s.frames[1:]
	return frame, nil
}

func out(stream pb.ExecOutput_Stream, value string) *pb.ExecOutput {
	return &pb.ExecOutput{Stream: stream, Data: []byte(value)}
}

func exited(code int32) *pb.ExecOutput {
	return &pb.ExecOutput{Stream: pb.ExecOutput_EXIT, ExitCode: code}
}

func TestCollectCLIRequiresCompleteBoundedOutput(t *testing.T) {
	tests := []struct {
		name   string
		frames []*pb.ExecOutput
		err    error
	}{
		{"missing exit", []*pb.ExecOutput{out(pb.ExecOutput_STDOUT, "{}")}, nil},
		{"duplicate exit", []*pb.ExecOutput{exited(0), exited(0)}, nil},
		{"output after exit", []*pb.ExecOutput{exited(0), out(pb.ExecOutput_STDOUT, "{}")}, nil},
		{"exit payload", []*pb.ExecOutput{out(pb.ExecOutput_EXIT, "PRIVATE")}, nil},
		{"unknown stream", []*pb.ExecOutput{{Stream: pb.ExecOutput_Stream(99)}}, nil},
		{"large stdout", []*pb.ExecOutput{out(pb.ExecOutput_STDOUT, strings.Repeat("x", maxCLIOutput+1))}, nil},
		{"large stderr", []*pb.ExecOutput{out(pb.ExecOutput_STDERR, strings.Repeat("x", maxCLIStderr+1))}, nil},
		{"rpc lost after exit", []*pb.ExecOutput{exited(0)}, errors.New("PRIVATE connection failure")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := collectCLI(&syntheticOutput{frames: tt.frames, err: tt.err})
			if !errors.Is(err, agenvo.ErrUnknown) || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatalf("must fail unknown without private output: %v", err)
			}
		})
	}
	stdout, stderr, code, err := collectCLI(&syntheticOutput{frames: []*pb.ExecOutput{out(pb.ExecOutput_STDOUT, "one"), out(pb.ExecOutput_STDERR, "diagnostic"), out(pb.ExecOutput_STDOUT, "two"), exited(7)}})
	if err != nil || string(stdout) != "onetwo" || string(stderr) != "diagnostic" || code != 7 {
		t.Fatalf("did not collect complete nonzero exit: %q %q %d %v", stdout, stderr, code, err)
	}
}

type recordingRuntime struct {
	pb.UnimplementedContainerServiceServer
	calls   atomic.Int32
	mu      sync.Mutex
	config  *pb.ExecInput
	input   []byte
	respond func(pb.ContainerService_ExecServer) error
}

func (r *recordingRuntime) Exec(stream pb.ContainerService_ExecServer) error {
	config, err := stream.Recv()
	if err != nil {
		return err
	}
	r.calls.Add(1)
	var input []byte
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		input = append(input, msg.GetStdinData()...)
	}
	r.mu.Lock()
	r.config, r.input = config, input
	r.mu.Unlock()
	return r.respond(stream)
}

func runtimeClient(t *testing.T, runtime *recordingRuntime) *bridge.Client {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	pb.RegisterContainerServiceServer(server, runtime)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() { server.Stop(); _ = listener.Close(); <-done })
	conn, err := grpc.NewClient("passthrough://native-herdr-synthetic", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return bridge.NewClientFromConn(conn)
}

func TestInvokeUsesNativeRuntimeWithoutReplay(t *testing.T) {
	target := syntheticTarget()
	scope := agenvo.Scope{BotID: "bot", TargetID: "mount", UserID: "user"}
	runtime := &recordingRuntime{respond: func(s pb.ContainerService_ExecServer) error { return s.Send(exited(0)) }}
	client := runtimeClient(t, runtime)
	resolves := 0
	executor := NewExecutor(func(_ context.Context, gotScope agenvo.Scope, gotTarget agenvo.Target) (*bridge.Client, bridge.WorkspaceInfo, error) {
		resolves++
		if gotScope != scope || gotTarget != target {
			t.Fatal("identity changed before resolving mount")
		}
		return client, bridge.WorkspaceInfo{Backend: bridge.WorkspaceBackendRemote, OS: "win32"}, nil
	})
	receipt, err := executor.Invoke(t.Context(), scope, target, "pane.run", map[string]any{"session": target.Session, "paneId": "p1", "command": "echo synthetic & value"})
	if err != nil || receipt.Execution != "accepted" || runtime.calls.Load() != 1 || resolves != 1 {
		t.Fatalf("unexpected CLI receipt: %+v %v calls=%d resolves=%d", receipt, err, runtime.calls.Load(), resolves)
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.config.GetCommand() != encodedPowerShell() || runtime.config.GetPty() || runtime.config.GetTimeoutSeconds() != 30 || runtime.config.GetCleanEnv() || len(runtime.config.GetEnv()) != 0 || !strings.Contains(strings.Join(runtime.config.GetUnsetEnv(), ","), "MEMOH_*") || runtime.config.GetWorkDir() != target.WorkDir {
		t.Fatal("unsafe Runtime exec configuration")
	}
	var payload cliPayload
	if json.Unmarshal(runtime.input, &payload) != nil || payload.Executable != target.HerdrCLI || payload.ConfigRoot != target.HerdrConfigRoot || !strings.Contains(payload.Arguments, "echo synthetic & value") {
		t.Fatal("CLI data lost or changed")
	}
}

func TestInvokeUnknownResolvesSameRuntimeOnExplicitRead(t *testing.T) {
	target := syntheticTarget()
	scope := agenvo.Scope{BotID: "bot", TargetID: "mount", UserID: "user"}
	oldRuntime := &recordingRuntime{respond: func(pb.ContainerService_ExecServer) error {
		return status.Error(codes.Unavailable, "PRIVATE lost acknowledgement")
	}}
	newRuntime := &recordingRuntime{respond: func(s pb.ContainerService_ExecServer) error {
		if err := s.Send(out(pb.ExecOutput_STDOUT, `{"id":"cli:workspace:list","result":{"type":"workspace_list","workspaces":[]}}`)); err != nil {
			return err
		}
		return s.Send(exited(0))
	}}
	current := runtimeClient(t, oldRuntime)
	newClient := runtimeClient(t, newRuntime)
	executor := NewExecutor(func(_ context.Context, gotScope agenvo.Scope, gotTarget agenvo.Target) (*bridge.Client, bridge.WorkspaceInfo, error) {
		if gotScope != scope || gotTarget != target {
			t.Fatal("reconnect changed pinned identity")
		}
		return current, bridge.WorkspaceInfo{Backend: bridge.WorkspaceBackendRemote, OS: "win32"}, nil
	})
	_, err := executor.Invoke(t.Context(), scope, target, "pane.run", map[string]any{"session": target.Session, "paneId": "p1", "command": "synthetic write"})
	if !errors.Is(err, agenvo.ErrUnknown) || strings.Contains(err.Error(), "PRIVATE") || oldRuntime.calls.Load() != 1 || newRuntime.calls.Load() != 0 {
		t.Fatalf("unknown operation was replayed or exposed private cause: %v", err)
	}
	current = newClient
	receipt, err := executor.Invoke(t.Context(), scope, target, "workspace.list", map[string]any{"session": target.Session})
	if err != nil || receipt.Execution != "accepted" || oldRuntime.calls.Load() != 1 || newRuntime.calls.Load() != 1 {
		t.Fatalf("explicit read did not resolve new generation: %+v %v", receipt, err)
	}
}

func TestInvokeTimeoutIsUnknownWithoutReplay(t *testing.T) {
	runtime := &recordingRuntime{respond: func(s pb.ContainerService_ExecServer) error { <-s.Context().Done(); return s.Context().Err() }}
	client := runtimeClient(t, runtime)
	executor := NewExecutor(func(context.Context, agenvo.Scope, agenvo.Target) (*bridge.Client, bridge.WorkspaceInfo, error) {
		return client, bridge.WorkspaceInfo{Backend: bridge.WorkspaceBackendRemote, OS: "win32"}, nil
	})
	executor.timeout = 100 * time.Millisecond
	target := syntheticTarget()
	_, err := executor.Invoke(t.Context(), agenvo.Scope{BotID: "bot", TargetID: "mount"}, target, "pane.run", map[string]any{"session": target.Session, "paneId": "p1", "command": "synthetic write"})
	if !errors.Is(err, agenvo.ErrUnknown) || runtime.calls.Load() != 1 {
		t.Fatalf("timeout must remain unknown, one invocation: %v calls=%d", err, runtime.calls.Load())
	}
}

func TestInvokeRejectsInvalidTargetAndUnavailableAuthority(t *testing.T) {
	target := syntheticTarget()
	scope := agenvo.Scope{BotID: "bot", TargetID: "mount"}
	resolves := 0
	executor := NewExecutor(func(context.Context, agenvo.Scope, agenvo.Target) (*bridge.Client, bridge.WorkspaceInfo, error) {
		resolves++
		return nil, bridge.WorkspaceInfo{}, agenvo.ErrTargetChanged
	})
	for _, path := range []string{"herdr.exe", `C:\Herdr\herdr.cmd`, `C:\Herdr\herdr.bat`, `C:\Herdr\herdr.ps1`, "C:\\Herdr\\bad\n.exe"} {
		bad := target
		bad.HerdrCLI = path
		if _, err := executor.Invoke(t.Context(), scope, bad, "workspace.list", map[string]any{"session": target.Session}); !errors.Is(err, agenvo.ErrInvalidTarget) {
			t.Fatalf("invalid executable accepted: %v", err)
		}
	}
	if resolves != 0 {
		t.Fatal("invalid deployment target reached resolver")
	}
	_, err := executor.Invoke(t.Context(), scope, target, "workspace.list", map[string]any{"session": target.Session})
	if !errors.Is(err, agenvo.ErrTargetChanged) || resolves != 1 {
		t.Fatalf("authority failure lost: %v", err)
	}
}
