package computertask

import (
	"strconv"

	"github.com/felinics/memoh/internal/agenvo"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

// OperationReconciliation contains only evidence already persisted by the
// operation owner. It neither pauses admission nor authorizes a host stop.
// Revision is a string so standard tool clients do not round large CAS values.
type OperationReconciliation struct {
	Revision          string               `json:"revision"`
	TargetFingerprint string               `json:"target_fingerprint"`
	Generation        string               `json:"generation,omitempty"`
	RequestHash       string               `json:"request_hash,omitempty"`
	TerminalID        string               `json:"terminal_id,omitempty"`
	ControlPaneID     string               `json:"control_pane_id,omitempty"`
	ControlTerminalID string               `json:"control_terminal_id,omitempty"`
	ReceiptState      string               `json:"receipt_state"`
	Receipt           *agenvo.BatchReceipt `json:"receipt,omitempty"`
	Files             []RecoveryFile       `json:"files"`
}

// RecoveryFile describes a previously validated collection checkpoint, not
// present media availability or delivery of declared business artifacts.
// Offset is confirmed bytes, never permission to discard the original file.
type RecoveryFile struct {
	Meta                agenvo.BatchFile `json:"meta"`
	Offset              int64            `json:"confirmed_offset"`
	State               string           `json:"state"`
	RecordedContentHash string           `json:"recorded_content_hash,omitempty"`
	CurrentAvailability string           `json:"current_availability"`
}

func operationReconciliation(r dbstore.ComputerOperationRecord, d Details) *OperationReconciliation {
	out := &OperationReconciliation{
		Revision: strconv.FormatInt(r.Revision, 10), TargetFingerprint: observationHexRef(r.TargetFingerprint, 64),
		ReceiptState: "not_applicable", Files: []RecoveryFile{},
	}
	b := d.Batch
	if b == nil {
		return out
	}
	p := b.Progress
	out.Generation, out.RequestHash = observationHexRef(p.Generation, 32), observationHexRef(p.RequestHash, 64)
	out.TerminalID = observationRef(p.TerminalID)
	out.ControlPaneID, out.ControlTerminalID = observationRef(p.ControlPane), observationRef(p.ControlTerminal)
	out.ReceiptState = "missing"
	if b.Receipt == nil {
		return out
	}
	out.ReceiptState = "invalid"
	if b.Receipt.Validate(p) != nil || observationRef(p.WorkspaceID) == "" || observationRef(p.PaneID) == "" || observationRef(p.TerminalID) == "" {
		return out
	}
	out.ReceiptState, out.Receipt = "validated", b.Receipt
	for _, meta := range []agenvo.BatchFile{b.Receipt.Stdout, b.Receipt.Stderr} {
		file := RecoveryFile{Meta: meta, State: "not_collected", CurrentAvailability: "not_rechecked"}
		if meta.Size == 0 {
			file.State = "empty" // No media object is created for empty files.
		}
		matches := 0
		for _, saved := range b.Files {
			if saved.Meta.Name != meta.Name {
				continue
			}
			matches++
			if matches != 1 || saved.Meta != meta || saved.Offset < 0 || saved.Offset > meta.Size ||
				(saved.ReadyHash != "" && (saved.ReadyHash != meta.Hash || saved.Offset != meta.Size || meta.Size == 0)) {
				file.State, file.Offset, file.RecordedContentHash = "invalid", 0, ""
				continue
			}
			file.Offset = saved.Offset
			switch {
			case saved.ReadyHash != "":
				file.State, file.RecordedContentHash = "recorded", saved.ReadyHash
			case saved.Offset > 0:
				file.State = "collecting"
			}
		}
		out.Files = append(out.Files, file)
	}
	return out
}
