package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// OAuthAuthorization is a short-lived completion receipt, not a credential or
// an authorization grant. Losing it on restart must never infer success from
// an older token. The ordinary authenticated connection ACL still applies.
type OAuthAuthorization struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type oauthAuthorizationEntry struct {
	OAuthAuthorization
	expires time.Time
}

type oauthAuthorizations struct {
	mu      sync.Mutex
	entries map[string]oauthAuthorizationEntry // one current flow per connection
}

func authorizationID(state string) string {
	sum := sha256.Sum256([]byte(state))
	return hex.EncodeToString(sum[:])
}

func (a *oauthAuthorizations) begin(connection, state string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for key, entry := range a.entries {
		if !now.Before(entry.expires) {
			delete(a.entries, key)
		}
	}
	// Bound observation-only memory. Eviction reports unknown, never success.
	if len(a.entries) >= 1024 {
		oldest := connection
		expires := now.Add(11 * time.Minute)
		for key, entry := range a.entries {
			if entry.expires.Before(expires) {
				oldest, expires = key, entry.expires
			}
		}
		delete(a.entries, oldest)
	}
	id := authorizationID(state)
	a.entries[connection] = oauthAuthorizationEntry{OAuthAuthorization: OAuthAuthorization{ID: id, Status: "pending"}, expires: now.Add(10 * time.Minute)}
	return id
}

func (a *oauthAuthorizations) finish(connection, state string, succeeded bool) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	entry, ok := a.entries[connection]
	if !ok || entry.ID != authorizationID(state) || entry.Status != "pending" || !time.Now().Before(entry.expires) {
		return
	}
	entry.Status = "failed"
	if succeeded {
		entry.Status = "succeeded"
	}
	a.entries[connection] = entry
}

// AuthorizationStatus is called only after the handler verifies access to this
// exact Bot/connection. A supplied ID is a selector, never authentication.
func (s *OAuthService) AuthorizationStatus(connection, id string) *OAuthAuthorization {
	result := &OAuthAuthorization{ID: id, Status: "unknown"}
	if s.authorizations == nil {
		return result
	}
	s.authorizations.mu.Lock()
	defer s.authorizations.mu.Unlock()
	entry, ok := s.authorizations.entries[connection]
	if ok && entry.ID == id && time.Now().Before(entry.expires) {
		result.Status = entry.Status
	}
	return result
}
