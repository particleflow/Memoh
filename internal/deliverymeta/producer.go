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

const (
	ManifestPrefix = "memoh_delivery_"
	// MaxArtifacts bounds one declaration/feedback/closed page, not the total backlog.
	MaxArtifacts = 16
)

var (
	reservedName = regexp.MustCompile(`^(COM|LPT)[1-9]$`)
	sourceName   = regexp.MustCompile(`^[a-zA-Z0-9_-]+(\.[a-zA-Z0-9_-]+)*$`)
	hex32        = regexp.MustCompile(`^[a-f0-9]{32}$`)
	hex64        = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// Artifact selects one explicitly requested closed result, never a directory
// scan of task outputs, logs, configuration or arbitrary producer paths.
type Artifact struct {
	SourceRef string `json:"source_ref"`
	Mime      string `json:"mime"`
}

// ValidSourceName accepts an exact flat result filename, including an explicitly
// declared extension. It does not infer a suffix or resolve a filesystem path.
func ValidSourceName(ref string) bool {
	if len(ref) > 64 || !sourceName.MatchString(ref) || strings.HasPrefix(ref, ManifestPrefix) {
		return false
	}
	stem, _, _ := strings.Cut(ref, ".")
	name := strings.ToUpper(stem)
	if name == "CON" || name == "PRN" || name == "AUX" || name == "NUL" || reservedName.MatchString(name) {
		return false
	}
	return true
}

func (a Artifact) Valid() bool {
	kind, params, err := mime.ParseMediaType(a.Mime)
	return ValidSourceName(a.SourceRef) && err == nil && len(params) == 0 && kind == a.Mime && len(a.Mime) <= 255
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
	Version   int        `json:"version"`
	Task      Task       `json:"task"`
	Artifact  Artifact   `json:"artifact"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
}

// ValidArtifacts rejects case aliases too: the intended host is Windows.
func ValidArtifacts(artifacts []Artifact) bool {
	if len(artifacts) < 1 || len(artifacts) > MaxArtifacts {
		return false
	}
	seen := make(map[string]bool, len(artifacts))
	for _, a := range artifacts {
		name := strings.ToLower(a.SourceRef)
		if !a.Valid() || seen[name] {
			return false
		}
		seen[name] = true
	}
	return true
}

func (p Plan) Valid() bool {
	if !p.Task.Valid() {
		return false
	}
	switch p.Version {
	case 1:
		return p.Artifact.Valid() && len(p.Artifacts) == 0
	case 2:
		return p.Artifact == (Artifact{}) && ValidArtifacts(p.Artifacts)
	default:
		return false
	}
}

// FilePlans expands only declared metadata. Every closed manifest remains the
// existing version 1 single-file format, with identity independent of list order.
func (p Plan) FilePlans() []Plan {
	if !p.Valid() {
		return nil
	}
	if p.Version == 1 {
		return []Plan{p}
	}
	plans := make([]Plan, 0, len(p.Artifacts))
	for _, artifact := range p.Artifacts {
		plans = append(plans, Plan{Version: 1, Task: p.Task, Artifact: artifact})
	}
	return plans
}

func (p Plan) DeliveryID() string {
	if p.Version != 1 {
		return ""
	}
	raw, _ := json.Marshal(struct{ Operation, Attempt, Generation, Source string }{p.Task.OperationID, p.Task.Attempt, p.Task.Generation, p.Artifact.SourceRef})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (p Plan) ManifestName() string {
	if p.Version != 1 {
		return ""
	}
	return ManifestPrefix + p.DeliveryID()
}

type Closed struct {
	Plan
	Closed    bool   `json:"closed"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

func (c Closed) Valid() bool {
	return c.Version == 1 && c.Plan.Valid() && c.Closed && hex64.MatchString(c.SHA256) && c.SizeBytes > 0 && c.SizeBytes <= 1<<40
}
