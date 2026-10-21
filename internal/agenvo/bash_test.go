package agenvo

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// These execute the actual Bash script on Linux with a synthetic uname only.
// They verify Bash semantics and framing, not the Windows launch boundary.
func runBashScript(t *testing.T, body string, simulateWindows bool) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("Linux Bash semantics test; Windows launch acceptance is separate")
	}
	dir := t.TempDir()
	path := "/usr/bin:/bin"
	if simulateWindows {
		//nolint:gosec // This fixed synthetic executable is confined to t.TempDir().
		if err := os.WriteFile(filepath.Join(dir, "uname"), []byte("#!/bin/sh\nprintf 'MINGW64_NT-SYNTHETIC\\n'\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		path = dir + ":" + path
	}
	poison := filepath.Join(dir, "bash-env")
	if err := os.WriteFile(poison, []byte("printf 'UNEXPECTED_PROFILE_OUTPUT\\n'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	//nolint:gosec // Only fixed synthetic bodies from the test table reach this subprocess.
	cmd := exec.CommandContext(ctx, "/bin/bash", "--noprofile", "--norc", "-p", "-c", bashScript(body, "test-nonce"))
	cmd.Env = []string{"PATH=" + path, "LC_ALL=C.UTF-8", "BASH_ENV=" + poison}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Bash wrapper failed: %v, %q", err, output)
	}
	if strings.Contains(string(output), "UNEXPECTED_PROFILE_OUTPUT") {
		t.Fatal("BASH_ENV was sourced")
	}
	return string(output)
}

func TestBashScriptExecutionSemantics(t *testing.T) {
	for _, test := range []struct {
		name, body, output string
		exit               int
		truncated          bool
	}{
		{name: "normal_unicode", body: `printf 'MEMOH_NORMAL|%s|中文|✓\n' "$((6 * 7))"`, output: "MEMOH_NORMAL|42|中文|✓\n"},
		{name: "stderr_does_not_abort", body: `printf 'MEMOH_STDERR\n' >&2; printf 'MEMOH_AFTER\n'`, output: "MEMOH_STDERR\nMEMOH_AFTER\n"},
		{name: "nonzero", body: `printf 'MEMOH_NONZERO\n'; exit 7`, output: "MEMOH_NONZERO\n", exit: 7},
		{name: "drains_excess_without_sigpipe", body: `printf '%1000000s' ''`, output: strings.Repeat(" ", 4096), truncated: true},
		{name: "incomplete_utf8_suffix", body: `printf '%4095s' ''; printf '中文'`, output: strings.Repeat(" ", 4095), truncated: true},
		{name: "syntax_error_is_nonzero", body: `if`, exit: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := runBashScript(t, test.body, true)
			result, found, err := parseCompletion(output, "test-nonce")
			if err != nil || !found || result.ExitCode == nil || *result.ExitCode != test.exit || result.Truncated != test.truncated || result.Failure != "" {
				t.Fatalf("result=%+v found=%v err=%v snapshot=%q", result, found, err, output)
			}
			if test.name == "syntax_error_is_nonzero" {
				if !strings.Contains(result.Output, "syntax error") {
					t.Fatalf("missing diagnostic: %q", result.Output)
				}
			} else if result.Output != test.output {
				t.Fatalf("output=%q want=%q", result.Output, test.output)
			}
		})
	}
}

func TestBashHostGuardRejectsLinuxBeforeUserCommand(t *testing.T) {
	output := runBashScript(t, `printf 'MUST_NOT_RUN\n'`, false)
	result, found, err := parseCompletion(output, "test-nonce")
	if err != nil || !found || result.Failure != "unsupported_bash_host" || result.ExitCode != nil || strings.Contains(output, "MUST_NOT_RUN") {
		t.Fatalf("result=%+v found=%v err=%v", result, found, err)
	}
}

func TestBashLauncherQuotesOneExplicitPathAndDoesNotExitPane(t *testing.T) {
	path := `C:\Synthetic Owner's Git\bin\bash.exe`
	command := bashCommand(`printf 'literal $value; ✓\n'`, "nonce", path)
	if !strings.HasSuffix(command, `'C:\Synthetic Owner''s Git\bin\bash.exe'`) || strings.Contains(command, "exit") || strings.Contains(command, "MEMOH_nonce:") || strings.Contains(command, "powershell.exe") {
		t.Fatalf("unsafe or unexpected launcher: %s", command)
	}
	if _, found, err := parseCompletion("PS C:\\synthetic> "+command+"\r\n", "nonce"); err != nil || found {
		t.Fatalf("launcher echo completed command: %v %v", found, err)
	}
}

func TestBashCompletionRejectsInvalidOutputAndExit(t *testing.T) {
	for _, payload := range []string{
		`{"output_base64":"%%%","exit_code":0}`,
		`{"output_base64":"/w==","exit_code":0,"truncated":true}`,
		`{"output_base64":"AA==","exit_code":0}`,
		`{"output_base64":"","exit_code":256}`,
		`{"output_base64":"","exit_code":null}`,
		`{"failure":"unrecognized_failure"}`,
		`{"failure":"bash_unavailable","exit_code":0}`,
	} {
		frame := "MEMOH_n:" + base64.StdEncoding.EncodeToString([]byte(payload)) + ":END_n"
		if _, _, err := parseCompletion(frame, "n"); !errors.Is(err, ErrUnknown) {
			t.Fatalf("accepted %s: %v", payload, err)
		}
	}
}
