package computertask

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/toolcontext"
)

// WithNativeObservationLimit applies validated startup configuration, not a
// caller-controlled quota. Lowering the limit preserves every existing record.
func (s *Service) WithNativeObservationLimit(limit int) *Service {
	if limit > 0 && limit <= 128 {
		s.nativeObservationLimit = limit
	}
	return s
}

type SampledInputReceipt struct {
	RequestID string `json:"request_id"`
	State     string `json:"input_state"`
}

// NativeObservationRecord is an immutable host sample, NOT a native execution
// or result ID. Session and terminal output are private; public history only
// projects metadata. Input receipts are sampled before polling, not ownership.
type NativeObservationRecord struct {
	ID              string                   `json:"id"`
	Fingerprint     string                   `json:"fingerprint"`
	Origin          string                   `json:"origin"`
	ReadStartedAt   string                   `json:"read_started_at,omitempty"`
	FirstObservedAt string                   `json:"first_observed_at,omitempty"`
	RecordedAt      string                   `json:"recorded_at"`
	Observation     agenvo.AgentObservation  `json:"observation"`
	Session         *agenvo.AgentSessionInfo `json:"agent_session,omitempty"`
	Inputs          []SampledInputReceipt    `json:"input_receipts_at_start,omitempty"`
}

type NativeExportRecord struct {
	ObservationID   string `json:"observation_id,omitempty"`
	ArtifactHash    string `json:"artifact_hash"`
	RecordedAt      string `json:"recorded_at"`
	BytesVerifiedAt string `json:"bytes_verified_at,omitempty"`
	Origin          string `json:"origin"`
}

type NativeEvidenceLedger struct {
	Version                   int                       `json:"version"`
	Limit                     int                       `json:"-"` // Derived from current server configuration.
	LatestObservationID       string                    `json:"latest_observation_id,omitempty"`
	LatestExportObservationID string                    `json:"latest_export_observation_id,omitempty"`
	LastSampledAt             string                    `json:"last_sampled_at,omitempty"`
	Observations              []NativeObservationRecord `json:"observations"`
	Exports                   []NativeExportRecord      `json:"exports,omitempty"`
}

type NativeObservationSummary struct {
	ID               string                `json:"observation_id"`
	Origin           string                `json:"origin"`
	ReadStartedAt    string                `json:"read_started_at,omitempty"`
	FirstObservedAt  string                `json:"first_observed_at,omitempty"`
	RecordedAt       string                `json:"recorded_at"`
	Status           string                `json:"agent_status"`
	CompletionSeq    string                `json:"completion_seq,omitempty"`
	Starting         bool                  `json:"starting"`
	InteractiveReady bool                  `json:"interactive_ready"`
	Truncated        bool                  `json:"truncated"`
	OutputFidelity   string                `json:"output_fidelity"`
	SessionRelation  string                `json:"session_relation"`
	SessionObserved  bool                  `json:"session_observed"`
	Inputs           []SampledInputReceipt `json:"input_receipts_at_start,omitempty"`
	Association      string                `json:"association"`
	ResultOwnership  string                `json:"verified_result_ownership"`
}

type NativeExportSummary struct {
	NativeExportRecord
	Availability    string `json:"current_availability"`
	ResultOwnership string `json:"verified_result_ownership"`
}

type NativeEvidenceResult struct {
	ExportCount               int                        `json:"export_count"`
	HistoryOffset             int                        `json:"history_offset"`
	NextHistoryOffset         *int                       `json:"next_history_offset,omitempty"`
	HistoryPartial            bool                       `json:"history_partial"`
	Limit                     int                        `json:"limit"`
	Count                     int                        `json:"count"`
	Full                      bool                       `json:"full"`
	RecordingState            string                     `json:"recording_state"`
	LatestObservationID       string                     `json:"latest_observation_id,omitempty"`
	LatestExportObservationID string                     `json:"latest_export_observation_id,omitempty"`
	LastSampledAt             string                     `json:"last_sampled_at,omitempty"`
	Observations              []NativeObservationSummary `json:"observations"`
	Exports                   []NativeExportSummary      `json:"exports,omitempty"`
}

func nativeEvidenceResult(ledger *NativeEvidenceLedger) *NativeEvidenceResult {
	if ledger == nil {
		return nil
	}
	offset := 0
	for i, sample := range ledger.Observations {
		if sample.ID == ledger.LatestObservationID {
			offset = i
			break
		}
	}
	return nativeEvidencePage(ledger, offset)
}

