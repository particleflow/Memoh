package sessiontask

import "github.com/felinics/memoh/internal/agenvo"

// The stored result is produced by #12's verified completion ingestion. This
// read model deliberately has no file/media/relay or operation-writing port.
type persistedPiFacts struct {
	Progress agenvo.BatchProgress `json:"progress"`
	Receipt  *agenvo.BatchReceipt `json:"receipt"`
	Result   *agenvo.PiJSONResult `json:"result"`
}

func projectPiFacts(t *Task, b *persistedPiFacts) {
	if b == nil || b.Progress.CompletionVersion != agenvo.BatchCompletionVersion || b.Receipt == nil || b.Receipt.Validate(b.Progress) != nil {
		return
	}
	if !b.Receipt.Started {
		t.Status, t.ProcessState, t.PiResult = "not_started", "not_started", "not_started"
		return
	}
	t.Status, t.ProcessState = "process_exited", "exited"
	if b.Result == nil || agenvo.ValidateBatchCompletionResult(*b.Result, b.Progress, *b.Receipt) != nil {
		return
	}
	t.PiResult = b.Result.Outcome
	if b.Result.Output != "" {
		t.ContentAvailable = true
		t.ResultSummary = &ResultSummary{Text: b.Result.Output, Truncated: b.Result.Truncated, Source: "pi_final_assistant"}
	}
}
