package agenvo

import (
	"crypto/sha256"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/deliverymeta"
)

// Only the byte counts mirror the reported requests; no field prompt or host
// configuration enters this fixture, and no Runtime or model is contacted.
func TestNativeProducerComplexTaskPreflight(t *testing.T) {
	for _, tc := range []struct {
		name string
		task string
	}{
		{"3046-bytes", strings.Repeat("x", 3046)},
		{"3435-bytes", strings.Repeat("x", 3435)},
		{"3435-utf8-bytes", strings.Repeat("雪", 1145)},
		{"3435-varied-bytes", variedSyntheticTask(3435)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := batchFixtureProgress()
			p.LayoutVersion, p.CompletionVersion = BatchLayoutVersion, BatchCompletionVersion
			p.History = &BatchHistoryExpectation{Version: BatchHistoryVersion, Mode: "unconfirmed"}
			target := Target{NativeRuntimeID: uuid.NewString(), WorkDir: `C:\Synthetic`, BashPath: `C:\Synthetic\Git\bin\bash.exe`, Agent: AgentProfile{Provider: "synthetic", Model: "synthetic-model", ConfigDir: `C:\Synthetic\Pi`, ExecutionMode: "pi-json-v1"}, Producer: ProducerLocator{ProgramPath: `C:\Synthetic\producer.exe`, ConfigDir: `C:\Synthetic\producer-config`, StateDir: `C:\Synthetic\producer-state`}}
			p.Producer = &deliverymeta.Plan{Version: 1, Task: deliverymeta.Task{OperationID: uuid.NewString(), BotID: uuid.NewString(), UserID: uuid.NewString(), SessionID: uuid.NewString(), TargetID: uuid.NewString(), RunID: uuid.NewString(), Attempt: p.Attempt, Generation: p.Generation, RootHash: deliverymeta.RootHash(target.WorkDir)}, Artifact: deliverymeta.Artifact{SourceRef: strings.Repeat("r", 23), Mime: "text/plain"}}
			if !p.valid() || !p.Producer.Valid() || target.Producer.validate() != nil {
				t.Fatal("invalid synthetic identity or producer configuration")
			}
			if err := ValidateBatchRequest(p, target, ".", tc.task); err != nil {
				t.Fatalf("native complex task rejected before operation claim: task_bytes=%d: %v", len(tc.task), err)
			}
			script, err := batchScript(p, target, target.WorkDir, tc.task)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("task_bytes=%d old_launch_bytes=%d native_launch_bytes=%d", len(tc.task), len(launchBashScript(script, target.BashPath)), len(launchBatchScript(script, target)))
		})
	}
}

func variedSyntheticTask(size int) string {
	var out strings.Builder
	for i := 0; out.Len() < size; i++ {
		sum := sha256.Sum256([]byte(strconv.Itoa(i)))
		for _, value := range sum {
			if out.Len() == size {
				break
			}
			out.WriteByte('!' + value%94)
		}
	}
	return out.String()
}
