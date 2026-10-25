//go:build linux

package transferclient

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Network tests reuse the explicit HTTP protocol fixture. Only TCP, TLS, the
// production client and the existing checkpoint fixture are exercised here;
// these tests are not a second Server/PG/NAS acceptance or a LAN/WAN topology.
type dualStackFixture struct {
	t         *testing.T
	file      *fixture
	port      string
	base      string
	auth      string
	selection string
	cert      tls.Certificate
	roots     *x509.CertPool
	servers   map[string]*httptest.Server
	calls     map[string]*atomic.Int64
	status    map[string]*atomic.Int32
	last      atomic.Value
	mu        sync.Mutex
	dials     []string
}

func networkCertificate(t *testing.T, hostname string) (tls.Certificate, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "isolated-network-fixture"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{hostname}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, leaf
}

func newDualStackFixture(t *testing.T) *dualStackFixture {
	t.Helper()
	ips, err := net.DefaultResolver.LookupIPAddr(t.Context(), "localhost")
	if err != nil {
		t.Fatal(err)
	}
	v4, v6 := false, false
	for _, ip := range ips {
		if !ip.IP.IsLoopback() {
			t.Skip("localhost includes a non-loopback mapping; do not contact it")
		}
		if ip.IP.IsLoopback() {
			if ip.IP.To4() != nil {
				v4 = true
			} else {
				v6 = true
			}
		}
	}
	if !v4 || !v6 {
		t.Skip("isolated dual-stack test requires existing localhost A and AAAA entries")
	}
	l, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback listener unavailable: %v", err)
	}
	_, port, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		_ = l.Close()
		t.Fatal(err)
	}
	cert, leaf := networkCertificate(t, "localhost")
	d := &dualStackFixture{t: t, file: &fixture{calls: map[string]int{}}, port: port, base: "https://localhost:" + port + "/api", auth: syntheticAuth, cert: cert, roots: x509.NewCertPool(), servers: map[string]*httptest.Server{}, calls: map[string]*atomic.Int64{"4": {}, "6": {}}, status: map[string]*atomic.Int32{"4": {}, "6": {}}}
	d.roots.AddCert(leaf)
	t.Cleanup(func() { d.stop("4"); d.stop("6") })
	d.startListener("6", l, cert)
	d.start("4", cert)
	return d
}

func (d *dualStackFixture) start(family string, cert tls.Certificate) {
	d.t.Helper()
	host := "127.0.0.1"
	if family == "6" {
		host = "::1"
	}
	l, err := (&net.ListenConfig{}).Listen(d.t.Context(), "tcp"+family, net.JoinHostPort(host, d.port))
	if err != nil {
		d.t.Fatal(err)
	}
	d.startListener(family, l, cert)
}

func (d *dualStackFixture) startListener(family string, l net.Listener, cert tls.Certificate) {
	d.t.Helper()
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "localhost:"+d.port || !strings.HasPrefix(r.URL.Path, "/api/internal/computer-transfers/operation-1/delivery-1/bots/bot-1/media") || r.Header.Get("Authorization") != d.auth || d.selection != "" && r.Header.Get("Memoh-Transfer-Context") != d.selection {
			d.t.Error("authority, API prefix, task/delivery or credential changed across routes")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		d.calls[family].Add(1)
		d.last.Store(family)
		if code := d.status[family].Load(); code != 0 {
			w.WriteHeader(int(code))
			return
		}
		// The configured-device fixture above verifies the real header first;
		// adapt only the existing in-memory protocol fixture's synthetic auth.
		r.Header.Set("Authorization", syntheticAuth)
		d.file.serve(w, r)
	}))
	_ = s.Listener.Close()
	s.Listener = l
	s.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	s.Config.ErrorLog = log.New(io.Discard, "", 0)
	s.StartTLS()
	d.servers[family] = s
}

func (d *dualStackFixture) stop(family string) {
	if s := d.servers[family]; s != nil {
		s.CloseClientConnections()
		s.Close()
		delete(d.servers, family)
	}
}

func (d *dualStackFixture) client() *Client {
	d.t.Helper()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Only isolated roots are injected. DialContext, resolver, proxy policy and
	// Happy Eyeballs remain the production DefaultTransport configuration.
	transport.TLSClientConfig = &tls.Config{RootCAs: d.roots, MinVersion: tls.VersionTLS12}
	c, err := NewTask(d.base, "operation-1", "delivery-1", &http.Client{Transport: transport}, func(context.Context) (string, error) { return syntheticAuth, nil })
	if err != nil {
		d.t.Fatal(err)
	}
	d.t.Cleanup(c.CloseIdleConnections)
	return c
}

func (d *dualStackFixture) trace(ctx context.Context) context.Context {
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{ConnectStart: func(_, address string) {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			d.t.Error(err)
			return
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			d.t.Error("fixture dial escaped loopback")
			return
		}
		family := "6"
		if ip.To4() != nil {
			family = "4"
		}
		d.mu.Lock()
		d.dials = append(d.dials, family)
		d.mu.Unlock()
	}})
}

func (d *dualStackFixture) logDials(stage string) {
	d.t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.t.Logf("%s tcp_families=%v http_ipv4=%d http_ipv6=%d unchanged_authority_and_scope=true", stage, d.dials, d.calls["4"].Load(), d.calls["6"].Load())
	d.dials = nil
}

