package imagecapability

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	tools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/agent/toolexec"
)

func TestCancellationBeforeAndAfterDispatch(t *testing.T) {
	for _, before := range []bool{true, false} {
		name := "after_dispatch"
		if before {
			name = "before_dispatch"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var calls atomic.Int32
			p, _ := fixture(t, func(_ http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				cancel()
				select {
				case <-r.Context().Done():
				case <-time.After(time.Second):
					t.Error("HTTP request did not observe cancellation")
				}
			})
			if before {
				cancel()
			}
			session := tools.SessionContext{BotID: "synthetic-bot", UserID: "synthetic-user", SessionID: "synthetic-session"}
			result, err := p.execute(&toolexec.ToolExecContext{Context: ctx, ToolCallID: "synthetic-cancel"}, session, request{Prompt: "synthetic"}, nil)
			if !errors.Is(ctx.Err(), context.Canceled) {
				t.Fatal("test did not cancel the request context")
			}
			if before {
				if err == nil || calls.Load() != 0 {
					t.Fatalf("pre-dispatch cancellation: calls=%d err=%v", calls.Load(), err)
				}
				return
			}
			out, ok := result.(map[string]any)
			if err != nil || !ok || calls.Load() != 1 || out["state"] != "unknown" || out["retryable"] != false || out["tool_call_id"] != "synthetic-cancel" {
				t.Fatalf("post-dispatch cancellation: calls=%d result=%v err=%v", calls.Load(), result, err)
			}
		})
	}
}
