package computertask

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"strings"
	"time"

	"github.com/felinics/memoh/internal/agenvo"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/toolcontext"
	"github.com/felinics/memoh/internal/workspace/bridge"
	pb "github.com/felinics/memoh/internal/workspace/bridgepb"
)

func (s *Service) dispatchBatch(ctx context.Context, b toolcontext.ComputerBinding, in Submit, target agenvo.Target, r *dbstore.ComputerOperationRecord, d *Details, save func(string) error) (Result, error) {
	if err := s.prepareInputs(ctx, b, in, target, *r, d, save); err != nil {
		return result(*r, *d), err
	}
	history, historyErr := s.batchHistoryExpectation(ctx, *r, d.Batch.Progress)
	d.Batch.Progress.History = history
	if historyErr != nil {
		d.Phase = "pi_continuity_unconfirmed"
		if err := save("unknown"); err != nil {
			return result(*r, *d), err
		}
		return result(*r, *d), historyErr
	}
	if history.Mode == "unconfirmed" {
		d.Phase = "pi_continuity_unconfirmed"
		if err := save("not_started"); err != nil {
			return result(*r, *d), err
		}
		return result(*r, *d), nil
	}
	if err := agenvo.ValidateBatchRequest(d.Batch.Progress, target, in.WorkDir, in.Task); err != nil {
		d.Phase = "pi_continuity_unconfirmed"
		if saveErr := save("not_started"); saveErr != nil {
			return result(*r, *d), saveErr
		}
		return result(*r, *d), err
	}
	if err := s.checkTaskInputAuthority(ctx, b, in, target, *r, d.Batch.Progress.Inputs); err != nil {
		return result(*r, *d), err
	}
	p, runErr := s.relay.SubmitBatch(ctx, scope(b, r.TargetID), in.Task, in.WorkDir, d.Batch.Progress, func(p agenvo.BatchProgress) error {
		if err := s.checkTaskInputAuthority(ctx, b, in, target, *r, p.Inputs); err != nil {
			return err
		}
		d.Batch.Progress, d.Phase = p, p.Phase
		return save("dispatching")
	})
	d.Batch.Progress, d.Phase = p, p.Phase
	state := "submitted"
	if runErr != nil {
		state = "unknown"
	}
	if err := save(state); err != nil {
		return result(*r, *d), err
	}
	return result(*r, *d), runErr
}

// Only an explicit unchanged request can resume input preparation. No native
// workspace/pane/Pi input has existed at this boundary; all later phases retain
// the original unknown/no-replay rules. The existing admission CAS owns the win.
func (s *Service) resumeInputPreparation(ctx context.Context, b toolcontext.ComputerBinding, in Submit, target agenvo.Target, r dbstore.ComputerOperationRecord, d Details) (Result, error) {
	p := d.Batch.Progress
	admitted, ok := s.store.(dbstore.ComputerOperationAdmissionStore)
	if !ok || r.CancelRequested || p.Phase != "" || p.WorkspaceID != "" || p.PaneID != "" || p.RequestID != "" || d.Batch.Receipt != nil || d.Batch.Result != nil || s.authorize == nil || (r.State != "unknown" && r.State != "dispatching" && r.State != "not_started") {
		return result(r, d), agenvo.ErrUnknown
	}
	// A live bounded preparation owns its file writes. CAS alone would allow
	// a later caller to read its new revision and start a second writer. A
	// crashed preparer can be retried only after its persisted work deadline
	// and the existing 3-second persistence tail, never by a concurrent call.
	if d.InputPreparation == "preparing" && (d.InputPreparationUntil.IsZero() || time.Now().Before(d.InputPreparationUntil.Add(3*time.Second))) {
		return result(r, d), agenvo.ErrUnknown
	}
	if err := s.authorize(ctx, b, r.TargetID, in.Kind, in.Task); err != nil {
		return result(r, d), err
	}
	pinned := agenvo.PinRegistration(ctx, scope(b, r.TargetID), r.TargetFingerprint)
	work, cancel := context.WithTimeout(pinned, 35*time.Second)
	defer cancel()
	first := true
	save := func(state string) error {
		r.State = state
		var detailsErr error
		r.Details, detailsErr = preparedInputDetails(r.Details, d)
		if detailsErr != nil {
			return agenvo.ErrUnknown
		}
		persist, stop := context.WithTimeout(context.WithoutCancel(work), 3*time.Second)
		defer stop()
		var next dbstore.ComputerOperationRecord
		var err error
		if first || d.Batch.Progress.Phase == "batch_creating_workspace" || d.Batch.Progress.Phase == "batch_creating_tab" || d.Batch.Progress.Phase == "batch_submitting" {
			first = false
			next, err = admitted.SaveComputerOperationWithAdmission(persist, r)
		} else {
			next, err = s.store.SaveComputerOperation(persist, r)
		}
		if err != nil {
			if admissionBlocked(err) {
				return err
			}
			return agenvo.ErrUnknown
		}
		r = next
		return nil
	}
	return s.dispatchBatch(work, b, in, target, &r, &d, save)
}

