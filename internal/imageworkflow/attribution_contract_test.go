package imageworkflow_test

import (
	"context"
	"errors"
	"testing"

	"github.com/felinics/memoh/internal/imageworkflow"
)

// F6: a delivery session that a run owns resolves the workflow's output label.
func TestF6AttributionFollowsARunSession(t *testing.T) {
	service, world := newContractService()
	workflow := world.SeedWorkflow(contractWF, contractUser, contractBot, "denoise", imageworkflow.StateActive, "x")
	world.SeedActivePairs(contractWF, contractBot)
	world.SeedBarePair(contractUser, contractBot, "bare-output")

	run, err := service.Trigger(context.Background(), contractUser, contractBot, contractWF, imageworkflow.TriggerRequest{
		Source: imageworkflow.SourceUpload,
		Files:  []imageworkflow.InputFile{{Path: "a.jpg", ContentHash: testHash("a"), SizeBytes: 10, Mime: "image/jpeg"}},
	})
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	const runSession = "66666666-6666-6666-6666-666666666666"
	world.SeedRunSession(run.RunID, runSession)

	pairKey, err := service.ResolveOutputPair(context.Background(), contractUser, contractBot, runSession)
	if err != nil {
		t.Fatalf("ResolveOutputPair: %v", err)
	}
	if pairKey != workflow.OutputPairKey {
		t.Fatalf("attributed pair = %q, want the workflow output label %q", pairKey, workflow.OutputPairKey)
	}
}

// F6: a sub-agent's session is attributed through its parent chain, which is
// what a spawned specialist's delivery carries.
func TestF6AttributionWalksTheParentChain(t *testing.T) {
	service, world := newContractService()
	workflow := world.SeedWorkflow(contractWF, contractUser, contractBot, "denoise", imageworkflow.StateActive, "x")
	world.SeedActivePairs(contractWF, contractBot)
	world.SeedBarePair(contractUser, contractBot, "bare-output")

	run, err := service.Trigger(context.Background(), contractUser, contractBot, contractWF, imageworkflow.TriggerRequest{
		Source: imageworkflow.SourceUpload,
		Files:  []imageworkflow.InputFile{{Path: "a.jpg", ContentHash: testHash("a"), SizeBytes: 10, Mime: "image/jpeg"}},
	})
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	const workflowSession = "77777777-7777-7777-7777-777777777777"
	const childSession = "88888888-8888-8888-8888-888888888888"
	const grandchildSession = "99999999-9999-9999-9999-999999999999"
	world.SeedRunSession(run.RunID, workflowSession)
	world.SeedParentSession(childSession, workflowSession)
	world.SeedParentSession(grandchildSession, childSession)

	pairKey, err := service.ResolveOutputPair(context.Background(), contractUser, contractBot, grandchildSession)
	if err != nil {
		t.Fatalf("ResolveOutputPair through the chain: %v", err)
	}
	if pairKey != workflow.OutputPairKey {
		t.Fatalf("attributed pair = %q, want %q", pairKey, workflow.OutputPairKey)
	}
}

// F6: no lineage falls back to the bare target, so existing pairings keep
// working.
func TestF6AttributionFallsBackToTheBareTarget(t *testing.T) {
	service, world := newContractService()
	world.SeedBarePair(contractUser, contractBot, "bare-output")
	pairKey, err := service.ResolveOutputPair(context.Background(), contractUser, contractBot, "session-without-lineage")
	if err != nil {
		t.Fatalf("ResolveOutputPair without lineage: %v", err)
	}
	if pairKey != "bare-output" {
		t.Fatalf("fallback pair = %q, want bare-output", pairKey)
	}
}

// F6: without lineage and without a bare target, nothing is queued.
func TestF6AttributionWithoutLineageOrBareTargetQueuesNothing(t *testing.T) {
	service, _ := newContractService()
	_, err := service.ResolveOutputPair(context.Background(), contractUser, contractBot, "orphan-session")
	if !errors.Is(err, imageworkflow.ErrPairUnavailable) {
		t.Fatalf("ResolveOutputPair with neither lineage nor bare target = %v, want ErrPairUnavailable", err)
	}
}

// F6: a revoked workflow resolves no output pairing.
func TestF6AttributionSkipsARevokedWorkflow(t *testing.T) {
	service, world := newContractService()
	world.SeedWorkflow(contractWF, contractUser, contractBot, "denoise", imageworkflow.StateActive, "x")
	world.SeedActivePairs(contractWF, contractBot)
	run, err := service.Trigger(context.Background(), contractUser, contractBot, contractWF, imageworkflow.TriggerRequest{
		Source: imageworkflow.SourceUpload,
		Files:  []imageworkflow.InputFile{{Path: "a.jpg", ContentHash: testHash("a"), SizeBytes: 10, Mime: "image/jpeg"}},
	})
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	const runSession = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	world.SeedRunSession(run.RunID, runSession)
	if _, err := service.Revoke(context.Background(), contractUser, contractWF); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := service.ResolveOutputPair(context.Background(), contractUser, contractBot, runSession); !errors.Is(err, imageworkflow.ErrPairUnavailable) {
		t.Fatalf("ResolveOutputPair for a revoked workflow = %v, want ErrPairUnavailable", err)
	}
}

// F6: the parent walk is bounded, so a corrupted cycle cannot hang attribution.
func TestF6AttributionStopsAtTheDepthBound(t *testing.T) {
	service, world := newContractService()
	session := "session-0"
	for i := 0; i < imageworkflow.MaxLineageDepth*3; i++ {
		next := "session-" + itoa(i+1)
		world.SeedParentSession(next, session)
		session = next
	}
	// A cycle: the deepest session points back at the first one.
	world.SeedParentSession("session-0", session)
	_, err := service.ResolveOutputPair(context.Background(), contractUser, contractBot, session)
	if !errors.Is(err, imageworkflow.ErrPairUnavailable) {
		t.Fatalf("ResolveOutputPair over an over-long chain = %v, want ErrPairUnavailable", err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
