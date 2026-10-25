//go:build integration

package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/config"
	memohdb "github.com/felinics/memoh/internal/db"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	mcpgw "github.com/felinics/memoh/internal/mcp"
	"github.com/felinics/memoh/internal/mcp/sources/federation"
	"github.com/felinics/memoh/internal/toolcontext"
	"github.com/felinics/memoh/internal/userruntime"
	"github.com/felinics/memoh/internal/workspace"
)

type cutoverPermission struct{ bot, owner string }

func (p cutoverPermission) HasBotPermission(_ context.Context, bot, user, permission string) (bool, error) {
	return bot == p.bot && user == p.owner && permission == "workspace_exec", nil
}

type cutoverGateway struct {
	federation.Gateway
	lists, calls int
}

func (g *cutoverGateway) ListHTTPConnectionTools(context.Context, mcpgw.Connection) ([]mcpgw.ToolDescriptor, error) {
	g.lists++
	return []mcpgw.ToolDescriptor{{Name: "synthetic", InputSchema: map[string]any{"type": "object"}}}, nil
}

func (g *cutoverGateway) CallHTTPConnectionTool(context.Context, mcpgw.Connection, string, map[string]any) (map[string]any, error) {
	g.calls++
	return nil, errors.New("retired transport reached")
}

func TestNativeRuntimeCutoverStopsLegacyAutomaticOAuthPaths(t *testing.T) {
	f := newNativeHerdrFixture(t)
	var legacyRuntime, legacyMount string
	var previous []byte
	if err := f.conn.QueryRow(t.Context(), `SELECT r.id,b.id,r.provider_config FROM user_runtimes r JOIN bot_remote_runtime_bindings b ON b.runtime_id=r.id WHERE r.user_id=$1 AND r.provider='agenvo'`, f.owner).Scan(&legacyRuntime, &legacyMount, &previous); err != nil {
		t.Fatal(err)
	}
	var oauthRequests atomic.Int32
	oauthServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		oauthRequests.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer oauthServer.Close()
	queries := postgresstore.NewQueries(f.store.SQLC())
	connections := mcpgw.NewConnectionService(nil, queries)
	connection, err := connections.Create(t.Context(), f.bot, mcpgw.UpsertRequest{Name: "legacy-agenvo", URL: oauthServer.URL + "/mcp", AuthType: "oauth"})
	if err != nil {
		t.Fatal(err)
	}
	legacy := agenvo.Target{ConnectionID: connection.ID, DeviceID: "synthetic-device", InstanceID: "synthetic-herdr", Session: "legacy", WorkDir: `C:\Synthetic`, BashPath: `C:\Synthetic\Git\bin\bash.exe`}
	raw, _ := json.Marshal(legacy)
	if _, err = f.store.ConfigureAgenvoRuntime(t.Context(), f.owner, legacyRuntime, previous, raw); err != nil {
		t.Fatal(err)
	}
	f.exec(t, `INSERT INTO mcp_oauth_tokens(connection_id,token_endpoint,access_token,refresh_token,expires_at) VALUES($1,$2,'synthetic-access','synthetic-refresh',now()-interval '1 hour')`, connection.ID, oauthServer.URL+"/token")
	f.exec(t, `UPDATE user_runtimes SET transfer_credential_id='synthetic-transfer-id',transfer_credential_digest='synthetic-transfer-digest' WHERE id=$1`, legacyRuntime)
	profile := agenvo.NativeHerdrProfile{InstanceID: "synthetic-native-instance", Session: "native", WorkDir: `C:\Synthetic`, BashPath: `C:\Synthetic\Git\bin\bash.exe`, HerdrCLI: `C:\Synthetic\herdr.exe`, HerdrConfigRoot: `C:\Synthetic\herdr`, Agent: agenvo.AgentProfile{ExecutionMode: "pi-json-v1", Provider: "synthetic", Model: "synthetic", ConfigDir: `C:\Synthetic\config`}}
	nativeConfig, _ := json.Marshal(map[string]any{"herdr": profile})
	if _, err = f.store.ConfigureNativeHerdrRuntime(t.Context(), f.owner, f.runtime, f.config, nativeConfig); err != nil {
		t.Fatal(err)
	}
	// Keep an old bound workdir/session and an unsettled task. Disabling must
	// not invoke the mount deletion cascade or redirect this original history.
	workdirID := uuid.NewString()
	f.exec(t, `INSERT INTO bot_workdirs(id,bot_id,name,target_kind,remote_binding_id,path) VALUES($1,$2,'legacy-workdir','remote',$3,'C:\Synthetic')`, workdirID, f.bot, legacyMount)
	f.exec(t, `UPDATE bot_sessions SET workdir_id=$2 WHERE id=$1`, f.session, workdirID)
	original := f.operation(legacyMount)
	original.TargetFingerprint = agenvo.TargetFingerprint(legacy)
	original, won, err := f.store.ClaimComputerOperation(t.Context(), original)
	if err != nil || !won {
		t.Fatal(won, err)
	}
	original.State = "unknown"
	original, err = f.store.SaveComputerOperation(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	legacyCalls, nativeCalls := 0, 0
	relay := agenvo.NewService(f.store, cutoverPermission{bot: f.bot, owner: f.owner}, func(context.Context, string, string, string, map[string]any) (json.RawMessage, error) {
		legacyCalls++
		return nil, agenvo.ErrAuthenticationUnavailable
	})
	relay.SetNativeCaller(func(_ context.Context, scope agenvo.Scope, target agenvo.Target, method string, _ map[string]any) (agenvo.Receipt, error) {
		nativeCalls++
		if scope.TargetID != f.mount || target.NativeRuntimeID != f.runtime || method != "workspace.list" {
			t.Fatal("native route changed identity", scope, method)
		}
		body, _ := json.Marshal(map[string]any{"session": profile.Session, "result": map[string]any{"workspaces": []any{}}})
		return agenvo.Receipt{Execution: "accepted", Result: body}, nil
	})
	owners := userruntime.NewService(f.store, nil)
	owners.SetAgenvoService(relay)
	remotes := workspace.NewRemoteWorkspaceService(f.store, owners)
	remotes.SetAgenvoService(relay)
	gateway := &cutoverGateway{}
	source := federation.NewSource(nil, gateway, connections)
	session := mcpgw.ToolSessionContext{BotID: f.bot}
	discovered, err := source.ListTools(t.Context(), session)
	if err != nil || len(discovered) != 1 || gateway.lists != 1 {
		t.Fatal("active positive control", len(discovered), gateway.lists, err)
	}
	if _, err = owners.ListRuntimes(t.Context(), f.owner); err != nil || legacyCalls != 1 {
		t.Fatal("active owner positive control", legacyCalls, err)
	}
	// Same endpoint and auth type: the existing update preserves all OAuth rows.
	inactive := false
	disabled, err := connections.Update(t.Context(), f.bot, connection.ID, mcpgw.UpsertRequest{Name: connection.Name, URL: oauthServer.URL + "/mcp", Active: &inactive})
	if err != nil || disabled.Active || disabled.AuthType != "oauth" {
		t.Fatal("disable", disabled.Active, disabled.AuthType, err)
	}
	legacyCalls = 0
	if err = remotes.SetPrimary(t.Context(), f.bot, f.mount); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		listed, err := owners.ListRuntimes(t.Context(), f.owner)
		if err != nil {
			t.Fatal(err)
		}
		for _, runtime := range listed {
			if runtime.ID == legacyRuntime && runtime.Availability != "connection_unavailable" {
				t.Fatal("disabled legacy asks for auth", runtime.Availability)
			}
		}
		targets, err := remotes.ListMounts(t.Context(), f.bot)
		if err != nil || len(targets) != 2 {
			t.Fatal(len(targets), err)
		}
		for _, target := range targets {
			if target.TargetID == legacyMount && target.Status != "connection_unavailable" {
				t.Fatal("disabled legacy target probed", target.Status)
			}
		}
	}
	manager := workspace.NewManager(slog.Default(), nil, nil, config.WorkspaceConfig{}, "", nil)
	manager.SetRemoteWorkspaceService(remotes)
	selected := toolcontext.WithComputerScope(t.Context(), f.mount)
	targets, err := manager.ListWorkspaceTargets(selected, f.bot)
	if err != nil || len(targets) != 1 || targets[0].TargetID != f.mount {
		t.Fatal("selected native inventory", targets, err)
	}
	// A cached MCP route rechecks active state before reaching the gateway.
	if _, err = source.CallTool(t.Context(), session, discovered[0].Name, nil); !errors.Is(err, mcpgw.ErrToolNotFound) {
		t.Fatal("disabled cached MCP route survived", err)
	}
	source = federation.NewSource(nil, gateway, connections)
	if tools, err := source.ListTools(t.Context(), session); err != nil || len(tools) != 0 {
		t.Fatal("restart rediscovered retired MCP", len(tools), err)
	}
	if status, err := mcpgw.NewOAuthService(nil, queries, "").GetStatus(t.Context(), connection.ID); err != nil || !status.Expired {
		t.Fatal("stored OAuth status", err)
	}
	if _, err = relay.Workspaces(selected, agenvo.Scope{BotID: f.bot, UserID: f.owner, TargetID: f.mount}); err != nil || nativeCalls != 1 {
		t.Fatal("native provider route", nativeCalls, err)
	}
	if _, err = relay.ResolveTarget(t.Context(), agenvo.Scope{BotID: f.bot, UserID: f.owner, TargetID: legacyMount}); !errors.Is(err, agenvo.ErrForbidden) {
		t.Fatal("retired execution target resolved", err)
	}
	again := original
	again.ID = uuid.NewString()
	recovered, won, err := f.store.ClaimComputerOperation(t.Context(), again)
	if err != nil || won || recovered.ID != original.ID {
		t.Fatal("unknown task replayed", won, err)
	}
	stored, err := f.store.ReadComputerOperation(t.Context(), original)
	if err != nil || stored.Revision != original.Revision || stored.State != "unknown" || string(stored.Details) != string(original.Details) || stored.TargetFingerprint != original.TargetFingerprint {
		t.Fatal("legacy evidence changed", err)
	}
	var retained bool
	if err = f.conn.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM bot_sessions s JOIN bot_workdirs w ON w.id=s.workdir_id JOIN user_runtimes r ON r.id=$3 JOIN mcp_oauth_tokens o ON o.connection_id=$4 WHERE s.id=$1 AND w.id=$2 AND r.transfer_credential_digest='synthetic-transfer-digest' AND o.access_token='synthetic-access' AND o.refresh_token='synthetic-refresh')`, f.session, workdirID, legacyRuntime, connection.ID).Scan(&retained); err != nil || !retained {
		t.Fatal("cutover removed history/config/credentials", err)
	}
	if legacyCalls != 0 || nativeCalls != 1 || oauthRequests.Load() != 0 || gateway.lists != 1 || gateway.calls != 0 {
		t.Fatal("retired route performed automatic work", legacyCalls, nativeCalls, oauthRequests.Load(), gateway.lists, gateway.calls)
	}
	if _, err = f.store.OwnedHTTPMCPConnectionBot(t.Context(), f.owner, connection.ID); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatal("disabled connection still usable", err)
	}
	t.Log("MCP inactive + native primary: owner/target listings, selected inventory, expired OAuth status and cached/restarted federation routes made zero old transport/OAuth calls; native route worked; unknown operation, bound workdir/session and synthetic credentials unchanged")
}
