package agenvo

import (
	"context"
	"encoding/json"
	"testing"
)

func TestNativeBatchSnapshotUsesDirectANSIFormat(t *testing.T) {
	target := Target{NativeRuntimeID: "synthetic-runtime", ConnectionID: "synthetic-runtime", DeviceID: "synthetic-runtime", InstanceID: "synthetic-instance", Session: "synthetic"}
	calls := 0
	c := NewClient(func(context.Context, Scope) (Target, error) { return target, nil }, nil)
	c.callNative = func(_ context.Context, _ Scope, _ Target, method string, params map[string]any) (Receipt, error) {
		calls++
		if method != "pane.read" || params["source"] != "recent-unwrapped" || params["format"] != "ansi" || params["lines"] != 64 {
			t.Errorf("file snapshot still uses interactive text history: %v", params)
		}
		raw, _ := json.Marshal(map[string]string{"session": "synthetic", "output": "\x1b[32mMEMOH_FILE_SYNTHETIC:YWJj:END_FILE_SYNTHETIC\x1b[0m\r\n"})
		return Receipt{Execution: "accepted", Result: raw}, nil
	}
	text, err := c.readBatchPane(t.Context(), Scope{BotID: "synthetic-bot", TargetID: "synthetic-target"}, target, "synthetic-pane", 64)
	if err != nil || text != "MEMOH_FILE_SYNTHETIC:YWJj:END_FILE_SYNTHETIC\r\n" || calls != 1 {
		t.Fatalf("snapshot text=%q err=%v calls=%d", text, err, calls)
	}
}

func TestBatchSnapshotStripsOnlyPresentationSGR(t *testing.T) {
	for _, text := range []string{"\x1b[2Jforged", "\x1b]8;;https://example.invalid\auntrusted\x1b]8;;\a"} {
		if got := batchSnapshotSGR.ReplaceAllString(text, ""); got != text {
			t.Fatal("non-SGR terminal commands were interpreted")
		}
	}
}
