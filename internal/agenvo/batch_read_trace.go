package agenvo

import "context"

// BatchReadTrace is call-local diagnostic metadata, never a task receipt or API
// payload. The synchronous reader records only fixed enums and byte counters;
// commands, paths, frames, file contents and native error messages stay out.
type BatchReadTrace struct {
	Stage, ReadStage, Reason, Method, FileKind string
	OperationRunID                             string
	Offset, Count, ActualBytes, CompletedPages int64
	FeedbackElapsedMS, DeliveryWakeElapsedMS   int64
	FeedbackFailed, DeliveryWakeFailed         bool
	ReadElapsedMS                              int64
	DeadlineExceeded, Canceled                 bool
	SaveKind, SaveErrorClass                   string
	ExpectedRevision                           int64
}

type batchReadTraceKey struct{}

func WithBatchReadTrace(ctx context.Context, trace *BatchReadTrace) context.Context {
	return context.WithValue(ctx, batchReadTraceKey{}, trace)
}

func BatchReadTraceFromContext(ctx context.Context) *BatchReadTrace {
	trace, _ := ctx.Value(batchReadTraceKey{}).(*BatchReadTrace)
	return trace
}

func traceBatchReadFailure(ctx context.Context, stage, reason string, err error) error {
	if trace := BatchReadTraceFromContext(ctx); trace != nil {
		trace.ReadStage, trace.Reason = stage, reason
	}
	return err // preserve the exact existing error, including sentinel identity
}

func traceBatchReadMethod(ctx context.Context, method string) {
	if trace := BatchReadTraceFromContext(ctx); trace != nil {
		trace.Method = method
	}
}
