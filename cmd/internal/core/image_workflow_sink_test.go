package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/albumwriteback"
	"github.com/felinics/memoh/internal/computertask"
	"github.com/felinics/memoh/internal/imageworkflow"
	"github.com/felinics/memoh/internal/imageworkflow/imageworkflowtest"
	"github.com/felinics/memoh/internal/toolcontext"
)

const (
	workflowSinkUser = "11111111-1111-1111-1111-111111111111"
	workflowSinkBot  = "22222222-2222-2222-2222-222222222222"
	workflowSinkWF   = "33333333-3333-3333-3333-333333333333"
)

func workflowSinkHash(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

// F6: a delivery made from a workflow run's session queues the product into the
// workflow's output pairing, not the bare target.
func TestF6ImageWorkflowSinkQueuesIntoTheWorkflowOutputPair(t *testing.T) {
	world := imageworkflowtest.New()
	world.SeedWorkflow(workflowSinkWF, workflowSinkUser, workflowSinkBot, "denoise", imageworkflow.StateActive, "x")
	world.SeedActivePairs(workflowSinkWF, workflowSinkBot)
	world.SeedBarePair(workflowSinkUser, workflowSinkBot, "bare-output")

	workflows := imageworkflow.NewService(nil, world.Deps())
	writebacks := &sinkWritebackStore{}
	sink := ImageWorkflowDeliveryCommitSink(slog.New(slog.DiscardHandler), workflows, albumwriteback.NewService(nil, writebacks, nil))
	if sink == nil {
		t.Fatal("sink is nil")
	}

	run, err := workflows.Trigger(context.Background(), workflowSinkUser, workflowSinkBot, workflowSinkWF, imageworkflow.TriggerRequest{
		Source: imageworkflow.SourceUpload,
		Files:  []imageworkflow.InputFile{{Path: "a.jpg", ContentHash: workflowSinkHash("a"), SizeBytes: 10, Mime: "image/jpeg"}},
	})
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	const runSession = "44444444-4444-4444-4444-444444444444"
	world.SeedRunSession(run.RunID, runSession)

	product := workflowSinkHash("product")
	sink(context.Background(), computertask.DeliveryIdentity{
		Binding: toolcontext.ComputerBinding{
			BotID: workflowSinkBot, UserID: workflowSinkUser, SessionID: runSession,
		},
		OperationID: "operation",
	}, computertask.DeliveryRequest{Version: 1, Direction: "upload", ContentHash: product, SizeBytes: 10, Mime: "image/jpeg"})

	queued := writebacks.rows
	if len(queued) != 1 {
		t.Fatalf("queued rows = %d, want 1", len(queued))
	}
	if !strings.HasPrefix(queued[0].PairKey, "wf-"+workflowSinkWF+"-out") {
		t.Fatalf("queued pair = %q, want the workflow output label", queued[0].PairKey)
	}
}

// F6: a delivery with no lineage keeps today's behavior and uses the bare
// target.
func TestF6ImageWorkflowSinkFallsBackToTheBareTarget(t *testing.T) {
	world := imageworkflowtest.New()
	world.SeedBarePair(workflowSinkUser, workflowSinkBot, "bare-output")
	workflows := imageworkflow.NewService(nil, world.Deps())
	writebacks := &sinkWritebackStore{}
	sink := ImageWorkflowDeliveryCommitSink(slog.New(slog.DiscardHandler), workflows, albumwriteback.NewService(nil, writebacks, nil))

	sink(context.Background(), computertask.DeliveryIdentity{
		Binding: toolcontext.ComputerBinding{
			BotID: workflowSinkBot, UserID: workflowSinkUser, SessionID: "55555555-5555-5555-5555-555555555555",
		},
		OperationID: "operation",
	}, computertask.DeliveryRequest{Version: 1, Direction: "upload", ContentHash: workflowSinkHash("product"), SizeBytes: 10, Mime: "image/jpeg"})

	if len(writebacks.rows) != 1 || writebacks.rows[0].PairKey != "bare-output" {
		t.Fatalf("queued rows = %+v, want one bare-output row", writebacks.rows)
	}
}

// F6: a download delivery is never a product.
func TestF6ImageWorkflowSinkIgnoresDownloads(t *testing.T) {
	world := imageworkflowtest.New()
	world.SeedBarePair(workflowSinkUser, workflowSinkBot, "bare-output")
	workflows := imageworkflow.NewService(nil, world.Deps())
	writebacks := &sinkWritebackStore{}
	sink := ImageWorkflowDeliveryCommitSink(slog.New(slog.DiscardHandler), workflows, albumwriteback.NewService(nil, writebacks, nil))

	sink(context.Background(), computertask.DeliveryIdentity{
		Binding: toolcontext.ComputerBinding{
			BotID: workflowSinkBot, UserID: workflowSinkUser, SessionID: "55555555-5555-5555-5555-555555555555",
		},
		OperationID: "operation",
	}, computertask.DeliveryRequest{Version: 1, Direction: "download", ContentHash: workflowSinkHash("product"), SizeBytes: 10, Mime: "image/jpeg"})

	if len(writebacks.rows) != 0 {
		t.Fatalf("a download queued %d rows", len(writebacks.rows))
	}
}
