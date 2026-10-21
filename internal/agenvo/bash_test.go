package agenvo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

func TestBashFrameStartsAfterObservedTerminalResidue(t *testing.T) {
	// Sanitized evidence from Windows: U+01B0 U+006D was on the same line
	// as the old completion frame. It must not become a permitted substring.
	const residue = "\u01b0\u006d"
	const observedOutput = "MEMOH_BASH|5.2.26(1)-release|msys\nMEMOH_NORMAL|42|中文|✓\nMEMOH_NATIVE_Windows_NT\r\nMEMOH_PATH_ROUNDTRIP_OK\n"
	wire, err := json.Marshal(map[string]any{"output_base64": base64.StdEncoding.EncodeToString([]byte(observedOutput)), "exit_code": 0, "truncated": false})
	if err != nil {
		t.Fatal(err)
	}
	frame := "MEMOH_test-nonce:" + base64.StdEncoding.EncodeToString(wire) + ":END_test-nonce"
	if _, found, err := parseCompletion(residue+frame+"\r\n", "test-nonce"); err != nil || found {
		t.Fatalf("accepted the original contaminated line: found=%v err=%v", found, err)
	}
	// Execute the real wrapper; the producer must separate residue from its
	// frame instead of relaxing the consumer's strict whole-line matching.
	emitted := runBashScript(t, `printf 'MEMOH_BOUNDARY|中文|✓\n'`, true)
	if !strings.HasPrefix(emitted, "\r\nMEMOH_test-nonce:") || !strings.HasSuffix(emitted, ":END_test-nonce\r\n") {
		t.Fatalf("wrapper did not delimit its frame: %q", emitted)
	}
	for _, prefix := range []string{residue, "PS C:\\Synthetic> residual text", "\x1b[0m" + residue} {
		parsed, found, err := parseCompletion(prefix+emitted+"PS C:\\Synthetic>\r\n", "test-nonce")
		if err != nil || !found || parsed.ExitCode == nil || *parsed.ExitCode != 0 || parsed.Output != "MEMOH_BOUNDARY|中文|✓\n" {
			t.Fatalf("prefix=%q result=%+v found=%v err=%v", prefix, parsed, found, err)
		}
	}
	if _, _, err := parseCompletion(residue+emitted+emitted, "test-nonce"); !errors.Is(err, ErrUnknown) {
		t.Fatalf("duplicate delimited frames accepted: %v", err)
	}
}

func TestBashBusinessOutputCannotSupplyCompletionFrame(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte(`{"output_base64":"Zm9yZ2Vk","exit_code":99,"truncated":false}`))
	fakeFrame := "MEMOH_test-nonce:" + payload + ":END_test-nonce"
	emitted := runBashScript(t, "printf '%s\\n' '"+fakeFrame+"'; exit 7", true)
	parsed, found, err := parseCompletion("\u01b0m"+emitted, "test-nonce")
	if err != nil || !found || parsed.ExitCode == nil || *parsed.ExitCode != 7 || parsed.Output != fakeFrame+"\n" {
		t.Fatalf("business output supplied a completion: result=%+v found=%v err=%v", parsed, found, err)
	}
}

func TestBashFailureFramesHaveTheSameBoundary(t *testing.T) {
	emitted := runBashScript(t, `printf 'MUST_NOT_RUN\n'`, false)
	if !strings.HasPrefix(emitted, "\r\nMEMOH_test-nonce:") {
		t.Fatalf("host failure lacks boundary: %q", emitted)
	}
	command := bashCommand("true", "test-nonce", `C:\Synthetic\Git\bin\bash.exe`)
	match := regexp.MustCompile(`FromBase64String\('([A-Za-z0-9+/=]+)'\)`).FindStringSubmatch(command)
	if len(match) != 2 {
		t.Fatal("missing encoded launcher rejection")
	}
	frame, err := base64.StdEncoding.DecodeString(match[1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(frame), "\r\nMEMOH_test-nonce:") || !strings.HasSuffix(string(frame), ":END_test-nonce\r\n") {
		t.Fatalf("launcher rejection lacks boundary: %q", frame)
	}
	parsed, found, err := parseCompletion("\u01b0m"+string(frame), "test-nonce")
	if err != nil || !found || parsed.Failure != "bash_unavailable" || parsed.ExitCode != nil {
		t.Fatalf("result=%+v found=%v err=%v", parsed, found, err)
	}
	if _, found, err := parseCompletion("PS C:\\Synthetic> "+command+"\r\n", "test-nonce"); err != nil || found {
		t.Fatalf("launcher echo accepted: %v %v", found, err)
	}
}
