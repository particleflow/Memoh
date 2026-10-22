package botdelegation

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type memoryLedger struct {
	mu      sync.Mutex
	records map[string]Record
}

func (m *memoryLedger) Claim(_ context.Context, r Record) (Record, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, old := range m.records {
		if old.Identity.SourceSessionID != r.Identity.SourceSessionID || old.Identity.UserID != r.Identity.UserID || old.Identity.SourceBotID != r.Identity.SourceBotID {
			continue
		}
		if old.RequestID == r.RequestID {
			if old.Fingerprint != r.Fingerprint {
				return Record{}, false, ErrConflict
			}
			return old, false, nil
		}
		if old.Identity.TargetBotID == r.Identity.TargetBotID && old.Fingerprint == r.Fingerprint && (old.State == "accepted" || old.State == "running" || old.State == "unknown") {
			return old, false, nil
		}
	}
	r.CreatedAt = time.Now().UTC()
	r.UpdatedAt = r.CreatedAt
	m.records[r.ID] = r
	return r, true, nil
}

func (m *memoryLedger) Get(_ context.Context, bot, user, session, id string) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.records[id]
	if !ok || r.Identity.SourceBotID != bot || r.Identity.UserID != user || r.Identity.SourceSessionID != session {
		return Record{}, pgx.ErrNoRows
	}
	return r, nil
}

func (m *memoryLedger) ForTarget(_ context.Context, bot, session string) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.records {
		if r.Identity.TargetBotID == bot && r.Identity.TargetSessionID == session {
			return r, nil
		}
	}
	return Record{}, pgx.ErrNoRows
}

func (m *memoryLedger) Save(_ context.Context, r Record) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.records[r.ID]
	if !ok || old.Revision != r.Revision {
		return Record{}, ErrConflict
	}
	r.Revision++
	r.UpdatedAt = time.Now().UTC()
	m.records[r.ID] = r
	return r, nil
}
func (*memoryLedger) List(context.Context, Filter) ([]Record, error) { return nil, nil }

type hostFixture struct {
	service                   *Service
	state                     string
	creates, runs, deliveries atomic.Int32
	block                     chan struct{}
	output                    string
	read                      string
	deliverErr                error
}

func (*hostFixture) Plan(context.Context, Identity, string) ([]DevicePair, string, error) {
	return nil, "native", nil
}

func (h *hostFixture) CreateSession(context.Context, Record) (string, error) {
	h.creates.Add(1)
	return "specialist-session", nil
}

func (h *hostFixture) Run(_ context.Context, _ Record, started func(string) error) (Result, string, error) {
	h.runs.Add(1)
	if err := started(uuid.NewString()); err != nil {
		return Result{}, "unknown", err
	}
	if h.block != nil {
		<-h.block
	}
	result := Result{Summary: &Summary{Text: "specialist final", Source: "assistant_final"}}
	if h.output != "" {
		result.Outputs = []string{h.output}
	}
	return result, h.state, nil
}

func (h *hostFixture) Deliver(ctx context.Context, r Record) error {
	h.deliveries.Add(1)
	if h.deliverErr != nil {
		return h.deliverErr
	}
	if h.output != "" {
		reader, err := h.service.Attachments(r).Open(ctx, h.output, true)
		if err != nil {
			return err
		}
		defer func() { _ = reader.Close() }()
		data, err := io.ReadAll(reader)
		h.read = string(data)
		return err
	}
	return nil
}

func serviceFixture(t *testing.T) (*Service, *authorityFixture, *hostFixture, Input) {
	t.Helper()
	a, f, _, _ := fixture()
	target := "20000000-0000-4000-8000-000000000003"
	f.teams[target] = "team"
	delete(f.teams, "specialist")
	v := f.sessions["specialist-session"]
	v.BotID = target
	f.sessions[v.ID] = v
	o := &originalFixture{}
	s := NewService(a, &memoryLedger{records: map[string]Record{}}, o, slog.New(slog.DiscardHandler))
	h := &hostFixture{service: s, state: "completed"}
	s.Host = h
	t.Cleanup(s.Close)
	return s, f, h, Input{RequestID: uuid.NewString(), TargetBotID: target, Message: "synthetic task"}
}

