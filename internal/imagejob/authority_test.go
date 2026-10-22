package imagejob

import (
	"context"
	"testing"
)

func TestOriginSnapshotsHostInputsAndCannotBeReplacedByChild(t *testing.T) {
	inputs := []string{"host-original"}
	ctx := WithOrigin(context.Background(), Origin{BotID: "bot", UserID: "user", SessionID: "root", RunID: "root-run", InputRefs: inputs})
	inputs[0] = "changed-after-admission"
	ctx = WithOrigin(ctx, Origin{BotID: "bot", UserID: "user", SessionID: "child", RunID: "child-run", InputRefs: []string{"untrusted-child-input"}})
	origin := ctx.Value(originKey{}).(Origin)
	if origin.SessionID != "root" || origin.RunID != "root-run" || len(origin.InputRefs) != 1 || origin.InputRefs[0] != "host-original" {
		t.Fatal("child or mutable caller replaced trusted root inputs")
	}
}
