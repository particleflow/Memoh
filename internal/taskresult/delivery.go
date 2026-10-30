// Package taskresult defines host-owned result delivery receipts. It does not
// execute tasks: a retry here can only request another summary of saved data.
package taskresult

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const MetadataKey = "task_result_receipt"

type Reference struct {
	Kind, TaskID, EventID string
	Attempt               int
}

func (r Reference) String() string {
	return fmt.Sprintf("%s:%s:%s:%d", r.Kind, r.TaskID, r.EventID, r.Attempt)
}

func Parse(s string) (Reference, error) {
	p := strings.Split(s, ":")
	if len(p) != 4 || (p[0] != "native_task" && p[0] != "bot_delegation" && p[0] != "schedule_fire") || p[1] == "" || len(p[2]) != 64 {
		return Reference{}, errors.New("invalid task result reference")
	}
	n, e := strconv.Atoi(p[3])
	if e != nil || n < 1 {
		return Reference{}, errors.New("invalid summary attempt")
	}
	if _, e = hex.DecodeString(p[2]); e != nil {
		return Reference{}, e
	}
	return Reference{p[0], p[1], p[2], n}, nil
}

// Events are retained separately when an uncertain execution later resolves.
// Attempt names only the summary invocation, never the original action.
type Event struct {
	ReportState       string     `json:"report_state"`
	State             string     `json:"state"`
	RunID             string     `json:"run_id,omitempty"`
	Text              string     `json:"text"`
	Attempt           int        `json:"attempt"`
	MessageID         string     `json:"message_id,omitempty"`
	SummaryRunID      string     `json:"summary_run_id,omitempty"`
	Recoveries        []Recovery `json:"recoveries,omitempty"`
	AutomaticAttempts int        `json:"automatic_attempts,omitempty"`
}

// Each explicit recovery authorizes one additional summary, never an action.
// Attempts remain monotonic; terminal run records retain every failed attempt.
type Recovery struct {
	FromAttempt      int    `json:"from_attempt"`
	Attempt          int    `json:"attempt"`
	RequestedBy      string `json:"requested_by"`
	RequestedAt      string `json:"requested_at"`
	PreviousRunID    string `json:"previous_run_id"`
	PreviousRunState string `json:"previous_run_state"`
}

// RecoveryAttempt derives the one-attempt grant from its durable audit. It
// applies even when attention occurred before the automatic retry budget ended.
func (e Event) RecoveryAttempt() bool {
	for _, r := range e.Recoveries {
		if r.Attempt == e.Attempt && r.FromAttempt == e.Attempt-1 && r.RequestedBy != "" && r.RequestedAt != "" && r.PreviousRunID != "" && Terminal(r.PreviousRunState) {
			return true
		}
	}
	return false
}

type Delivery map[string]Event

func EventID(state, run, text string) string {
	b, _ := json.Marshal([]string{state, run, text})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func Terminal(state string) bool {
	return state == "completed" || state == "failed" || state == "aborted" || state == "lost"
}
