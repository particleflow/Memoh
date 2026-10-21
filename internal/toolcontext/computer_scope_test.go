package toolcontext

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestComputerScopeCannotBeBroadenedByChildContext(t *testing.T) {
	root := WithComputerScope(t.Context(), "pc-a")
	for _, replacement := range []string{"", "native", "pc-b", "pc-a"} {
		ctx := WithComputerScope(root, replacement)
		if ComputerScope(ctx) != "pc-a" || !errors.Is(CheckComputerTarget(ctx, "pc-b"), ErrComputerScope) || !errors.Is(CheckComputerTarget(ctx, "native"), ErrComputerScope) {
			t.Fatalf("scope broadened by %q", replacement)
		}
		if CheckComputerTarget(ctx, "pc-a") != nil || CheckComputerTarget(ctx, "") != nil {
			t.Fatal("selected/default target rejected")
		}
	}
	if CheckComputerTarget(WithComputerScope(t.Context(), "native"), "pc-b") != nil {
		t.Fatal("NAS scope must leave Bot grant validation to the target resolver")
	}
}

func TestComputerScopeBindAndMerge(t *testing.T) {
	base := Session{WorkspaceTargetID: "pc-a", WorkspaceTargetKind: "remote", WorkdirPath: "synthetic"}
	for _, run := range []context.Context{nil, t.Context(), WithComputerScope(t.Context(), "pc-a")} {
		bound, cancel := Bind(t.Context(), Session{WorkspaceTargetID: "pc-a", RunContext: run})
		if ComputerScope(bound) != "pc-a" {
			t.Fatal("Bind lost computer scope")
		}
		cancel()
	}
	merged := Merge(base, Session{SessionID: "new-session"})
	if merged.WorkspaceTargetID != "pc-a" || merged.WorkdirPath != "synthetic" {
		t.Fatal("partial session merge dropped scope")
	}
	merged = Merge(base, Session{PublicRequest: true, WorkspaceTargetID: "pc-b", WorkdirPath: "other"})
	if merged.WorkspaceTargetID != "pc-a" || merged.WorkdirPath != "synthetic" {
		t.Fatal("public merge overrode authoritative workspace")
	}
	encoded, err := json.Marshal(base)
	if err != nil || strings.Contains(string(encoded), "pc-a") || strings.Contains(string(encoded), "synthetic") {
		t.Fatal("runtime-only workspace fields escaped serialization")
	}
}
