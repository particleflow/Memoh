package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceStorageUsesOriginalContainerConfiguration(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(file, []byte("[container.storage]\nroot = 'synthetic-storage'\nshare_root = '\\\\synthetic-nas\\existing-share'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(cfg.Workspace.Storage.Root) || cfg.Workspace.Storage != cfg.Container.Storage || cfg.Workspace.Storage.ShareRoot != `\\synthetic-nas\existing-share` {
		t.Fatalf("storage configuration: %+v", cfg.Workspace.Storage)
	}
	if err = os.WriteFile(file, []byte("[container.storage]\nroot='one'\n[workspace]\ndata_root='two'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(file); err == nil {
		t.Fatal("mixed storage configuration silently ignored")
	}
}
