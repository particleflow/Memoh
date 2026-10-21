package federation

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	mcpgw "github.com/felinics/memoh/internal/mcp"
)

type testConnectionLister struct {
	items []mcpgw.Connection
	err   error
}

func (l *testConnectionLister) ListActiveByBot(_ context.Context, _ string) ([]mcpgw.Connection, error) {
	if l.err != nil {
		return nil, l.err
	}
	return l.items, nil
}

type testGateway struct {
	listHTTP  []mcpgw.ToolDescriptor
	listSSE   []mcpgw.ToolDescriptor
	listStdio []mcpgw.ToolDescriptor

	lastCallType string
}

func (g *testGateway) ListHTTPConnectionTools(_ context.Context, _ mcpgw.Connection) ([]mcpgw.ToolDescriptor, error) {
	return g.listHTTP, nil
}

func (g *testGateway) CallHTTPConnectionTool(_ context.Context, _ mcpgw.Connection, _ string, _ map[string]any) (map[string]any, error) {
	g.lastCallType = "http"
	return map[string]any{"result": map[string]any{"ok": true, "route": "http"}}, nil
}

func (g *testGateway) ListSSEConnectionTools(_ context.Context, _ mcpgw.Connection) ([]mcpgw.ToolDescriptor, error) {
	return g.listSSE, nil
}

func (g *testGateway) CallSSEConnectionTool(_ context.Context, _ mcpgw.Connection, _ string, _ map[string]any) (map[string]any, error) {
	g.lastCallType = "sse"
	return map[string]any{"result": map[string]any{"ok": true, "route": "sse"}}, nil
}

func (g *testGateway) ListStdioConnectionTools(_ context.Context, _ string, _ mcpgw.Connection) ([]mcpgw.ToolDescriptor, error) {
	return g.listStdio, nil
}

func (g *testGateway) CallStdioConnectionTool(_ context.Context, _ string, _ mcpgw.Connection, _ string, _ map[string]any) (map[string]any, error) {
	g.lastCallType = "stdio"
	return map[string]any{"result": map[string]any{"ok": true, "route": "stdio"}}, nil
}

func TestSourceListToolsIncludesSSETools(t *testing.T) {
	gateway := &testGateway{
		listSSE: []mcpgw.ToolDescriptor{
			{
				Name:        "search",
				Description: "search remote data",
				InputSchema: map[string]any{"type": "object"},
			},
		},
	}
	lister := &testConnectionLister{
		items: []mcpgw.Connection{
			{
				ID:     "conn-1",
				Name:   "Remote SSE",
				Type:   "sse",
				Active: true,
				Config: map[string]any{"url": "http://example.com/sse"},
			},
		},
	}

	source := NewSource(slog.Default(), gateway, lister)
	tools, err := source.ListTools(context.Background(), mcpgw.ToolSessionContext{BotID: "bot-1"})
	if err != nil {
		t.Fatalf("list tools failed: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "remote_sse_search" {
		t.Fatalf("unexpected tool alias: %s", tools[0].Name)
	}
}

func TestSourceCallToolRoutesToSSEConnection(t *testing.T) {
	gateway := &testGateway{
		listSSE: []mcpgw.ToolDescriptor{
			{
				Name:        "search",
				Description: "search remote data",
				InputSchema: map[string]any{"type": "object"},
			},
		},
	}
	lister := &testConnectionLister{
		items: []mcpgw.Connection{
			{
				ID:     "conn-1",
				Name:   "Remote SSE",
				Type:   "sse",
				Active: true,
				Config: map[string]any{"url": "http://example.com/sse"},
			},
		},
	}
	source := NewSource(slog.Default(), gateway, lister)

	result, err := source.CallTool(context.Background(), mcpgw.ToolSessionContext{BotID: "bot-1"}, "remote_sse_search", map[string]any{"query": "hello"})
	if err != nil {
		t.Fatalf("call tool failed: %v", err)
	}
	if gateway.lastCallType != "sse" {
		t.Fatalf("expected sse route, got: %s", gateway.lastCallType)
	}
	if ok, _ := result["ok"].(bool); !ok {
		t.Fatalf("expected ok=true in result")
	}
}

func TestSourceCallToolRechecksRuntimeGuardBeforeFederatedEffect(t *testing.T) {
	guardErr := errors.New("runtime ownership lost")
	gateway := &testGateway{listSSE: []mcpgw.ToolDescriptor{{
		Name: "search", InputSchema: map[string]any{"type": "object"},
	}}}
	lister := &testConnectionLister{items: []mcpgw.Connection{{
		ID: "conn-guard", Name: "Remote SSE", Type: "sse", Active: true,
		Config: map[string]any{"url": "http://example.com/sse"},
	}}}
	source := NewSource(slog.Default(), gateway, lister)
	if _, err := source.ListTools(context.Background(), mcpgw.ToolSessionContext{BotID: "bot-1"}); err != nil {
		t.Fatalf("prime routes: %v", err)
	}

	_, err := source.CallTool(context.Background(), mcpgw.ToolSessionContext{
		BotID: "bot-1", RuntimeGuard: func(context.Context) error { return guardErr },
	}, "remote_sse_search", nil)
	if !errors.Is(err, guardErr) {
		t.Fatalf("CallTool() error = %v, want runtime guard error", err)
	}
	if gateway.lastCallType != "" {
		t.Fatalf("federated effect executed through %q after guard rejection", gateway.lastCallType)
	}
}

func TestSourceRenamesReservedToolAliases(t *testing.T) {
	gateway := &testGateway{
		listSSE: []mcpgw.ToolDescriptor{
			{
				Name:        "observe",
				Description: "observe browser state",
				InputSchema: map[string]any{"type": "object"},
			},
		},
	}
	lister := &testConnectionLister{
		items: []mcpgw.Connection{
			{
				ID:     "conn-1",
				Name:   "browser",
				Type:   "sse",
				Active: true,
				Config: map[string]any{"url": "http://example.com/sse"},
			},
		},
	}
	source := NewSource(slog.Default(), gateway, lister, WithReservedToolName(func(name string) bool {
		return name == "browser_observe"
	}))

	tools, err := source.ListTools(context.Background(), mcpgw.ToolSessionContext{BotID: "bot-1"})
	if err != nil {
		t.Fatalf("list tools failed: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "browser_observe_2" {
		t.Fatalf("tools = %#v, want reserved alias renamed to browser_observe_2", tools)
	}

	result, err := source.CallTool(context.Background(), mcpgw.ToolSessionContext{BotID: "bot-1"}, "browser_observe_2", map[string]any{})
	if err != nil {
		t.Fatalf("call renamed tool failed: %v", err)
	}
	if route, _ := result["route"].(string); route != "sse" {
		t.Fatalf("result = %#v, want sse route", result)
	}
}

func TestCachedRouteCannotCallRemovedOrReconfiguredConnection(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "removed-or-disabled", true: "reconfigured"}[changed], func(t *testing.T) {
			gateway := &testGateway{listHTTP: []mcpgw.ToolDescriptor{{Name: "search"}}}
			lister := &testConnectionLister{items: []mcpgw.Connection{{ID: "connection", Name: "Remote", Type: "http", Active: true, Config: map[string]any{"url": "https://example.com/mcp"}}}}
			source := NewSource(slog.Default(), gateway, lister)
			session := mcpgw.ToolSessionContext{BotID: "bot"}
			if _, err := source.ListTools(t.Context(), session); err != nil {
				t.Fatal(err)
			}
			if changed {
				lister.items[0].UpdatedAt = time.Now()
			} else {
				lister.items = nil
			}
			if _, err := source.CallTool(t.Context(), session, "remote_search", nil); !errors.Is(err, mcpgw.ErrToolNotFound) {
				t.Fatalf("stale call error: %v", err)
			}
			if gateway.lastCallType != "" {
				t.Fatal("stale route reached remote server")
			}
		})
	}
}

