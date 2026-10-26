package core

import (
	"context"
	"strings"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/computertask"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/toolcontext"
)

func computerTaskWorkspaceDefault(getMount func(context.Context, string, string) (dbstore.BotRemoteRuntimeBindingRecord, error), selectedDirectory func(context.Context, string, string) (string, error)) computertask.DefaultWorkDir {
	return func(ctx context.Context, b toolcontext.ComputerBinding, id string, target agenvo.Target) (string, error) {
		mount, err := getMount(ctx, b.BotID, id)
		if err != nil || mount.ID != id || mount.BotID != b.BotID || mount.RuntimeProvider != "memoh" || mount.RuntimeRevoked || mount.RuntimeID != target.NativeRuntimeID || mount.RuntimeUserID != mount.BotOwnerUserID {
			return "", computertask.ErrDenied
		}
		selected, err := selectedDirectory(ctx, mount.RuntimeUserID, mount.RuntimeID)
		if err != nil || selected == "" {
			return selected, err
		}
		// The existing lexical root guard remains authoritative. Never broaden
		// a local target to a NAS root simply because settings were saved.
		if err = agenvo.ValidateWorkDir(target.WorkDir, selected); err != nil {
			return "", err
		}
		if strings.EqualFold(strings.TrimRight(strings.ReplaceAll(target.WorkDir, "/", `\`), `\`), strings.TrimRight(selected, `\`)) {
			return ".", nil
		}
		return selected, nil
	}
}
