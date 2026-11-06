package imagecapability

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	googleuuid "github.com/google/uuid"

	tools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/agent/toolexec"
	"github.com/felinics/memoh/internal/logger"
	"github.com/felinics/memoh/internal/toolcontext"
)

func TestImageRequestObservationSeparatesCancellationAndTimeoutFacts(t *testing.T) {
	for _, mode := range []string{"http504", "cancel", "cancel_with504", "deadline", "client_timeout", "body_timeout", "invalid200", "completed", "before_dispatch"} {
		t.Run(mode, func(t *testing.T) {
			var logs bytes.Buffer
			p, _ := fixture(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected real route") })
			p.SetLogger(logger.New(&logs, "info", "json"))
			resolve := p.resolve
			providerID := googleuuid.NewString()
			p.resolve = func(ctx context.Context, bot string) (selection, bool, error) {
				s, enabled, err := resolve(ctx, bot)
				s.providerID = providerID
				s.credentials = func(context.Context) (string, error) { return "PRIVATE_API_KEY", nil }
				return s, enabled, err
			}
			ctx := logger.ContextWithRequestID(t.Context(), "synthetic-source-request")
			runID := googleuuid.NewString()
			ctx, err := toolcontext.WithComputerBinding(ctx, toolcontext.ComputerBinding{
				BotID: "synthetic-bot", UserID: "synthetic-user", SessionID: "synthetic-session", TargetID: "synthetic-nas", RunID: runID,
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			if mode == "deadline" {
				var deadlineCancel context.CancelFunc
				ctx, deadlineCancel = context.WithTimeout(ctx, 200*time.Millisecond)
				defer deadlineCancel()
			}
			if mode == "client_timeout" || mode == "body_timeout" {
				p.http.Timeout = 20 * time.Millisecond
			}
			if mode == "before_dispatch" {
				cancel()
			}
			calls := 0
			outboundID := ""
			p.http.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				outboundID = r.Header.Get("X-Request-ID")
				if r.Header.Get("Authorization") != "Bearer PRIVATE_API_KEY" {
					t.Fatal("original provider credential changed")
				}
				if _, err := googleuuid.Parse(outboundID); err != nil || r.GetBody != nil {
					t.Fatal("missing generated request identity or replay body retained")
				}
				if mode == "cancel" {
					cancel()
					return nil, r.Context().Err()
				}
				if mode == "deadline" || mode == "client_timeout" {
					<-r.Context().Done()
					return nil, r.Context().Err()
				}
				if mode == "cancel_with504" {
					cancel()
				}
				status := http.StatusGatewayTimeout
				body := io.NopCloser(strings.NewReader("PRIVATE_RESPONSE"))
				switch mode {
				case "completed":
					status = http.StatusOK
					body = io.NopCloser(strings.NewReader(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(pngBytes(t, 5)) + `"}]}`))
				case "invalid200":
					status = http.StatusOK
				case "body_timeout":
					status = http.StatusOK
					body = canceledImageBody{ctx: r.Context()}
				}
				return &http.Response{StatusCode: status, Body: body, Header: http.Header{"X-Request-Id": []string{"PRIVATE_UPSTREAM_ID"}}, Request: r}, nil
			})
			session := tools.SessionContext{BotID: "synthetic-bot", UserID: "synthetic-user", SessionID: "synthetic-session"}
			result, executionErr := p.execute(&toolexec.ToolExecContext{Context: ctx, ToolName: "generate_image", ToolCallID: "synthetic-call"}, session, request{Prompt: "PRIVATE_PROMPT"}, nil)
			var entries []map[string]any
			for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
				var entry map[string]any
				if err := json.Unmarshal([]byte(line), &entry); err != nil {
					t.Fatal(err)
				}
				entries = append(entries, entry)
			}
			wantEntries := 2
			if mode == "before_dispatch" {
				wantEntries = 1
			}
			if len(entries) != wantEntries {
				t.Fatalf("wrong observation count: %d", len(entries))
			}
			finish := entries[len(entries)-1]
			if finish["msg"] != "image request finished" || finish["run_id"] != runID || finish["tool_call_id"] != "synthetic-call" || finish["request_id"] != "synthetic-source-request" {
				t.Fatal("missing original source/run/tool identity", finish)
			}
			if strings.Contains(logs.String(), "PRIVATE_") || strings.Contains(logs.String(), "/v1") {
				t.Fatal("private provider content entered diagnostic logs")
			}
			snapshot, err := json.Marshal(finish)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("synthetic observation: %s", snapshot)
			if mode == "before_dispatch" {
				if calls != 0 || executionErr == nil || finish["state"] != "not_dispatched" || finish["outbound_request_id"] != nil {
					t.Fatal("pre-dispatch cancellation pretends to have sent a request", finish)
				}
				return
			}
			if calls != 1 || executionErr != nil || finish["provider_id"] != providerID || finish["outbound_request_id"] != outboundID || entries[0]["outbound_request_id"] != outboundID {
				t.Fatal("request correlation or single dispatch changed", finish)
			}
			wantState := "unknown"
			if mode == "completed" {
				wantState = "completed"
			}
			if result.(map[string]any)["state"] != wantState || finish["state"] != wantState {
				t.Fatal("tool state changed or an unconfirmed image was shown as successful", result, finish)
			}
			if wantState == "unknown" && (result.(map[string]any)["retryable"] != false || result.(map[string]any)["content_hash"] != nil) {
				t.Fatal("unknown result gained retry or successful receipt", result)
			}
			requestContext, toolContext := "active", "active"
			switch mode {
			case "cancel", "cancel_with504":
				requestContext, toolContext = "canceled", "canceled"
			case "deadline":
				requestContext, toolContext = "deadline_exceeded", "deadline_exceeded"
			case "client_timeout", "body_timeout":
				requestContext = "deadline_exceeded"
			}
			if finish["request_context_state"] != requestContext || finish["tool_context_state"] != toolContext {
				t.Fatal("caller cancellation, local client timeout and provider status were conflated", finish)
			}
			if mode == "http504" || mode == "cancel_with504" {
				sum := sha256.Sum256([]byte("PRIVATE_UPSTREAM_ID"))
				if finish["http_status"] != float64(http.StatusGatewayTimeout) || finish["http_state"] != "response_received" || finish["provider_request_id_hash"] != hex.EncodeToString(sum[:]) {
					t.Fatal("504 response facts lost", finish)
				}
			}
		})
	}
}

