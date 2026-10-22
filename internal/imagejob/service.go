// Package imagejob is the image tool's outer execution/publication adapter.
// It has no agent loop, prompt rewriting, provider fallback or scheduler.
package imagejob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/media"
)

type Mode string

const (
	Generate      Mode  = "generate"
	Edit          Mode  = "edit"
	MaxImageBytes int64 = 20 << 20
)

var (
	ErrDenied      = errors.New("image task is unavailable in this conversation")
	ErrInvalid     = errors.New("invalid image task request")
	ErrUnsupported = errors.New("configured image adapter does not support this operation")
)

// Binding comes from the authenticated host, not tool arguments. Authorize must
// validate it and current Bot/session access on every submission/result read.
type (
	Binding   struct{ TeamID, BotID, SessionID, UserID string }
	Authorize func(context.Context, Binding, []string) error
	Input     struct {
		Data []byte
		Mime string
	}
	Image struct {
		Data []byte
		Mime string
	}
	Adapter interface {
		Identity() string
		Supports(Mode) bool
		Invoke(context.Context, Mode, string, string, []Input) (Image, error)
	}
)

type Request struct {
	Key       string   `json:"key"`
	Mode      Mode     `json:"mode"`
	Prompt    string   `json:"prompt"`
	Size      string   `json:"size,omitempty"`
	InputRefs []string `json:"input_refs,omitempty"`
}

// Result never contains image bytes, storage paths or provider diagnostics.
type Result struct {
	TaskID         string     `json:"task_id"`
	State          string     `json:"state"`
	Code           string     `json:"code,omitempty"`
	Artifacts      []Artifact `json:"artifact_refs,omitempty"`
	AutomaticRetry bool       `json:"automatic_retry"`
}
type Artifact struct {
	ContentHash string `json:"content_hash"`
	Mime        string `json:"mime"`
	SizeBytes   int64  `json:"size_bytes"`
}

type Service struct {
	media     *media.Service
	journal   *media.ImageTaskJournal
	authorize Authorize
}

func New(m *media.Service, journal *media.ImageTaskJournal, authorize Authorize) *Service {
	return &Service{media: m, journal: journal, authorize: authorize}
}

func digest(value any) string {
	raw, _ := json.Marshal(value)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

func (s *Service) allowed(ctx context.Context, b Binding, refs []string) error {
	if s == nil || s.media == nil || !s.media.RequiresPersistence() || s.media.Uploads() == nil || s.journal == nil || s.authorize == nil || b.TeamID == "" || b.BotID == "" || b.SessionID == "" || b.UserID == "" {
		return ErrDenied
	}
	if err := s.authorize(ctx, b, refs); err != nil {
		return ErrDenied
	}
	return nil
}

func (s *Service) Submit(ctx context.Context, b Binding, req Request, adapter Adapter) (Result, error) {
	if req.Key == "" || len(req.Key) > 128 || strings.TrimSpace(req.Prompt) == "" || len(req.Prompt) > 16000 || len(req.Size) > 64 || (req.Mode != Generate && req.Mode != Edit) || (req.Mode == Edit && len(req.InputRefs) != 1) || (req.Mode == Generate && len(req.InputRefs) != 0) {
		return Result{}, ErrInvalid
	}
	if err := s.allowed(ctx, b, req.InputRefs); err != nil {
		return Result{}, err
	}
	if adapter == nil || adapter.Identity() == "" || !adapter.Supports(req.Mode) {
		return Result{}, ErrUnsupported
	}
	inputs := make([]Input, 0, len(req.InputRefs))
	for _, ref := range req.InputRefs {
		if len(ref) != 64 {
			return Result{}, ErrInvalid
		}
		r, asset, err := s.media.Open(ctx, b.BotID, ref)
		if err != nil {
			return Result{}, ErrDenied
		}
		data, readErr := io.ReadAll(io.LimitReader(r, MaxImageBytes+1))
		_ = r.Close()
		actualHash := sha256.Sum256(data)
		if readErr != nil || int64(len(data)) > MaxImageBytes || hex.EncodeToString(actualHash[:]) != ref {
			return Result{}, ErrInvalid
		}
		if validateImage(data, asset.Mime) != nil {
			return Result{}, ErrInvalid
		}
		inputs = append(inputs, Input{Data: data, Mime: asset.Mime})
	}
	if err := s.allowed(ctx, b, req.InputRefs); err != nil {
		return Result{}, err
	}
	scope := digest(b)
	fingerprint := digest(struct {
		Request Request
		Adapter string
	}{req, adapter.Identity()})
	r, created, err := s.journal.Claim(ctx, media.ImageTaskReceipt{ID: req.Key, Scope: scope, Fingerprint: fingerprint, InputRefs: append([]string(nil), req.InputRefs...)})
	if err != nil {
		if errors.Is(err, media.ErrUploadBusy) && r.ID != "" {
			return Result{TaskID: r.ID, State: r.State, Code: "image.blocked_by_task"}, nil
		}
		if errors.Is(err, media.ErrUploadConflict) {
			return Result{TaskID: req.Key, State: "conflict", Code: "image.key_conflict"}, nil
		}
		return Result{TaskID: req.Key, State: "unavailable", Code: "image.claim_unavailable"}, nil
	}
	if !created {
		return s.recover(ctx, b, r)
	}
	// The durable claim precedes the sole business request. Any uncertain
	// transport/parse failure keeps the claim, including across process restart.
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	image, callErr := adapter.Invoke(callCtx, req.Mode, req.Prompt, req.Size, inputs)
	cancel()
	if callErr != nil || validateImage(image.Data, image.Mime) != nil {
		r.State = "unknown"
		_ = s.journal.Save(context.WithoutCancel(ctx), "dispatching", r)
		return Result{TaskID: r.ID, State: "unknown", Code: "image.outcome_unknown"}, nil
	}
	// Publication is a separate action. Preserve returned bytes through the
	// existing upload journal before acknowledging any ready artifact.
	publicationCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer stop()
	hash := sha256.Sum256(image.Data)
	r.State = "publication_pending"
	r.UploadID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(scope+"\x00"+r.ID)).String()
	if err = s.journal.Save(publicationCtx, "dispatching", r); err != nil {
		return Result{TaskID: r.ID, State: "unknown", Code: "image.journal_unavailable"}, nil
	}
	u, err := s.media.Uploads().CreateResult(publicationCtx, scope, b.BotID, r.UploadID, media.UploadInput{SizeBytes: int64(len(image.Data)), SHA256: hex.EncodeToString(hash[:]), Mime: image.Mime})
	if err != nil {
		return Result{TaskID: r.ID, State: "unknown", Code: "image.result_not_staged"}, nil
	}
	for offset := int64(0); offset < int64(len(image.Data)); {
		end := min(offset+media.UploadChunkBytes, int64(len(image.Data)))
		part := image.Data[offset:end]
		partHash := sha256.Sum256(part)
		_, err = s.media.Uploads().Append(publicationCtx, scope, b.BotID, u.ID, offset, int64(len(part)), hex.EncodeToString(partHash[:]), bytes.NewReader(part))
		if err != nil {
			return Result{TaskID: r.ID, State: r.State, Code: "image.result_not_staged"}, nil
		}
		offset = end
	}
	return s.recover(publicationCtx, b, r)
}

