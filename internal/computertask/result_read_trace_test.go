package computertask

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/mcp"
)

type readTraceOutcome struct {
	Error                   bool
	State, Phase, ErrorText string
	HasPiResult             bool
	ControlRuns, PiRuns     int
}

func runResultReadTrace(t *testing.T, mode string, logged bool) (readTraceOutcome, map[string]any) {
	t.Helper()
	f, service, ctx := newNativeTaskFixture(t)
	original, err := service.submit(ctx, f.binding, Submit{Kind: "herdr_pi_task", Task: "PRIVATE_PROMPT"})
	if err != nil {
		t.Fatal(err)
	}
	outputSize := 1500
	for range 5 {
		output := "PRIVATE_OUTPUT" + strings.Repeat("x", outputSize)
		f.batch.stdout = bytes.ReplaceAll(syntheticPiStream(f.batch.progress.PiSessionID, "stop"), []byte("42 中文 ✓"), []byte(output))
		f.batch.receipt.Stdout = batchFile("stdout.jsonl", f.batch.stdout)
		if mode == "parse" {
			f.batch.completionAttempt = "other"
		}
		f.batch.projectCompletion()
		if len(f.batch.completion) == 2269 {
			break
		}
		outputSize += 2269 - len(f.batch.completion)
	}
	if len(f.batch.completion) != 2269 {
		t.Fatalf("synthetic completion size=%d", len(f.batch.completion))
	}
	record, d, err := service.load(ctx, f.binding, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	receipt := f.batch.receipt
	d.Batch.Receipt = &receipt
	d.Batch.Files = []BatchTransfer{{Meta: receipt.Stdout}, {Meta: receipt.Stderr}}
	d.Phase = "pi_exited_result_pending"
	record.State = "completed"
	if mode == "reader_not_ready" {
		d.Batch.RecoveryReader = &agenvo.BatchRecoveryReader{Phase: "creating"}
	}
	if mode == "reader_save" {
		f.batch.changedPane = true
		revision := record.Revision
		f.store.failRevision = &revision
	}
	record.Details, _ = json.Marshal(d)
	f.store.records[record.ID] = record
	f.batch.pageOffsets = nil
	controlRuns := 0
	service.relay.SetNativeCaller(func(callCtx context.Context, scope agenvo.Scope, target agenvo.Target, method string, params map[string]any) (agenvo.Receipt, error) {
		if method == "pane.run" && params["paneId"] == "control" {
			controlRuns++
			if mode == "native_first" || mode == "native_second" && controlRuns == 2 {
				return agenvo.Receipt{}, agenvo.ErrUnknown
			}
		}
		value, err := f.invoke(callCtx, scope, target, method, params)
		if method == "pane.run" && params["paneId"] == "control" {
			if mode == "short_second" && controlRuns == 2 {
				f.batch.pending = regexp.MustCompile(`(MEMOH_FILE_[a-f0-9]{32}:)[^:\r\n]+(:END_FILE_[a-f0-9]{32})`).ReplaceAllString(f.batch.pending, "${1}eA==${2}")
			}
			if mode == "bad_base64" && controlRuns == 2 {
				f.batch.pending = regexp.MustCompile(`(MEMOH_FILE_[a-f0-9]{32}:)[^:\r\n]+(:END_FILE_[a-f0-9]{32})`).ReplaceAllString(f.batch.pending, "${1}!!!!${2}")
			}
			if mode == "frame_deadline" {
				f.batch.pending = ""
			}
		}
		return value, err
	})
	if mode == "frame_deadline" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
	}
	source := NewSource(service)
	var log bytes.Buffer
	if logged {
		source.SetLogger(slog.New(slog.NewJSONHandler(&log, nil)))
	}
	session := mcp.ToolSessionContext{BotID: f.binding.BotID, UserID: f.binding.UserID, SessionID: f.binding.SessionID, RunID: f.binding.RunID, ToolCallID: "synthetic-read-call", RuntimeActive: true, RunContext: ctx}
	response, err := source.CallTool(t.Context(), session, "computer_task_result", map[string]any{"operation_id": record.ID})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(response["structuredContent"])
	var envelope struct {
		Computer Result `json:"computer"`
		Error    string `json:"error"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Computer.ID != record.ID {
		t.Fatalf("retained computer response missing: %s", raw)
	}
	out := readTraceOutcome{Error: response["isError"] == true, State: envelope.Computer.State, Phase: envelope.Computer.Phase, ErrorText: envelope.Error, HasPiResult: envelope.Computer.PiResult != nil, ControlRuns: controlRuns, PiRuns: f.batch.runs}
	if mode == "success" {
		if out.Error || !out.HasPiResult || out.Phase != "pi_succeeded" || controlRuns != 2 {
			t.Fatalf("two-page result failed: %+v", out)
		}
	} else if !out.Error || out.HasPiResult || out.ErrorText != agenvo.ErrUnknown.Error() || out.Phase != "pi_exited_result_pending" {
		t.Fatalf("failure changed: %+v", out)
	}
	if f.batch.runs != 1 {
		t.Fatal("diagnostic replayed Pi")
	}
	if bytes.Contains(raw, []byte("read_stage")) || bytes.Contains(raw, []byte("completed_pages")) {
		t.Fatal("internal diagnostic entered response")
	}
	var fields map[string]any
	if logged && mode != "success" {
		if json.Unmarshal(log.Bytes(), &fields) != nil {
			t.Fatalf("expected one structured failure log: %s", log.Bytes())
		}
		for _, secret := range []string{"PRIVATE_PROMPT", "PRIVATE_OUTPUT", "PRIVATE-read-diagnostic", "C:\\Synthetic", "MEMOH_FILE_"} {
			if strings.Contains(log.String(), secret) {
				t.Fatalf("private data logged: %s", secret)
			}
		}
		for _, key := range []string{"command", "stdout", "stderr", "output", "frame", "base64", "nonce", "path", "error"} {
			if _, ok := fields[key]; ok {
				t.Fatalf("non-whitelisted private diagnostic field: %s", key)
			}
		}
		if fields["operation_id"] != record.ID || fields["run_id"] != f.binding.RunID || fields["operation_run_id"] != f.binding.RunID || fields["tool_call_id"] != "synthetic-read-call" {
			t.Fatal("failure log lost original call correlation")
		}
	} else if log.Len() != 0 {
		t.Fatal("unexpected diagnostic log")
	}
	return out, fields
}

func TestResultReadTracePreservesErrorsAndDispatch(t *testing.T) {
	for _, tc := range []struct {
		mode, stage, reason          string
		offset, count, actual, pages float64
	}{
		{"success", "", "", 0, 0, 0, 0},
		{"native_first", "completion_page", "native_error", 0, 2048, -1, 0},
		{"native_second", "completion_page", "native_error", 2048, 221, -1, 1},
		{"short_second", "completion_page", "short_page", 2048, 221, 1, 1},
		{"bad_base64", "completion_page", "invalid_base64", 2048, 221, 0, 1},
		{"parse", "completion_parse", "validation_failed", 2048, 221, 221, 2},
		{"reader_not_ready", "completion_page", "reader_not_ready", 0, 0, -1, 0},
		{"reader_save", "completion_page", "reader_save_failed", 0, 2048, -1, 0},
		{"frame_deadline", "completion_page", "context_ended_waiting_frame", 0, 2048, -1, 0},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			plain, _ := runResultReadTrace(t, tc.mode, false)
			traced, fields := runResultReadTrace(t, tc.mode, true)
			if !reflect.DeepEqual(plain, traced) {
				t.Fatalf("instrumentation changed behavior: plain=%+v traced=%+v", plain, traced)
			}
			if tc.mode == "success" {
				return
			}
			if fields["stage"] != tc.stage || fields["reason"] != tc.reason || fields["offset"] != tc.offset || fields["count"] != tc.count || fields["actual_bytes"] != tc.actual || fields["completed_pages"] != tc.pages {
				t.Fatalf("incorrect safe diagnostic: %+v", fields)
			}
			if tc.mode == "frame_deadline" && fields["deadline_exceeded"] != true {
				t.Fatal("parent deadline lost")
			}
			if tc.mode == "reader_save" && (fields["save_kind"] != "reader_reservation" || fields["save_error_class"] != "other" || fields["expected_revision"].(float64) < 1) {
				t.Fatal("reader CAS failure not distinguished")
			}
		})
	}
}
