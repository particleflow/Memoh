package nativeherdr

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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
	stdout, stderr, code := powerShellChildOutput(t, os.Getenv("MEMOH_POWERSHELL_TEST_MODE"))
	drained := make(chan error, 1)
	go func() {
		_, err := os.Stderr.Write(stderr)
		drained <- err
	}()
	_, outErr := os.Stdout.Write(stdout)
	if err := <-drained; err != nil || outErr != nil {
		os.Exit(97)
	}
	os.Exit(code)
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
	for _, mode := range []string{"pinned-json", "large-stderr", "nonzero-exit"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, shell, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encoded) //nolint:gosec // Explicit local shell; encoded script is a production constant.
			cmd.Env = append(append([]string(nil), env...), "MEMOH_POWERSHELL_TEST_MODE="+mode)
			cmd.Stdin = bytes.NewReader(payload)
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			if ctx.Err() != nil {
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
			wantOut, wantErr, wantExit := powerShellChildOutput(t, mode)
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
