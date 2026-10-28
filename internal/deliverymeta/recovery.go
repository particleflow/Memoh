package deliverymeta

import "encoding/json"

// ResultRecovery is metadata from the authorized original operation owner, not
// a model tool argument, credential, new task or authority to upload. The local
// adapter verifies it against the original protected attempt before admission.
type ResultRecovery struct {
	Version              int             `json:"version"`
	Plan                 Plan            `json:"plan"`
	SourceRef            string          `json:"source_ref"`
	PhysicalName         string          `json:"physical_name"`
	SHA256               string          `json:"sha256"`
	SizeBytes            int64           `json:"size_bytes"`
	HostBinding          string          `json:"host_binding"`
	TargetFingerprint    string          `json:"target_fingerprint"`
	RequestHash          string          `json:"request_hash"`
	OperationFingerprint string          `json:"operation_fingerprint"`
	ContextHash          string          `json:"context_hash"`
	Receipt              json.RawMessage `json:"receipt"`
	RemoteOutcome        string          `json:"remote_outcome"`
	KnownReceipt         string          `json:"known_receipt,omitempty"`
}

func (r ResultRecovery) FilePlan() (Plan, bool) {
	if r.Version != 1 || !r.Plan.Valid() || !ValidSourceName(r.PhysicalName) ||
		!hex64.MatchString(r.SHA256) || r.SizeBytes <= 0 || r.SizeBytes > 1<<40 ||
		!hex64.MatchString(r.HostBinding) || !hex64.MatchString(r.TargetFingerprint) ||
		!hex64.MatchString(r.RequestHash) || !hex64.MatchString(r.OperationFingerprint) || !hex64.MatchString(r.ContextHash) ||
		len(r.Receipt) == 0 || len(r.Receipt) >= 4096 || !json.Valid(r.Receipt) {
		return Plan{}, false
	}
	if r.RemoteOutcome != "absent" && r.RemoteOutcome != "existing" && r.RemoteOutcome != "unknown" {
		return Plan{}, false
	}
	for _, p := range r.Plan.FilePlans() {
		if p.Artifact.SourceRef == r.SourceRef {
			return p, true
		}
	}
	return Plan{}, false
}
