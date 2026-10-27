//go:build integration

package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/config"
	memohdb "github.com/felinics/memoh/internal/db"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	dbstore "github.com/felinics/memoh/internal/db/store"
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
	f.exec(t, `UPDATE user_runtimes SET provider_config=$2 WHERE id=$1`, legacyRuntime, raw)
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
	// An unsettled transfer is independent evidence, not disposable mount state.
	f.exec(t, `INSERT INTO computer_delivery_entries(operation_id,delivery_id,version,entry) VALUES($1,'synthetic_unknown_transfer',1,'{"reference":{"direction":"upload"},"outcome":"unknown"}')`, original.ID)
	preservedRows := func() map[string]json.RawMessage {
		t.Helper()
		var raw []byte
		if err := f.conn.QueryRow(t.Context(), `SELECT jsonb_build_object(
		    'runtime_and_transfer_credential', (SELECT to_jsonb(r) FROM user_runtimes r WHERE r.id=$1),
		    'mount', (SELECT to_jsonb(b) FROM bot_remote_runtime_bindings b WHERE b.id=$2),
		    'workdir', (SELECT to_jsonb(w) FROM bot_workdirs w WHERE w.id=$3),
		    'session', (SELECT to_jsonb(s) FROM bot_sessions s WHERE s.id=$4),
		    'unknown_operation', (SELECT to_jsonb(o) FROM computer_operations o WHERE o.id=$5),
		    'oauth', (SELECT to_jsonb(o) FROM mcp_oauth_tokens o WHERE o.connection_id=$6),
		    'unknown_transfer', (SELECT to_jsonb(d) FROM computer_delivery_entries d WHERE d.operation_id=$5 AND d.delivery_id='synthetic_unknown_transfer'))`, legacyRuntime, legacyMount, workdirID, f.session, original.ID, connection.ID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var rows map[string]json.RawMessage
		if err := json.Unmarshal(raw, &rows); err != nil {
			t.Fatal(err)
		}
		for name, row := range rows {
			if string(row) == "null" {
				t.Fatal("missing retained fixture", name)
			}
		}
		return rows
	}
	before := preservedRows()
	legacyCalls, nativeCalls, otherCalls := 0, 0, 0
	relay := agenvo.NewService(f.store, cutoverPermission{bot: f.bot, owner: f.owner}, nil)
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
	gateway := &cutoverGateway{}
	source := federation.NewSource(nil, gateway, connections)
	session := mcpgw.ToolSessionContext{BotID: f.bot}
	discovered, err := source.ListTools(t.Context(), session)
	if err != nil || len(discovered) != 1 || gateway.lists != 1 {
		t.Fatal("active positive control", len(discovered), gateway.lists, err)
	}
	// Even a separate active Agenvo registration must be omitted from inventories.
	other, err := connections.Create(t.Context(), f.bot, mcpgw.UpsertRequest{Name: "unrelated-active-agenvo", URL: oauthServer.URL + "/other-mcp"})
	if err != nil {
		t.Fatal(err)
	}
	otherTarget := legacy
	otherTarget.ConnectionID, otherTarget.Session = other.ID, "other-active"
	otherConfig, _ := json.Marshal(otherTarget)
	otherRuntime := dbstore.UserRuntimeRecord{ID: uuid.NewString()}
	f.exec(t, "INSERT INTO user_runtimes(id,user_id,name,api_token,provider,provider_config,activated_at,pending_expires_at) VALUES($1,$2,$3,$4,'agenvo',$5,now(),NULL)", otherRuntime.ID, f.owner, "unrelated-active-agenvo", uuid.NewString(), otherConfig)
	otherMount := uuid.NewString()
	f.exec(t, `INSERT INTO bot_remote_runtime_bindings(id,bot_id,runtime_id) VALUES($1,$2,$3)`, otherMount, f.bot, otherRuntime.ID)
	assertInventories := func() {
		t.Helper()
		wantRuntimes := map[string]bool{f.runtime: true}
		wantMounts := map[string]bool{f.mount: true}
		listed, err := owners.ListRuntimes(t.Context(), f.owner)
		if err != nil {
			t.Fatal(err)
		}
		runtimes := make(map[string]bool)
		for _, runtime := range listed {
			runtimes[runtime.ID] = true
		}
		if len(listed) != len(wantRuntimes) || !maps.Equal(runtimes, wantRuntimes) {
			t.Error("owner runtime inventory does not match active connections and native runtime")
		}
		targets, err := remotes.ListMounts(t.Context(), f.bot)
		if err != nil {
			t.Fatal(err)
		}
		mounts := make(map[string]bool)
		for _, target := range targets {
			mounts[target.TargetID] = true
		}
		if len(targets) != len(wantMounts) || !maps.Equal(mounts, wantMounts) {
			t.Error("bot mount inventory does not match active connections and native runtime")
		}
		grants, err := remotes.ListAccountGrants(t.Context(), f.owner)
		if err != nil {
			t.Fatal(err)
		}
		grantedMounts, grantedRuntimes := make(map[string]bool), make(map[string]bool)
		for _, grant := range grants {
			if grant.BotID != f.bot {
				t.Fatal("grant changed bot")
			}
			grantedMounts[grant.TargetID], grantedRuntimes[grant.RuntimeID] = true, true
		}
		if len(grants) != len(wantMounts) || !maps.Equal(grantedMounts, wantMounts) || !maps.Equal(grantedRuntimes, wantRuntimes) {
			t.Error("account grant inventory does not match active connections and native runtime")
		}
	}
	assertInventories()
	if legacyCalls != 0 || otherCalls != 0 {
		t.Fatal("retired listings contacted a legacy connection", legacyCalls, otherCalls)
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
		assertInventories()
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
	if tools, err := source.ListTools(t.Context(), session); err != nil || len(tools) != 1 {
		t.Fatal("restart lost unrelated active MCP or rediscovered retired MCP", len(tools), err)
	}
	if status, err := mcpgw.NewOAuthService(nil, queries, "").GetStatus(t.Context(), connection.ID); err != nil || !status.Expired {
		t.Fatal("stored OAuth status", err)
	}
	if _, err = relay.Workspaces(selected, agenvo.Scope{BotID: f.bot, UserID: f.owner, TargetID: f.mount}); err != nil || nativeCalls != 1 {
		t.Fatal("native provider route", nativeCalls, err)
	}
	if _, err = relay.ResolveTarget(t.Context(), agenvo.Scope{BotID: f.bot, UserID: f.owner, TargetID: legacyMount}); !errors.Is(err, agenvo.ErrLegacyRetired) {
		t.Fatal("retired execution target resolved", err)
	}
	if _, err = remotes.ResolveMount(t.Context(), f.bot, legacyMount); !errors.Is(err, workspace.ErrWorkspaceTargetNotFound) {
		t.Fatal("retired mount resolved executable transport", err)
	}
	if _, err := remotes.ResolveMount(t.Context(), f.bot, otherMount); !errors.Is(err, workspace.ErrWorkspaceTargetNotFound) {
		t.Fatal("active legacy mount remained callable", err)
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
	for name, row := range preservedRows() {
		if string(row) != string(before[name]) {
			t.Error("cutover changed retained record", name)
		}
	}
	if legacyCalls != 0 || nativeCalls != 1 || oauthRequests.Load() != 0 || gateway.lists != 2 || gateway.calls != 0 || otherCalls != 0 {
		t.Fatal("retired route performed automatic work", legacyCalls, nativeCalls, oauthRequests.Load(), gateway.lists, gateway.calls)
	}
	if _, err = f.store.OwnedHTTPMCPConnectionBot(t.Context(), f.owner, connection.ID); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatal("disabled connection still usable", err)
	}
	t.Log("MCP inactive + native primary: owner runtimes, bot mounts and account grants omit retired IDs while keeping native and omitting all active Agenvo IDs; zero old transport/OAuth; runtime, mount, workdir/session, unknown operation/transfer and synthetic credentials unchanged")
}

func TestNativeRuntimeCutoverListsRecognizeUUIDTextForms(t *testing.T) {
	f := newNativeHerdrFixture(t)
	connection := "abcdefab-cdef-4abc-8def-abcdefabcdef"
	f.exec(t, `INSERT INTO mcp_connections(id,bot_id,name,type,is_active) VALUES($1,$2,'synthetic-inactive-formats','http',false)`, connection, f.bot)
	upper := strings.ToUpper(connection)
	invalid := "g" + connection[1:]
	owners := userruntime.NewService(f.store, nil)
	remotes := workspace.NewRemoteWorkspaceService(f.store, owners)
	for _, tc := range []struct {
		name    string
		value   any
		missing bool
		hidden  bool
	}{
		{name: "canonical", value: connection, hidden: true},
		{name: "uppercase", value: upper, hidden: true},
		{name: "compact", value: strings.ReplaceAll(upper, "-", ""), hidden: true},
		{name: "urn", value: "URN:UUID:" + upper, hidden: true},
		{name: "braces", value: "{" + upper + "}", hidden: true},
		// google/uuid.Parse accepts any surrounding bytes for its 38-byte form.
		{name: "non-brace-wrapper", value: "<" + upper + ">", hidden: true},
		{name: "invalid-32", value: strings.Repeat("g", 32)},
		{name: "invalid-36", value: invalid},
		{name: "invalid-38", value: "{" + invalid + "}"},
		{name: "invalid-urn-prefix", value: "not:uuid:" + connection},
		{name: "invalid-length", value: "not-a-uuid"},
		{name: "empty", value: ""},
		{name: "null", value: nil},
		{name: "missing", missing: true},
		{name: "non-string", value: map[string]string{"id": connection}},
		{name: "missing-connection", value: uuid.NewString()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if value, ok := tc.value.(string); ok {
				parsed, err := uuid.Parse(value)
				if matches := err == nil && parsed.String() == connection; matches != tc.hidden {
					t.Fatal("fixture differs from existing UUID parser acceptance", err)
				}
			}
			profile := map[string]any{"session": tc.name}
			if !tc.missing {
				profile["connection_id"] = tc.value
			}
			raw, _ := json.Marshal(profile)
			runtime := dbstore.UserRuntimeRecord{ID: uuid.NewString()}
			f.exec(t, "INSERT INTO user_runtimes(id,user_id,name,api_token,provider,provider_config,activated_at,pending_expires_at) VALUES($1,$2,$3,$4,'agenvo',$5,now(),NULL)", runtime.ID, f.owner, "format-"+tc.name, uuid.NewString(), raw)
			mount := uuid.NewString()
			f.exec(t, `INSERT INTO bot_remote_runtime_bindings(id,bot_id,runtime_id) VALUES($1,$2,$3)`, mount, f.bot, runtime.ID)
			runtimes, err := owners.ListRuntimes(t.Context(), f.owner)
			if err != nil {
				t.Fatal("owner list rejected stored config", err)
			}
			mounts, err := remotes.ListMounts(t.Context(), f.bot)
			if err != nil {
				t.Fatal("mount list rejected stored config", err)
			}
			grants, err := remotes.ListAccountGrants(t.Context(), f.owner)
			if err != nil {
				t.Fatal("grant list rejected stored config", err)
			}
			foundRuntime, foundMount, foundGrant := false, false, false
			for _, item := range runtimes {
				foundRuntime = foundRuntime || item.ID == runtime.ID
			}
			for _, item := range mounts {
				foundMount = foundMount || item.TargetID == mount
			}
			for _, item := range grants {
				foundGrant = foundGrant || item.RuntimeID == runtime.ID || item.TargetID == mount
			}
			if foundRuntime || foundMount || foundGrant {
				t.Fatalf("hidden=%v, listed owner=%v mount=%v grant=%v", tc.hidden, foundRuntime, foundMount, foundGrant)
			}
		})
	}
}

