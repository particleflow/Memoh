package computertarget

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func preflightBatchFile(name string, raw []byte) BatchFile {
	sum := sha256.Sum256(raw)
	return BatchFile{Name: name, Hash: hex.EncodeToString(sum[:]), Size: int64(len(raw))}
}

// TestBatchScriptExplicitPathIsDataAndOptional pins the incident E3 fix shape:
// the configured agent PATH is decoded as data, converted with cygpath -p, and
// prepended inside the pane command itself, so Pi resolution never depends on
// Herdr's per-login pane environment.
func TestBatchScriptExplicitPathIsDataAndOptional(t *testing.T) {
	p := batchFixtureProgress()
	p.LayoutVersion = BatchLayoutVersion
	p.CompletionVersion = BatchCompletionVersion
	p.History = &BatchHistoryExpectation{Version: BatchHistoryVersion, Mode: "first"}
	noPath := Target{WorkDir: `C:\Synthetic`, Agent: AgentProfile{Provider: "synthetic", Model: "synthetic-model"}}
	withPath := noPath
	withPath.Agent.Path = `C:\Program Files\nodejs;C:\Synthetic Pi\bin`
	plain := batchFixtureScript(t, p, noPath, noPath.WorkDir, "task")
	fixed := batchFixtureScript(t, p, withPath, withPath.WorkDir, "task")
	for _, script := range []string{plain, fixed} {
		if !strings.Contains(script, `memoh_preflight=ok`) ||
			!strings.Contains(script, `elif ! command -v pi >/dev/null; then memoh_preflight=preflight_pi_missing`) ||
			!strings.Contains(script, `elif ! command -v node >/dev/null; then memoh_preflight=preflight_node_missing`) ||
			!strings.Contains(script, `elif ! mkdir "$memoh_session/lock" 2>/dev/null; then memoh_preflight=preflight_lock_failed`) ||
			!strings.Contains(script, `if ! cd "$memoh_cwd"; then memoh_preflight=preflight_cwd_failed`) ||
			!strings.Contains(script, `,"preflight":"%s"' "$memoh_started"`) {
			t.Fatal("preflight chain or receipt field missing")
		}
	}
	// The guarded export line exists in both envelopes; only the configured
	// value differs (empty stays inert behind -n).
	exportLine := `if [ -n "$memoh_path" ]; then memoh_path="$(cygpath -p -u "$memoh_path")"; PATH="$memoh_path:$PATH"; export PATH; fi`
	if !strings.Contains(plain, exportLine) || !strings.Contains(fixed, exportLine) {
		t.Fatal("explicit agent PATH export missing")
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(withPath.Agent.Path))
	if !strings.Contains(fixed, encoded) {
		t.Fatal("configured agent PATH value absent from the envelope")
	}
	if strings.Contains(plain, encoded) {
		t.Fatal("empty agent PATH carried a value")
	}
	// The configured PATH value itself must only enter the script as base64
	// data, never as shell source (it is owner configuration).
	if strings.Contains(fixed, `C:\Synthetic Pi`) {
		t.Fatal("agent PATH leaked as shell source")
	}
}

// TestBatchReceiptPreflightValidation pins the receipt contract for the
// never-started reason code, including receipts written before the field
// existed.
func TestBatchReceiptPreflightValidation(t *testing.T) {
	p := batchFixtureProgress()
	base := BatchReceipt{
		Version: 1, Attempt: p.Attempt, Generation: p.Generation, RequestHash: p.RequestHash,
		PiSessionID: p.PiSessionID, WorkspaceID: p.WorkspaceID, PaneID: p.PaneID, TerminalID: p.TerminalID,
		Exited: true, Started: false, ExitCode: 125,
		Stdout: preflightBatchFile("stdout.jsonl", nil), Stderr: preflightBatchFile("stderr.txt", nil),
	}
	for _, tc := range []struct {
		name      string
		preflight string
		started   bool
		valid     bool
	}{
		{"legacy_empty", "", false, true},
		{"ok_not_started", batchPreflightOK, false, true},
		{"pi_missing", batchPreflightPi, false, true},
		{"node_missing", batchPreflightNode, false, true},
		{"cwd_failed", batchPreflightCwd, false, true},
		{"lock_failed", batchPreflightLock, false, true},
		{"started_with_ok", batchPreflightOK, true, true},
		{"started_with_failure", batchPreflightPi, true, false},
		{"unknown_code", "preflight_nonsense", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			r.Preflight, r.Started = tc.preflight, tc.started
			err := r.Validate(p)
			if tc.valid && err != nil {
				t.Fatalf("rejected valid receipt: %v", err)
			}
			if !tc.valid && err == nil {
				t.Fatal("accepted invalid receipt")
			}
		})
	}
}

