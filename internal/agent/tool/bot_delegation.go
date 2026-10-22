package tools

import (
	"context"
	"errors"

	sdk "github.com/felinics/twilight/sdk"
	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agent/toolexec"
	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/bots"
)

type delegationBots interface {
	ListAccessible(context.Context, string) ([]bots.Bot, error)
}

// BotDelegationProvider invokes the ordinary target Bot through the host
// service. Identity comes from the authenticated session, never tool arguments.
type BotDelegationProvider struct {
	service *botdelegation.Service
	bots    delegationBots
}

func NewBotDelegationProvider(service *botdelegation.Service, bots delegationBots) *BotDelegationProvider {
	return &BotDelegationProvider{service: service, bots: bots}
}

func (*BotDelegationProvider) Usage(context.Context, SessionContext, AvailableTools) string {
	return "Use list_delegate_bots to discover existing specialized Bots available to this user. delegate_to_bot starts one asynchronous handoff with optional existing conversation content_hash references; accepted/running is not completion. Continue other work while the specialist's final result and original references are returned to this conversation. get_bot_delegation only reads durable state. An unknown execution must not be resubmitted as a new task; retry_bot_delivery retries only delivery of a known finished result. Existing device restrictions remain in effect."
}

func (p *BotDelegationProvider) Tools(_ context.Context, s SessionContext) ([]toolexec.Tool, error) {
	if p.service == nil || p.bots == nil || s.BotID == "" || s.UserID == "" || s.SessionID == "" {
		return nil, nil
	}
	return p.definitions(s), nil
}

type delegateBotArgs struct {
	TargetBotID string                          `json:"target_bot_id" jsonschema:"Existing authorized Bot ID from list_delegate_bots."`
	Message     string                          `json:"message" jsonschema:"Task for the specialist. Do not embed file bytes or credentials."`
	Attachments []botdelegation.AttachmentInput `json:"attachments,omitempty" jsonschema:"Existing source conversation content_hash references; original bytes remain in NAS media storage."`
}
type delegationIDArgs struct {
	DelegationID string `json:"delegation_id"`
}
type listDelegateBotsArgs struct{}

func (p *BotDelegationProvider) definitions(s SessionContext) []toolexec.Tool {
	return []toolexec.Tool{
		{Name: ToolListDelegateBots().String(), Description: "List existing specialized Bots that this user may delegate to from this conversation. No execution or device probing.", Parameters: toolexec.SchemaFor[listDelegateBotsArgs](), Execute: toolexec.Typed(func(ctx *toolexec.ToolExecContext, _ listDelegateBotsArgs) (sdk.ToolOutput, error) {
			if _, err := p.service.Authority.Source(ctx.Context, s.UserID, s.BotID, s.SessionID); err != nil {
				return sdk.ToolOutput{}, delegationToolError(err)
			}
			items, err := p.bots.ListAccessible(ctx.Context, s.UserID)
			if err != nil {
				return sdk.ToolOutput{}, delegationToolError(err)
			}
			out := []map[string]string{}
			for _, item := range items {
				if _, _, err := p.service.Authority.Bind(ctx.Context, s.UserID, s.BotID, s.SessionID, item.ID); err != nil {
					continue
				}
				out = append(out, map[string]string{"bot_id": item.ID, "name": item.Name, "display_name": item.DisplayName})
			}
			return toolexec.OutputFromValue(map[string]any{"bots": out}), nil
		})},
		{Name: ToolDelegateToBot().String(), Description: "Delegate one task to an existing specialized Bot using the current user's permissions. Returns a durable correlation ID immediately. Final summary and actual returned attachment references arrive asynchronously; acceptance is not business completion.", Parameters: toolexec.SchemaFor[delegateBotArgs](), Execute: toolexec.Typed(func(ctx *toolexec.ToolExecContext, args delegateBotArgs) (sdk.ToolOutput, error) {
			if ctx.ToolCallID == "" {
				return sdk.ToolOutput{}, delegationToolError(botdelegation.ErrInput)
			}
			id := uuid.NewSHA1(uuid.NameSpaceURL, []byte("memoh/delegate/v1\x00"+s.BotID+"\x00"+s.UserID+"\x00"+s.SessionID+"\x00"+ctx.ToolCallID)).String()
			v, err := p.service.Submit(ctx.Context, s.BotID, s.UserID, s.SessionID, botdelegation.Input{RequestID: id, TargetBotID: args.TargetBotID, Message: args.Message, Attachments: args.Attachments})
			if err != nil {
				return sdk.ToolOutput{}, delegationToolError(err)
			}
			return toolexec.OutputFromValue(v), nil
		})},
		{Name: ToolGetBotDelegation().String(), Description: "Read one delegated task's current persisted state and safe final summary. This never dispatches or polls devices.", Parameters: toolexec.SchemaFor[delegationIDArgs](), Execute: toolexec.Typed(func(ctx *toolexec.ToolExecContext, args delegationIDArgs) (sdk.ToolOutput, error) {
			v, err := p.service.Get(ctx.Context, s.BotID, s.UserID, s.SessionID, args.DelegationID)
			if err != nil {
				return sdk.ToolOutput{}, delegationToolError(err)
			}
			return toolexec.OutputFromValue(v), nil
		})},
		{Name: ToolRetryBotDelivery().String(), Description: "Retry only delivery of a completed or failed specialist result to this conversation. Uses the original queue identity and never re-executes specialist work. Unknown execution is not retryable here.", Parameters: toolexec.SchemaFor[delegationIDArgs](), Execute: toolexec.Typed(func(ctx *toolexec.ToolExecContext, args delegationIDArgs) (sdk.ToolOutput, error) {
			v, err := p.service.RetryDelivery(ctx.Context, s.BotID, s.UserID, s.SessionID, args.DelegationID)
			if err != nil {
				return sdk.ToolOutput{}, delegationToolError(err)
			}
			return toolexec.OutputFromValue(v), nil
		})},
	}
}

func delegationToolError(err error) error {
	switch {
	case errors.Is(err, botdelegation.ErrDenied):
		return apperror.New(apperror.CodeSessionNotFound, nil)
	case errors.Is(err, botdelegation.ErrConflict):
		return apperror.New(apperror.CodeHTTPConflict, nil)
	case errors.Is(err, botdelegation.ErrInput):
		return apperror.New(apperror.CodeHTTPBadRequest, nil)
	default:
		return apperror.Wrap(apperror.CodeInternal, err, nil)
	}
}
