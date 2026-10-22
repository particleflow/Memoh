package agenvo

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// These kill only the test-owned Node child. They exercise the real fixed Bash
// envelope, atomic rename and public receipt; they do not simulate power loss.
func TestCompactCompletionAtomicWriteInterruption(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"partial_tmp", "before_rename", "after_rename", "existing_tmp"} {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "bin")
			if err := os.Mkdir(bin, 0o700); err != nil {
				t.Fatal(err)
			}
			fixtures := map[string]string{
				"uname":   "#!/bin/sh\nprintf MINGW64_NT-SYNTHETIC\n",
				"cygpath": "#!/bin/sh\ncase \"$2\" in 'C:\\Synthetic') printf '%s' \"$SYNTH_ROOT\";; *) printf '%s' \"$2\";; esac\n",
				"pi":      "#!/bin/sh\nprintf 'started\\n' >> \"$SYNTH_ROOT/starts\"\ncat > /dev/null\ncat \"$SYNTH_ROOT/events\"\n",
			}
			for name, body := range fixtures {
				//nolint:gosec // Fixed synthetic programs in the test-owned directory.
				if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(node, filepath.Join(bin, "node")); err != nil {
				t.Fatal(err)
			}
			// Node's official --require loads this test-only fault injector. No
			// production launcher or vendor source is patched for fault injection.
			faults := `const fs=require('node:fs'),path=require('node:path');
const stage=process.env.SYNTH_FAULT;
const kill=()=>process.kill(process.pid,'SIGKILL');
const write=fs.writeFileSync;
if(stage==='partial_tmp')fs.writeFileSync=function(fd,raw,...args){write.call(fs,fd,String(raw).slice(0,17),...args);kill();};
const rename=fs.renameSync;
fs.renameSync=function(src,dst){if(stage==='before_rename')kill();rename.call(fs,src,dst);if(stage==='after_rename')kill();};
if(stage==='existing_tmp')write.call(fs,path.join(path.dirname(process.argv[1]),'result.tmp'),'prior-interrupted-attempt');
`
			faultFile := filepath.Join(dir, "faults.cjs")
			if err := os.WriteFile(faultFile, []byte(faults), 0o600); err != nil {
				t.Fatal(err)
			}
			p := batchFixtureProgress()
			p.CompletionVersion = BatchCompletionVersion
			p.LayoutVersion = BatchLayoutVersion
			if err := os.WriteFile(filepath.Join(dir, "events"), []byte(piEvents(p.PiSessionID, "stop", "42 中文 ✓", false)), 0o600); err != nil {
				t.Fatal(err)
			}
			target := Target{WorkDir: `C:\Synthetic`, Agent: AgentProfile{Provider: "synthetic", Model: "synthetic-model"}}
			script := batchFixtureScript(t, p, target, target.WorkDir, "synthetic task")
			run := func() ([]byte, error) {
				//nolint:gosec // Own fixed envelope and local synthetic fixtures only.
				cmd := exec.CommandContext(t.Context(), "/bin/bash", "--noprofile", "--norc", "-p", "-c", script)
				cmd.Dir = dir
				cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin", "SYNTH_ROOT=" + dir, "SYNTH_FAULT=" + stage, "NODE_OPTIONS=--require=" + faultFile}
				return cmd.CombinedOutput()
			}
			out, err := run()
			if err != nil {
				t.Fatalf("envelope: %v %s", err, out)
			}
			receipt, err := parseBatchReceipt(string(out), p)
			if err != nil || receipt == nil || !receipt.Started || receipt.ExitCode != 0 || receipt.Completion != nil {
				t.Fatalf("projection crash fabricated a result or lost exit: %+v %v", receipt, err)
			}
			attempt := filepath.Join(dir, ".memoh-computer", "attempts", p.Attempt)
			raw := readBatchFixture(t, filepath.Join(attempt, "receipt.json"))
			var persisted BatchReceipt
			if json.Unmarshal(raw, &persisted) != nil || persisted.Validate(p) != nil || persisted.Completion != nil {
				t.Fatal("partial or uncorrelated exit receipt")
			}
			resultPath := filepath.Join(attempt, "result.json")
			if stage == "after_rename" {
				if !json.Valid(readBatchFixture(t, resultPath)) {
					t.Fatal("renamed result was partial")
				}
			} else if _, err := os.Stat(resultPath); !os.IsNotExist(err) {
				t.Fatal("uncommitted result became visible")
			}
			if stage == "existing_tmp" && string(readBatchFixture(t, filepath.Join(attempt, "result.tmp"))) != "prior-interrupted-attempt" {
				t.Fatal("exclusive temporary file overwritten")
			}
			before := string(raw)
			if _, err := run(); err == nil {
				t.Fatal("interrupted projection allowed business redispatch")
			}
			if string(readBatchFixture(t, filepath.Join(dir, "starts"))) != "started\n" || string(readBatchFixture(t, filepath.Join(attempt, "receipt.json"))) != before {
				t.Fatal("duplicate attempt changed business/receipt")
			}
		})
	}
}

