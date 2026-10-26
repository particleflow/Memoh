package agenvo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/deliverymeta"
)

type batchPane struct {
	ID        string `json:"pane_id"`
	Workspace string `json:"workspace_id"`
	Terminal  string `json:"terminal_id"`
}

// ValidateBatchRequest applies the existing connector's command size limit
// before a durable claim or any remote mutation. Native IDs are bounded too.
func ValidateBatchRequest(p BatchProgress, target Target, workDir, task string) error {
	if !p.valid() {
		return ErrUnsupported
	}
	cwd, err := resolveWorkDir(target.WorkDir, workDir)
	if err != nil {
		return err
	}
	p.WorkspaceID, p.PaneID, p.TerminalID = strings.Repeat("x", 256), strings.Repeat("x", 256), strings.Repeat("x", 256)
	script, err := batchScript(p, target, cwd, task)
	if err != nil {
		return err
	}
	if len(launchBatchScript(script, target)) > 48000 {
		return ErrUnsupported
	}
	return nil
}

// SubmitBatch only starts a previously reserved new invocation. The original
// workspace pane is reserved for fixed file reads, and Pi gets a separate tab.
func (s *Service) SubmitBatch(ctx context.Context, scope Scope, task, workDir string, p BatchProgress, save func(BatchProgress) error) (BatchProgress, error) {
	c, target, err := s.Client(ctx, scope)
	if err != nil {
		return p, err
	}
	if !p.valid() || p.Phase != "" || target.Agent.ExecutionMode != "pi-json-v1" || target.Agent.Validate() != nil || ValidateBatchRequest(p, target, workDir, task) != nil {
		return p, ErrUnsupported
	}
	cwd, err := resolveWorkDir(target.WorkDir, workDir)
	if err != nil {
		return p, err
	}
	call := func(phase, method string, args map[string]any) (Receipt, error) {
		p.Phase = phase
		if err := save(p); err != nil {
			return Receipt{}, err
		}
		return c.native(ctx, scope, target, method, args)
	}
	created, err := call("batch_creating_workspace", "workspace.create", map[string]any{"session": target.Session, "cwd": cwd, "label": "memoh-batch-" + p.Attempt})
	if err != nil {
		return p, err
	}
	var ws struct {
		Workspace struct {
			ID    string `json:"workspace_id"`
			Label string `json:"label"`
		} `json:"workspace"`
		Root batchPane `json:"root_pane"`
	}
	if resultBody(created, &ws) != nil || ws.Workspace.ID == "" || ws.Workspace.Label != "memoh-batch-"+p.Attempt || ws.Root.ID == "" || ws.Root.Terminal == "" || ws.Root.Workspace != ws.Workspace.ID {
		return p, ErrUnknown
	}
	p.WorkspaceID, p.ControlPane, p.ControlTerminal = ws.Workspace.ID, ws.Root.ID, ws.Root.Terminal
	env := map[string]string{"PI_CODING_AGENT_DIR": target.Agent.ConfigDir}
	if target.Agent.Path != "" {
		env["PATH"] = target.Agent.Path
	}
	created, err = call("batch_creating_tab", "tab.create", map[string]any{"session": target.Session, "workspaceId": p.WorkspaceID, "cwd": cwd, "label": "memoh-pi-" + p.Attempt, "env": env})
	if err != nil {
		return p, err
	}
	var tab struct {
		Root batchPane `json:"root_pane"`
	}
	if resultBody(created, &tab) != nil || tab.Root.Workspace != p.WorkspaceID || tab.Root.ID == "" || tab.Root.Terminal == "" || tab.Root.ID == p.ControlPane {
		return p, ErrUnknown
	}
	p.PaneID, p.TerminalID = tab.Root.ID, tab.Root.Terminal
	if len(p.PaneID) > 256 || len(p.WorkspaceID) > 256 || len(p.TerminalID) > 256 {
		return p, ErrUnknown
	}
	script, err := batchScript(p, target, cwd, task)
	if err != nil {
		return p, err
	}
	command := launchBatchScript(script, target)
	// Agenvo's official pane.run contract has a 48,000-character limit.
	if len(command) > 48000 {
		return p, ErrUnsupported
	}
	r, err := call("batch_submitting", "pane.run", map[string]any{"session": target.Session, "paneId": p.PaneID, "command": command})
	p.RequestID = r.RequestID
	if err != nil {
		return p, err
	}
	p.Phase = "batch_submitted"
	return p, save(p)
}

