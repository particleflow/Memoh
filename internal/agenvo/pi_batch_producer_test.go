package agenvo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/deliverymeta"
)

// Synthetic Bash utilities, not a Herdr installation or Windows acceptance.
func TestBatchProducerClosesOnlyDeclaredResult(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, script := range map[string]string{
		"uname":   "#!/bin/sh\nprintf MINGW64_NT-SYNTHETIC\n",
		"cygpath": "#!/bin/sh\ncase \"$2\" in 'C:\\Synthetic') printf '%s' \"$SYNTH_ROOT\";; *) printf '%s' \"$2\";; esac\n",
		"pi":      "#!/bin/sh\ncat > /dev/null\ntest -f \"$MEMOH_DELIVERY_CONTEXT_FILE\" || exit 1\nprintf 'PRIVATE-OUTPUT-CONTENT' > result_one\nprintf 'unrelated' > unrelated\n",
		"herdr":   "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$SYNTH_ROOT/hook-args\"\nprintf 'fixture-accepted'\n",
	} {
		//nolint:gosec // Fixed synthetic executable confined to t.TempDir.
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	p := batchFixtureProgress()
	p.Producer = &deliverymeta.Plan{Version: 1, Task: deliverymeta.Task{OperationID: uuid.NewString(), BotID: uuid.NewString(), UserID: uuid.NewString(), SessionID: uuid.NewString(), TargetID: uuid.NewString(), RunID: "run", Attempt: p.Attempt, Generation: p.Generation, RootHash: deliverymeta.RootHash(`C:\Synthetic`)}, Artifact: deliverymeta.Artifact{SourceRef: "result_one", Mime: "text/plain"}}
	target := Target{WorkDir: `C:\Synthetic`, Agent: AgentProfile{Provider: "synthetic", Model: "synthetic"}}
	script := batchFixtureScript(t, p, target, target.WorkDir, "create the declared result")
	//nolint:gosec // Fixed wrapper and isolated synthetic binaries only.
	command := exec.CommandContext(t.Context(), "/bin/bash", "--noprofile", "--norc", "-p", "-c", script)
	command.Dir = dir
	command.Env = []string{"PATH=" + bin + ":/usr/bin:/bin", "SYNTH_ROOT=" + dir}
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("wrapper: %v %s", err, out)
	}
	if strings.Contains(string(out), "PRIVATE-OUTPUT") || strings.Contains(string(out), "fixture-accepted") {
		t.Fatal("file bytes or hook output entered receipt channel")
	}
	receipt, err := parseBatchReceipt(string(out), p)
	if err != nil || receipt == nil || !receipt.Started {
		t.Fatal("business receipt lost", err)
	}
	raw := readBatchFixture(t, filepath.Join(dir, p.Producer.ManifestName()))
	var closed deliverymeta.Closed
	sum := sha256.Sum256([]byte("PRIVATE-OUTPUT-CONTENT"))
	if json.Unmarshal(raw, &closed) != nil || !closed.Valid() || closed.Plan != *p.Producer || closed.SHA256 != hex.EncodeToString(sum[:]) || closed.SizeBytes != int64(len("PRIVATE-OUTPUT-CONTENT")) {
		t.Fatal("wrong closed handoff", string(raw))
	}
	args := string(readBatchFixture(t, filepath.Join(dir, "hook-args")))
	if args != "plugin\naction\ninvoke\ncollect-result\n--plugin\nmemoh.delivery\n" {
		t.Fatal("non-fixed plugin action", args)
	}
}