// One immutable sample per page keeps historical lookup independent of the
// latest terminal text and the gateway's output budget. Offsets are local
// append-only indexes, not upstream event cursors or execution sequences.
func nativeEvidencePage(ledger *NativeEvidenceLedger, offset int) *NativeEvidenceResult {
	out := &NativeEvidenceResult{Limit: ledger.Limit, Count: len(ledger.Observations), Full: ledger.Limit > 0 && len(ledger.Observations) >= ledger.Limit, RecordingState: "stored", LatestObservationID: ledger.LatestObservationID, LatestExportObservationID: ledger.LatestExportObservationID, LastSampledAt: ledger.LastSampledAt, Observations: []NativeObservationSummary{}}
	out.ExportCount, out.HistoryOffset = len(ledger.Exports), offset
	out.HistoryPartial = len(ledger.Observations) > 1
	if offset+1 < len(ledger.Observations) {
		next := offset + 1
		out.NextHistoryOffset = &next
	}
	selected := ""
	if offset < len(ledger.Observations) {
		r := ledger.Observations[offset]
		selected = r.ID
		o := r.Observation
		out.Observations = append(out.Observations, NativeObservationSummary{ID: r.ID, Origin: r.Origin, ReadStartedAt: r.ReadStartedAt, FirstObservedAt: r.FirstObservedAt, RecordedAt: r.RecordedAt, Status: o.Status, CompletionSeq: o.CompletionSeq, Starting: o.Starting, InteractiveReady: o.InteractiveReady, Truncated: o.Truncated, OutputFidelity: o.OutputFidelity, SessionRelation: o.SessionRelation, SessionObserved: r.Session.Valid(), Inputs: r.Inputs, Association: "sample_only", ResultOwnership: "unknown"})
	}
	for _, e := range ledger.Exports {
		if e.ObservationID != selected && (offset != 0 || e.ObservationID != "") {
			continue
		}
		out.Exports = append(out.Exports, NativeExportSummary{NativeExportRecord: e, Availability: "not_rechecked", ResultOwnership: "unknown"})
	}
	return out
}

func newNativeObservation(operation string, obs agenvo.AgentObservation, origin, started string, inputs []SampledInputReceipt) NativeObservationRecord {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	obs.TaskSuccessVerified = false
	r := NativeObservationRecord{Origin: origin, Observation: obs, Session: obs.AgentSession, Inputs: inputs, ReadStartedAt: started, RecordedAt: now}
	if origin == "native_read" {
		r.FirstObservedAt = now
	}
	r.Fingerprint = fingerprint(struct {
		Origin      string
		Observation agenvo.AgentObservation
		Session     *agenvo.AgentSessionInfo
		Inputs      []SampledInputReceipt
	}{origin, obs, obs.AgentSession, inputs})
	// A local content-derived record ID is solely for lookup/deduplication.
	// It is never sent to Herdr or interpreted as a native generation/result ID.
	r.ID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(operation+":"+r.Fingerprint)).String()
	return r
}

func evidenceResponse(r dbstore.ComputerOperationRecord, d Details, state string) Result {
	out := result(r, d)
	if out.NativeEvidence != nil {
		out.NativeEvidence.RecordingState = state
	}
	return out
}

// Patch only keys owned by this boundary, preserving follow-up/delivery and
// unknown extension metadata. No original or historical record is deleted.
func (s *Service) saveNativeEvidence(ctx context.Context, r dbstore.ComputerOperationRecord, d Details, view, exported bool) (dbstore.ComputerOperationRecord, error) {
	var object map[string]json.RawMessage
	if json.Unmarshal(r.Details, &object) != nil || object == nil {
		return r, agenvo.ErrUnknown
	}
	updates := map[string]any{"native_evidence": d.NativeEvidence}
	if view {
		updates["observation"], updates["output"], updates["truncated"] = d.Observation, d.Output, d.Truncated
		r.State = "unknown" // Sampling never verifies business completion.
	}
	if exported {
		updates["artifact_hash"] = d.ArtifactHash
	}
	for key, value := range updates {
		raw, err := json.Marshal(value)
		if err != nil {
			return r, agenvo.ErrUnknown
		}
		object[key] = raw
	}
	raw, err := json.Marshal(object)
	if err != nil {
		return r, agenvo.ErrUnknown
	}
	r.Details = raw
	return s.store.SaveComputerOperation(ctx, r)
}

