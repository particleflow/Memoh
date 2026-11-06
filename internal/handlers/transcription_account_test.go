package handlers

import (
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	audiopkg "github.com/felinics/memoh/internal/audio"
)

func TestTranscriptionManagementRequiresAccount(t *testing.T) {
	inventory, _, _, _ := newProviderBoundary(t)
	NewAudioHandler(slog.Default(), audiopkg.NewService(slog.Default(), nil, audiopkg.NewRegistry()), nil).Register(inventory)
	routes := 0
	for _, route := range inventory.Routes() {
		if !strings.HasPrefix(route.Path, "/transcription-") || route.Method == "echo_route_not_found" {
			continue
		}
		routes++
		for _, kind := range []string{"chat", "chat-with-account-id", "anonymous", "revoked", "expired"} {
			for _, query := range []bool{false, true} {
				t.Run(route.Method+route.Path+"/"+kind+map[bool]string{true: "/query"}[query], func(t *testing.T) {
					e, q, calls, _ := newProviderBoundary(t)
					NewAudioHandler(slog.Default(), audiopkg.NewService(slog.Default(), q, audiopkg.NewRegistry()), nil).Register(e)
					path := strings.ReplaceAll(route.Path, ":id", q.provider.ID.String())
					rec := providerBoundaryRequest(e, route.Method, path, `{}`, providerBoundaryToken(t, kind), query)
					expected := http.StatusUnauthorized
					if kind == "chat" || kind == "chat-with-account-id" {
						expected = http.StatusForbidden
					}
					require.Equal(t, expected, rec.Code)
					require.Zero(t, q.reads)
					require.Zero(t, q.writes)
					require.Zero(t, *calls)
				})
			}
		}
	}
	require.Equal(t, 10, routes, "all native transcription management routes must be covered")
}

func TestTranscriptionMetadataAllowsValidatedAccount(t *testing.T) {
	e, q, calls, validated := newProviderBoundary(t)
	NewAudioHandler(slog.Default(), audiopkg.NewService(slog.Default(), q, audiopkg.NewRegistry()), nil).Register(e)
	rec := providerBoundaryRequest(e, http.MethodGet, "/transcription-providers/meta", "", providerBoundaryToken(t, "active-account"), false)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, *validated)
	require.Zero(t, q.writes)
	require.Zero(t, *calls)
}
