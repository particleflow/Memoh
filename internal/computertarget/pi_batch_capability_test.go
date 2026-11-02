package computertarget

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func intPtr(v int) *int { return &v }

func capabilityFixture() *BatchModel {
	return &BatchModel{ContextWindow: intPtr(1048576), MaxTokens: intPtr(65536), Input: []string{"text", "image"}}
}

func TestBatchScriptCarriesModelCapabilityOnlyWhenProvided(t *testing.T) {
	p := batchFixtureProgress()
	p.LayoutVersion = BatchLayoutVersion
	target := Target{WorkDir: `C:\Synthetic`, Agent: AgentProfile{Provider: "magpie", Model: "synthetic", ConfigDir: `C:\Synthetic\pi`}}
	withCap := p
	withCap.Model = capabilityFixture()
	script, err := batchScript(withCap, target, target.WorkDir, "task data", "vendor/wide-model")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`memoh_agent_dir="$memoh_attempt/agent"`, `export PI_CODING_AGENT_DIR="$(cygpath -w "$memoh_agent_dir")"`, `model-config.cjs`, `--model "$memoh_model"`} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q", want)
		}
	}
	if strings.Contains(script, `export PI_CODING_AGENT_DIR=%s`) {
		t.Fatal("shared configuration directory must stay in the tab environment only")
	}
	plain, err := batchScript(p, target, target.WorkDir, "task data", "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain, "PI_CODING_AGENT_DIR") || strings.Contains(plain, "model-config.cjs") {
		t.Fatal("an invocation without capability metadata must leave Pi's shared configuration untouched")
	}
	if _, err := batchScript(BatchProgress{Attempt: p.Attempt, Generation: p.Generation, RequestHash: p.RequestHash, PiSessionID: p.PiSessionID, Model: &BatchModel{ContextWindow: intPtr(-1)}}, target, target.WorkDir, "task", "m"); err == nil {
		t.Fatal("invalid capability metadata was accepted")
	}
}

// The per-task directory must be derived by the embedded Node program from the
// deployment's own models.json: the chosen model gets the capability the live
// catalog stated, and no credential material is copied out of the shared
// directory.
func TestBatchScriptBuildsPerTaskModelsFile(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("Node required for model configuration contract tests")
	}
	dir := t.TempDir()
	shared := filepath.Join(dir, "shared")
	if err := os.Mkdir(shared, 0o700); err != nil {
		t.Fatal(err)
	}
	anchor := `{"providers":{"magpie":{"baseUrl":"http://127.0.0.1:1/v1","api":"openai-completions","apiKey":"!powershell.exe -NoProfile -File \"${PI_CODING_AGENT_DIR}/read-dpapi.ps1\"","authHeader":true,"models":[{"id":"anchor/model","api":"openai-completions","baseUrl":"http://127.0.0.1:1/v1","input":["text","image"],"reasoning":true,"contextWindow":1048576,"maxTokens":393216}]}}}`
	if err := os.WriteFile(filepath.Join(shared, "models.json"), []byte(anchor), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shared, "settings.json"), []byte(`{"defaultProvider":"magpie"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	p := batchFixtureProgress()
	p.LayoutVersion = BatchLayoutVersion
	p.Model = capabilityFixture()
	target := Target{WorkDir: `C:\Synthetic`, Agent: AgentProfile{Provider: "magpie", Model: "synthetic", ConfigDir: shared}}
	script, err := batchScript(p, target, target.WorkDir, "task data", "vendor/wide-model")
	if err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(dir, "model-config.cjs")
	spec := filepath.Join(dir, "model-config.json")
	if err := os.WriteFile(program, extractScriptFile(t, script, "model-config.cjs"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec, extractScriptFile(t, script, "model-config.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	var modelSpec BatchModel
	if raw := readBatchFixture(t, spec); json.Unmarshal(raw, &modelSpec) != nil || modelSpec.ID != "vendor/wide-model" || modelSpec.Provider != "magpie" {
		t.Fatalf("spec was not bound to this invocation: %s", raw)
	}
	out := filepath.Join(dir, "agent")
	//nolint:gosec // Embedded fixed program and synthetic fixture files only.
	if raw, err := exec.CommandContext(t.Context(), node, program, shared, out, spec).CombinedOutput(); err != nil {
		t.Fatalf("model configuration: %v %s", err, raw)
	}
	raw, err := os.ReadFile(filepath.Join(out, "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Providers map[string]struct {
			APIKey string `json:"apiKey"`
			Models []struct {
				ID            string   `json:"id"`
				Name          string   `json:"name"`
				ContextWindow int      `json:"contextWindow"`
				MaxTokens     int      `json:"maxTokens"`
				Input         []string `json:"input"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	provider := cfg.Providers["magpie"]
	if len(provider.Models) != 2 {
		t.Fatalf("expected the chosen entry plus the anchor entry: %+v", provider.Models)
	}
	chosen := provider.Models[0]
	if chosen.ID != "vendor/wide-model" || chosen.Name != "vendor/wide-model" || chosen.ContextWindow != 1048576 || chosen.MaxTokens != 65536 || len(chosen.Input) != 2 {
		t.Fatalf("capability not written: %+v", chosen)
	}
	if provider.Models[1].ID != "anchor/model" {
		t.Fatalf("anchor entry dropped: %+v", provider.Models)
	}
	if !strings.Contains(provider.APIKey, filepath.ToSlash(shared)) || strings.Contains(provider.APIKey, "PI_CODING_AGENT_DIR") {
		t.Fatalf("credential command must point at the shared directory instead of being copied: %q", provider.APIKey)
	}
	if settings, err := os.ReadFile(filepath.Join(out, "settings.json")); err != nil || !strings.Contains(string(settings), "magpie") {
		t.Fatalf("settings not carried: %v %q", err, settings)
	}
	if _, err := os.Stat(filepath.Join(out, "private")); !os.IsNotExist(err) {
		t.Fatal("credential directory must not be copied")
	}
}