// Import only the surviving legacy view before it can be overwritten. Its old
// hash has no provable association with that view, so keep it separately.
func (s *Service) ensureNativeEvidence(ctx context.Context, b toolcontext.ComputerBinding, id string) (dbstore.ComputerOperationRecord, Details, error) {
	for range 3 {
		r, d, err := s.load(ctx, b, id)
		if err != nil {
			return r, d, err
		}
		if d.Batch != nil || d.Kind != "herdr_pi_task" || d.Agent.Name == "" {
			return r, d, agenvo.ErrUnsupported
		}
		if d.NativeEvidence != nil {
			if d.NativeEvidence.Version != 1 {
				return r, d, agenvo.ErrUnknown
			}
			return r, d, nil
		}
		ledger := &NativeEvidenceLedger{Version: 1, Limit: s.nativeObservationLimit, Observations: []NativeObservationRecord{}}
		if d.Observation != nil || d.Output != "" {
			o := agenvo.AgentObservation{Status: "unknown", Output: d.Output, Truncated: d.Truncated, OutputFidelity: "legacy_snapshot_unverified", SessionRelation: "unavailable"}
			if d.Observation != nil {
				o = *d.Observation
				if o.Output != d.Output {
					return r, d, agenvo.ErrUnknown
				}
			}
			if o.SessionRelation == "" {
				o.SessionRelation = "unavailable"
			}
			if len(o.Output) > 16384 {
				return r, d, agenvo.ErrUnknown
			}
			old := newNativeObservation(id, o, "legacy_latest_view", "", nil)
			ledger.Observations, ledger.LatestObservationID = append(ledger.Observations, old), old.ID
		}
		if d.ArtifactHash != "" {
			ledger.Exports = append(ledger.Exports, NativeExportRecord{ArtifactHash: d.ArtifactHash, RecordedAt: time.Now().UTC().Format(time.RFC3339Nano), Origin: "legacy_unassigned"})
		}
		d.NativeEvidence = ledger
		if _, err = s.saveNativeEvidence(ctx, r, d, false, false); err == nil {
			return s.load(ctx, b, id)
		}
		// A lost/conflicting save is resolved by reading, never another RPC.
	}
	return dbstore.ComputerOperationRecord{}, Details{}, agenvo.ErrUnknown
}

func (s *Service) observeNative(ctx context.Context, b toolcontext.ComputerBinding, id string) (Result, error) {
	r, d, err := s.ensureNativeEvidence(ctx, b, id)
	if err != nil {
		return Result{}, err
	}
	var inputs []SampledInputReceipt
	for _, f := range d.Followups {
		inputs = append(inputs, SampledInputReceipt{RequestID: f.RequestID, State: f.Progress.State})
	}
	started := time.Now().UTC().Format(time.RFC3339Nano)
	obs, observeErr := s.relay.ObserveAgent(agenvo.PinRegistration(ctx, scope(b, r.TargetID), r.TargetFingerprint), scope(b, r.TargetID), d.Agent)
	candidate := newNativeObservation(r.ID, obs, "native_read", started, inputs)
	binding := fingerprint(d.Agent)
	for range 3 {
		current, details, err := s.load(ctx, b, r.ID)
		if err != nil {
			return Result{}, err
		}
		if observeErr != nil {
			return evidenceResponse(current, details, "observation_unavailable"), observeErr
		}
		if fingerprint(details.Agent) != binding || details.NativeEvidence == nil || details.NativeEvidence.Version != 1 {
			return Result{}, agenvo.ErrUnknown
		}
		ledger := details.NativeEvidence
		state, found := "recorded", false
		for _, old := range ledger.Observations {
			if old.ID == candidate.ID {
				found, state = true, "duplicate"
				break
			}
		}
		if !found {
			if len(ledger.Observations) >= s.nativeObservationLimit {
				// Explicit structured capacity state accompanies the rejected
				// request; neither the latest view nor old evidence is replaced.
				return evidenceResponse(current, details, "capacity_reached"), ErrInvalid
			}
			ledger.Observations = append(ledger.Observations, candidate)
		}
		ledger.LatestObservationID, ledger.LastSampledAt = candidate.ID, candidate.FirstObservedAt
		details.Observation, details.Output, details.Truncated = &obs, obs.Output, obs.Truncated
		if _, err := s.saveNativeEvidence(ctx, current, details, true, false); err == nil {
			latest, saved, err := s.load(ctx, b, r.ID)
			if err != nil {
				return Result{}, err
			}
			return evidenceResponse(latest, saved, state), nil
		}
	}
	return Result{}, agenvo.ErrUnknown
}

