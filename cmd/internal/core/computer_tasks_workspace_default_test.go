package core

import (
	"context"
	"errors"
	"testing"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/computertask"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/toolcontext"
)

func TestComputerTaskWorkspaceDefaultBindingAndRoots(t *testing.T) {
	const root = `\\synthetic-nas\existing-share\computer`
	for _, tc := range []struct {
		name, selected, targetRoot, want, mutate string
		wantErr                                  error
	}{
		{name: "absent selection preserves local", targetRoot: `C:\Synthetic`},
		{name: "selected root preserves dot artifact semantics", selected: root, targetRoot: root, want: "."},
		{name: "selected child", selected: root + `\project`, targetRoot: root, want: root + `\project`},
		{name: "local root not broadened", selected: root, targetRoot: `C:\Synthetic`, wantErr: agenvo.ErrUnsupported},
		{name: "different registered Runtime", targetRoot: root, mutate: "runtime", wantErr: computertask.ErrDenied},
		{name: "different Bot", targetRoot: root, mutate: "bot", wantErr: computertask.ErrDenied},
		{name: "different owner", targetRoot: root, mutate: "owner", wantErr: computertask.ErrDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mount := dbstore.BotRemoteRuntimeBindingRecord{ID: "target", BotID: "bot", RuntimeID: "runtime", RuntimeProvider: "memoh", RuntimeUserID: "owner", BotOwnerUserID: "owner"}
			switch tc.mutate {
			case "runtime":
				mount.RuntimeID = "other"
			case "bot":
				mount.BotID = "other"
			case "owner":
				mount.RuntimeUserID = "other"
			}
			lookups := 0
			resolve := computerTaskWorkspaceDefault(func(context.Context, string, string) (dbstore.BotRemoteRuntimeBindingRecord, error) {
				return mount, nil
			}, func(_ context.Context, owner, runtime string) (string, error) {
				lookups++
				if owner != "owner" || runtime != "runtime" {
					t.Fatal("lookup used caller-supplied owner or Runtime")
				}
				return tc.selected, nil
			})
			got, err := resolve(t.Context(), toolcontext.ComputerBinding{BotID: "bot"}, "target", agenvo.Target{NativeRuntimeID: "runtime", WorkDir: tc.targetRoot})
			if !errors.Is(err, tc.wantErr) || got != tc.want || tc.mutate != "" && lookups != 0 {
				t.Fatalf("directory=%q err=%v lookups=%d", got, err, lookups)
			}
		})
	}
}
