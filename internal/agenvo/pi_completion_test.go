package agenvo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Execute the production Node projector, not a fixture-generated receipt.
func TestPiCompletionOfficialProtocolProjection(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("Node required for compact-result contract tests")
	}
	dir := t.TempDir()
	reader := filepath.Join(dir, "reader.cjs")
	if err := os.WriteFile(reader, []byte(completionAdapterProgram()), 0o600); err != nil {
		t.Fatal(err)
	}
	good := piEvents("session-a", "stop", "42 中文 ✓", false)
	first := piEvents("session-a", "error", "retry only", false)
	first = first[:strings.LastIndex(first, `{"aborted"`)]
	second := good[strings.IndexByte(good, '\n')+1:]
	tool := strings.Replace(good, `{"type":"turn_end"}`, "{\"type\":\"tool_execution_start\",\"toolCallId\":\"x\"}\n{\"type\":\"tool_execution_end\",\"toolCallId\":\"x\"}\n{\"type\":\"turn_end\"}", 1)
	for _, tc := range []struct {
		name, data, outcome string
		exit                int
	}{
		{"normal", good, "succeeded", 0},
		{"error_exit_zero", piEvents("session-a", "error", "failure", false), "failed", 0},
		{"nonzero", good, "failed", 7},
		{"aborted", piEvents("session-a", "stop", "partial", true), "aborted", 0},
		{"length", piEvents("session-a", "length", "partial", false), "incomplete", 0},
		{"tool_round", tool, "succeeded", 0},
		{"retry_success", first + second, "succeeded", 0},
		{"empty_slash_no_run", "", "invalid_protocol", 0},
		{"missing_settled", good[:strings.LastIndex(good, `{"aborted"`)], "invalid_protocol", 0},
		{"partial_line", strings.TrimSuffix(good, "\n"), "invalid_protocol", 0},
		{"wrong_provider", strings.ReplaceAll(good, `"provider":"synthetic"`, `"provider":"other"`), "invalid_protocol", 0},
		{"wrong_session", strings.Replace(good, "session-a", "other", 1), "invalid_protocol", 0},
		{"invalid_utf8", good + string([]byte{255, '\n'}), "invalid_protocol", 0},
		{"after_settled", good + "{\"type\":\"pretend_completed\"}\n", "invalid_protocol", 0},
		{"private_thinking", strings.Replace(good, `"content":[`, `"content":[{"type":"thinking","thinking":"PRIVATE_REASONING"},`, 1), "succeeded", 0},
		{"bounded_text", piEvents("session-a", "stop", strings.Repeat("中文\x01", 4000), false), "succeeded", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := filepath.Join(dir, "events")
			if err := os.WriteFile(events, []byte(tc.data), 0o600); err != nil {
				t.Fatal(err)
			}
			expected, _ := json.Marshal(map[string]string{"pi_session_id": "session-a", "provider": "synthetic", "model": "synthetic-model"})
			//nolint:gosec // Fixed local adapter and synthetic input, no provider or user code.
			cmd := exec.CommandContext(t.Context(), node, "-e", `const x=require(process.argv[1]); process.stdout.write(JSON.stringify(x.summarize(process.argv[2],JSON.parse(process.argv[3]),Number(process.argv[4]))));`, reader, events, string(expected), strconv.Itoa(tc.exit))
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("Node: %v %s", err, out)
			}
			var got struct {
				Stdout BatchFile    `json:"stdout"`
				Result PiJSONResult `json:"result"`
			}
			if json.Unmarshal(out, &got) != nil || got.Result.Outcome != tc.outcome || got.Stdout.Size != int64(len(tc.data)) || len(got.Result.Output) > 2048 || strings.Contains(string(out), "PRIVATE_REASONING") {
				t.Fatalf("projection: %s", out)
			}
			legacy := VerifyPiJSON(strings.NewReader(tc.data), "session-a", "synthetic", "synthetic-model", tc.exit)
			if got.Result.Outcome != legacy.Outcome {
				t.Fatalf("protocol parity: %+v vs %+v", got.Result, legacy)
			}
			if tc.outcome == "invalid_protocol" && (got.Result.Output != "" || got.Result.StopReason != "") {
				t.Fatal("invalid stream leaked an earlier reply")
			}
		})
	}
}

