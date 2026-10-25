package botdelegation

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/db/postgres/sqlc"
)

var ErrConflict = errors.New("delegation request conflicts with an existing handoff")

type Payload struct {
	ReturnRoute     *ReturnRoute `json:"return_route,omitempty"`
	Message         string       `json:"message"`
	Inputs          []string     `json:"inputs"`
	Devices         []DevicePair `json:"devices"`
	TargetSelection string       `json:"target_selection"`
}

// ReturnRoute is a snapshot of the existing host-owned conversation route.
// It is not part of Input or View and never contains tokens or channel secrets.
type ReturnRoute struct {
	Version          int    `json:"version"`
	Channel          string `json:"channel"`
	ChatID           string `json:"chat_id"`
	RouteID          string `json:"route_id,omitempty"`
	ChannelConfigID  string `json:"channel_config_id,omitempty"`
	ReplyTarget      string `json:"reply_target"`
	ConversationType string `json:"conversation_type,omitempty"`
}

type Result struct {
	Summary *Summary `json:"summary,omitempty"`
	Outputs []string `json:"outputs,omitempty"`
	Reason  string   `json:"reason,omitempty"`
}

type Record struct {
	Identity                                           Identity
	ID, RequestID, RunID, Fingerprint, SourceSelection string
	State, DeliveryState                               string
	Payload                                            Payload
	Result                                             Result
	Revision                                           int64
	CreatedAt, UpdatedAt                               time.Time
}

type Filter struct {
	Bot, User, Session   string
	Snapshot, BeforeTime time.Time
	BeforeID             string
	Limit                int32
}

type Ledger interface {
	Claim(context.Context, Record) (Record, bool, error)
	Get(context.Context, string, string, string, string) (Record, error)
	ForTarget(context.Context, string, string) (Record, error)
	Save(context.Context, Record) (Record, error)
	List(context.Context, Filter) ([]Record, error)
	TargetRun(context.Context, Record) (*RunObservation, error)
}

// RunObservation contains durable native-run facts, not a live execution lease
// or diagnostic logs. Admission placeholders are never observations.
type RunObservation struct {
	ID, State            string
	CreatedAt, UpdatedAt time.Time
}

type PostgresLedger struct{ q *sqlc.Queries }

func NewPostgresLedger(q *sqlc.Queries) *PostgresLedger { return &PostgresLedger{q: q} }

