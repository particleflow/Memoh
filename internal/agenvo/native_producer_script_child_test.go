package agenvo

import (
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/felinics/memoh/internal/deliverymeta"
)

// A test-binary entry point for the DB integration fixture. It exports no
// production API and performs no file admission. Scripts come from the real
// batch generator, not a duplicated completion/collection implementation.
func TestNativeProducerScriptChild(t *testing.T) {
	if os.Getenv("MEMOH_NATIVE_SCRIPT_CHILD") != "1" {
		return
	}
	var input struct {
		Target Target
		Plan   deliverymeta.Plan
	}
	if err := json.NewDecoder(io.LimitReader(os.Stdin, 32768)).Decode(&input); err != nil {
		t.Fatal(err)
	}
	p := batchFixtureProgress()
	p.LayoutVersion = BatchLayoutVersion
	p.Producer = &input.Plan
	p.Attempt, p.Generation = input.Plan.Task.Attempt, input.Plan.Task.Generation
	script, err := batchScript(p, input.Target, input.Target.WorkDir, "produce the declared synthetic result once")
	if err != nil {
		t.Fatal(err)
	}
	wake, err := nativeProducerCollectCommand(input.Target.Producer)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]string{"script": script, "wake": wake}); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}
