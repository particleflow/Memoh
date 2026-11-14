package imageworkflow_test

import (
	"context"
	"errors"
	"testing"

	"github.com/felinics/memoh/internal/imageworkflow"
)

// F8: the two layers the database owns are stated as contract facts.
func TestF8WorkflowLevelIsolationIsStructural(t *testing.T) {
	if imageworkflow.RoleInput != "input" || imageworkflow.RoleOutput != "output" {
		t.Fatalf("role vocabulary = %q/%q, want input/output", imageworkflow.RoleInput, imageworkflow.RoleOutput)
	}
	// A pairing graph edge carries both labels; the contract forbids an edge
	// whose ends are equal, which is what the table CHECK enforces.
	edge := imageworkflow.Edge{WorkflowID: contractWF, InputPairKey: "same", OutputPairKey: "same"}
	if err := imageworkflow.DetectCycle(nil, edge); !errors.Is(err, imageworkflow.ErrCycle) {
		t.Fatalf("DetectCycle for a self-referencing edge = %v, want ErrCycle", err)
	}
}

// F8: an edge that closes a longer cycle is detected.
func TestF8DetectCycleFindsAnIndirectCycle(t *testing.T) {
	edges := []imageworkflow.Edge{
		{WorkflowID: "a", InputPairKey: "in-a", OutputPairKey: "out-a"},
		{WorkflowID: "b", InputPairKey: "out-a", OutputPairKey: "out-b"},
	}
	candidate := imageworkflow.Edge{WorkflowID: "c", InputPairKey: "out-b", OutputPairKey: "in-a"}
	if err := imageworkflow.DetectCycle(edges, candidate); !errors.Is(err, imageworkflow.ErrCycle) {
		t.Fatalf("DetectCycle for in-a -> out-a -> out-b -> in-a = %v, want ErrCycle", err)
	}
}

// F8: an acyclic edge is accepted, including a shared input label.
func TestF8DetectCycleAcceptsAnAcyclicEdge(t *testing.T) {
	edges := []imageworkflow.Edge{
		{WorkflowID: "a", InputPairKey: "in-a", OutputPairKey: "out-a"},
	}
	for _, candidate := range []imageworkflow.Edge{
		{WorkflowID: "b", InputPairKey: "in-a", OutputPairKey: "out-b"},
		{WorkflowID: "c", InputPairKey: "out-a", OutputPairKey: "out-c"},
	} {
		if err := imageworkflow.DetectCycle(edges, candidate); err != nil {
			t.Fatalf("DetectCycle(%+v) = %v, want nil", candidate, err)
		}
	}
}

// F8: an update ignores the workflow's own previous edge, so re-saving unchanged
// labels is not a cycle against itself.
func TestF8DetectCycleIgnoresTheCandidatesOwnEdge(t *testing.T) {
	edges := []imageworkflow.Edge{
		{WorkflowID: "a", InputPairKey: "in-a", OutputPairKey: "out-a"},
	}
	candidate := imageworkflow.Edge{WorkflowID: "a", InputPairKey: "in-a", OutputPairKey: "out-a"}
	if err := imageworkflow.DetectCycle(edges, candidate); err != nil {
		t.Fatalf("DetectCycle re-checking an unchanged edge = %v, want nil", err)
	}
}

// F8: the service reads the owner's live edges and refuses a closing edge, and
// a revoked workflow's edge no longer constrains anything.
func TestF8CheckCreateCycleUsesLiveEdgesOnly(t *testing.T) {
	service, world := newContractService()
	live := world.SeedWorkflow("a", contractUser, contractBot, "first", imageworkflow.StateActive, "x")
	// A candidate consuming the live workflow's output and feeding its input
	// runs backwards through the live edge, which closes the pairing graph.
	closed := imageworkflow.Edge{WorkflowID: "b", InputPairKey: live.OutputPairKey, OutputPairKey: live.InputPairKey}
	if err := service.CheckCreateCycle(context.Background(), contractUser, closed); !errors.Is(err, imageworkflow.ErrCycle) {
		t.Fatalf("CheckCreateCycle with a live closing edge = %v, want ErrCycle", err)
	}

	revokedService, revokedWorld := newContractService()
	revoked := revokedWorld.SeedWorkflow("a", contractUser, contractBot, "first", imageworkflow.StateRevoked, "x")
	revokedEdge := imageworkflow.Edge{WorkflowID: "b", InputPairKey: revoked.OutputPairKey, OutputPairKey: revoked.InputPairKey}
	if err := revokedService.CheckCreateCycle(context.Background(), contractUser, revokedEdge); err != nil {
		t.Fatalf("CheckCreateCycle with only a revoked edge = %v, want nil", err)
	}
}

