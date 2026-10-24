package agenvo

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/deliverymeta"
)

// Synthetic Bash utilities, not a Herdr installation or Windows acceptance.
func TestBatchProducerClosesOnlyDeclaredResult(t *testing.T)          { exerciseBatchProducer(t, false) }
func TestBatchProducerClosesMultipleResultsWithoutRerun(t *testing.T) { exerciseBatchProducer(t, true) }
func TestBatchProducerAdditionalClosedPagesWithoutRerun(t *testing.T) {
	exerciseBatchProducer(t, true, 18)
}

func exerciseBatchProducer(t *testing.T, multiple bool, pageCount ...int) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, script := range map[string]string{
		"uname":   "#!/bin/sh\nprintf MINGW64_NT-SYNTHETIC\n",
		"cygpath": "#!/bin/sh\ncase \"$2\" in 'C:\\Synthetic') printf '%s' \"$SYNTH_ROOT\";; *) printf '%s' \"$2\";; esac\n",
		"pi":      "#!/bin/sh\ncat > /dev/null\ntest -f \"$MEMOH_DELIVERY_CONTEXT_FILE\" || exit 1\nprintf 'PRIVATE-OUTPUT-CONTENT' > result_one\nprintf 'SECOND-OUTPUT' > result_two\nprintf 'run' >> \"$SYNTH_ROOT/pi-count\"\nprintf 'unrelated' > unrelated\n",
		"herdr":   "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$SYNTH_ROOT/hook-args\"\nprintf 'fixture-accepted'\n",
	} {
		//nolint:gosec // Fixed synthetic executable confined to t.TempDir.
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	p := batchFixtureProgress()
	p.Producer = &deliverymeta.Plan{Version: 1, Task: deliverymeta.Task{OperationID: uuid.NewString(), BotID: uuid.NewString(), UserID: uuid.NewString(), SessionID: uuid.NewString(), TargetID: uuid.NewString(), RunID: "run", Attempt: p.Attempt, Generation: p.Generation, RootHash: deliverymeta.RootHash(`C:\Synthetic`)}, Artifact: deliverymeta.Artifact{SourceRef: "result_one", Mime: "text/plain"}}
	if multiple {
		p.Producer.Version = 2
		p.Producer.Artifacts = []deliverymeta.Artifact{p.Producer.Artifact, {SourceRef: "result_two", Mime: "text/plain"}, {SourceRef: "missing_result", Mime: "text/plain"}}
		p.Producer.Artifact = deliverymeta.Artifact{}
	}
	var pages []deliverymeta.ClosedPage
	if len(pageCount) != 0 {
		p.Producer.Artifacts = nil
		var files []deliverymeta.Closed
		var producer strings.Builder
		producer.WriteString("#!/bin/sh\ncat > /dev/null\ntest -f \"$MEMOH_DELIVERY_CONTEXT_FILE\" || exit 1\n")
		fmt.Fprintf(&producer, "test \"$MEMOH_DELIVERY_PAGE_PREFIX\" = '%s' || exit 1\n", deliverymeta.ProducerPagePrefix)
		hash := sha256.Sum256([]byte("PRIVATE-OUTPUT-CONTENT"))
		for i := range pageCount[0] {
			a := deliverymeta.Artifact{SourceRef: fmt.Sprintf("page_result_%02d", i), Mime: "text/plain"}
			if i < deliverymeta.MaxArtifacts {
				p.Producer.Artifacts = append(p.Producer.Artifacts, a)
			}
			files = append(files, deliverymeta.Closed{Plan: deliverymeta.Plan{Version: 1, Task: p.Producer.Task, Artifact: a}, Closed: true, SHA256: hex.EncodeToString(hash[:]), SizeBytes: 22})
			fmt.Fprintf(&producer, "printf 'PRIVATE-OUTPUT-CONTENT' > '%s'\n", a.SourceRef)
		}
		for start := 0; start < len(files); start += deliverymeta.MaxArtifacts {
			page := deliverymeta.ClosedPage{Version: 1, Files: files[start:min(start+deliverymeta.MaxArtifacts, len(files))]}
			pages = append(pages, page)
			raw, _ := json.Marshal(page)
			fmt.Fprintf(&producer, "printf '%%s' '%s' | base64 --decode > '%s'\n", base64.StdEncoding.EncodeToString(raw), page.ManifestName())
		}
		producer.WriteString("printf 'run' >> \"$SYNTH_ROOT/pi-count\"\n")
		if err := os.WriteFile(filepath.Join(bin, "pi"), []byte(producer.String()), 0o700); err != nil { //nolint:gosec // Isolated synthetic executable, not Agent modification.
			t.Fatal(err)
		}
	}
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
	for _, file := range p.Producer.FilePlans() {
		if file.Artifact.SourceRef == "missing_result" {
			if _, err := os.Stat(filepath.Join(dir, file.ManifestName())); !os.IsNotExist(err) {
				t.Fatal("missing output fabricated", err)
			}
			continue
		}
		raw := readBatchFixture(t, filepath.Join(dir, file.ManifestName()))
		var closed deliverymeta.Closed
		content := "PRIVATE-OUTPUT-CONTENT"
		if file.Artifact.SourceRef == "result_two" {
			content = "SECOND-OUTPUT"
		}
		sum := sha256.Sum256([]byte(content))
		if json.Unmarshal(raw, &closed) != nil || !closed.Valid() || !reflect.DeepEqual(closed.Plan, file) || closed.SHA256 != hex.EncodeToString(sum[:]) || closed.SizeBytes != int64(len(content)) {
			t.Fatal("wrong closed handoff")
		}
	}
	for _, page := range pages {
		raw := readBatchFixture(t, filepath.Join(dir, page.ManifestName()))
		decoded, ok := deliverymeta.DecodeClosedPage(raw)
		if !ok || !reflect.DeepEqual(decoded, page) {
			t.Fatal("additional closed page did not retain original task")
		}
	}
	// Replaying the existing launch cannot restart Pi, overwrite closed manifests
	// or manufacture the missing result. The original exclusive attempt wins.
	duplicate := exec.CommandContext(t.Context(), "/bin/bash", "--noprofile", "--norc", "-p", "-c", script) //nolint:gosec // Same synthetic wrapper, isolated fixture only.
	duplicate.Dir, duplicate.Env = command.Dir, command.Env
	if err := duplicate.Run(); err == nil {
		t.Fatal("duplicate business launch accepted")
	}
	if string(readBatchFixture(t, filepath.Join(dir, "pi-count"))) != "run" {
		t.Fatal("Pi ran again")
	}
	args := string(readBatchFixture(t, filepath.Join(dir, "hook-args")))
	if args != "plugin\naction\ninvoke\ncollect-result\n--plugin\nmemoh.delivery\n" {
		t.Fatal("non-fixed plugin action", args)
	}
}

