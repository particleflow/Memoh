package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/transferclient"
)

func TestCLIProducerPageChild(_ *testing.T) {
	if os.Getenv("MEMOH_PAGE_CHILD") != "1" {
		return
	}
	os.Exit(runCommand([]string{"-action", "enqueue-results"}, os.Stdin, os.Stdout, os.Stderr, os.Getenv, nil, time.Now))
}

func TestCLIPathProducerPagesRestartAndPartialFailure(t *testing.T) {
	for _, missing := range []int{0, 7} {
		t.Run(strconv.Itoa(missing), func(t *testing.T) {
			root := cliPrivateRoot(t)
			config, state, source := filepath.Join(root, "config"), filepath.Join(root, "state"), filepath.Join(root, "source")
			for _, path := range []string{config, state, source} {
				if err := cliFixtureDir(path); err != nil {
					t.Fatal(err)
				}
			}
			write := func(path string, data []byte) {
				t.Helper()
				if err := cliFixtureWrite(path, data); err != nil {
					t.Fatal(err)
				}
			}
			settings, _ := json.Marshal(map[string]any{"version": 1, "origin": "https://fixture.invalid/api", "source_root": source})
			write(filepath.Join(config, "transfer.json"), settings)
			write(filepath.Join(config, "device.credential"), []byte("synthetic-page-device"))
			data := []byte("page-source")
			sum := sha256.Sum256(data)
			task := deliverymeta.Task{OperationID: uuid.NewString(), BotID: uuid.NewString(), UserID: uuid.NewString(), SessionID: uuid.NewString(), TargetID: uuid.NewString(), RunID: "fixture", Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32), RootHash: deliverymeta.RootHash(source)}
			files := make([]deliverymeta.Closed, deliverymeta.MaxArtifacts+2)
			for i := range files {
				files[i] = deliverymeta.Closed{Plan: deliverymeta.Plan{Version: 1, Task: task, Artifact: deliverymeta.Artifact{SourceRef: fmt.Sprintf("file_%02d", i), Mime: "text/plain"}}, Closed: true, SHA256: hex.EncodeToString(sum[:]), SizeBytes: int64(len(data))}
				if i != missing {
					write(filepath.Join(source, files[i].Artifact.SourceRef), data)
				}
			}
			relative, _ := deliverymeta.AttemptDirectory(task.Attempt)
			current := source
			for _, segment := range strings.Split(relative, "/") {
				current = filepath.Join(current, segment)
				if err := cliFixtureDir(current); err != nil {
					t.Fatal(err)
				}
			}
			anchor := deliverymeta.Plan{Version: 2, Task: task}
			for _, file := range files[:deliverymeta.MaxArtifacts] {
				anchor.Artifacts = append(anchor.Artifacts, file.Artifact)
			}
			anchorRaw, _ := json.Marshal(anchor)
			write(filepath.Join(current, deliverymeta.ProducerContextName), anchorRaw)
			invoke := func(page []deliverymeta.Closed, want int) transferclient.ProducerPageResult {
				t.Helper()
				raw, _ := json.Marshal(deliverymeta.ClosedPage{Version: 1, Files: page})
				cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestCLIProducerPageChild$") //nolint:gosec // The current test executable, never a downloaded program.
				cmd.Env = append(os.Environ(), "MEMOH_PAGE_CHILD=1", "HERDR_PLUGIN_CONFIG_DIR="+config, "HERDR_PLUGIN_STATE_DIR="+state)
				cmd.Stdin = bytes.NewReader(raw)
				var out, stderr bytes.Buffer
				cmd.Stdout, cmd.Stderr = &out, &stderr
				err := cmd.Run()
				if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != want {
					t.Fatalf("page process: %v %s %s", err, out.String(), stderr.String())
				}
				for _, private := range []string{source, "synthetic-page-device", "fixture.invalid"} {
					if bytes.Contains(out.Bytes(), []byte(private)) || bytes.Contains(stderr.Bytes(), []byte(private)) {
						t.Fatal("private metadata leaked")
					}
				}
				var result transferclient.ProducerPageResult
				if json.Unmarshal(out.Bytes(), &result) != nil {
					t.Fatal("invalid output")
				}
				return result
			}
			first := invoke(files[:deliverymeta.MaxArtifacts], 1)
			if len(first.Files) != deliverymeta.MaxArtifacts {
				t.Fatal("independent files were skipped")
			}
			for i, item := range first.Files {
				if item.Registered != (i != missing) || item.DeliveryID != files[i].DeliveryID() {
					t.Fatal("partial page lost identity", i)
				}
			}
			second := invoke(files[deliverymeta.MaxArtifacts:], 0)
			if len(second.Files) != 2 || !second.Files[1].Registered {
				t.Fatal("second page blocked by total count")
			}
			write(filepath.Join(source, files[missing].Artifact.SourceRef), data)
			invoke(files[:deliverymeta.MaxArtifacts], 0)
			// A fresh process replays the same registered metadata even if a source
			// is now absent. Index/transfer will separately report source_missing.
			if err := os.Remove(filepath.Join(source, files[17].Artifact.SourceRef)); err != nil {
				t.Fatal(err)
			}
			replayed := invoke(files[deliverymeta.MaxArtifacts:], 0)
			if !replayed.Files[1].Registered {
				t.Fatal("restart re-admitted missing source")
			}
			conflict := append([]deliverymeta.Closed(nil), files[:deliverymeta.MaxArtifacts]...)
			conflict[0].SHA256 = strings.Repeat("0", 64)
			invoke(conflict, 1)
			invoke(files[:deliverymeta.MaxArtifacts], 0)
			invoke(files, 1) // 16 is the wire page size; the already registered 18 persist.
			foreign := append([]deliverymeta.Closed(nil), files[deliverymeta.MaxArtifacts:]...)
			for i := range foreign {
				foreign[i].Task.Generation = strings.Repeat("f", 32)
			}
			invoke(foreign, 1) // A valid-looking page cannot replace the original context.
			getenv := func(k string) string {
				if k == "HERDR_PLUGIN_CONFIG_DIR" {
					return config
				}
				if k == "HERDR_PLUGIN_STATE_DIR" {
					return state
				}
				return ""
			}
			limits, err := transferclient.LoadHostQueueLimits(getenv)
			if err != nil {
				t.Fatal(err)
			}
			q, err := transferclient.OpenHerdrQueue(getenv, "native-profile-v1", limits)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = q.Close() }()
			if _, err := q.Inspect(foreign[0].DeliveryID()); err == nil {
				t.Fatal("foreign page admitted before original-context validation")
			}
			for _, file := range files {
				r, err := q.Inspect(file.DeliveryID())
				if err != nil || !r.Registered || r.Job.Spec.SHA256 != file.SHA256 || r.Admitted || r.Job.ReceiptID != "" {
					t.Fatal("page replay changed original journal", err)
				}
			}
		})
	}
}
