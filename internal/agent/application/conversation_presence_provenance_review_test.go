package application

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/conversationpresence"
	"github.com/felinics/memoh/internal/inputsource"
)

// Independent follow-up of the original SQL-derived machine-only history case.
// No model, credential, provider, network or durable business row is accessed.
func TestIndependentPresenceMachineHistoryFollowup(t *testing.T) {
	const bot, session = "review-bot", "review-session"
	now := time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC)
	direct := inputsource.Source{Kind: inputsource.User, BotID: bot, SessionID: session, UserID: "review-user"}
	for _, tc := range []struct {
		name   string
		source any
		want   conversationpresence.Temperature
	}{
		{"schedule", inputsource.Source{Kind: inputsource.Schedule, BotID: bot, SessionID: session, TaskID: "review-task"}, conversationpresence.Cold},
		{"task result", inputsource.Source{Kind: inputsource.Result, BotID: bot, SessionID: session, TaskID: "review-task"}, conversationpresence.Cold},
		{"unknown history stays conservative", nil, conversationpresence.Uncertain},
		{"foreign machine stays conservative", inputsource.Source{Kind: inputsource.Result, BotID: "foreign-bot", SessionID: session, TaskID: "review-task"}, conversationpresence.Uncertain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal([]map[string]any{{"source": tc.source, "created_at": now.Add(-time.Minute)}})
			if err != nil {
				t.Fatal(err)
			}
			count, last, reliable, err := presenceHistoryContinuity(raw, bot, session, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			facts := conversationpresence.Facts{Admitted: true, FirstDirectInput: count == 0, HistoryAvailable: reliable, PreviousSettled: true, LastDirectInput: last, Now: now}
			got := conversationpresence.Classify(direct, bot, session, facts, conversationpresence.ContinuityPolicy{IdleAfter: time.Minute, IdleStartsEpisode: true})
			if got.Temperature != tc.want {
				t.Fatalf("temperature=%s want=%s facts=%+v", got.Temperature, tc.want, facts)
			}
		})
	}
}
