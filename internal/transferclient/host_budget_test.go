package transferclient

import (
	"errors"
	"os"
	"testing"
)

func TestHostBudgetConfiguration(t *testing.T) {
	if got, err := LoadHostQueueLimits(func(string) string { return "" }); err != nil || got != DefaultHostQueueLimits() {
		t.Fatal("pre-configuration storage profile", got, err)
	}
	path := configFixtureRoot(t)
	env := func(string) string { return path }
	got, err := LoadHostQueueLimits(env)
	if err != nil || got != DefaultHostQueueLimits() {
		t.Fatal("legacy defaults", got, err)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	for _, raw := range []string{
		`{"version":1,"bytes":8589934592}`,
		`{"version":1,"bytes":0}`, `{"version":1,"bytes":-1}`,
		`{"version":1,"bytes":1099511627777}`, `{"version":2,"bytes":1}`,
		`{"version":1,"bytes":1,"origin":"https://other.invalid"}`,
		`{"version":1,"bytes":"8589934592"}`, `{"version":1}`, `{`,
	} {
		f, err := openPrivateFile(root, "queue-budget.json", os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if err = f.Truncate(0); err == nil {
			_, err = f.WriteString(raw)
		}
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
		got, err := LoadHostQueueLimits(env)
		if raw == `{"version":1,"bytes":8589934592}` {
			want := DefaultHostQueueLimits()
			want.Bytes = 8 << 30
			if err != nil || got != want {
				t.Fatal("8GiB metadata config", got, err)
			}
		} else if !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid policy must not fall back", err)
		}
	}
}
