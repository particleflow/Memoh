package agenvo

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// This selector runs in the existing Windows native workflow, against the
// production PowerShell 5.1 boundary and its installed Git for Windows Bash.
func TestNativeBatchLauncherWindowsPowerShell(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 acceptance runs in the native Windows job")
	}
	shell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	git, err := exec.LookPath("git.exe")
	if err != nil {
		t.Fatal("Git for Windows is required by the native launch contract", err)
	}
	bash := filepath.Join(filepath.Dir(filepath.Dir(git)), "bin", "bash.exe")
	if _, err := os.Stat(bash); err != nil {
		t.Fatal("installed Git for Windows Bash unavailable", err)
	}
	want := "雪🙂 ' quoted $(literal) \" " + variedSyntheticTask(3435)
	script := "# CRLF fixture\r\nprintf '%s' '" + strings.ReplaceAll(want, "'", "'\"'\"'") + "'\n" + strings.Repeat("# stdin fixture\n", 3000)
	command := launchBatchScript(script, Target{NativeRuntimeID: "synthetic", BashPath: bash})
	cmd := exec.CommandContext(t.Context(), shell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", command) //nolint:gosec // OS PowerShell and installed Git Bash, generated synthetic script only.
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil || stdout.String() != want || stderr.Len() != 0 {
		t.Fatalf("Windows native stdin changed output: error=%v stdout_bytes=%d want_bytes=%d stderr=%q", err, stdout.Len(), len(want), stderr.String())
	}
}

func TestNativeBatchLauncherRoundTripAndLegacyCompatibility(t *testing.T) {
	script := "printf '%s' '雪🙂\" $(not-source)'\r\n" + strings.Repeat("# fixture\n", 4000)
	target := Target{BashPath: `C:\Synthetic Owner's Bin\bash.exe`}
	if got := launchBatchScript(script, target); got != launchBashScript(script, target.BashPath) {
		t.Fatal("legacy relay command changed")
	}
	target.NativeRuntimeID = "synthetic-native-runtime"
	command := launchBatchScript(script, target)
	match := regexp.MustCompile(`FromBase64String\('([A-Za-z0-9+/=]+)'\)`).FindStringSubmatch(command)
	if len(match) != 2 || !strings.Contains(command, "Arguments='--noprofile --norc -p'") || !strings.Contains(command, "StandardInput.BaseStream.Write") || strings.Contains(command, script) {
		t.Fatal("script entered argv or the fixed native launch boundary changed")
	}
	raw, err := base64.StdEncoding.DecodeString(match[1])
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(decoded) != script {
		t.Fatal("native packing changed script bytes", err)
	}
}

func TestNativeBatchLauncherPowerShellPreservesBytesBeforeExecution(t *testing.T) {
	shell := os.Getenv("MEMOH_TEST_PWSH")
	if shell == "" {
		t.Skip("set MEMOH_TEST_PWSH to an isolated PowerShell executable")
	}
	dir := t.TempDir()
	bash := filepath.Join(dir, "Bash's fixture")
	capture := filepath.Join(dir, "captured")
	// The controlled peer captures stdin before running Bash, so byte equality
	// includes CRLF/UTF-8 and proves the production launcher adds no BOM/newline.
	body := "#!/bin/sh\ncat > \"$SYNTH_CAPTURE\"\nexec /bin/bash --noprofile --norc -p \"$SYNTH_CAPTURE\"\n"
	//nolint:gosec // Fixed executable in this test's private temporary directory.
	if err := os.WriteFile(bash, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	script := "printf '%s' '雪🙂 $(literal) quoted'\r\nprintf '%s' 'stderr fixture' >&2\n" + strings.Repeat("# fixture\n", 4000)
	target := Target{NativeRuntimeID: "synthetic", BashPath: bash}
	command := launchBatchScript(script, target)
	run := func(code string) ([]byte, []byte, error) {
		cmd := exec.CommandContext(t.Context(), shell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", code) //nolint:gosec // Explicit isolated PowerShell, generated fixed script and synthetic peer.
		cmd.Env = []string{"PATH=/usr/bin:/bin", "SYNTH_CAPTURE=" + capture, "XDG_CONFIG_HOME=" + dir, "XDG_CACHE_HOME=" + dir, "POWERSHELL_TELEMETRY_OPTOUT=1", "POWERSHELL_UPDATECHECK=Off"}
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		return stdout.Bytes(), stderr.Bytes(), err
	}
	stdout, stderr, err := run(command)
	if err != nil || string(stdout) != "雪🙂 $(literal) quoted\r" || string(stderr) != "stderr fixture" {
		t.Fatalf("native launch output: stdout=%q stderr=%q error=%v", stdout, stderr, err)
	}
	got, err := os.ReadFile(capture) //nolint:gosec // Fixed capture in this test's private temporary directory.
	if err != nil || string(got) != script {
		t.Fatal("stdin script bytes changed", err)
	}
	if err := os.Remove(capture); err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`FromBase64String\('([A-Za-z0-9+/=]+)'\)`).FindStringSubmatch(command)
	raw, err := base64.StdEncoding.DecodeString(match[1])
	if err != nil {
		t.Fatal(err)
	}
	hash := regexp.MustCompile(`\$hash -ne '([a-f0-9]{64})'`).FindStringSubmatch(command)
	for name, corrupt := range map[string]string{
		"invalid-gzip":   strings.Replace(command, match[1], base64.StdEncoding.EncodeToString([]byte("not gzip")), 1),
		"truncated-gzip": strings.Replace(command, match[1], base64.StdEncoding.EncodeToString(raw[:len(raw)/2]), 1),
		"wrong-hash":     strings.Replace(command, hash[1], strings.Repeat("0", 64), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := run(corrupt); err == nil {
				t.Fatal("corrupt envelope accepted")
			}
			if _, err := os.Stat(capture); !os.IsNotExist(err) {
				t.Fatal("corrupt envelope started Bash")
			}
		})
	}
}
