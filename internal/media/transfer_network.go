package media

import (
	"encoding/json"
	"errors"
	"net/netip"
)

// TransferNetworkPolicy contains TCP address hints only. The authenticated API
// authority, port, TLS identity and storage locations remain with their owners.
type TransferNetworkPolicy struct {
	Version      int      `json:"version" toml:"version"`
	LANAddresses []string `json:"lan_addresses" toml:"lan_addresses"`
}

func (p *TransferNetworkPolicy) Normalized() (*TransferNetworkPolicy, error) {
	if p == nil {
		return nil, nil
	}
	raw, err := json.Marshal(p)
	if err != nil || len(raw) > 8192 || p.Version != 1 || len(p.LANAddresses) == 0 {
		return nil, errors.New("invalid file network policy")
	}
	next := &TransferNetworkPolicy{Version: 1, LANAddresses: make([]string, len(p.LANAddresses))}
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
	return &TransferNetworkPolicy{Version: p.Version, LANAddresses: append([]string(nil), p.LANAddresses...)}
}
