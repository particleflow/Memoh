//go:build integration

package db_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/transferclient"
)

// Observed after /api stripping and immediately before the actual Server. No
// credential, body or private source path is collected in this request record.
type apiBaseReadRequest struct {
	method, path, query, byteRange, ifRange string
}

func exerciseAPIBaseDownload(ctx context.Context, t *testing.T, assets *media.Service, id computertask.DeliveryIdentity, upload transferclient.Job, base string, transport *http.Client, credential string, seen func(apiBaseReadRequest) bool) {
	t.Helper()
	job := upload
	job.DeliveryID, job.SnapshotID = "api-base-download", "api-base-download"
	client, err := transferclient.NewDeviceTask(base, job, "download", job.Task, transport, func(context.Context) (string, error) { return "Memoh-Transfer " + credential, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	file, err := os.CreateTemp(t.TempDir(), "download-*.part")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	content := []byte("synthetic delivery content")
	var offsets []int64
	checkpoint := func(offset int64) error { offsets = append(offsets, offset); return nil }
	if err := client.Download(ctx, job.BotID, job.Spec.SHA256, job.Spec.SizeBytes, 0, file, checkpoint); err != nil {
		t.Fatal("prefixed full download", err)
	}
	// Keep a real downloaded prefix and reopen the private staging file. This is
	// a deterministic Range checkpoint setup, not a crash/connection-loss test.
	const offset = int64(7)
	if file.Truncate(offset) != nil || file.Sync() != nil || file.Close() != nil {
		t.Fatal("prepare partial downloaded checkpoint")
	}
	file, err = os.OpenFile(file.Name(), os.O_RDWR, 0o600) //nolint:gosec // Reopen only the test-owned private staging file.
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Download(ctx, job.BotID, job.Spec.SHA256, job.Spec.SizeBytes, offset, file, checkpoint); err != nil {
		t.Fatal("prefixed Range and full hash", err)
	}
	if err := client.Download(ctx, job.BotID, job.Spec.SHA256, job.Spec.SizeBytes, job.Spec.SizeBytes, file, checkpoint); err != nil {
		t.Fatal("prefixed completed checkpoint HEAD", err)
	}
	if len(offsets) != 2 || offsets[0] != job.Spec.SizeBytes || offsets[1] != job.Spec.SizeBytes {
		t.Fatal("unexpected download checkpoints")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(io.LimitReader(file, job.Spec.SizeBytes+1))
	if err != nil || !bytes.Equal(got, content) {
		t.Fatal("staged bytes changed", err)
	}
	path := "/internal/computer-transfers/" + job.OperationID + "/" + job.DeliveryID + "/bots/" + job.BotID + "/media/" + job.Spec.SHA256
	etag := `"` + job.Spec.SHA256 + `"`
	for _, want := range []apiBaseReadRequest{{http.MethodGet, path, "", "", ""}, {http.MethodGet, path, "", "bytes=7-", etag}, {http.MethodHead, path, "", "", ""}} {
		if !seen(want) {
			t.Fatal("production library lost download path or Range headers")
		}
	}

	// The production library has no query-bearing download API. These explicit
	// HTTP probes retain its existing wire selectors and real device verifier;
	// the selectors grant no authority on their own. No new protocol is added.
	selection, err := json.Marshal(struct {
		Identity  computertask.DeliveryIdentity `json:"identity"`
		Reference computertask.DeliveryRequest  `json:"reference"`
	}{id, computertask.DeliveryRequest{Version: 1, Direction: "download", ContentHash: job.Spec.SHA256, SizeBytes: job.Spec.SizeBytes, Mime: job.Spec.Mime}})
	if err != nil {
		t.Fatal(err)
	}
	const query = "probe=left%2fright&probe=left%2Fright&space=a+b&space=a%20b&empty=&flag&encoded=%25"
	for _, tc := range []struct {
		name, method, query, byteRange string
		status                         int
		body                           []byte
	}{
		{"head-query", http.MethodHead, query, "", http.StatusOK, nil},
		{"range-query", http.MethodGet, query, "bytes=7-", http.StatusPartialContent, content[offset:]},
		{"token-query-rejected", http.MethodGet, query + "&token=synthetic-rejected-value", "bytes=7-", http.StatusUnauthorized, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(ctx, tc.method, base+path+"?"+tc.query, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Memoh-Transfer "+credential)
			req.Header.Set("Memoh-Transfer-Context", base64.RawURLEncoding.EncodeToString(selection))
			req.Header.Set("Accept-Encoding", "identity")
			if tc.byteRange != "" {
				req.Header.Set("Range", tc.byteRange)
				req.Header.Set("If-Range", etag)
			}
			resp, err := transport.Do(req) //nolint:gosec // Only the test-owned HTTPS server and fixed query cases; no user/remote URL input.
			if err != nil {
				t.Fatal("query probe transport failed")
			}
			defer func() { _ = resp.Body.Close() }()
			body, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
			if err != nil || len(body) > 4096 || resp.StatusCode != tc.status || resp.Header.Get("Cache-Control") != "no-store" {
				t.Fatal("query response status/bounds/cache mismatch", resp.StatusCode)
			}
			if !seen(apiBaseReadRequest{tc.method, path, tc.query, tc.byteRange, req.Header.Get("If-Range")}) {
				t.Fatal("RawQuery order, escaping or path changed before real handler")
			}
			if tc.status == http.StatusUnauthorized {
				if resp.Header.Get("ETag") != "" || bytes.Contains(body, content) {
					t.Fatal("query credential rejection exposed original")
				}
				return
			}
			length := job.Spec.SizeBytes
			if tc.status == http.StatusPartialContent {
				length -= offset
				if resp.Header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", offset, job.Spec.SizeBytes-1, job.Spec.SizeBytes) {
					t.Fatal("query Range metadata changed")
				}
			}
			if resp.ContentLength != length || resp.Header.Get("ETag") != etag || !bytes.Equal(body, tc.body) {
				t.Fatal("query changed media bytes or identity")
			}
		})
	}
	reader, asset, err := assets.OpenSeekable(ctx, job.BotID, job.Spec.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	original, err := io.ReadAll(io.LimitReader(reader, job.Spec.SizeBytes+1))
	if err != nil || asset.ContentHash != job.Spec.SHA256 || !bytes.Equal(original, content) {
		t.Fatal("original changed after downloads", err)
	}
	t.Log("real prefixed media GET/Range/HEAD and full hash passed; RawQuery bytes preserved; unknown query ignored, token query rejected; original bytes retained; no local publication")
}
