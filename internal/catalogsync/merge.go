// Package catalogsync holds the catalog facts used by provider reconciliation.
package catalogsync

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type Model struct {
	ID   string
	Name string
	// Config contains only facts actually returned by discovery. An omitted key
	// means unknown, not false or a request to remove the user's configuration.
	Config json.RawMessage
}

// Merge fills new facts and refreshes earlier discovered facts only while the
// persisted value still matches the last discovered value. User edits win.
// Invalid stored state aborts the sync rather than overwriting it.
func Merge(current, discovered json.RawMessage) (json.RawMessage, error) {
	var c, d map[string]json.RawMessage
	if err := json.Unmarshal(current, &c); err != nil || c == nil {
		return nil, fmt.Errorf("invalid stored model config")
	}
	if err := json.Unmarshal(discovered, &d); err != nil || d == nil {
		return nil, fmt.Errorf("invalid discovered model config")
	}
	previous := map[string]json.RawMessage{}
	_, managed := c["catalog_discovered"]
	overrides := map[string]bool{}
	if raw, ok := c["catalog_overrides"]; ok {
		if err := json.Unmarshal(raw, &overrides); err != nil || overrides == nil {
			return nil, fmt.Errorf("invalid catalog overrides")
		}
	}
	if raw, ok := c["catalog_discovered"]; ok {
		if err := json.Unmarshal(raw, &previous); err != nil || previous == nil {
			return nil, fmt.Errorf("invalid discovery provenance")
		}
	}
	for key, value := range d {
		if key == "catalog_discovered" || key == "catalog_available" || key == "catalog_overrides" {
			return nil, fmt.Errorf("reserved catalog field")
		}
		old, exists := c[key]
		prior, known := previous[key]
		if managed && !overrides[key] && ((!exists && !known) || (exists && known && jsonEqual(old, prior))) {
			c[key] = value
			previous[key] = value
		}
	}
	if !managed {
		c["catalog_available"] = json.RawMessage("true")
		return json.Marshal(c)
	}
	raw, err := json.Marshal(previous)
	if err != nil {
		return nil, err
	}
	c["catalog_discovered"] = raw
	c["catalog_available"] = json.RawMessage("true")
	return json.Marshal(c)
}

func jsonEqual(a, b []byte) bool {
	var x, y bytes.Buffer
	if json.Compact(&x, a) != nil || json.Compact(&y, b) != nil {
		return false
	}
	return bytes.Equal(x.Bytes(), y.Bytes())
}

// NewConfig establishes ownership only for a newly inserted row.
func NewConfig(discovered json.RawMessage) (json.RawMessage, error) {
	var facts map[string]json.RawMessage
	if err := json.Unmarshal(discovered, &facts); err != nil || facts == nil {
		return nil, fmt.Errorf("invalid discovered model config")
	}
	for key := range facts {
		if key == "catalog_discovered" || key == "catalog_available" || key == "catalog_overrides" {
			return nil, fmt.Errorf("reserved catalog field")
		}
	}
	raw, err := json.Marshal(facts)
	if err != nil {
		return nil, err
	}
	facts["catalog_discovered"] = raw
	facts["catalog_available"] = json.RawMessage("true")
	return json.Marshal(facts)
}

// RecordUserEdits protects fields explicitly changed through model editing. The
// catalog-owned metadata always comes from the current database row.
func RecordUserEdits(current, proposed json.RawMessage) (json.RawMessage, error) {
	var c, p map[string]json.RawMessage
	if json.Unmarshal(proposed, &p) != nil || p == nil {
		return nil, fmt.Errorf("invalid model config")
	}
	if json.Unmarshal(current, &c) != nil {
		return nil, fmt.Errorf("invalid model config")
	}
	if c == nil {
		return proposed, nil
	}
	// Ordinary and upstream-managed model updates retain their original semantics.
	// Provenance controls apply only to rows created by this synchronizer.
	if _, ok := c["catalog_discovered"]; !ok {
		return proposed, nil
	}
	var prev map[string]json.RawMessage
	if raw, ok := c["catalog_discovered"]; ok {
		if json.Unmarshal(raw, &prev) != nil || prev == nil {
			return nil, fmt.Errorf("invalid discovery provenance")
		}
		overrides := map[string]bool{}
		if raw, ok := c["catalog_overrides"]; ok {
			if json.Unmarshal(raw, &overrides) != nil || overrides == nil {
				return nil, fmt.Errorf("invalid catalog overrides")
			}
		}
		keys := make(map[string]bool, len(prev)+len(c)+len(p))
		for k := range prev {
			keys[k] = true
		}
		for k := range c {
			keys[k] = true
		}
		for k := range p {
			keys[k] = true
		}
		for k := range keys {
			if k == "catalog_discovered" || k == "catalog_overrides" || k == "catalog_available" {
				continue
			}
			before, b := c[k]
			after, a := p[k]
			if b != a || (b && !jsonEqual(before, after)) {
				overrides[k] = true
			}
		}
		raw, err := json.Marshal(overrides)
		if err != nil {
			return nil, err
		}
		p["catalog_overrides"] = raw
		p["catalog_discovered"] = c["catalog_discovered"]
	} else {
		delete(p, "catalog_discovered")
		delete(p, "catalog_overrides")
	}
	if raw, ok := c["catalog_available"]; ok {
		p["catalog_available"] = raw
	} else {
		delete(p, "catalog_available")
	}
	return json.Marshal(p)
}