func (c *Client) readBatchPane(ctx context.Context, scope Scope, target Target, pane string, lines int) (string, error) {
	// Agenvo v0.2.0 herdr.ts uses z.number().int().min(1).max(500).
	if lines < 1 || lines > batchReadMaxLines {
		return "", traceBatchReadFailure(ctx, "snapshot", "invalid_window", ErrUnknown)
	}
	traceBatchReadMethod(ctx, "pane.read")
	r, err := c.native(ctx, scope, target, "pane.read", map[string]any{"session": target.Session, "paneId": pane, "lines": lines, "source": "recent-unwrapped"})
	if err != nil {
		if r.Error != nil && r.Error.Code == "result_too_large" {
			return "", errBatchSnapshotTooLarge
		}
		// Rejection applies to this observation, never to the already
		// submitted Pi invocation. Only a correlated receipt proves no start.
		if errors.Is(err, ErrRejected) {
			return "", traceBatchReadFailure(ctx, "snapshot", "native_error", ErrUnknown)
		}
		return "", traceBatchReadFailure(ctx, "snapshot", "native_error", err)
	}
	var out struct {
		Output  *string `json:"output"`
		Session string  `json:"session"`
	}
	if json.Unmarshal(r.Result, &out) != nil || out.Output == nil {
		return "", traceBatchReadFailure(ctx, "snapshot", "invalid_envelope", ErrUnknown)
	}
	if out.Session != target.Session {
		return "", traceBatchReadFailure(ctx, "snapshot", "session_mismatch", ErrUnknown)
	}
	return *out.Output, nil
}

// ObserveBatch never types into Pi, including after timeout or cancellation.
func (s *Service) ObserveBatch(ctx context.Context, scope Scope, p BatchProgress) (*BatchReceipt, error) {
	return s.ObserveBatchWithReader(ctx, scope, p, nil)
}

// ObserveBatchWithReader keeps receipt validation bound to original execution,
// while an optional durable reader supplies only the immutable file channel.
func (s *Service) ObserveBatchWithReader(ctx context.Context, scope Scope, p BatchProgress, reader *BatchRecoveryReader) (*BatchReceipt, error) {
	if !p.valid() || p.PaneID == "" || p.TerminalID == "" {
		return nil, ErrUnknown
	}
	if reader == nil {
		c, target, err := s.Client(ctx, scope)
		if err != nil {
			return nil, err
		}
		text, err := c.readBatchPane(ctx, scope, target, p.PaneID, 20)
		if err != nil && !errors.Is(err, errBatchSnapshotTooLarge) {
			return nil, err
		}
		receipt, err := parseBatchReceipt(text, p)
		if err != nil || receipt != nil {
			return receipt, err
		}
	}
	// A terminal snapshot may have lost the frame. Read the immutable receipt
	// through the separate control pane; never type a status query into Pi.
	raw, err := s.readBatchFileWithReader(ctx, scope, p, reader, "receipt.json", 0, batchReceiptBytes)
	if err != nil || len(raw) == 0 {
		return nil, err
	}
	var persisted BatchReceipt
	if len(raw) >= batchReceiptBytes || json.Unmarshal(raw, &persisted) != nil || persisted.Validate(p) != nil {
		return nil, ErrUnknown
	}
	return &persisted, nil
}

// Data pages are independent of the receipt's complete JSON budget. Persisted
// offsets/chunks remain byte based, including those written with 16 KiB pages.
const (
	BatchPageBytes    = 2048
	batchReceiptBytes = 4096
	batchReadMaxLines = 500
)

var errBatchSnapshotTooLarge = errors.New("batch observation exceeds connector response limit")

// ReadBatchPage reads only a receipt-bound stdout/stderr file. It is never a
// model-specified path or shell command. The control pane never hosts Pi.
// Metadata checks detect a changed pane but cannot provide Herdr's missing
// atomic expected-generation operation; no native restart/close safety is claimed.
func (s *Service) ReadBatchPage(ctx context.Context, scope Scope, p BatchProgress, receipt BatchReceipt, file BatchFile, offset int64) ([]byte, error) {
	return s.ReadBatchPageWithReader(ctx, scope, p, nil, receipt, file, offset)
}

