// Package sessiontask projects persisted task facts for the conversation owner.
// It cannot submit, poll a computer, mutate a receipt or retrieve media.
package sessiontask

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	thread "github.com/felinics/memoh/internal/chat/thread"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/toolcontext"
	"github.com/felinics/memoh/internal/workdir"
)

var (
	ErrUnavailable = errors.New("task not available in this conversation")
	ErrCursor      = errors.New("invalid task cursor")
)

const (
	Subagent          = "subagent"
	ComputerOperation = "computer_operation"
	maxDepth          = 64
	scanBudget        = 200
)

type Sessions interface {
	Get(context.Context, string) (thread.Thread, error)
}
type Workdirs interface {
	Get(context.Context, string, string) (workdir.Workdir, error)
}
type Targets interface {
	ResolveTarget(context.Context, agenvo.Scope) (agenvo.Target, error)
}

type Service struct {
	store        dbstore.SessionTaskReader
	sessions     Sessions
	workdirs     Workdirs
	targets      Targets
	cursorCipher cipher.AEAD
}

func NewService(store dbstore.SessionTaskReader, sessions Sessions, workdirs Workdirs, targets Targets, cursorSecret string) *Service {
	s := &Service{store: store, sessions: sessions, workdirs: workdirs, targets: targets}
	// Reuse the configured server secret with domain separation. Cursors are
	// encrypted because filtered/revoked candidate IDs must not leak through
	// a continuation. Possession still grants no authority.
	if cursorSecret != "" {
		key := sha256.Sum256([]byte("memoh/session-tasks/cursor/v1\x00" + cursorSecret))
		block, _ := aes.NewCipher(key[:])
		s.cursorCipher, _ = cipher.NewGCM(block)
	}
	return s
}

type Task struct {
	TaskType  string `json:"task_type" validate:"required" enums:"subagent,computer_operation"`
	TaskID    string `json:"task_id" validate:"required" format:"uuid"`
	SessionID string `json:"session_id" validate:"required" format:"uuid"`
	// ParentSessionID is the requested aggregation root, not a rewritten owner.
	ParentSessionID      string    `json:"parent_session_id" validate:"required" format:"uuid"`
	CreatedAt            time.Time `json:"created_at" validate:"required"`
	UpdatedAt            time.Time `json:"updated_at" validate:"required"`
	Status               string    `json:"status" validate:"required" enums:"unknown,submitted,process_exited,not_started,rejected"`
	Summary              string    `json:"summary" validate:"required"`
	ContentAvailable     bool      `json:"content_available" validate:"required"`
	AttachmentsAvailable bool      `json:"attachments_available" validate:"required"`
	TargetID             string    `json:"target_id,omitempty" format:"uuid"`
	OperationKind        string    `json:"operation_kind,omitempty" enums:"foreground_bash,herdr_pi_task,unknown"`
	Revision             string    `json:"revision,omitempty"`
	CancelRequested      *bool     `json:"cancel_requested,omitempty"`
	ProcessState         string    `json:"process_state,omitempty" enums:"unknown,exited,not_started"`
	PiResult             string    `json:"pi_result,omitempty" enums:"unknown,not_applicable"`
	BusinessState        string    `json:"business_state,omitempty" enums:"unknown"`
}