// TestBatchEnvelopePreflightReasonCodes executes the real envelope in a login
// PATH that cannot resolve pi (incident E3): the receipt and stderr.txt must
// name preflight_pi_missing, and the explicit agent PATH must make pi
// resolvable without any Herdr environment inheritance.
func TestBatchEnvelopePreflightReasonCodes(t *testing.T) {
	for _, tc := range []struct {
		name, code, agentPath string
		loginPi, loginNode    bool
		precreateLock         bool
		cwdMissing            bool
	}{
		{"explicit_path_starts_pi", "", `C:\Synthetic\agent`, false, false, false, false},
		{"pi_missing", batchPreflightPi, "", false, false, false, false},
		{"node_missing", batchPreflightNode, "", true, false, false, false},
		{"lock_failed", batchPreflightLock, "", true, true, true, false},
		{"cwd_failed", batchPreflightCwd, "", true, true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "bin")
			agent := filepath.Join(dir, "agent")
			if err := os.Mkdir(bin, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(agent, 0o700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"base64", "sha256sum", "wc", "mkdir", "mv", "rm", "rmdir"} {
				executable, err := exec.LookPath(name)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(executable, filepath.Join(bin, name)); err != nil {
					t.Fatal(err)
				}
			}
			piShim := "#!/bin/sh\nprintf started > \"$SYNTH_ROOT/started\"\n/bin/cat > /dev/null\nprintf 'no valid JSON protocol'\n"
			//nolint:gosec // Fixed synthetic programs in the test directory only.
			for name, body := range map[string]string{
				"uname": "#!/bin/sh\nprintf MINGW64_NT-SYNTHETIC\n",
				// cygpath -p -u converts the configured Windows PATH list to the
				// agent-only directory; plain paths keep the synthetic root mapping.
				"cygpath": "#!/bin/sh\ncase \"$1\" in -p) printf '%s' \"$SYNTH_ROOT/agent\";; *) case \"$2\" in 'C:\\Synthetic'|'C:/Synthetic') printf '%s' \"$SYNTH_ROOT\";; *) printf '%s' \"$2\";; esac;; esac\n",
			} {
				if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			link := func(dir, name string) {
				t.Helper()
				if name == "pi" {
					//nolint:gosec // Fixed synthetic program in the test directory only.
					if err := os.WriteFile(filepath.Join(dir, name), []byte(piShim), 0o700); err != nil {
						t.Fatal(err)
					}
					return
				}
				executable, err := exec.LookPath(name)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(executable, filepath.Join(dir, name)); err != nil {
					t.Fatal(err)
				}
			}
			if tc.loginPi {
				link(bin, "pi")
			}
			if tc.loginNode {
				link(bin, "node")
			}
			if tc.agentPath != "" {
				link(agent, "pi")
				link(agent, "node")
			}
			p := batchFixtureProgress()
			p.LayoutVersion = BatchLayoutVersion
			p.CompletionVersion = BatchCompletionVersion
			if tc.precreateLock {
				if err := os.MkdirAll(filepath.Join(dir, ".memoh-computer", "sessions", p.PiSessionID, "lock"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			target := Target{WorkDir: `C:\Synthetic`, Agent: AgentProfile{Provider: "synthetic", Model: "synthetic-model", Path: tc.agentPath}}
			// The root stays mapped to the synthetic dir so attempt files land
			// in a real location; only the task cwd points at a directory that
			// does not exist (the shim passes unmatched paths through).
			cwd := target.WorkDir
			if tc.cwdMissing {
				cwd = `C:\Synthetic\no-such-dir`
			}
			script := batchFixtureScript(t, p, target, cwd, "task data")
			//nolint:gosec // Actual fixed production envelope with only synthetic executables.
			cmd := exec.CommandContext(t.Context(), "/bin/bash", "--noprofile", "--norc", "-p", "-c", script)
			// The login PATH deliberately cannot see pi/node: only the explicit
			// agent PATH or its absence decides resolution.
			cmd.Env = []string{"PATH=" + bin, "SYNTH_ROOT=" + dir}
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("envelope %v %s", err, out)
			}
			receipt, err := parseBatchReceipt(string(out), p)
			if err != nil || receipt == nil {
				t.Fatalf("bad receipt %+v %v %s", receipt, err, out)
			}
			if tc.code == "" {
				if !receipt.Started {
					t.Fatalf("explicit agent PATH did not start pi: %+v stderr=%q", receipt, readBatchFixture(t, filepath.Join(dir, ".memoh-computer", "attempts", p.Attempt, "stderr.txt")))
				}
				return
			}
			if receipt.Started || receipt.ExitCode != 125 || receipt.Preflight != tc.code {
				t.Fatalf("want %s receipt, got %+v", tc.code, receipt)
			}
			stderr := readBatchFixture(t, filepath.Join(dir, ".memoh-computer", "attempts", p.Attempt, "stderr.txt"))
			if !strings.Contains(string(stderr), tc.code) {
				t.Fatalf("stderr.txt missing reason code %q: %q", tc.code, stderr)
			}
		})
	}
}
