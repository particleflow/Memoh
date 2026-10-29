package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/media"
)

func TestUploadFailureClientToAuthenticatedHandler(t *testing.T) {
	if os.Getenv("HARMONY_TEST_TYPESCRIPT") == "" {
		t.Skip("set HARMONY_TEST_TYPESCRIPT for the existing client integration fixture")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is required for the isolated client fixture")
	}
	f := newUploadHTTPFixture(t)
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	baseEnv := append(os.Environ(), "HARMONY_UPLOAD_FAILURE_HTTP_ONLY=1", "HARMONY_UPLOAD_FAILURE_SERVER_URL="+f.server.URL, "HARMONY_UPLOAD_FAILURE_TOKEN="+f.token, "HARMONY_UPLOAD_FAILURE_OWNER="+invocationTestOtherUser, "HARMONY_UPLOAD_FAILURE_BOT_ID="+invocationTestBotID, "HARMONY_UPLOAD_FAILURE_OUTBOX_PATH="+filepath.Join(t.TempDir(), "outbox.json"))
	command := exec.CommandContext(t.Context(), "node", filepath.Join(repo, "mobile/client/tests/stage2/harmony_resumable_frontend.cjs"))
	command.Env = append(baseEnv, "HARMONY_UPLOAD_FAILURE_UNKNOWN_ACK=1", "HARMONY_UPLOAD_FAILURE_RESTORE_ONLY=0")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("client fixture: %v\n%s", err, output)
	}
	var result struct {
		RealHTTPServerReadback bool   `json:"realHTTPServerReadback"`
		Operation              string `json:"operation"`
		FileBytesSent          int    `json:"fileBytesSent"`
		PID                    int    `json:"pid"`
		Report                 string `json:"report"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output), &result); err != nil || !result.RealHTTPServerReadback || result.FileBytesSent != 0 || result.Report != "pending" {
		t.Fatalf("unexpected client result: %s %v", output, err)
	}
	records, err := f.handler.mediaService.Uploads().Failures(t.Context(), invocationTestOtherUser, invocationTestBotID, result.Operation)
	if err != nil || len(records) != 1 || records[0].SDKCode != 13900012 || records[0].Phase != "source-open" {
		t.Fatal(records, err)
	}
	t.Log(string(bytes.TrimSpace(output)))
	second := exec.CommandContext(t.Context(), "node", filepath.Join(repo, "mobile/client/tests/stage2/harmony_resumable_frontend.cjs"))
	second.Env = append(baseEnv, "HARMONY_UPLOAD_FAILURE_UNKNOWN_ACK=0", "HARMONY_UPLOAD_FAILURE_RESTORE_ONLY=1")
	resumed, err := second.CombinedOutput()
	if err != nil {
		t.Fatalf("restore process: %v\n%s", err, resumed)
	}
	var restored struct {
		Operation      string `json:"operation"`
		PID            int    `json:"pid"`
		Report         string `json:"report"`
		NativeCommands int    `json:"nativeCommands"`
		FileBytesSent  int    `json:"fileBytesSent"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(resumed), &restored); err != nil || restored.Operation != result.Operation || restored.PID == result.PID || restored.Report != "reported" || restored.NativeCommands != 0 || restored.FileBytesSent != 0 {
		t.Fatalf("unexpected independent restore: %s %v", resumed, err)
	}
	after, err := f.handler.mediaService.Uploads().Failures(t.Context(), invocationTestOtherUser, invocationTestBotID, result.Operation)
	if err != nil || len(after) != 1 || after[0] != records[0] {
		t.Fatal("metadata restart replaced first record", after, err)
	}
	t.Log(string(bytes.TrimSpace(resumed)))
}

func TestUploadFailureAuthenticatedHTTPMetadataOnly(t *testing.T) {
	f := newUploadHTTPFixture(t)
	base := "/bots/" + invocationTestBotID + "/media/upload-failures"
	operation := strings.Repeat("a", 32)
	path := base + "/" + operation
	input := media.UploadFailureInput{Version: 1, Phase: "source-open", Code: "VOICE_UNAVAILABLE", SDKCode: 13900012, At: time.Now().UnixMilli()}
	body, _ := json.Marshal(input)
	request := func(method, path, token string, data []byte, want int) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, f.server.URL+path, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := f.server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		got, err := io.ReadAll(res.Body)
		if err != nil || res.StatusCode != want {
			t.Fatalf("status %d want %d: %s %v", res.StatusCode, want, got, err)
		}
		if bytes.Contains(got, []byte("SECRET")) || bytes.Contains(got, []byte("Bearer")) {
			t.Fatal("private fields exposed")
		}
		return got
	}
	first := request(http.MethodPut, path, f.token, body, 200)
	if got := request(http.MethodPut, path, f.token, body, 200); !bytes.Equal(got, first) {
		t.Fatal("repeat changed record")
	}
	var report media.UploadFailure
	if err := json.Unmarshal(first, &report); err != nil || report.Operation != operation {
		t.Fatal(report, err)
	}
	got := request(http.MethodGet, path, f.token, nil, 200)
	originalReadback := append([]byte(nil), got...)
	var rows []media.UploadFailure
	if err := json.Unmarshal(got, &rows); err != nil || len(rows) != 1 || rows[0] != report {
		t.Fatal(rows, err)
	}
	request(http.MethodGet, base, f.token, nil, 200)
	changed := input
	changed.Code = "MEDIA_SHA_MISMATCH"
	conflict, _ := json.Marshal(changed)
	request(http.MethodPut, path, f.token, conflict, 409)
	request(http.MethodPut, path, f.token, []byte(`{"version":1,"phase":"source-open","code":"VOICE_UNAVAILABLE","at":1,"uri":"SECRET_URI"}`), 400)
	request(http.MethodPut, path, f.token, []byte(strings.Repeat("x", 4096)), 400)
	request(http.MethodGet, path, "", nil, 401)
	request(http.MethodGet, path, "bad-token", nil, 401)
	other, _, err := auth.GenerateToken("44444444-4444-4444-8444-444444444444", "synthetic-http-secret", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	request(http.MethodGet, path, other, nil, 404)
	cross := strings.Replace(path, invocationTestBotID, "44444444-4444-4444-4444-444444444444", 1)
	request(http.MethodGet, cross, f.token, nil, 404)
	f.queries.revoked.Store(true)
	request(http.MethodGet, path, f.token, nil, 403)
	request(http.MethodPut, path, f.token, body, 403)
	f.queries.revoked.Store(false)
	if got := request(http.MethodGet, path, f.token, nil, 200); !bytes.Equal(got, originalReadback) {
		t.Fatal("denied request changed data")
	}
	uploads, err := f.handler.mediaService.Uploads().Failures(t.Context(), invocationTestOtherUser, invocationTestBotID, operation)
	if err != nil || len(uploads) != 1 {
		t.Fatal(uploads, err)
	}
}
