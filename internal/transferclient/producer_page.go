package transferclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
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
	var page deliverymeta.ClosedPage
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&page) != nil || dec.Decode(new(any)) != io.EOF || !page.Valid() || !validID(revision) {
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
