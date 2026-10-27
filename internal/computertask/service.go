// Package computertask owns bounded computer operations independently of any
// Agent engine. MCP and native tool adapters use the same host binding/service.
package computertask

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/toolcontext"
)

var (
	ErrDenied    = errors.New("computer operation is unavailable in this conversation")
	ErrAmbiguous = errors.New("multiple authorized computers match; select a target_id")
	ErrInvalid   = errors.New("invalid computer task arguments")
	ErrApproval  = errors.New("computer execution requires approval under the existing target policy")
	ErrConflict  = errors.New("another unresolved computer operation blocks this task; inspect blocked_by_operation without resubmitting")
	// Only used before the ledger claim and any business dispatch. It does not
	// classify an in-flight transport failure, which must remain unknown.
	ErrPreflightUnavailable = errors.New("computer connection is unavailable before submission; no operation was created; check the existing connection and do not resubmit automatically")
)

type Registry interface {
	ListMounts(context.Context, string) ([]dbstore.BotRemoteRuntimeBindingRecord, error)
	GetMount(context.Context, string, string) (dbstore.BotRemoteRuntimeBindingRecord, error)
}
type Assets interface {
	Ingest(context.Context, media.IngestInput) (media.Asset, error)
	Open(context.Context, string, string) (io.ReadCloser, media.Asset, error)
}
type Authorize func(context.Context, toolcontext.ComputerBinding, string, string, string) error

type DefaultWorkDir func(context.Context, toolcontext.ComputerBinding, string, agenvo.Target) (string, error)

type Service struct {
	nativeObservationLimit int
	store                  dbstore.ComputerOperationStore
	registry               Registry
	relay                  *agenvo.Service
	assets                 Assets
	authorize              Authorize
	defaultWorkDir         DefaultWorkDir
	inputFiles             InputFileResolver
}

// WithDefaultWorkDir injects the existing owned directory selection. It is
// consulted only for new requests without an explicit working directory.
func (s *Service) WithDefaultWorkDir(resolve DefaultWorkDir) *Service {
	s.defaultWorkDir = resolve
	return s
}

func NewService(store dbstore.ComputerOperationStore, registry Registry, relay *agenvo.Service, assets Assets, authorize Authorize) *Service {
	return &Service{store: store, registry: registry, relay: relay, assets: assets, authorize: authorize, nativeObservationLimit: 32}
}

