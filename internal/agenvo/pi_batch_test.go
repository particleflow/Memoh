package agenvo

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	for _, tc := range []struct {
		name, root          string
		version, completion int
	}{
		{"legacy", `C:\Synthetic`, 0, 0},
		{"versioned-backslashes", `C:\Synthetic`, BatchLayoutVersion, 0},
		{"versioned-slashes", "C:/Synthetic", BatchLayoutVersion, 0},
		{"compact-result", `C:\Synthetic`, BatchLayoutVersion, BatchCompletionVersion},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testBatchEnvelopeAndReadback(t, tc.root, tc.version, tc.completion)
		})
	}
}

func testBatchEnvelopeAndReadback(t *testing.T, root string, version, completion int) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"uname":   "#!/bin/sh\nprintf MINGW64_NT-SYNTHETIC\n",
		"cygpath": "#!/bin/sh\ncase \"$2\" in 'C:\\Synthetic'|'C:/Synthetic') printf '%s' \"$SYNTH_ROOT\";; *) printf '%s' \"$2\";; esac\n",
		"pi":      "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$SYNTH_ARGS\"\ncat > \"$SYNTH_INPUT\"\ncat \"$SYNTH_EVENTS\"\nprintf 'ordinary stderr 中文' >&2\nexit 7\n",
	} {
		//nolint:gosec // Fixed synthetic utilities in t.TempDir; no Windows/provider contacted.
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	p := batchFixtureProgress()
	p.LayoutVersion = version
	p.CompletionVersion = completion
	if completion != 0 {
		node, err := exec.LookPath("node")
		if err != nil {
			t.Fatal("Node required for compact-result contract tests")
		}
		if err := os.Symlink(node, filepath.Join(bin, "node")); err != nil {
			t.Fatal(err)
		}
	}
	events := filepath.Join(dir, "events")
	if err := os.WriteFile(events, []byte(piEvents(p.PiSessionID, "stop", "42 中文 ✓", false)), 0o600); err != nil {
		t.Fatal(err)
	}
	target := Target{WorkDir: root, Agent: AgentProfile{Provider: "synthetic", Model: "synthetic-model"}}
	task := "--model other; $(touch MUST_NOT_EXIST)\n@private-file\n'中文'"
	script := batchFixtureScript(t, p, target, target.WorkDir, task)
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
	if completion != 0 {
		if r.Completion == nil {
			t.Fatal("local projection did not produce completion metadata")
		}
		raw := readBatchFixture(t, filepath.Join(dir, ".memoh-computer", "attempts", p.Attempt, "result.json"))
		result, err := ParseBatchCompletion(raw, p, *r)
		if err != nil || result.Outcome != "failed" || result.Output != "42 中文 ✓" {
			t.Fatalf("actual Bash/Node projection: %+v %v", result, err)
		}
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
	if !strings.Contains(string(args), "--session-dir\n"+filepath.Join(dir, ".memoh-computer", "sessions", p.PiSessionID)+"\n") {
		t.Fatal("Pi session directory changed from the existing layout")
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
	// Simulate a service restart using only serialized progress. Old operations
	// omit layout_version; neither path resolution nor reading may move files.
	durable, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var restored BatchProgress
	if json.Unmarshal(durable, &restored) != nil || restored.LayoutVersion != version {
		t.Fatal("durable layout version was lost")
	}
	if version == 0 && strings.Contains(string(durable), "layout_version") {
		t.Fatal("legacy fixture must have no layout version")
	}
	writer := script
	names := []string{"receipt.json", "stdout.jsonl", "stderr.txt"}
	if completion != 0 {
		names = append(names, "result.json")
	}
	for _, name := range names {
		nonce := strings.Repeat("1", 32)
		count := int64(BatchPageBytes)
		if name == "receipt.json" {
			count = batchReceiptBytes
		}
		script, err = batchReadScript(restored, target, name, 0, count, nonce)
		if err != nil {
			t.Fatal(err)
		}
		frame, err := run()
		if err != nil {
			t.Fatalf("read envelope: %v %s", err, frame)
		}
		encoded := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(string(frame)), "MEMOH_FILE_"+nonce+":"), ":END_FILE_"+nonce)
		got, err := base64.StdEncoding.DecodeString(encoded)
		want := readBatchFixture(t, filepath.Join(dir, ".memoh-computer", "attempts", p.Attempt, name))
		if err != nil || string(got) != string(want) {
			t.Fatalf("restored reader disagrees with writer for %s: %v", name, err)
		}
	}
	script = writer
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
	script = batchFixtureScript(t, next, target, target.WorkDir, "must not reach Pi")
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
	script = batchFixtureScript(t, next, target, filepath.Join(dir, "missing-directory"), "must not start")
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

func batchFixtureScript(t *testing.T, p BatchProgress, target Target, cwd, task string) string {
	t.Helper()
	script, err := batchScript(p, target, cwd, task)
	if err != nil {
		t.Fatal(err)
	}
	return script
}

func TestBatchLayoutRejectsUnsupportedVersionAndPaths(t *testing.T) {
	for _, modify := range []func(*BatchProgress){
		func(p *BatchProgress) { p.LayoutVersion = BatchLayoutVersion + 1 },
		func(p *BatchProgress) { p.LayoutVersion = -1 },
		func(p *BatchProgress) { p.Attempt = "../another-attempt" },
		func(p *BatchProgress) { p.PiSessionID = `..\another-session` },
	} {
		p := batchFixtureProgress()
		modify(&p)
		if p.valid() {
			t.Fatal("invalid layout accepted")
		}
		if _, err := batchScript(p, Target{}, ".", "must not start"); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("writer accepted unsupported layout: %v", err)
		}
		if _, err := batchReadScript(p, Target{}, "receipt.json", 0, batchReceiptBytes, strings.Repeat("1", 32)); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("reader accepted unsupported layout: %v", err)
		}
	}
	for _, name := range []string{"../receipt.json", `..\receipt.json`, "prompt.txt", "/stdout.jsonl"} {
		if _, err := batchReadScript(batchFixtureProgress(), Target{}, name, 0, 1, strings.Repeat("1", 32)); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("reader accepted a non-result file: %v", err)
		}
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
