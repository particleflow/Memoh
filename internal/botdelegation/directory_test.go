package botdelegation

import (
	"errors"
	"reflect"
	"testing"
)

func TestDirectoryMetadataUsesOriginalAuthorityAndNeverDispatches(t *testing.T) {
	s, f, h, r := historicalNativeFixture(t)
	r.State = "accepted"
	ledger := s.Ledger.(*memoryLedger)
	ledger.records[r.ID], ledger.run = r, nil
	read := func(bot, user, session string) (View, error) {
		return s.DirectoryMetadata(t.Context(), bot, user, session, r.ID)
	}
	v, err := read(r.Identity.SourceBotID, r.Identity.UserID, r.Identity.SourceSessionID)
	if err != nil || v.ID != r.ID || v.State != "unknown" || v.Summary != nil || len(v.Attachments) != 0 {
		t.Fatalf("%+v %v", v, err)
	}
	for _, args := range [][3]string{{r.Identity.TargetBotID, r.Identity.UserID, r.Identity.SourceSessionID}, {r.Identity.SourceBotID, "other", r.Identity.SourceSessionID}, {r.Identity.SourceBotID, r.Identity.UserID, "other"}} {
		if _, err = read(args[0], args[1], args[2]); !errors.Is(err, ErrDenied) {
			t.Fatalf("forged authority: %v", err)
		}
	}
	for _, bot := range []string{"manager/chat", "specialist/chat"} {
		f.denied[bot] = true
		if _, err = read(r.Identity.SourceBotID, r.Identity.UserID, r.Identity.SourceSessionID); !errors.Is(err, ErrDenied) {
			t.Fatalf("revoked %s: %v", bot, err)
		}
		delete(f.denied, bot)
	}
	if !reflect.DeepEqual(r, ledger.records[r.ID]) || h.creates.Load()+h.runs.Load()+h.deliveries.Load() != 0 || f.mcpCalls != 0 {
		t.Fatal("directory read mutated or dispatched")
	}
}
