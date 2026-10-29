package media

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

// UploadFailureInput is client-reported metadata, never upload/byte proof.
// Operation is the existing client command ID, not a newly created upload ID.
type UploadFailureInput struct {
	Version int    `json:"version" validate:"required"`
	Phase   string `json:"phase" validate:"required"`
	Code    string `json:"code" validate:"required"`
	SDKCode int64  `json:"sdk_code"`
	At      int64  `json:"at" validate:"required"`
	Handle  string `json:"handle,omitempty"`
}

type UploadFailure struct {
	UploadFailureInput
	Operation  string    `json:"operation" validate:"required"`
	ReceivedAt time.Time `json:"received_at" validate:"required"`
	ExpiresAt  time.Time `json:"expires_at" validate:"required"`
}

type uploadFailureRecord struct {
	UploadFailure
	Owner string `json:"owner"`
	Bot   string `json:"bot"`
}

const failureJournal = "failures.journal"

func validClientOperation(id string) bool { return len(id) == 32 && validSHA(id+id) }

func validUploadFailure(in UploadFailureInput) bool {
	if in.Version != 1 || in.At <= 0 || in.At > (1<<53)-1 || in.SDKCode < 0 || in.SDKCode > 2147483647 || (in.Handle != "" && !validClientOperation(in.Handle)) {
		return false
	}
	switch in.Phase {
	case "owner-read", "limits-read", "picker", "source-metadata", "source-open", "snapshot-create", "snapshot-copy", "snapshot-hash", "source-verify", "journal-publish", "journal-dir-sync":
	default:
		return false
	}
	switch in.Code {
	case "MEDIA_SHA_MISMATCH", "MEDIA_SIZE_MISMATCH", "MEDIA_SHORT_READ", "MEDIA_SHORT_WRITE", "UPLOAD_CANCELLED", "UPLOAD_REQUEST", "UPLOAD_IDENTITY", "UPLOAD_LIMIT_INVALID", "UPLOAD_JOURNAL_SIZE", "UPLOAD_JOURNAL_WRITE", "UPLOAD_RECEIPT_SIZE", "UPLOAD_HTTP_413", "VOICE_UNAVAILABLE", "UNCLASSIFIED":
		return true
	default:
		return false
	}
}

func (s *UploadStore) readFailures() ([]uploadFailureRecord, error) {
	f, err := openUploadFile(s.root, failureJournal, os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// Reuse the existing receipt-count ceiling, without reserving file bytes or
	// consuming active-upload slots. No new transfer byte budget is introduced.
	data, err := io.ReadAll(io.LimitReader(f, int64(maxUploadSessions*2048+1)))
	if err != nil {
		return nil, err
	}
	if len(data) > maxUploadSessions*2048 {
		return nil, ErrUploadInvalid
	}
	var records []uploadFailureRecord
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&records); err != nil {
		return nil, err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, ErrUploadInvalid
	}
	if len(records) > maxUploadSessions {
		return nil, ErrUploadInvalid
	}
	for _, r := range records {
		if !validClientOperation(r.Operation) || !validPathComponent(r.Owner) || !validPathComponent(r.Bot) || !validUploadFailure(r.UploadFailureInput) || r.ReceivedAt.IsZero() || !r.ExpiresAt.Equal(r.ReceivedAt.Add(UploadTTL)) {
			return nil, ErrUploadInvalid
		}
	}
	// Preserve the existing journal acknowledgement durability barrier.
	if err := s.syncFile(f); err != nil {
		return nil, err
	}
	if err := syncRootWith(s.root, s.syncFile); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *UploadStore) saveFailures(records []uploadFailureRecord) error {
	if len(records) == 0 {
		if err := s.remove(failureJournal); err != nil {
			return err
		}
		return syncRootWith(s.root, s.syncFile)
	}
	const temporary = "failures.pending"
	if err := s.remove(temporary); err != nil {
		return err
	}
	f, err := openUploadFile(s.root, temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close(); _ = s.root.Remove(temporary) }()
	if err := json.NewEncoder(f).Encode(records); err != nil {
		return err
	}
	if err := s.syncFile(f); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := s.root.Rename(temporary, failureJournal); err != nil {
		return err
	}
	return syncRootWith(s.root, s.syncFile)
}

// sweepFailures runs only under the existing admission lock. Expiry and GC
// match the original upload queue: logically unavailable at TTL, physical GC
// on Sweep/admission activity. It cannot delete an original or upload receipt.
func (s *UploadStore) sweepFailures() error {
	records, err := s.readFailures()
	if err != nil {
		return err
	}
	kept := make([]uploadFailureRecord, 0, len(records))
	for _, r := range records {
		if s.now().Before(r.ExpiresAt) {
			kept = append(kept, r)
		}
	}
	if len(kept) == len(records) {
		return nil
	}
	return s.saveFailures(kept)
}

func (s *UploadStore) ReportFailure(ctx context.Context, owner, bot, operation string, in UploadFailureInput) (UploadFailure, error) {
	if !validPathComponent(owner) || !validPathComponent(bot) || !validClientOperation(operation) || !validUploadFailure(in) {
		return UploadFailure{}, ErrUploadInvalid
	}
	unlock, err := s.lock(ctx, "admission")
	if err != nil {
		return UploadFailure{}, err
	}
	defer unlock()
	if err := s.sweepFailures(); err != nil {
		return UploadFailure{}, err
	}
	records, err := s.readFailures()
	if err != nil {
		return UploadFailure{}, err
	}
	for _, r := range records {
		if r.Owner == owner && r.Bot == bot && r.Operation == operation {
			if r.UploadFailureInput != in {
				return UploadFailure{}, ErrUploadConflict
			}
			return r.UploadFailure, nil
		}
	}
	if len(records) >= maxUploadSessions {
		return UploadFailure{}, ErrUploadQuota
	}
	now := s.now().UTC()
	result := UploadFailure{UploadFailureInput: in, Operation: operation, ReceivedAt: now, ExpiresAt: now.Add(UploadTTL)}
	records = append(records, uploadFailureRecord{UploadFailure: result, Owner: owner, Bot: bot})
	if err := s.saveFailures(records); err != nil {
		return UploadFailure{}, err
	}
	return result, nil
}

func (s *UploadStore) Failures(ctx context.Context, owner, bot, operation string) ([]UploadFailure, error) {
	if !validPathComponent(owner) || !validPathComponent(bot) || (operation != "" && !validClientOperation(operation)) {
		return nil, ErrUploadInvalid
	}
	unlock, err := s.lock(ctx, "admission")
	if err != nil {
		return nil, err
	}
	defer unlock()
	records, err := s.readFailures()
	if err != nil {
		return nil, err
	}
	result := make([]UploadFailure, 0)
	for _, r := range records {
		if r.Owner == owner && r.Bot == bot && (operation == "" || r.Operation == operation) && s.now().Before(r.ExpiresAt) {
			result = append(result, r.UploadFailure)
		}
	}
	if operation != "" && len(result) == 0 {
		return nil, ErrUploadNotFound
	}
	return result, nil
}
