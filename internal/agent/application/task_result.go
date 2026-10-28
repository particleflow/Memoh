package application

import (
	"context"
	"strings"

	"github.com/felinics/memoh/internal/agent/step"
	"github.com/felinics/memoh/internal/taskresult"
	sdk "github.com/felinics/twilight/sdk"
)

// SetTaskResultWake connects the existing terminal observer to host-owned
// durable result delivery. It neither owns a scheduler nor executes a task.
func (s *Service) SetTaskResultWake(wake func(context.Context)) { s.taskResultWake = wake }

func taskResultFinalStep(record *step.Record, messages []ModelMessage, mode stepCommitMode) bool {
	if record == nil || mode != stepCompleted || record.Deferred != nil || record.Result.FinishReason != sdk.FinishReasonStop || len(record.Result.ToolCalls) != 0 {
		return false
	}
	i := lastAssistantMessageIndex(messages)
	return i >= 0 && strings.TrimSpace(messages[i].TextContent()) != ""
}

// This annotation is written in the same fenced transaction as the visible
// final reply. Progress, empty output, interruption and run completion cannot
// manufacture it. The host rechecks authority immediately before this commit.
func withTaskResultReceipt(opts storeRoundOptions, receipt string, messages []ModelMessage) storeRoundOptions {
	i := lastAssistantMessageIndex(messages)
	if opts.MessageMetadataByIndex == nil {
		opts.MessageMetadataByIndex = map[int]map[string]any{}
	}
	if opts.MessageMetadataByIndex[i] == nil {
		opts.MessageMetadataByIndex[i] = map[string]any{}
	}
	opts.MessageMetadataByIndex[i][taskresult.MetadataKey] = receipt
	return opts
}
