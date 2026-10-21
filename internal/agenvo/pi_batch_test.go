package agenvo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func batchFixtureProgress() BatchProgress {
	return BatchProgress{Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32), RequestHash: strings.Repeat("c", 64), PiSessionID: strings.Repeat("d", 64), WorkspaceID: "w-synthetic", PaneID: "p-synthetic", TerminalID: "terminal-synthetic"}
}

func TestBatchEnvelopeUsesStdinAtomicReceiptAndExclusiveAttempt(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"uname":   "#!/bin/sh\nprintf MINGW64_NT-SYNTHETIC\n",
		"cygpath": "#!/bin/sh\ncase \"$2\" in 'C:\\Synthetic') printf '%s' \"$SYNTH_ROOT\";; *) printf '%s' \"$2\";; esac\n",
		"pi":      "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$SYNTH_ARGS\"\ncat > \"$SYNTH_INPUT\"\ncat \"$SYNTH_EVENTS\"\nprintf 'ordinary stderr 中文' >&2\nexit 7\n",
	} {
		//nolint:gosec // Fixed synthetic utilities in t.TempDir; no Windows/provider contacted.
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	p := batchFixtureProgress()
	events := filepath.Join(dir, "events")
	if err := os.WriteFile(events, []byte(piEvents(p.PiSessionID, "stop", "42 中文 ✓", false)), 0o600); err != nil {
		t.Fatal(err)
	}
	target := Target{WorkDir: `C:\Synthetic`, Agent: AgentProfile{Provider: "synthetic", Model: "synthetic-model"}}
	task := "--model other; $(touch MUST_NOT_EXIST)\n@private-file\n'中文'"
	script := batchScript(p, target, target.WorkDir, task)
	run := func() ([]byte, error) {
		//nolint:gosec // Generated fixed envelope and synthetic task only.
		cmd := exec.CommandContext(t.Context(), "/bin/bash", "--noprofile", "--norc", "-p", "-c", script)
		cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin", "SYNTH_ROOT=" + dir, "SYNTH_ARGS=" + filepath.Join(dir, "args"), "SYNTH_INPUT=" + filepath.Join(dir, "input"), "SYNTH_EVENTS=" + events}
		cmd.Dir = dir
		return cmd.CombinedOutput()
	}
	output, err := run()
	if err != nil {
		t.Fatalf("envelope: %v %s", err, output)
	}
	r, err := parseBatchReceipt(string(output), p)
	if err != nil || r == nil || !r.Started || r.ExitCode != 7 {
		t.Fatalf("receipt=%+v err=%v output=%s", r, err, output)
	}
	input := readBatchFixture(t, filepath.Join(dir, "input"))
	if string(input) != task {
		t.Fatal("task was not delivered as stdin data")
	}
	if _, err := os.Stat(filepath.Join(dir, "MUST_NOT_EXIST")); !os.IsNotExist(err) {
		t.Fatal("task became shell source")
	}
	args := readBatchFixture(t, filepath.Join(dir, "args"))
	if !strings.Contains(string(args), "--mode\njson\n--provider\nsynthetic\n") || !strings.Contains(string(args), "--session-id\n"+p.PiSessionID) || strings.Contains(string(args), "--continue") {
		t.Fatalf("wrong CLI contract: %s", args)
	}
	for _, f := range []BatchFile{r.Stdout, r.Stderr} {
		raw := readBatchFixture(t, filepath.Join(dir, ".memoh-computer", "attempts", p.Attempt, f.Name))
		sum := sha256.Sum256(raw)
		if int64(len(raw)) != f.Size || hex.EncodeToString(sum[:]) != f.Hash {
			t.Fatal("receipt file identity mismatch")
		}
	}
	stored := readBatchFixture(t, filepath.Join(dir, ".memoh-computer", "attempts", p.Attempt, "receipt.json"))
	var persisted BatchReceipt
	if json.Unmarshal(stored, &persisted) != nil || persisted.Validate(p) != nil {
		t.Fatal("atomic receipt absent")
	}
	if _, err := run(); err == nil {
		t.Fatal("duplicate attempt was executed")
	}
	if _, err := parseBatchReceipt(string(output)+string(output), p); err == nil {
		t.Fatal("duplicate receipt frame accepted")
	}
	// A stale session lock must not be stolen, and another attempt must not
	// launch Pi or overwrite its session while the owner is uncertain.
	lock := filepath.Join(dir, ".memoh-computer", "sessions", p.PiSessionID, "lock")
	if err := os.Mkdir(lock, 0o700); err != nil {
		t.Fatal(err)
	}
	next := p
	next.Attempt = strings.Repeat("f", 32)
	script = batchScript(next, target, target.WorkDir, "must not reach Pi")
	output, err = run()
	blocked, parseErr := parseBatchReceipt(string(output), next)
	if err != nil || parseErr != nil || blocked == nil || blocked.Started || blocked.ExitCode != 125 {
		t.Fatal("session lock was not respected")
	}
	if _, err := os.Stat(lock); err != nil {
		t.Fatal("stale lock removed")
	}
	input = readBatchFixture(t, filepath.Join(dir, "input"))
	if string(input) != task {
		t.Fatal("another attempt concurrently invoked Pi")
	}
	// A nonexistent working directory is a proven pre-input rejection, not a
	// forever-unknown task. It must still emit the atomic not-started receipt.
	next.Attempt = strings.Repeat("1", 32)
	script = batchScript(next, target, filepath.Join(dir, "missing-directory"), "must not start")
	output, err = run()
	missing, parseErr := parseBatchReceipt(string(output), next)
	if err != nil || parseErr != nil || missing == nil || missing.Started || missing.ExitCode != 125 {
		t.Fatal("preflight failure lost its receipt")
	}
	p.Generation = strings.Repeat("e", 32)
	if r.Validate(p) == nil {
		t.Fatal("stale attempt generation accepted")
	}
}

func readBatchFixture(t *testing.T, path string) []byte {
	t.Helper()
	//nolint:gosec // Test-owned synthetic path below t.TempDir only.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
