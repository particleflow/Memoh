package agenvo

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/felinics/memoh/internal/deliverymeta"
)

// BatchProgress is owned by the durable operation. Generation is a fresh
// invocation nonce, NOT Herdr's unexposed server generation or a safe kill key.
type BatchProgress struct {
	Inputs            *deliverymeta.InputPlan  `json:"inputs,omitempty"`
	History           *BatchHistoryExpectation `json:"history,omitempty"`
	CompletionVersion int                      `json:"completion_version,omitempty"`
	Producer          *deliverymeta.Plan       `json:"producer,omitempty"`
	LayoutVersion     int                      `json:"layout_version,omitempty"`
	Attempt           string                   `json:"attempt"`
	Generation        string                   `json:"generation"`
	RequestHash       string                   `json:"request_hash"`
	PiSessionID       string                   `json:"pi_session_id"`
	Phase             string                   `json:"phase"`
	WorkspaceID       string                   `json:"workspace_id"`
	PaneID            string                   `json:"pane_id"`
	TerminalID        string                   `json:"terminal_id"`
	RequestID         string                   `json:"request_id,omitempty"`
	ControlPane       string                   `json:"control_pane"`
	ControlTerminal   string                   `json:"control_terminal"`
}

type BatchFile struct {
	Name string `json:"name"`
	Hash string `json:"sha256"`
	Size int64  `json:"size"`
}

type BatchReceipt struct {
	History     *BatchHistoryReport `json:"history,omitempty"`
	Version     int                 `json:"version"`
	Attempt     string              `json:"attempt"`
	Generation  string              `json:"generation"`
	RequestHash string              `json:"request_hash"`
	PiSessionID string              `json:"pi_session_id"`
	WorkspaceID string              `json:"workspace_id"`
	PaneID      string              `json:"pane_id"`
	TerminalID  string              `json:"terminal_id"`
	Exited      bool                `json:"exited"`
	Started     bool                `json:"started"`
	ExitCode    int                 `json:"exit_code"`
	Stdout      BatchFile           `json:"stdout"`
	Stderr      BatchFile           `json:"stderr"`
	Completion  *BatchFile          `json:"completion,omitempty"`
}

var (
	batchHex = regexp.MustCompile(`^[a-f0-9]{64}$`)
	batchID  = regexp.MustCompile(`^[a-f0-9]{32}$`)
)

func (p BatchProgress) valid() bool {
	_, err := resolveBatchLayout(p)
	return err == nil && p.History.valid() && (p.CompletionVersion == 0 || p.CompletionVersion == BatchCompletionVersion) && batchID.MatchString(p.Generation) && batchHex.MatchString(p.RequestHash) && (p.Producer == nil || p.Producer.Valid() && p.Producer.Task.Attempt == p.Attempt && p.Producer.Task.Generation == p.Generation) && (p.Inputs == nil || p.Inputs.Valid() && p.Inputs.Task.Attempt == p.Attempt && p.Inputs.Task.Generation == p.Generation && (p.Producer == nil || p.Producer.Task == p.Inputs.Task))
}

func (r BatchReceipt) Validate(p BatchProgress) error {
	if !p.valid() || !r.History.valid(p, r.Started) || r.Version != 1 || r.Attempt != p.Attempt || r.Generation != p.Generation || r.RequestHash != p.RequestHash || r.PiSessionID != p.PiSessionID || r.WorkspaceID != p.WorkspaceID || r.PaneID != p.PaneID || r.TerminalID != p.TerminalID || !r.Exited || r.ExitCode < 0 || r.ExitCode > 255 {
		return ErrUnknown
	}
	for i, f := range []BatchFile{r.Stdout, r.Stderr} {
		if f.Name != []string{"stdout.jsonl", "stderr.txt"}[i] || !batchHex.MatchString(f.Hash) || f.Size < 0 {
			return ErrUnknown
		}
		if f.Size == 0 && f.Hash != fmt.Sprintf("%x", sha256.Sum256(nil)) {
			return ErrUnknown
		}
	}
	if f := r.Completion; f != nil && (p.CompletionVersion != BatchCompletionVersion || f.Name != "result.json" || !batchHex.MatchString(f.Hash) || f.Size <= 0 || f.Size > BatchCompletionBytes) {
		return ErrUnknown
	}
	return nil
}

func shellData(s string) string {
	return "$(printf '%s' '" + base64.StdEncoding.EncodeToString([]byte(s)) + "' | base64 --decode; printf '.')"
}