func TestCompactEnvelopePreflightAndProjectionFailure(t *testing.T) {
	for _, mode := range []string{"missing_node", "projection_failed"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "bin")
			if err := os.Mkdir(bin, 0o700); err != nil {
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
			fixtures := map[string]string{
				"uname":   "#!/bin/sh\nprintf MINGW64_NT-SYNTHETIC\n",
				"cygpath": "#!/bin/sh\ncase \"$2\" in 'C:\\Synthetic') printf '%s' \"$SYNTH_ROOT\";; *) printf '%s' \"$2\";; esac\n",
				"pi":      "#!/bin/sh\nprintf started > \"$SYNTH_ROOT/started\"\n/bin/cat > /dev/null\nprintf 'no valid JSON protocol'\n",
			}
			if mode == "projection_failed" {
				fixtures["node"] = "#!/bin/sh\nexit 1\n"
			}
			for name, body := range fixtures {
				//nolint:gosec // Fixed synthetic programs in the test directory only.
				if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			p := batchFixtureProgress()
			p.LayoutVersion = BatchLayoutVersion
			p.CompletionVersion = BatchCompletionVersion
			target := Target{WorkDir: `C:\Synthetic`, Agent: AgentProfile{Provider: "synthetic", Model: "synthetic-model"}}
			script := batchFixtureScript(t, p, target, target.WorkDir, "task data")
			//nolint:gosec // Actual fixed production envelope with only synthetic executables.
			cmd := exec.CommandContext(t.Context(), "/bin/bash", "--noprofile", "--norc", "-p", "-c", script)
			cmd.Env = []string{"PATH=" + bin, "SYNTH_ROOT=" + dir}
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("envelope %v %s", err, out)
			}
			receipt, err := parseBatchReceipt(string(out), p)
			if err != nil || receipt == nil || receipt.Completion != nil {
				t.Fatalf("bad exit receipt %+v %v", receipt, err)
			}
			if mode == "missing_node" && (receipt.Started || receipt.ExitCode != 125) {
				t.Fatal("missing runtime launched Pi")
			}
			if mode == "projection_failed" && (!receipt.Started || receipt.ExitCode != 0) {
				t.Fatal("projection failure hid the real invocation exit")
			}
		})
	}
}

func TestCompactCompletionWireBudgetAndCorrelation(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	p := batchFixtureProgress()
	p.CompletionVersion = BatchCompletionVersion
	contextJSON, _ := json.Marshal(map[string]string{"attempt": p.Attempt, "generation": p.Generation, "request_hash": p.RequestHash, "pi_session_id": p.PiSessionID, "workspace_id": p.WorkspaceID, "pane_id": p.PaneID, "terminal_id": p.TerminalID, "provider": "synthetic", "model": "synthetic-model"})
	stream := []byte(piEvents(p.PiSessionID, "stop", strings.Repeat("\x01中文", 4000), false))
	for name, raw := range map[string][]byte{"reader.cjs": []byte(completionAdapterProgram()), "result-context.json": contextJSON, "stdout.jsonl": stream} {
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	//nolint:gosec // Fixed projector CLI, synthetic input and test-owned paths.
	cmd := exec.CommandContext(t.Context(), node, filepath.Join(dir, "reader.cjs"), "0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("projector: %v %s", err, out)
	}
	raw := readBatchFixture(t, filepath.Join(dir, "result.json"))
	if len(raw) > BatchCompletionBytes {
		t.Fatal("escaped text exceeded wire budget")
	}
	file := func(name string, b []byte) BatchFile {
		sum := sha256.Sum256(b)
		return BatchFile{Name: name, Hash: hex.EncodeToString(sum[:]), Size: int64(len(b))}
	}
	r := BatchReceipt{Version: 1, Attempt: p.Attempt, Generation: p.Generation, RequestHash: p.RequestHash, PiSessionID: p.PiSessionID, WorkspaceID: p.WorkspaceID, PaneID: p.PaneID, TerminalID: p.TerminalID, Exited: true, Started: true, Stdout: file("stdout.jsonl", stream), Stderr: file("stderr.txt", nil)}
	meta := file("result.json", raw)
	r.Completion = &meta
	result, err := ParseBatchCompletion(raw, p, r)
	if err != nil || result.Outcome != "succeeded" || !result.Truncated {
		t.Fatalf("bounded result %+v %v", result, err)
	}
	for _, field := range []string{"attempt", "generation", "request_hash", "pi_session_id", "workspace_id", "pane_id", "terminal_id", "stdout"} {
		t.Run(field, func(t *testing.T) {
			var c map[string]any
			if json.Unmarshal(raw, &c) != nil {
				t.Fatal("bad test result")
			}
			c[field] = "other"
			altered, _ := json.Marshal(c)
			receipt := r
			newMeta := file("result.json", altered)
			receipt.Completion = &newMeta
			if _, err := ParseBatchCompletion(altered, p, receipt); err == nil {
				t.Fatal("rehashed cross-invocation projection accepted")
			}
		})
	}
	if _, err := ParseBatchCompletion(append(raw, ' '), p, r); err == nil {
		t.Fatal("wire hash mismatch accepted")
	}
}