func TestProjectWideAgenvoRetirementPreservesRowsAndRejectsMountAndTransfer(t *testing.T) {
	f := newAdmissionFixture(t)
	// An active connection is insufficient: all legacy providers are retired.
	f.exec(t, `INSERT INTO mcp_connections(id,bot_id,name,type,is_active) VALUES($1,$2,'legacy-active-retirement','http',true)`, f.connection, f.bot)
	f.exec(t, `UPDATE user_runtimes SET transfer_credential_id='historical-credential',transfer_credential_digest='historical-digest',transfer_target_digest='historical-target' WHERE id=$1`, f.runtime)
	var before, after string
	snapshot := func(dest *string) {
		t.Helper()
		if err := f.conn.QueryRow(t.Context(), `SELECT to_jsonb(r)::text FROM user_runtimes r WHERE id=$1`, f.runtime).Scan(dest); err != nil {
			t.Fatal(err)
		}
	}
	snapshot(&before)
	if _, err := f.store.CreateOrUpdateMount(t.Context(), f.bot, f.runtime); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatal("retired runtime accepted a grant", err)
	}
	if _, err := f.store.ReadOwnedRuntimeTransfer(t.Context(), f.runtime, f.owner); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatal("retired owner transfer remained available", err)
	}
	if _, err := f.store.ReadRuntimeTransfer(t.Context(), "historical-credential"); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatal("retired credential remained usable", err)
	}
	snapshot(&after)
	if before != after {
		t.Fatal("retirement query changed historical runtime or credentials")
	}
	var mounts int
	if err := f.conn.QueryRow(t.Context(), `SELECT count(*) FROM bot_remote_runtime_bindings WHERE runtime_id=$1`, f.runtime).Scan(&mounts); err != nil || mounts != 1 {
		t.Fatal("historical mount changed", err)
	}
}
