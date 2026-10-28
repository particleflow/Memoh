package sessiontask

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/botdelegation"
	thread "github.com/felinics/memoh/internal/chat/thread"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

const directoryCursorScope = "bot-task-directory/v1"

func directoryKind(kind string) bool {
	return kind == "native_agent" || kind == ComputerOperation || kind == "bot_delegation" || kind == "schedule_fire"
}

type DelegationDirectory interface {
	DirectoryMetadata(context.Context, string, string, string, string) (botdelegation.View, error)
}

func (s *Service) WithDelegationDirectory(reader DelegationDirectory) *Service {
	s.delegations = reader
	return s
}

// DirectoryTask retains an original typed task ID and its durable occurrence.
// Native bg IDs are opaque and can recur across process lifetimes. RecordID is
// the original admission ID (or run ID for legacy occurrences), not a replacement task identity.
type DirectoryTask struct {
	TaskType    string           `json:"task_type" validate:"required" enums:"native_agent,computer_operation,bot_delegation,schedule_fire"`
	TaskID      string           `json:"task_id" validate:"required"`
	RecordID    string           `json:"record_id" validate:"required" format:"uuid"`
	Name        string           `json:"name" validate:"required"`
	State       string           `json:"state" validate:"required"`
	CreatedAt   time.Time        `json:"created_at" validate:"required"`
	Destination *TaskDestination `json:"destination,omitempty"`
}

type TaskDestination struct {
	BotID     string `json:"bot_id" validate:"required" format:"uuid"`
	SessionID string `json:"session_id" validate:"required" format:"uuid"`
	RunID     string `json:"run_id,omitempty"`
}

