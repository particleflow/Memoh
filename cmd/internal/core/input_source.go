package core

import (
	"context"
	"strings"

	tools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/botdelegation"
	messagepkg "github.com/felinics/memoh/internal/chat/message"
	"github.com/felinics/memoh/internal/inputsource"
)

// Decorate the existing host ports rather than changing the Agent or spawn
// runtime. Queue restoration supplies a fresh, authorized child-scoped source.
type sourceSpawnAgent struct{ tools.SpawnAgent }

func sourceSpawnConfig(ctx context.Context, cfg tools.SpawnRunConfig) tools.SpawnRunConfig {
	source, _ := inputsource.FromContext(ctx, cfg.Identity.BotID, cfg.Identity.SessionID)
	cfg.System += "\n\n" + source.Prompt()
	return cfg
}

func (a sourceSpawnAgent) Generate(ctx context.Context, cfg tools.SpawnRunConfig) (*tools.SpawnResult, error) {
	return a.SpawnAgent.Generate(ctx, sourceSpawnConfig(ctx, cfg))
}

func (a sourceSpawnAgent) GenerateWithWatchdog(ctx context.Context, cfg tools.SpawnRunConfig, touch func()) (*tools.SpawnResult, error) {
	return a.SpawnAgent.GenerateWithWatchdog(ctx, sourceSpawnConfig(ctx, cfg), touch)
}

type sourceSpawnMessages struct{ messagepkg.Service }

func (s sourceSpawnMessages) Persist(ctx context.Context, in messagepkg.PersistInput) (messagepkg.Message, error) {
	source, _ := inputsource.FromContext(ctx, in.BotID, in.SessionID)
	meta := make(map[string]any, len(in.Metadata)+1)
	for key, value := range in.Metadata {
		meta[key] = value
	}
	meta[inputsource.MetadataKey] = source
	in.Metadata = meta
	return s.Service.Persist(ctx, in)
}

type sourceSubagentAdmitter struct {
	tools.SubagentAdmitter
	queue *nativeTaskQueueHost
}

// Keep the existing optional checkpoint port visible through the decorator.
func (a sourceSubagentAdmitter) RecordSubagentResume(ctx context.Context, session tools.SessionContext, child, query, model string) error {
	if recorder, ok := a.SubagentAdmitter.(interface {
		RecordSubagentResume(context.Context, tools.SessionContext, string, string, string) error
	}); ok {
		return recorder.RecordSubagentResume(ctx, session, child, query, model)
	}
	return nil
}

func (a sourceSubagentAdmitter) AdmitSubagentRun(ctx context.Context, bot, session, invocation string, submission []byte) (context.Context, tools.SubagentAdmission, func(tools.SubagentTerminal), error) {
	source := inputsource.Unattributed(bot, session)
	if strings.HasPrefix(invocation, "subagent:") {
		raw, err := a.queue.host.store.FindNativeTask(ctx, strings.TrimPrefix(invocation, "subagent:"))
		if err != nil {
			return nil, tools.SubagentAdmission{}, nil, err
		}
		r, err := decodeNativeTask(raw)
		if err != nil || r.BotID != bot || r.SessionID != session {
			return nil, tools.SubagentAdmission{}, nil, botdelegation.ErrDenied
		}
		verified, _, err := a.queue.RestoreSession(ctx, r)
		if err != nil {
			return nil, tools.SubagentAdmission{}, nil, err
		}
		source, _ = inputsource.FromContext(verified, bot, session)
	}
	ctx, admission, finish, err := a.SubagentAdmitter.AdmitSubagentRun(inputsource.WithContext(ctx, source), bot, session, invocation, submission)
	if err == nil {
		source.TaskRunID = admission.RunID
		ctx = inputsource.WithContext(ctx, source)
	}
	return ctx, admission, finish, err
}
