package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/felinics/memoh/internal/accounts"
	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/auth"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/errs"
)

const authSessionRevokeLogout = "logout"

type AuthHandler struct {
	accountService *accounts.Service
	sessions       dbstore.AuthSessionStore
	jwtSecret      string
	expiresIn      time.Duration
	refreshIdleTTL time.Duration
	logger         *slog.Logger
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"` //nolint:gosec // intentional: JSON request field carrying a user-supplied credential
	// DeviceID is a stable identifier the client generates for itself. A login
	// with a device_id replaces that device's live session.
	DeviceID   string `json:"device_id,omitempty"`
	DeviceName string `json:"device_name,omitempty"`
}

type LoginResponse struct {
	AccessToken      string `json:"access_token"` //nolint:gosec // intentional: JWT is the purpose of this response field
	TokenType        string `json:"token_type"`
	ExpiresAt        string `json:"expires_at"`
	UserID           string `json:"user_id"`
	Role             string `json:"role"`
	DisplayName      string `json:"display_name"`
	Username         string `json:"username"`
	Timezone         string `json:"timezone,omitempty"`
	RefreshToken     string `json:"refresh_token"` //nolint:gosec // intentional: the device session credential is the purpose of this response field
	RefreshExpiresAt string `json:"refresh_expires_at"`
}

func NewAuthHandler(log *slog.Logger, accountService *accounts.Service, sessions dbstore.AuthSessionStore, jwtSecret string, expiresIn, refreshIdleTTL time.Duration) *AuthHandler {
	return &AuthHandler{
		accountService: accountService,
		sessions:       sessions,
		jwtSecret:      jwtSecret,
		expiresIn:      expiresIn,
		refreshIdleTTL: refreshIdleTTL,
		logger:         log.With(slog.String("handler", "auth")),
	}
}

func (h *AuthHandler) Register(e *echo.Echo) {
	e.POST("/auth/login", h.Login)
	e.POST("/auth/refresh", h.Refresh)
	e.POST("/auth/logout", h.Logout)
}

// AccountSessionValidator revalidates account tokens for the JWT middleware.
// The account must be active; a device session token's session must also be
// live and belong to that account.
func AccountSessionValidator(accountService *accounts.Service, sessions dbstore.AuthSessionStore) auth.UserSessionValidator {
	return func(ctx context.Context, userID, sessionID string) error {
		if err := accountService.ValidateSession(ctx, userID); err != nil {
			return err
		}
		if sessionID == "" {
			return nil
		}
		if sessions == nil {
			return errors.New("auth session store not configured")
		}
		return sessions.GetActiveAuthSession(ctx, sessionID, userID)
	}
}

func (h *AuthHandler) configured() error {
	if h.accountService == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "user service not configured")
	}
	if h.sessions == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "auth session store not configured")
	}
	if strings.TrimSpace(h.jwtSecret) == "" {
		return echo.NewHTTPError(http.StatusInternalServerError, "jwt secret not configured")
	}
	if h.expiresIn <= 0 {
		return echo.NewHTTPError(http.StatusInternalServerError, "jwt expiry not configured")
	}
	if h.refreshIdleTTL <= 0 {
		return echo.NewHTTPError(http.StatusInternalServerError, "refresh token idle ttl not configured")
	}
	return nil
}