type Target struct {
	ID            string   `json:"target_id"`
	Name          string   `json:"name"`
	Capabilities  []string `json:"capabilities"`
	Availability  string   `json:"availability"`
	AgentProtocol string   `json:"agent_protocol,omitempty"`
}
type Submit struct {
	ResumeOperationID string                  `json:"resume_operation_id,omitempty"`
	Inputs            []string                `json:"inputs,omitempty"`
	Artifact          *deliverymeta.Artifact  `json:"artifact,omitempty"`
	Artifacts         []deliverymeta.Artifact `json:"artifacts,omitempty"`
	TargetID          string                  `json:"target_id,omitempty"`
	Kind              string                  `json:"kind"`
	Task              string                  `json:"task"`
	WorkDir           string                  `json:"work_dir,omitempty"`
}
type Details struct {
	InputPreparationUntil time.Time                `json:"input_preparation_until,omitzero"`
	InputPreparation      string                   `json:"input_preparation,omitempty"`
	TargetSnapshot        *agenvo.Target           `json:"target_snapshot,omitempty"`
	DefaultWorkDir        string                   `json:"default_work_dir,omitempty"`
	NativeEvidence        *NativeEvidenceLedger    `json:"native_evidence,omitempty"`
	Followups             []FollowupRecord         `json:"followups,omitempty"`
	Batch                 *BatchDetails            `json:"batch,omitempty"`
	Kind                  string                   `json:"kind"`
	Agent                 agenvo.AgentProgress     `json:"agent"`
	Output                string                   `json:"output,omitempty"`
	ExitCode              *int                     `json:"exit_code,omitempty"`
	Truncated             bool                     `json:"truncated"`
	Observation           *agenvo.AgentObservation `json:"observation,omitempty"`
	ArtifactHash          string                   `json:"artifact_hash,omitempty"`
	Phase                 string                   `json:"phase"`
	Foreground            *agenvo.Result           `json:"foreground,omitempty"`
}
type Result struct {
	Inputs             *InputResult             `json:"inputs,omitempty"`
	NativeEvidence     *NativeEvidenceResult    `json:"native_evidence,omitempty"`
	NativeAgentName    string                   `json:"native_agent_name,omitempty"`
	SessionBinding     string                   `json:"session_binding,omitempty"`
	Followups          []FollowupResult         `json:"followups,omitempty"`
	Followup           *FollowupResult          `json:"followup,omitempty"`
	BlockedByRequest   string                   `json:"blocked_by_request,omitempty"`
	RequestConflict    bool                     `json:"request_conflict,omitempty"`
	Continuity         *PiContinuity            `json:"continuity,omitempty"`
	Delivery           *ProducerResult          `json:"delivery,omitempty"`
	PiResult           *agenvo.PiJSONResult     `json:"pi_result,omitempty"`
	DiagnosticsState   string                   `json:"diagnostics_state,omitempty"`
	StdoutHash         string                   `json:"stdout_hash,omitempty"`
	StderrHash         string                   `json:"stderr_hash,omitempty"`
	StdoutEmpty        bool                     `json:"stdout_empty,omitempty"`
	StderrEmpty        bool                     `json:"stderr_empty,omitempty"`
	ID                 string                   `json:"operation_id"`
	TargetID           string                   `json:"target_id"`
	State              string                   `json:"state"`
	Phase              string                   `json:"phase"`
	Kind               string                   `json:"kind"`
	Output             string                   `json:"output,omitempty"`
	ExitCode           *int                     `json:"exit_code,omitempty"`
	Truncated          bool                     `json:"truncated"`
	Observation        *agenvo.AgentObservation `json:"observation,omitempty"`
	ArtifactHash       string                   `json:"artifact_hash,omitempty"`
	CancelRequested    bool                     `json:"cancel_requested"`
	ProcessStopped     bool                     `json:"process_stopped"`
	CleanupRequired    bool                     `json:"cleanup_required"`
	BlockedByOperation string                   `json:"blocked_by_operation,omitempty"`
	WorkspaceID        string                   `json:"workspace_id,omitempty"`
	PaneID             string                   `json:"pane_id,omitempty"`
	RequestID          string                   `json:"request_id,omitempty"`
}

