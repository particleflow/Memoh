package core

import (
	"context"

	sdk "github.com/felinics/twilight/sdk"

	tools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/agent/toolexec"
	"github.com/felinics/memoh/internal/toolcontext"
)

type delegationToolScope struct{ tools.ToolProvider }

func (p delegationToolScope) ToolNotice(ctx context.Context, s tools.SessionContext) string {
	if notice, ok := p.ToolProvider.(tools.ToolNotice); ok {
		return notice.ToolNotice(ctx, s)
	}
	return ""
}

func delegationScopedTools(providers []tools.ToolProvider) []tools.ToolProvider {
	out := make([]tools.ToolProvider, 0, len(providers))
	for _, p := range providers {
		out = append(out, delegationToolScope{p})
	}
	return out
}

func (p delegationToolScope) Usage(ctx context.Context, s tools.SessionContext, a tools.AvailableTools) string {
	if usage, ok := p.ToolProvider.(tools.ToolUsage); ok {
		return usage.Usage(ctx, s, a)
	}
	return ""
}

func (p delegationToolScope) Tools(ctx context.Context, s tools.SessionContext) ([]toolexec.Tool, error) {
	if err := toolcontext.CheckDelegation(ctx); err != nil {
		return nil, err
	}
	list, err := p.ToolProvider.Tools(ctx, s)
	if err != nil {
		return nil, err
	}
	for i := range list {
		original, name := list[i].Execute, list[i].Name
		list[i].Execute = func(ctx *toolexec.ToolExecContext, input sdk.ToolArguments) (sdk.ToolOutput, error) {
			if err := toolcontext.CheckDelegation(ctx.Context); err != nil {
				return sdk.ToolOutput{}, err
			}
			permission := ""
			switch name {
			case "read", "list", "browser_observe", "computer_observe":
				permission = "workspace_read"
			case "write", "edit", "apply_patch":
				permission = "workspace_write"
			case "exec", "browser_action", "computer_action", "browser_remote_session":
				permission = "workspace_exec"
			}
			if permission != "" {
				var args struct {
					TargetID string `json:"target_id"`
				}
				_ = input.Unmarshal(&args)
				target := args.TargetID
				if target == "" {
					target = s.WorkspaceTargetID
				}
				if err := toolcontext.CheckDelegationTarget(ctx.Context, s.BotID, target, permission); err != nil {
					return sdk.ToolOutput{}, err
				}
			}
			return original(ctx, input)
		}
	}
	return list, nil
}
