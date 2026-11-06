package application

import (
	"encoding/json"
	"testing"

	sdk "github.com/felinics/twilight/sdk"

	"github.com/felinics/memoh/internal/agent/step"
	"github.com/felinics/memoh/internal/agent/turn"
	chatview "github.com/felinics/memoh/internal/agent/view"
	"github.com/felinics/memoh/internal/taskresult"
)

func TestTaskResultReceiptRequiresCompleteFinalReply(t *testing.T) {
	text := []ModelMessage{{Role: "assistant", Content: json.RawMessage(`"Saved task finished."`)}}
	for _, tc := range []struct {
		name     string
		reason   sdk.FinishReason
		mode     stepCommitMode
		messages []ModelMessage
		want     bool
	}{
		{"final", sdk.FinishReasonStop, stepCompleted, text, true},
		{"progress", sdk.FinishReasonToolCalls, stepCompleted, text, false},
		{"truncated", sdk.FinishReasonLength, stepCompleted, text, false},
		{"interrupted", sdk.FinishReasonStop, stepInterrupted, text, false},
		{"steered", sdk.FinishReasonStop, stepSteered, text, false},
		{"empty", sdk.FinishReasonStop, stepCompleted, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := taskResultFinalStep(&step.Record{Result: sdk.ModelResult{FinishReason: tc.reason}}, tc.messages, tc.mode); got != tc.want {
				t.Fatalf("receipt=%v", got)
			}
		})
	}
	opts := withTaskResultReceipt(storeRoundOptions{}, "receipt", text)
	if opts.MessageMetadataByIndex[0][taskresult.MetadataKey] != "receipt" {
		t.Fatal("final row missing receipt")
	}
}

func TestNativeTaskResultIsInternalAcrossQueueLiveAndResume(t *testing.T) {
	ref := taskresult.Reference{Kind: "native_task", TaskID: "bg_test", EventID: taskresult.EventID("unknown", "", "uncertain"), Attempt: 1}.String()
	cmd := turn.StartTurnCommand{TaskResultReceipt: ref, Query: "raw machine envelope", UserVisibleText: "raw display", UserID: "owner"}
	raw, err := encodeQueueCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if QueuePayloadText(raw) != "" || chatview.NewRequestUserTurn(cmd, "turn") != nil {
		t.Fatal("internal native result exposed")
	}
	if _, err = rewriteQueuePayloadText(raw, "owner", "tamper"); err == nil {
		t.Fatal("internal result editable")
	}
	restored := decodeQueuePayload(raw).Command
	if restored == nil || restored.TaskResultReceipt != ref {
		t.Fatal("queue lost origin")
	}
	q := &resumeSaveQueries{}
	s := &Service{queries: q, resumeSecret: "test"}
	if err = s.recordRunResumeContext(t.Context(), ChatRequest{RunID: "run", TaskResultReceipt: ref}); err != nil || len(q.saved.ResumeContext) != 0 {
		t.Fatal("summary must recover from durable result, not an old execution checkpoint")
	}
	req := chatRequestFromCommand(cmd)
	if botTaskResultMetadata(req)["task_result_receipt"] != ref {
		t.Fatal("history lost origin")
	}
}
