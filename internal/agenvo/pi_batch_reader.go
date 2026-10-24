package agenvo

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// ErrBatchControlChanged only denotes a well-formed observation of the original
// workspace/pane with a different terminal. Transport, grant, mapping and
// malformed-response errors must not authorize creation of a recovery reader.
var ErrBatchControlChanged = fmt.Errorf("%w: batch control terminal changed", ErrTargetChanged)

// BatchRecoveryReader is private operation-owned metadata, never a model input
// or replacement execution identity. Creating is an unresolved reservation;
// only the coordinator's durable ready record permits commands in this pane.
type BatchRecoveryReader struct {
	RequestID   string `json:"request_id"`
	Phase       string `json:"phase"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	PaneID      string `json:"pane_id,omitempty"`
	TerminalID  string `json:"terminal_id,omitempty"`
}

func batchReaderID(value string) bool {
	return value != "" && !strings.HasPrefix(value, "-") && boundedNativeText(value, 256)
}

func (r BatchRecoveryReader) valid(p BatchProgress) bool {
	_, err := uuid.Parse(r.RequestID)
	return err == nil && r.Phase == "ready" && batchReaderID(r.WorkspaceID) && batchReaderID(r.PaneID) && batchReaderID(r.TerminalID) &&
		r.WorkspaceID != p.WorkspaceID && r.PaneID != p.PaneID && r.PaneID != p.ControlPane && r.TerminalID != p.TerminalID && r.TerminalID != p.ControlTerminal
}

// CreateBatchRecoveryReader issues one official workspace.create after the
// caller has reserved creating with operation CAS. It never searches by label,
// retries, types commands, starts Pi or closes any workspace. The response must
// be persisted by the coordinator before use. A lost response can leave an
// unidentifiable workspace; that uncertainty cannot be repaired by guessing.
func (s *Service) CreateBatchRecoveryReader(ctx context.Context, scope Scope, p BatchProgress, reservation BatchRecoveryReader) (BatchRecoveryReader, error) {
	if !p.valid() || !batchReaderID(p.WorkspaceID) || !batchReaderID(p.PaneID) || !batchReaderID(p.TerminalID) || reservation.Phase != "creating" || reservation.WorkspaceID != "" || reservation.PaneID != "" || reservation.TerminalID != "" {
		return reservation, ErrUnsupported
	}
	if _, err := uuid.Parse(reservation.RequestID); err != nil {
		return reservation, ErrUnsupported
	}
	c, target, err := s.Client(ctx, scope)
	if err != nil {
		return reservation, err
	}
	label := "memoh-reader-" + reservation.RequestID
	created, err := c.native(ctx, scope, target, "workspace.create", map[string]any{"session": target.Session, "cwd": target.WorkDir, "label": label})
	if err != nil {
		return reservation, err
	}
	var ws struct {
		Workspace struct {
			ID    string `json:"workspace_id"`
			Label string `json:"label"`
		} `json:"workspace"`
		Root batchPane `json:"root_pane"`
	}
	if resultBody(created, &ws) != nil || ws.Workspace.Label != label || ws.Root.Workspace != ws.Workspace.ID {
		return reservation, ErrUnknown
	}
	ready := BatchRecoveryReader{RequestID: reservation.RequestID, Phase: "ready", WorkspaceID: ws.Workspace.ID, PaneID: ws.Root.ID, TerminalID: ws.Root.Terminal}
	if !ready.valid(p) {
		return reservation, ErrUnknown
	}
	return ready, nil
}
