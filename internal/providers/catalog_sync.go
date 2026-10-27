package providers

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/felinics/memoh/internal/catalogsync"
	"github.com/felinics/memoh/internal/db"
	"github.com/felinics/memoh/internal/models"
)

// CatalogApply persists a complete successful discovery atomically. It must
// preserve explicit user state and must not treat absent embedding models as
// removals: no-probe discovery may omit them.
type CatalogApply func(context.Context, string, []RemoteModel) error

const (
	catalogSyncInterval = 15 * time.Minute
	catalogRetryMin     = time.Minute
	catalogRetryMax     = 15 * time.Minute
)

type catalogSyncState struct {
	mu      sync.Mutex
	entries map[string]*catalogSyncEntry
	now     func() time.Time
}

type catalogSyncEntry struct {
	running  bool
	next     time.Time
	failures int
}

// SyncModelCatalog synchronizes an explicitly selected provider, subject to a
// per-provider cooldown. Discovery errors never reach apply. The target is
// supplied by the caller, not inferred from a model, hostname, or credential.
// It performs no inference requests, including embedding-dimensions probes.
func (s *Service) SyncModelCatalog(ctx context.Context, id string, apply CatalogApply) (bool, error) {
	if apply == nil {
		return false, errors.New("catalog apply is required")
	}
	providerID, err := db.ParseUUID(id)
	if err != nil {
		return false, err
	}
	id = providerID.String()
	if !s.catalogSync.begin(id, s.catalogSync.timeNow()) {
		return false, nil
	}
	success := false
	defer func() { s.catalogSync.finish(id, s.catalogSync.timeNow(), success) }()
	provider, err := s.queries.GetProviderByID(ctx, providerID)
	if err != nil {
		return true, err
	}
	clientType := models.ClientType(provider.ClientType)
	if !provider.Enable || (clientType != models.ClientTypeOpenAICompletions && clientType != models.ClientTypeOpenAIResponses) {
		return true, errors.New("catalog sync requires enabled OpenAI-compatible provider")
	}
	// Validate a complete Magpie response before any write. The ordinary SDK
	// can accept a response without a data array as an empty list; that must
	// not turn malformed upstream data into mass unavailability.
	catalog, err := s.DiscoverMediaCatalog(ctx, id)
	if err != nil {
		return true, err
	}
	validIDs := make(map[string]MediaCatalogModel, len(catalog.Models))
	for _, row := range catalog.Models {
		validIDs[row.ID] = row
	}
	remote, err := s.fetchRemoteModelsViaSDKWithOptions(ctx, provider, false, false)
	if err != nil {
		return true, err
	}
	if len(remote) == 0 && len(catalog.Models) != 0 {
		return true, errors.New("inconsistent empty provider catalog")
	}
	seen := make(map[string]bool, len(remote))
	for i := range remote {
		row := &remote[i]
		fact, ok := validIDs[row.ID]
		if !ok || seen[row.ID] || (fact.Kind != "" && fact.Kind != "chat") {
			return true, errors.New("inconsistent provider catalog")
		}
		seen[row.ID] = true
		// The upstream response, not template enrichment, owns these fields.
		row.ContextWindow = fact.ContextWindow
		row.MaxOutputTokens = fact.MaxOutputTokens
	}
	if err := apply(ctx, id, remote); err != nil {
		return true, err
	}
	success = true
	return true, nil
}

// timeNow uses the real clock in production; tests may inject a controlled clock.
func (s *catalogSyncState) timeNow() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *catalogSyncState) begin(id string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries == nil {
		s.entries = make(map[string]*catalogSyncEntry)
	}
	e := s.entries[id]
	if e == nil {
		e = &catalogSyncEntry{}
		s.entries[id] = e
	}
	if e.running || now.Before(e.next) {
		return false
	}
	e.running = true
	return true
}

func (s *catalogSyncState) finish(id string, now time.Time, success bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.entries[id]
	e.running = false
	if success {
		e.failures = 0
		e.next = now.Add(catalogSyncInterval)
		return
	}
	e.failures++
	delay := catalogRetryMin
	for n := 1; n < e.failures && delay < catalogRetryMax; n++ {
		delay *= 2
	}
	if delay > catalogRetryMax {
		delay = catalogRetryMax
	}
	e.next = now.Add(delay)
}

// SyncStoredModelCatalog connects discovery to the existing transactional store.
// Explicit provider selection and lifecycle scheduling remain caller-owned.
func (s *Service) SyncStoredModelCatalog(ctx context.Context, id string) (bool, error) {
	store, ok := s.queries.(interface {
		ApplyModelCatalog(context.Context, string, []byte, string, []catalogsync.Model) error
	})
	if !ok {
		return false, errors.New("atomic catalog store unavailable")
	}
	parsed, err := db.ParseUUID(id)
	if err != nil {
		return false, err
	}
	provider, err := s.queries.GetProviderByID(ctx, parsed)
	if err != nil {
		return false, err
	}
	return s.SyncModelCatalog(ctx, id, func(ctx context.Context, id string, remote []RemoteModel) error {
		records := make([]catalogsync.Model, 0, len(remote))
		for _, m := range remote {
			if m.Type != string(models.ModelTypeChat) {
				continue
			}
			// The generic SDK's template enrichment is not a live Magpie capability
			// assertion. Until Magpie returns verified capability facts, leave them
			// unknown and avoid propagating template- or protocol-derived guesses.
			config, err := json.Marshal(models.ModelConfig{ContextWindow: m.ContextWindow, MaxOutputTokens: m.MaxOutputTokens})
			if err != nil {
				return err
			}
			records = append(records, catalogsync.Model{ID: m.ID, Name: m.Name, Config: config})
		}
		return store.ApplyModelCatalog(ctx, id, provider.Config, provider.ClientType, records)
	})
}

// CatalogTargetResolver returns the one explicitly selected existing provider ID.
// It must return an empty ID until that identity is established. It must never
// derive a provider from a hostname or from its model names.
type CatalogTargetResolver func(context.Context) (string, error)

// RunModelCatalogSync runs inside the existing server lifecycle. The caller
// supplies the established provider identity and cancels ctx on shutdown. The
// target resolver is the only integration seam; no new provider config exists.
func (s *Service) RunModelCatalogSync(ctx context.Context, resolve CatalogTargetResolver) {
	if resolve == nil {
		return
	}
	tick := time.NewTicker(catalogRetryMin)
	defer tick.Stop()
	s.runModelCatalogSync(ctx, resolve, tick.C, nil)
}

// tick and after allow deterministic lifecycle tests without changing the
// production clock or cadence. The completion callback is nil in production.
func (s *Service) runModelCatalogSync(ctx context.Context, resolve CatalogTargetResolver, tick <-chan time.Time, after func()) {
	for {
		if ctx.Err() != nil {
			return
		}
		id, err := resolve(ctx)
		if err == nil && id != "" {
			if _, syncErr := s.SyncStoredModelCatalog(ctx, id); syncErr != nil && ctx.Err() == nil {
				s.logger.WarnContext(ctx, "automatic model catalog sync failed")
			}
		}
		if after != nil {
			after()
		}
		select {
		case <-ctx.Done():
			return
		case <-tick:
		}
	}
}
