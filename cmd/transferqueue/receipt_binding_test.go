package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReceiptBindingCommandDoesNotInitializeOrUseRecovery(t *testing.T) {
	base := cliPrivateRoot(t)
	config, state, source := filepath.Join(base, "config"), filepath.Join(base, "state"), filepath.Join(base, "source")
	for _, dir := range []string{config, state, source} {
		if err := cliFixtureDir(dir); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := json.Marshal(map[string]any{"version": 1, "origin": "https://fixture.invalid", "source_root": source})
	if err := os.WriteFile(filepath.Join(config, "transfer.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	env := func(key string) string {
		switch key {
		case "HERDR_PLUGIN_CONFIG_DIR":
			return config
		case "HERDR_PLUGIN_STATE_DIR":
			return state
		}
		return ""
	}
	for _, id := range []string{"original", "", "../outside"} {
		var out, stderr bytes.Buffer
		code := runCommand([]string{"-action", "receipt-binding", "-delivery", id}, strings.NewReader(""), &out, &stderr, env, nil, time.Now)
		var result map[string]string
		if code != 1 || json.Unmarshal(out.Bytes(), &result) != nil || result["state"] != "receipt_binding_unavailable" {
			t.Fatal("read-only command was not dispatched/refused", code, out.String())
		}
		files, err := os.ReadDir(state)
		if err != nil || len(files) != 0 {
			t.Fatal("receipt query initialized state", files, err)
		}
		got, err := os.ReadFile(filepath.Join(config, "transfer.json"))
		if err != nil || !bytes.Equal(got, raw) || stderr.Len() != 0 {
			t.Fatal("read changed configuration or emitted raw diagnostics", err)
		}
	}
}