func TestDualStackSameOriginRecovery(t *testing.T) {
	d := newDualStackFixture(t)
	c := d.client()
	if _, err := c.Limits(d.trace(t.Context()), "bot-1"); err != nil {
		t.Fatal(err)
	}
	first := d.last.Load().(string)
	other := "4"
	if first == "4" {
		other = "6"
	}
	d.logDials("both_available")
	src := frozen("route-transition")
	s := storeJob(t, newJob(src))
	advance(t, c, s, src)
	initial := advance(t, c, s, src)
	if initial.Offset != 4 || initial.ReceiptID == "" {
		t.Fatal("initial checkpoint")
	}
	d.stop(first)
	// Read an existing receipt through the surviving actual socket, with the
	// same client first (including its previously used connection pool).
	got, err := c.Get(d.trace(t.Context()), initial.BotID, initial.ReceiptID)
	if err != nil || got.Offset != initial.Offset || d.last.Load() != other {
		t.Fatal("surviving-family receipt", err)
	}
	d.logDials("preferred_connection_closed")
	c = d.client() // The next finite invocation creates a new client as in the host.
	next := advance(t, c, s, src)
	if next.Offset != 8 || next.ReceiptID != initial.ReceiptID || next.OperationID != initial.OperationID {
		t.Fatal("resume changed identity or offset")
	}
	d.stop(other)
	c.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(d.trace(t.Context()), time.Second)
	defer cancel()
	if err := c.Advance(ctx, next, s.save, src, time.Now()); err != nil {
		t.Fatal(err)
	}
	pending := s.load()
	if pending.Phase != "uploading" || pending.ErrorCode != "connection" || pending.Attempts != 1 || pending.NextAttempt.IsZero() || pending.Offset != 8 || pending.ReceiptID != initial.ReceiptID {
		t.Fatal("all routes down must retain original checkpoint and finite retry")
	}
	d.logDials("both_connections_closed")
	d.start(first, d.cert)
	c = d.client()
	got, err = c.Get(d.trace(t.Context()), pending.BotID, pending.ReceiptID)
	if err != nil || got.Offset != 8 || d.last.Load() != first {
		t.Fatal("original family restored", err)
	}
	d.logDials("original_family_restored")
	for range 5 {
		if s.load().Ready == nil {
			advance(t, c, s, src)
		}
	}
	ready := s.load()
	prefix := "/api/internal/computer-transfers/operation-1/delivery-1/bots/bot-1/media"
	if ready.Ready == nil || ready.Ready.ContentHash != digest(src) || ready.ReceiptID != initial.ReceiptID || !bytes.Equal(fixtureReady(d.file), src) || d.file.count("POST "+prefix+"/uploads") != 1 || d.file.count("POST "+prefix+"/uploads/receipt-1/complete") != 1 || d.file.count("PATCH "+prefix+"/uploads/receipt-1") != len(src)/4 {
		t.Fatal("route recovery duplicated upload or changed final bytes/reference")
	}
	if d.calls["4"].Load() == 0 || d.calls["6"].Load() == 0 {
		t.Fatal("both actual IP families were not exercised")
	}
	t.Logf("ready bytes=%d create=1 complete=1 successful_patch=%d same_receipt=true hash_match=true retry_attempts_during_outage=1", len(src), len(src)/4)
}

func TestDualStackDoesNotRerouteTLSOrAuthorizationFailure(t *testing.T) {
	for _, failure := range []string{"trusted_wrong_hostname", "authorization_403"} {
		t.Run(failure, func(t *testing.T) {
			d := newDualStackFixture(t)
			c := d.client()
			if _, err := c.Limits(d.trace(t.Context()), "bot-1"); err != nil {
				t.Fatal(err)
			}
			first := d.last.Load().(string)
			other := "4"
			if first == "4" {
				other = "6"
			}
			before := d.calls[other].Load()
			c.CloseIdleConnections()
			if failure == "trusted_wrong_hostname" {
				wrong, leaf := networkCertificate(t, "wrong-host.invalid")
				d.roots.AddCert(leaf)
				if _, err := leaf.Verify(x509.VerifyOptions{Roots: d.roots, DNSName: "wrong-host.invalid"}); err != nil {
					t.Fatal("wrong-host certificate was not independently trusted", err)
				}
				d.stop(first)
				d.start(first, wrong)
			} else {
				d.status[first].Store(http.StatusForbidden)
			}
			c = d.client()
			ctx, cancel := context.WithTimeout(d.trace(t.Context()), 2*time.Second)
			defer cancel()
			_, err := c.Limits(ctx, "bot-1")
			if failure == "trusted_wrong_hostname" {
				if !errors.Is(err, ErrTransport) {
					t.Fatal("hostname failure accepted", err)
				}
			} else {
				var status *HTTPError
				if !errors.As(err, &status) || status.Status != http.StatusForbidden {
					t.Fatal("authorization failure hidden", err)
				}
			}
			if d.calls[other].Load() != before {
				t.Fatal("TLS/authorization failure silently rerouted to alternate address")
			}
			d.logDials(failure)
			t.Log("healthy alternate remained unused after post-connect failure; no TLS or authorization downgrade")
		})
	}
}