type endpointGateway struct {
	testGateway
	checks int
	t      *testing.T
}

func (g *endpointGateway) check(ctx context.Context) {
	g.t.Helper()
	g.checks++
	if err := mcpgw.CheckEndpoint(ctx, "https://synthetic.invalid/managed"); !errors.Is(err, mcpgw.ErrToolNotFound) {
		g.t.Fatal("transport lost actual endpoint guard", err)
	}
}

func (g *endpointGateway) ListHTTPConnectionTools(ctx context.Context, c mcpgw.Connection) ([]mcpgw.ToolDescriptor, error) {
	g.check(ctx)
	return g.testGateway.ListHTTPConnectionTools(ctx, c)
}

func (g *endpointGateway) CallHTTPConnectionTool(ctx context.Context, c mcpgw.Connection, n string, a map[string]any) (map[string]any, error) {
	g.check(ctx)
	return g.testGateway.CallHTTPConnectionTool(ctx, c, n, a)
}

func TestComputerEndpointGuardSurvivesFederationListAndCall(t *testing.T) {
	g := &endpointGateway{t: t, testGateway: testGateway{listHTTP: []mcpgw.ToolDescriptor{{Name: "echo", InputSchema: map[string]any{"type": "object"}}}}}
	l := &testConnectionLister{items: []mcpgw.Connection{{ID: "alias", Name: "safe", Type: "http", Active: true, Config: map[string]any{"url": "https://synthetic.invalid/safe"}}}}
	source := NewSource(nil, g, l, WithConnectionCheck(func(_ context.Context, c mcpgw.Connection) error {
		if c.Config["url"] == "https://synthetic.invalid/managed" {
			return mcpgw.ErrToolNotFound
		}
		return nil
	}))
	session := mcpgw.ToolSessionContext{BotID: "bot"}
	listed, err := source.ListTools(t.Context(), session)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list: %v %v", listed, err)
	}
	if _, err = source.CallTool(t.Context(), session, listed[0].Name, nil); err != nil {
		t.Fatal(err)
	}
	if g.checks != 2 {
		t.Fatalf("checks=%d", g.checks)
	}
}
