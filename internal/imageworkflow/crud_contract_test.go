package imageworkflow_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/imageworkflow"
	"github.com/felinics/memoh/internal/imageworkflow/imageworkflowtest"
)

const (
	contractUser = "11111111-1111-1111-1111-111111111111"
	contractBot  = "22222222-2222-2222-2222-222222222222"
	contractWF   = "33333333-3333-3333-3333-333333333333"
	otherWF      = "44444444-4444-4444-4444-444444444444"
)

func newContractService() (*imageworkflow.Service, *imageworkflowtest.World) {
	world := imageworkflowtest.New()
	return imageworkflow.NewService(nil, world.Deps()), world
}

func testHash(seed string) string {
	// A stable 64-hex identity per seed; the contract only ever treats it as an
	// opaque content hash.
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

// F3: create registers a workflow in state active and mints two distinct
// pairing labels that satisfy the write-back label vocabulary.
func TestF3CreateMintsTwoDistinctPairLabels(t *testing.T) {
	service, world := newContractService()
	created, err := service.Create(context.Background(), contractUser, imageworkflow.CreateInput{
		Name:        "denoise",
		Instruction: "sharpen and denoise every photo",
		BotID:       contractBot,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == "" {
		t.Fatal("Create returned an empty workflow id")
	}
	if created.State != imageworkflow.StateActive {
		t.Fatalf("created state = %q, want %q", created.State, imageworkflow.StateActive)
	}
	if created.InputPairKey == created.OutputPairKey {
		t.Fatalf("input and output labels are identical: %q", created.InputPairKey)
	}
	for _, key := range []string{created.InputPairKey, created.OutputPairKey} {
		if key == "" {
			t.Fatal("Create returned an empty pairing label")
		}
		for _, ch := range key {
			allowed := ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_' || ch == '.'
			if !allowed {
				t.Fatalf("pairing label %q contains %q, which the write-back queue rejects", key, ch)
			}
		}
	}
	if rows := world.Workflows(); len(rows) != 1 {
		t.Fatalf("stored workflows = %d, want 1", len(rows))
	}
}

// F3: a duplicate name is a conflict, and a revoked workflow keeps its name.
func TestF3CreateRejectsATakenNameEvenAfterRevoke(t *testing.T) {
	service, world := newContractService()
	world.SeedWorkflow(contractWF, contractUser, contractBot, "denoise", imageworkflow.StateRevoked, "x")
	_, err := service.Create(context.Background(), contractUser, imageworkflow.CreateInput{
		Name: "denoise", Instruction: "y", BotID: contractBot,
	})
	if !errors.Is(err, imageworkflow.ErrNameTaken) {
		t.Fatalf("Create with a revoked owner's name = %v, want ErrNameTaken", err)
	}
}

// F3: malformed input is refused before anything is stored.
func TestF3CreateRejectsMalformedInput(t *testing.T) {
	service, world := newContractService()
	for _, tc := range []struct {
		name string
		in   imageworkflow.CreateInput
	}{
		{"empty name", imageworkflow.CreateInput{Name: "", Instruction: "x", BotID: contractBot}},
		{"blank instruction", imageworkflow.CreateInput{Name: "n", Instruction: "   ", BotID: contractBot}},
		{"bad bot", imageworkflow.CreateInput{Name: "n", Instruction: "x", BotID: "not-a-uuid"}},
	} {
		if _, err := service.Create(context.Background(), contractUser, tc.in); !errors.Is(err, imageworkflow.ErrInvalid) {
			t.Fatalf("%s: Create = %v, want ErrInvalid", tc.name, err)
		}
	}
	if rows := world.Workflows(); len(rows) != 0 {
		t.Fatalf("a refused create stored %d rows", len(rows))
	}
}

// F3: the detail response carries the newest runs and per-state counts.
func TestF3GetReportsRunsAndCounts(t *testing.T) {
	service, world := newContractService()
	world.SeedWorkflow(contractWF, contractUser, contractBot, "denoise", imageworkflow.StateActive, "x")
	detail, err := service.Get(context.Background(), contractUser, contractWF)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if detail.Workflow.ID != contractWF {
		t.Fatalf("detail workflow = %q", detail.Workflow.ID)
	}
	if detail.RunCounts == nil {
		t.Fatal("detail RunCounts is nil; the contract requires a per-state map")
	}
	for _, state := range []string{imageworkflow.RunHeld, imageworkflow.RunArmed, imageworkflow.RunRunning, imageworkflow.RunCompleted, imageworkflow.RunFailed} {
		if _, ok := detail.RunCounts[state]; !ok {
			t.Fatalf("detail RunCounts has no %q key", state)
		}
	}
}

// F3: update changes the name and instruction and never the labels.
func TestF3UpdateKeepsPairLabels(t *testing.T) {
	service, world := newContractService()
	seeded := world.SeedWorkflow(contractWF, contractUser, contractBot, "denoise", imageworkflow.StateActive, "old")
	newName := "denoise-v2"
	newInstruction := "crop to 4:3 then denoise"
	updated, err := service.Update(context.Background(), contractUser, contractWF, imageworkflow.UpdateInput{
		Name: &newName, Instruction: &newInstruction,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != newName || updated.Instruction != newInstruction {
		t.Fatalf("updated = %+v", updated)
	}
	if updated.InputPairKey != seeded.InputPairKey || updated.OutputPairKey != seeded.OutputPairKey {
		t.Fatalf("Update changed the pairing labels: %+v", updated)
	}
}

// F3: revoke is a soft delete that keeps runs and products.
func TestF3RevokeKeepsRunsAndLabels(t *testing.T) {
	service, world := newContractService()
	seeded := world.SeedWorkflow(contractWF, contractUser, contractBot, "denoise", imageworkflow.StateActive, "x")
	world.SeedActivePairs(contractWF, contractBot)
	revoked, err := service.Revoke(context.Background(), contractUser, contractWF)
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if revoked.State != imageworkflow.StateRevoked {
		t.Fatalf("revoked state = %q", revoked.State)
	}
	if revoked.Name != seeded.Name || revoked.InputPairKey != seeded.InputPairKey || revoked.OutputPairKey != seeded.OutputPairKey {
		t.Fatalf("Revoke changed the identity: %+v", revoked)
	}
	if got := world.RevokedWorkflows(); len(got) != 1 || got[0] != contractWF {
		t.Fatalf("pairing rows revoked = %v, want [%s]", got, contractWF)
	}
}

// F7: pause is idempotent and keeps the pending work flowing.
func TestF7PauseIsIdempotentAndKeepsPendingWritebacks(t *testing.T) {
	service, world := newContractService()
	world.SeedWorkflow(contractWF, contractUser, contractBot, "denoise", imageworkflow.StateActive, "x")
	first, err := service.Pause(context.Background(), contractUser, contractWF)
	if err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if first.State != imageworkflow.StatePaused {
		t.Fatalf("paused state = %q", first.State)
	}
	if _, err := service.Pause(context.Background(), contractUser, contractWF); err != nil {
		t.Fatalf("second Pause: %v", err)
	}
	rows := world.Workflows()
	if len(rows) != 1 || rows[0].State != imageworkflow.StatePaused {
		t.Fatalf("workflow rows after two pauses = %+v", rows)
	}
}

// F7: a revoked workflow cannot be paused; that is not_active, not not_found.
func TestF7PauseRejectsARevokedWorkflow(t *testing.T) {
	service, world := newContractService()
	world.SeedWorkflow(contractWF, contractUser, contractBot, "denoise", imageworkflow.StateRevoked, "x")
	if _, err := service.Pause(context.Background(), contractUser, contractWF); !errors.Is(err, imageworkflow.ErrNotActive) {
		t.Fatalf("Pause on a revoked workflow = %v, want ErrNotActive", err)
	}
}

// F7: resume arms every held run and returns the workflow to active.
func TestF7ResumeArmsEveryHeldRun(t *testing.T) {
	service, world := newContractService()
	world.SeedWorkflow(contractWF, contractUser, contractBot, "denoise", imageworkflow.StatePaused, "x")
	world.SeedActivePairs(contractWF, contractBot)
	service2, err := service.Trigger(context.Background(), contractUser, contractBot, contractWF, imageworkflow.TriggerRequest{
		Source: imageworkflow.SourceUpload,
		Files:  []imageworkflow.InputFile{{Path: "a.jpg", ContentHash: testHash("a"), SizeBytes: 10, Mime: "image/jpeg"}},
	})
	if err != nil {
		t.Fatalf("Trigger while paused: %v", err)
	}
	_ = service2
	result, err := service.Resume(context.Background(), contractUser, contractWF)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if result.Workflow.State != imageworkflow.StateActive {
		t.Fatalf("resumed state = %q", result.Workflow.State)
	}
	if result.ArmedRuns != 1 {
		t.Fatalf("armed runs = %d, want 1", result.ArmedRuns)
	}
	for _, run := range world.Runs() {
		if run.WorkflowID == contractWF && run.State == imageworkflow.RunHeld {
			t.Fatalf("run %s stayed held after resume", run.ID)
		}
	}
	// Arming uses the run's own recorded hashes, not whatever the directory
	// holds now: the batch is the snapshot, not the current file set.
	schedules := world.Schedules()
	if len(schedules) != 1 {
		t.Fatalf("schedules after resume = %d, want 1", len(schedules))
	}
	if !strings.Contains(schedules[0].Req.Command, testHash("a")) {
		t.Fatalf("armed schedule does not carry the run's recorded hash:\n%s", schedules[0].Req.Command)
	}
}

// F7: resuming an active workflow arms nothing.
func TestF7ResumeOnAnActiveWorkflowArmsNothing(t *testing.T) {
	service, world := newContractService()
	world.SeedWorkflow(contractWF, contractUser, contractBot, "denoise", imageworkflow.StateActive, "x")
	result, err := service.Resume(context.Background(), contractUser, contractWF)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if result.ArmedRuns != 0 {
		t.Fatalf("armed runs = %d, want 0", result.ArmedRuns)
	}
}