// Cached PC bytes are not a grant. Check the original NAS reference again at
// every native dispatch boundary, including later workspace-only continuation.
func (s *Service) checkTaskInputAuthority(ctx context.Context, b toolcontext.ComputerBinding, in Submit, target agenvo.Target, r dbstore.ComputerOperationRecord, p *deliverymeta.InputPlan) error {
	if p == nil {
		return nil
	}
	if !p.Valid() || ctx.Err() != nil || r.CancelRequested || s.authorize == nil || s.authorize(ctx, b, r.TargetID, in.Kind, in.Task) != nil {
		return ErrDeliveryAsset
	}
	if p.Task.OperationID != r.ID || p.Task.BotID != r.BotID || p.Task.UserID != r.UserID || p.Task.SessionID != r.SessionID || p.Task.TargetID != r.TargetID || p.Task.RunID != r.RunID || p.Task.RootHash != deliverymeta.RootHash(target.WorkDir) {
		return ErrDeliveryAsset
	}
	current, err := s.relay.ResolveTarget(ctx, scope(b, r.TargetID))
	if err != nil || agenvo.TargetFingerprint(current) != r.TargetFingerprint {
		return ErrDeliveryAsset
	}
	for _, file := range p.Files {
		reader, actual, err := s.openTaskInput(ctx, b.BotID, file.ContentHash)
		if err != nil {
			return ErrDeliveryAsset
		}
		closeErr := reader.Close()
		if closeErr != nil || actual != file {
			return ErrDeliveryAsset
		}
	}
	return nil
}

// Input continuation changes preparation/native observations only. Retain raw
// original input/producer declarations, future fields and NAS delivery ledgers;
// decoding them through interface{} would also round large numeric identities.
func preparedInputDetails(raw dbstore.JSON, d Details) (dbstore.JSON, error) {
	if d.Batch == nil || d.Batch.Progress.Inputs == nil {
		return nil, agenvo.ErrUnknown
	}
	updated, err := resumedBatchDetails(raw, d.Batch.Progress)
	if err != nil {
		return nil, err
	}
	var details, batch, progress map[string]json.RawMessage
	if json.Unmarshal(updated, &details) != nil || json.Unmarshal(details["batch"], &batch) != nil || json.Unmarshal(batch["progress"], &progress) != nil {
		return nil, agenvo.ErrUnknown
	}
	if d.Batch.Progress.History != nil {
		var history map[string]json.RawMessage
		if prior := progress["history"]; len(prior) != 0 && string(prior) != "null" {
			if json.Unmarshal(prior, &history) != nil {
				return nil, agenvo.ErrUnknown
			}
		}
		if history == nil {
			history = make(map[string]json.RawMessage)
		}
		encoded, _ := json.Marshal(d.Batch.Progress.History)
		var current map[string]json.RawMessage
		if json.Unmarshal(encoded, &current) != nil {
			return nil, agenvo.ErrUnknown
		}
		for key, value := range current {
			history[key] = value
		}
		progress["history"], _ = json.Marshal(history)
	}
	batch["progress"], _ = json.Marshal(progress)
	details["batch"], _ = json.Marshal(batch)
	details["phase"], _ = json.Marshal(d.Phase)
	details["input_preparation"], _ = json.Marshal(d.InputPreparation)
	details["input_preparation_until"], _ = json.Marshal(d.InputPreparationUntil)
	return json.Marshal(details)
}

// InputFileSystem is the existing authorized Runtime file RPC surface, without
// any exec port. No server/NAS pathname, credentials or bytes enter task JSON.
type InputFileSystem interface {
	Stat(context.Context, string) (*pb.FileEntry, error)
	Mkdir(context.Context, string) error
	ReadRaw(context.Context, string) (io.ReadCloser, error)
	WriteRaw(context.Context, string, io.Reader) (int64, error)
	ListDir(context.Context, string, bool, int32, int32, int32) (*bridge.ListDirResult, error)
}

