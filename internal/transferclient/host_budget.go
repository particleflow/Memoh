package transferclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
)

// DefaultHostQueueLimits is the historical installed-command profile. The
// library accepts limits explicitly; only the CLI resolves user configuration.
// Bytes remains in the immutable legacy locator; it does not limit registered
// upload sources, download sizes/backlog, or retained native download objects.
// Downloads use actual filesystem space and the existing worker locks.
func DefaultHostQueueLimits() QueueLimits {
	return QueueLimits{Jobs: 8, Bytes: 256 << 20, JournalBytes: 8 << 20, Workers: 1}
}

// LoadHostQueueLimits reads one optional policy file from the same private Herdr
// CONFIG_DIR. It does not rewrite transfer.json: those exact bytes remain the
// existing task/endpoint binding. Only an absent file selects legacy defaults.
func LoadHostQueueLimits(getenv func(string) string) (QueueLimits, error) {
	limits := DefaultHostQueueLimits()
	if getenv == nil {
		return QueueLimits{}, ErrBinding
	}
	path := getenv("HERDR_PLUGIN_CONFIG_DIR")
	if path == "" {
		// Preserve pre-configuration storage probes/history maintenance. Network
		// and producer actions still require OpenHostConfig and its private root.
		return limits, nil
	}
	root, guard, err := privateHostRoot(path)
	if err != nil {
		return QueueLimits{}, err
	}
	h := &HostConfig{root: root, guard: guard}
	defer func() { _ = h.Close() }()
	if _, err := root.Lstat("queue-budget.json"); errors.Is(err, os.ErrNotExist) {
		return limits, nil
	} else if err != nil {
		return QueueLimits{}, ErrBinding
	}
	raw, err := h.readPrivate("queue-budget.json", 1024)
	if err != nil {
		return QueueLimits{}, err
	}
	var cfg struct {
		Version int   `json:"version"`
		Bytes   int64 `json:"bytes"`
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&cfg) != nil || d.Decode(new(any)) != io.EOF || cfg.Version != 1 || cfg.Bytes < 1 || cfg.Bytes > 1<<40 {
		return QueueLimits{}, ErrInvalid
	}
	limits.Bytes = cfg.Bytes
	return limits, nil
}