func TestSubmitDeduplicatesConcurrentRequestsAndGETNeverDispatches(t *testing.T) {
	s, _, h, in := serviceFixture(t)
	h.block = make(chan struct{})
	var wg sync.WaitGroup
	ids := make(chan string, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := s.Submit(t.Context(), "manager", "user", "child", in)
			if err != nil {
				t.Error(err)
				return
			}
			ids <- v.ID
		}()
	}
	wg.Wait()
	close(ids)
	close(h.block)
	s.wg.Wait()
	var id string
	for candidate := range ids {
		if id != "" && candidate != id {
			t.Fatal("duplicate ledger rows")
		}
		id = candidate
	}
	for range 5 {
		v, err := s.Get(t.Context(), "manager", "user", "child", id)
		if err != nil || v.State != "completed" || v.DeliveryState != "accepted" {
			t.Fatalf("view=%+v error=%v", v, err)
		}
	}
	if h.runs.Load() != 1 || h.creates.Load() != 1 || h.deliveries.Load() != 1 {
		t.Fatalf("counts create/run/deliver=%d/%d/%d", h.creates.Load(), h.runs.Load(), h.deliveries.Load())
	}
	in.Message = "changed"
	if _, err := s.Submit(t.Context(), "manager", "user", "child", in); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed replay=%v", err)
	}
}

func TestUnknownAndDeliveryFailureDoNotRerunBusiness(t *testing.T) {
	for _, state := range []string{"unknown", "completed"} {
		t.Run(state, func(t *testing.T) {
			s, _, h, in := serviceFixture(t)
			h.state = state
			h.deliverErr = errors.New("SECRET delivery diagnostic")
			v, err := s.Submit(t.Context(), "manager", "user", "child", in)
			if err != nil {
				t.Fatal(err)
			}
			s.wg.Wait()
			if state == "unknown" {
				in.RequestID = uuid.NewString()
			}
			replayed, err := s.Submit(t.Context(), "manager", "user", "child", in)
			if err != nil || replayed.ID != v.ID {
				t.Fatalf("replay=%+v %v", replayed, err)
			}
			got, err := s.Get(t.Context(), "manager", "user", "child", v.ID)
			if err != nil {
				t.Fatal(err)
			}
			if h.runs.Load() != 1 || (state == "unknown" && (got.Summary != nil || h.deliveries.Load() != 0)) || (state == "completed" && got.DeliveryState != "unknown") {
				t.Fatalf("dishonest state or rerun: %+v runs=%d", got, h.runs.Load())
			}
		})
	}
}

func TestTextAndSingleOriginalReturnThenRevocation(t *testing.T) {
	s, f, h, in := serviceFixture(t)
	hash := strings.Repeat("a", 64)
	h.output = hash
	in.Attachments = []AttachmentInput{{ContentHash: hash}}
	f.assets["manager/root/"+hash] = true
	f.assets[in.TargetBotID+"/specialist-session/"+hash] = true
	o := s.Originals.(*originalFixture)
	o.asset.ContentHash = hash
	o.asset.BotID = "manager"
	o.asset.Mime = "text/plain"
	o.content = "same original bytes"
	v, err := s.Submit(t.Context(), "manager", "user", "child", in)
	if err != nil {
		t.Fatal(err)
	}
	s.wg.Wait()
	got, err := s.Get(t.Context(), "manager", "user", "child", v.ID)
	if err != nil || got.DeliveryState != "accepted" || len(got.Attachments) != 1 || h.read != "same original bytes" || o.owner != "manager" {
		t.Fatalf("return=%+v read=%q err=%v", got, h.read, err)
	}
	f.denied[in.TargetBotID+"/chat"] = true
	if _, err := s.Get(t.Context(), "manager", "user", "child", v.ID); !errors.Is(err, ErrDenied) {
		t.Fatalf("revoked result=%v", err)
	}
	if _, err := s.Submit(t.Context(), "manager", "user", "child", in); !errors.Is(err, ErrDenied) {
		t.Fatalf("revoked submit=%v", err)
	}
	if h.runs.Load() != 1 {
		t.Fatal("revocation restarted execution")
	}
}