func (s *Service) Result(ctx context.Context, b Binding, id string) (Result, error) {
	if err := s.allowed(ctx, b, nil); err != nil {
		return Result{}, err
	}
	r, err := s.journal.Read(ctx, digest(b), id)
	if err != nil {
		return Result{}, ErrDenied
	}
	return s.recover(ctx, b, r)
}

func (s *Service) recover(ctx context.Context, b Binding, r media.ImageTaskReceipt) (Result, error) {
	if err := s.allowed(ctx, b, r.InputRefs); err != nil {
		return Result{}, err
	}
	if r.State == "publication_pending" {
		staged, err := s.media.Uploads().Get(ctx, r.Scope, b.BotID, r.UploadID)
		if err != nil || (staged.State == "uploading" && staged.Offset != staged.SizeBytes) {
			// A crash before all response bytes reached staging is not a safe
			// reason to repeat the provider. Recovery cannot invent missing bytes.
			return Result{TaskID: r.ID, State: "unknown", Code: "image.result_not_staged"}, nil
		}
		u, err := s.media.Uploads().Complete(ctx, r.Scope, b.BotID, r.UploadID)
		if err != nil {
			return Result{TaskID: r.ID, State: r.State, Code: "image.publication_pending"}, nil
		}
		asset, err := s.media.Resolve(ctx, b.BotID, u.ContentHash)
		if err != nil {
			return Result{TaskID: r.ID, State: r.State, Code: "image.publication_pending"}, nil
		}
		asset.SizeBytes = u.SizeBytes
		r.State = "ready"
		r.Asset = &asset
		if err := s.journal.Save(ctx, "publication_pending", r); err != nil {
			latest, readErr := s.journal.Read(ctx, r.Scope, r.ID)
			if readErr != nil || latest.State != "ready" {
				return Result{TaskID: r.ID, State: "publication_pending", Code: "image.publication_pending"}, nil
			}
			r = latest
		}
	}
	if r.State == "ready" && r.Asset != nil {
		if err := s.allowed(ctx, b, r.InputRefs); err != nil {
			return Result{}, err
		}
		reader, asset, err := s.media.Open(ctx, b.BotID, r.Asset.ContentHash)
		if err != nil {
			return Result{TaskID: r.ID, State: "unavailable", Code: "image.artifact_unavailable"}, nil
		}
		h := sha256.New()
		n, readErr := io.Copy(h, io.LimitReader(reader, MaxImageBytes+1))
		_ = reader.Close()
		if readErr != nil || n != r.Asset.SizeBytes || hex.EncodeToString(h.Sum(nil)) != asset.ContentHash {
			return Result{TaskID: r.ID, State: "unavailable", Code: "image.artifact_unavailable"}, nil
		}
		// Cleanup failure cannot undo a durable result or repeat the model.
		_ = s.media.Uploads().ReleaseResult(ctx, r.Scope, b.BotID, r.UploadID)
		return Result{TaskID: r.ID, State: "ready", Artifacts: []Artifact{{ContentHash: asset.ContentHash, Mime: asset.Mime, SizeBytes: n}}}, nil
	}
	return Result{TaskID: r.ID, State: "unknown", Code: "image.outcome_unknown"}, nil
}
