package media

import (
	"encoding/json"
	"errors"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// TransferNetworkPolicy changes physical routing only. Version 1 retains TCP
// hints; version 2 explicitly maps one logical server to a physical endpoint.
type TransferNetworkPolicy struct {
	Version      int               `json:"version" toml:"version"`
	LANAddresses []string          `json:"lan_addresses" toml:"lan_addresses"`
	Endpoint     *TransferEndpoint `json:"endpoint,omitempty" toml:"endpoint"`
}

// TransferEndpoint is an operator-configured assertion that both origins serve
// the same NAS. Paths, credentials, receipts and persisted identities stay with
// the logical origin. It is never inferred from a redirect or a failed request.
type TransferEndpoint struct {
	LogicalOrigin  string `json:"logical_origin" toml:"logical_origin"`
	PhysicalOrigin string `json:"physical_origin" toml:"physical_origin"`
}

func transferOrigin(value string) bool {
	if len(value) > 2048 || strings.ContainsAny(value, "?#%\\") {
		return false
	}
	for _, c := range value {
		if c <= ' ' || c > '~' {
			return false
		}
	}
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || strings.HasSuffix(u.Host, ":") {
		return false
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return false
	}
	// Match the canonical origins returned by browser URL.origin.
	if u.Scheme == "https" && u.Port() == "443" || u.Scheme == "http" && u.Port() == "80" {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	return u.Scheme+"://"+strings.ToLower(u.Host) == value
}

func (p *TransferNetworkPolicy) Normalized() (*TransferNetworkPolicy, error) {
	if p == nil {
		return nil, nil
	}
	raw, err := json.Marshal(p)
	if err != nil || len(raw) > 8192 || (p.Version != 1 && p.Version != 2) ||
		(p.Version == 1 && (len(p.LANAddresses) == 0 || p.Endpoint != nil)) ||
		(p.Version == 2 && (p.Endpoint == nil || !transferOrigin(p.Endpoint.LogicalOrigin) || !transferOrigin(p.Endpoint.PhysicalOrigin))) {
		return nil, errors.New("invalid file network policy")
	}
	next := &TransferNetworkPolicy{Version: p.Version, LANAddresses: make([]string, len(p.LANAddresses))}
	if p.Endpoint != nil {
		endpoint := *p.Endpoint
		next.Endpoint = &endpoint
	}
	seen := make(map[netip.Addr]bool, len(p.LANAddresses))
	for i, value := range p.LANAddresses {
		ip, err := netip.ParseAddr(value)
		if err != nil {
			return nil, errors.New("invalid file network address")
		}
		ip = ip.Unmap()
		if ip.IsUnspecified() || ip.IsMulticast() || seen[ip] {
			return nil, errors.New("invalid file network address")
		}
		seen[ip] = true
		next.LANAddresses[i] = ip.String()
	}
	return next, nil
}

// SetTransferNetworkPolicy is startup-only, like SetMaxUploadBytes. Snapshot
// copies keep request consumers from mutating the process-wide configuration.
func (s *Service) SetTransferNetworkPolicy(p *TransferNetworkPolicy) error {
	next, err := p.Normalized()
	if err != nil {
		return err
	}
	s.transferNetworkPolicy = next
	return nil
}

func (s *Service) TransferNetworkPolicy() *TransferNetworkPolicy {
	if s == nil || s.transferNetworkPolicy == nil {
		return nil
	}
	p := s.transferNetworkPolicy
	next := &TransferNetworkPolicy{Version: p.Version, LANAddresses: append([]string(nil), p.LANAddresses...)}
	if p.Endpoint != nil {
		endpoint := *p.Endpoint
		next.Endpoint = &endpoint
	}
	return next
}