func TestCompactCompletionLargeDiagnosticsStayLocal(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	p := batchFixtureProgress()
	ctx, _ := json.Marshal(map[string]string{"attempt": p.Attempt, "generation": p.Generation, "request_hash": p.RequestHash, "pi_session_id": p.PiSessionID, "workspace_id": p.WorkspaceID, "pane_id": p.PaneID, "terminal_id": p.TerminalID, "provider": "synthetic", "model": "synthetic-model"})
	stream := piEvents(p.PiSessionID, "stop", "42 中文 ✓", false)
	// Large tool progress stays inside the invocation's valid event boundaries.
	largeEvent := `{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","delta":"` + strings.Repeat("PRIVATE_REASONING", 32768) + `"}}` + "\n"
	stream = strings.Replace(stream, `{"message":{"content"`, largeEvent+`{"message":{"content"`, 1)
	for name, raw := range map[string][]byte{"reader.cjs": []byte(completionAdapterProgram()), "result-context.json": ctx, "stdout.jsonl": []byte(stream), "stderr.txt": []byte(strings.Repeat("PRIVATE_DIAGNOSTIC\n", 131072))} {
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	//nolint:gosec // Fixed production projector and synthetic local files.
	cmd := exec.CommandContext(t.Context(), node, filepath.Join(dir, "reader.cjs"), "0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("projector %v %s", err, out)
	}
	raw := readBatchFixture(t, filepath.Join(dir, "result.json"))
	var result BatchCompletion
	if json.Unmarshal(raw, &result) != nil || result.Result.Outcome != "succeeded" || result.Result.Output != "42 中文 ✓" || result.Stdout.Size < 500000 || len(raw) > 2048 || strings.Contains(string(raw), "PRIVATE_") {
		t.Fatalf("large diagnostic affected compact result: %s", raw)
	}
}

func TestCompactCompletionLongLineHasLinearReadAndCopyCost(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{63 << 20, 65 << 20} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			dir := t.TempDir()
			reader := filepath.Join(dir, "reader.cjs")
			events := filepath.Join(dir, "events")
			if err := os.WriteFile(reader, []byte(completionAdapterProgram()), 0o600); err != nil {
				t.Fatal(err)
			}
			stream := piEvents("session-a", "stop", "42", false)
			split := strings.Index(stream, `{"message":{"content"`)
			if split < 0 {
				t.Fatal("fixture boundary missing")
			}
			//nolint:gosec // Fixed synthetic output below t.TempDir, no user path.
			f, err := os.Create(events)
			if err != nil {
				t.Fatal(err)
			}
			write := func(b []byte) {
				if _, err := f.Write(b); err != nil {
					t.Fatal(err)
				}
			}
			write([]byte(stream[:split] + `{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","delta":"`))
			chunk := []byte(strings.Repeat("x", 65536))
			for n := 0; n < size; n += len(chunk) {
				write(chunk)
			}
			write([]byte("\"}}\n" + stream[split:]))
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			// Independent instrumentation of actual Buffer/fs operations, not a
			// counter maintained by the implementation under test.
			instrument := `const fs=require('node:fs'),x=require(process.argv[1]);
let read=0,scan=0,copy=0;
const rs=fs.readSync;fs.readSync=function(...a){const n=rs.apply(this,a);read+=n;return n;};
const ix=Buffer.prototype.indexOf;Buffer.prototype.indexOf=function(value,start=0,...a){const n=ix.call(this,value,start,...a);scan+=(n<0?this.length:n+1)-start;return n;};
const cat=Buffer.concat;Buffer.concat=function(list,...a){copy+=list.reduce((n,b)=>n+b.length,0);return cat.call(this,list,...a);};
const result=x.summarize(process.argv[2],{pi_session_id:'session-a',provider:'synthetic',model:'synthetic-model'},0);
process.stdout.write(JSON.stringify({read,scan,copy,...result}));`
			//nolint:gosec // Fixed instrumentation and projector, synthetic local data only.
			cmd := exec.CommandContext(t.Context(), node, "-e", instrument, reader, events)
			raw, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("projector: %v %s", err, raw)
			}
			var got struct {
				Read, Scan, Copy int64
				Stdout           BatchFile    `json:"stdout"`
				Result           PiJSONResult `json:"result"`
			}
			if json.Unmarshal(raw, &got) != nil {
				t.Fatal("bad projection")
			}
			want := "succeeded"
			if size > 64<<20 {
				want = "output_limit"
			}
			if got.Result.Outcome != want || got.Read != got.Stdout.Size || got.Scan > got.Read || got.Copy > got.Read {
				t.Fatalf("not linear/bounded: %s", raw)
			}
			t.Logf("input=%d read=%d scanned=%d copied=%d outcome=%s", got.Stdout.Size, got.Read, got.Scan, got.Copy, got.Result.Outcome)
		})
	}
}

func TestCompactCompletionWatchdogStopsBlockedParser(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	reader := filepath.Join(dir, "reader.cjs")
	fault := filepath.Join(dir, "block.cjs")
	if err := os.WriteFile(reader, []byte(completionAdapterProgram()), 0o600); err != nil {
		t.Fatal(err)
	}
	blocker := `if(!require('node:worker_threads').isMainThread){require('node:fs').writeFileSync('blocked','worker reached fault');Atomics.wait(new Int32Array(new SharedArrayBuffer(4)),0,0);}`
	if err := os.WriteFile(fault, []byte(blocker), 0o600); err != nil {
		t.Fatal(err)
	}
	// Install the same watchdog with a short test deadline. The production CLI
	// uses a fixed 30 seconds, without an environment-configurable override.
	//nolint:gosec // Owned helper child and fixed deliberate blocking fault.
	cmd := exec.CommandContext(t.Context(), node, "--require", fault, "-e", `require(process.argv[1]).runWithDeadline(0,500);`, reader)
	cmd.Dir = dir
	start := time.Now()
	out, err := cmd.CombinedOutput()
	if err == nil || time.Since(start) < 450*time.Millisecond || time.Since(start) > 5*time.Second || len(out) != 0 {
		t.Fatalf("watchdog: %v duration=%v output=%s", err, time.Since(start), out)
	}
	if string(readBatchFixture(t, filepath.Join(dir, "blocked"))) != "worker reached fault" {
		t.Fatal("fault did not run")
	}
	if _, err := os.Stat(filepath.Join(dir, "result.json")); !os.IsNotExist(err) {
		t.Fatal("timed-out parser published a result")
	}
}
