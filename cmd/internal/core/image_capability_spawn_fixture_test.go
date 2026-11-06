package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/agent/application"
	messagepkg "github.com/felinics/memoh/internal/chat/message"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/messaging"
	"github.com/felinics/memoh/internal/runtimefence"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

const (
	imageSpawnBot      = "00000000-0000-0000-0000-000000000001"
	imageSpawnModel    = "00000000-0000-0000-0000-000000000002"
	imageSpawnProvider = "00000000-0000-0000-0000-000000000003"
	imageSpawnParent   = "00000000-0000-0000-0000-000000000004"
	imageSpawnUser     = "00000000-0000-0000-0000-000000000005"
	imageSpawnOtherBot = "00000000-0000-0000-0000-000000000006"
)

func imageSpawnUUID(id string) pgtype.UUID { var v pgtype.UUID; _ = v.Scan(id); return v }

type imageSpawnStorage struct {
	*localfs.Originals
	opens atomic.Int32
}

func (s *imageSpawnStorage) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	s.opens.Add(1)
	return s.Originals.Open(ctx, key)
}

// Only storage ports are synthetic. Thread creation, model resolution, Bot
// permissions, admission and native execution use their production services.
type imageSpawnQueries struct {
	dbstore.Queries
	mu              sync.Mutex
	model           sqlc.Model
	provider        sqlc.Provider
	sessions        map[string]sqlc.BotSession
	configs         map[string]sqlc.SubagentConfig
	allowed         atomic.Bool
	authorizedChild map[string]string
}

func (q *imageSpawnQueries) GetSettingsByBotID(_ context.Context, id pgtype.UUID) (sqlc.GetSettingsByBotIDRow, error) {
	if id.String() != imageSpawnBot {
		return sqlc.GetSettingsByBotIDRow{}, pgx.ErrNoRows
	}
	return sqlc.GetSettingsByBotIDRow{ImageModelID: q.model.ID, ChatModelID: q.model.ID}, nil
}

func (q *imageSpawnQueries) GetModelByID(_ context.Context, id pgtype.UUID) (sqlc.Model, error) {
	if id != q.model.ID {
		return sqlc.Model{}, pgx.ErrNoRows
	}
	return q.model, nil
}

func (q *imageSpawnQueries) GetProviderByID(_ context.Context, id pgtype.UUID) (sqlc.Provider, error) {
	if id != q.provider.ID {
		return sqlc.Provider{}, pgx.ErrNoRows
	}
	return q.provider, nil
}

func (q *imageSpawnQueries) ListEnabledModelsByType(context.Context, string) ([]sqlc.Model, error) {
	return []sqlc.Model{q.model}, nil
}

func (q *imageSpawnQueries) GetSessionByID(_ context.Context, id pgtype.UUID) (sqlc.BotSession, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	row, ok := q.sessions[id.String()]
	if !ok {
		return row, pgx.ErrNoRows
	}
	return row, nil
}

func (q *imageSpawnQueries) CreateSession(_ context.Context, in sqlc.CreateSessionParams) (sqlc.BotSession, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	row := sqlc.BotSession{ID: imageSpawnUUID(uuid.NewString()), BotID: in.BotID, Type: in.Type, SessionMode: in.SessionMode, RuntimeType: in.RuntimeType, Visibility: in.Visibility, Title: in.Title, Metadata: in.Metadata, RuntimeMetadata: in.RuntimeMetadata, ParentSessionID: in.ParentSessionID, CreatedByUserID: in.CreatedByUserID, ChannelType: in.ChannelType}
	q.sessions[row.ID.String()] = row
	return row, nil
}

func (q *imageSpawnQueries) CreateSubagentConfig(_ context.Context, in sqlc.CreateSubagentConfigParams) (sqlc.SubagentConfig, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	row := sqlc.SubagentConfig{SessionID: in.SessionID, ModelUuid: in.ModelUuid, ModelID: in.ModelID, ProviderName: in.ProviderName, Forked: in.Forked}
	q.configs[in.SessionID.String()] = row
	return row, nil
}

func (q *imageSpawnQueries) GetSubagentConfig(_ context.Context, id pgtype.UUID) (sqlc.SubagentConfig, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	row, ok := q.configs[id.String()]
	if !ok {
		return row, pgx.ErrNoRows
	}
	return row, nil
}

