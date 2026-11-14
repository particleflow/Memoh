package handlers

import (
	"log/slog"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/imageworkflow"
	"github.com/felinics/memoh/internal/server"
)

const imageWorkflowID = "33333333-3333-3333-3333-333333333333"

// imageWorkflowToken signs a token with the same test secret the workflow test
// server verifies, so these tests never depend on the album-pair suite's key.
func imageWorkflowToken(t *testing.T) string {
	t.Helper()
	const secret = "synthetic-image-workflow-signing-key" //nolint:gosec // G101: isolated test-only signing key.
	token, _, err := auth.GenerateInternalToken(albumPairUser, secret, time.Minute)
	if err != nil {
		t.Fatalf("sign test token: %v", err)
	}
	return token
}

// newImageWorkflowTestServer mounts the workflow handler behind the same JWT
// boundary the real server uses. The service is the frozen stub, so the routes
// exist and answer 501 until the feature lands.
func newImageWorkflowTestServer(t *testing.T) *echo.Echo {
	t.Helper()
	const secret = "synthetic-image-workflow-signing-key" //nolint:gosec // G101: isolated test-only signing key.
	e := echo.New()
	e.HTTPErrorHandler = server.NewHTTPErrorHandler(slog.New(slog.DiscardHandler))
	e.Use(auth.JWTMiddleware(secret, func(echo.Context) bool { return false }))
	NewImageWorkflowHandler(nil, imageworkflow.NewService(nil, imageworkflow.Deps{})).Register(e)
	return e
}

func init() {
	// Fail the build if the handler ever stops being constructible with a nil
	// logger, which the test above relies on.
	_ = NewImageWorkflowHandler(nil, nil)
}
