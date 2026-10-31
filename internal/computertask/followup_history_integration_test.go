package computertask

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/mcp"
)

// Exercise both merged routes through the standard client, rather than merely
// checking that the two result fields survived a textual merge.
func TestIntegratedNativeFollowupAndBatchHistoryRemainSeparate(t *testing.T) {
	t.Run("batch", func(t *testing.T) {
		source, store, transport, _ := setupBatch(t)
		client := standardClient(t, source, "pc-a", false)
		initial := parseResult(t, invoke(t, client, "computer_task_submit", map[string]any{"kind": "herdr_pi_task", "task": "synthetic combined history check"}, false))
		got := batchResultUntilReady(t, client, initial.ID)
		if got.Continuity == nil || got.Continuity.Status != "unconfirmed" || got.Continuity.LastInvocation == nil || got.Continuity.LastInvocation.After != "created" || got.PiResult == nil {
			t.Fatalf("batch result lost its historical evidence: %+v", got)
		}
		if got.NativeAgentName != "" || got.SessionBinding != "" || got.Followup != nil || len(got.Followups) != 0 {
			t.Fatal("batch operation advertised native follow-up identity")
		}
		before := observationSnapshot(t, store)
		calls, runs := slices.Clone(transport.calls), transport.runs
		invoke(t, client, "computer_task_followup", followupArgs(initial.ID, "current_agent"), true)
		if !bytes.Equal(before, observationSnapshot(t, store)) || !slices.Equal(calls, transport.calls) || runs != transport.runs {
			t.Fatal("unsupported native follow-up mutated or dispatched a batch operation")
		}
	})
}

func TestIntegratedComputerToolCatalogPreservesBothBoundaries(t *testing.T) {
	source, _, _ := setup(t)
	tools, err := source.ListTools(t.Context(), mcp.ToolSessionContext{})
	if err != nil {
		t.Fatal(err)
	}
	descriptions := map[string]string{}
	for _, tool := range tools {
		if _, duplicate := descriptions[tool.Name]; duplicate {
			t.Fatalf("duplicate tool after merge: %s", tool.Name)
		}
		descriptions[tool.Name] = tool.Description
	}
	for name, fragment := range map[string]string{
		"computer_task_followup": "Accepted means prompt delivery, not business completion",
		"computer_task_get":      "follow-up history",
		"computer_task_result":   "not current history availability or retention of all old messages",
		"computer_task_recover":  "never restored history",
	} {
		if !strings.Contains(descriptions[name], fragment) {
			t.Fatalf("merged tool %s lost its boundary description", name)
		}
	}
}
