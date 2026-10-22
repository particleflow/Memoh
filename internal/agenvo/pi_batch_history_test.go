package agenvo

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func historyFixture(t *testing.T) (BatchProgress, Target, string, func(BatchProgress) (*BatchReceipt, error)) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(node, filepath.Join(bin, "node")); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"uname":   "#!/bin/sh\nprintf MINGW64_NT-SYNTHETIC\n",
		"cygpath": "#!/bin/sh\nprintf '%s' \"$2\"\n",
		"pi": `#!/usr/bin/env node
const fs=require('node:fs'),path=require('node:path');
const args=process.argv.slice(2),at=k=>args[args.indexOf(k)+1];
fs.appendFileSync(process.env.SYNTH_CALLS,JSON.stringify(args)+'\n');
fs.readFileSync(0);
const dir=at('--session-dir');
const file=args.includes('--session')?at('--session'):path.join(dir,'synthetic_'+at('--session-id')+'.jsonl');
const now='2026-10-22T00:00:00.000Z';
if(!fs.existsSync(file)) fs.writeFileSync(file,JSON.stringify({type:'session',version:3,id:at('--session-id'),timestamp:now,cwd:process.cwd()})+'\n');
const entries=fs.readFileSync(file,'utf8').trim().split('\n').map(JSON.parse),last=entries.at(-1);
const role=fs.existsSync(process.env.SYNTH_USER_ONLY)?'user':'assistant';
fs.appendFileSync(file,JSON.stringify({type:'message',id:'entry'+entries.length,parentId:last.type==='session'?null:last.id,timestamp:now,message:{role,content:[{type:'text',text:'synthetic'}],timestamp:Date.parse(now)}})+'\n');
if(fs.existsSync(process.env.SYNTH_DELETE)) fs.unlinkSync(file);
`,
	} {
		//nolint:gosec // Fixed synthetic utilities only, no provider or PC.
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	p := batchFixtureProgress()
	p.History = &BatchHistoryExpectation{Version: BatchHistoryVersion, Mode: "first"}
	target := Target{WorkDir: dir, Agent: AgentProfile{Provider: "synthetic", Model: "synthetic"}}
	run := func(p BatchProgress) (*BatchReceipt, error) {
		script, err := batchScript(p, target, dir, "synthetic only")
		if err != nil {
			return nil, err
		}
		//nolint:gosec // Generated host envelope, isolated fixture filesystem.
		cmd := exec.CommandContext(t.Context(), "/bin/bash", "--noprofile", "--norc", "-p", "-c", script)
		cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin", "SYNTH_CALLS=" + filepath.Join(dir, "calls"), "SYNTH_DELETE=" + filepath.Join(dir, "delete"), "SYNTH_USER_ONLY=" + filepath.Join(dir, "user-only")}
		raw, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("envelope: %w: %s", err, raw)
		}
		r, err := parseBatchReceipt(string(raw), p)
		if err == nil && r == nil {
			t.Fatalf("missing receipt: %s", raw)
		}
		return r, err
	}
	return p, target, dir, run
}

func TestBatchHistoryFirstResumeAndDuplicate(t *testing.T) {
	p, _, dir, run := historyFixture(t)
	first, err := run(p)
	if err != nil || !first.Started || first.History.After != "created" {
		t.Fatalf("first: %+v %v", first, err)
	}
	if _, err = run(p); err == nil {
		t.Fatal("duplicate attempt was replayed")
	}
	next := p
	next.Attempt = strings.Repeat("e", 32)
	next.History = &BatchHistoryExpectation{Version: 1, Mode: "resume", Evidence: first.History.Evidence}
	// Serialize to simulate application restart; only metadata survives.
	raw, _ := json.Marshal(next)
	var restored BatchProgress
	if err = json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	second, err := run(restored)
	if err != nil || !second.Started || second.History.Before != "verified" || second.History.After != "verified" || second.History.Evidence.Size <= first.History.Evidence.Size {
		t.Fatalf("resume: %+v %v", second, err)
	}
	calls := strings.Split(strings.TrimSpace(string(readBatchFixture(t, filepath.Join(dir, "calls")))), "\n")
	if len(calls) != 2 {
		t.Fatal("duplicate Pi dispatch")
	}
	var firstArgs, nextArgs []string
	_ = json.Unmarshal([]byte(calls[0]), &firstArgs)
	_ = json.Unmarshal([]byte(calls[1]), &nextArgs)
	if !strings.Contains(strings.Join(firstArgs, "\n"), "--session-id\n"+p.PiSessionID) || strings.Contains(calls[1], "--session-id") || !strings.Contains(calls[1], "--session\"") {
		t.Fatalf("explicit official CLI reuse absent: %v", calls)
	}
}

