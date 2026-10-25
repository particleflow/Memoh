package nativeherdr

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/felinics/memoh/internal/agenvo"
)

//go:embed testdata/herdr-v0.9.5.json
var powerShellOfficialProbe []byte

func powerShellChildOutput(t *testing.T, mode string) (stdout, stderr []byte, exit int) {
	t.Helper()
	var fixture struct {
		Observations []struct {
			Method string `json:"method"`
			Stdout string `json:"stdout"`
		} `json:"observations"`
	}
	if err := json.Unmarshal(powerShellOfficialProbe, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, observation := range fixture.Observations {
		if observation.Method == "workspace.create" {
			// Preserve the captured official envelope and IDs; only its synthetic
			// label changes to exercise raw UTF-8 forwarding without transcoding.
			stdout = []byte(strings.ReplaceAll(observation.Stdout, `"label":"memoh-contract"`, `"label":"synthetic 中文🙂"`))
			break
		}
	}
	if len(stdout) == 0 {
		t.Fatal("official workspace.create receipt is missing")
	}
	switch mode {
	case "large-stderr":
		stderr = bytes.Repeat([]byte("synthetic diagnostic: é 中文\r\n"), 32768) // Exceeds OS pipe capacity; no real CLI involved.
	case "nonzero-exit":
		stdout = []byte("{\"session\":\"synthetic\",\"ok\":false,\"error\":\"synthetic rejection\"}\r\n")
		stderr = []byte("synthetic diagnostic: é 中文\r\n")
		exit = 23
	}
	return stdout, stderr, exit
}

// This test binary is the child executable. os.Exit keeps the Go test runner's
// own PASS text out of the deliberately pinned CLI stdout and stderr.
func TestPowerShellChildFixture(t *testing.T) {
	if os.Getenv("MEMOH_POWERSHELL_TEST_CHILD") != "1" {
		return
	}
	powerShellChildTrace("child-enter")
	stdout, stderr, code := powerShellChildOutput(t, os.Getenv("MEMOH_POWERSHELL_TEST_MODE"))
	powerShellChildTrace("child-output-ready")
	drained := make(chan error, 1)
	go func() {
		_, err := os.Stderr.Write(stderr)
		powerShellChildTrace(fmt.Sprintf("child-stderr-written error=%t", err != nil))
		drained <- err
	}()
	_, outErr := os.Stdout.Write(stdout)
	powerShellChildTrace(fmt.Sprintf("child-stdout-written error=%t", outErr != nil))
	if err := <-drained; err != nil || outErr != nil {
		powerShellChildTrace("child-exit=97")
		os.Exit(97)
	}
	powerShellChildTrace(fmt.Sprintf("child-exit=%d", code))
	os.Exit(code)
}

// Only the isolated fixture supplies this path. Trace markers stay out of the
// child's byte streams and contain no deployment paths, arguments or secrets.
func powerShellChildTrace(stage string) {
	path := os.Getenv("MEMOH_POWERSHELL_CHILD_TRACE")
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // Explicit synthetic fixture trace, never used by production.
	if err != nil {
		return
	}
	_, _ = fmt.Fprintln(f, stage) //nolint:gosec // Fixed synthetic stage markers in a private test file, not HTML.
	_ = f.Close()
}

func powerShellOutputDiagnostic(raw []byte) string {
	return fmt.Sprintf("bytes=%d sha256=%x prefix=%q", len(raw), sha256.Sum256(raw), raw[:min(len(raw), 256)])
}

func logPowerShellFixtureTrace(t *testing.T, name, path string) {
	t.Helper()
	raw, err := os.ReadFile(path) //nolint:gosec // Test-owned temporary trace path.
	if err != nil {
		t.Logf("%s: trace unavailable (%T)", name, err)
		return
	}
	t.Logf("%s: %s", name, raw[:min(len(raw), 8192)])
}

// A failed production run gets one diagnostic replay of the SYNTHETIC child.
// The passing assertion always uses the untouched production script. Markers
// distinguish stdin, launch and individual pipe waits on Windows .NET Framework;
// a passing diagnostic replay cannot turn the original failure into a pass.
func diagnosePowerShellFixture(t *testing.T, shell string, env []string, payload []byte, root string) {
	t.Helper()
	trace := filepath.Join(root, "powershell-stages.txt")
	childTrace := filepath.Join(root, "diagnostic-child-stages.txt")
	script := "function Write-FixtureStage([string]$stage) { [IO.File]::AppendAllText($env:MEMOH_POWERSHELL_TRACE, $stage+[Environment]::NewLine) }\nWrite-FixtureStage 'script-enter'\n" + cliPowerShell
	for _, marker := range []struct{ line, stage string }{
		{"[Console]::InputEncoding=[Text.UTF8Encoding]::new($false)", "input-encoding-set"},
		{"$p=[Console]::In.ReadToEnd() | ConvertFrom-Json", "input-parsed"},
		{"$c.StartInfo=$s", "child-configured"},
		{"[void]$c.Start()", "child-started"},
		{"$c.StandardInput.Close()", "child-stdin-closed"},
		{"$o=$c.StandardOutput.BaseStream.CopyToAsync([Console]::OpenStandardOutput())", "stdout-copy-returned"},
		{"$e=$c.StandardError.BaseStream.CopyToAsync([Console]::OpenStandardError())", "stderr-copy-returned"},
		{"$c.WaitForExit()", "child-wait-returned"},
		{"[void]$o.GetAwaiter().GetResult()", "stdout-copy-completed"},
		{"[void]$e.GetAwaiter().GetResult()", "stderr-copy-completed"},
	} {
		if strings.Count(script, marker.line) != 1 {
			t.Fatalf("diagnostic anchor changed: %s", marker.stage)
		}
		script = strings.Replace(script, marker.line, marker.line+"\n  Write-FixtureStage '"+marker.stage+"'", 1)
	}
	units := utf16.Encode([]rune(script))
	raw := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.LittleEndian.PutUint16(raw[i*2:], unit)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(raw)) //nolint:gosec // Fixed production script with synthetic phase markers; no live Herdr command.
	cmd.Env = append(append([]string(nil), env...), "MEMOH_POWERSHELL_TRACE="+trace, "MEMOH_POWERSHELL_CHILD_TRACE="+childTrace)
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	t.Logf("diagnostic replay: process_error=%v context_error=%v stdout={%s} stderr={%s}", err, ctx.Err(), powerShellOutputDiagnostic(stdout.Bytes()), powerShellOutputDiagnostic(stderr.Bytes()))
	logPowerShellFixtureTrace(t, "PowerShell phases", trace)
	logPowerShellFixtureTrace(t, "diagnostic child phases", childTrace)
}