type canceledImageBody struct{ ctx context.Context }

func (b canceledImageBody) Read([]byte) (int, error) { <-b.ctx.Done(); return 0, b.ctx.Err() }
func (canceledImageBody) Close() error               { return nil }

func TestImageObservationDoesNotInventMissingRunIdentity(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		var logs bytes.Buffer
		p := &Provider{}
		p.SetLogger(logger.New(&logs, "info", "json"))
		ctx := t.Context()
		if foreign {
			var err error
			ctx, err = toolcontext.WithComputerBinding(ctx, toolcontext.ComputerBinding{BotID: "other-bot", UserID: "other-user", SessionID: "other-session", TargetID: "other-target", RunID: "PRIVATE_FOREIGN_RUN"})
			if err != nil {
				t.Fatal(err)
			}
		}
		o := p.newRequestObservation(&toolexec.ToolExecContext{Context: ctx}, tools.SessionContext{BotID: "synthetic-bot", UserID: "synthetic-user", SessionID: "synthetic-session"})
		o.finish(ctx, nil, errors.New("PRIVATE_ERROR"))
		if strings.Contains(logs.String(), "run_id") || strings.Contains(logs.String(), "PRIVATE_") {
			t.Fatal("missing/foreign run identity or private error was invented/leaked")
		}
	}
}