func fingerprint(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func scope(b toolcontext.ComputerBinding, target string) agenvo.Scope {
	return agenvo.Scope{BotID: b.BotID, UserID: b.UserID, TargetID: target}
}

func result(r dbstore.ComputerOperationRecord, d Details) Result {
	// A persisted reservation is not proof a worker is still alive. In
	// particular, a process restart must not turn it into a running claim.
	if r.State == "dispatching" {
		r.State = "unknown"
	}
	out := Result{ID: r.ID, TargetID: r.TargetID, State: r.State, Phase: d.Phase, Kind: d.Kind, Output: d.Output, ExitCode: d.ExitCode, Truncated: d.Truncated, Observation: d.Observation, ArtifactHash: d.ArtifactHash, CancelRequested: r.CancelRequested, CleanupRequired: d.Phase != ""}
	out.Inputs = inputResult(d)
	out.Delivery = producerResult(r, d)
	if d.Batch == nil && d.Agent.Name != "" {
		out.NativeEvidence = nativeEvidenceResult(d.NativeEvidence)
		out.NativeAgentName, out.WorkspaceID, out.PaneID = d.Agent.Name, d.Agent.WorkspaceID, d.Agent.PaneID
		out.SessionBinding = "unavailable"
		if d.Agent.AgentSession.Valid() {
			out.SessionBinding = "observed"
		}
		for _, f := range d.Followups {
			out.Followups = append(out.Followups, followupResult(f))
		}
	}
	if d.Foreground != nil {
		out.WorkspaceID, out.PaneID, out.RequestID, out.CleanupRequired = d.Foreground.WorkspaceID, d.Foreground.PaneID, d.Foreground.RequestID, d.Foreground.CleanupRequired
	}
	if d.Batch != nil {
		out.Continuity = batchContinuity(d.Batch)
		out.WorkspaceID, out.PaneID, out.RequestID = d.Batch.Progress.WorkspaceID, d.Batch.Progress.PaneID, d.Batch.Progress.RequestID
		out.ProcessStopped = d.Batch.Receipt != nil && d.Batch.Receipt.Validate(d.Batch.Progress) == nil
		out.PiResult = d.Batch.Result
		if d.Batch.Receipt != nil {
			out.DiagnosticsState = "not_collected"
			ready := len(d.Batch.Files) == 2
			for _, f := range d.Batch.Files {
				ready = ready && (f.ReadyHash != "" || f.Meta.Size == 0)
				if f.Offset > 0 {
					out.DiagnosticsState = "collecting"
				}
			}
			if ready {
				out.DiagnosticsState = "ready"
			}
		}
		for _, f := range d.Batch.Files {
			switch f.Meta.Name {
			case "stdout.jsonl":
				out.StdoutHash = f.ReadyHash
				out.StdoutEmpty = out.ProcessStopped && f.Meta.Size == 0
			case "stderr.txt":
				out.StderrHash = f.ReadyHash
				out.StderrEmpty = out.ProcessStopped && f.Meta.Size == 0
			}
		}
	}
	return out
}

func (s *Service) targets(ctx context.Context, b toolcontext.ComputerBinding) ([]Target, error) {
	var records []dbstore.BotRemoteRuntimeBindingRecord
	if b.TargetID != "native" {
		r, err := s.registry.GetMount(ctx, b.BotID, b.TargetID)
		if err != nil {
			return nil, ErrDenied
		}
		records = []dbstore.BotRemoteRuntimeBindingRecord{r}
	} else {
		var err error
		records, err = s.registry.ListMounts(ctx, b.BotID)
		if err != nil {
			return nil, ErrDenied
		}
	}
	items := []Target{}
	for _, r := range records {
		t, err := s.relay.ResolveTarget(ctx, scope(b, r.ID))
		if err != nil {
			continue
		}
		caps := []string{"foreground_bash"}
		if t.Agent != (agenvo.AgentProfile{}) {
			caps = append(caps, "herdr_pi_task")
		}
		protocol := t.Agent.ExecutionMode
		items = append(items, Target{ID: r.ID, Name: r.RuntimeName, Capabilities: caps, Availability: "not_probed", AgentProtocol: protocol})
	}
	return items, nil
}

func (s *Service) choose(ctx context.Context, b toolcontext.ComputerBinding, requested, kind string) (string, agenvo.Target, error) {
	if requested == "" && b.TargetID != "native" {
		requested = b.TargetID
	}
	if requested == "" {
		items, err := s.targets(ctx, b)
		if err != nil {
			return "", agenvo.Target{}, err
		}
		for _, item := range items {
			for _, cap := range item.Capabilities {
				if cap == kind {
					if requested != "" {
						return "", agenvo.Target{}, ErrAmbiguous
					}
					requested = item.ID
				}
			}
		}
	}
	if requested == "" {
		return "", agenvo.Target{}, ErrDenied
	}
	if err := toolcontext.CheckComputerTarget(ctx, requested); err != nil {
		return "", agenvo.Target{}, ErrDenied
	}
	t, err := s.relay.ResolveTarget(ctx, scope(b, requested))
	if err != nil {
		return "", agenvo.Target{}, ErrDenied
	}
	return requested, t, nil
}

func (s *Service) submit(ctx context.Context, b toolcontext.ComputerBinding, in Submit) (Result, error) {
	if b.RunID == "" || (in.Kind != "foreground_bash" && in.Kind != "herdr_pi_task") || strings.TrimSpace(in.Task) == "" || len(in.Task) > 32000 || !utf8.ValidString(in.Task) || strings.ContainsRune(in.Task, 0) || (in.Kind == "foreground_bash" && len(in.Task) > 4096) {
		return Result{}, ErrInvalid
	}
	if in.WorkDir != "" && in.WorkDir != "." {
		ctx = toolcontext.WithExplicitWorkdir(ctx, in.WorkDir)
	}
	id, target, err := s.choose(ctx, b, in.TargetID, in.Kind)
	if err != nil {
		return Result{}, err
	}
	in.TargetID = id
	usedDefault := false
	if in.WorkDir == "" || in.WorkDir == "." {
		if in.ResumeOperationID != "" {
			_, original, err := s.load(ctx, b, in.ResumeOperationID)
			if err != nil {
				return Result{}, err
			}
			in.WorkDir = original.DefaultWorkDir
		} else if explicit := toolcontext.ExplicitWorkdir(ctx); explicit != "" {
			in.WorkDir = explicit
			usedDefault = true
		} else if s.defaultWorkDir != nil && target.NativeRuntimeID != "" {
			in.WorkDir, err = s.defaultWorkDir(ctx, b, id, target)
			if err != nil {
				return Result{}, err
			}
			usedDefault = in.WorkDir != ""
		}
		if in.WorkDir == "" {
			in.WorkDir = "."
		}
	}
	if in.Kind == "herdr_pi_task" && target.Agent == (agenvo.AgentProfile{}) {
		return Result{}, agenvo.ErrAgentNotConfigured
	}
	if in.Kind == "herdr_pi_task" && target.Agent.ExecutionMode != "pi-json-v1" {
		return Result{}, agenvo.ErrUnsupported
	}

	if err := agenvo.ValidateWorkDir(target.WorkDir, in.WorkDir); err != nil {
		return Result{}, err
	}
	if in.ResumeOperationID != "" {
		ctx = toolcontext.WithExplicitWorkdir(ctx, in.WorkDir)
		operationID := in.ResumeOperationID
		in.ResumeOperationID = ""
		return s.resumeBatchWorkspace(ctx, b, operationID, in, target)
	}
	if s.authorize == nil {
		return Result{}, ErrDenied
	}
	if err := s.authorize(ctx, b, id, in.Kind, in.Task); err != nil {
		return Result{}, err
	}
	if (in.Artifact != nil || in.Artifacts != nil) && (in.Artifact != nil && (in.Artifacts != nil || !in.Artifact.Valid()) || in.Artifacts != nil && !deliverymeta.ValidArtifacts(in.Artifacts) || in.Kind != "herdr_pi_task" || target.Agent.ExecutionMode != "pi-json-v1" || in.WorkDir != ".") {
		return Result{}, ErrInvalid
	}
	inputs, err := s.inputReferences(ctx, b, in, target)
	if err != nil {
		return Result{}, err
	}
	operationID := uuid.NewString()
	d := Details{Kind: in.Kind}
	if target.NativeRuntimeID != "" {
		snapshot := target
		d.TargetSnapshot = &snapshot
	}
	if usedDefault {
		d.DefaultWorkDir = in.WorkDir
	}
	if in.Kind == "herdr_pi_task" && target.Agent.ExecutionMode == "pi-json-v1" {
		d.Batch = &BatchDetails{Progress: agenvo.BatchProgress{History: &agenvo.BatchHistoryExpectation{Version: agenvo.BatchHistoryVersion, Mode: "unconfirmed"}, LayoutVersion: agenvo.BatchLayoutVersion, CompletionVersion: agenvo.BatchCompletionVersion, Attempt: strings.ReplaceAll(uuid.NewString(), "-", ""), Generation: strings.ReplaceAll(uuid.NewString(), "-", ""), RequestHash: fingerprint(in), PiSessionID: fingerprint([]string{b.BotID, b.SessionID, b.UserID, id, in.WorkDir})}}
		if len(inputs) != 0 {
			d.Batch.Progress.Inputs = &deliverymeta.InputPlan{Version: 1, Task: deliverymeta.Task{OperationID: operationID, BotID: b.BotID, UserID: b.UserID, SessionID: b.SessionID, TargetID: id, RunID: b.RunID, Attempt: d.Batch.Progress.Attempt, Generation: d.Batch.Progress.Generation, RootHash: deliverymeta.RootHash(target.WorkDir)}, Files: inputs}
		}

		if in.Artifact != nil || in.Artifacts != nil {
			d.Batch.Progress.Producer = &deliverymeta.Plan{Version: 1, Task: deliverymeta.Task{OperationID: operationID, BotID: b.BotID, UserID: b.UserID, SessionID: b.SessionID, TargetID: id, RunID: b.RunID, Attempt: d.Batch.Progress.Attempt, Generation: d.Batch.Progress.Generation, RootHash: deliverymeta.RootHash(target.WorkDir)}}
			if in.Artifact != nil {
				d.Batch.Progress.Producer.Artifact = *in.Artifact
			} else {
				d.Batch.Progress.Producer.Version = 2
				d.Batch.Progress.Producer.Artifacts = append([]deliverymeta.Artifact(nil), in.Artifacts...)
			}
		}
		if err := agenvo.ValidateBatchRequest(d.Batch.Progress, target, in.WorkDir, in.Task); err != nil {
			return Result{}, err
		}
	}
	raw, _ := json.Marshal(d)
	r, created, err := s.store.ClaimComputerOperation(ctx, dbstore.ComputerOperationRecord{ID: operationID, BotID: b.BotID, SessionID: b.SessionID, UserID: b.UserID, RunID: b.RunID, TargetID: id, Fingerprint: fingerprint(in), TargetFingerprint: agenvo.TargetFingerprint(target), Details: raw})
	if err != nil {
		if admissionBlocked(err) {
			return Result{}, err
		}
		return Result{}, ErrDenied
	}
	if !created {
		if r.TargetFingerprint != agenvo.TargetFingerprint(target) {
			return Result{}, agenvo.ErrTargetChanged
		}
		d = Details{} // absent legacy fields must not inherit a new request's defaults
		if json.Unmarshal(r.Details, &d) != nil {
			return Result{}, agenvo.ErrUnknown
		}
		if r.Fingerprint != fingerprint(in) {
			blocked := result(r, d)
			blocked.BlockedByOperation = r.ID
			return blocked, ErrConflict
		}
		return result(r, d), nil
	}
	save := func(state string) error {
		r.State = state
		if d.Batch != nil && d.Batch.Progress.Inputs != nil {
			var err error
			r.Details, err = preparedInputDetails(r.Details, d)
			if err != nil {
				return agenvo.ErrUnknown
			}
		} else {
			r.Details, _ = json.Marshal(d)
		}
		persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		next, err := s.store.SaveComputerOperation(persist, r)
		if err != nil {
			r.State = "unknown"
			d.ExitCode = nil
			return agenvo.ErrUnknown
		}
		r = next
		return nil
	}
	work, cancel := context.WithTimeout(agenvo.PinRegistration(ctx, scope(b, id), r.TargetFingerprint), 35*time.Second)
	defer cancel()
	if in.Kind == "foreground_bash" {
		d.Phase = "foreground_dispatch"
		if err := save("dispatching"); err != nil {
			return result(r, d), err
		}
		out, runErr := s.relay.Execute(work, agenvo.Request{Scope: scope(b, id), Command: in.Task, WorkDir: in.WorkDir, Timeout: 30 * time.Second})
		d.Foreground = &out
		d.Output, d.ExitCode, d.Truncated = out.Output, out.ExitCode, out.Truncated
		state := out.State
		if state != "completed" && state != "not_started" && state != "rejected" {
			state = "unknown"
		}
		d.Phase = "foreground_" + state
		if err := save(state); err != nil {
			return result(r, d), err
		}
		if runErr != nil && state == "unknown" {
			return result(r, d), nil
		}
		return result(r, d), runErr
	}
	if d.Batch != nil {
		return s.dispatchBatch(work, b, in, target, &r, &d, save)
	}
	return result(r, d), agenvo.ErrUnsupported
}

func (s *Service) load(ctx context.Context, b toolcontext.ComputerBinding, id string) (dbstore.ComputerOperationRecord, Details, error) {
	return s.loadWithResolver(ctx, b, id, s.relay.ResolveTarget, false)
}

func (s *Service) loadHistory(ctx context.Context, b toolcontext.ComputerBinding, id string) (dbstore.ComputerOperationRecord, Details, error) {
	return s.loadWithResolver(ctx, b, id, s.relay.ResolveHistoricalTarget, true)
}

func (s *Service) loadWithResolver(ctx context.Context, b toolcontext.ComputerBinding, id string, resolve func(context.Context, agenvo.Scope) (agenvo.Target, error), history bool) (dbstore.ComputerOperationRecord, Details, error) {
	r, err := s.store.ReadComputerOperation(ctx, dbstore.ComputerOperationRecord{ID: id, BotID: b.BotID, SessionID: b.SessionID, UserID: b.UserID})
	if err != nil || r.BotID != b.BotID || r.SessionID != b.SessionID || r.UserID != b.UserID {
		return r, Details{}, ErrDenied
	}
	if err := toolcontext.CheckComputerTarget(ctx, r.TargetID); err != nil {
		return r, Details{}, ErrDenied
	}
	t, err := resolve(ctx, scope(b, r.TargetID))
	if err != nil {
		return r, Details{}, ErrDenied
	}
	var d Details
	if json.Unmarshal(r.Details, &d) != nil {
		return r, d, agenvo.ErrUnknown
	}
	if history {
		if !agenvo.HistoricalTargetMatches(t, d.TargetSnapshot, r.TargetFingerprint) {
			return r, d, agenvo.ErrTargetChanged
		}
	} else if agenvo.TargetFingerprint(t) != r.TargetFingerprint {
		return r, d, agenvo.ErrTargetChanged
	}
	if d.TargetSnapshot != nil && agenvo.TargetFingerprint(*d.TargetSnapshot) != r.TargetFingerprint {
		return r, d, agenvo.ErrUnknown
	}
	if d.NativeEvidence != nil {
		d.NativeEvidence.Limit = s.nativeObservationLimit
	}
	if rows, ok := s.store.(dbstore.ComputerDeliveryEntryStore); ok && d.Batch != nil && d.Batch.Progress.Producer != nil {
		r.DeliveryEntries = make(map[string]dbstore.ComputerDeliveryEntry)
		for _, plan := range d.Batch.Progress.Producer.FilePlans() {
			entry, err := rows.ReadComputerDeliveryEntry(ctx, r, plan.DeliveryID())
			if errors.Is(err, db.ErrNotFound) {
				continue
			}
			if err != nil {
				return r, d, err
			}
			r.DeliveryEntries[plan.DeliveryID()] = entry
		}
	}
	return r, d, nil
}

func (s *Service) observe(ctx context.Context, b toolcontext.ComputerBinding, id string, refresh bool) (Result, error) {
	load := s.loadHistory
	if refresh {
		load = s.load
	}
	r, d, err := load(ctx, b, id)
	if err != nil {
		return Result{}, err
	}
	if refresh && d.Batch != nil {
		return s.observeBatch(ctx, b, r, d)
	}
	if refresh && d.Kind == "herdr_pi_task" && d.Agent.Name != "" && r.State != "rejected" {
		return s.observeNative(ctx, b, r.ID)
	}
	return result(r, d), nil
}

func (s *Service) cancel(ctx context.Context, b toolcontext.ComputerBinding, id string) (Result, error) {
	r, d, err := s.load(ctx, b, id)
	if err != nil {
		return Result{}, err
	}
	active := r.State != "completed" && r.State != "not_started" && r.State != "rejected"
	delivery := producerResult(r, d)
	// A later producer page may not have reached NAS yet. Persist intent for a
	// completed batch too; absence of a current page is not proof of no delivery.
	if active || d.Batch != nil || delivery != nil && delivery.hasPending() {
		if active {
			r.State = "unknown"
		}
		r.CancelRequested = true
		r, err = s.store.SaveComputerOperation(ctx, r)
		if err != nil {
			return Result{}, agenvo.ErrUnknown
		}
	}
	return result(r, d), nil // no atomic safe stop contract; never send kill/close/keys
}

func (s *Service) artifactForObservation(ctx context.Context, b toolcontext.ComputerBinding, id, observationID string) (Result, error) {
	r, d, err := s.load(ctx, b, id)
	if err != nil {
		return Result{}, err
	}
	if d.Batch == nil && d.Kind == "herdr_pi_task" && d.Agent.Name != "" {
		return s.exportNative(ctx, b, r.ID, observationID)
	}
	if observationID != "" {
		return Result{}, ErrInvalid
	}
	if d.Batch != nil {
		if d.Batch.Result == nil {
			return Result{}, agenvo.ErrUnknown
		}
		return result(r, d), nil
	}
	if s.assets == nil || d.Output == "" {
		return Result{}, agenvo.ErrUnsupported
	}
	a, err := s.assets.Ingest(ctx, media.IngestInput{BotID: b.BotID, Mime: "text/plain", Reader: strings.NewReader(d.Output), MaxBytes: 16384, OriginalExt: ".txt"})
	if err != nil {
		return Result{}, err
	}
	reader, ready, err := s.assets.Open(ctx, b.BotID, a.ContentHash)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(io.LimitReader(reader, 16385))
	if err != nil || string(data) != d.Output || ready.ContentHash != a.ContentHash {
		return Result{}, agenvo.ErrUnknown
	}
	if _, _, err := s.load(ctx, b, id); err != nil {
		return Result{}, err
	}
	// Only the bounded terminal snapshot is supported, not arbitrary PC files.
	d.ArtifactHash = a.ContentHash
	r.Details, _ = json.Marshal(d)
	r, err = s.store.SaveComputerOperation(ctx, r)
	if err != nil {
		return Result{}, agenvo.ErrUnknown
	}
	return result(r, d), nil
}

func (s *Service) diagnostics(ctx context.Context, b toolcontext.ComputerBinding, id string) (Result, error) {
	r, d, err := s.load(ctx, b, id)
	if err != nil {
		return Result{}, err
	}
	if d.Batch == nil {
		return Result{}, agenvo.ErrUnsupported
	}
	return s.readBatch(ctx, b, r, d, true)
}
