//go:build linux

package transferclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/media"
)

func endpointPolicy(logical, physical string) *hostNetworkPolicy {
	return &hostNetworkPolicy{Version: 2, Endpoint: &media.TransferEndpoint{LogicalOrigin: logical, PhysicalOrigin: physical}}
}

func endpointClient(t *testing.T, logical, physical string) *Client {
	t.Helper()
	c, err := New(logical+"/api", &http.Client{Transport: &http.Transport{}}, func(context.Context) (string, error) { return syntheticAuth, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := c.useNetworkPolicy(endpointPolicy(logical, physical)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.CloseIdleConnections)
	return c
}

func TestEndpointMappingTrustRedirectAndCentralBootstrap(t *testing.T) {
	t.Run("legacy_explicit_default_port_retains_identity", func(t *testing.T) {
		physical := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(Limits{MaxUploadBytes: 1024})
		}))
		defer physical.Close()
		c, err := New("https://NAS.fixture.invalid:443/api", &http.Client{Transport: &http.Transport{}}, func(context.Context) (string, error) { return syntheticAuth, nil })
		if err != nil {
			t.Fatal(err)
		}
		defer c.CloseIdleConnections()
		if err := c.useNetworkPolicy(endpointPolicy("https://nas.fixture.invalid", physical.URL)); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Limits(t.Context(), "bot-1"); err != nil || c.base != "https://NAS.fixture.invalid:443/api" {
			t.Fatal("logical identity changed", err)
		}
	})
	t.Run("mapped_https_verifies_destination", func(t *testing.T) {
		var hits atomic.Int32
		server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
		defer server.Close()
		c := endpointClient(t, "https://nas.fixture.invalid", server.URL)
		if _, err := c.Limits(t.Context(), "bot-1"); !errors.Is(err, ErrTransport) || hits.Load() != 0 {
			t.Fatal("mapped HTTPS bypassed certificate verification", err)
		}
	})
	var physicalHits, escaped atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { escaped.Add(1) }))
	defer other.Close()
	physical := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		physicalHits.Add(1)
		if r.Header.Get("Authorization") != syntheticAuth || r.URL.Path != "/api/bots/bot-1/media/limits" {
			t.Error("wire identity/path changed")
		}
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	defer physical.Close()
	const logical = "https://nas.fixture.invalid"
	c := endpointClient(t, logical, physical.URL)
	if err := c.useNetworkPolicy(endpointPolicy("https://different.fixture.invalid", physical.URL)); !errors.Is(err, ErrBinding) {
		t.Fatal("cross-identity mapping accepted", err)
	}
	_, err := c.Limits(t.Context(), "bot-1")
	var status *HTTPError
	if !errors.As(err, &status) || status.Status != 307 || physicalHits.Load() != 1 || escaped.Load() != 0 || c.base != logical+"/api" {
		t.Fatal("redirect replay or logical identity changed", err)
	}
	request, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, other.URL, nil)
	response, err := c.http.Transport.RoundTrip(request)
	if response != nil {
		_ = response.Body.Close()
	}
	if !errors.Is(err, ErrBinding) {
		t.Fatal("foreign request escaped mapping", err)
	}

	var controlHits atomic.Int32
	var origin string
	control := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		controlHits.Add(1)
		if r.Header.Get("Authorization") != syntheticAuth {
			t.Error("bootstrap auth changed")
		}
		_ = json.NewEncoder(w).Encode(Limits{MaxUploadBytes: 1 << 20, ChunkMaxBytes: 1 << 20, ResumableEnabled: true, NetworkPolicy: endpointPolicy(origin, physical.URL)})
	}))
	defer control.Close()
	origin = control.URL
	c, err = New(origin+"/api", control.Client(), func(context.Context) (string, error) { return syntheticAuth, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseIdleConnections()
	c.policyBot = "bot-1"
	if _, err := c.Limits(t.Context(), "bot-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Limits(t.Context(), "bot-1"); err == nil || controlHits.Load() != 1 || physicalHits.Load() != 2 {
		t.Fatal("central mapping was not adopted once")
	}
}

// Synthetic local file adapter + the production disk queue/client. This is
// process recovery and actual bytes evidence, not NAS/Windows/phone acceptance.
func TestEndpointMappedMultiFileProcessRecovery(t *testing.T) {
	if os.Getenv("MEMOH_ENDPOINT_CHILD") == "1" {
		getenv := os.Getenv
		h, err := OpenHostConfig(getenv)
		if err != nil {
			t.Fatal(err)
		}
		q := openQueue(t, getenv("HERDR_PLUGIN_STATE_DIR"), endpointQueueLimits())
		status := inspectQueue(t, q, "delivery-1")
		c, err := h.newDeviceTask(status.Job, "upload", &http.Client{Transport: &http.Transport{}})
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if err := q.RunOne(t.Context(), "delivery-1", c, nil, time.Now().Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
		}
		// Deliberately bypass defer/Close; OS locks must release at process exit.
		os.Exit(0)
	}
	remotes := make(map[string]*fixture)
	data := make(map[string][]byte)
	for i := 1; i <= 3; i++ {
		id := fmt.Sprintf("delivery-%d", i)
		data[id] = bytes.Repeat([]byte{byte(i), 0, 0xff, 0x80, 31}, (32<<20)/5+7)
		remotes[id] = &fixture{calls: map[string]int{}}
	}
	remotes["delivery-1"].setDrop("PATCH /bots/bot-1/media/uploads/receipt-1")
	var denied atomic.Bool
	var firstRange atomic.Int64
	var calls, received atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if denied.Load() {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.Header.Get("Authorization") != "Memoh-Transfer fixture-stable-device" || r.Header.Get("Memoh-Transfer-Context") == "" {
			t.Error("device identity/context missing")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/api/internal/computer-transfers/operation-1/"), "/", 2)
		if len(parts) != 2 || remotes[parts[0]] == nil {
			t.Error("task path changed")
			w.WriteHeader(http.StatusNotFound)
			return
		}
		f := remotes[parts[0]]
		path := "/" + parts[1]
		if strings.HasSuffix(path, "/limits") {
			_ = json.NewEncoder(w).Encode(Limits{MaxUploadBytes: 64 << 20, ChunkMaxBytes: 1 << 20, ResumableEnabled: true})
			return
		}
		if !strings.Contains(path, "/uploads") {
			_, original := f.current()
			if r.Header.Get("Range") != "" {
				var offset int64
				_, _ = fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-", &offset)
				firstRange.CompareAndSwap(0, offset)
			}
			w.Header().Set("ETag", `"`+digest(original)+`"`)
			http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(original))
			return
		}
		if r.Method == http.MethodPatch {
			received.Add(r.ContentLength)
		}
		r.URL.Path = path
		r.Header.Set("Authorization", syntheticAuth)
		f.serve(w, r)
	})
	physical := httptest.NewServer(handler)
	defer physical.Close()
	control := httptest.NewTLSServer(handler)
	defer control.Close()
	logical := control.URL
	getenv, _, write := producerFixture(t, logical+"/api")
	policyRaw, _ := json.Marshal(endpointPolicy(logical, physical.URL))
	h, err := OpenHostConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	initialBinding := h.Binding
	q := openQueue(t, getenv("HERDR_PLUGIN_STATE_DIR"), endpointQueueLimits())
	for id, src := range data {
		file, err := os.CreateTemp(t.TempDir(), "immutable-output")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = file.Write(src)
		job := networkJob(src)
		job.DeliveryID, job.SnapshotID, job.HostBinding = id, id, initialBinding
		if err := q.Admit(t.Context(), "upload", job, file); err != nil {
			t.Fatal(err)
		}
		_ = file.Close()
	}
	originalClient, err := h.newDeviceTask(inspectQueue(t, q, "delivery-1").Job, "upload", control.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := q.RunOne(t.Context(), "delivery-1", originalClient, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	originalClient.CloseIdleConnections()
	_ = h.Close()
	_ = q.Close()
	write(getenv("HERDR_PLUGIN_CONFIG_DIR"), "transfer-network.json", policyRaw)
	child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestEndpointMappedMultiFileProcessRecovery$") //nolint:gosec // Re-execute only the current test binary with a fixed test selector and synthetic fixture configuration.
	child.Env = append(os.Environ(), "MEMOH_ENDPOINT_CHILD=1", "HERDR_PLUGIN_CONFIG_DIR="+getenv("HERDR_PLUGIN_CONFIG_DIR"), "HERDR_PLUGIN_STATE_DIR="+getenv("HERDR_PLUGIN_STATE_DIR"))
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("child %v: %s", err, output)
	}
	q = openQueue(t, getenv("HERDR_PLUGIN_STATE_DIR"), endpointQueueLimits())
	partial := inspectQueue(t, q, "delivery-1").Job
	if partial.ReceiptID != "receipt-1" || partial.Offset != 0 || received.Load() != 1<<20 {
		t.Fatal("lost patch/process exit changed receipt or checkpoint", partial)
	}
	// A physical endpoint edit leaves transfer.json and the original queue binding unchanged.
	address, _ := url.Parse(physical.URL)
	address.Host = "localhost:" + address.Port()
	policyRaw, _ = json.Marshal(endpointPolicy(logical, address.String()))
	write(getenv("HERDR_PLUGIN_CONFIG_DIR"), "transfer-network.json", policyRaw)
	h, err = OpenHostConfig(getenv)
	if err != nil || h.Binding != initialBinding {
		t.Fatal("route edit invalidated host binding", err)
	}
	defer func() { _ = h.Close() }()
	uploadBefore := received.Load()
	start := time.Now()
	var wg sync.WaitGroup
	for id := range data {
		wg.Go(func() {
			var requestStart, firstResponse atomic.Int64
			ctx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{GetConn: func(string) { requestStart.Store(time.Now().UnixNano()) }, GotFirstResponseByte: func() { firstResponse.CompareAndSwap(0, time.Now().UnixNano()-requestStart.Load()) }})
			job := inspectQueue(t, q, id).Job
			c, err := h.newDeviceTask(job, "upload", &http.Client{Transport: &http.Transport{}})
			if err != nil {
				t.Error(err)
				return
			}
			defer c.CloseIdleConnections()
			for range 40 {
				if inspectQueue(t, q, id).Job.Ready != nil {
					break
				}
				if err := q.RunOne(ctx, id, c, nil, time.Now().Add(2*time.Hour)); err != nil {
					t.Error(err)
					return
				}
			}
			t.Logf("isolated_http_first_request id=%s ttfb_ms=%.3f", id, float64(firstResponse.Load())/float64(time.Millisecond))
		})
	}
	wg.Wait()
	elapsed := time.Since(start)
	var total int64
	for id, src := range data {
		got := inspectQueue(t, q, id).Job
		_, original := remotes[id].current()
		if got.Ready == nil || got.ReceiptID != "receipt-1" || got.HostBinding != initialBinding || got.OperationID != "operation-1" || !bytes.Equal(original, src) || got.Ready.ContentHash != digest(src) {
			t.Fatal("original identity/bytes/reference lost", id, got)
		}
		if remotes[id].count("POST /bots/bot-1/media/uploads") != 1 || remotes[id].count("POST /bots/bot-1/media/uploads/receipt-1/complete") != 1 {
			t.Fatal("create/complete duplicated")
		}
		total += int64(len(src))
		t.Logf("upload id=%s bytes=%d hash=%s receipt=%s create=1 complete=1", id, len(src), digest(src), got.ReceiptID)
	}
	if received.Load() != total {
		t.Fatal("confirmed byte retransmission", received.Load(), total)
	}
	intervalBytes := received.Load() - uploadBefore
	t.Logf("isolated_http_multi_file completed_total_bytes=%d interval_bytes=%d duration_ms=%.3f MiB_per_s=%.3f concurrent_jobs=3", total, intervalBytes, float64(elapsed)/float64(time.Millisecond), float64(intervalBytes)/(1<<20)/elapsed.Seconds())
	job := inspectQueue(t, q, "delivery-1").Job
	c, err := h.newDeviceTask(job, "download", &http.Client{Transport: &http.Transport{}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseIdleConnections()
	if _, err := c.Limits(t.Context(), job.BotID); err != nil {
		t.Fatal(err)
	}
	file, err := os.CreateTemp(t.TempDir(), "resume-part")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	prefix := int64(1 << 20)
	_, _ = file.Write(data["delivery-1"][:prefix])
	_ = file.Sync()
	var checkpoint int64
	downloadStart := time.Now()
	var first time.Duration
	ctx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{GotFirstResponseByte: func() {
		if first == 0 {
			first = time.Since(downloadStart)
		}
	}})
	if err := c.Download(ctx, job.BotID, job.Spec.SHA256, job.Spec.SizeBytes, prefix, file, func(n int64) error { checkpoint = n; return nil }); err != nil {
		t.Fatal(err)
	}
	if checkpoint != job.Spec.SizeBytes || firstRange.Load() != prefix {
		t.Fatal("Range/checkpoint changed")
	}
	_, _ = file.Seek(0, io.SeekStart)
	got, _ := io.ReadAll(file)
	if !bytes.Equal(got, data["delivery-1"]) {
		t.Fatal("download hash mismatch")
	}
	t.Logf("isolated_http_download total_bytes=%d resumed_at=%d duration_ms=%.3f ttfb_ms=%.3f hash=%s", len(got), prefix, float64(time.Since(downloadStart))/float64(time.Millisecond), float64(first)/float64(time.Millisecond), digest(got))
	denied.Store(true)
	if err := c.Download(t.Context(), job.BotID, job.Spec.SHA256, job.Spec.SizeBytes, job.Spec.SizeBytes, file, func(int64) error { return nil }); err == nil {
		t.Fatal("complete cache bypassed current authorization")
	}
	t.Logf("physical_route_only=true same_host_binding=true process_reopen=true revoked_complete_cache_denied=true requests=%d", calls.Load())
}

func endpointQueueLimits() QueueLimits {
	return QueueLimits{Jobs: 8, Bytes: 128 << 20, JournalBytes: 1 << 20, Workers: 3}
}
