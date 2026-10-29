package botdelegation

import (
	"context"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/job"
)

var ErrInput = errors.New("invalid delegation input")

type Input struct {
	RequestID   string            `json:"request_id" validate:"required" format:"uuid"`
	TargetBotID string            `json:"target_bot_id" validate:"required" format:"uuid"`
	Message     string            `json:"message" validate:"required"`
	Attachments []AttachmentInput `json:"attachments,omitempty"`
}

type AttachmentInput struct {
	ContentHash string `json:"content_hash" validate:"required"`
}

type View struct {
	ID                 string       `json:"delegation_id" validate:"required" format:"uuid"`
	RequestID          string       `json:"request_id" validate:"required" format:"uuid"`
	SourceSessionID    string       `json:"source_session_id" validate:"required" format:"uuid"`
	TargetBotID        string       `json:"target_bot_id" validate:"required" format:"uuid"`
	TargetSessionID    string       `json:"target_session_id,omitempty" format:"uuid"`
	TargetRunID        string       `json:"target_run_id,omitempty" format:"uuid"`
	TargetRunState     string       `json:"target_run_state,omitempty" enums:"accepted,running,waiting_decision,finishing,completed,aborted,failed,lost"`
	TargetRunCreatedAt *time.Time   `json:"target_run_created_at,omitempty"`
	TargetRunUpdatedAt *time.Time   `json:"target_run_updated_at,omitempty"`
	State              string       `json:"state" validate:"required" enums:"accepted,running,unknown,completed,failed"`
	DeliveryState      string       `json:"delivery_state" validate:"required" enums:"pending,accepted,unknown,unavailable"`
	Summary            *Summary     `json:"summary,omitempty"`
	Attachments        []Attachment `json:"attachments" validate:"required"`
	CreatedAt          time.Time    `json:"created_at" validate:"required"`
	UpdatedAt          time.Time    `json:"updated_at" validate:"required"`
}

// Host uses existing Bot chat, sessions, and follow-up delivery. There is no
// alternate Agent implementation or external job service behind this port.
type Host interface {
	SourceRoute(context.Context, Identity) (ReturnRoute, error)
	Plan(context.Context, Identity, string) ([]DevicePair, string, error)
	CreateSession(context.Context, Record) (string, error)
	Run(context.Context, Record, func(string) error) (Result, string, error)
	Deliver(context.Context, Record) error
}

type Service struct {
	executionTimeout time.Duration
	Authority        *Authority
	Ledger           Ledger
	Originals        Originals
	Host             Host
	logger           *slog.Logger
	active           sync.Map
	ctx              context.Context
	cancel           context.CancelFunc
	wg               sync.WaitGroup
	cursorCipher     cipher.AEAD
	lifecycle        sync.Mutex
	closed           bool
}

