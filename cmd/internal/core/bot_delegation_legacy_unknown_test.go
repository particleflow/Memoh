package core

import (
	"context"
	"errors"
	"testing"

	"github.com/felinics/memoh/internal/agent/application"
	"github.com/felinics/memoh/internal/botdelegation"
)

type legacyUnknownLedger struct {
	botdelegation.Ledger
	record botdelegation.Record
}

func (l legacyUnknownLedger) Get(context.Context, string, string, string, string) (botdelegation.Record, error) {
	return l.record, nil
}

func TestLegacyUnknownCannotEnterDeliveryOrQueuedAdmission(t *testing.T) {
	_, authority, identity := newDelegationPlanFixture(t, "native")
	r := botdelegation.Record{ID: "legacy-unknown", Identity: identity, State: "unknown"}
	h := &delegationHost{service: &botdelegation.Service{Authority: authority, Ledger: legacyUnknownLedger{record: r}}}
	if _, err := h.service.ReadRecord(t.Context(), identity.SourceBotID, identity.UserID, identity.SourceSessionID, r.ID); err != nil {
		t.Fatalf("test must exercise an authorized legacy record: %v", err)
	}
	// Downstream route/media/agent dependencies are deliberately absent: these
	// entry points must reject before preparing or enqueuing legacy notification.
	if err := h.Deliver(t.Context(), r); !errors.Is(err, botdelegation.ErrDenied) {
		t.Fatalf("legacy delivery admitted: %v", err)
	}
	_, _, err := h.prepare(t.Context(), application.ChatRequest{BotID: identity.SourceBotID, UserID: identity.UserID, ThreadID: identity.SourceSessionID, DelegationResultID: r.ID})
	if !errors.Is(err, botdelegation.ErrDenied) {
		t.Fatalf("queued legacy unknown admitted: %v", err)
	}
}
