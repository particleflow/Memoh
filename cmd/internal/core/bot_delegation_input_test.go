package core

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/db/postgres/sqlc"
)

func TestDelegationPendingInputRequiresCurrentOwnedAdmission(t *testing.T) {
	bot, session, user, team := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	hash := "05e4f2914ef7436d3cd626b3e0745c7c4f83cdcd29ee63b26a53c13934bb5c91"
	payload := map[string]any{"kind": "message", "session_id": session, "attachments": "opaque-websocket-digest", "resume": map[string]any{"version": 1, "user_id": user, "input_asset_hashes": []string{hash}}}
	raw, _ := json.Marshal(payload)
	baseRun := sqlc.SessionRun{BotID: db.ParseUUIDOrEmpty(bot), SessionID: db.ParseUUIDOrEmpty(session), TeamID: db.ParseUUIDOrEmpty(team), FencingToken: 7, State: "running", InputJson: raw}
	baseSession := sqlc.BotSession{ID: baseRun.SessionID, BotID: baseRun.BotID, TeamID: baseRun.TeamID, CreatedByUserID: db.ParseUUIDOrEmpty(user), RuntimeFencingToken: 7}
	if !admittedDelegationInput(baseRun, baseSession, bot, hash) {
		t.Fatal("current input was not recognized before first step persistence")
	}
	for _, tc := range []struct {
		name  string
		alter func(*sqlc.SessionRun, *sqlc.BotSession)
	}{
		{"old fence", func(_ *sqlc.SessionRun, s *sqlc.BotSession) { s.RuntimeFencingToken++ }},
		{"unclaimed", func(r *sqlc.SessionRun, s *sqlc.BotSession) { r.FencingToken = 0; s.RuntimeFencingToken = 0 }},
		{"terminal", func(r *sqlc.SessionRun, _ *sqlc.BotSession) { r.State = "completed" }},
		{"lost", func(r *sqlc.SessionRun, _ *sqlc.BotSession) { r.State = "lost" }},
		{"accepted before authenticated resume", func(r *sqlc.SessionRun, _ *sqlc.BotSession) { r.State = "accepted" }},
		{"other bot", func(r *sqlc.SessionRun, _ *sqlc.BotSession) { r.BotID = db.ParseUUIDOrEmpty(uuid.NewString()) }},
		{"other team", func(r *sqlc.SessionRun, _ *sqlc.BotSession) { r.TeamID = db.ParseUUIDOrEmpty(uuid.NewString()) }},
		{"other session", func(r *sqlc.SessionRun, _ *sqlc.BotSession) { r.SessionID = db.ParseUUIDOrEmpty(uuid.NewString()) }},
		{"other user", func(_ *sqlc.SessionRun, s *sqlc.BotSession) {
			s.CreatedByUserID = db.ParseUUIDOrEmpty(uuid.NewString())
		}},
		{"ownerless", func(_ *sqlc.SessionRun, s *sqlc.BotSession) { s.CreatedByUserID.Valid = false }},
		{"deleted", func(_ *sqlc.SessionRun, s *sqlc.BotSession) { s.DeletedAt.Valid = true }},
		{"malformed input", func(r *sqlc.SessionRun, _ *sqlc.BotSession) { r.InputJson = []byte(`{`) }},
		{"only model metadata", func(r *sqlc.SessionRun, _ *sqlc.BotSession) {
			r.InputJson = []byte(`{"metadata":{"attachments":["hash:` + hash + `"]}}`)
		}},
		{"only submitted input without host references", func(r *sqlc.SessionRun, _ *sqlc.BotSession) {
			r.InputJson, _ = json.Marshal(map[string]any{"bot_id": bot, "thread_id": session, "attachments": []string{"hash:" + hash}, "resume": map[string]any{"version": 1, "user_id": user}})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, s := baseRun, baseSession
			tc.alter(&r, &s)
			if admittedDelegationInput(r, s, bot, hash) {
				t.Fatal("untrusted or retired input authorized")
			}
		})
	}
	if admittedDelegationInput(baseRun, baseSession, bot, "other") {
		t.Fatal("unsubmitted original was authorized")
	}
}