// Runs the production encoded cliPowerShell in an actual PowerShell process.
// Linux verifies its stream-copy contract only. Windows runs the exact native
// Windows PowerShell 5.1 executable used by the production Runtime command.
func TestPowerShellCLITransportPreservesChildBytesAndExit(t *testing.T) {
	var shell string
	switch runtime.GOOS {
	case "windows":
		root := os.Getenv("SystemRoot")
		if root == "" {
			t.Fatal("SystemRoot is required to test the production Windows PowerShell")
		}
		shell = filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	case "linux":
		shell = os.Getenv("MEMOH_TEST_PWSH")
		if shell == "" {
			t.Skip("set MEMOH_TEST_PWSH to an explicit isolated PowerShell executable")
		}
	default:
		t.Skip("requires Windows PowerShell 5.1 or explicitly selected Linux PowerShell")
	}
	if !filepath.IsAbs(shell) {
		t.Fatal("PowerShell executable must be an absolute path")
	}
	if _, err := os.Stat(shell); err != nil { //nolint:gosec // Explicit local test executable; absolute path checked above.
		t.Fatal(err)
	}
	child, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	env := []string{
		"HOME=" + home, "USERPROFILE=" + home, "APPDATA=" + home, "LOCALAPPDATA=" + home,
		"XDG_CONFIG_HOME=" + home, "XDG_CACHE_HOME=" + home,
		"POWERSHELL_TELEMETRY_OPTOUT=1", "POWERSHELL_UPDATECHECK=Off",
		"MEMOH_POWERSHELL_TEST_CHILD=1",
	}
	for _, key := range []string{"SystemRoot", "WINDIR", "PATH", "PATHEXT", "TEMP", "TMP"} {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	if runtime.GOOS == "windows" {
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		version := exec.CommandContext(ctx, shell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", "$PSVersionTable.PSVersion.ToString()") //nolint:gosec // Exact SystemRoot Windows PowerShell path, fixed command.
		version.Env = env
		out, err := version.Output()
		if err != nil || !strings.HasPrefix(strings.TrimSpace(string(out)), "5.1.") {
			t.Fatalf("expected production Windows PowerShell 5.1, got %q: %v", out, err)
		}
	}
	// Linux needs a local helper path, so only the payload's deployment paths
	// are synthetic. The encoded production script is used without rewriting it.
	payload, err := json.Marshal(cliPayload{
		Executable: child, Arguments: quoteWindowsArg("-test.run=^TestPowerShellChildFixture$"),
		ConfigRoot: home, ConfigPath: filepath.Join(home, "config.toml"),
		SocketPath: filepath.Join(home, "herdr.sock"), XDGConfigHome: home, Session: "synthetic",
	})
	if err != nil {
		t.Fatal(err)
	}
	bootstrap := encodedPowerShell()
	encoded := bootstrap[strings.LastIndex(bootstrap, " ")+1:]
	diagnosed := false
	for _, mode := range []string{"pinned-json", "large-stderr", "nonzero-exit"} {
		t.Run(mode, func(t *testing.T) {
			wantOut, wantErr, wantExit := powerShellChildOutput(t, mode)
			modeEnv := append(append([]string(nil), env...), "MEMOH_POWERSHELL_TEST_MODE="+mode)
			// Establish that the same Go fixture works under the isolated Windows
			// environment before attributing a timeout to PowerShell pipe handling.
			directCtx, directCancel := context.WithTimeout(t.Context(), 20*time.Second)
			direct := exec.CommandContext(directCtx, child, "-test.run=^TestPowerShellChildFixture$") //nolint:gosec // This test executable only, fixed child selector.
			direct.Env = modeEnv
			var directOut, directErr bytes.Buffer
			direct.Stdout, direct.Stderr = &directOut, &directErr
			directRunErr := direct.Run()
			directContextErr := directCtx.Err()
			directCancel()
			if directContextErr != nil || direct.ProcessState == nil || direct.ProcessState.ExitCode() != wantExit || !bytes.Equal(directOut.Bytes(), wantOut) || !bytes.Equal(directErr.Bytes(), wantErr) {
				t.Fatalf("direct synthetic child baseline failed: process_error=%v context_error=%v stdout={%s} stderr={%s}", directRunErr, directContextErr, powerShellOutputDiagnostic(directOut.Bytes()), powerShellOutputDiagnostic(directErr.Bytes()))
			}
			t.Logf("direct synthetic child baseline passed: stdout=%d stderr=%d exit=%d", directOut.Len(), directErr.Len(), wantExit)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, shell, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encoded) //nolint:gosec // Explicit local shell; encoded script is a production constant.
			childTrace := filepath.Join(t.TempDir(), "production-child-stages.txt")
			cmd.Env = append(append([]string(nil), modeEnv...), "MEMOH_POWERSHELL_CHILD_TRACE="+childTrace)
			cmd.Stdin = bytes.NewReader(payload)
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			if ctx.Err() != nil {
				t.Logf("production timeout: process_error=%v stdout={%s} stderr={%s}", err, powerShellOutputDiagnostic(stdout.Bytes()), powerShellOutputDiagnostic(stderr.Bytes()))
				logPowerShellFixtureTrace(t, "production child phases", childTrace)
				if !diagnosed {
					diagnosed = true
					diagnosePowerShellFixture(t, shell, modeEnv, payload, t.TempDir())
				}
				t.Fatal("PowerShell did not drain concurrent child output before timeout", ctx.Err())
			}
			gotExit := 0
			if err != nil {
				var exited *exec.ExitError
				if !errors.As(err, &exited) {
					t.Fatal(err)
				}
				gotExit = exited.ExitCode()
			}
			if !bytes.Equal(stdout.Bytes(), wantOut) {
				t.Errorf("stdout changed: got %q, want exact child JSON %q", stdout.Bytes(), wantOut)
			}
			if !bytes.Equal(stderr.Bytes(), wantErr) {
				t.Errorf("stderr changed: got %d bytes, want %d exact child bytes", stderr.Len(), len(wantErr))
			}
			if gotExit != wantExit {
				t.Errorf("child exit lost: got %d, want %d", gotExit, wantExit)
			}
			if mode == "pinned-json" {
				receipt, err := parseReceipt("workspace.create", agenvo.Target{Session: "synthetic"}, gotExit, stdout.Bytes(), stderr.Bytes())
				if err != nil || receipt.Execution != "accepted" {
					t.Errorf("official create receipt lost across PowerShell and parser: execution=%s err=%v", receipt.Execution, err)
				}
			}
		})
	}
}