func TestBatchProducerMaximumCountAndCommandBound(t *testing.T) {
	exerciseProducerCommandBound(t, 0)
}

func TestBatchProducerCompactCompletionCommandBound(t *testing.T) {
	exerciseProducerCommandBound(t, BatchCompletionVersion)
}

func exerciseProducerCommandBound(t *testing.T, completion int) {
	t.Helper()
	p := batchFixtureProgress()
	p.CompletionVersion = completion
	task := deliverymeta.Task{OperationID: uuid.NewString(), BotID: uuid.NewString(), UserID: uuid.NewString(), SessionID: uuid.NewString(), TargetID: uuid.NewString(), RunID: "fixture", Attempt: p.Attempt, Generation: p.Generation, RootHash: deliverymeta.RootHash(`C:\Synthetic`)}
	p.Producer = &deliverymeta.Plan{Version: 2, Task: task}
	for i := range deliverymeta.MaxArtifacts {
		p.Producer.Artifacts = append(p.Producer.Artifacts, deliverymeta.Artifact{SourceRef: fmt.Sprintf("file_%02d", i), Mime: "application/octet-stream"})
	}
	target := Target{WorkDir: `C:\Synthetic`, BashPath: `C:\Synthetic\Git\bin\bash.exe`, Agent: AgentProfile{Provider: "synthetic", Model: "synthetic"}}
	if err := ValidateBatchRequest(p, target, ".", "create declared results"); err != nil {
		t.Fatal("16 short declarations rejected", err)
	}
	script := batchFixtureScript(t, p, target, target.WorkDir, "create declared results")
	if size := len(launchBashScript(script, target.BashPath)); size > 30000 {
		t.Fatal("launch exceeds finite producer command bound", size)
	}
	if err := ValidateBatchRequest(p, target, ".", strings.Repeat("x", 32000)); err == nil {
		t.Fatal("oversized launch accepted")
	}
}
