// Package followupqueue supplies application-owned persistence for the existing
// session queue port. It does not execute turns or replace runtime admission.
package followupqueue

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	runtime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
)

// State retains terminal receipts as well as pending input. Runtime TTL and
// transient compaction cannot erase a durable input's idempotency identity.
type State struct {
	Items      []runtime.FollowUpItem                   `json:"items"`
	HTTP       map[string]bool                          `json:"http,omitempty"`
	Promotions map[string]runtime.PromoteFollowUpResult `json:"promotions,omitempty"`
	Claims     map[string]string                        `json:"claims"`
}

type Store struct {
	pool *pgxpool.Pool
	box  cipher.AEAD
}

// New uses existing server key material; it creates no new credential. Queue
// credential-bearing envelopes require encryption. Credential-free HTTP input
// can be retained without provisioning a new encryption key.
// The existing at-rest key (not the login JWT key) must remain available until
// receipts are migrated; an unreadable envelope is an error, never an empty queue.
func New(pool *pgxpool.Pool, encodedKey string) (*Store, error) {
	if pool == nil {
		return nil, errors.New("durable follow-up storage requires postgres")
	}
	s := &Store{pool: pool}
	encodedKey = strings.TrimSpace(encodedKey)
	if encodedKey == "" {
		return s, nil
	}
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil || len(key) != 32 {
		return nil, errors.New("existing at-rest encryption key is invalid")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	s.box, err = cipher.NewGCM(block)
	return s, err
}

func hasCredential(state State) bool {
	for _, item := range state.Items {
		var payload map[string]json.RawMessage
		if json.Unmarshal(item.Payload, &payload) != nil {
			return true
		}
		for _, name := range []string{"command", "internal_command"} {
			if raw := payload[name]; len(raw) > 0 {
				var command map[string]json.RawMessage
				if json.Unmarshal(raw, &command) != nil {
					return true
				}
				for _, field := range []string{"Token", "ChatToken", "SessionToken"} {
					if v := command[field]; len(v) > 0 && !bytes.Equal(v, []byte(`""`)) && !bytes.Equal(v, []byte(`null`)) {
						return true
					}
				}
			}
		}
	}
	return false
}

func (s *Store) encode(key runtime.Key, state State) ([]byte, error) {
	raw, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	if s.box == nil {
		if hasCredential(state) {
			return nil, errors.New("queued credentials require the configured at-rest encryption capability")
		}
		return append([]byte("v1p:"), raw...), nil
	}
	nonce := make([]byte, s.box.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return append([]byte("v1e:"), s.box.Seal(nonce, nonce, raw, []byte("follow-up/"+key.String()))...), nil
}

func (s *Store) decode(key runtime.Key, raw []byte) (State, error) {
	var state State
	var plain []byte
	switch {
	case bytes.HasPrefix(raw, []byte("v1p:")):
		plain = raw[4:]
	case bytes.HasPrefix(raw, []byte("v1e:")):
		if s.box == nil {
			return state, errors.New("durable follow-up encryption key unavailable")
		}
		raw = raw[4:]
		if len(raw) < s.box.NonceSize() {
			return state, errors.New("invalid durable follow-up envelope")
		}
		var err error
		plain, err = s.box.Open(nil, raw[:s.box.NonceSize()], raw[s.box.NonceSize():], []byte("follow-up/"+key.String()))
		if err != nil {
			return state, fmt.Errorf("read durable follow-up envelope: %w", err)
		}
	default:
		return state, errors.New("unsupported durable follow-up envelope")
	}
	if err := json.Unmarshal(plain, &state); err != nil {
		return state, err
	}
	if state.HTTP == nil {
		state.HTTP = map[string]bool{}
	}
	if state.Promotions == nil {
		state.Promotions = map[string]runtime.PromoteFollowUpResult{}
	}
	if state.Claims == nil {
		state.Claims = map[string]string{}
	}
	return state, nil
}

// Change locks one queue row for the complete read/modify/write operation.
// Commit failure is returned without converting it to rejection or success.
// The caller must resolve an uncertain write by its original identity.
func (s *Store) Change(ctx context.Context, key runtime.Key, fn func(*State) error) error {
	if key.BotID == "" || key.SessionID == "" {
		return runtime.ErrQueueInvalidReference
	}
	botID, err := db.ParseUUID(key.BotID)
	if err != nil {
		return err
	}
	sessionID, err := db.ParseUUID(key.SessionID)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	empty, err := s.encode(key, State{Claims: map[string]string{}})
	if err != nil {
		return err
	}
	queries := sqlc.New(tx)
	err = queries.EnsureSessionFollowUpQueue(ctx, sqlc.EnsureSessionFollowUpQueueParams{BotID: botID, SessionID: sessionID, Envelope: empty})
	if err != nil {
		return err
	}
	raw, err := queries.LockSessionFollowUpQueue(ctx, sqlc.LockSessionFollowUpQueueParams{BotID: botID, SessionID: sessionID})
	if err != nil {
		return err
	}
	state, err := s.decode(key, raw)
	if err != nil {
		return err
	}
	if err = fn(&state); err != nil {
		return err
	}
	raw, err = s.encode(key, state)
	if err != nil {
		return err
	}
	if err = queries.WriteSessionFollowUpQueue(ctx, sqlc.WriteSessionFollowUpQueueParams{BotID: botID, SessionID: sessionID, Envelope: raw}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) Read(ctx context.Context, key runtime.Key) (State, error) {
	botID, err := db.ParseUUID(key.BotID)
	if err != nil {
		return State{}, err
	}
	sessionID, err := db.ParseUUID(key.SessionID)
	if err != nil {
		return State{}, err
	}
	raw, err := sqlc.New(s.pool).ReadSessionFollowUpQueue(ctx, sqlc.ReadSessionFollowUpQueueParams{BotID: botID, SessionID: sessionID})
	if errors.Is(err, pgx.ErrNoRows) {
		return State{Claims: map[string]string{}}, nil
	}
	if err != nil {
		return State{}, err
	}
	return s.decode(key, raw)
}

func (s *Store) Keys(ctx context.Context) ([]runtime.Key, error) {
	rows, err := sqlc.New(s.pool).ListSessionFollowUpQueueKeys(ctx)
	if err != nil {
		return nil, err
	}
	keys := make([]runtime.Key, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, runtime.Key{BotID: row.BotID, SessionID: row.SessionID})
	}
	return keys, nil
}
