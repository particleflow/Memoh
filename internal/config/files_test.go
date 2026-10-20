package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOriginalsConfigurationAndEnvironmentOverride(t *testing.T) {
	t.Setenv("MEMOH_ORIGINALS_ROOT", "")
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("[files]\noriginals_root = '/srv/test-originals'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil || cfg.Files.OriginalsRoot != "/srv/test-originals" {
		t.Fatalf("config original root: %+v, %v", cfg.Files, err)
	}
	t.Setenv("MEMOH_ORIGINALS_ROOT", "/mounted/originals")
	cfg, err = Load(configPath)
	if err != nil || cfg.Files.OriginalsRoot != "/mounted/originals" {
		t.Fatalf("env original root: %+v, %v", cfg.Files, err)
	}
}
