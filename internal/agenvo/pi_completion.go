package agenvo

import (
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

const (
	BatchCompletionVersion = 1
	BatchCompletionBytes   = 8192
)

//go:embed pi_result.cjs
var piResultAdapter string

// BatchCompletion contains a local post-invocation projection, not full logs
// or independently verified completion of the user's real-world objective.
type BatchCompletion struct {
	Version     int          `json:"version"`
	Attempt     string       `json:"attempt"`
	Generation  string       `json:"generation"`
	RequestHash string       `json:"request_hash"`
	PiSessionID string       `json:"pi_session_id"`
	WorkspaceID string       `json:"workspace_id"`
	PaneID      string       `json:"pane_id"`
	TerminalID  string       `json:"terminal_id"`
	Stdout      BatchFile    `json:"stdout"`
	Result      PiJSONResult `json:"result"`
}

func ParseBatchCompletion(raw []byte, p BatchProgress, r BatchReceipt) (PiJSONResult, error) {
	if p.CompletionVersion != BatchCompletionVersion || r.Validate(p) != nil || r.Completion == nil || !r.Started || len(raw) > BatchCompletionBytes || !utf8.Valid(raw) {
		return PiJSONResult{}, ErrUnknown
	}
	sum := sha256.Sum256(raw)
	if int64(len(raw)) != r.Completion.Size || hex.EncodeToString(sum[:]) != r.Completion.Hash {
		return PiJSONResult{}, ErrUnknown
	}
	var c BatchCompletion
	if json.Unmarshal(raw, &c) != nil || c.Version != BatchCompletionVersion || c.Attempt != p.Attempt || c.Generation != p.Generation || c.RequestHash != p.RequestHash || c.PiSessionID != p.PiSessionID || c.WorkspaceID != p.WorkspaceID || c.PaneID != p.PaneID || c.TerminalID != p.TerminalID || c.Stdout != r.Stdout || len(c.Result.Output) > 2048 || c.Result.Records < 0 {
		return PiJSONResult{}, ErrUnknown
	}
	if ValidateBatchCompletionResult(c.Result, p, r) != nil {
		return PiJSONResult{}, ErrUnknown
	}
	return c.Result, nil
}

// ValidateBatchCompletionResult checks the same bounded result facts for both
// ingestion and read-only projections of already verified, persisted receipts.
// It does not re-read files or independently verify a business outcome.
func ValidateBatchCompletionResult(result PiJSONResult, p BatchProgress, r BatchReceipt) error {
	if p.CompletionVersion != BatchCompletionVersion || r.Validate(p) != nil || !r.Started || r.Completion == nil || len(result.Output) > 2048 || !utf8.ValidString(result.Output) || result.Records < 0 {
		return ErrUnknown
	}
	valid := false
	switch result.Outcome {
	case "succeeded":
		valid = r.ExitCode == 0 && result.StopReason == "stop" && result.Records >= 8
	case "failed":
		valid = (r.ExitCode != 0 || result.StopReason == "error") && result.Records >= 8
	case "aborted":
		valid = result.Records >= 8
	case "incomplete":
		valid = (result.StopReason == "length" || result.StopReason == "toolUse") && result.Records >= 8
	case "invalid_protocol", "output_limit":
		valid = result.Output == "" && result.StopReason == "" && !result.Truncated
	}
	if !valid {
		return ErrUnknown
	}
	return nil
}

// The adapter is finite deployment code written into this exclusive attempt,
// outside Pi's installation/configuration. Node is an explicit preflight
// requirement; missing Node cannot launch Pi and cannot select another runtime.
func completionScriptParts(p BatchProgress, target Target) (prepare, preflight, project, receipt string) {
	if p.CompletionVersion != BatchCompletionVersion {
		return "", "", "", ""
	}
	identity := map[string]any{
		"attempt": p.Attempt, "generation": p.Generation, "request_hash": p.RequestHash,
		"pi_session_id": p.PiSessionID, "workspace_id": p.WorkspaceID, "pane_id": p.PaneID,
		"terminal_id": p.TerminalID, "provider": target.Agent.Provider, "model": target.Agent.Model,
	}
	context, _ := json.Marshal(identity)
	prepare = fmt.Sprintf(`printf '%%s' '%s' | base64 --decode > "$memoh_attempt/result-reader.cjs"
printf '%%s' '%s' | base64 --decode > "$memoh_attempt/result-context.json"
`, base64.StdEncoding.EncodeToString([]byte(piResultAdapter)), base64.StdEncoding.EncodeToString(context))
	preflight = ` && command -v node >/dev/null`
	project = `memoh_result_ready=false
if [ "$memoh_started" = true ] && node "$memoh_attempt/result-reader.cjs" "$memoh_exit"; then
  memoh_result_hash="$(sha256sum "$memoh_attempt/result.json")"; memoh_result_hash="${memoh_result_hash%% *}"
  memoh_result_size="$(wc -c < "$memoh_attempt/result.json")"
  memoh_result_ready=true
fi
`
	receipt = `  if [ "$memoh_result_ready" = true ]; then
    printf ',"completion":{"name":"result.json","sha256":"%s","size":%d}' "$memoh_result_hash" "$memoh_result_size"
  fi
`
	return prepare, preflight, project, receipt
}
