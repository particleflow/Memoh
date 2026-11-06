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
	"strings"
	"time"

	"github.com/felinics/memoh/internal/media"
)

// This optional transport policy lives in the existing private CONFIG_DIR.
// transfer.json remains the sole identity/source configuration: changing route
// hints must not change its byte binding or require a queue migration.
type hostNetworkPolicy struct {
	Version      int                     `json:"version"`
	LANAddresses []string                `json:"lan_addresses"`
	Endpoint     *media.TransferEndpoint `json:"endpoint,omitempty"`
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
	value, err := (&media.TransferNetworkPolicy{Version: p.Version, LANAddresses: p.LANAddresses, Endpoint: p.Endpoint}).Normalized()
	if err != nil {
		return nil, ErrInvalid
	}
	return &hostNetworkPolicy{Version: value.Version, LANAddresses: value.LANAddresses, Endpoint: value.Endpoint}, nil
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
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF {
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

// Routing only: v1 selects TCP addresses; v2 explicitly maps a physical origin.
// net/http still verifies TLS for an HTTPS destination. Certificate, auth and
// HTTP failures never select another endpoint or replay a business request.
func (c *Client) useNetworkPolicy(p *hostNetworkPolicy) error {
	if p == nil {
		return nil
	}
	var err error
	p, err = p.normalized()
	if err != nil {
		return err
	}
	// Rebuild from the validated original transport; never stack a NAS policy
	// on the old local dialer or keep its bootstrap connection pooled.
	t := c.transport.Clone()
	u, err := url.Parse(c.base)
	if err != nil {
		return ErrInvalid
	}
	logical := *u
	if p.Endpoint != nil {
		host := strings.ToLower(u.Host)
		if u.Scheme == "https" && u.Port() == "443" || u.Scheme == "http" && u.Port() == "80" {
			host = strings.ToLower(u.Hostname())
			if strings.Contains(host, ":") {
				host = "[" + host + "]"
			}
		}
		if p.Endpoint.LogicalOrigin != u.Scheme+"://"+host {
			return ErrBinding
		}
		physical, err := url.Parse(p.Endpoint.PhysicalOrigin)
		if err != nil {
			return ErrInvalid
		}
		u.Scheme, u.Host = physical.Scheme, physical.Host
		t.TLSClientConfig.ServerName = physical.Hostname()
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
	if len(p.LANAddresses) > 0 {
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
	}
	c.http.CloseIdleConnections()
	if p.Endpoint != nil {
		c.http.Transport = &endpointTransport{logical: logical, physical: *u, next: t}
	} else {
		c.http.Transport = t
	}
	return nil
}

// endpointTransport leaves the original request/identity intact and changes
// only the dispatch destination. net/http's existing redirect refusal applies.
type endpointTransport struct {
	logical, physical url.URL
	next              *http.Transport
}

func (t *endpointTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != t.logical.Scheme || request.URL.Host != t.logical.Host {
		return nil, ErrBinding
	}
	next := request.Clone(request.Context())
	next.URL.Scheme, next.URL.Host = t.physical.Scheme, t.physical.Host
	next.Host = ""
	return t.next.RoundTrip(next)
}

func (t *endpointTransport) CloseIdleConnections() { t.next.CloseIdleConnections() }
