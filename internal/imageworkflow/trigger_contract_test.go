package imageworkflow_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/imageworkflow"
)

// F4: an active workflow with both albums designated arms one one-shot
// schedule whose command carries the instruction verbatim.
func TestF4TriggerArmsOneOneShotScheduleWithTheInstruction(t *testing.T) {
	service, world := newContractService()
	workflow := world.SeedWorkflow(contractWF, contractUser, contractBot, "denoise", imageworkflow.StateActive, "sharpen then denoise")
	world.SeedActivePairs(contractWF, contractBot)
	world.AddAsset(contractBot, testHash("a"), 10, "image/jpeg")

	result, err := service.Trigger(context.Background(), contractUser, contractBot, contractWF, imageworkflow.TriggerRequest{
		Source: imageworkflow.SourceUpload,
		Files:  []imageworkflow.InputFile{{Path: "a.jpg", ContentHash: testHash("a"), SizeBytes: 10, Mime: "image/jpeg"}},
	})
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if result.RunID == "" || result.ScheduleID == "" {
		t.Fatalf("trigger result = %+v, want a run and a schedule", result)
	}
	if result.Duplicate {
		t.Fatal("the first trigger reported duplicate=true")
	}
	if result.State != imageworkflow.RunArmed {
		t.Fatalf("first trigger state = %q, want %q", result.State, imageworkflow.RunArmed)
	}
	schedules := world.Schedules()
	if len(schedules) != 1 {
		t.Fatalf("schedules created = %d, want 1", len(schedules))
	}
	request := schedules[0].Req
	if request.Pattern != "@every 5s" || request.MaxCalls != 1 {
		t.Fatalf("schedule pattern/max_calls = %q/%d, want '@every 5s'/1", request.Pattern, request.MaxCalls)
	}
	if request.RunTarget != "new_session" {
		t.Fatalf("schedule run target = %q, want new_session", request.RunTarget)
	}
	if !strings.Contains(request.Command, "sharpen then denoise") {
		t.Fatalf("schedule command does not carry the instruction verbatim:\n%s", request.Command)
	}
	if !strings.Contains(request.Command, testHash("a")) {
		t.Fatalf("schedule command does not name the input hash:\n%s", request.Command)
	}
	if !strings.Contains(request.Command, workflow.Name) {
		t.Fatalf("schedule command does not name the workflow:\n%s", request.Command)
	}
	runs := world.Runs()
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	if runs[0].InstructionSnapshot != "sharpen then denoise" {
		t.Fatalf("run instruction snapshot = %q", runs[0].InstructionSnapshot)
	}
	if runs[0].Fingerprint == "" || len(runs[0].Files) != 1 {
		t.Fatalf("run row = %+v", runs[0])
	}
}

// F4: a repeated batch identity returns the original run and creates no second
// schedule.
func TestF4RepeatedTriggerReturnsTheOriginalRun(t *testing.T) {
	service, world := newContractService()
	world.SeedWorkflow(contractWF, contractUser, contractBot, "denoise", imageworkflow.StateActive, "x")
	world.SeedActivePairs(contractWF, contractBot)
	world.AddAsset(contractBot, testHash("a"), 10, "image/jpeg")
	request := imageworkflow.TriggerRequest{
		Source: imageworkflow.SourceUpload,
		Files:  []imageworkflow.InputFile{{Path: "a.jpg", ContentHash: testHash("a"), SizeBytes: 10, Mime: "image/jpeg"}},
	}
	first, err := service.Trigger(context.Background(), contractUser, contractBot, contractWF, request)
	if err != nil {
		t.Fatalf("first Trigger: %v", err)
	}
	second, err := service.Trigger(context.Background(), contractUser, contractBot, contractWF, request)
	if err != nil {
		t.Fatalf("replayed Trigger: %v", err)
	}
	if second.RunID != first.RunID {
		t.Fatalf("replayed trigger made a new run: %s -> %s", first.RunID, second.RunID)
	}
	if !second.Duplicate {
		t.Fatal("replayed trigger did not report duplicate=true")
	}
	if got := len(world.Runs()); got != 1 {
		t.Fatalf("runs after a replay = %d, want 1", got)
	}
	if got := len(world.Schedules()); got != 1 {
		t.Fatalf("schedules after a replay = %d, want 1", got)
	}
}

