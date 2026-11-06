package application

import (
	"github.com/felinics/memoh/internal/agent/runtime/native"
	"github.com/felinics/memoh/internal/agent/sessionmode"
	"github.com/felinics/memoh/internal/inputsource"
	"github.com/felinics/memoh/internal/schedule"
)

func requestInputSource(req ChatRequest) inputsource.Source {
	if req.InputSource != nil {
		return req.InputSource.Scoped(req.BotID, req.ThreadID)
	}
	// These inputs must be resolved by their host ledger, never guessed from
	// the authorization user carried alongside them.
	if req.SessionType == sessionmode.Schedule || req.SessionType == sessionmode.Subagent || req.DelegationResultID != "" || req.TaskResultReceipt != "" || req.ShutdownResume || req.ReusePersistedUserMessage {
		return inputsource.Unattributed(req.BotID, req.ThreadID)
	}
	return (inputsource.Source{Kind: inputsource.User, BotID: req.BotID, SessionID: req.ThreadID, UserID: req.UserID, ChannelIdentityID: req.SourceChannelIdentityID, Channel: req.CurrentChannel}).Scoped(req.BotID, req.ThreadID)
}

func scheduleInputSource(bot string, payload schedule.TriggerPayload) inputsource.Source {
	return (inputsource.Source{Kind: inputsource.Schedule, BotID: bot, SessionID: payload.SessionID, TaskKind: "schedule", TaskID: payload.ID, OccurrenceID: payload.FireID}).Scoped(bot, payload.SessionID)
}

func sourceSystemSection(source inputsource.Source) native.SystemSection {
	return native.SystemSection{ID: "system.input_source", Kind: "runtime_context", Priority: 15, RetentionTier: "required", Text: source.Prompt()}
}
