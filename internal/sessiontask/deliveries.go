package sessiontask

import (
	"context"
	"strconv"

	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/toolcontext"
)

// DeliveryEntries reads one bounded page after the same parent/descendant, user,
// target and current Bot-permission checks as historical task detail. It never polls a device,
// dispatches work, marks completion, or turns a reference into download authority.
func (s *Service) DeliveryEntries(ctx context.Context, bot, user, parent, kind, id, after string) (dbstore.ComputerOperationRecord, []dbstore.ComputerDeliveryEntry, error) {
	item, err := s.Get(ctx, bot, user, parent, kind, id)
	if err != nil {
		return dbstore.ComputerOperationRecord{}, nil, err
	}
	if item.TaskType != ComputerOperation {
		return dbstore.ComputerOperationRecord{}, nil, ErrUnavailable
	}
	if after != "" {
		cursor, err := strconv.ParseInt(after, 10, 64)
		if err != nil || cursor < 0 || strconv.FormatInt(cursor, 10) != after {
			return dbstore.ComputerOperationRecord{}, nil, ErrCursor
		}
	}
	reader, ok := s.store.(interface {
		ListComputerDeliveryEntries(context.Context, dbstore.ComputerOperationRecord, string, int) ([]dbstore.ComputerDeliveryEntry, error)
	})
	if !ok {
		return dbstore.ComputerOperationRecord{}, nil, ErrUnavailable
	}
	revision, err := strconv.ParseInt(item.Revision, 10, 64)
	if err != nil {
		return dbstore.ComputerOperationRecord{}, nil, ErrUnavailable
	}
	record := dbstore.ComputerOperationRecord{ID: item.TaskID, BotID: bot, UserID: user, SessionID: item.SessionID, TargetID: item.TargetID, Revision: revision}
	entries, err := reader.ListComputerDeliveryEntries(ctx, record, after, deliverymeta.MaxArtifacts+1)
	if err != nil {
		return dbstore.ComputerOperationRecord{}, nil, err
	}
	return record, entries, nil
}

// ComputerBinding resolves the actual operation owner after validating every
// ancestor, the selected device and its current grant. It narrows observation
// to that device even for a NAS parent; callers cannot supply native references.
func (s *Service) ComputerBinding(ctx context.Context, bot, user, parent, kind, id string) (toolcontext.ComputerBinding, error) {
	item, err := s.GetLive(ctx, bot, user, parent, kind, id)
	if err != nil {
		return toolcontext.ComputerBinding{}, err
	}
	if item.TaskType != ComputerOperation {
		return toolcontext.ComputerBinding{}, ErrUnavailable
	}
	return toolcontext.ComputerBinding{BotID: bot, UserID: user, SessionID: item.SessionID, TargetID: item.TargetID}, nil
}
