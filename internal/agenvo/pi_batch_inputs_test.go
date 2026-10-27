package agenvo

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/deliverymeta"
)

func taskInputProgress(t *testing.T, root string, data []byte) BatchProgress {
	t.Helper()
	p := batchFixtureProgress()
	sum := sha256.Sum256(data)
	p.Inputs = &deliverymeta.InputPlan{Version: 1, Task: deliverymeta.Task{OperationID: uuid.NewString(), BotID: uuid.NewString(), UserID: uuid.NewString(), SessionID: uuid.NewString(), TargetID: uuid.NewString(), RunID: "synthetic-input-run", Attempt: p.Attempt, Generation: p.Generation, RootHash: deliverymeta.RootHash(root)}, Files: []deliverymeta.Input{{ContentHash: hex.EncodeToString(sum[:]), SizeBytes: int64(len(data)), Mime: "application/octet-stream"}}}
	return p
}

func TestTaskInputVerifierChecksActualFileAndImmutableContext(t *testing.T) {
	for _, kind := range []string{"valid", "changed", "ancestor-link", "hardlink", "missing", "changed-context"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			data := bytes.Repeat([]byte("actual input bytes 雪\x00\xff"), 17)
			p := taskInputProgress(t, root, data)
			path := filepath.Join(root, filepath.FromSlash(p.Inputs.Path(p.Inputs.Files[0])))
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			context, err := taskInputContext(p, Target{WorkDir: root})
			if err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(context)
			switch kind {
			case "changed":
				data[0] ^= 0xff
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			case "ancestor-link":
				dir := filepath.Dir(filepath.Dir(path))
				if err := os.Rename(dir, dir+"-original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(dir+"-original", dir); err != nil {
					t.Fatal("fixture symlink", err)
				}
			case "hardlink":
				if err := os.Link(path, path+"-alias"); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "changed-context":
				context = append(context, '\n')
			}
			contextPath := filepath.Join(t.TempDir(), "input-context.json")
			if err := os.WriteFile(contextPath, context, 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(t.Context(), "node", "-e", taskInputVerifier, contextPath, hex.EncodeToString(hash[:])) //nolint:gosec // Fixed production verifier; isolated fixture paths and public digests only.
			out, err := cmd.CombinedOutput()
			if kind == "valid" && err != nil || kind != "valid" && err == nil || len(out) != 0 {
				t.Fatalf("verifier %s: %v %s", kind, err, out)
			}
		})
	}
}

func TestTaskInputDestinationKeepsRegisteredWindowsRoot(t *testing.T) {
	for _, root := range []string{`C:\Approved task root`, `\\synthetic-nas\workspace\computer`} {
		p := taskInputProgress(t, root, []byte("root-bound"))
		got, err := TaskInputDirectory(Target{WorkDir: root}, *p.Inputs)
		if err != nil || got != root+`\.memoh-computer\inputs\`+p.Attempt {
			t.Fatal("input escaped/rewrote registered root", got, err)
		}
		p.Inputs.Task.RootHash = strings.Repeat("0", 64)
		if _, err := TaskInputDirectory(Target{WorkDir: root}, *p.Inputs); err == nil {
			t.Fatal("wrong original root accepted")
		}
	}
}