func (s *Service) exportNative(ctx context.Context, b toolcontext.ComputerBinding, id, selected string) (Result, error) {
	r, d, err := s.ensureNativeEvidence(ctx, b, id)
	if err != nil {
		return Result{}, err
	}
	if selected == "" {
		selected = d.NativeEvidence.LatestObservationID
	}
	var sample *NativeObservationRecord
	for i := range d.NativeEvidence.Observations {
		if d.NativeEvidence.Observations[i].ID == selected {
			sample = &d.NativeEvidence.Observations[i]
			break
		}
	}
	if sample == nil {
		return Result{}, ErrInvalid
	}
	if sample.Observation.Output == "" {
		return Result{}, agenvo.ErrUnsupported
	}
	sum := sha256.Sum256([]byte(sample.Observation.Output))
	hash := hex.EncodeToString(sum[:])
	exported := NativeExportRecord{ObservationID: selected, ArtifactHash: hash, Origin: "native_snapshot", RecordedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	for _, old := range d.NativeEvidence.Exports {
		if old.ArtifactHash == hash && old.BytesVerifiedAt != "" {
			exported.BytesVerifiedAt = old.BytesVerifiedAt // Same bytes; not a fresh availability claim.
			break
		}
	}
	if exported.BytesVerifiedAt == "" {
		if s.assets == nil {
			return Result{}, agenvo.ErrUnsupported
		}
		a, err := s.assets.Ingest(ctx, media.IngestInput{BotID: b.BotID, Mime: "text/plain", Reader: strings.NewReader(sample.Observation.Output), MaxBytes: 16384, OriginalExt: ".txt"})
		if err != nil {
			return Result{}, agenvo.ErrUnknown
		}
		// Use the existing media interface only. No legacy storage traversal,
		// file lookup, remote file reading or original deletion is added here.
		if a.ContentHash != hash {
			return Result{}, agenvo.ErrUnknown
		}
		if _, _, err := s.load(ctx, b, r.ID); err != nil {
			return Result{}, err
		}
		reader, ready, err := s.assets.Open(ctx, b.BotID, a.ContentHash)
		if err != nil {
			return Result{}, agenvo.ErrUnknown
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, 16385))
		_ = reader.Close()
		if readErr != nil || string(data) != sample.Observation.Output || ready.ContentHash != hash {
			return Result{}, agenvo.ErrUnknown
		}
		exported.BytesVerifiedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	for range 3 {
		current, details, err := s.load(ctx, b, r.ID)
		if err != nil {
			return Result{}, err
		}
		if details.NativeEvidence == nil || details.NativeEvidence.Version != 1 {
			return Result{}, agenvo.ErrUnknown
		}
		ledger := details.NativeEvidence
		present := false
		for _, old := range ledger.Observations {
			if old.ID == sample.ID && old.Fingerprint == sample.Fingerprint {
				present = true
				break
			}
		}
		if !present {
			return Result{}, agenvo.ErrUnknown
		}
		found := false
		for _, old := range ledger.Exports {
			if old.ObservationID == selected {
				if old.ArtifactHash != hash {
					return Result{}, agenvo.ErrUnknown
				}
				found = true
				break
			}
		}
		state := "export_recorded"
		if found {
			state = "export_reused"
			if ledger.LatestExportObservationID == selected && details.ArtifactHash == hash {
				return exportEvidenceResponse(current, details, state, selected), nil
			}
		} else {
			ledger.Exports = append(ledger.Exports, exported)
		}
		ledger.LatestExportObservationID, details.ArtifactHash = selected, hash
		if _, err := s.saveNativeEvidence(ctx, current, details, false, true); err == nil {
			latest, saved, err := s.load(ctx, b, r.ID)
			if err != nil {
				return Result{}, err
			}
			return exportEvidenceResponse(latest, saved, state, selected), nil
		}
	}
	return Result{}, agenvo.ErrUnknown
}

// Read a compact page through the same operation, conversation and target
// authorization as result/export. No provider call or storage mutation.
func (s *Service) nativeHistory(ctx context.Context, b toolcontext.ComputerBinding, id string, offset int) (Result, error) {
	r, d, err := s.load(ctx, b, id)
	if err != nil {
		return Result{}, err
	}
	if d.Batch != nil || d.Kind != "herdr_pi_task" || d.NativeEvidence == nil || d.NativeEvidence.Version != 1 {
		return Result{}, agenvo.ErrUnsupported
	}
	if offset < 0 || offset >= len(d.NativeEvidence.Observations) && offset != 0 {
		return Result{}, ErrInvalid
	}
	// Keep the same operation facts as ordinary get. Only omit the large
	// latest-view fields; zero values would falsely negate cancellation and
	// cleanup facts when serialized by Result's non-omitempty fields.
	out := result(r, d)
	out.Output, out.Observation, out.Followups = "", nil, nil
	out.NativeEvidence = nativeEvidencePage(d.NativeEvidence, offset)
	return out, nil
}

func exportEvidenceResponse(r dbstore.ComputerOperationRecord, d Details, state, selected string) Result {
	out := result(r, d)
	for i, sample := range d.NativeEvidence.Observations {
		if sample.ID == selected {
			out.NativeEvidence = nativeEvidencePage(d.NativeEvidence, i)
			break
		}
	}
	out.NativeEvidence.RecordingState = state
	return out
}