type DirectoryPage struct {
	Items      []DirectoryTask `json:"items" validate:"required"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

// Preserve read failures separately from a confirmed access denial. The shared
// activity traversal has a boolean visibility port, so retain its first error.
func (s *Service) directoryPath(ctx context.Context, bot, id string, readable func(thread.Thread) (bool, error)) ([]thread.Thread, error) {
	var readErr error
	path, err := s.activityPath(ctx, bot, id, func(row thread.Thread) bool {
		if readErr != nil {
			return false
		}
		var allowed bool
		allowed, readErr = readable(row)
		return allowed && readErr == nil
	})
	if readErr != nil {
		return nil, readErr
	}
	return path, err
}

func (s *Service) directoryTask(ctx context.Context, bot, user string, row dbstore.BotTaskDirectoryCandidate, readable func(thread.Thread) (bool, error), schedules bool) (DirectoryTask, error) {
	item := DirectoryTask{TaskType: row.Type, TaskID: row.ID, RecordID: row.RecordID, Name: row.Name, State: row.State, CreatedAt: row.CreatedAt}
	if !directoryKind(row.Type) || row.ID == "" || row.RecordID == "" {
		return item, ErrUnavailable
	}
	if item.State == "" {
		item.State = "unknown"
	}
	if row.Type == "bot_delegation" {
		if s.delegations == nil || row.OwnerUserID != user {
			return item, ErrUnavailable
		}
		v, err := s.delegations.DirectoryMetadata(ctx, row.SourceBotID, user, row.SourceSessionID, row.ID)
		if errors.Is(err, botdelegation.ErrDenied) {
			return item, ErrUnavailable
		}
		if err != nil {
			return item, err
		}
		if v.ID != row.ID || (row.SourceBotID != bot && v.TargetBotID != bot) {
			return item, ErrUnavailable
		}
		if _, err := s.directoryPath(ctx, row.SourceBotID, row.SourceSessionID, readable); err != nil {
			return item, err
		}
		item.State = v.State
		if v.TargetSessionID != "" {
			path, err := s.directoryPath(ctx, v.TargetBotID, v.TargetSessionID, readable)
			if err != nil {
				return item, err
			}
			item.Name = path[0].Title
			item.Destination = &TaskDestination{BotID: v.TargetBotID, SessionID: v.TargetSessionID, RunID: v.TargetRunID}
		}
		return item, nil
	}
	if row.SourceBotID != bot {
		return item, ErrUnavailable
	}
	if row.Type == "schedule_fire" {
		if !schedules {
			return item, ErrUnavailable
		}
		// A fire that failed before obtaining a session is still a real task.
		if row.SessionID == "" {
			return item, nil
		}
	}
	path, err := s.directoryPath(ctx, bot, row.SessionID, readable)
	if err != nil {
		// Managers may retain schedule-log facts after history deletion, but
		// there is no navigable session to expose. Other errors are not success.
		if row.Type == "schedule_fire" && errors.Is(err, ErrUnavailable) {
			return item, nil
		}
		return item, err
	}
	if _, err = s.scope(ctx, bot, user, path); err != nil {
		return item, err
	}
	if row.Type == "native_agent" && (path[0].Type != Subagent || path[0].ParentThreadID != row.SourceSessionID || (!row.QueueAdmission && row.RunID != row.RecordID)) {
		return item, ErrUnavailable
	}
	if row.Type == ComputerOperation {
		projected, err := s.project(ctx, bot, user, path[len(path)-1].ID, dbstore.SessionTaskCandidate{Type: ComputerOperation, ID: row.ID, SessionID: row.SessionID, CreatedAt: row.CreatedAt})
		if err != nil {
			return item, err
		}
		item.State = projected.Status
		// A provider run_id is opaque TEXT, never a native conversation run.
		row.RunID = ""
	}
	item.Destination = &TaskDestination{BotID: bot, SessionID: row.SessionID, RunID: row.RunID}
	return item, nil
}

// ListDirectory scans all persisted task occurrences, including terminal and
// unknown states. The bound is per page, never a cap on historical discovery.
func (s *Service) ListDirectory(ctx context.Context, bot, user string, limit int, raw string, readable func(thread.Thread) (bool, error), schedules bool) (DirectoryPage, error) {
	page := DirectoryPage{Items: []DirectoryTask{}}
	if limit < 1 || limit > 100 {
		return page, ErrCursor
	}
	reader, ok := s.store.(dbstore.BotTaskDirectoryReader)
	if !ok {
		return page, ErrUnavailable
	}
	c, err := s.decodeCursor(raw, bot, user, directoryCursorScope)
	if err != nil {
		return page, err
	}
	rows, err := reader.ListBotTaskDirectoryCandidates(ctx, dbstore.SessionTaskFilter{BotID: bot, UserID: user, SnapshotAt: c.Snapshot, Before: c.Before, Limit: scanBudget + 1})
	if err != nil {
		return page, err
	}
	for i, row := range rows {
		if i == scanBudget || len(page.Items) == limit {
			page.NextCursor, err = s.encodeCursor(c)
			return page, err
		}
		c.Before = dbstore.SessionTaskCandidate{Type: row.Type, ID: row.RecordID, CreatedAt: row.CreatedAt}
		item, err := s.directoryTask(ctx, bot, user, row, readable, schedules)
		if errors.Is(err, ErrUnavailable) {
			continue
		}
		if err != nil {
			return DirectoryPage{}, err
		}
		page.Items = append(page.Items, item)
	}
	return page, nil
}

func (s *Service) GetDirectoryTask(ctx context.Context, bot, user, kind, recordID string, readable func(thread.Thread) (bool, error), schedules bool) (DirectoryTask, error) {
	if !directoryKind(kind) {
		return DirectoryTask{}, ErrUnavailable
	}
	if id, err := uuid.Parse(recordID); err != nil || id == uuid.Nil {
		return DirectoryTask{}, ErrUnavailable
	}
	reader, ok := s.store.(dbstore.BotTaskDirectoryReader)
	if !ok {
		return DirectoryTask{}, ErrUnavailable
	}
	rows, err := reader.ListBotTaskDirectoryCandidates(ctx, dbstore.SessionTaskFilter{BotID: bot, UserID: user, SnapshotAt: time.Now().UTC(), Type: kind, ID: recordID, Limit: 1})
	if err != nil {
		return DirectoryTask{}, err
	}
	if len(rows) != 1 || rows[0].Type != kind || rows[0].RecordID != recordID {
		return DirectoryTask{}, ErrUnavailable
	}
	return s.directoryTask(ctx, bot, user, rows[0], readable, schedules)
}
