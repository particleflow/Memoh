package transferclient

import (
	"bytes"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHostNetworkPolicyKeepsOriginalQueueBinding(t *testing.T) {
	getenv, _, write := producerFixture(t, "https://fixture.invalid/api")
	config := getenv("HERDR_PLUGIN_CONFIG_DIR")
	original, err := os.ReadFile(filepath.Join(config, "transfer.json")) //nolint:gosec // Isolated private fixture only.
	if err != nil {
		t.Fatal(err)
	}
	job := newJob([]byte("fixture"))
	job.SnapshotID = job.DeliveryID
	job.Task = DeviceTask{UserID: "user", SessionID: "session", TargetID: "target", RunID: "run", Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32)}
	action := ConfiguredAction{Kind: "enqueue-upload", DeliveryID: job.DeliveryID, Job: job, SourceRef: "result-one"}
	limits := DefaultHostQueueLimits()
	first, err := InvokeConfiguredHerdr(t.Context(), getenv, job.LocatorRevision, limits, action, nil, time.Now())
	if err != nil || !first.Registered {
		t.Fatal("legacy metadata registration", err)
	}
	for _, policy := range []string{
		`{"version":1,"lan_addresses":["192.0.2.1","2001:db8::1"]}`,
		`{"version":1,"lan_addresses":["2001:db8::2"]}`,
	} {
		write(config, "transfer-network.json", []byte(policy))
		// Avoid contacting any endpoint: re-registration and client construction
		// validate configuration only. Explicit no-proxy is isolated test policy.
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		got, err := InvokeConfiguredHerdr(t.Context(), getenv, job.LocatorRevision, limits, action, &http.Client{Transport: transport}, time.Now())
		if err != nil || !got.Registered || got.Job != first.Job {
			t.Fatal("network policy changed original queued identity", err)
		}
	}
	got, err := os.ReadFile(filepath.Join(config, "transfer.json")) //nolint:gosec // Same isolated fixture.
	if err != nil || !bytes.Equal(got, original) {
		t.Fatal("network policy rewrote identity configuration", err)
	}
	t.Log("route policy added/changed; original transfer.json bytes, HostBinding, task/attempt/delivery and source metadata unchanged")
}

func TestHostNetworkPolicyValidation(t *testing.T) {
	getenv, _, write := producerFixture(t, "https://fixture.invalid/api")
	config := getenv("HERDR_PLUGIN_CONFIG_DIR")
	h, err := OpenHostConfig(getenv)
	if err != nil || h.network != nil {
		t.Fatal("absent policy did not preserve legacy behavior", err)
	}
	_ = h.Close()
	for _, policy := range []string{
		`{`, `null`, `{"version":2,"lan_addresses":["127.0.0.1"]}`,
		`{"version":1}`, `{"version":1,"lan_addresses":[]}`,
		`{"version":1,"lan_addresses":["https://example.invalid"]}`,
		`{"version":1,"lan_addresses":["example.invalid"]}`,
		`{"version":1,"lan_addresses":["127.0.0.1:443"]}`,
		`{"version":1,"lan_addresses":["0.0.0.0"]}`,
		`{"version":1,"lan_addresses":["::ffff:0.0.0.0"]}`,
		`{"version":1,"lan_addresses":["::ffff:224.0.0.1"]}`,
		`{"version":1,"lan_addresses":["::"]}`,
		`{"version":1,"lan_addresses":["ff02::1"]}`,
		`{"version":1,"lan_addresses":["224.0.0.1"]}`,
		`{"version":1,"lan_addresses":["::ffff:127.0.0.1","127.0.0.1"]}`,
		`{"version":1,"lan_addresses":["::1"],"origin":"https://other.invalid"}`,
		`{"version":1,"lan_addresses":["::1"]} {}`,
	} {
		write(config, "transfer-network.json", []byte(policy))
		if h, err := OpenHostConfig(getenv); err == nil {
			_ = h.Close()
			t.Fatal("invalid policy silently accepted", policy)
		} else if !errors.Is(err, ErrInvalid) {
			t.Fatal("unexpected error class", err)
		}
	}
	write(config, "transfer-network.json", []byte(`{"version":1,"lan_addresses":["fe80::1%fixture-interface","::1","127.0.0.1"]}`))
	h, err = OpenHostConfig(getenv)
	if err != nil || len(h.network.LANAddresses) != 3 {
		t.Fatal("explicit IPv6 including zone rejected", err)
	}
	_ = h.Close()
}

func TestHostNetworkPolicyRejectsProxyConflict(t *testing.T) {
	getenv, _, write := producerFixture(t, "https://fixture.invalid/api")
	write(getenv("HERDR_PLUGIN_CONFIG_DIR"), "transfer-network.json", []byte(`{"version":1,"lan_addresses":["127.0.0.1"]}`))
	h, err := OpenHostConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = h.Close() }()
	job := newJob([]byte("fixture"))
	job.Task = DeviceTask{UserID: "user", SessionID: "session", TargetID: "target", RunID: "run", Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32)}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = func(*http.Request) (*url.URL, error) { return &url.URL{Scheme: "http", Host: "proxy.invalid"}, nil }
	if _, err := h.newDeviceTask(job, "upload", &http.Client{Transport: transport}); !errors.Is(err, ErrBinding) {
		t.Fatal("direct routing bypassed configured proxy", err)
	}
}
