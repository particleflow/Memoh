package federation

import (
	"context"
	"errors"
	"testing"

	"github.com/felinics/memoh/internal/mcp"
	"github.com/felinics/memoh/internal/toolcontext"
)

func TestComputerConnectionCannotEscapeViaCachedRawRoute(t *testing.T) {
	gateway := &testGateway{listHTTP: []mcp.ToolDescriptor{
		{Name: "search", InputSchema: map[string]any{"type": "object"}},
		{Name: "execute", InputSchema: map[string]any{"type": "object"}},
	}}
	lister := &testConnectionLister{items: []mcp.Connection{
		{ID: "computer-connection", Name: "Computer", Type: "http", Active: true},
		{ID: "ordinary-connection", Name: "Ordinary", Type: "http", Active: true},
	}}
	managed := false
	policyErr := error(nil)
	source := NewSource(nil, gateway, lister, WithConnectionCheck(func(_ context.Context, connection mcp.Connection) error {
		if policyErr != nil {
			return policyErr
		}
		if managed && connection.ID == "computer-connection" {
			return mcp.ErrToolNotFound
		}
		return nil
	}))
	session := mcp.ToolSessionContext{BotID: "bot-a"}
	listed, err := source.ListTools(t.Context(), session)
	if err != nil || len(listed) != 4 {
		t.Fatalf("prime connection cache: %d %v", len(listed), err)
	}
	managed = true
	listed, err = source.ListTools(t.Context(), session)
	if err != nil || len(listed) != 2 || listed[0].Name != "ordinary_search" || listed[1].Name != "ordinary_execute" {
		t.Fatalf("cached computer tools exposed, or unrelated connection blocked: %+v %v", listed, err)
	}
	for _, name := range []string{"computer_search", "computer_execute"} {
		if _, err := source.CallTool(t.Context(), session, name, map[string]any{"code": "arbitrary relay code"}); !errors.Is(err, mcp.ErrToolNotFound) {
			t.Fatalf("cached route %s bypassed managed connection: %v", name, err)
		}
	}
	if gateway.lastCallType != "" {
		t.Fatal("blocked connection dispatched a raw call")
	}
	if _, err := source.CallTool(t.Context(), session, "ordinary_search", nil); err != nil || gateway.lastCallType != "http" {
		t.Fatalf("ordinary connection changed: %v", err)
	}
	policyErr = errors.New("synthetic registry unavailable")
	gateway.lastCallType = ""
	if _, err := source.ListTools(t.Context(), session); !errors.Is(err, policyErr) {
		t.Fatal("registry failure exposed cached tools")
	}
	if _, err := source.CallTool(t.Context(), session, "ordinary_execute", nil); !errors.Is(err, policyErr) || gateway.lastCallType != "" {
		t.Fatal("registry failure dispatched through cached route")
	}
}

func TestPCMountCannotUseRawFederationDespiteHeadersOrCachedNASRoutes(t *testing.T) {
	gateway := &testGateway{listHTTP: []mcp.ToolDescriptor{{Name: "execute", InputSchema: map[string]any{"type": "object"}}}}
	lister := &testConnectionLister{items: []mcp.Connection{{ID: "raw", Name: "Computer", Type: "http", Active: true}}}
	source := NewSource(nil, gateway, lister, WithConnectionCheck(func(context.Context, mcp.Connection) error { return nil }))
	nas := mcp.ToolSessionContext{BotID: "bot"}
	if tools, err := source.ListTools(t.Context(), nas); err != nil || len(tools) != 1 {
		t.Fatal(err)
	}
	pc := mcp.ToolSessionContext{BotID: "bot", RunContext: toolcontext.WithComputerScope(t.Context(), "pc-a")}
	if tools, err := source.ListTools(t.Context(), pc); err != nil || len(tools) != 0 {
		t.Fatalf("PC catalog leak: %+v %v", tools, err)
	}
	if _, err := source.CallTool(t.Context(), pc, "computer_execute", map[string]any{"code": "arbitrary"}); !errors.Is(err, mcp.ErrToolNotFound) || gateway.lastCallType != "" {
		t.Fatal("cached raw route escaped PC binding")
	}
	if tools, err := source.ListTools(t.Context(), nas); err != nil || len(tools) != 1 {
		t.Fatal("PC catalog polluted NAS cache")
	}
}