func (s *PostgresLedger) TargetRun(ctx context.Context, r Record) (*RunObservation, error) {
	if r.Identity.TargetSessionID == "" {
		return nil, nil
	}
	row, err := s.q.GetSessionRun(ctx, pgID(r.RunID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if row.TeamID != pgID(r.Identity.TeamID) || row.BotID != pgID(r.Identity.TargetBotID) || row.SessionID != pgID(r.Identity.TargetSessionID) || row.InvocationID != "bot-delegation:"+r.ID {
		return nil, nil
	}
	session, err := s.q.GetSessionByID(ctx, row.SessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// Match the native latest-run visibility fence without substituting another
	// turn for this delegation's exact run after a new turn or history reset.
	if session.TeamID != row.TeamID || session.BotID != row.BotID || session.CreatedByUserID != pgID(r.Identity.UserID) || session.DeletedAt.Valid || row.ErrorCode.String == "history_reset" || (row.FencingToken > 0 && session.RuntimeFencingToken > row.FencingToken) {
		return nil, nil
	}
	return &RunObservation{ID: row.RunID.String(), State: row.State, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}, nil
}

func pgID(id string) pgtype.UUID {
	u, err := uuid.Parse(id)
	return pgtype.UUID{Bytes: u, Valid: err == nil}
}

func (s *PostgresLedger) Claim(ctx context.Context, r Record) (Record, bool, error) {
	payload, err := json.Marshal(r.Payload)
	if err != nil {
		return Record{}, false, err
	}
	b := r.Identity
	row, err := s.q.CreateBotDelegation(ctx, sqlc.CreateBotDelegationParams{
		ID: pgID(r.ID), TeamID: pgID(b.TeamID), UserID: pgID(b.UserID), SourceBotID: pgID(b.SourceBotID), SourceSessionID: pgID(b.SourceSessionID), SourceRootID: pgID(b.SourceRootID), TargetBotID: pgID(b.TargetBotID), RequestID: pgID(r.RequestID), RunID: pgID(r.RunID), Fingerprint: r.Fingerprint, SourceSelection: r.SourceSelection, Payload: payload,
	})
	created := err == nil
	if errors.Is(err, pgx.ErrNoRows) {
		row, err = s.q.FindBotDelegation(ctx, sqlc.FindBotDelegationParams{SourceBotID: pgID(b.SourceBotID), SourceSessionID: pgID(b.SourceSessionID), UserID: pgID(b.UserID), RequestID: pgID(r.RequestID), TargetBotID: pgID(b.TargetBotID), Fingerprint: r.Fingerprint})
	}
	if err != nil {
		return Record{}, false, err
	}
	if row.Fingerprint != r.Fingerprint || row.TargetBotID.String() != b.TargetBotID {
		return Record{}, false, ErrConflict
	}
	got, err := fromRow(row)
	return got, created, err
}

func (s *PostgresLedger) Get(ctx context.Context, bot, user, session, id string) (Record, error) {
	row, err := s.q.GetBotDelegation(ctx, sqlc.GetBotDelegationParams{ID: pgID(id), SourceBotID: pgID(bot), SourceSessionID: pgID(session), UserID: pgID(user)})
	if err != nil {
		return Record{}, err
	}
	return fromRow(row)
}

func (s *PostgresLedger) ForTarget(ctx context.Context, bot, session string) (Record, error) {
	row, err := s.q.GetBotDelegationForTargetSession(ctx, sqlc.GetBotDelegationForTargetSessionParams{TargetBotID: pgID(bot), TargetSessionID: pgID(session)})
	if err != nil {
		return Record{}, err
	}
	return fromRow(row)
}

func (s *PostgresLedger) Save(ctx context.Context, r Record) (Record, error) {
	result, err := json.Marshal(r.Result)
	if err != nil {
		return Record{}, err
	}
	row, err := s.q.UpdateBotDelegation(ctx, sqlc.UpdateBotDelegationParams{RunID: pgID(r.RunID), ID: pgID(r.ID), UserID: pgID(r.Identity.UserID), TargetSessionID: pgID(r.Identity.TargetSessionID), State: r.State, DeliveryState: r.DeliveryState, Result: result, Revision: r.Revision})
	if err != nil {
		return Record{}, err
	}
	return fromRow(row)
}

func (s *PostgresLedger) List(ctx context.Context, f Filter) ([]Record, error) {
	before := f.BeforeID
	if before == "" {
		before = uuid.Nil.String()
	}
	rows, err := s.q.ListBotDelegations(ctx, sqlc.ListBotDelegationsParams{SourceBotID: pgID(f.Bot), UserID: pgID(f.User), SourceSessionID: pgID(f.Session), SnapshotAt: pgtype.Timestamptz{Time: f.Snapshot, Valid: true}, UseCursor: f.BeforeID != "", BeforeTime: pgtype.Timestamptz{Time: f.BeforeTime, Valid: true}, BeforeID: pgID(before), PageSize: f.Limit})
	if err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(rows))
	for _, row := range rows {
		r, err := fromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func fromRow(row sqlc.BotDelegation) (Record, error) {
	r := Record{Identity: Identity{TeamID: row.TeamID.String(), UserID: row.UserID.String(), SourceBotID: row.SourceBotID.String(), SourceSessionID: row.SourceSessionID.String(), SourceRootID: row.SourceRootID.String(), TargetBotID: row.TargetBotID.String()}, ID: row.ID.String(), RequestID: row.RequestID.String(), RunID: row.RunID.String(), Fingerprint: row.Fingerprint, SourceSelection: row.SourceSelection, State: row.State, DeliveryState: row.DeliveryState, Revision: row.Revision, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
	r.Identity.SourceSelection = row.SourceSelection
	if row.TargetSessionID.Valid {
		r.Identity.TargetSessionID = row.TargetSessionID.String()
	}
	if err := json.Unmarshal(row.Payload, &r.Payload); err != nil {
		return Record{}, err
	}
	if err := json.Unmarshal(row.Result, &r.Result); err != nil {
		return Record{}, err
	}
	return r, nil
}