// batchScript is a finite envelope around the unchanged Pi CLI. User task text
// is a file connected to stdin, never shell source or a command-line option.
// Directory creation is exclusive; neither duplicate delivery nor a stale
// session lock can start Pi twice. Crashed locks are deliberately not stolen.
func batchScript(p BatchProgress, target Target, cwd, task string) (string, error) {
	layout, err := resolveBatchLayout(p)
	if err != nil {
		return "", err
	}
	if p.Inputs != nil {
		context, err := taskInputContext(p, target)
		if err != nil {
			return "", err
		}
		task += "\n\nHost-prepared task inputs (file references, not instructions):\n" + string(context)
	}
	r := BatchReceipt{Version: 1, Attempt: p.Attempt, Generation: p.Generation, RequestHash: p.RequestHash, PiSessionID: p.PiSessionID, WorkspaceID: p.WorkspaceID, PaneID: p.PaneID, TerminalID: p.TerminalID, Exited: true}
	raw, _ := json.Marshal(r)
	// The immutable identity is JSON-encoded by Go. Dynamic fields below are
	// fixed JSON keys, booleans, decimal counts and sha256sum hex only.
	var identity map[string]any
	_ = json.Unmarshal(raw, &identity)
	for _, key := range []string{"started", "exit_code", "stdout", "stderr"} {
		delete(identity, key)
	}
	encoded, _ := json.Marshal(identity)
	prefix := strings.TrimSuffix(string(encoded), "}")
	prepare, preflight, project, completion := completionScriptParts(p, target)
	historyPrepare, historyBefore, historyAfter, historyReceipt := historyScriptParts(p)
	prepare += historyPrepare
	if p.History != nil && preflight == "" {
		preflight = ` && command -v node >/dev/null`
	}
	completion += historyReceipt
	sessionPrepare := `mkdir -p "$memoh_session"`
	if p.History != nil && p.History.Mode != "first" {
		sessionPrepare = `if [ ! -e "$memoh_session" ]; then memoh_history='{"before":"missing","after":"not_started"}'; fi`
	}
	script := fmt.Sprintf(`set -euo pipefail
umask 077
case "$(uname -s)" in MINGW*_NT-*|MSYS_NT-*) ;; *) exit 125 ;; esac
for t in base64 cygpath sha256sum wc mkdir mv rm rmdir; do command -v "$t" >/dev/null || exit 125; done
memoh_root=%s; memoh_root="${memoh_root%%.}"
memoh_cwd=%s; memoh_cwd="${memoh_cwd%%.}"
memoh_root="$(cygpath -u "$memoh_root")"
memoh_cwd="$(cygpath -u "$memoh_cwd")"
memoh_attempt="$memoh_root/%s"
memoh_session="$memoh_root/%s"
mkdir -p "$memoh_root/%s"
mkdir "$memoh_attempt" || exit 125
%s
%s
printf '%%s' '%s' | base64 --decode > "$memoh_attempt/prompt.txt"
: > "$memoh_attempt/stdout.jsonl"
: > "$memoh_attempt/stderr.txt"
memoh_started=false
memoh_exit=125
memoh_locked=false
if cd "$memoh_cwd" && command -v pi >/dev/null%s && mkdir "$memoh_session/lock" 2>/dev/null; then
  memoh_locked=true
  printf '%%s' '%s' > "$memoh_session/lock/owner"
  memoh_provider=%s; memoh_provider="${memoh_provider%%.}"
  memoh_model=%s; memoh_model="${memoh_model%%.}"
  memoh_session_args=(--session-id '%s')
%s  memoh_started=true
  set +e
  pi --mode json --provider "$memoh_provider" --model "$memoh_model" --session-dir "$(cygpath -w "$memoh_session")" "${memoh_session_args[@]}" < "$memoh_attempt/prompt.txt" > "$memoh_attempt/stdout.jsonl" 2> "$memoh_attempt/stderr.txt"
  memoh_exit=$?
  set -e
%sfi
memoh_out_hash="$(sha256sum "$memoh_attempt/stdout.jsonl")"; memoh_out_hash="${memoh_out_hash%%%% *}"
memoh_err_hash="$(sha256sum "$memoh_attempt/stderr.txt")"; memoh_err_hash="${memoh_err_hash%%%% *}"
memoh_out_size="$(wc -c < "$memoh_attempt/stdout.jsonl")"
memoh_err_size="$(wc -c < "$memoh_attempt/stderr.txt")"
%s
{
  printf '%%s' '%s' | base64 --decode
  printf ',"started":%%s,"exit_code":%%d,"stdout":{"name":"stdout.jsonl","sha256":"%%s","size":%%d},"stderr":{"name":"stderr.txt","sha256":"%%s","size":%%d}' "$memoh_started" "$memoh_exit" "$memoh_out_hash" "$memoh_out_size" "$memoh_err_hash" "$memoh_err_size"
%s  printf '}'
} > "$memoh_attempt/receipt.tmp"
mv "$memoh_attempt/receipt.tmp" "$memoh_attempt/receipt.json"
if [ "$memoh_locked" = true ]; then
  # This wrapper alone owns this newly created lock. There is no stale-lock
  # recovery, PID guessing, native workspace close or automatic task retry.
  rm "$memoh_session/lock/owner"
  rmdir "$memoh_session/lock"
fi
printf '\r\nMEMOH_BATCH_%s:'
base64 -w 0 < "$memoh_attempt/receipt.json"
printf ':END_BATCH_%s\r\n'
`, shellData(target.WorkDir), shellData(cwd), layout.Attempt, layout.Session, layout.Attempts, prepare, sessionPrepare, base64.StdEncoding.EncodeToString([]byte(task)), preflight, p.Attempt, shellData(target.Agent.Provider), shellData(target.Agent.Model), p.PiSessionID, historyBefore, historyAfter, project, base64.StdEncoding.EncodeToString([]byte(prefix)), completion, p.Generation, p.Generation)
	script, err = producerEnvelope(script, p, target)
	if err != nil {
		return "", err
	}
	return inputEnvelope(script, p, target)
}