// F8: the fingerprint is order independent, so the same bytes share one run.
func TestF8FingerprintIsOrderIndependent(t *testing.T) {
	hashes := []string{testHash("a"), testHash("b"), testHash("c")}
	reversed := []string{hashes[2], hashes[0], hashes[1]}
	first, err := imageworkflow.Fingerprint(hashes)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	second, err := imageworkflow.Fingerprint(reversed)
	if err != nil {
		t.Fatalf("Fingerprint (reversed): %v", err)
	}
	if first != second {
		t.Fatalf("fingerprints differ for the same byte set: %q vs %q", first, second)
	}
	if len(first) != 64 {
		t.Fatalf("fingerprint length = %d, want a 64-character SHA-256 hex", len(first))
	}
	other, err := imageworkflow.Fingerprint([]string{testHash("a"), testHash("b")})
	if err != nil {
		t.Fatalf("Fingerprint (subset): %v", err)
	}
	if other == first {
		t.Fatal("a different file set produced the same fingerprint")
	}
}

// F8: an empty set has no fingerprint; the trigger refuses it as a bad request.
func TestF8FingerprintRejectsAnEmptySet(t *testing.T) {
	if _, err := imageworkflow.Fingerprint(nil); !errors.Is(err, imageworkflow.ErrInvalid) {
		t.Fatalf("Fingerprint(nil) = %v, want ErrInvalid", err)
	}
}

// F8: the run idempotency key is what makes a replay cheap, so the same batch
// can never produce two runs while it is still open.
func TestF8ReplayAfterACompletedRunIsStillTheOriginal(t *testing.T) {
	service, world := newContractService()
	world.SeedWorkflow(contractWF, contractUser, contractBot, "denoise", imageworkflow.StateActive, "x")
	world.SeedActivePairs(contractWF, contractBot)
	request := imageworkflow.TriggerRequest{
		Source: imageworkflow.SourceUpload,
		Files:  []imageworkflow.InputFile{{Path: "a.jpg", ContentHash: testHash("a"), SizeBytes: 10, Mime: "image/jpeg"}},
	}
	first, err := service.Trigger(context.Background(), contractUser, contractBot, contractWF, request)
	if err != nil {
		t.Fatalf("first Trigger: %v", err)
	}
	if _, err := world.SetRunState(context.Background(), first.RunID, imageworkflow.RunCompleted); err != nil {
		t.Fatalf("complete the run: %v", err)
	}
	second, err := service.Trigger(context.Background(), contractUser, contractBot, contractWF, request)
	if err != nil {
		t.Fatalf("replayed Trigger after completion: %v", err)
	}
	if second.RunID != first.RunID || !second.Duplicate {
		t.Fatalf("replay after completion = %+v, want the original run marked duplicate", second)
	}
	if got := len(world.Runs()); got != 1 {
		t.Fatalf("runs = %d, want 1", got)
	}
}

// F8: a second workflow watching the same input album has its own run identity,
// so sharing an input does not collapse two pipelines into one.
func TestF8SharedInputKeepsPerWorkflowRuns(t *testing.T) {
	service, world := newContractService()
	world.SeedWorkflow(contractWF, contractUser, contractBot, "first", imageworkflow.StateActive, "x")
	world.SeedWorkflow(otherWF, contractUser, contractBot, "second", imageworkflow.StateActive, "y")
	world.SeedActivePairs(contractWF, contractBot)
	world.SeedActivePairs(otherWF, contractBot)
	files := []imageworkflow.InputFile{{Path: "a.jpg", ContentHash: testHash("a"), SizeBytes: 10, Mime: "image/jpeg"}}

	first, err := service.Trigger(context.Background(), contractUser, contractBot, contractWF, imageworkflow.TriggerRequest{Source: imageworkflow.SourceUpload, Files: files})
	if err != nil {
		t.Fatalf("first workflow Trigger: %v", err)
	}
	second, err := service.Trigger(context.Background(), contractUser, contractBot, otherWF, imageworkflow.TriggerRequest{Source: imageworkflow.SourceUpload, Files: files})
	if err != nil {
		t.Fatalf("second workflow Trigger: %v", err)
	}
	if first.RunID == second.RunID {
		t.Fatalf("two workflows shared one run id: %s", first.RunID)
	}
	if second.Duplicate {
		t.Fatal("a different workflow's batch was reported as a duplicate")
	}
}