func NewService(a *Authority, ledger Ledger, originals Originals, log *slog.Logger, executionTimeout time.Duration) *Service {
	if log == nil {
		log = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{Authority: a, Ledger: ledger, Originals: originals, logger: log, ctx: ctx, cancel: cancel, executionTimeout: executionTimeout}
}

func (s *Service) Close() {
	s.lifecycle.Lock()
	s.closed = true
	s.cancel()
	s.lifecycle.Unlock()
	s.wg.Wait()
}

func (s *Service) Submit(ctx context.Context, bot, user, session string, in Input) (View, error) {
	if s.Host == nil || s.Ledger == nil {
		return View{}, ErrDenied
	}
	if _, err := uuid.Parse(in.RequestID); err != nil {
		return View{}, ErrInput
	}
	if _, err := uuid.Parse(in.TargetBotID); err != nil {
		return View{}, ErrInput
	}
	in.Message = strings.TrimSpace(in.Message)
	if in.Message == "" || len(in.Message) > 64*1024 || len(in.Attachments) > 16 {
		return View{}, ErrInput
	}
	b, selected, err := s.Authority.Bind(ctx, user, bot, session, in.TargetBotID)
	if err != nil {
		return View{}, err
	}
	// A delegated specialist cannot silently relay authority into a new Bot.
	// Further hops need their own persisted ancestor chain, not context copying.
	if _, err = s.ForSession(ctx, bot, user, session); err == nil {
		return View{}, ErrDenied
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return View{}, err
	}
	var hashes []string
	seen := map[string]bool{}
	for _, input := range in.Attachments {
		if seen[input.ContentHash] {
			return View{}, ErrInput
		}
		if err = s.Authority.AuthorizeAsset(ctx, b, input.ContentHash, false); err != nil {
			return View{}, err
		}
		hashes = append(hashes, input.ContentHash)
		seen[input.ContentHash] = true
	}
	devices, target, err := s.Host.Plan(ctx, b, selected)
	if err != nil {
		return View{}, err
	}
	returnRoute, err := s.Host.SourceRoute(ctx, b)
	if err != nil {
		return View{}, err
	}
	// Opt in only where the native result-only publication proof is supported.
	// Other ingress/runtime types keep their existing delivery behavior.
	resultVersion := 0
	source, err := s.Authority.Sessions.Get(ctx, b.SourceSessionID)
	if err != nil {
		return View{}, err
	}
	if returnRoute.Channel == "web" && (source.RuntimeType == "" || source.RuntimeType == thread.RuntimeModel) {
		resultVersion = 1
	}
	canonical, _ := json.Marshal(struct {
		Target, Message string
		Inputs          []string
	}{in.TargetBotID, in.Message, hashes})
	fp := sha256.Sum256(canonical)
	r := Record{Identity: b, ID: uuid.NewString(), RequestID: in.RequestID, RunID: uuid.NewString(), Fingerprint: hex.EncodeToString(fp[:]), SourceSelection: selected, State: "accepted", DeliveryState: "pending", Payload: Payload{ResultDeliveryVersion: resultVersion, Message: in.Message, Inputs: hashes, Devices: devices, TargetSelection: target, ReturnRoute: &returnRoute}}
	r, created, err := s.Ledger.Claim(ctx, r)
	if err != nil {
		return View{}, err
	}
	if created {
		s.start(r)
	}
	return s.project(ctx, r)
}

// start reconstructs a fresh destination execution from durable identity.
// Source tool capabilities and PC mount IDs must not be copied into another Bot.
func (s *Service) start(r Record) {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	if s.closed {
		return
	}
	s.active.Store(r.ID, true)
	s.wg.Add(1)
	// Only this successful durable claim dispatches. Reads/restarts and an
	// unknown/failed result never invoke Run again.
	job.Go(s.ctx, s.logger, "bot.delegation", job.Options{OwnRequestID: true}, func(jobCtx context.Context) error {
		defer s.wg.Done()
		defer s.active.Delete(r.ID)
		ctx, cancel := s.executionContext(jobCtx)
		defer cancel()
		return s.execute(ctx, r)
	})
}

func (s *Service) executionContext(parent context.Context) (context.Context, context.CancelFunc) {
	if s.executionTimeout == 0 {
		// Explicitly disabled outer deadline; shutdown/cancellation still apply.
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, s.executionTimeout)
}

func (s *Service) execute(ctx context.Context, r Record) error {
	if _, err := s.Authority.Authorize(ctx, r.Identity); err != nil {
		return s.notStarted(ctx, r, err)
	}
	id, err := s.Host.CreateSession(ctx, r)
	if err != nil {
		return s.notStarted(ctx, r, err)
	}
	r.Identity.TargetSessionID = id
	r.State = "running"
	r, err = s.Ledger.Save(ctx, r)
	if err != nil {
		return err
	}
	if _, err = s.Authority.Authorize(ctx, r.Identity); err != nil {
		return s.notStarted(ctx, r, err)
	}
	result, state, runErr := s.Host.Run(ctx, r, func(id string) error { r.RunID = id; var err error; r, err = s.Ledger.Save(ctx, r); return err })
	if ctx.Err() != nil {
		state, runErr = "unknown", errors.Join(runErr, ctx.Err())
	}
	switch state {
	case "completed", "failed":
	default:
		state = "unknown"
	}
	r.State, r.Result = state, result
	// Unknown does not assert failure/termination, and never exports an
	// intermediate narration as a final summary.
	if state != "completed" {
		r.Result.Summary = nil
		r.Result.Outputs = nil
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	r, err = s.Ledger.Save(saveCtx, r)
	if err != nil {
		return err
	}
	// Legacy records and unvalidated ingress/runtime types keep their original
	// unknown behavior. Only persisted Web/native opt-in permits notification.
	if state == "unknown" && r.Payload.ResultDeliveryVersion != 1 {
		return runErr
	}
	if _, err = s.Authority.Authorize(saveCtx, r.Identity); err != nil {
		r.DeliveryState = "unavailable"
	} else if err = s.Host.Deliver(saveCtx, r); err != nil {
		r.DeliveryState = "unknown"
		if errors.Is(err, ErrDenied) {
			r.DeliveryState = "unavailable"
		}
	} else {
		r.DeliveryState = "accepted"
	}
	_, saveErr := s.Ledger.Save(saveCtx, r)
	return errors.Join(runErr, err, saveErr)
}

func (s *Service) notStarted(ctx context.Context, r Record, cause error) error {
	r.State = "failed"
	r.DeliveryState = "unavailable"
	r.Result = Result{Reason: "not_started"}
	_, err := s.Ledger.Save(ctx, r)
	return errors.Join(cause, err)
}

func (s *Service) Get(ctx context.Context, bot, user, session, id string) (View, error) {
	r, err := s.ReadRecord(ctx, bot, user, session, id)
	if err != nil {
		return View{}, err
	}
	return s.project(ctx, r)
}

// ReadRecord is an authorized read, shared by detail and scoped attachment HTTP.
func (s *Service) ReadRecord(ctx context.Context, bot, user, session, id string) (Record, error) {
	r, err := s.Ledger.Get(ctx, bot, user, session, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, ErrDenied
	}
	if err != nil {
		return Record{}, err
	}
	if _, err = s.Authority.Authorize(ctx, r.Identity); err != nil {
		return Record{}, err
	}
	return r, nil
}

// RetryDelivery never creates a session or executes a specialist. The host
// uses the same durable queue invocation ID even after an uncertain response.
func (s *Service) RetryDelivery(ctx context.Context, bot, user, session, id string) (View, error) {
	r, err := s.ReadRecord(ctx, bot, user, session, id)
	if err != nil {
		return View{}, err
	}
	if r.State != "completed" && r.State != "failed" && (r.State != "unknown" || r.Payload.ResultDeliveryVersion != 1) {
		return View{}, ErrConflict
	}
	if r.DeliveryState == "accepted" && r.Payload.ResultDeliveryVersion == 0 {
		return s.project(ctx, r)
	}
	if s.Host == nil {
		return View{}, ErrDenied
	}
	if err = s.Host.Deliver(ctx, r); err != nil {
		r.DeliveryState = "unknown"
		if errors.Is(err, ErrDenied) {
			r.DeliveryState = "unavailable"
		}
	} else {
		r.DeliveryState = "accepted"
	}
	r, saveErr := s.Ledger.Save(ctx, r)
	if errors.Is(saveErr, pgx.ErrNoRows) || errors.Is(saveErr, ErrConflict) {
		return s.Get(ctx, bot, user, session, id)
	}
	if saveErr != nil {
		return View{}, saveErr
	}
	return s.project(ctx, r)
}

func (s *Service) Attachments(r Record) *AttachmentSet {
	return &AttachmentSet{Authority: s.Authority, Originals: s.Originals, Identity: r.Identity, Inputs: r.Payload.Inputs, Results: r.Result.Outputs}
}

func (s *Service) project(ctx context.Context, r Record) (View, error) {
	if _, err := s.Authority.Authorize(ctx, r.Identity); err != nil {
		return View{}, err
	}
	v, err := s.projectMetadata(ctx, r)
	if err != nil {
		return View{}, err
	}
	if v.State == "completed" {
		v.Summary = r.Result.Summary
		for _, hash := range r.Result.Outputs {
			a, err := s.Attachments(r).Describe(ctx, hash, true)
			if err != nil {
				return View{}, err
			}
			a.URL = AttachmentURL(r, hash)
			v.Attachments = append(v.Attachments, a)
		}
	}
	return v, nil
}

// A past handoff remains a conversation reference after its execution binding
// changes. This fallback grants no summary, attachment, continuation or delivery.
func (s *Service) projectHistory(ctx context.Context, r Record) (View, error) {
	v, err := s.project(ctx, r)
	if !errors.Is(err, ErrDenied) {
		return v, err
	}
	if _, err := s.Authority.authorizeHistory(ctx, r.Identity); err != nil {
		return View{}, err
	}
	return s.projectMetadata(ctx, r)
}

// Callers must first authorize either the full projection or the history reference.
func (s *Service) projectMetadata(ctx context.Context, r Record) (View, error) {
	state := r.State
	if _, active := s.active.Load(r.ID); !active && (state == "running" || state == "accepted") {
		state = "unknown"
	}
	v := View{ID: r.ID, RequestID: r.RequestID, SourceSessionID: r.Identity.SourceSessionID, TargetBotID: r.Identity.TargetBotID, TargetSessionID: r.Identity.TargetSessionID, State: state, DeliveryState: r.DeliveryState, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Attachments: []Attachment{}}
	run, err := s.Ledger.TargetRun(ctx, r)
	if err != nil {
		return View{}, err
	}
	if run != nil {
		v.TargetRunID, v.TargetRunState = run.ID, run.State
		v.TargetRunCreatedAt, v.TargetRunUpdatedAt = &run.CreatedAt, &run.UpdatedAt
	}
	return v, nil
}
