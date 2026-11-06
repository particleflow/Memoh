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

func TestOriginalsRootUsesLoadWorkingDirectory(t *testing.T) {
	t.Setenv("MEMOH_ORIGINALS_ROOT", "")
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("[files]\noriginals_root = 'storage/originals'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		cwd := t.TempDir()
		t.Chdir(cwd)
		cfg, err := Load(configPath)
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(cwd, "storage", "originals")
		if cfg.Files.OriginalsRoot != want {
			t.Fatalf("root = %q, want load-time working directory %q", cfg.Files.OriginalsRoot, want)
		}
		// Consumers receive an absolute snapshot, not a path that changes when
		// a later process operation changes the working directory.
		t.Chdir(t.TempDir())
		resolved, err := filepath.Abs(cfg.Files.OriginalsRoot)
		if err != nil || resolved != want {
			t.Fatalf("root changed after Load: %q, %v", resolved, err)
		}
	}
}

func TestOriginalsRootEnvironmentAndLegacyPaths(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, root, want string }{
		{"empty", "", ""},
		{"relative", "./storage/../originals", filepath.Join(cwd, "originals")},
		{"absolute", filepath.Join(cwd, "absolute"), filepath.Join(cwd, "absolute")},
		{"home", "~/memoh-originals", filepath.Join(home, "memoh-originals")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MEMOH_ORIGINALS_ROOT", tc.root)
			// The first-start/missing-config branch must normalize overrides too.
			cfg, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
			if err != nil || cfg.Files.OriginalsRoot != tc.want {
				t.Fatalf("root = %q, want %q, err = %v", cfg.Files.OriginalsRoot, tc.want, err)
			}
		})
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
