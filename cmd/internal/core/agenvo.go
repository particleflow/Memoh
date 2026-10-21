package core

import (
	"context"
	"encoding/json"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/bots"
	postgresstore "github.com/felinics/memoh/internal/db/postgres/store"
	"github.com/felinics/memoh/internal/handlers"
	"github.com/felinics/memoh/internal/mcp"
	"github.com/felinics/memoh/internal/userruntime"
	"github.com/felinics/memoh/internal/workspace"
)

func provideAgenvoService(store *postgresstore.Store, connections *mcp.ConnectionService, gateway *handlers.MCPFederationGateway, oauth *mcp.OAuthService, botService *bots.Service, accounts *accounts.Service) *agenvo.Service {
	gateway.SetOAuthService(oauth)
	return agenvo.NewService(store, &applicationBotPermissionChecker{bots: botService, accounts: accounts}, func(ctx context.Context, ownerID, connectionID, tool string, args map[string]any) (json.RawMessage, error) {
		botID, err := store.OwnedHTTPMCPConnectionBot(ctx, ownerID, connectionID)
		if err != nil {
			return nil, agenvo.ErrForbidden
		}
		connection, err := connections.Get(ctx, botID, connectionID)
		if err != nil || !connection.Active || connection.Type != "http" {
			return nil, agenvo.ErrUnavailable
		}
		result, err := gateway.CallHTTPConnectionTool(ctx, connection, tool, args)
		if err != nil {
			return nil, err
		}
		return json.Marshal(result["result"])
	})
}

func setupAgenvoService(provider *agenvo.Service, runtimes *userruntime.Service, targets *workspace.RemoteWorkspaceService) {
	runtimes.SetAgenvoService(provider)
	targets.SetAgenvoService(provider)
}
