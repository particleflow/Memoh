package handlers

import (
	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/auth"
)

// Provider management requires an account JWT, whose current session is checked
// by the server's JWT middleware. A chat-route JWT deliberately skips that
// account check and must never reach provider configuration or credential use.
// OAuth callbacks keep their separate state validation and do not use this guard.
func requireProviderAccount(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, err := auth.ChatTokenFromContext(c); err == nil {
			return apperror.New(apperror.CodeHTTPForbidden, nil)
		}
		if _, err := auth.UserIDFromContext(c); err != nil {
			return apperror.New(apperror.CodeHTTPUnauthorized, nil)
		}
		return next(c)
	}
}
