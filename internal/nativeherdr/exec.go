// Package nativeherdr transports bounded official Herdr CLI calls over an
// already-authorized Memoh Runtime connection. It never starts a Herdr server.
package nativeherdr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/workspace/bridge"
	pb "github.com/felinics/memoh/internal/workspace/bridgepb"
)

const (
	maxCLIOutput        = 1024 * 1024
	maxCLIStderr        = 64 * 1024
	maxCLIInput         = 128 * 1024
	maxWindowsArguments = 24000 // UTF-16 units; leave room below CreateProcess's limit.
)

// ResolveRuntime rechecks the Bot mount, permissions, pinned runtime identity,
// revocation and current ready connection before every call. It must use the
// selected mount, never the Bot's current default or the NAS native workspace.
type ResolveRuntime func(context.Context, agenvo.Scope, agenvo.Target) (*bridge.Client, bridge.WorkspaceInfo, error)

type Executor struct {
	resolve ResolveRuntime
	timeout time.Duration
}

func NewExecutor(resolve ResolveRuntime) *Executor {
	return &Executor{resolve: resolve, timeout: 30 * time.Second}
}

// Invoke makes one CLI invocation, with no replay, including after cancellation
// or reconnect. Only complete output plus an EXIT frame and clean EOF establish
// a CLI receipt. That receipt is not evidence that an agent task has completed.
func (e *Executor) Invoke(ctx context.Context, scope agenvo.Scope, target agenvo.Target, method string, params map[string]any) (agenvo.Receipt, error) {
	if e == nil || e.resolve == nil || scope.BotID == "" || scope.TargetID == "" || target.NativeRuntimeID == "" || !validWindowsPath(target.HerdrCLI) || !strings.HasSuffix(strings.ToLower(target.HerdrCLI), ".exe") || !validHerdrRoot(target.HerdrConfigRoot) {
		return agenvo.Receipt{}, agenvo.ErrInvalidTarget
	}
	argv, err := buildCommand(method, params, target)
	if err != nil {
		return agenvo.Receipt{}, err
	}
	input, err := cliInput(target, argv)
	if err != nil {
		return agenvo.Receipt{}, err
	}
	client, info, err := e.resolve(ctx, scope, target)
	if err != nil {
		return agenvo.Receipt{}, err
	}
	if client == nil || info.Backend != bridge.WorkspaceBackendRemote || (info.OS != "win32" && info.OS != "windows") {
		return agenvo.Receipt{}, agenvo.ErrUnsupported
	}
	if ctx.Err() != nil {
		return agenvo.Receipt{}, agenvo.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	// Runtime itself inherits only its OS environment allowlist. Preserve those
	// OS roots for PowerShell; clean_env would remove SystemRoot and USERPROFILE.
	// No caller-controlled environment or executable lookup enters this call.
	stream, err := client.ExecStreamWithOptions(ctx, encodedPowerShell(), target.WorkDir, 30, bridge.ExecOptions{
		UnsetEnv: []string{"HERDR_*", "MEMOH_*", "PI_*", "MAGPIE_*", "OPENAI_*", "ANTHROPIC_*", "AZURE_*", "AWS_*", "GOOGLE_*", "GEMINI_*", "NODE_OPTIONS", "BASH_ENV", "ENV"},
	})
	if err != nil {
		return agenvo.Receipt{}, agenvo.ErrUnknown
	}
	defer func() { _ = stream.Close() }()
	if err := stream.SendStdin(input); err != nil && !errors.Is(err, io.EOF) {
		return agenvo.Receipt{}, agenvo.ErrUnknown
	}
	if err := stream.CloseSend(); err != nil && !errors.Is(err, io.EOF) {
		return agenvo.Receipt{}, agenvo.ErrUnknown
	}
	stdout, stderr, exit, err := collectCLI(stream)
	if err != nil {
		return agenvo.Receipt{}, err
	}
	return parseReceipt(method, target, exit, stdout, stderr)
}

type cliPayload struct {
	Executable    string `json:"executable"`
	Arguments     string `json:"arguments"`
	ConfigRoot    string `json:"config_root"`
	ConfigPath    string `json:"config_path"`
	SocketPath    string `json:"socket_path"`
	XDGConfigHome string `json:"xdg_config_home"`
	Session       string `json:"session"`
}

func cliInput(target agenvo.Target, argv []string) ([]byte, error) {
	if !validHerdrRoot(target.HerdrConfigRoot) {
		return nil, agenvo.ErrInvalidTarget
	}
	if len(argv) == 0 || len(argv) > 64 {
		return nil, agenvo.ErrUnsupported
	}
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		if strings.ContainsRune(arg, 0) || !utf8.ValidString(arg) {
			return nil, agenvo.ErrUnsupported
		}
		quoted[i] = quoteWindowsArg(arg)
	}
	args := strings.Join(quoted, " ")
	if len(utf16.Encode([]rune(args))) > maxWindowsArguments {
		return nil, agenvo.ErrUnsupported
	}
	root := strings.TrimRight(strings.ReplaceAll(target.HerdrConfigRoot, "/", "\\"), "\\")
	parent := root[:strings.LastIndex(root, "\\")]
	if len(parent) == 2 {
		parent += "\\"
	}
	socket := root + `\herdr.sock`
	if target.Session != "default" {
		socket = root + `\sessions\` + target.Session + `\herdr.sock`
	}
	input, err := json.Marshal(cliPayload{
		Executable: target.HerdrCLI, Arguments: args, ConfigRoot: root,
		ConfigPath: root + `\config.toml`, SocketPath: socket, XDGConfigHome: parent, Session: target.Session,
	})
	if err != nil || len(input) > maxCLIInput {
		return nil, agenvo.ErrUnsupported
	}
	return input, nil
}

// quoteWindowsArg follows the CommandLineToArgvW/CRT backslash-and-quote rules.
// It is used only in ProcessStartInfo with UseShellExecute=false and an absolute
// .exe. It must not be reused for cmd.exe, .bat, .cmd, or PowerShell source.
func quoteWindowsArg(arg string) string {
	var out strings.Builder
	out.WriteByte('"')
	backslashes := 0
	for _, ch := range arg {
		if ch == '\\' {
			backslashes++
			continue
		}
		if ch == '"' {
			out.WriteString(strings.Repeat("\\", backslashes*2+1))
		} else {
			out.WriteString(strings.Repeat("\\", backslashes))
		}
		backslashes = 0
		out.WriteRune(ch)
	}
	out.WriteString(strings.Repeat("\\", backslashes*2))
	out.WriteByte('"')
	return out.String()
}

func validWindowsPath(path string) bool {
	if len(path) < 3 || len(path) > 4096 || !utf8.ValidString(path) || ((path[0] < 'A' || path[0] > 'Z') && (path[0] < 'a' || path[0] > 'z')) || path[1] != ':' || (path[2] != '\\' && path[2] != '/') {
		return false
	}
	for _, ch := range path[2:] {
		if ch < 32 || strings.ContainsRune(`<>:"|?*`, ch) {
			return false
		}
	}
	return true
}

