package transferclient

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
)

// DeviceTask contains only task lookup selectors obtained from the existing
// execution handoff. It neither authenticates the PC nor grants access. The
// server checks all selectors against the operation and current target grants.
type DeviceTask struct {
	UserID     string `json:"user_id"`
	SessionID  string `json:"session_id"`
	TargetID   string `json:"target_id"`
	RunID      string `json:"run_id"`
	Attempt    string `json:"attempt"`
	Generation string `json:"generation"`
}

// NewDeviceTask sends the SAME configured device credential for any delivery.
// Its per-request metadata is not a signed token; it confers no authority.
func NewDeviceTask(base string, job Job, direction string, task DeviceTask, client *http.Client, auth Authorization) (*Client, error) {
	if !job.valid() || direction != "upload" && direction != "download" || !task.valid() {
		return nil, ErrInvalid
	}
	c, err := NewTask(base, job.OperationID, job.DeliveryID, client, auth)
	if err != nil {
		return nil, err
	}
	// Keep this wire-only shape independent of the server/runtime packages.
	selection := map[string]any{
		"identity": map[string]any{
			"Binding":     map[string]string{"BotID": job.BotID, "UserID": task.UserID, "SessionID": task.SessionID, "TargetID": task.TargetID, "RunID": task.RunID},
			"OperationID": job.OperationID, "Attempt": task.Attempt, "Generation": task.Generation,
		},
		"reference": map[string]any{"version": 1, "direction": direction, "content_hash": job.Spec.SHA256, "size_bytes": job.Spec.SizeBytes, "mime": job.Spec.Mime},
	}
	raw, err := json.Marshal(selection)
	if err != nil || len(raw) > 4608 {
		return nil, ErrInvalid
	}
	c.selection = base64.RawURLEncoding.EncodeToString(raw)
	return c, nil
}

func (task DeviceTask) valid() bool {
	return validID(task.UserID) && validID(task.SessionID) && validID(task.TargetID) && task.RunID != "" && len(task.RunID) <= 256 && len(task.Attempt) == 32 && len(task.Generation) == 32
}
