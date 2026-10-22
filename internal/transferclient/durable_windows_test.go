//go:build windows

package transferclient

import (
	"errors"
	"testing"
)

func TestUnverifiedWindowsHostFailsBeforeFilesystemAccess(t *testing.T) {
	_, err := OpenHerdrQueue(func(string) string { t.Fatal("unverified Windows binding accessed environment"); return "" }, "revision-1", QueueLimits{Jobs: 8, Bytes: 32 << 20, JournalBytes: 1 << 20, Workers: 1})
	if !errors.Is(err, ErrBinding) {
		t.Fatal("unverified native host was enabled", err)
	}
}