// ReadBatchPageWithReader never substitutes reader identity into the receipt.
func (s *Service) ReadBatchPageWithReader(ctx context.Context, scope Scope, p BatchProgress, reader *BatchRecoveryReader, receipt BatchReceipt, file BatchFile, offset int64) ([]byte, error) {
	isCompletion := receipt.Completion != nil && file == *receipt.Completion
	if receipt.Validate(p) != nil || (file != receipt.Stdout && file != receipt.Stderr && !isCompletion) || offset < 0 || offset >= file.Size || p.ControlPane == "" || p.ControlPane == p.PaneID {
		return nil, ErrUnsupported
	}
	count := min(int64(BatchPageBytes), file.Size-offset)
	data, err := s.readBatchFileWithReader(ctx, scope, p, reader, file.Name, offset, count)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != count {
		return nil, traceBatchReadFailure(ctx, "page", "short_page", ErrUnknown)
	}
	if trace := BatchReadTraceFromContext(ctx); trace != nil {
		trace.ReadStage = "page_complete"
	}
	return data, nil
}

// readBatchFile issues a bounded, fixed read on the dedicated control shell.
// An absent receipt is a complete empty response, not evidence of termination.
func (s *Service) readBatchFile(ctx context.Context, scope Scope, p BatchProgress, name string, offset, count int64) ([]byte, error) {
	return s.readBatchFileWithReader(ctx, scope, p, nil, name, offset, count)
}

