// Package deliverymeta defines non-secret producer handoff data. It owns no
// filesystem root, credential, task scheduler or persistent registry.
package deliverymeta

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

const ManifestPrefix = "memoh_delivery_"

var (
	reservedName = regexp.MustCompile(`^(COM|LPT)[1-9]$`)
	logicalID    = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	hex32        = regexp.MustCompile(`^[a-f0-9]{32}$`)
	hex64        = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// Artifact selects one explicitly requested closed result, never a directory
// scan of task outputs, logs, configuration or arbitrary producer paths.
type Artifact struct {
	SourceRef string `json:"source_ref"`
	Mime      string `json:"mime"`
}

func (a Artifact) Valid() bool {
	name := strings.ToUpper(a.SourceRef)
	if name == "CON" || name == "PRN" || name == "AUX" || name == "NUL" || reservedName.MatchString(name) {
		return false
	}
	kind, params, err := mime.ParseMediaType(a.Mime)
	return logicalID.MatchString(a.SourceRef) && !strings.HasPrefix(a.SourceRef, ManifestPrefix) && err == nil && len(params) == 0 && kind == a.Mime && len(a.Mime) <= 255
}

// Task comes from the original durable operation/BatchProgress, not a new
// delivery attempt. Selectors are reauthorized by Server; this is not authority.
type Task struct {
	OperationID string `json:"operation_id"`
	BotID       string `json:"bot_id"`
	UserID      string `json:"user_id"`
	SessionID   string `json:"session_id"`
	TargetID    string `json:"target_id"`
	RunID       string `json:"run_id"`
	Attempt     string `json:"attempt"`
	Generation  string `json:"generation"`
	RootHash    string `json:"root_hash"`
}

func (t Task) Valid() bool {
	for _, id := range []string{t.OperationID, t.BotID, t.UserID, t.SessionID, t.TargetID} {
		u, err := uuid.Parse(id)
		if err != nil || u.String() != id {
			return false
		}
	}
	return t.RunID != "" && len(t.RunID) <= 256 && hex32.MatchString(t.Attempt) && hex32.MatchString(t.Generation) && hex64.MatchString(t.RootHash)
}

// RootHash checks that existing execution and producer configuration agree.
// It is a consistency check, never a lookup root or an authorization claim.
func RootHash(root string) string {
	normalized := strings.ToLower(strings.TrimRight(strings.ReplaceAll(root, "/", `\`), `\`))
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

type Plan struct {
	Version  int      `json:"version"`
	Task     Task     `json:"task"`
	Artifact Artifact `json:"artifact"`
}

func (p Plan) Valid() bool { return p.Version == 1 && p.Task.Valid() && p.Artifact.Valid() }
func (p Plan) DeliveryID() string {
	raw, _ := json.Marshal(struct{ Operation, Attempt, Generation, Source string }{p.Task.OperationID, p.Task.Attempt, p.Task.Generation, p.Artifact.SourceRef})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (p Plan) ManifestName() string { return ManifestPrefix + p.DeliveryID() }

type Closed struct {
	Plan
	Closed    bool   `json:"closed"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

func (c Closed) Valid() bool {
	return c.Plan.Valid() && c.Closed && hex64.MatchString(c.SHA256) && c.SizeBytes > 0 && c.SizeBytes <= 1<<40
}
