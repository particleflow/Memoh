package transferclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/felinics/memoh/internal/deliverymeta"
)

type ProducerPageResult struct {
	Version int                `json:"version"`
	Files   []ProducerPageFile `json:"files"`
}
type ProducerPageFile struct {
	DeliveryID string `json:"delivery_id"`
	Registered bool   `json:"registered"`
	Associated bool   `json:"associated"`
	Phase      string `json:"phase"`
	ErrorClass string `json:"error_class"`
	ErrorCode  string `json:"error_code,omitempty"`
}

// EnqueueProducerPage writes the existing per-file journals, one bounded page.
// A lost/partial response is reconciled by replaying the same files. Independent
// files continue after a source failure; nothing claims page-wide atomic commit.
// No source bytes, credential, root, or raw diagnostic appears in this result.
func EnqueueProducerPage(ctx context.Context, getenv func(string) string, revision string, limits QueueLimits, input io.Reader, transport *http.Client, now time.Time) (ProducerPageResult, error) {
	out := ProducerPageResult{Version: 1, Files: []ProducerPageFile{}}
	raw, err := io.ReadAll(io.LimitReader(input, deliverymeta.ProducerPageMaxBytes+1))
	if err != nil || len(raw) > deliverymeta.ProducerPageMaxBytes {
		return out, ErrInvalid
	}
	page, valid := deliverymeta.DecodeClosedPage(raw)
	if !valid || !validID(revision) {
		return out, ErrInvalid
	}
	cfg, err := OpenHostConfig(getenv)
	if err != nil {
		return out, err
	}
	defer func() { _ = cfg.Close() }()
	if cfg.sourcePath == "" || deliverymeta.RootHash(cfg.sourcePath) != page.Files[0].Task.RootHash {
		return out, ErrBinding
	}
	if err := cfg.validateProducerTask(page.Files[0].Task); err != nil {
		return out, err
	}
	q, err := OpenHerdrQueue(getenv, revision, limits)
	if err != nil {
		return out, err
	}
	defer func() { _ = q.Close() }()
	var failed error
	for _, file := range page.Files {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		action := producerAction(file, revision)
		action.Job.HostBinding = cfg.Binding
		status, err := q.enqueueConfigured(ctx, cfg, action, transport, now)
		item := ProducerPageFile{DeliveryID: file.DeliveryID(), Registered: status.Registered || status.Admitted, Associated: status.Associated, Phase: status.Job.Phase, ErrorClass: "none"}
		if err != nil {
			failed = errors.Join(failed, err)
			item.ErrorClass = LifecycleFailure(err).ErrorClass
			if errors.Is(err, os.ErrNotExist) {
				item.ErrorClass, item.ErrorCode = "configuration", "source_missing"
			}
			if errors.Is(err, ErrSnapshot) {
				item.ErrorClass, item.ErrorCode = "configuration", "source_changed"
			}
		}
		out.Files = append(out.Files, item)
	}
	return out, failed
}

func (h *HostConfig) validateProducerTask(task deliverymeta.Task) error {
	relative, ok := deliverymeta.AttemptDirectory(task.Attempt)
	if !ok || h.sourcePath == "" || task.RootHash != deliverymeta.RootHash(h.sourcePath) {
		return ErrProducerFeedback
	}
	root, guard, err := privateHostRoot(filepath.Join(h.sourcePath, filepath.FromSlash(relative)))
	if err != nil {
		return ErrProducerFeedback
	}
	defer func() { closeQueueGuard(guard); _ = root.Close() }()
	f, err := openPrivateFile(root, deliverymeta.ProducerContextName, os.O_RDONLY, 0)
	if err != nil {
		return ErrProducerFeedback
	}
	raw, err := io.ReadAll(io.LimitReader(f, 16385))
	_ = f.Close()
	if err != nil || len(raw) > 16384 {
		return ErrProducerFeedback
	}
	var anchor deliverymeta.Plan
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&anchor) != nil || d.Decode(new(any)) != io.EOF || !anchor.Valid() || anchor.Task != task {
		return ErrProducerFeedback
	}
	return nil
}

func (h *HostConfig) readProducerPage(name string) ([]byte, error) {
	f, err := h.OpenSource(name)
	if err != nil {
		return nil, ErrProducerManifest
	}
	raw, err := io.ReadAll(io.LimitReader(f, deliverymeta.ProducerPageMaxBytes+1))
	_ = f.Close()
	page, ok := deliverymeta.DecodeClosedPage(raw)
	if err != nil || !ok || deliverymeta.ProducerPageName(raw) != name || h.validateProducerTask(page.Files[0].Task) != nil {
		return nil, ErrProducerManifest
	}
	return raw, nil
}

func collectProducerPage(ctx context.Context, cfg *HostConfig, getenv func(string) string, revision string, limits QueueLimits, name string, raw []byte, transport *http.Client, now time.Time) (QueueStatus, error) {
	page, enqueueErr := EnqueueProducerPage(ctx, getenv, revision, limits, bytes.NewReader(raw), transport, now)
	allRegistered := enqueueErr == nil && len(page.Files) > 0
	var selected string
	for _, file := range page.Files {
		allRegistered = allRegistered && file.Registered
		if file.Registered && selected == "" {
			selected = file.DeliveryID
		}
	}
	if allRegistered {
		// The original per-file queue records now own the declarations. Only this
		// consumed metadata page is removed, never context, source or unknown work.
		if err := cfg.sources.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return QueueStatus{}, ErrStore
		}
		if syncQueueDir(cfg.sources) != nil {
			return QueueStatus{}, ErrStore
		}
	}
	if enqueueErr != nil {
		enqueueErr = errors.Join(ErrProducerSource, enqueueErr)
	}
	if selected == "" {
		return QueueStatus{}, errors.Join(ErrProducerSource, enqueueErr)
	}
	// Partial admission must not repeatedly select an already terminal member
	// while independent registered files are due. Reuse the existing scheduler.
	status, advanceErr := AdvanceProducer(ctx, getenv, revision, limits, transport, now)
	if errors.Is(advanceErr, ErrNoWork) {
		q, err := OpenHerdrQueue(getenv, revision, limits)
		if err != nil {
			return QueueStatus{}, errors.Join(enqueueErr, err)
		}
		status, advanceErr = q.Inspect(selected)
		_ = q.Close()
	}
	return status, errors.Join(enqueueErr, advanceErr)
}
