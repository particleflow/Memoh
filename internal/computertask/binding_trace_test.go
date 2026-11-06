package computertask

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/mcp"
	"github.com/felinics/memoh/internal/toolcontext"
)

func TestComputerBindingDiagnosticsDoNotInventConnectivityOrLeakInput(t *testing.T) {
	for _, scenario := range []string{"missing", "mismatched_session", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			var output bytes.Buffer
			source := NewSource(nil)
			source.SetLogger(slog.New(slog.NewJSONHandler(&output, nil)))
			session := mcp.ToolSessionContext{BotID: "bot", UserID: "PRIVATE_USER", SessionID: "session", RunID: "synthetic-run", ToolCallID: "synthetic-call"}
			ctx := t.Context()
			if scenario == "mismatched_session" {
				var err error
				ctx, err = toolcontext.WithComputerBinding(ctx, toolcontext.ComputerBinding{BotID: "bot", UserID: "PRIVATE_USER", SessionID: "another-session", TargetID: "PRIVATE_TARGET", RunID: "synthetic-run"})
				if err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "canceled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			_, err := source.CallTool(ctx, session, "computer_task_submit", map[string]any{"task": "PRIVATE_PROMPT", "path": "PRIVATE_PATH"})
			if scenario == "canceled" {
				if !errors.Is(err, context.Canceled) || output.Len() != 0 {
					t.Fatal("cancellation misclassified as binding failure")
				}
				return
			}
			if !errors.Is(err, toolcontext.ErrComputerBinding) {
				t.Fatalf("binding error lost: %v", err)
			}
			var fields map[string]any
			if err = json.Unmarshal(output.Bytes(), &fields); err != nil {
				t.Fatal(err)
			}
			if fields["tool_call_id"] != "synthetic-call" || fields["run_id"] != "synthetic-run" || fields["binding_present"] != (scenario == "mismatched_session") || fields["session_matches"] != false {
				t.Fatalf("wrong diagnostic facts: %+v", fields)
			}
			for _, private := range []string{"PRIVATE_USER", "PRIVATE_TARGET", "PRIVATE_PROMPT", "PRIVATE_PATH", "offline", "protection"} {
				if strings.Contains(output.String(), private) {
					t.Fatal("private input or unverified diagnosis logged")
				}
			}
		})
	}
}
