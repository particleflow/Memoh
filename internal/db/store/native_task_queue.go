package store

import (
	"encoding/json"
	"time"
)

// NativeTaskReceipt is an admitted native tool request, not a session/run alias.
// Its original identity survives the later allocation of an execution run.
type NativeTaskReceipt struct {
	ResultDeliveryVersion int             `json:"result_delivery_version,omitempty"`
	ResultDelivery        json.RawMessage `json:"result_delivery,omitempty"`
	ReturnRoute           json.RawMessage `json:"return_route,omitempty"`
	Version               int             `json:"version"`
	CreatedAt             time.Time       `json:"created_at,omitempty"`
	ID                    string          `json:"id"`
	TaskID                string          `json:"task_id"`
	SourceRunID           string          `json:"source_run_id"`
	BotID                 string          `json:"bot_id"`
	ParentSessionID       string          `json:"parent_session_id"`
	UserID                string          `json:"user_id"`
	SessionID             string          `json:"session_id"`
	AgentID               string          `json:"agent_id"`
	CallID                string          `json:"call_id"`
	CallFingerprint       string          `json:"call_fingerprint"`
	Fingerprint           string          `json:"fingerprint"`
	State                 string          `json:"state"`
	ClaimToken            string          `json:"claim_token,omitempty"`
	RunID                 string          `json:"run_id,omitempty"`
	Message               string          `json:"message"`
	Description           string          `json:"description"`
	Config                json.RawMessage `json:"config"`
	Parent                json.RawMessage `json:"parent"`
}