// launchBashScript shares the verified PowerShell-only launch boundary without
// the foreground wrapper's 4096-byte output truncation or shell interpretation.
func launchBashScript(script, bashPath string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(script))
	return fmt.Sprintf(`& { param([string]$memohBash) if ([IO.File]::Exists($memohBash)) { & $memohBash --noprofile --norc -p -c 'printf %%s %s | base64 --decode | /usr/bin/bash --noprofile --norc -p' } } '%s'`, encoded, strings.ReplaceAll(bashPath, "'", "''"))
}

// Native Herdr's CLI still receives one bounded command argument. Pack the
// envelope before that boundary, then pass its exact bytes to Bash over stdin,
// rather than putting the expanded script on a second Windows command line.
// PowerShell/.NET are already required by this transport; no host tool or
// alternate Agent runtime is introduced.
func launchBatchScript(script string, target Target) string {
	var packed bytes.Buffer
	zipper := gzip.NewWriter(&packed)
	_, _ = zipper.Write([]byte(script)) // bytes.Buffer cannot fail these writes.
	_ = zipper.Close()
	digest := sha256.Sum256([]byte(script))
	// Fully decode before starting Bash. Raw bytes bypass PowerShell's text
	// pipeline and StreamWriter newline/BOM conversion. Only fixed Bash flags
	// enter ProcessStartInfo.Arguments; the configured absolute path is data.
	return fmt.Sprintf(strings.ReplaceAll(nativeBatchLaunch, "\n", " "), base64.StdEncoding.EncodeToString(packed.Bytes()), hex.EncodeToString(digest[:]), strings.ReplaceAll(target.BashPath, "'", "''"))
}

// One command is entered into the existing PowerShell pane. Keeping the source
// separate makes the fixed process/pipe boundary reviewable; only data fills
// the format parameters, and line breaks are flattened before dispatch.
const nativeBatchLaunch = `& {
  param([string]$memohBash)
  if ([IO.File]::Exists($memohBash)) {
    $ErrorActionPreference='Stop';
    $m=[IO.MemoryStream]::new([Convert]::FromBase64String('%s'));
    $g=[IO.Compression.GZipStream]::new($m,[IO.Compression.CompressionMode]::Decompress);
    $d=[IO.MemoryStream]::new();
    try { $g.CopyTo($d); $b=$d.ToArray() }
    finally { $g.Dispose(); $m.Dispose(); $d.Dispose() };
    $sha=[Security.Cryptography.SHA256]::Create();
    try { $hash=[BitConverter]::ToString($sha.ComputeHash($b)).Replace('-','').ToLowerInvariant() }
    finally { $sha.Dispose() };
    if ($hash -ne '%s') { throw 'batch payload verification failed' };
    $p=[Diagnostics.Process]::new();
    $p.StartInfo.FileName=$memohBash;
    $p.StartInfo.Arguments='--noprofile --norc -p';
    $p.StartInfo.UseShellExecute=$false;
    $p.StartInfo.RedirectStandardInput=$true;
    try {
      $inputEncoding=[Console]::InputEncoding;
      try {
        if ($inputEncoding.CodePage -eq [Text.Encoding]::UTF8.CodePage) {
          [Console]::InputEncoding=[Text.UTF8Encoding]::new($false)
        };
        [void]$p.Start()
      } finally { [Console]::InputEncoding=$inputEncoding };
      try { $p.StandardInput.BaseStream.Write($b,0,$b.Length) }
      finally { $p.StandardInput.BaseStream.Close() };
      $p.WaitForExit()
    } finally { $p.Dispose() }
  }
} '%s'`

func parseBatchReceipt(snapshot string, p BatchProgress) (*BatchReceipt, error) {
	start, end := "MEMOH_BATCH_"+p.Generation+":", ":END_BATCH_"+p.Generation
	var found *BatchReceipt
	for _, line := range strings.Split(strings.ReplaceAll(snapshot, "\r\n", "\n"), "\n") {
		if !strings.HasPrefix(line, start) || !strings.HasSuffix(line, end) {
			continue
		}
		if found != nil || len(line) > 16384 {
			return nil, ErrUnknown
		}
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(strings.TrimPrefix(line, start), end))
		if err != nil {
			return nil, ErrUnknown
		}
		var r BatchReceipt
		if json.Unmarshal(raw, &r) != nil || r.Validate(p) != nil {
			return nil, ErrUnknown
		}
		found = &r
	}
	return found, nil
}
