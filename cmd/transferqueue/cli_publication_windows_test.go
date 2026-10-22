//go:build windows

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/transferclient"
)

func TestCLIPathPublicationMigration(t *testing.T) {
	state := cliPrivateRoot(t)
	env := func(key string) string {
		if key == "HERDR_PLUGIN_STATE_DIR" {
			return state
		}
		return ""
	}
	limits := transferclient.QueueLimits{Jobs: 8, Bytes: 256 << 20, JournalBytes: 8 << 20, Workers: 1}
	q, err := transferclient.OpenHerdrQueue(env, "native-profile-v1", limits)
	if err != nil {
		t.Fatal(err)
	}
	p, err := q.OpenNativePublication(func(ctx context.Context, _ transferclient.Reference) error { return ctx.Err() })
	if err != nil {
		t.Fatal(err)
	}
	_ = p.Close()
	_ = q.Close()
	f, err := os.OpenFile(filepath.Join(state, "deliveries-v1", "publications-v1", "locator"), os.O_RDWR, 0) //nolint:gosec // Existing private fixture locator; no owner/ACL change.
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteAt([]byte{'1'}, int64(len(`{"version":`)))
	if err == nil {
		err = f.Sync()
	}
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		var out, stderr bytes.Buffer
		code := runCommand([]string{"-action", "migrate-publications"}, strings.NewReader(""), &out, &stderr, env, nil, time.Now)
		if code != 0 || strings.TrimSpace(out.String()) != `{"publication_layout":2,"migration":"complete"}` {
			t.Fatal("publication maintenance CLI", code, out.String(), stderr.String())
		}
	}
}