func (q *imageSpawnQueries) ListSubagentSessionsByParent(_ context.Context, id pgtype.UUID) ([]sqlc.BotSession, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	var out []sqlc.BotSession
	for _, s := range q.sessions {
		if s.ParentSessionID == id {
			out = append(out, s)
		}
	}
	return out, nil
}

func (*imageSpawnQueries) CountMessagesBySession(context.Context, pgtype.UUID) (int64, error) {
	return 0, nil
}

func (q *imageSpawnQueries) GetBotByID(ctx context.Context, id pgtype.UUID) (sqlc.GetBotByIDRow, error) {
	if id.String() != imageSpawnBot {
		return sqlc.GetBotByIDRow{}, pgx.ErrNoRows
	}
	if handle, ok := application.SubagentRunHandleFromContext(ctx); ok {
		q.mu.Lock()
		q.authorizedChild[handle.SessionID] = handle.RunID
		q.mu.Unlock()
	}
	owner := imageSpawnUser
	if !q.allowed.Load() {
		owner = imageSpawnOtherBot
	}
	return sqlc.GetBotByIDRow{ID: id, OwnerUserID: imageSpawnUUID(owner), Name: "synthetic-image-bot", IsActive: true, Metadata: []byte(`{}`)}, nil
}

func (*imageSpawnQueries) ListBotUserGrantsForUser(context.Context, sqlc.ListBotUserGrantsForUserParams) ([]sqlc.ListBotUserGrantsForUserRow, error) {
	return nil, nil
}

type imageSpawnAccounts struct{ dbstore.AccountStore }

func (imageSpawnAccounts) GetByUserID(_ context.Context, id string) (dbstore.AccountRecord, error) {
	if id != imageSpawnUser {
		return dbstore.AccountRecord{}, errors.New("synthetic account missing")
	}
	return dbstore.AccountRecord{ID: id, Role: "user", IsActive: true}, nil
}

type imageSpawnMessages struct {
	messagepkg.Service
	mu    sync.Mutex
	rows  []messagepkg.Message
	steps int
}

func (m *imageSpawnMessages) Persist(ctx context.Context, in messagepkg.PersistInput) (messagepkg.Message, error) {
	f, ok := runtimefence.FromContext(ctx)
	if !ok || f.Token <= 0 || f.BotID != in.BotID || f.SessionID != in.SessionID {
		return messagepkg.Message{}, errors.New("synthetic store requires admitted fence")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	row := messagepkg.Message{ID: uuid.NewString(), BotID: in.BotID, SessionID: in.SessionID, Role: in.Role, Content: append(json.RawMessage(nil), in.Content...), Metadata: in.Metadata}
	m.rows = append(m.rows, row)
	return row, nil
}

func (m *imageSpawnMessages) ListBySession(_ context.Context, id string) ([]messagepkg.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []messagepkg.Message
	for _, row := range m.rows {
		if row.SessionID == id {
			out = append(out, row)
		}
	}
	return out, nil
}

func (m *imageSpawnMessages) PersistAgentStep(ctx context.Context, step messagepkg.AgentStep) ([]messagepkg.Message, error) {
	var out []messagepkg.Message
	for _, in := range step.Messages {
		row, err := m.Persist(ctx, in)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	m.mu.Lock()
	m.steps++
	m.mu.Unlock()
	return out, nil
}

type imageSpawnSender struct {
	media *media.Service
	calls atomic.Int32
	hash  string
	bytes []byte
}

func (*imageSpawnSender) ParseChannelType(raw string) (messaging.Platform, error) {
	if raw != "local" {
		return "", errors.New("synthetic surface only")
	}
	return messaging.Platform(raw), nil
}

func (s *imageSpawnSender) Send(ctx context.Context, bot string, _ messaging.Platform, req messaging.SendRequest) error {
	if bot != imageSpawnBot || len(req.Message.Attachments) != 1 {
		return errors.New("unexpected synthetic delivery")
	}
	s.hash = req.Message.Attachments[0].ContentHash
	r, _, err := s.media.Open(ctx, bot, s.hash)
	if err != nil {
		return err
	}
	s.bytes, err = io.ReadAll(r)
	err = errors.Join(err, r.Close())
	if err == nil {
		s.calls.Add(1)
	}
	return err
}
