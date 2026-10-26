package transferclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/felinics/memoh/internal/media"
)

// This optional transport policy lives in the existing private CONFIG_DIR.
// transfer.json remains the sole identity/source configuration: changing route
// hints must not change its byte binding or require a queue migration.
type hostNetworkPolicy struct {
	Version      int      `json:"version"`
	LANAddresses []string `json:"lan_addresses"`
}

func (p *hostNetworkPolicy) UnmarshalJSON(raw []byte) error {
	type wire hostNetworkPolicy
	var value wire
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&value) != nil || d.Decode(new(any)) != io.EOF {
		return ErrInvalid
	}
	*p = hostNetworkPolicy(value)
	return nil
}

func (p *hostNetworkPolicy) normalized() (*hostNetworkPolicy, error) {
	value, err := (&media.TransferNetworkPolicy{Version: p.Version, LANAddresses: p.LANAddresses}).Normalized()
	if err != nil {
		return nil, ErrInvalid
	}
	return &hostNetworkPolicy{Version: value.Version, LANAddresses: value.LANAddresses}, nil
}

func (h *HostConfig) readNetworkPolicy() (*hostNetworkPolicy, error) {
	if _, err := h.root.Lstat("transfer-network.json"); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, ErrBinding
	}
	raw, err := h.readPrivate("transfer-network.json", 8192)
	if err != nil {
		return nil, err
	}
	var p hostNetworkPolicy
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF || p.Version != 1 || len(p.LANAddresses) == 0 {
		return nil, ErrInvalid
	}
	return p.normalized()
}

func (h *HostConfig) newDeviceTask(job Job, direction string, transport *http.Client) (*Client, error) {
	c, err := NewDeviceTask(h.Origin, job, direction, job.Task, transport, h.Authorization)
	if err != nil {
		return nil, err
	}
	if err := c.useNetworkPolicy(h.network); err != nil {
		c.CloseIdleConnections()
		return nil, err
	}
	c.policyBot = job.BotID
	c.bootstrapPolicy = direction == "download"
	return c, nil
}

func (c *Client) adoptNetworkPolicy(policy *hostNetworkPolicy) error {
	if policy != nil {
		p, err := policy.normalized()
		if err != nil {
			return ErrProtocol
		}
		if err = c.useNetworkPolicy(p); err != nil {
			return err
		}
	}
	c.policyLoaded = true
	return nil
}

// TCP selection only: net/http still owns TLS (including the original ServerName)
// and HTTP. Once TCP succeeds, a certificate/authorization/HTTP failure never
// causes this selector to try a different address. No credential is used here.
func (c *Client) useNetworkPolicy(p *hostNetworkPolicy) error {
	if p == nil {
		return nil
	}
	// Rebuild from the validated original transport; never stack a NAS policy
	// on the old local dialer or keep its bootstrap connection pooled.
	t := c.transport.Clone()
	u, err := url.Parse(c.base)
	if err != nil {
		return ErrInvalid
	}
	proxy := t.Proxy
	// A proxy owns routing itself. Do not silently bypass one to implement a
	// direct-LAN preference, nor claim it can enforce the same policy remotely.
	if proxy != nil {
		if target, err := proxy(&http.Request{URL: u}); err != nil || target != nil {
			return ErrBinding
		}
	}
	t.Proxy = func(r *http.Request) (*url.URL, error) {
		if r.URL.Scheme != u.Scheme || r.URL.Host != u.Host {
			return nil, ErrBinding
		}
		if proxy != nil {
			if target, err := proxy(r); err != nil || target != nil {
				return nil, ErrBinding
			}
		}
		return nil, nil
	}
	dial := t.DialContext
	if dial == nil {
		dial = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	}
	addresses := append([]string(nil), p.LANAddresses...)
	t.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" {
			return nil, ErrTransport
		}
		_, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, ErrTransport
		}
		// Reuse DefaultTransport's 30-second TCP bound for the entire LAN
		// phase, not per address; share remaining time across configured hints.
		lan, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		deadline, _ := lan.Deadline()
		for i, ip := range addresses {
			if lan.Err() != nil {
				break
			}
			attempt, done := context.WithTimeout(lan, time.Until(deadline)/time.Duration(len(addresses)-i))
			conn, err := dial(attempt, "tcp", net.JoinHostPort(ip, port))
			done()
			if err == nil {
				return conn, nil
			}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Resolve only the original authority's IPv6 addresses. It may be an
		// on-link address too; IP family is never used to classify LAN vs WAN.
		return dial(ctx, "tcp6", address)
	}
	c.http.CloseIdleConnections()
	c.http.Transport = t
	return nil
}