type Page struct {
	Items      []Task `json:"items" validate:"required"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type cursor struct {
	Version  int                          `json:"v"`
	Bot      string                       `json:"bot"`
	User     string                       `json:"user"`
	Parent   string                       `json:"parent"`
	Snapshot time.Time                    `json:"at"`
	Before   dbstore.SessionTaskCandidate `json:"before"`
}

func (s *Service) decodeCursor(raw, bot, user, parent string) (cursor, error) {
	c := cursor{Version: 1, Bot: bot, User: user, Parent: parent, Snapshot: time.Now().UTC()}
	if s.cursorCipher == nil {
		return c, ErrUnavailable
	}
	if raw == "" {
		return c, nil
	}
	if len(raw) > 2048 {
		return c, ErrCursor
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(data) < s.cursorCipher.NonceSize() {
		return c, ErrCursor
	}
	data, err = s.cursorCipher.Open(nil, data[:s.cursorCipher.NonceSize()], data[s.cursorCipher.NonceSize():], nil)
	if err != nil || json.Unmarshal(data, &c) != nil || c.Version != 1 || c.Bot != bot || c.User != user || c.Parent != parent || c.Snapshot.IsZero() || c.Before.CreatedAt.IsZero() || c.Before.CreatedAt.After(c.Snapshot) || (c.Before.Type != Subagent && c.Before.Type != ComputerOperation) {
		return c, ErrCursor
	}
	if _, err := uuid.Parse(c.Before.ID); err != nil {
		return c, ErrCursor
	}
	return c, nil
}

func (s *Service) encodeCursor(c cursor) (string, error) {
	nonce := make([]byte, s.cursorCipher.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(s.cursorCipher.Seal(nonce, nonce, data, nil)), nil
}

// path validates every ancestor, including ancestors above a requested child
// root. It never trusts a caller-supplied parent or an admin bypass.
func (s *Service) path(ctx context.Context, bot, user, id string) ([]thread.Thread, error) {
	var path []thread.Thread
	seen := map[string]bool{}
	for id != "" {
		if seen[id] || len(path) >= maxDepth {
			return nil, ErrUnavailable
		}
		seen[id] = true
		t, err := s.sessions.Get(ctx, id)
		if err != nil || t.ID != id || t.BotID != bot || t.CreatedByUserID != user || (t.Type != thread.TypeChat && t.Type != Subagent) || (t.ParentThreadID != "" && t.Type != Subagent) {
			return nil, ErrUnavailable
		}
		path = append(path, t)
		id = t.ParentThreadID
	}
	if len(path) == 0 {
		return nil, ErrUnavailable
	}
	return path, nil
}

// scope reads workdir records only. It must never resolve a bridge or probe a
// device: an unavailable computer is still a valid persisted receipt target.
func (s *Service) scope(ctx context.Context, bot, user string, path []thread.Thread) (string, error) {
	selected := "native"
	for i := len(path) - 1; i >= 0; i-- {
		candidate := ""
		if path[i].WorkdirID != "" {
			w, err := s.workdirs.Get(ctx, bot, path[i].WorkdirID)
			if err != nil || w.BotID != bot || w.ID != path[i].WorkdirID || w.WorkspaceTargetID == "" {
				return "", ErrUnavailable
			}
			candidate = w.WorkspaceTargetID
		} else {
			var err error
			candidate, err = selection(path[i].Metadata)
			if err != nil {
				return "", err
			}
			// A missing child selection inherits its parent. Only the top
			// unbound session falls back to the current Bot primary.
			if candidate == "" && i == len(path)-1 {
				primary, err := s.store.GetPrimaryMount(ctx, bot)
				switch {
				case errors.Is(err, db.ErrNotFound):
					candidate = "native"
				case err != nil:
					return "", err
				case primary.BotID != bot || primary.ID == "":
					return "", ErrUnavailable
				default:
					candidate = primary.ID
				}
			}
		}
		if candidate == "" {
			continue
		}
		if selected != "native" && selected != candidate {
			return "", ErrUnavailable
		}
		selected = candidate
	}
	if selected != "native" {
		if _, err := s.targets.ResolveTarget(ctx, agenvo.Scope{BotID: bot, UserID: user, TargetID: selected}); err != nil {
			return "", ErrUnavailable
		}
	}
	return selected, nil
}

// The existing unbound-session picker persists its last selection in
// metadata. This is a selection, never a grant; it cannot override a workdir
// or broaden a PC ancestor. Malformed or conflicting snapshots fail closed.
func selection(metadata map[string]any) (string, error) {
	id := ""
	if raw, ok := metadata["workspace_target_id"]; ok {
		v, valid := raw.(string)
		if !valid || strings.TrimSpace(v) == "" {
			return "", ErrUnavailable
		}
		id = strings.TrimSpace(v)
	}
	if raw, ok := metadata["workspace_target"]; ok {
		snapshot, valid := raw.(map[string]any)
		if !valid {
			return "", ErrUnavailable
		}
		v, valid := snapshot["target_id"].(string)
		v = strings.TrimSpace(v)
		if !valid || v == "" || (id != "" && id != v) {
			return "", ErrUnavailable
		}
		id = v
	}
	return id, nil
}

func (s *Service) authorizeRoot(ctx context.Context, bot, user, parent string) error {
	p, err := s.path(ctx, bot, user, parent)
	if err != nil {
		return err
	}
	_, err = s.scope(ctx, bot, user, p)
	return err
}

func (s *Service) project(ctx context.Context, bot, user, parent string, c dbstore.SessionTaskCandidate) (Task, error) {
	p, err := s.path(ctx, bot, user, c.SessionID)
	if err != nil {
		return Task{}, err
	}
	member := false
	for _, session := range p {
		if session.ID == parent {
			member = true
		}
	}
	if !member {
		return Task{}, ErrUnavailable
	}
	selected, err := s.scope(ctx, bot, user, p)
	if err != nil {
		return Task{}, err
	}
	t := Task{TaskType: c.Type, TaskID: c.ID, SessionID: c.SessionID, ParentSessionID: parent, CreatedAt: c.CreatedAt, UpdatedAt: p[0].UpdatedAt, Status: "unknown", Summary: "Subagent session recorded; live execution status and content are unavailable."}
	if c.Type == Subagent {
		if c.ID != c.SessionID || c.ID == parent || p[0].Type != Subagent {
			return Task{}, ErrUnavailable
		}
		return t, nil
	}
	if c.Type != ComputerOperation {
		return Task{}, ErrUnavailable
	}
	r, err := s.store.ReadComputerOperation(ctx, dbstore.ComputerOperationRecord{ID: c.ID, BotID: bot, UserID: user, SessionID: c.SessionID})
	if err != nil || r.ID != c.ID || r.BotID != bot || r.UserID != user || r.SessionID != c.SessionID || r.TargetID == "" || (selected != "native" && selected != r.TargetID) {
		return Task{}, ErrUnavailable
	}
	target, err := s.targets.ResolveTarget(toolcontext.WithComputerScope(ctx, selected), agenvo.Scope{BotID: bot, UserID: user, TargetID: r.TargetID})
	if err != nil || r.TargetFingerprint == "" || r.TargetFingerprint != agenvo.TargetFingerprint(target) {
		return Task{}, ErrUnavailable
	}
	// Decode only the approved scalar facts. Never deserialize or forward
	// arbitrary output, observation, raw JSONL, metadata, hashes or URLs.
	var facts struct {
		Kind     string `json:"kind"`
		ExitCode *int   `json:"exit_code"`
	}
	if len(r.Details) > 2*1024*1024 || json.Unmarshal(r.Details, &facts) != nil {
		return Task{}, ErrUnavailable
	}
	t.TargetID = r.TargetID
	t.OperationKind = "unknown"
	if facts.Kind == "foreground_bash" || facts.Kind == "herdr_pi_task" {
		t.OperationKind = facts.Kind
	}
	t.Revision = strconv.FormatInt(r.Revision, 10)
	t.CancelRequested = &r.CancelRequested
	t.UpdatedAt = r.UpdatedAt
	t.ProcessState = "unknown"
	t.PiResult = "unknown"
	t.BusinessState = "unknown"
	if facts.Kind == "foreground_bash" {
		t.PiResult = "not_applicable"
	}
	switch r.State {
	case "submitted":
		t.Status = "submitted"
	case "not_started", "rejected":
		t.Status = r.State
		t.ProcessState = "not_started"
	case "completed":
		if facts.Kind == "foreground_bash" && facts.ExitCode != nil {
			t.Status = "process_exited"
			t.ProcessState = "exited"
		}
	}
	t.Summary = "Computer receipt recorded; business completion is unverified."
	if r.CancelRequested {
		t.Summary = "Cancellation requested; this does not confirm that the process stopped."
	}
	return t, nil
}

// List uses immutable creation keys, a page-chain upper bound and a bounded
// scan. Revoked entries disappear; even an empty page can carry a continuation.
func (s *Service) List(ctx context.Context, bot, user, parent string, limit int, rawCursor string) (Page, error) {
	page := Page{Items: []Task{}}
	if err := s.authorizeRoot(ctx, bot, user, parent); err != nil {
		return page, err
	}
	c, err := s.decodeCursor(rawCursor, bot, user, parent)
	if err != nil {
		return page, err
	}
	if limit < 1 || limit > 100 {
		return page, ErrCursor
	}
	rows, err := s.store.ListSessionTaskCandidates(ctx, dbstore.SessionTaskFilter{BotID: bot, UserID: user, ParentID: parent, SnapshotAt: c.Snapshot, Before: c.Before, Limit: scanBudget + 1})
	if err != nil {
		return page, err
	}
	for i, row := range rows {
		if i == scanBudget || len(page.Items) == limit {
			page.NextCursor, err = s.encodeCursor(c)
			if err != nil {
				return Page{}, err
			}
			break
		}
		c.Before = row
		item, err := s.project(ctx, bot, user, parent, row)
		if errors.Is(err, ErrUnavailable) {
			continue
		}
		if err != nil {
			return Page{}, err
		}
		page.Items = append(page.Items, item)
	}
	return page, nil
}

func (s *Service) Get(ctx context.Context, bot, user, parent, kind, id string) (Task, error) {
	if err := s.authorizeRoot(ctx, bot, user, parent); err != nil {
		return Task{}, err
	}
	if kind != Subagent && kind != ComputerOperation {
		return Task{}, ErrUnavailable
	}
	rows, err := s.store.ListSessionTaskCandidates(ctx, dbstore.SessionTaskFilter{BotID: bot, UserID: user, ParentID: parent, SnapshotAt: time.Now().UTC(), Type: kind, ID: id, Limit: 1})
	if err != nil {
		return Task{}, err
	}
	if len(rows) != 1 {
		return Task{}, ErrUnavailable
	}
	return s.project(ctx, bot, user, parent, rows[0])
}
