package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilesTransferEndpointConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "endpoint.toml")
	raw := []byte("[files]\noriginals_root='/isolated/originals'\n[files.network_policy]\nversion=2\n[files.network_policy.endpoint]\nlogical_origin='https://nas.example.invalid'\nphysical_origin='http://127.0.0.1:18068'\n")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil || cfg.Files.NetworkPolicy.Endpoint.PhysicalOrigin != "http://127.0.0.1:18068" || cfg.Files.OriginalsRoot != "/isolated/originals" {
		t.Fatal("endpoint/storage config changed", err)
	}
}
