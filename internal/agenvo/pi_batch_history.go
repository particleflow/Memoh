package agenvo

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
)

const BatchHistoryVersion = 1

// BatchHistoryExpectation is selected from durable operations after claiming the
// existing unsettled slot. Absence of local files alone never proves first use.
type BatchHistoryExpectation struct {
	Version  int                   `json:"version"`
	Mode     string                `json:"mode"`
	Evidence *BatchHistoryEvidence `json:"evidence,omitempty"`
}

// File is a basename relative to resolveBatchLayout's session directory. The
// byte prefix anchors history independently of Pi's best-effort ID discovery.
type BatchHistoryEvidence struct {
	File string `json:"file"`
	Size int64  `json:"size"`
	Hash string `json:"sha256"`
}

type BatchHistoryReport struct {
	Before   string                `json:"before"`
	After    string                `json:"after"`
	Evidence *BatchHistoryEvidence `json:"evidence,omitempty"`
}

var historyFile = regexp.MustCompile(`^[A-Za-z0-9_-]{1,180}\.jsonl$`)

func (e *BatchHistoryEvidence) Valid() bool {
	return e != nil && historyFile.MatchString(e.File) && e.Size > 0 && e.Size <= 64<<20 && batchHex.MatchString(e.Hash)
}

func (h *BatchHistoryExpectation) valid() bool {
	if h == nil {
		return true
	} // persisted legacy operations remain readable
	if h.Version != BatchHistoryVersion {
		return false
	}
	switch h.Mode {
	case "first", "unconfirmed":
		return h.Evidence == nil
	case "resume":
		return h.Evidence.Valid()
	}
	return false
}

func historyFailure(s string) bool {
	switch s {
	case "missing", "corrupt", "unreadable", "changed", "unconfirmed", "not_checked":
		return true
	}
	return false
}

func (h *BatchHistoryReport) valid(p BatchProgress, started bool) bool {
	if p.History == nil {
		return h == nil
	}
	if h == nil {
		return false
	}
	if !started {
		return historyFailure(h.Before) && h.After == "not_started" && h.Evidence == nil
	}
	if h.Before != "new" && h.Before != "verified" {
		return false
	}
	if (p.History.Mode == "first") != (h.Before == "new") || p.History.Mode == "unconfirmed" {
		return false
	}
	if h.After == "created" || h.After == "verified" {
		if !h.Evidence.Valid() || (h.After == "created") != (h.Before == "new") {
			return false
		}
		return p.History.Evidence == nil || h.Evidence.File == p.History.Evidence.File && h.Evidence.Size >= p.History.Evidence.Size
	}
	return historyFailure(h.After) && h.Evidence == nil
}

//go:embed pi_history.cjs
var piHistoryAdapter string

func historyScriptParts(p BatchProgress) (prepare, before, after, receipt string) {
	if p.History == nil {
		return "", "", "", ""
	}
	var packed bytes.Buffer
	zipper := gzip.NewWriter(&packed)
	_, _ = zipper.Write([]byte(piHistoryAdapter))
	_ = zipper.Close()
	program := fmt.Sprintf("eval(require('node:zlib').gunzipSync(Buffer.from('%s','base64')).toString('utf8'));", base64.StdEncoding.EncodeToString(packed.Bytes()))
	context, _ := json.Marshal(struct {
		ID      string                   `json:"id"`
		History *BatchHistoryExpectation `json:"history"`
	}{p.PiSessionID, p.History})
	prepare = fmt.Sprintf(`printf '%%s' '%s' | base64 --decode > "$memoh_attempt/history-reader.cjs"
printf '%%s' '%s' | base64 --decode > "$memoh_attempt/history-context.json"
memoh_history='{"before":"not_checked","after":"not_started"}'
`, base64.StdEncoding.EncodeToString([]byte(program)), base64.StdEncoding.EncodeToString(context))
	before = `  if node "$memoh_attempt/history-reader.cjs" before "$(cygpath -w "$memoh_session")"; then
    if [ -f "$memoh_attempt/history-file.txt" ]; then
      memoh_history_file="$(cat "$memoh_attempt/history-file.txt")"
      memoh_session_args=(--session "$(cygpath -w "$memoh_session/$memoh_history_file")")
    fi
`
	after = `  else
    memoh_exit=125
  fi
  node "$memoh_attempt/history-reader.cjs" after "$(cygpath -w "$memoh_session")" "$memoh_started"
  memoh_history="$(cat "$memoh_attempt/history-report.json")"
`
	receipt = `  printf ',"history":%s' "$memoh_history"
`
	return prepare, before, after, receipt
}
