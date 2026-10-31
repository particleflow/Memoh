package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/transferclient"
)

func TestCLIPathFeedbackApplicability(t *testing.T) {
	for _, mode := range []string{"download-no-context", "download-foreign-context", "upload-v1", "upload-missing-context", "upload-foreign-context", "upload-write-failure"} {
		t.Run(mode, func(t *testing.T) {
			root := cliPrivateRoot(t)
			config, state, source := filepath.Join(root, "config"), filepath.Join(root, "state"), filepath.Join(root, "source")
			for _, p := range []string{config, state, source} {
				if err := cliFixtureDir(p); err != nil {
					t.Fatal(err)
				}
			}
			write := func(path string, data []byte) {
				t.Helper()
				if err := cliFixtureWrite(path, data); err != nil {
					t.Fatal(err)
				}
			}
			settings, _ := json.Marshal(map[string]any{"version": 1, "origin": "https://localhost", "source_root": source})
			write(filepath.Join(config, "transfer.json"), settings)
			write(filepath.Join(config, "device.credential"), []byte("synthetic-feedback-device"))
			data := []byte("feedback fixture")
			sum := sha256.Sum256(data)
			closed := deliverymeta.Closed{Plan: deliverymeta.Plan{Version: 1, Task: deliverymeta.Task{OperationID: uuid.NewString(), BotID: uuid.NewString(), UserID: uuid.NewString(), SessionID: uuid.NewString(), TargetID: uuid.NewString(), RunID: "fixture-run", Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32), RootHash: deliverymeta.RootHash(source)}, Artifact: deliverymeta.Artifact{SourceRef: "result-one", Mime: "text/plain"}}, Closed: true, SHA256: hex.EncodeToString(sum[:]), SizeBytes: int64(len(data))}
			write(filepath.Join(source, "result-one"), data)
			relative, _ := deliverymeta.AttemptDirectory(closed.Task.Attempt)
			attempt := source
			for _, part := range strings.Split(relative, "/") {
				attempt = filepath.Join(attempt, part)
				if err := cliFixtureDir(attempt); err != nil {
					t.Fatal(err)
				}
			}
			plan := closed.Plan
			if strings.Contains(mode, "foreign-context") {
				plan.Task.TargetID = uuid.NewString()
			}
			contextRaw, _ := json.Marshal(plan)
			contextPath := filepath.Join(attempt, deliverymeta.ProducerContextName)
			if !strings.Contains(mode, "no-context") && !strings.Contains(mode, "missing-context") {
				write(contextPath, contextRaw)
			}
			feedbackPath := filepath.Join(attempt, deliverymeta.ProducerFeedbackName)
			if mode == "upload-write-failure" {
				// A real open-for-write failure after successful context validation;
				// do not change any existing file's ownership or ACL to simulate it.
				if err := cliFixtureDir(feedbackPath); err != nil {
					t.Fatal(err)
				}
			}
			env := func(key string) string {
				if key == "HERDR_PLUGIN_CONFIG_DIR" {
					return config
				}
				if key == "HERDR_PLUGIN_STATE_DIR" {
					return state
				}
				return ""
			}
			raw, _ := json.Marshal(closed)
			action, _, err := transferclient.ProducerAdmission(bytes.NewReader(raw), "native-profile-v1")
			if err != nil {
				t.Fatal(err)
			}
			kind, want := "enqueue-result", "unavailable"
			if strings.HasPrefix(mode, "download-") {
				kind, want = "enqueue-download", "not_applicable"
				raw, _ = json.Marshal(admissionInput{Job: action.Job})
			} else if mode == "upload-v1" {
				want = "recorded"
			}
			invoke := func(args []string, raw []byte, wantCode int) map[string]json.RawMessage {
				t.Helper()
				var out, stderr bytes.Buffer
				if code := runCommand(args, bytes.NewReader(raw), &out, &stderr, env, nil, time.Now); code != wantCode {
					t.Fatalf("CLI exit=%d output=%s stderr=%s", code, out.String(), stderr.String())
				}
				var result map[string]json.RawMessage
				if json.Unmarshal(out.Bytes(), &result) != nil {
					t.Fatal("invalid JSON", out.String())
				}
				return result
			}
			for range 2 {
				for _, result := range []map[string]json.RawMessage{invoke([]string{"-action", kind}, raw, 0), invoke([]string{"-action", "inspect", "-delivery", closed.DeliveryID()}, nil, 0)} {
					if string(result["feedback_status"]) != `"`+want+`"` || string(result["delivery_id"]) != `"`+closed.DeliveryID()+`"` || string(result["associated"]) != "false" {
						t.Fatalf("wrong feedback/applicability or fabricated completion: %s", result)
					}
				}
			}
			if mode == "upload-v1" {
				raw, err := os.ReadFile(feedbackPath) //nolint:gosec // Private fixture path.
				feedback, ok := deliverymeta.DecodeFeedback(raw, closed.Plan)
				if err != nil || !ok || feedback.Version != 1 || len(feedback.Files) != 1 || feedback.Files[0].DeliveryID != closed.DeliveryID() {
					t.Fatal("legacy v1 feedback changed", err)
				}
			} else if mode != "upload-write-failure" {
				if _, err := os.Stat(feedbackPath); !os.IsNotExist(err) {
					t.Fatal("unexpected feedback written", err)
				}
			}
			if strings.Contains(mode, "foreign-context") {
				got, err := os.ReadFile(contextPath) //nolint:gosec // Private fixture path.
				if err != nil || !bytes.Equal(got, contextRaw) {
					t.Fatal("foreign producer context changed", err)
				}
			}
			unknown := invoke([]string{"-action", "inspect", "-delivery", "missing-delivery"}, nil, 1)
			if string(unknown["feedback_status"]) == `"not_applicable"` {
				t.Fatal("unknown direction mislabeled as inbound")
			}
		})
	}
}
