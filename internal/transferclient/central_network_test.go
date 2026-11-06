package transferclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCentralNetworkPolicyBootstrapsOnceBeforeWrites(t *testing.T) {
	f := &fixture{calls: map[string]int{}}
	reads := 0
	fileReads := 0
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != syntheticAuth {
			t.Fatal("credential changed")
		}
		if strings.HasSuffix(r.URL.Path, "/limits") {
			reads++
			_ = json.NewEncoder(w).Encode(Limits{MaxUploadBytes: 1024, ChunkMaxBytes: 4, ResumableEnabled: true, NetworkPolicy: &hostNetworkPolicy{Version: 1, LANAddresses: []string{"192.0.2.1", "192.0.2.2"}}})
			return
		}
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/uploads/") {
			recorder := httptest.NewRecorder()
			f.serve(recorder, r)
			var reply map[string]any
			if json.Unmarshal(recorder.Body.Bytes(), &reply) != nil {
				t.Error("invalid fixture receipt")
				return
			}
			reply["network_policy"] = hostNetworkPolicy{Version: 1, LANAddresses: []string{"192.0.2.1", "192.0.2.2"}}
			w.WriteHeader(recorder.Code)
			_ = json.NewEncoder(w).Encode(reply)
			return
		}
		receipt, ready := f.current()
		if len(receipt.ContentHash) == 64 && strings.HasSuffix(r.URL.Path, "/media/"+receipt.ContentHash) {
			fileReads++
			w.Header().Set("ETag", `"`+receipt.ContentHash+`"`)
			w.Header().Set("Content-Type", receipt.Mime)
			http.ServeContent(w, r, "original", time.Time{}, bytes.NewReader(ready))
			return
		}
		f.serve(w, r)
	}))
	defer s.Close()
	transport := s.Client().Transport.(*http.Transport).Clone()
	transport.Proxy = nil
	original := strings.TrimPrefix(s.URL, "https://")
	var addresses []string
	transport.DialContext = func(ctx context.Context, _, address string) (net.Conn, error) {
		addresses = append(addresses, address)
		host, _, _ := net.SplitHostPort(address)
		if host == "192.0.2.1" {
			return nil, errors.New("fixture refused before sending")
		}
		return (&net.Dialer{}).DialContext(ctx, "tcp", original)
	}
	c, err := New(s.URL+"/api", &http.Client{Transport: transport}, func(context.Context) (string, error) { return syntheticAuth, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseIdleConnections()
	c.policyBot = "bot-1" // Same opt-in as the configured host constructor.
	if _, err := c.Limits(t.Context(), "bot-1"); err != nil {
		t.Fatal(err)
	}
	spec := Spec{SizeBytes: 4, SHA256: digest([]byte("test")), Mime: "text/plain"}
	receipt, err := c.Create(t.Context(), "bot-1", spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Patch(t.Context(), "bot-1", receipt.ID, 0, []byte("test")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Complete(t.Context(), "bot-1", receipt.ID); err != nil {
		t.Fatal(err)
	}
	if reads != 1 || len(addresses) != 3 || !strings.HasPrefix(addresses[1], "192.0.2.1:") || !strings.HasPrefix(addresses[2], "192.0.2.2:") {
		t.Fatal(reads, addresses)
	}
	if f.count("POST /api/bots/bot-1/media/uploads") != 1 || f.count("PATCH /api/bots/bot-1/media/uploads/receipt-1") != 1 {
		t.Fatal("write replayed")
	}
	// A reopened upload already has its original receipt GET. It must not add
	// another limits/probe request on every scheduler/chunk invocation.
	reopened, err := New(s.URL+"/api", &http.Client{Transport: transport}, func(context.Context) (string, error) { return syntheticAuth, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.CloseIdleConnections()
	reopened.policyBot = "bot-1"
	if _, err := reopened.Get(t.Context(), "bot-1", receipt.ID); err != nil {
		t.Fatal(err)
	}
	if !reopened.policyLoaded || reads != 1 {
		t.Fatal("resume added a limits read", reads)
	}
	if _, err := reopened.Get(t.Context(), "bot-1", receipt.ID); err != nil {
		t.Fatal(err)
	}
	if reads != 1 {
		t.Fatal("chunk metadata added a limits read")
	}
	download, err := New(s.URL+"/api", &http.Client{Transport: transport}, func(context.Context) (string, error) { return syntheticAuth, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer download.CloseIdleConnections()
	download.policyBot, download.bootstrapPolicy = "bot-1", true
	file, err := os.CreateTemp(t.TempDir(), "original")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	if err := download.Download(t.Context(), "bot-1", spec.SHA256, spec.SizeBytes, 0, file, func(int64) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if reads != 2 || fileReads != 1 {
		t.Fatal("download added per-chunk reads", reads)
	}
}

func TestCentralNetworkPolicyRejectsInvalidAndDeniedMetadata(t *testing.T) {
	for _, value := range []string{
		`{"network_policy":{"version":1,"lan_addresses":["https://other.invalid"]}}`,
		`{"network_policy":{"version":1,"lan_addresses":["192.0.2.1"],"origin":"https://other.invalid"}}`,
		`denied`,
	} {
		t.Run(value, func(t *testing.T) {
			writes := 0
			s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					writes++
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				if value == "denied" {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				_, _ = w.Write([]byte(strings.TrimSuffix(value, "}") + `,"max_upload_bytes":1024}`))
			}))
			defer s.Close()
			c, err := New(s.URL, s.Client(), func(context.Context) (string, error) { return syntheticAuth, nil })
			if err != nil {
				t.Fatal(err)
			}
			defer c.CloseIdleConnections()
			c.policyBot = "bot-1"
			c.bootstrapPolicy = true // Download's existing scoped authorization/config read.
			if _, err := c.Create(t.Context(), "bot-1", Spec{SizeBytes: 4, SHA256: digest([]byte("test")), Mime: "text/plain"}); err == nil || writes != 0 {
				t.Fatal("metadata failure allowed write", err, writes)
			}
		})
	}
}
