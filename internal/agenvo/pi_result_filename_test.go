package agenvo

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/deliverymeta"
)

func TestExplicitResultFilenameClosesPNGWithOriginalReceipt(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 97, 83))
	for y := range 83 {
		for x := range 97 {
			img.SetNRGBA(x, y, color.NRGBA{R: byte(x*53 + y*31), G: byte(x*17 + y*71), B: byte(x ^ y*13), A: 255}) //nolint:gosec // Deliberate bounded synthetic pixel truncation.
		}
	}
	var content bytes.Buffer
	if err := png.Encode(&content, img); err != nil {
		t.Fatal(err)
	}
	const filename = "windows_photo_a0_result.png"
	p := batchFixtureProgress()
	p.LayoutVersion, p.CompletionVersion = BatchLayoutVersion, BatchCompletionVersion
	p.Producer = &deliverymeta.Plan{Version: 1, Task: deliverymeta.Task{OperationID: uuid.NewString(), BotID: uuid.NewString(), UserID: uuid.NewString(), SessionID: uuid.NewString(), TargetID: uuid.NewString(), RunID: "synthetic-filename-run", Attempt: p.Attempt, Generation: p.Generation, RootHash: deliverymeta.RootHash(`C:\Synthetic`)}, Artifact: deliverymeta.Artifact{SourceRef: filename, Mime: "image/png"}}
	target := Target{WorkDir: `C:\Synthetic`, Agent: AgentProfile{Provider: "synthetic", Model: "synthetic-model"}, Producer: ProducerLocator{ProgramPath: `C:\Synthetic\producer.exe`, ConfigDir: `C:\Synthetic\config`, StateDir: `C:\Synthetic\state`}}
	if err := ValidateBatchRequest(p, target, ".", "write exactly the declared PNG filename"); err != nil {
		t.Fatal("future exact declaration rejected before Pi", err)
	}
	events := filepath.Join(dir, "events")
	if err := os.WriteFile(events, []byte(piEvents(p.PiSessionID, "stop", "Synthetic PNG fixture completed.", false)), 0o600); err != nil {
		t.Fatal(err)
	}
	programs := map[string]string{
		"uname":        "#!/bin/sh\nprintf MINGW64_NT-SYNTHETIC\n",
		"cygpath":      "#!/bin/sh\ncase \"$2\" in 'C:\\Synthetic') printf '%s' \"$SYNTH_ROOT\";; 'C:\\Synthetic\\producer.exe') printf '%s/bin/producer.exe' \"$SYNTH_ROOT\";; *) printf '%s' \"$2\";; esac\n",
		"pi":           "#!/bin/sh\ncat >/dev/null\nprintf '%s' \"$SYNTH_PNG\" | base64 --decode > \"$SYNTH_FILENAME\"\nprintf 'once' >> \"$SYNTH_ROOT/pi-count\"\ncat \"$SYNTH_EVENTS\"\n",
		"producer.exe": "#!/bin/sh\nprintf 'collect' >> \"$SYNTH_ROOT/collect-count\"\n",
	}
	for name, body := range programs {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o700); err != nil { //nolint:gosec // Fixed isolated fixture executables, not native Agent or production host.
			t.Fatal(err)
		}
	}
	script := batchFixtureScript(t, p, target, target.WorkDir, "write exactly the declared PNG filename")
	run := func() ([]byte, error) {
		cmd := exec.CommandContext(t.Context(), "/bin/bash", "--noprofile", "--norc", "-p", "-c", script) //nolint:gosec // Production generated envelope with fixed synthetic executables only.
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "SYNTH_ROOT="+dir, "SYNTH_FILENAME="+filename, "SYNTH_EVENTS="+events, "SYNTH_PNG="+base64.StdEncoding.EncodeToString(content.Bytes()))
		return cmd.CombinedOutput()
	}
	out, err := run()
	if err != nil {
		t.Fatalf("actual generated envelope: %v %s", err, out)
	}
	receipt, err := parseBatchReceipt(string(out), p)
	if err != nil || receipt == nil || !receipt.Started || receipt.ExitCode != 0 || receipt.RequestHash != p.RequestHash || receipt.Completion == nil {
		t.Fatal("original completed receipt missing", err)
	}
	attempt, _ := deliverymeta.AttemptDirectory(p.Attempt)
	context := readBatchFixture(t, filepath.Join(dir, attempt, deliverymeta.ProducerContextName))
	var plan deliverymeta.Plan
	if err := json.Unmarshal(context, &plan); err != nil {
		t.Fatal(err)
	}
	canonical, _ := json.Marshal(p.Producer)
	if !bytes.Equal(context, canonical) || deliverymeta.PlanHash(plan) != deliverymeta.PlanHash(*p.Producer) {
		t.Fatal("original producer context changed")
	}
	manifest := readBatchFixture(t, filepath.Join(dir, p.Producer.ManifestName()))
	var closed deliverymeta.Closed
	sum := sha256.Sum256(content.Bytes())
	if json.Unmarshal(manifest, &closed) != nil || !closed.Valid() || closed.Task != p.Producer.Task || closed.Artifact != p.Producer.Artifact || closed.DeliveryID() != p.Producer.DeliveryID() || closed.SHA256 != hex.EncodeToString(sum[:]) || closed.SizeBytes != int64(content.Len()) {
		t.Fatal("closed PNG lost filename, bytes or original identity")
	}
	actual := readBatchFixture(t, filepath.Join(dir, filename))
	if !bytes.Equal(actual, content.Bytes()) {
		t.Fatal("PNG source bytes changed")
	}
	if _, err := run(); err == nil || string(readBatchFixture(t, filepath.Join(dir, "pi-count"))) != "once" || string(readBatchFixture(t, filepath.Join(dir, "collect-count"))) != "collect" {
		t.Fatal("same attempt reran Pi or recollected")
	}
	if !bytes.Equal(readBatchFixture(t, filepath.Join(dir, p.Producer.ManifestName())), manifest) {
		t.Fatal("duplicate launch changed original manifest")
	}
	t.Logf("valid PNG bytes=%d sha256=%x context_sha256=%x plan_hash=%s; actual Bash closure + original compact completion receipt + exact manifest; Pi fixture=1; no HTTP upload", content.Len(), sum, sha256.Sum256(context), deliverymeta.PlanHash(plan))
}
