package botdelegation

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/toolcontext"
)

func historicalNativeFixture(t *testing.T) (*Service, *nativeMountFixture, *hostFixture, Record) {
	t.Helper()
	a, f, b, _ := nativeFixture(t)
	b.SourceSessionID, b.SourceRootID, b.SourceSelection = "root", "root", "native"
	r := Record{
		Identity: b, ID: uuid.NewString(), RunID: uuid.NewString(), SourceSelection: "native", State: "completed", DeliveryState: "accepted", Revision: 7,
		Payload: Payload{TargetSelection: "native"}, Result: Result{Summary: &Summary{Text: "not part of a history reference"}}, CreatedAt: time.Now().Add(-time.Hour),
	}
	ledger := &memoryLedger{records: map[string]Record{r.ID: r}, run: &RunObservation{ID: r.RunID, State: "completed"}}
	s := NewService(a, ledger, &originalFixture{}, nil, time.Minute)
	s.SetCursorSecret("synthetic-history-cursor")
	h := &hostFixture{service: s}
	s.Host = h
	t.Cleanup(s.Close)
	return s, f, h, r
}

func TestHistoryListRetainsNativeReferenceAfterSelectionChange(t *testing.T) {
	s, f, h, r := historicalNativeFixture(t)
	p, err := s.List(t.Context(), r.Identity.SourceBotID, r.Identity.UserID, r.Identity.SourceSessionID, 100, "")
	if err != nil || len(p.Items) != 1 || p.Items[0].ID != r.ID || p.Items[0].TargetBotID != r.Identity.TargetBotID || p.Items[0].TargetSessionID != r.Identity.TargetSessionID || p.Items[0].TargetRunID != r.RunID || p.Items[0].TargetRunState != "completed" {
		t.Fatalf("historical reference=%+v err=%v", p, err)
	}
	if p.Items[0].Summary != nil || len(p.Items[0].Attachments) != 0 {
		t.Fatal("history fallback exposed execution-scoped content")
	}
	if _, err = s.Authority.Authorize(t.Context(), r.Identity); !errors.Is(err, ErrDenied) {
		t.Fatalf("execution authority changed: %v", err)
	}
	if _, err = s.ForSession(t.Context(), r.Identity.TargetBotID, r.Identity.UserID, r.Identity.TargetSessionID); !errors.Is(err, ErrDenied) {
		t.Fatalf("delegated continuation authority changed: %v", err)
	}
	if err = toolcontext.CheckDelegationTarget(s.BindScope(t.Context(), r), r.Identity.TargetBotID, "native", ""); !errors.Is(err, ErrDenied) {
		t.Fatalf("tool authority changed: %v", err)
	}
	if _, err = s.RetryDelivery(t.Context(), r.Identity.SourceBotID, r.Identity.UserID, r.Identity.SourceSessionID, r.ID); !errors.Is(err, ErrDenied) {
		t.Fatalf("delivery authority changed: %v", err)
	}
	if h.creates.Load()+h.runs.Load()+h.deliveries.Load() != 0 || f.mcpCalls != 0 {
		t.Fatal("history read invoked business execution")
	}
	if !reflect.DeepEqual(s.Ledger.(*memoryLedger).records[r.ID], r) {
		t.Fatal("history read changed the original record")
	}
}

func TestHistoryReferenceKeepsLiveOwnershipAndRevocationChecks(t *testing.T) {
	for _, name := range []string{"source grant", "target grant", "target owner", "target team", "source owner", "root", "login"} {
		t.Run(name, func(t *testing.T) {
			s, f, _, r := historicalNativeFixture(t)
			switch name {
			case "source grant":
				f.denied["manager/chat"] = true
			case "target grant":
				f.denied["specialist/chat"] = true
			case "target owner", "target team":
				v := f.sessions[r.Identity.TargetSessionID]
				if name == "target owner" {
					v.CreatedByUserID = "other"
				} else {
					v.TeamID = "other"
				}
				f.sessions[v.ID] = v
			case "source owner":
				v := f.sessions[r.Identity.SourceSessionID]
				v.CreatedByUserID = "other"
				f.sessions[v.ID] = v
			case "root":
				r.Identity.SourceRootID = "forged"
				s.Ledger.(*memoryLedger).records[r.ID] = r
			case "login":
				f.active = false
			}
			p, err := s.List(t.Context(), r.Identity.SourceBotID, r.Identity.UserID, r.Identity.SourceSessionID, 100, "")
			if len(p.Items) != 0 || (err != nil && !errors.Is(err, ErrDenied)) {
				t.Fatalf("unauthorized history=%+v err=%v", p, err)
			}
		})
	}
}

func TestHistoryFallbackDoesNotGrantOriginalsAndKeepsNormalProjection(t *testing.T) {
	s, f, _, r := historicalNativeFixture(t)
	hash := strings.Repeat("a", 64)
	r.Result.Outputs = []string{hash}
	s.Ledger.(*memoryLedger).records[r.ID] = r
	f.assets[r.Identity.TargetBotID+"/"+r.Identity.TargetSessionID+"/"+hash] = true
	o := s.Originals.(*originalFixture)
	o.asset = media.Asset{BotID: r.Identity.TargetBotID, ContentHash: hash}
	p, err := s.List(t.Context(), r.Identity.SourceBotID, r.Identity.UserID, r.Identity.SourceSessionID, 100, "")
	if err != nil || len(p.Items) != 1 || len(p.Items[0].Attachments) != 0 || o.stats != 0 {
		t.Fatalf("history attachment capability=%+v err=%v stats=%d", p, err, o.stats)
	}
	if _, err = s.Attachments(r).Open(t.Context(), hash, true); !errors.Is(err, ErrDenied) || o.opens != 0 {
		t.Fatalf("original authority changed: %v", err)
	}
	root := f.sessions[r.Identity.SourceSessionID]
	root.Metadata = map[string]any{"workspace_target_id": "native"}
	f.sessions[root.ID] = root
	p, err = s.List(t.Context(), r.Identity.SourceBotID, r.Identity.UserID, r.Identity.SourceSessionID, 100, "")
	if err != nil || len(p.Items) != 1 || p.Items[0].Summary == nil || len(p.Items[0].Attachments) != 1 || o.stats != 1 {
		t.Fatalf("normal result projection regressed: %+v err=%v stats=%d", p, err, o.stats)
	}
}

func TestHistoryReferencePreservesUnknownAndPropagatesReadFailure(t *testing.T) {
	s, _, _, r := historicalNativeFixture(t)
	r.State = "unknown"
	ledger := s.Ledger.(*memoryLedger)
	ledger.records[r.ID], ledger.run = r, nil
	p, err := s.List(t.Context(), r.Identity.SourceBotID, r.Identity.UserID, r.Identity.SourceSessionID, 100, "")
	if err != nil || len(p.Items) != 1 || p.Items[0].State != "unknown" || p.Items[0].TargetRunState != "" {
		t.Fatalf("unknown history=%+v err=%v", p, err)
	}
	if !reflect.DeepEqual(ledger.records[r.ID], r) {
		t.Fatal("unknown record changed")
	}
	readErr := errors.New("synthetic durable run read failed")
	ledger.runErr = readErr
	if _, err = s.List(t.Context(), r.Identity.SourceBotID, r.Identity.UserID, r.Identity.SourceSessionID, 100, ""); !errors.Is(err, readErr) {
		t.Fatalf("read error replaced with an empty success: %v", err)
	}
}