// F4: a paused workflow stores the batch as held and creates no schedule.
func TestF4PausedWorkflowHoldsTheBatch(t *testing.T) {
	service, world := newContractService()
	world.SeedWorkflow(contractWF, contractUser, contractBot, "denoise", imageworkflow.StatePaused, "x")
	world.SeedActivePairs(contractWF, contractBot)
	world.AddAsset(contractBot, testHash("a"), 10, "image/jpeg")
	result, err := service.Trigger(context.Background(), contractUser, contractBot, contractWF, imageworkflow.TriggerRequest{
		Source: imageworkflow.SourceUpload,
		Files:  []imageworkflow.InputFile{{Path: "a.jpg", ContentHash: testHash("a"), SizeBytes: 10, Mime: "image/jpeg"}},
	})
	if err != nil {
		t.Fatalf("Trigger while paused = %v, want a held run and no error", err)
	}
	if result.State != imageworkflow.RunHeld {
		t.Fatalf("held state = %q, want %q", result.State, imageworkflow.RunHeld)
	}
	if result.ScheduleID != "" {
		t.Fatalf("a held run created a schedule: %s", result.ScheduleID)
	}
	if got := len(world.Schedules()); got != 0 {
		t.Fatalf("schedules after a held trigger = %d, want 0", got)
	}
	if got := len(world.Runs()); got != 1 {
		t.Fatalf("runs after a held trigger = %d, want 1", got)
	}
}

// F4: a revoked workflow refuses the trigger and stores nothing.
func TestF4RevokedWorkflowRefusesTheTrigger(t *testing.T) {
	service, world := newContractService()
	world.SeedWorkflow(contractWF, contractUser, contractBot, "denoise", imageworkflow.StateRevoked, "x")
	world.SeedActivePairs(contractWF, contractBot)
	_, err := service.Trigger(context.Background(), contractUser, contractBot, contractWF, imageworkflow.TriggerRequest{
		Source: imageworkflow.SourceUpload,
		Files:  []imageworkflow.InputFile{{Path: "a.jpg", ContentHash: testHash("a"), SizeBytes: 10, Mime: "image/jpeg"}},
	})
	if !errors.Is(err, imageworkflow.ErrNotActive) {
		t.Fatalf("Trigger on a revoked workflow = %v, want ErrNotActive", err)
	}
	if got := len(world.Runs()); got != 0 {
		t.Fatalf("a refused trigger stored %d runs", got)
	}
}

// F4: a workflow whose albums are not both designated refuses the trigger.
func TestF4UndesignatedPairRefusesTheTrigger(t *testing.T) {
	service, world := newContractService()
	world.SeedWorkflow(contractWF, contractUser, contractBot, "denoise", imageworkflow.StateActive, "x")
	world.SeedPair(contractWF, contractBot, imageworkflow.RoleInput, "active")
	_, err := service.Trigger(context.Background(), contractUser, contractBot, contractWF, imageworkflow.TriggerRequest{
		Source: imageworkflow.SourceUpload,
		Files:  []imageworkflow.InputFile{{Path: "a.jpg", ContentHash: testHash("a"), SizeBytes: 10, Mime: "image/jpeg"}},
	})
	if !errors.Is(err, imageworkflow.ErrPairUnavailable) {
		t.Fatalf("Trigger without an output album = %v, want ErrPairUnavailable", err)
	}
}

// F4: malformed batches are refused before anything is stored.
func TestF4TriggerRejectsMalformedBatches(t *testing.T) {
	service, world := newContractService()
	world.SeedWorkflow(contractWF, contractUser, contractBot, "denoise", imageworkflow.StateActive, "x")
	world.SeedActivePairs(contractWF, contractBot)
	for _, tc := range []struct {
		name string
		req  imageworkflow.TriggerRequest
	}{
		{"unknown source", imageworkflow.TriggerRequest{Source: "carrier-pigeon", Files: []imageworkflow.InputFile{{Path: "a.jpg", ContentHash: testHash("a")}}}},
		{"empty upload batch", imageworkflow.TriggerRequest{Source: imageworkflow.SourceUpload}},
		{"upload without a hash", imageworkflow.TriggerRequest{Source: imageworkflow.SourceUpload, Files: []imageworkflow.InputFile{{Path: "a.jpg"}}}},
		{"absolute directory path", imageworkflow.TriggerRequest{Source: imageworkflow.SourceDirectory, Files: []imageworkflow.InputFile{{Path: "/etc/passwd"}}}},
		{"escaping directory path", imageworkflow.TriggerRequest{Source: imageworkflow.SourceDirectory, Files: []imageworkflow.InputFile{{Path: "../outside.jpg"}}}},
		{"too many files", imageworkflow.TriggerRequest{Source: imageworkflow.SourceUpload, Files: manyFiles(imageworkflow.MaxBatchFiles + 1)}},
	} {
		if _, err := service.Trigger(context.Background(), contractUser, contractBot, contractWF, tc.req); !errors.Is(err, imageworkflow.ErrInvalid) {
			t.Fatalf("%s: Trigger = %v, want ErrInvalid", tc.name, err)
		}
	}
	if got := len(world.Runs()); got != 0 {
		t.Fatalf("refused triggers stored %d runs", got)
	}
}

func manyFiles(n int) []imageworkflow.InputFile {
	files := make([]imageworkflow.InputFile, 0, n)
	for i := 0; i < n; i++ {
		files = append(files, imageworkflow.InputFile{Path: "f.jpg", ContentHash: testHash("f"), SizeBytes: 1, Mime: "image/jpeg"})
	}
	return files
}
