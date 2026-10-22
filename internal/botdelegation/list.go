package botdelegation

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrCursor = errors.New("invalid delegation cursor")

type Page struct {
	Items      []View `json:"items" validate:"required"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type cursor struct {
	Version                       int `json:"v"`
	Bot, User, Session, Selection string
	Snapshot, BeforeTime          time.Time
	BeforeID                      string
}

// SetCursorSecret reuses the server secret, with an independent domain. It is
// configured once before serving requests and does not grant attachment access.
func (s *Service) SetCursorSecret(secret string) {
	if secret == "" {
		return
	}
	key := sha256.Sum256([]byte("memoh/bot-delegation/cursor/v1\x00" + secret))
	block, _ := aes.NewCipher(key[:])
	s.cursorCipher, _ = cipher.NewGCM(block)
}

func (s *Service) decodeCursor(raw string, b Identity) (cursor, error) {
	c := cursor{Version: 1, Bot: b.SourceBotID, User: b.UserID, Session: b.SourceSessionID, Selection: b.SourceSelection, Snapshot: time.Now().UTC()}
	if s.cursorCipher == nil {
		return c, ErrDenied
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
	if err != nil || json.Unmarshal(data, &c) != nil || c.Version != 1 || c.Bot != b.SourceBotID || c.User != b.UserID || c.Session != b.SourceSessionID || c.Selection != b.SourceSelection || c.Snapshot.IsZero() || c.BeforeTime.IsZero() || c.BeforeTime.After(c.Snapshot) {
		return c, ErrCursor
	}
	if _, err = uuid.Parse(c.BeforeID); err != nil {
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

// List scans a bounded number of persisted candidates. Revoked target Bots
// are omitted; a nonempty cursor must be followed even when a page is empty.
// Creation order is fixed by the cursor snapshot; status remains live.
func (s *Service) List(ctx context.Context, bot, user, session string, limit int, raw string) (Page, error) {
	p := Page{Items: []View{}}
	b, err := s.Authority.Source(ctx, user, bot, session)
	if err != nil {
		return p, err
	}
	if limit < 1 || limit > 100 {
		return p, ErrInput
	}
	c, err := s.decodeCursor(raw, b)
	if err != nil {
		return p, err
	}
	for scanned := 0; scanned < 200; {
		rows, err := s.Ledger.List(ctx, Filter{Bot: bot, User: user, Session: session, Snapshot: c.Snapshot, BeforeTime: c.BeforeTime, BeforeID: c.BeforeID, Limit: 1})
		if err != nil {
			return p, err
		}
		if len(rows) == 0 {
			return p, nil
		}
		r := rows[0]
		c.BeforeTime, c.BeforeID = r.CreatedAt, r.ID
		scanned++
		v, err := s.project(ctx, r)
		if err != nil && !errors.Is(err, ErrDenied) {
			return p, err
		}
		if err == nil {
			p.Items = append(p.Items, v)
		}
		if len(p.Items) == limit || scanned == 200 {
			p.NextCursor, err = s.encodeCursor(c)
			return p, err
		}
	}
	return p, nil
}
