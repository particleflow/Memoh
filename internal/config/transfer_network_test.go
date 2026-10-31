package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilesTransferNetworkConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, policy string
		valid        bool
	}{
		{"omitted", "", true},
		{"ordered", "[files.network_policy]\nversion = 1\nlan_addresses = ['192.0.2.1', '2001:db8::1']\n", true},
		{"empty", "[files.network_policy]\nversion = 1\nlan_addresses = []\n", false},
		{"authority", "[files.network_policy]\nversion = 1\nlan_addresses = ['https://other.invalid:443']\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte("[files]\noriginals_root = '/isolated/originals'\n"+tc.policy), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if (err == nil) != tc.valid {
				t.Fatal(err)
			}
			if err == nil && cfg.Files.OriginalsRoot != "/isolated/originals" {
				t.Fatal("storage binding changed")
			}
		})
	}
}
