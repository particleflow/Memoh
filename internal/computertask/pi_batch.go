package computertask

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/felinics/memoh/internal/agenvo"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/toolcontext"
)

type BatchChunk struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}
type BatchTransfer struct {
	Meta      agenvo.BatchFile `json:"meta"`
	Chunks    []BatchChunk     `json:"chunks"`
	Offset    int64            `json:"offset"`
	ReadyHash string           `json:"ready_hash,omitempty"`
}
type BatchDetails struct {
	RecoveryReader *agenvo.BatchRecoveryReader `json:"recovery_reader,omitempty"`
	Feedback       *deliverymeta.Feedback      `json:"delivery_feedback,omitempty"`
	Progress       agenvo.BatchProgress        `json:"progress"`
	Receipt        *agenvo.BatchReceipt        `json:"receipt,omitempty"`
	Files          []BatchTransfer             `json:"files,omitempty"`
	Result         *agenvo.PiJSONResult        `json:"result,omitempty"`
}

// storeBatchBytes verifies content-addressed readiness before recording a ref.
// Page bytes never enter conversation history or the operation JSON payload.
func (s *Service) storeBatchBytes(ctx context.Context, bot string, raw []byte) (string, error) {
	if s.assets == nil {
		return "", agenvo.ErrUnavailable
	}
	sum := sha256.Sum256(raw)
	want := hex.EncodeToString(sum[:])
	a, err := s.assets.Ingest(ctx, media.IngestInput{BotID: bot, Mime: "application/octet-stream", Reader: bytes.NewReader(raw), MaxBytes: int64(len(raw)) + 1, OriginalExt: ".bin"})
	if err != nil {
		return "", agenvo.ErrUnavailable
	}
	r, asset, err := s.assets.Open(ctx, bot, a.ContentHash)
	if err != nil {
		return "", agenvo.ErrUnavailable
	}
	defer func() { _ = r.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(r, int64(len(raw))+1))
	if err != nil || n != int64(len(raw)) || hex.EncodeToString(h.Sum(nil)) != want || asset.ContentHash != want || a.ContentHash != want {
		return "", agenvo.ErrUnknown
	}
	return want, nil
}

func (s *Service) finishBatchFile(ctx context.Context, bot string, f *BatchTransfer) error {
	// Only hashes/offsets are persisted. Reconstruct from verified cloud chunks,
	// bounded by the receipt limit, without refetching PC data after a restart.
	var data bytes.Buffer
	for _, chunk := range f.Chunks {
		r, asset, err := s.assets.Open(ctx, bot, chunk.Hash)
		if err != nil {
			return agenvo.ErrUnavailable
		}
		raw, readErr := io.ReadAll(io.LimitReader(r, chunk.Size+1))
		_ = r.Close()
		sum := sha256.Sum256(raw)
		if readErr != nil || int64(len(raw)) != chunk.Size || asset.ContentHash != chunk.Hash || hex.EncodeToString(sum[:]) != chunk.Hash {
			return agenvo.ErrUnknown
		}
		data.Write(raw)
		if int64(data.Len()) > f.Meta.Size {
			return agenvo.ErrUnknown
		}
	}
	sum := sha256.Sum256(data.Bytes())
	if int64(data.Len()) != f.Meta.Size || hex.EncodeToString(sum[:]) != f.Meta.Hash {
		return agenvo.ErrUnknown
	}
	hash, err := s.storeBatchBytes(ctx, bot, data.Bytes())
	if err != nil {
		return err
	}
	f.ReadyHash = hash
	return nil
}

func (s *Service) observeBatch(ctx context.Context, b toolcontext.ComputerBinding, r dbstore.ComputerOperationRecord, d Details) (Result, error) {
	return s.readBatch(ctx, b, r, d, false)
}