type InputFileResolver func(context.Context, toolcontext.ComputerBinding, string, agenvo.Target) (InputFileSystem, error)

type InputFileResult struct {
	deliverymeta.Input
	Path string `json:"path"`
}

type InputResult struct {
	State string            `json:"state"`
	Files []InputFileResult `json:"files"`
}

// This is the stored preparation observation, not fresh device availability or
// permission. Pi's envelope independently verifies the actual files before use.
func inputResult(d Details) *InputResult {
	if d.Batch == nil || d.Batch.Progress.Inputs == nil || !d.Batch.Progress.Inputs.Valid() {
		return nil
	}
	p := d.Batch.Progress.Inputs
	out := &InputResult{State: d.InputPreparation}
	if out.State == "" {
		out.State = "pending"
	}
	for _, file := range p.Files {
		out.Files = append(out.Files, InputFileResult{Input: file, Path: p.Path(file)})
	}
	return out
}

func (s *Service) WithInputFiles(resolve InputFileResolver) *Service {
	s.inputFiles = resolve
	return s
}

// Ordinary media.Open metadata intentionally has no SizeBytes. Reuse the
// existing verified seek/range source and derive length from that exact stream,
// just like the NAS delivery verifier; never trust a caller/filename length.
func (s *Service) openTaskInput(ctx context.Context, bot, hash string) (media.ReadSeekCloser, deliverymeta.Input, error) {
	assets, ok := s.assets.(DeliveryAssets)
	if !ok {
		return nil, deliverymeta.Input{}, ErrDeliveryAsset
	}
	reader, asset, err := assets.OpenSeekable(ctx, bot, hash)
	if err != nil || reader == nil {
		if reader != nil {
			_ = reader.Close()
		}
		return nil, deliverymeta.Input{}, ErrDeliveryAsset
	}
	size, seekErr := reader.Seek(0, io.SeekEnd)
	_, resetErr := reader.Seek(0, io.SeekStart)
	file := deliverymeta.Input{ContentHash: hash, SizeBytes: size, Mime: asset.Mime}
	if seekErr != nil || resetErr != nil || asset.BotID != bot || asset.ContentHash != hash || !file.Valid() {
		_ = reader.Close()
		return nil, deliverymeta.Input{}, ErrDeliveryAsset
	}
	return reader, file, nil
}

func (s *Service) inputReferences(ctx context.Context, b toolcontext.ComputerBinding, in Submit, target agenvo.Target) ([]deliverymeta.Input, error) {
	if len(in.Inputs) == 0 {
		return nil, nil
	}
	if len(in.Inputs) > deliverymeta.MaxArtifacts || s.inputFiles == nil || s.assets == nil || in.Kind != "herdr_pi_task" || target.NativeRuntimeID == "" || target.Agent.ExecutionMode != "pi-json-v1" {
		return nil, ErrInvalid
	}
	seen := make(map[string]bool, len(in.Inputs))
	files := make([]deliverymeta.Input, 0, len(in.Inputs))
	for _, hash := range in.Inputs {
		if !deliveryHash.MatchString(hash) || seen[hash] {
			return nil, ErrInvalid
		}
		seen[hash] = true
		reader, file, err := s.openTaskInput(ctx, b.BotID, hash)
		if err != nil {
			return nil, ErrDeliveryAsset
		}
		err = reader.Close()
		if err != nil {
			return nil, ErrDeliveryAsset
		}
		files = append(files, file)
	}
	return files, nil
}