// Login godoc
// @Summary Login
// @Description Validate user credentials, start a device session and issue a JWT with its refresh token
// @Tags auth
// @Param payload body LoginRequest true "Login request"
// @Success 200 {object} LoginResponse
// @Failure 400 {object} server.Problem
// @Failure 401 {object} server.Problem
// @Failure 500 {object} server.Problem
// @Failure 503 {object} server.Problem
// @Router /auth/login [post].
func (h *AuthHandler) Login(c echo.Context) error {
	if err := h.configured(); err != nil {
		return err
	}

	var req LoginRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		return apperror.FieldRequired("username")
	}
	if strings.TrimSpace(req.Password) == "" {
		return apperror.FieldRequired("password")
	}

	ctx := c.Request().Context()
	account, err := h.accountService.Login(ctx, req.Username, req.Password)
	if err != nil {
		if errors.Is(err, accounts.ErrInvalidCredentials) {
			return echo.NewHTTPError(http.StatusUnauthorized, "invalid credentials")
		}
		if errors.Is(err, accounts.ErrInactiveAccount) {
			return echo.NewHTTPError(http.StatusUnauthorized, "user is inactive")
		}
		return errs.Wrap(err, "login")
	}
	secret, hash, err := auth.NewRefreshSecret()
	if err != nil {
		return errs.Wrap(err, "generate refresh secret")
	}
	session, err := h.sessions.CreateAuthSession(ctx, dbstore.CreateAuthSessionInput{
		UserID:      account.ID,
		DeviceID:    strings.TrimSpace(req.DeviceID),
		DeviceName:  strings.TrimSpace(req.DeviceName),
		RefreshHash: hash,
		ExpiresAt:   time.Now().UTC().Add(h.refreshIdleTTL),
	})
	if err != nil {
		if errors.Is(err, db.ErrAuthSessionConflict) {
			return apperror.Wrap(apperror.CodeHTTPServiceUnavailable, err, nil)
		}
		return errs.Wrap(err, "create auth session")
	}
	token, expiresAt, err := auth.GenerateSessionToken(account.ID, session.ID, h.jwtSecret, h.expiresIn)
	if err != nil {
		return errs.Wrap(err, "generate token")
	}
	// Stale sessions are cleaned up here rather than by a timer: a login
	// already writes, and refreshes stay single-row.
	if _, err := h.sessions.DeleteStaleAuthSessions(ctx); err != nil {
		h.logger.WarnContext(ctx, "delete stale auth sessions failed", slog.Any("error", err))
	}

	return c.JSON(http.StatusOK, LoginResponse{
		AccessToken:      token,
		TokenType:        "Bearer",
		ExpiresAt:        expiresAt.Format(time.RFC3339),
		UserID:           account.ID,
		Username:         account.Username,
		Role:             account.Role,
		DisplayName:      account.DisplayName,
		Timezone:         account.Timezone,
		RefreshToken:     auth.FormatRefreshToken(session.ID, secret),
		RefreshExpiresAt: session.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"` //nolint:gosec // intentional: JSON request field carrying the device session credential
}

type RefreshResponse struct {
	AccessToken      string `json:"access_token"` //nolint:gosec // intentional: JWT is the purpose of this response field
	TokenType        string `json:"token_type"`
	ExpiresAt        string `json:"expires_at"`
	RefreshToken     string `json:"refresh_token"` //nolint:gosec // intentional: the device session credential is returned unchanged
	RefreshExpiresAt string `json:"refresh_expires_at"`
}

// Refresh godoc
// @Summary Refresh Token
// @Description Exchange a device session refresh token for a new access token. The refresh token is returned unchanged and its session expiry slides forward.
// @Tags auth
// @Param payload body RefreshRequest true "Refresh request"
// @Success 200 {object} RefreshResponse
// @Failure 400 {object} server.Problem
// @Failure 401 {object} server.Problem
// @Failure 500 {object} server.Problem
// @Failure 503 {object} server.Problem
// @Router /auth/refresh [post].
func (h *AuthHandler) Refresh(c echo.Context) error {
	if err := h.configured(); err != nil {
		return err
	}
	var req RefreshRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	// Only a refresh token that is wrong answers 401. A database or other
	// infrastructure failure answers 503 and never revokes the session, so an
	// outage does not log devices out.
	sessionID, secret, ok := auth.ParseRefreshToken(req.RefreshToken)
	if !ok {
		return invalidRefreshToken()
	}
	ctx := c.Request().Context()
	session, err := h.sessions.GetAuthSessionByID(ctx, sessionID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return invalidRefreshToken()
		}
		return apperror.Wrap(apperror.CodeHTTPServiceUnavailable, err, nil)
	}
	if !auth.RefreshSecretMatches(secret, session.RefreshHash) {
		return invalidRefreshToken()
	}
	if err := h.accountService.ValidateSession(ctx, session.UserID); err != nil {
		if errors.Is(err, accounts.ErrInactiveAccount) {
			return invalidRefreshToken()
		}
		return apperror.Wrap(apperror.CodeHTTPServiceUnavailable, err, nil)
	}
	now := time.Now().UTC()
	if !session.RevokedAt.IsZero() || !session.ExpiresAt.After(now) {
		return invalidRefreshToken()
	}
	refreshExpiresAt := now.Add(h.refreshIdleTTL)
	touched, err := h.sessions.TouchAuthSession(ctx, session.ID, refreshExpiresAt)
	if err != nil {
		return apperror.Wrap(apperror.CodeHTTPServiceUnavailable, err, nil)
	}
	// Zero rows: the session was revoked or expired after it was read.
	if touched != 1 {
		return invalidRefreshToken()
	}
	token, expiresAt, err := auth.GenerateSessionToken(session.UserID, session.ID, h.jwtSecret, h.expiresIn)
	if err != nil {
		return errs.Wrap(err, "generate token")
	}

	return c.JSON(http.StatusOK, RefreshResponse{
		AccessToken:      token,
		TokenType:        "Bearer",
		ExpiresAt:        expiresAt.Format(time.RFC3339),
		RefreshToken:     auth.FormatRefreshToken(session.ID, secret),
		RefreshExpiresAt: refreshExpiresAt.Format(time.RFC3339),
	})
}

type LogoutRequest struct {
	RefreshToken string `json:"refresh_token"` //nolint:gosec // intentional: JSON request field carrying the device session credential
}

// Logout godoc
// @Summary Logout
// @Description Revoke the device session of a refresh token. An unknown, mismatched or already revoked refresh token also answers 204.
// @Tags auth
// @Param payload body LogoutRequest true "Logout request"
// @Success 204 "No Content"
// @Failure 400 {object} server.Problem
// @Failure 500 {object} server.Problem
// @Failure 503 {object} server.Problem
// @Router /auth/logout [post].
func (h *AuthHandler) Logout(c echo.Context) error {
	if h.sessions == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "auth session store not configured")
	}
	var req LogoutRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	sessionID, secret, ok := auth.ParseRefreshToken(req.RefreshToken)
	if !ok {
		return c.NoContent(http.StatusNoContent)
	}
	ctx := c.Request().Context()
	session, err := h.sessions.GetAuthSessionByID(ctx, sessionID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return c.NoContent(http.StatusNoContent)
		}
		return apperror.Wrap(apperror.CodeHTTPServiceUnavailable, err, nil)
	}
	if !auth.RefreshSecretMatches(secret, session.RefreshHash) {
		return c.NoContent(http.StatusNoContent)
	}
	if _, err := h.sessions.RevokeAuthSession(ctx, session.ID, session.UserID, authSessionRevokeLogout); err != nil {
		return apperror.Wrap(apperror.CodeHTTPServiceUnavailable, err, nil)
	}
	return c.NoContent(http.StatusNoContent)
}

func invalidRefreshToken() error {
	return echo.NewHTTPError(http.StatusUnauthorized, "invalid refresh token")
}