// A catalog that states nothing leaves every field at the anchor entry value,
// so Pi keeps the deployment's current configuration rather than a zero one.
func TestModelConfigKeepsAnchorValuesForUnstatedFields(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("Node required for model configuration contract tests")
	}
	dir := t.TempDir()
	shared := filepath.Join(dir, "shared")
	if err := os.Mkdir(shared, 0o700); err != nil {
		t.Fatal(err)
	}
	anchor := `{"providers":{"magpie":{"api":"openai-completions","models":[{"id":"anchor/model","contextWindow":1048576,"maxTokens":393216,"input":["text","image"],"reasoning":true}]}}}`
	if err := os.WriteFile(filepath.Join(shared, "models.json"), []byte(anchor), 0o600); err != nil {
		t.Fatal(err)
	}
	p := batchFixtureProgress()
	p.LayoutVersion = BatchLayoutVersion
	// The catalog states a smaller window; only that field moves.
	p.Model = &BatchModel{ContextWindow: intPtr(32000)}
	script, err := batchScript(p, Target{WorkDir: `C:\Synthetic`, Agent: AgentProfile{Provider: "magpie", Model: "synthetic", ConfigDir: shared}}, `C:\Synthetic`, "task", "small/model")
	if err != nil {
		t.Fatal(err)
	}
	program, spec := filepath.Join(dir, "m.cjs"), filepath.Join(dir, "m.json")
	if err := os.WriteFile(program, extractScriptFile(t, script, "model-config.cjs"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec, extractScriptFile(t, script, "model-config.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "agent")
	//nolint:gosec // Embedded fixed program and synthetic fixture files only.
	if raw, err := exec.CommandContext(t.Context(), node, program, shared, out, spec).CombinedOutput(); err != nil {
		t.Fatalf("model configuration: %v %s", err, raw)
	}
	var cfg struct {
		Providers map[string]struct {
			Models []struct {
				ContextWindow int      `json:"contextWindow"`
				MaxTokens     int      `json:"maxTokens"`
				Input         []string `json:"input"`
			} `json:"models"`
		} `json:"providers"`
	}
	raw := readBatchFixture(t, filepath.Join(out, "models.json"))
	if json.Unmarshal(raw, &cfg) != nil {
		t.Fatal(string(raw))
	}
	chosen := cfg.Providers["magpie"].Models[0]
	if chosen.ContextWindow != 32000 || chosen.MaxTokens != 393216 || len(chosen.Input) != 2 {
		t.Fatalf("unstated capability did not keep the anchor value: %+v", chosen)
	}
}

// A broken shared file must fall back to the deployment configuration instead
// of blocking the task.
func TestModelConfigFailsClosedWithoutBlocking(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("Node required for model configuration contract tests")
	}
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	if err := os.Mkdir(empty, 0o700); err != nil {
		t.Fatal(err)
	}
	p := batchFixtureProgress()
	p.LayoutVersion = BatchLayoutVersion
	p.Model = capabilityFixture()
	script, err := batchScript(p, Target{WorkDir: `C:\Synthetic`, Agent: AgentProfile{Provider: "magpie", Model: "synthetic", ConfigDir: empty}}, `C:\Synthetic`, "task", "vendor/wide-model")
	if err != nil {
		t.Fatal(err)
	}
	program, spec := filepath.Join(dir, "m.cjs"), filepath.Join(dir, "m.json")
	if err := os.WriteFile(program, extractScriptFile(t, script, "model-config.cjs"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec, extractScriptFile(t, script, "model-config.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "agent")
	//nolint:gosec // Embedded fixed program and synthetic fixture files only.
	if raw, err := exec.CommandContext(t.Context(), node, program, empty, out, spec).CombinedOutput(); err == nil {
		t.Fatalf("missing provider was reported as success: %s", raw)
	}
	if !strings.Contains(script, "rm -rf \"$memoh_agent_dir\"") {
		t.Fatal("a failed derivation must remove the per-task directory so Pi keeps the shared configuration")
	}
}

// A model that is already the deployment anchor must not be listed twice: Pi
// resolves --model against the entry list, so a duplicate id is ambiguous.
func TestModelConfigDoesNotDuplicateTheAnchorId(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("Node required for model configuration contract tests")
	}
	dir := t.TempDir()
	shared := filepath.Join(dir, "shared")
	if err := os.Mkdir(shared, 0o700); err != nil {
		t.Fatal(err)
	}
	anchor := `{"providers":{"magpie":{"api":"openai-completions","models":[{"id":"anchor/model","contextWindow":1048576,"maxTokens":393216}]}}}`
	if err := os.WriteFile(filepath.Join(shared, "models.json"), []byte(anchor), 0o600); err != nil {
		t.Fatal(err)
	}
	p := batchFixtureProgress()
	p.LayoutVersion = BatchLayoutVersion
	p.Model = &BatchModel{ContextWindow: intPtr(200000)}
	script, err := batchScript(p, Target{WorkDir: `C:\Synthetic`, Agent: AgentProfile{Provider: "magpie", Model: "synthetic", ConfigDir: shared}}, `C:\Synthetic`, "task", "anchor/model")
	if err != nil {
		t.Fatal(err)
	}
	program, spec := filepath.Join(dir, "m.cjs"), filepath.Join(dir, "m.json")
	if err := os.WriteFile(program, extractScriptFile(t, script, "model-config.cjs"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec, extractScriptFile(t, script, "model-config.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "agent")
	//nolint:gosec // Embedded fixed program and synthetic fixture files only.
	if raw, err := exec.CommandContext(t.Context(), node, program, shared, out, spec).CombinedOutput(); err != nil {
		t.Fatalf("model configuration: %v %s", err, raw)
	}
	var cfg struct {
		Providers map[string]struct {
			Models []struct {
				ID            string `json:"id"`
				ContextWindow int    `json:"contextWindow"`
			} `json:"models"`
		} `json:"providers"`
	}
	raw := readBatchFixture(t, filepath.Join(out, "models.json"))
	if json.Unmarshal(raw, &cfg) != nil {
		t.Fatal(string(raw))
	}
	models := cfg.Providers["magpie"].Models
	if len(models) != 1 || models[0].ID != "anchor/model" || models[0].ContextWindow != 200000 {
		t.Fatalf("chosen anchor entry must be listed once with the stated capability: %+v", models)
	}
}

func extractScriptFile(t *testing.T, script, name string) []byte {
	t.Helper()
	const prefix = `printf '%s' '`
	for _, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, prefix) || !strings.Contains(trimmed, name) || !strings.Contains(trimmed, "base64 --decode >") {
			continue
		}
		rest := strings.TrimPrefix(trimmed, prefix)
		end := strings.Index(rest, `' | base64`)
		if end < 0 {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(rest[:end])
		if err != nil {
			t.Fatalf("%s payload: %v", name, err)
		}
		return raw
	}
	t.Fatalf("script carries no %s payload", name)
	return nil
}
