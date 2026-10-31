//go:build linux

package transferclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Configuration and the same constructor used by InvokeConfiguredHerdr are
// real; the HTTP/file adapter remains the explicitly isolated protocol fixture.
func configuredNetworkClient(t *testing.T, d *dualStackFixture, getenv func(string) string, job Job) *Client {
	t.Helper()
	h, err := OpenHostConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: d.roots, MinVersion: tls.VersionTLS12}
	c, err := h.newDeviceTask(job, "upload", &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.CloseIdleConnections)
	return c
}

func networkJob(src []byte) Job {
	j := newJob(src)
	j.Task = DeviceTask{UserID: "user", SessionID: "session", TargetID: "target", RunID: "run", Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32)}
	return j
}

func TestConfiguredLANPreferenceAndIPv6Resume(t *testing.T) {
	d := newDualStackFixture(t)
	d.auth = "Memoh-Transfer fixture-stable-device"
	getenv, _, write := producerFixture(t, d.base)
	policy := func(addresses ...string) {
		raw, _ := json.Marshal(hostNetworkPolicy{Version: 1, LANAddresses: addresses})
		write(getenv("HERDR_PLUGIN_CONFIG_DIR"), "transfer-network.json", raw)
	}
	src := frozen("explicit-routing")
	s := storeJob(t, networkJob(src))
	policy("127.0.0.1", "::1")
	c := configuredNetworkClient(t, d, getenv, s.load())
	d.selection = c.selection
	if _, err := c.Limits(d.trace(t.Context()), "bot-1"); err != nil || d.last.Load() != "4" || d.calls["6"].Load() != 0 {
		t.Fatal("explicit LAN order was not preferred", err)
	}
	d.logDials("configured_ipv4_first")
	c.CloseIdleConnections()
	policy("::1", "127.0.0.1")
	c = configuredNetworkClient(t, d, getenv, s.load())
	if _, err := c.Limits(d.trace(t.Context()), "bot-1"); err != nil || d.last.Load() != "6" {
		t.Fatal("explicit IPv6 LAN order was not preferred", err)
	}
	d.logDials("configured_ipv6_first")
	c.CloseIdleConnections()
	policy("127.0.0.1")
	c = configuredNetworkClient(t, d, getenv, s.load())
	advance(t, c, s, src)
	initial := advance(t, c, s, src)
	if initial.Offset != 4 || initial.ReceiptID == "" || d.last.Load() != "4" {
		t.Fatal("first LAN checkpoint")
	}
	d.stop("4")
	got, err := c.Get(d.trace(t.Context()), initial.BotID, initial.ReceiptID)
	if err != nil || got.Offset != 4 || d.last.Load() != "6" {
		t.Fatal("LAN refusal did not fall back to original authority IPv6", err)
	}
	d.logDials("lan_refused_ipv6_fallback")
	c.CloseIdleConnections()
	c = configuredNetworkClient(t, d, getenv, s.load())
	next := advance(t, c, s, src)
	if next.Offset != 8 || next.ReceiptID != initial.ReceiptID || next.Task != initial.Task {
		t.Fatal("fallback restart lost original task/receipt")
	}
	d.stop("6")
	c.CloseIdleConnections()
	if err := c.Advance(d.trace(t.Context()), next, s.save, src, time.Now()); err != nil {
		t.Fatal(err)
	}
	pending := s.load()
	if pending.Offset != 8 || pending.ReceiptID != initial.ReceiptID || pending.Attempts != 1 || pending.NextAttempt.IsZero() || pending.ErrorCode != "connection" {
		t.Fatal("total outage lost checkpoint or finite retry")
	}
	d.logDials("all_routes_refused")
	d.start("4", d.cert)
	d.start("6", d.cert)
	c = configuredNetworkClient(t, d, getenv, s.load())
	if _, err := c.Get(d.trace(t.Context()), pending.BotID, pending.ReceiptID); err != nil || d.last.Load() != "4" {
		t.Fatal("new invocation did not return to configured LAN", err)
	}
	d.logDials("lan_restored_preferred_again")
	for range 5 {
		if s.load().Ready == nil {
			advance(t, c, s, src)
		}
	}
	ready := s.load()
	prefix := "/api/internal/computer-transfers/operation-1/delivery-1/bots/bot-1/media"
	if ready.Ready == nil || ready.Ready.ContentHash != digest(src) || ready.ReceiptID != initial.ReceiptID || ready.Task != initial.Task || !bytes.Equal(fixtureReady(d.file), src) || d.file.count("POST "+prefix+"/uploads") != 1 || d.file.count("PATCH "+prefix+"/uploads/receipt-1") != 4 || d.file.count("POST "+prefix+"/uploads/receipt-1/complete") != 1 {
		t.Fatal("policy/restart duplicated confirmed bytes or changed final reference")
	}
	t.Log("ready bytes=16 create=1 patch=4 complete=1 same_task_attempt_receipt=true hash_match=true")
}

func TestConfiguredLANPostConnectFailureDoesNotFallback(t *testing.T) {
	for _, failure := range []string{"trusted_wrong_hostname", "authorization_403"} {
		t.Run(failure, func(t *testing.T) {
			d := newDualStackFixture(t)
			d.auth = "Memoh-Transfer fixture-stable-device"
			getenv, _, write := producerFixture(t, d.base)
			write(getenv("HERDR_PLUGIN_CONFIG_DIR"), "transfer-network.json", []byte(`{"version":1,"lan_addresses":["127.0.0.1","::1"]}`))
			if failure == "trusted_wrong_hostname" {
				wrong, leaf := networkCertificate(t, "wrong-host.invalid")
				d.roots.AddCert(leaf)
				if _, err := leaf.Verify(x509.VerifyOptions{Roots: d.roots, DNSName: "wrong-host.invalid"}); err != nil {
					t.Fatal("wrong-host certificate was not independently trusted", err)
				}
				d.stop("4")
				d.start("4", wrong)
			} else {
				d.status["4"].Store(http.StatusForbidden)
			}
			c := configuredNetworkClient(t, d, getenv, networkJob([]byte("fixture")))
			d.selection = c.selection
			_, err := c.Limits(d.trace(t.Context()), "bot-1")
			if failure == "trusted_wrong_hostname" {
				if !errors.Is(err, ErrTransport) {
					t.Fatal("hostname failure bypassed", err)
				}
			} else {
				var status *HTTPError
				if !errors.As(err, &status) || status.Status != http.StatusForbidden {
					t.Fatal("authorization failure bypassed", err)
				}
			}
			if d.calls["6"].Load() != 0 {
				t.Fatal("post-connect error fell back to another configured/resolved address")
			}
			d.logDials(failure)
		})
	}
}

func TestConfiguredLANContextCancellation(t *testing.T) {
	d := newDualStackFixture(t)
	c := d.client()
	if err := c.useNetworkPolicy(&hostNetworkPolicy{Version: 1, LANAddresses: []string{"127.0.0.1"}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.Limits(d.trace(ctx), "bot-1"); !errors.Is(err, context.Canceled) || d.calls["4"].Load()+d.calls["6"].Load() != 0 {
		t.Fatal("cancellation sent a new request", err)
	}
}