func (s *Service) prepareInputs(ctx context.Context, b toolcontext.ComputerBinding, in Submit, target agenvo.Target, r dbstore.ComputerOperationRecord, d *Details, save func(string) error) error {
	if d.Batch == nil || d.Batch.Progress.Inputs == nil {
		return nil
	}
	p := d.Batch.Progress.Inputs
	want := deliverymeta.Task{OperationID: r.ID, BotID: r.BotID, UserID: r.UserID, SessionID: r.SessionID, TargetID: r.TargetID, RunID: r.RunID, Attempt: d.Batch.Progress.Attempt, Generation: d.Batch.Progress.Generation, RootHash: deliverymeta.RootHash(target.WorkDir)}
	if !p.Valid() || p.Task != want || s.inputFiles == nil || s.assets == nil {
		return ErrDeliveryAsset
	}
	d.InputPreparation, d.Phase = "preparing", "pi_inputs_preparing"
	deadline, ok := ctx.Deadline()
	if !ok || ctx.Err() != nil {
		return ErrDeliveryAsset
	}
	d.InputPreparationUntil = deadline
	if err := save("dispatching"); err != nil {
		return err
	}
	if err := s.materializeInputs(ctx, b, target, r.TargetFingerprint, *p, func() error { return s.authorize(ctx, b, r.TargetID, in.Kind, in.Task) }); err != nil {
		d.InputPreparation, d.Phase = "unavailable", "pi_inputs_unavailable"
		if saveErr := save("not_started"); saveErr != nil {
			return saveErr
		}
		return ErrDeliveryAsset // Never return RPC paths, NAS diagnostics or secrets.
	}
	d.InputPreparation, d.Phase = "verified", "pi_inputs_verified"
	return save("dispatching")
}

