package imageworkflow_test

import (
	"context"
	"testing"

	"github.com/felinics/memoh/internal/imageworkflow"
)

// F9: the route table names the absolute input directory of every live
// workflow, plus the paused ones so the watcher can stop without a second call.
func TestF9RoutesNameEveryLiveInputDirectory(t *testing.T) {
	service, world := newContractService()
	active := world.SeedWorkflow(contractWF, contractUser, contractBot, "first", imageworkflow.StateActive, "x")
	world.SeedWorkflow(otherWF, contractUser, contractBot, "second", imageworkflow.StatePaused, "y")
	world.SeedWorkflow("55555555-5555-5555-5555-555555555555", contractUser, contractBot, "third", imageworkflow.StateRevoked, "z")

	routes, err := service.Routes(context.Background(), contractUser)
	if err != nil {
		t.Fatalf("Routes: %v", err)
	}
	if len(routes) != 2 {
		t.Fatalf("routes = %d, want 2 (active and paused, never revoked)", len(routes))
	}
	byWorkflow := map[string]imageworkflow.Route{}
	for _, route := range routes {
		byWorkflow[route.WorkflowID] = route
	}
	got, ok := byWorkflow[contractWF]
	if !ok {
		t.Fatalf("no route for the active workflow: %+v", routes)
	}
	if got.InputPairKey != active.InputPairKey {
		t.Fatalf("route input label = %q, want %q", got.InputPairKey, active.InputPairKey)
	}
	if got.InputDir != "/srv/workflows/"+active.InputPairKey {
		t.Fatalf("route input dir = %q, want /srv/workflows/%s", got.InputDir, active.InputPairKey)
	}
	if got.BotID != contractBot || got.State != imageworkflow.StateActive {
		t.Fatalf("route = %+v", got)
	}
	if pausedRoute, ok := byWorkflow[otherWF]; !ok || pausedRoute.State != imageworkflow.StatePaused {
		t.Fatalf("paused route = %+v, want state paused", pausedRoute)
	}
	if _, ok := byWorkflow["55555555-5555-5555-5555-555555555555"]; ok {
		t.Fatal("a revoked workflow appeared in the route table")
	}
}

// F9: the route table is empty for an owner with no workflows, not an error.
func TestF9RoutesAreEmptyWithoutWorkflows(t *testing.T) {
	service, _ := newContractService()
	routes, err := service.Routes(context.Background(), contractUser)
	if err != nil {
		t.Fatalf("Routes: %v", err)
	}
	if len(routes) != 0 {
		t.Fatalf("routes = %+v, want none", routes)
	}
}
