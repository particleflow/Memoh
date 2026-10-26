package imagecapability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"

	googleuuid "github.com/google/uuid"

	tools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/agent/toolexec"
	"github.com/felinics/memoh/internal/toolcontext"
)

// SetLogger installs the existing application logger; observations never own
// task state, change cancellation, or persist provider content.
func (p *Provider) SetLogger(log *slog.Logger) { p.logger = log }

type requestObservation struct {
	logger                    *slog.Logger
	requestID, providerID     string
	httpState, requestContext string
	providerRequestIDHash     string
	status                    int
}

func (p *Provider) newRequestObservation(exec *toolexec.ToolExecContext, session tools.SessionContext) *requestObservation {
	o := &requestObservation{httpState: "not_dispatched"}
	if p.logger == nil {
		return o
	}
	tool := "unknown"
	if exec.ToolName == "generate_image" || exec.ToolName == "edit_image" {
		tool = exec.ToolName
	}
	o.logger = p.logger.With(slog.String("component", "imagecapability"), slog.String("tool", tool),
		slog.String("bot_id", session.BotID), slog.String("session_id", session.SessionID),
		slog.String("tool_call_id", exec.ToolCallID))
	// This is the existing host-owned binding, not a guessed latest session run
	// or an identity supplied by the model. Missing/mismatched identity stays absent.
	if b, ok := toolcontext.HostComputerBinding(exec.Context); ok && b.BotID == session.BotID &&
		b.SessionID == session.SessionID && b.UserID == session.UserID && b.RunID != "" {
		o.logger = o.logger.With(slog.String("run_id", b.RunID))
	}
	return o
}

func (o *requestObservation) start(req *http.Request) {
	if o == nil {
		return
	}
	o.requestID = googleuuid.NewString()
	req.Header.Set("X-Request-ID", o.requestID)
	o.httpState = "dispatched"
	o.requestContext = "not_returned"
	if o.logger != nil {
		attrs := []slog.Attr{slog.String("outbound_request_id", o.requestID)}
		if o.providerID != "" {
			attrs = append(attrs, slog.String("provider_id", o.providerID))
		}
		o.logger.LogAttrs(req.Context(), slog.LevelInfo, "image request started", attrs...)
	}
}

func (o *requestObservation) returned(ctx context.Context, response *http.Response, err error) {
	if o == nil {
		return
	}
	o.requestContext = contextState(ctx)
	o.httpState = "response_received"
	if err != nil {
		o.httpState = "transport_error"
	}
	if response != nil {
		o.status = response.StatusCode
		// A provider controls its response header. Hash bounded identifiers so an
		// echoed credential, signed URL or diagnostic cannot enter our log verbatim.
		if id := response.Header.Get("X-Request-ID"); id != "" && len(id) <= 256 {
			sum := sha256.Sum256([]byte(id))
			o.providerRequestIDHash = hex.EncodeToString(sum[:])
		}
	}
}

func contextState(ctx context.Context) string {
	switch ctx.Err() {
	case context.Canceled:
		return "canceled"
	case context.DeadlineExceeded:
		return "deadline_exceeded"
	default:
		return "active"
	}
}

func (o *requestObservation) finish(ctx context.Context, output any, err error) {
	if o.logger == nil {
		return
	}
	state := "not_dispatched"
	if result, ok := output.(map[string]any); ok {
		state, _ = result["state"].(string)
	} else if o.requestID != "" {
		state = "unknown"
	}
	attrs := []slog.Attr{
		slog.String("state", state), slog.Bool("tool_error", err != nil),
		slog.String("http_state", o.httpState), slog.String("tool_context_state", contextState(ctx)),
	}
	if o.requestID != "" {
		attrs = append(attrs, slog.String("outbound_request_id", o.requestID),
			slog.String("request_context_state", o.requestContext))
	}
	if o.providerID != "" {
		attrs = append(attrs, slog.String("provider_id", o.providerID))
	}
	if o.status != 0 {
		attrs = append(attrs, slog.Int("http_status", o.status))
	}
	if o.providerRequestIDHash != "" {
		attrs = append(attrs, slog.String("provider_request_id_hash", o.providerRequestIDHash))
	}
	// A 504 is only a response status. Context facts stay separate and never
	// assert which cancellation/timeout happened first or whether an image exists.
	o.logger.LogAttrs(ctx, slog.LevelInfo, "image request finished", attrs...)
}
