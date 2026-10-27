package agenvo

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strconv"
	"strings"

	"github.com/google/uuid"

	dbstore "github.com/felinics/memoh/internal/db/store"
)

// NativeProfileCutoverRequest is a proposed owner control-plane contract.
// It is not wired to an endpoint or a write until an atomic admission-fenced
// database operation implements the same comparisons under the owner lock.
type NativeProfileCutoverRequest struct {
	RuntimeID                 string `json:"runtime_id"`
	TargetID                  string `json:"target_id"`
	InstanceID                string `json:"instance_id"`
	ExpectedConfigSHA256      string `json:"expected_config_sha256"`
	ExpectedTargetFingerprint string `json:"expected_target_fingerprint"`
	PauseRequestID            string `json:"pause_request_id"`
	ExpectedPauseRevision     string `json:"expected_pause_revision"`
	AgentConfigDir            string `json:"agent_config_dir"`
}

// DecodeNativeProfileCutover rejects unknown or concatenated request fields.
// A future handler must also apply its existing request-body size limit.
func DecodeNativeProfileCutover(raw []byte) (NativeProfileCutoverRequest, error) {
	var request NativeProfileCutoverRequest
	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		return request, ErrInvalidTarget
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, ErrInvalidTarget
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return request, ErrInvalidTarget
	}
	return request, nil
}

// nativeProfileConfigDigest hashes a canonical JSON value, not credentials in
// an API response. Preserve numbers without float conversion or truncation.
func nativeProfileConfigDigest(raw []byte) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil || value == nil {
		return "", ErrInvalidTarget
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", ErrInvalidTarget
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// ValidateNativeProfileCutover validates already authorized, transaction-read
// owner/runtime/target and gate facts. It returns a proposal only: callers must
// compare these facts again and persist atomically under the DB admission lock.
func ValidateNativeProfileCutover(owner, ownedTargetID string, row dbstore.UserRuntimeRecord, gate dbstore.ComputerAdmission, in NativeProfileCutoverRequest) (dbstore.JSON, error) {
	for _, value := range []string{in.RuntimeID, in.TargetID, in.PauseRequestID} {
		id, err := uuid.Parse(value)
		if err != nil || id == uuid.Nil || id.String() != value {
			return nil, ErrInvalidTarget
		}
	}
	revision, err := strconv.ParseInt(in.ExpectedPauseRevision, 10, 64)
	if err != nil || revision < 1 || strconv.FormatInt(revision, 10) != in.ExpectedPauseRevision {
		return nil, ErrInvalidTarget
	}
	if owner == "" || row.UserID != owner || row.ID != in.RuntimeID || ownedTargetID != in.TargetID || row.Provider != "memoh" || row.ActivatedAt.IsZero() {
		return nil, ErrForbidden
	}
	if !gate.Paused || gate.ConnectionID != row.ID || gate.DeviceID != row.ID || gate.InstanceID != in.InstanceID || gate.RequestID != in.PauseRequestID || gate.Revision != revision {
		return nil, ErrNativeProfileConflict
	}
	target, err := DecodeNativeTarget(row.ID, row.ProviderConfig)
	if err != nil || target.InstanceID != in.InstanceID || target.Agent.ExecutionMode != "pi-json-v1" {
		return nil, ErrNativeProfileConflict
	}
	digest, err := nativeProfileConfigDigest(row.ProviderConfig)
	if err != nil || digest != in.ExpectedConfigSHA256 || TargetFingerprint(target) != in.ExpectedTargetFingerprint {
		return nil, ErrNativeProfileConflict
	}
	if validateNativeLocalPath(in.AgentConfigDir) != nil || strings.Contains(in.AgentConfigDir, "/") || strings.Contains(in.AgentConfigDir[3:], `\\`) || len(in.AgentConfigDir) > 3 && strings.HasSuffix(in.AgentConfigDir, `\`) {
		return nil, ErrInvalidTarget
	}
	var cfg map[string]json.RawMessage
	if json.Unmarshal(row.ProviderConfig, &cfg) != nil {
		return nil, ErrInvalidTarget
	}
	var herdr map[string]json.RawMessage
	if json.Unmarshal(cfg["herdr"], &herdr) != nil || herdr == nil {
		return nil, ErrInvalidTarget
	}
	var agent map[string]json.RawMessage
	if json.Unmarshal(herdr["agent"], &agent) != nil || agent == nil {
		return nil, ErrInvalidTarget
	}
	agent["config_dir"], _ = json.Marshal(in.AgentConfigDir)
	herdr["agent"], err = json.Marshal(agent)
	if err != nil {
		return nil, ErrInvalidTarget
	}
	cfg["herdr"], err = json.Marshal(herdr)
	if err != nil {
		return nil, ErrInvalidTarget
	}
	return json.Marshal(cfg)
}