func (s *Service) readBatch(ctx context.Context, b toolcontext.ComputerBinding, r dbstore.ComputerOperationRecord, d Details, diagnostics bool) (Result, error) {
	batch := d.Batch
	if batch == nil {
		return Result{}, ErrInvalid
	}
	pinned := agenvo.PinRegistration(ctx, scope(b, r.TargetID), r.TargetFingerprint)
	readCtx, cancel := context.WithTimeout(pinned, 20*time.Second)
	defer cancel()
	save := func() error {
		// Recheck grant and mapping after reading/ingesting, before returning
		// private data. A concurrent receipt writer loses CAS; it cannot release
		// another operation or advance this operation's offsets silently.
		if _, _, err := s.load(ctx, b, r.ID); err != nil {
			return err
		}
		r.Details, _ = json.Marshal(d)
		next, err := s.store.SaveComputerOperation(ctx, r)
		if err != nil {
			return agenvo.ErrUnknown
		}
		r = next
		return nil
	}
	collect := func(read func(*agenvo.BatchRecoveryReader) error) error {
		return s.collectBatchRead(readCtx, scope(b, r.TargetID), batch, save, read)
	}
	readPage := func(file agenvo.BatchFile, offset int64) ([]byte, error) {
		var raw []byte
		err := collect(func(reader *agenvo.BatchRecoveryReader) error {
			var err error
			raw, err = s.relay.ReadBatchPageWithReader(readCtx, scope(b, r.TargetID), batch.Progress, reader, *batch.Receipt, file, offset)
			return err
		})
		return raw, err
	}
	if batch.Receipt == nil {
		var receipt *agenvo.BatchReceipt
		err := collect(func(reader *agenvo.BatchRecoveryReader) error {
			var err error
			receipt, err = s.relay.ObserveBatchWithReader(readCtx, scope(b, r.TargetID), batch.Progress, reader)
			return err
		})
		if err != nil {
			if errors.Is(err, agenvo.ErrUnknown) {
				return result(r, d), err
			}
			return Result{}, err
		}
		if receipt == nil {
			return result(r, d), nil
		}
		batch.Receipt = receipt
		batch.Files = []BatchTransfer{{Meta: receipt.Stdout}, {Meta: receipt.Stderr}}
		// The exact invocation has exited. This releases only its own durable
		// slot, including on failure, without claiming Pi/AI success or retrying.
		r.State = "completed"
		d.Phase = "pi_exited_output_pending"
		if batch.Progress.CompletionVersion == agenvo.BatchCompletionVersion {
			d.Phase = "pi_exited_result_pending"
		}
		d.ExitCode = &receipt.ExitCode
		if err := save(); err != nil {
			return Result{}, err
		}
	}
	// Read an advisory even when a prior block suppresses transfer work. Only a
	// new correctly correlated local observation can clear that block; missing,
	// partial or foreign receipts leave the last durable observation unchanged.
	if delivery := producerResult(r, d); delivery != nil && delivery.hasPending() && !r.CancelRequested {
		feedback, readErr := s.relay.ReadBatchProducerFeedback(readCtx, scope(b, r.TargetID), batch.Progress)
		current, currentDetails, err := s.load(ctx, b, r.ID)
		if err != nil {
			return Result{}, err
		}
		if current.Revision != r.Revision {
			return result(current, currentDetails), nil
		}
		if readErr == nil && feedback != nil && (batch.Feedback == nil || feedback.Hash() != batch.Feedback.Hash()) {
			batch.Feedback = feedback
			if err := save(); err != nil {
				return Result{}, err
			}
		}
	}
	// Exit evidence remains valid even when output exceeds the import budget.
	if delivery := producerResult(r, d); delivery != nil && delivery.canAdvance(time.Now()) && !r.CancelRequested {
		// A finite idempotent wakeup, not a new execution attempt or a scheduler.
		// Its transport outcome cannot change execution or delivery truth. A later
		// query repeats this safe wakeup and reads the original ledger after restart.
		_ = s.relay.AdvanceBatchDelivery(readCtx, scope(b, r.TargetID), batch.Progress)
		current, currentDetails, err := s.load(ctx, b, r.ID)
		if err != nil {
			return Result{}, err
		}
		// A concurrent association may have advanced revision. Do not overwrite it
		// with this observer's stale business details; retry observation next call.
		if current.Revision != r.Revision {
			return result(current, currentDetails), nil
		}
	}
	if batch.Progress.CompletionVersion == agenvo.BatchCompletionVersion && !diagnostics {
		if batch.Result != nil {
			return result(r, d), nil
		}
		if !batch.Receipt.Started {
			batch.Result = &agenvo.PiJSONResult{Outcome: "not_started"}
		} else {
			meta := batch.Receipt.Completion
			if meta == nil {
				return result(r, d), agenvo.ErrUnknown
			}
			// At most 8 KiB / four fixed pages. No log ingestion or model-driven
			// continuation is needed; failed reads only retry this immutable file.
			var raw []byte
			for int64(len(raw)) < meta.Size {
				page, err := readPage(*meta, int64(len(raw)))
				if err != nil {
					return result(r, d), err
				}
				raw = append(raw, page...)
			}
			parsed, err := agenvo.ParseBatchCompletion(raw, batch.Progress, *batch.Receipt)
			if err != nil {
				return result(r, d), err
			}
			batch.Result = &parsed
		}
		d.Output, d.Truncated = batch.Result.Output, batch.Result.Truncated
		d.Phase = "pi_" + batch.Result.Outcome
		if err := save(); err != nil {
			return Result{}, err
		}
		return result(r, d), nil
	}
	// Release the invocation slot, but do not claim AI success or expose refs.
	for _, f := range batch.Files {
		if f.Meta.Size > 64<<20 {
			if diagnostics {
				return result(r, d), agenvo.ErrUnsupported // never overwrite a known Pi outcome
			}
			batch.Result = &agenvo.PiJSONResult{Outcome: "output_limit"}
			d.Phase = "pi_output_limit"
			if err := save(); err != nil {
				return Result{}, err
			}
			return result(r, d), nil
		}
	}
	if batch.Result != nil && !diagnostics {
		return result(r, d), nil
	}
	if s.assets == nil {
		return result(r, d), agenvo.ErrUnavailable
	}
	pages := 0
	for i := range batch.Files {
		f := &batch.Files[i]
		for f.Offset < f.Meta.Size && pages < 16 {
			if readCtx.Err() != nil {
				return result(r, d), nil
			}
			raw, err := readPage(f.Meta, f.Offset)
			if err != nil {
				if errors.Is(err, agenvo.ErrUnknown) {
					return result(r, d), err
				}
				return Result{}, err
			}
			hash, err := s.storeBatchBytes(ctx, b.BotID, raw)
			if err != nil {
				return Result{}, err
			}
			f.Chunks = append(f.Chunks, BatchChunk{Hash: hash, Size: int64(len(raw))})
			f.Offset += int64(len(raw))
			pages++
			if err := save(); err != nil {
				return Result{}, err
			}
		}
		if f.Offset != f.Meta.Size {
			return result(r, d), nil
		}
		if f.ReadyHash == "" && f.Meta.Size > 0 {
			if err := s.finishBatchFile(ctx, b.BotID, f); err != nil {
				return Result{}, err
			}
			if err := save(); err != nil {
				return Result{}, err
			}
		}
	}
	if batch.Progress.CompletionVersion == agenvo.BatchCompletionVersion {
		return result(r, d), nil // optional logs cannot decide or change the Pi result
	}
	target, err := s.relay.ResolveTarget(pinned, scope(b, r.TargetID))
	if err != nil {
		return Result{}, err
	}
	// Media intentionally rejects empty assets. The validated receipt already
	// proves the empty digest/size; expose an explicit empty flag, never a ref
	// that cannot be opened. Empty stdout still fails the Pi JSON protocol.
	reader := io.NopCloser(bytes.NewReader(nil))
	if batch.Files[0].Meta.Size > 0 {
		reader, _, err = s.assets.Open(ctx, b.BotID, batch.Files[0].ReadyHash)
		if err != nil {
			return Result{}, agenvo.ErrUnavailable
		}
	}
	parsed := agenvo.VerifyPiJSON(reader, batch.Progress.PiSessionID, target.Agent.Provider, target.Agent.Model, batch.Receipt.ExitCode)
	_ = reader.Close()
	if !batch.Receipt.Started {
		parsed.Outcome = "not_started"
	}
	batch.Result = &parsed
	d.Output = parsed.Output
	d.Truncated = parsed.Truncated
	d.Phase = "pi_" + parsed.Outcome
	if err := save(); err != nil {
		return Result{}, err
	}
	return result(r, d), nil
}
