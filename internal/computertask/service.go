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

type Service struct {
	store     dbstore.ComputerOperationStore
	registry  Registry
	relay     *agenvo.Service
	assets    Assets
	authorize Authorize
}

func NewService(store dbstore.ComputerOperationStore, registry Registry, relay *agenvo.Service, assets Assets, authorize Authorize) *Service {
	return &Service{store: store, registry: registry, relay: relay, assets: assets, authorize: authorize}
}

type Target struct {
	ID            string   `json:"target_id"`
	Name          string   `json:"name"`
	Capabilities  []string `json:"capabilities"`
	Availability  string   `json:"availability"`
	AgentProtocol string   `json:"agent_protocol,omitempty"`
}
type Submit struct {
	Artifact  *deliverymeta.Artifact  `json:"artifact,omitempty"`
	Artifacts []deliverymeta.Artifact `json:"artifacts,omitempty"`
	TargetID  string                  `json:"target_id,omitempty"`
	Kind      string                  `json:"kind"`
	Task      string                  `json:"task"`
	WorkDir   string                  `json:"work_dir,omitempty"`
}
type Details struct {
	Batch        *BatchDetails            `json:"batch,omitempty"`
	Kind         string                   `json:"kind"`
	Agent        agenvo.AgentProgress     `json:"agent"`
	Output       string                   `json:"output,omitempty"`
	ExitCode     *int                     `json:"exit_code,omitempty"`
	Truncated    bool                     `json:"truncated"`
	Observation  *agenvo.AgentObservation `json:"observation,omitempty"`
	ArtifactHash string                   `json:"artifact_hash,omitempty"`
	Phase        string                   `json:"phase"`
	Foreground   *agenvo.Result           `json:"foreground,omitempty"`
}
type Result struct {
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
	out.Delivery = producerResult(r, d)
	if d.Foreground != nil {
		out.WorkspaceID, out.PaneID, out.RequestID, out.CleanupRequired = d.Foreground.WorkspaceID, d.Foreground.PaneID, d.Foreground.RequestID, d.Foreground.CleanupRequired
	}
	if d.Batch != nil {
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
		if t.Agent != (agenvo.AgentProfile{}) && protocol == "" {
			protocol = "herdr-tui-observation"
		}
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
	if in.WorkDir == "" {
		in.WorkDir = "."
	}
	id, target, err := s.choose(ctx, b, in.TargetID, in.Kind)
	if err != nil {
		return Result{}, err
	}
	in.TargetID = id
	if in.Kind == "herdr_pi_task" && target.Agent == (agenvo.AgentProfile{}) {
		return Result{}, agenvo.ErrAgentNotConfigured
	}
	if err := agenvo.ValidateWorkDir(target.WorkDir, in.WorkDir); err != nil {
		return Result{}, err
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
	operationID := uuid.NewString()
	d := Details{Kind: in.Kind}
	if in.Kind == "herdr_pi_task" && target.Agent.ExecutionMode == "pi-json-v1" {
		d.Batch = &BatchDetails{Progress: agenvo.BatchProgress{LayoutVersion: agenvo.BatchLayoutVersion, CompletionVersion: agenvo.BatchCompletionVersion, Attempt: strings.ReplaceAll(uuid.NewString(), "-", ""), Generation: strings.ReplaceAll(uuid.NewString(), "-", ""), RequestHash: fingerprint(in), PiSessionID: fingerprint([]string{b.BotID, b.SessionID, b.UserID, id, in.WorkDir})}}

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
		return Result{}, ErrDenied
	}
	if !created {
		if r.TargetFingerprint != agenvo.TargetFingerprint(target) {
			return Result{}, agenvo.ErrTargetChanged
		}
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
		r.Details, _ = json.Marshal(d)
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
		p, runErr := s.relay.SubmitBatch(work, scope(b, id), in.Task, in.WorkDir, d.Batch.Progress, func(p agenvo.BatchProgress) error {
			d.Batch.Progress = p
			d.Phase = p.Phase
			return save("dispatching")
		})
		d.Batch.Progress = p
		d.Phase = p.Phase
		state := "submitted"
		if runErr != nil {
			state = "unknown"
		}
		if err := save(state); err != nil {
			return result(r, d), err
		}
		return result(r, d), runErr
	}
	d.Agent.Name = "m" + strings.ReplaceAll(uuid.NewString(), "-", "")[:30]
	p, runErr := s.relay.SubmitAgent(work, scope(b, id), in.Task, in.WorkDir, d.Agent, func(p agenvo.AgentProgress) error { d.Agent = p; d.Phase = p.Phase; return save("dispatching") })
	d.Agent = p
	d.Phase = p.Phase
	state := "submitted"
	if runErr != nil {
		state = "unknown"
		if p.Phase == "prompt_rejected" && errors.Is(runErr, agenvo.ErrRejected) {
			state = "rejected"
		}
	}
	if err := save(state); err != nil {
		return result(r, d), err
	}
	return result(r, d), runErr
}

func (s *Service) load(ctx context.Context, b toolcontext.ComputerBinding, id string) (dbstore.ComputerOperationRecord, Details, error) {
	r, err := s.store.ReadComputerOperation(ctx, dbstore.ComputerOperationRecord{ID: id, BotID: b.BotID, SessionID: b.SessionID, UserID: b.UserID})
	if err != nil || r.BotID != b.BotID || r.SessionID != b.SessionID || r.UserID != b.UserID {
		return r, Details{}, ErrDenied
	}
	if err := toolcontext.CheckComputerTarget(ctx, r.TargetID); err != nil {
		return r, Details{}, ErrDenied
	}
	t, err := s.relay.ResolveTarget(ctx, scope(b, r.TargetID))
	if err != nil {
		return r, Details{}, ErrDenied
	}
	if agenvo.TargetFingerprint(t) != r.TargetFingerprint {
		return r, Details{}, agenvo.ErrTargetChanged
	}
	var d Details
	if json.Unmarshal(r.Details, &d) != nil {
		return r, d, agenvo.ErrUnknown
	}
	return r, d, nil
}

func (s *Service) observe(ctx context.Context, b toolcontext.ComputerBinding, id string, refresh bool) (Result, error) {
	r, d, err := s.load(ctx, b, id)
	if err != nil {
		return Result{}, err
	}
	if refresh && d.Batch != nil {
		return s.observeBatch(ctx, b, r, d)
	}
	if refresh && d.Kind == "herdr_pi_task" && d.Agent.Name != "" && r.State != "rejected" {
		obs, observeErr := s.relay.ObserveAgent(agenvo.PinRegistration(ctx, scope(b, r.TargetID), r.TargetFingerprint), scope(b, r.TargetID), d.Agent)
		if observeErr != nil {
			return result(r, d), observeErr
		}
		d.Observation = &obs
		d.Output = obs.Output
		r.State = "unknown" // observation is not a verified task receipt
		r.Details, _ = json.Marshal(d)
		r, err = s.store.SaveComputerOperation(ctx, r)
		if err != nil {
			return Result{}, agenvo.ErrUnknown
		}
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
	if active || delivery != nil && delivery.hasPending() {
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

func (s *Service) artifact(ctx context.Context, b toolcontext.ComputerBinding, id string) (Result, error) {
	r, d, err := s.load(ctx, b, id)
	if err != nil {
		return Result{}, err
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