func TestBatchHistoryDamageBlocksWithoutTouchingFiles(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		mutate     func(string, string) error
	}{
		{"missing", "missing", func(_, file string) error { return os.Remove(file) }},
		{"whole-directory-missing", "missing", func(dir, file string) error {
			return os.Rename(filepath.Dir(file), filepath.Join(dir, "preserved-session"))
		}},
		{"empty", "corrupt", func(_, file string) error { return os.WriteFile(file, nil, 0o600) }},
		{"truncated", "changed", func(_, file string) error { return os.Truncate(file, 10) }},
		{"same-size-rewrite", "changed", func(_, file string) error {
			//nolint:gosec // File is a synthetic history under t.TempDir.
			b, _ := os.ReadFile(file)
			b[len(b)-3] = 'x'
			return os.WriteFile(file, b, 0o600)
		}},
		{"malformed-tail", "corrupt", func(_, file string) error {
			//nolint:gosec // File is a synthetic history under t.TempDir.
			f, e := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
			if e != nil {
				return e
			}
			defer func() { _ = f.Close() }()
			_, e = f.WriteString("{broken}\n")
			return e
		}},
		{"broken-parent", "corrupt", func(_, file string) error {
			//nolint:gosec // File is a synthetic history under t.TempDir.
			f, e := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
			if e != nil {
				return e
			}
			defer func() { _ = f.Close() }()
			_, e = f.WriteString("{\"type\":\"session_info\",\"id\":\"tail\",\"parentId\":\"missing\",\"timestamp\":\"2026-10-22\"}\n")
			return e
		}},
		{"unreadable", "unreadable", func(_, file string) error { return os.Chmod(file, 0) }},
		{"duplicate-file", "unconfirmed", func(_, file string) error {
			//nolint:gosec // File is a synthetic history under t.TempDir.
			b, e := os.ReadFile(file)
			if e != nil {
				return e
			}
			return os.WriteFile(filepath.Join(filepath.Dir(file), "duplicate.jsonl"), b, 0o600)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _, dir, run := historyFixture(t)
			first, err := run(p)
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(dir, ".memoh-computer", "sessions", p.PiSessionID, first.History.Evidence.File)
			if err = tc.mutate(dir, file); err != nil {
				t.Fatal(err)
			}
			//nolint:gosec // File is a synthetic history under t.TempDir.
			before, _ := os.ReadFile(file)
			p.Attempt = strings.Repeat("e", 32)
			p.History = &BatchHistoryExpectation{Version: 1, Mode: "resume", Evidence: first.History.Evidence}
			r, err := run(p)
			if err != nil || r.Started || r.History.Before != tc.want || r.History.After != "not_started" {
				t.Fatalf("blocked: %+v %v", r, err)
			}
			//nolint:gosec // File is a synthetic history under t.TempDir.
			after, _ := os.ReadFile(file)
			if tc.name == "whole-directory-missing" {
				if _, err := os.Stat(filepath.Dir(file)); !os.IsNotExist(err) {
					t.Fatal("missing resume directory recreated")
				}
			}
			if string(before) != string(after) {
				t.Fatal("guard changed history")
			}
			if strings.Count(string(readBatchFixture(t, filepath.Join(dir, "calls"))), "\n") != 1 {
				t.Fatal("Pi started on damaged history")
			}
		})
	}
}

func TestBatchHistoryPostInvocationLossAndUnknownLegacy(t *testing.T) {
	t.Run("post-loss", func(t *testing.T) {
		p, _, dir, run := historyFixture(t)
		if err := os.WriteFile(filepath.Join(dir, "delete"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		r, err := run(p)
		if err != nil || !r.Started || r.History.After != "missing" || r.History.Evidence != nil {
			t.Fatalf("post loss: %+v %v", r, err)
		}
	})
	for _, mode := range []string{"first", "unconfirmed"} {
		t.Run(mode, func(t *testing.T) {
			p, _, dir, run := historyFixture(t)
			first, err := run(p)
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(dir, ".memoh-computer", "sessions", p.PiSessionID, first.History.Evidence.File)
			original := readBatchFixture(t, file)
			p.Attempt = strings.Repeat("e", 32)
			p.History = &BatchHistoryExpectation{Version: 1, Mode: mode}
			r, err := run(p)
			if err != nil || r.Started || r.History.Before != "unconfirmed" {
				t.Fatalf("legacy: %+v %v", r, err)
			}
			if string(original) != string(readBatchFixture(t, file)) {
				t.Fatal("legacy history changed")
			}
		})
	}
}

func TestBatchHistoryReceiptRequiresFileEvidence(t *testing.T) {
	p, _, _, run := historyFixture(t)
	r, err := run(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*BatchReceipt){
		func(r *BatchReceipt) { r.History = nil },
		func(r *BatchReceipt) { r.History.Evidence.File = "../other.jsonl" },
		func(r *BatchReceipt) { r.History.Evidence.Hash = "nonce-is-not-history" },
		func(r *BatchReceipt) { r.History.After = "verified" },
	} {
		raw, _ := json.Marshal(r)
		var bad BatchReceipt
		_ = json.Unmarshal(raw, &bad)
		mutate(&bad)
		if bad.Validate(p) == nil {
			t.Fatal("invalid continuity receipt accepted")
		}
	}
}

func TestBatchHistoryStaleLockNeverStolen(t *testing.T) {
	p, _, dir, run := historyFixture(t)
	first, err := run(p)
	if err != nil {
		t.Fatal(err)
	}
	session := filepath.Join(dir, ".memoh-computer", "sessions", p.PiSessionID)
	lock := filepath.Join(session, "lock")
	if err = os.Mkdir(lock, 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(lock, "owner"), []byte("uncertain-owner"), 0o600); err != nil {
		t.Fatal(err)
	}
	p.Attempt = strings.Repeat("e", 32)
	p.History = &BatchHistoryExpectation{Version: 1, Mode: "resume", Evidence: first.History.Evidence}
	r, err := run(p)
	if err != nil || r.Started || r.History.Before != "not_checked" {
		t.Fatalf("stale lock: %+v %v", r, err)
	}
	if string(readBatchFixture(t, filepath.Join(lock, "owner"))) != "uncertain-owner" || strings.Count(string(readBatchFixture(t, filepath.Join(dir, "calls"))), "\n") != 1 {
		t.Fatal("stale lock stolen or business replayed")
	}
}