func (s *Service) materializeInputs(ctx context.Context, b toolcontext.ComputerBinding, target agenvo.Target, fingerprint string, plan deliverymeta.InputPlan, authorize func() error) error {
	directory, err := agenvo.TaskInputDirectory(target, plan)
	if err != nil || fingerprint != agenvo.TargetFingerprint(target) {
		return ErrDeliveryAsset
	}
	check := func(file deliverymeta.Input) error {
		if authorize == nil || authorize() != nil {
			return ErrDenied
		}
		current, err := s.relay.ResolveTarget(ctx, scope(b, plan.Task.TargetID))
		if err != nil || agenvo.TargetFingerprint(current) != fingerprint || ctx.Err() != nil {
			return ErrDenied
		}
		reader, currentFile, err := s.openTaskInput(ctx, b.BotID, file.ContentHash)
		if err != nil {
			return ErrDeliveryAsset
		}
		defer func() { _ = reader.Close() }()
		if currentFile != file {
			return ErrDeliveryAsset
		}
		return nil
	}
	if err := check(plan.Files[0]); err != nil {
		return err
	}
	files, err := s.inputFiles(ctx, b, plan.Task.TargetID, target)
	if err != nil || files == nil {
		return ErrDeliveryAsset
	}
	if err := check(plan.Files[0]); err != nil {
		return err
	}
	root := target.WorkDir
	if err := inputRoot(ctx, files, root); err != nil {
		return err
	}
	for _, component := range []string{".memoh-computer", "inputs", plan.Task.Attempt} {
		if err := check(plan.Files[0]); err != nil {
			return err
		}
		if err := inputChildDirectory(ctx, files, root, component); err != nil {
			return err
		}
		root = strings.TrimRight(root, `\/`) + `\` + component
	}
	for _, file := range plan.Files {
		if err := check(file); err != nil {
			return err
		}
		path := directory + `\` + file.ContentHash
		entry, err := inputChild(ctx, files, directory, file.ContentHash)
		switch {
		case err == nil:
			// Unknown prior WriteRaw completion is reconciled by actual bytes.
			// Conflicts are retained; never overwrite an existing input/original.
			if entry == nil || entry.IsDir || unsafeInputMode(entry.Mode) || entry.Size != file.SizeBytes {
				return ErrDeliveryAsset
			}
			if err := verifyInputFile(ctx, files, path, file); err != nil {
				return err
			}
		case errors.Is(err, bridge.ErrNotFound):
			reader, sourceFile, err := s.openTaskInput(ctx, b.BotID, file.ContentHash)
			if err != nil || reader == nil {
				return ErrDeliveryAsset
			}
			if sourceFile != file {
				_ = reader.Close()
				return ErrDeliveryAsset
			}
			source := &inputReader{reader: reader, expected: file, hash: sha256.New(), seal: func() error { return check(file) }}
			written, writeErr := files.WriteRaw(ctx, path, source)
			closeErr := reader.Close()
			if writeErr != nil || closeErr != nil || written != file.SizeBytes || !source.verified {
				return ErrDeliveryAsset
			}
			if err := verifyInputFile(ctx, files, path, file); err != nil {
				return err
			}
		default:
			return ErrDeliveryAsset
		}
		if err := check(file); err != nil {
			return err
		}
	}
	return nil
}

func unsafeInputMode(mode string) bool {
	return strings.Contains(mode, "L") || strings.HasPrefix(mode, "l") || strings.Contains(strings.ToLower(mode), "reparse") || strings.Contains(strings.ToLower(mode), "symlink")
}

// Stat follows links on some supported Runtime implementations. The existing
// directory entry API supplies the link's own mode, so inspect each child
// before descending or writing. This does not turn Runtime FS into a sandbox.
func inputChild(ctx context.Context, files InputFileSystem, parent, name string) (*pb.FileEntry, error) {
	for offset := int32(0); ; {
		page, err := files.ListDir(ctx, parent, false, offset, 64, 0)
		if err != nil || page == nil || len(page.Entries) > 64 {
			return nil, ErrDeliveryAsset
		}
		for _, entry := range page.Entries {
			if entry == nil {
				return nil, ErrDeliveryAsset
			}
			path := strings.ReplaceAll(entry.Path, `\`, "/")
			leaf := path[strings.LastIndex(path, "/")+1:]
			if strings.EqualFold(leaf, name) {
				if unsafeInputMode(entry.Mode) {
					return nil, ErrDeliveryAsset
				}
				return entry, nil
			}
		}
		if !page.Truncated {
			return nil, bridge.ErrNotFound
		}
		if len(page.Entries) == 0 || offset > 1<<30 {
			return nil, ErrDeliveryAsset
		}
		for range page.Entries {
			offset++
		}
	}
}

func inputChildDirectory(ctx context.Context, files InputFileSystem, parent, name string) error {
	entry, err := inputChild(ctx, files, parent, name)
	if errors.Is(err, bridge.ErrNotFound) {
		if err := files.Mkdir(ctx, strings.TrimRight(parent, `\/`)+`\`+name); err != nil {
			return ErrDeliveryAsset
		}
		entry, err = inputChild(ctx, files, parent, name)
	}
	if err != nil || entry == nil || !entry.IsDir {
		return ErrDeliveryAsset
	}
	return nil
}

func inputRoot(ctx context.Context, files InputFileSystem, path string) error {
	// Stat follows a root junction too. Inspect each existing component from
	// the drive/share volume using directory entries; never create an ancestor.
	path = strings.ReplaceAll(path, "/", `\`)
	var volume, tail string
	switch {
	case strings.HasPrefix(path, `\\`):
		parts := strings.Split(path[2:], `\`)
		if len(parts) < 2 {
			return ErrDeliveryAsset
		}
		volume, tail = `\\`+parts[0]+`\`+parts[1]+`\`, strings.Join(parts[2:], `\`)
	case len(path) >= 3 && path[1:3] == `:\`:
		volume, tail = path[:3], path[3:]
	default:
		return ErrDeliveryAsset
	}
	parent := volume
	for _, name := range strings.Split(tail, `\`) {
		if name == "" {
			continue
		}
		if name == "." || name == ".." {
			return ErrDeliveryAsset
		}
		entry, err := inputChild(ctx, files, parent, name)
		if err != nil || entry == nil || !entry.IsDir {
			return ErrDeliveryAsset
		}
		parent = strings.TrimRight(parent, `\`) + `\` + name
	}
	entry, err := files.Stat(ctx, path)
	if err != nil || entry == nil || !entry.IsDir || unsafeInputMode(entry.Mode) {
		return ErrDeliveryAsset
	}
	return nil
}

type inputReader struct {
	reader   io.Reader
	expected deliverymeta.Input
	hash     hash.Hash
	total    int64
	seal     func() error
	verified bool
}

func (r *inputReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.total += int64(n)
	_, _ = r.hash.Write(p[:n])
	if r.total > r.expected.SizeBytes {
		return n, ErrDeliveryAsset
	}
	if err == io.EOF {
		if r.total != r.expected.SizeBytes || hex.EncodeToString(r.hash.Sum(nil)) != r.expected.ContentHash || r.seal != nil && r.seal() != nil {
			return n, ErrDeliveryAsset
		}
		r.verified = true
	}
	return n, err
}

func verifyInputFile(ctx context.Context, files InputFileSystem, path string, file deliverymeta.Input) error {
	reader, err := files.ReadRaw(ctx, path)
	if err != nil {
		return ErrDeliveryAsset
	}
	defer func() { _ = reader.Close() }()
	checked := &inputReader{reader: io.LimitReader(reader, file.SizeBytes+1), expected: file, hash: sha256.New()}
	buffer := make([]byte, 64<<10)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, err := checked.Read(buffer)
		if errors.Is(err, io.EOF) && checked.verified {
			return nil
		}
		if err != nil {
			return ErrDeliveryAsset
		}
	}
}