func (s *Service) readBatchFileWithReader(ctx context.Context, scope Scope, p BatchProgress, reader *BatchRecoveryReader, name string, offset, count int64) ([]byte, error) {
	if trace := BatchReadTraceFromContext(ctx); trace != nil {
		trace.Offset, trace.Count, trace.ActualBytes = offset, count, -1
		trace.ReadStage, trace.Reason, trace.Method = "read_guard", "", ""
		trace.FileKind = "unknown"
		switch name {
		case "result.json":
			trace.FileKind = "result"
		case deliverymeta.ProducerFeedbackName:
			trace.FileKind = "feedback"
		case "receipt.json":
			trace.FileKind = "receipt"
		case "stdout.jsonl":
			trace.FileKind = "stdout"
		case "stderr.txt":
			trace.FileKind = "stderr"
		}
	}
	limit := int64(BatchPageBytes)
	if name == "receipt.json" {
		limit = batchReceiptBytes
	}
	if name == deliverymeta.ProducerFeedbackName {
		limit = deliverymeta.FeedbackMaxBytes
	}
	if !p.valid() || p.ControlPane == "" || p.ControlTerminal == "" || p.ControlPane == p.PaneID || offset < 0 || count <= 0 || count > limit || (name != "receipt.json" && name != "stdout.jsonl" && name != "stderr.txt" && name != "result.json" && name != deliverymeta.ProducerFeedbackName) {
		return nil, traceBatchReadFailure(ctx, "read_guard", "unsupported_input", ErrUnsupported)
	}
	workspace, pane, terminal := p.WorkspaceID, p.ControlPane, p.ControlTerminal
	if reader != nil {
		if !reader.valid(p) {
			return nil, traceBatchReadFailure(ctx, "reader", "invalid_reader", ErrUnknown)
		}
		workspace, pane, terminal = reader.WorkspaceID, reader.PaneID, reader.TerminalID
	}
	c, target, err := s.Client(ctx, scope)
	if err != nil {
		return nil, traceBatchReadFailure(ctx, "target", "target_unavailable", err)
	}
	if name == deliverymeta.ProducerFeedbackName && (p.Producer == nil || p.Producer.Task.RootHash != deliverymeta.RootHash(target.WorkDir)) {
		return nil, traceBatchReadFailure(ctx, "target", "root_changed", ErrTargetChanged)
	}
	nonce := strings.ReplaceAll(uuid.NewString(), "-", "")
	script, err := batchReadScript(p, target, name, offset, count, nonce)
	if err != nil {
		return nil, traceBatchReadFailure(ctx, "read_guard", "invalid_script_input", err)
	}
	traceBatchReadMethod(ctx, "pane.get")
	r, err := c.native(ctx, scope, target, "pane.get", map[string]any{"session": target.Session, "paneId": pane})
	if err != nil {
		if errors.Is(err, ErrRejected) {
			return nil, traceBatchReadFailure(ctx, "control_lookup", "native_error", ErrUnknown)
		}
		return nil, traceBatchReadFailure(ctx, "control_lookup", "native_error", err)
	}
	var info struct {
		Type string    `json:"type"`
		Pane batchPane `json:"pane"`
	}
	if resultBody(r, &info) != nil || info.Type != "pane_info" || info.Pane.ID != pane || info.Pane.Workspace != workspace || !batchReaderID(info.Pane.Terminal) {
		return nil, traceBatchReadFailure(ctx, "control_lookup", "reference_mismatch", ErrTargetChanged)
	}
	if info.Pane.Terminal != terminal {
		if reader == nil {
			return nil, traceBatchReadFailure(ctx, "control_lookup", "terminal_changed", ErrBatchControlChanged)
		}
		return nil, traceBatchReadFailure(ctx, "control_lookup", "reader_terminal_changed", ErrTargetChanged) // never replace a changed recovery reader
	}
	traceBatchReadMethod(ctx, "pane.run")
	_, err = c.native(ctx, scope, target, "pane.run", map[string]any{"session": target.Session, "paneId": pane, "command": launchBashScript(script, target.BashPath)})
	if err != nil {
		if errors.Is(err, ErrRejected) {
			return nil, traceBatchReadFailure(ctx, "command_dispatch", "native_error", ErrUnknown)
		}
		return nil, traceBatchReadFailure(ctx, "command_dispatch", "native_error", err)
	} // never repeat an uncertain input call
	lines := 8
	for {
		// Do not expand into old history while this response is delayed.
		// Only this nonce's exact end marker permits growing the snapshot.
		// Herdr counts rendered rows even for recent-unwrapped.
		text, err := c.readBatchPane(ctx, scope, target, pane, lines)
		if errors.Is(err, errBatchSnapshotTooLarge) {
			// Old terminal history can exceed 64 KiB even for a legal row
			// count. Narrow only the read; never retype the control command.
			if lines == 1 {
				return nil, traceBatchReadFailure(ctx, "snapshot", "snapshot_too_large", ErrUnknown)
			}
			lines = max(1, lines/2)
		} else if err != nil {
			return nil, err
		}
		var data []byte
		found := false
		ended := false
		for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
			start, end := "MEMOH_FILE_"+nonce+":", ":END_FILE_"+nonce
			ended = ended || strings.HasSuffix(line, end)
			if !strings.HasPrefix(line, start) || !strings.HasSuffix(line, end) {
				continue
			}
			if found {
				return nil, traceBatchReadFailure(ctx, "frame", "duplicate_frame", ErrUnknown)
			}
			if len(line) > base64.StdEncoding.EncodedLen(int(count))+len(start)+len(end) {
				return nil, traceBatchReadFailure(ctx, "frame", "frame_oversize", ErrUnknown)
			}
			data, err = base64.StdEncoding.DecodeString(strings.TrimSuffix(strings.TrimPrefix(line, start), end))
			found = true
			if trace := BatchReadTraceFromContext(ctx); trace != nil {
				trace.ActualBytes = int64(len(data))
			}
			if err != nil {
				return nil, traceBatchReadFailure(ctx, "frame", "invalid_base64", ErrUnknown)
			}
			if int64(len(data)) > count {
				return nil, traceBatchReadFailure(ctx, "frame", "decoded_oversize", ErrUnknown)
			}
		}
		if found {
			return data, nil
		}
		if ended && lines < batchReadMaxLines {
			lines = min(lines*2, batchReadMaxLines)
			continue
		}
		if ended {
			return nil, traceBatchReadFailure(ctx, "frame", "window_exhausted", ErrUnknown) // complete frame does not fit the legal window
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, traceBatchReadFailure(ctx, "frame", "context_ended_waiting_frame", ErrUnknown)
		case <-timer.C:
		}
	}
}

func batchReadScript(p BatchProgress, target Target, name string, offset, count int64, nonce string) (string, error) {
	layout, err := resolveBatchLayout(p)
	if err != nil {
		return "", err
	}
	file, err := layout.resultFile(name)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`set -euo pipefail
memoh_root=%s; memoh_root="${memoh_root%%.}"
memoh_root="$(cygpath -u "$memoh_root")"
memoh_file="$memoh_root/%s"
printf '\r\nMEMOH_FILE_%s:'
if test -f "$memoh_file"; then
  dd if="$memoh_file" bs=1 skip=%d count=%d status=none | base64 -w 0
fi
printf ':END_FILE_%s\r\n'
`, shellData(target.WorkDir), file, nonce, offset, count, nonce), nil
}
