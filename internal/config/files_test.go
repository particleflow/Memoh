package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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

func TestFileUploadLimitConfiguration(t *testing.T) {
	for _, limit := range []int64{0, 1, 256 * 1024 * 1024, -1, 1 << 53} {
		t.Run(strconv.FormatInt(limit, 10), func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(filename, fmt.Appendf(nil, "[files]\nmax_upload_bytes = %d\n", limit), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(filename)
			if limit < 0 || limit >= 1<<53 {
				if err == nil {
					t.Fatal("invalid upload limit accepted")
				}
			} else if err != nil || cfg.Files.MaxUploadBytes != limit {
				t.Fatalf("limit=%d: %+v, %v", limit, cfg.Files, err)
			}
		})
	}
}
