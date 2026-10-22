package botdelegation

import (
	"context"
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
	ID              string       `json:"delegation_id" validate:"required" format:"uuid"`
	RequestID       string       `json:"request_id" validate:"required" format:"uuid"`
	SourceSessionID string       `json:"source_session_id" validate:"required" format:"uuid"`
	TargetBotID     string       `json:"target_bot_id" validate:"required" format:"uuid"`
	TargetSessionID string       `json:"target_session_id,omitempty" format:"uuid"`
	State           string       `json:"state" validate:"required" enums:"accepted,running,unknown,completed,failed"`
	DeliveryState   string       `json:"delivery_state" validate:"required" enums:"pending,accepted,unknown,unavailable"`
	Summary         *Summary     `json:"summary,omitempty"`
	Attachments     []Attachment `json:"attachments" validate:"required"`
	CreatedAt       time.Time    `json:"created_at" validate:"required"`
	UpdatedAt       time.Time    `json:"updated_at" validate:"required"`
}

// Host uses existing Bot chat, sessions, and follow-up delivery. There is no
// alternate Agent implementation or external job service behind this port.
type Host interface {
	Plan(context.Context, Identity, string) ([]DevicePair, string, error)
	CreateSession(context.Context, Record) (string, error)
	Run(context.Context, Record, func(string) error) (Result, string, error)
	Deliver(context.Context, Record) error
}

type Service struct {
	Authority *Authority
	Ledger    Ledger
	Originals Originals
	Host      Host
	logger    *slog.Logger
	active    sync.Map
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

func NewService(a *Authority, ledger Ledger, originals Originals, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{Authority: a, Ledger: ledger, Originals: originals, logger: log, ctx: ctx, cancel: cancel}
}

func (s *Service) Close() { s.cancel(); s.wg.Wait() }

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
	canonical, _ := json.Marshal(struct {
		Target, Message string
		Inputs          []string
	}{in.TargetBotID, in.Message, hashes})
	fp := sha256.Sum256(canonical)
	r := Record{Identity: b, ID: uuid.NewString(), RequestID: in.RequestID, RunID: uuid.NewString(), Fingerprint: hex.EncodeToString(fp[:]), SourceSelection: selected, State: "accepted", DeliveryState: "pending", Payload: Payload{Message: in.Message, Inputs: hashes, Devices: devices, TargetSelection: target}}
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
	s.active.Store(r.ID, true)
	s.wg.Add(1)
	// Only this successful durable claim dispatches. Reads/restarts and an
	// unknown/failed result never invoke Run again.
	job.Go(s.ctx, s.logger, "bot.delegation", job.Options{OwnRequestID: true}, func(jobCtx context.Context) error {
		defer s.wg.Done()
		defer s.active.Delete(r.ID)
		ctx, cancel := context.WithTimeout(jobCtx, 30*time.Minute)
		defer cancel()
		return s.execute(ctx, r)
	})
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
	if state == "unknown" {
		return runErr
	}
	if _, err = s.Authority.Authorize(saveCtx, r.Identity); err != nil {
		r.DeliveryState = "unavailable"
	} else if err = s.Host.Deliver(saveCtx, r); err != nil {
		r.DeliveryState = "unknown"
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
	r, err := s.Ledger.Get(ctx, bot, user, session, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return View{}, ErrDenied
	}
	if err != nil {
		return View{}, err
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
	state := r.State
	if _, active := s.active.Load(r.ID); !active && (state == "running" || state == "accepted") {
		state = "unknown"
	}
	v := View{ID: r.ID, RequestID: r.RequestID, SourceSessionID: r.Identity.SourceSessionID, TargetBotID: r.Identity.TargetBotID, TargetSessionID: r.Identity.TargetSessionID, State: state, DeliveryState: r.DeliveryState, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Attachments: []Attachment{}}
	if state == "completed" {
		v.Summary = r.Result.Summary
		for _, hash := range r.Result.Outputs {
			a, err := s.Attachments(r).Describe(ctx, hash, true)
			if err != nil {
				return View{}, err
			}
			v.Attachments = append(v.Attachments, a)
		}
	}
	return v, nil
}