func validHerdrRoot(path string) bool {
	if !validWindowsPath(path) {
		return false
	}
	root := strings.TrimRight(strings.ReplaceAll(path, "/", "\\"), "\\")
	// The release CLI derives config_dir as XDG_CONFIG_HOME/herdr regardless
	// of HERDR_CONFIG_PATH. Require the same directory for plugins and sockets.
	return strings.EqualFold(root[strings.LastIndex(root, "\\")+1:], "herdr")
}

type outputReceiver interface {
	Recv() (*pb.ExecOutput, error)
}

func collectCLI(stream outputReceiver) ([]byte, []byte, int, error) {
	var stdout, stderr bytes.Buffer
	exit, sawExit := 0, false
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			if !sawExit {
				return nil, nil, 0, agenvo.ErrUnknown
			}
			return stdout.Bytes(), stderr.Bytes(), exit, nil
		}
		if err != nil || msg == nil || sawExit {
			return nil, nil, 0, agenvo.ErrUnknown
		}
		switch msg.GetStream() {
		case pb.ExecOutput_STDOUT:
			if len(msg.GetData()) > maxCLIOutput-stdout.Len() {
				return nil, nil, 0, agenvo.ErrUnknown
			}
			stdout.Write(msg.GetData())
		case pb.ExecOutput_STDERR:
			if len(msg.GetData()) > maxCLIStderr-stderr.Len() {
				return nil, nil, 0, agenvo.ErrUnknown
			}
			stderr.Write(msg.GetData())
		case pb.ExecOutput_EXIT:
			if len(msg.GetData()) != 0 {
				return nil, nil, 0, agenvo.ErrUnknown
			}
			exit, sawExit = int(msg.GetExitCode()), true
		default:
			return nil, nil, 0, agenvo.ErrUnknown
		}
	}
}

func encodedPowerShell() string {
	units := utf16.Encode([]rune(cliPowerShell))
	raw := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.LittleEndian.PutUint16(raw[i*2:], unit)
	}
	return `"%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe" -NoLogo -NoProfile -NonInteractive -EncodedCommand ` + base64.StdEncoding.EncodeToString(raw)
}

// The script is constant. Prompt, command arguments and deployment paths enter
// only as stdin JSON, never as PowerShell or cmd source. Raw stdout/stderr are
// copied concurrently to avoid pipe deadlock and preserve Herdr's UTF-8 output.
// The Herdr service must already run independently: Runtime owns this entire
// short-lived child process tree and terminates it on timeout or disconnect.
const cliPowerShell = `$ErrorActionPreference='Stop'
try {
  [Console]::InputEncoding=[Text.UTF8Encoding]::new($false)
  $p=[Console]::In.ReadToEnd() | Microsoft.PowerShell.Utility\ConvertFrom-Json
  $s=[Diagnostics.ProcessStartInfo]::new()
  $s.FileName=[string]$p.executable
  $s.Arguments=[string]$p.arguments
  $s.UseShellExecute=$false
  $s.CreateNoWindow=$true
  $s.RedirectStandardOutput=$true
  $s.RedirectStandardError=$true
  $s.RedirectStandardInput=$true
  $s.EnvironmentVariables['HERDR_CONFIG_PATH']=[string]$p.config_path
  $s.EnvironmentVariables['HERDR_SOCKET_PATH']=[string]$p.socket_path
  $s.EnvironmentVariables['XDG_CONFIG_HOME']=[string]$p.xdg_config_home
  if ($p.session -eq 'default') {
    $s.EnvironmentVariables.Remove('HERDR_SESSION')
  } else {
    $s.EnvironmentVariables['HERDR_SESSION']=[string]$p.session
  }
  $s.EnvironmentVariables['NO_COLOR']='1'
  $c=[Diagnostics.Process]::new()
  $c.StartInfo=$s
  [void]$c.Start()
  $c.StandardInput.Close()
  $o=$c.StandardOutput.BaseStream.CopyToAsync([Console]::OpenStandardOutput())
  $e=$c.StandardError.BaseStream.CopyToAsync([Console]::OpenStandardError())
  $c.WaitForExit()
  [void]$o.GetAwaiter().GetResult()
  [void]$e.GetAwaiter().GetResult()
  exit $c.ExitCode
} catch {
  [Console]::Error.WriteLine('Herdr CLI transport failed')
  exit 125
}`
