package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	echojwt "github.com/labstack/echo-jwt/v4"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// UserSessionValidator revalidates an account-backed JWT against current
// server-side state before the request reaches a handler. sessionID is the
// device session the token is bound to; it is empty for an internal token,
// which is checked against the account alone.
type UserSessionValidator func(ctx context.Context, userID, sessionID string) error

const (
	claimSubject           = "sub"
	claimUserID            = "user_id"
	claimChannelIdentityID = "channel_identity_id"
	claimType              = "typ"
	claimSessionID         = "sid"
	claimBotID             = "bot_id"
	claimChatID            = "chat_id"
	claimRouteID           = "route_id"
	chatTokenType          = "chat_route"
	internalTokenType      = "internal"
)

// JWTMiddleware returns a JWT auth middleware configured for HS256 tokens.
//
// With a validator, an account token passes only as one of two kinds: a token
// with a sid claim is a device session token, revalidated with that session;
// a token without one must carry typ=internal and is revalidated against the
// account alone. A sid that is empty or not a UUID is rejected and never read
// as an internal token. Any other account token is rejected. Chat route
// tokens keep their own scope and are not revalidated as accounts.
func JWTMiddleware(secret string, skipper middleware.Skipper, validators ...UserSessionValidator) echo.MiddlewareFunc {
	jwtMiddleware := echojwt.WithConfig(echojwt.Config{
		SigningKey:    []byte(secret),
		SigningMethod: "HS256",
		TokenLookup:   "header:Authorization:Bearer ,query:token",
		Skipper:       skipper,
		NewClaimsFunc: func(_ echo.Context) jwt.Claims {
			return jwt.MapClaims{}
		},
	})
	if len(validators) == 0 || validators[0] == nil {
		return jwtMiddleware
	}
	validateSession := validators[0]
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		validatedNext := func(c echo.Context) error {
			if skipper != nil && skipper(c) {
				return next(c)
			}
			if isChatRouteToken(c) {
				return next(c)
			}
			userID, err := UserIDFromContext(c)
			if err != nil {
				return err
			}
			sessionID, err := accountTokenSession(c)
			if err != nil {
				return err
			}
			if err := validateSession(c.Request().Context(), userID, sessionID); err != nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "user session is no longer active")
			}
			return next(c)
		}
		return jwtMiddleware(validatedNext)
	}
}

// accountTokenSession returns the device session an account token is bound
// to, or "" for an internal token. It rejects every other token.
func accountTokenSession(c echo.Context) (string, error) {
	if sessionID, present := SessionIDFromContext(c); present {
		if sessionID == "" {
			return "", echo.NewHTTPError(http.StatusUnauthorized, "token session is empty")
		}
		if _, err := uuid.Parse(sessionID); err != nil {
			return "", echo.NewHTTPError(http.StatusUnauthorized, "token session is invalid")
		}
		return sessionID, nil
	}
	if InternalTokenFromContext(c) {
		return "", nil
	}
	return "", echo.NewHTTPError(http.StatusUnauthorized, "token has no session")
}

func isChatRouteToken(c echo.Context) bool {
	claims, ok := validClaims(c)
	return ok && claimString(claims, claimType) == chatTokenType
}

// SessionIDFromContext returns the trimmed sid claim of the validated token and
// whether the token carries a sid claim at all.
func SessionIDFromContext(c echo.Context) (string, bool) {
	claims, ok := validClaims(c)
	if !ok {
		return "", false
	}
	if _, present := claims[claimSessionID]; !present {
		return "", false
	}
	return strings.TrimSpace(claimString(claims, claimSessionID)), true
}

// InternalTokenFromContext reports whether the validated token is an internal
// token minted by GenerateInternalToken.
func InternalTokenFromContext(c echo.Context) bool {
	claims, ok := validClaims(c)
	return ok && claimString(claims, claimType) == internalTokenType
}

func validClaims(c echo.Context) (jwt.MapClaims, bool) {
	token, ok := c.Get("user").(*jwt.Token)
	if !ok || token == nil || !token.Valid {
		return nil, false
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	return claims, ok
}

// UserIDFromContext extracts the user id from JWT claims.
func UserIDFromContext(c echo.Context) (string, error) {
	token, ok := c.Get("user").(*jwt.Token)
	if !ok || token == nil || !token.Valid {
		return "", echo.NewHTTPError(http.StatusUnauthorized, "invalid token")
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", echo.NewHTTPError(http.StatusUnauthorized, "invalid token claims")
	}
	if userID := claimString(claims, claimUserID); userID != "" {
		return userID, nil
	}
	if userID := claimString(claims, claimSubject); userID != "" {
		return userID, nil
	}
	return "", echo.NewHTTPError(http.StatusUnauthorized, "user id missing")
}

// RawTokenFromContext returns the token the middleware validated, as it
// arrived, whether from the Authorization header or the token query parameter.
// It is empty when the request carries no valid token.
func RawTokenFromContext(c echo.Context) string {
	token, ok := c.Get("user").(*jwt.Token)
	if !ok || token == nil || !token.Valid {
		return ""
	}
	return token.Raw
}

// GenerateInternalToken creates a signed account JWT for a server-side caller
// (channel replies, local channel runtimes, schedule triggers). It carries
// typ=internal and no device session, so it is revalidated against the account
// alone.
func GenerateInternalToken(userID, secret string, expiresIn time.Duration) (string, time.Time, error) {
	return generateAccountToken(userID, secret, expiresIn, claimType, internalTokenType)
}

// GenerateSessionToken creates a signed account JWT bound to a device session.
// It carries the session id in sid and no typ.
func GenerateSessionToken(userID, sessionID, secret string, expiresIn time.Duration) (string, time.Time, error) {
	if strings.TrimSpace(sessionID) == "" {
		return "", time.Time{}, errors.New("session id is required")
	}
	return generateAccountToken(userID, secret, expiresIn, claimSessionID, sessionID)
}

func generateAccountToken(userID, secret string, expiresIn time.Duration, kindClaim, kindValue string) (string, time.Time, error) {
	if strings.TrimSpace(userID) == "" {
		return "", time.Time{}, errors.New("user id is required")
	}
	if strings.TrimSpace(secret) == "" {
		return "", time.Time{}, errors.New("jwt secret is required")
	}
	if expiresIn <= 0 {
		return "", time.Time{}, errors.New("jwt expires in must be positive")
	}

	now := time.Now().UTC()
	expiresAt := now.Add(expiresIn)
	claims := jwt.MapClaims{
		claimSubject: userID,
		claimUserID:  userID,
		kindClaim:    kindValue,
		"iat":        now.Unix(),
		"exp":        expiresAt.Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expiresAt, nil
}

// ChatToken holds the claims for a chat-based JWT used for route-based reply.
type ChatToken struct {
	BotID             string
	ChatID            string
	RouteID           string
	UserID            string
	ChannelIdentityID string
}

// GenerateChatToken creates a signed JWT for chat route reply.
func GenerateChatToken(info ChatToken, secret string, expiresIn time.Duration) (string, time.Time, error) {
	if strings.TrimSpace(info.BotID) == "" {
		return "", time.Time{}, errors.New("bot id is required")
	}
	if strings.TrimSpace(info.ChatID) == "" {
		return "", time.Time{}, errors.New("chat id is required")
	}
	if strings.TrimSpace(info.UserID) == "" {
		info.UserID = strings.TrimSpace(info.ChannelIdentityID)
	}
	if strings.TrimSpace(info.UserID) == "" {
		return "", time.Time{}, errors.New("user id is required")
	}
	if strings.TrimSpace(secret) == "" {
		return "", time.Time{}, errors.New("jwt secret is required")
	}
	if expiresIn <= 0 {
		return "", time.Time{}, errors.New("jwt expires in must be positive")
	}

	now := time.Now().UTC()
	expiresAt := now.Add(expiresIn)
	claims := jwt.MapClaims{
		claimType:              chatTokenType,
		claimBotID:             info.BotID,
		claimChatID:            info.ChatID,
		claimRouteID:           info.RouteID,
		claimUserID:            info.UserID,
		claimChannelIdentityID: info.ChannelIdentityID,
		"iat":                  now.Unix(),
		"exp":                  expiresAt.Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expiresAt, nil
}

// ChatTokenFromContext extracts the chat token claims from context.
func ChatTokenFromContext(c echo.Context) (ChatToken, error) {
	token, ok := c.Get("user").(*jwt.Token)
	if !ok || token == nil || !token.Valid {
		return ChatToken{}, echo.NewHTTPError(http.StatusUnauthorized, "invalid token")
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return ChatToken{}, echo.NewHTTPError(http.StatusUnauthorized, "invalid token claims")
	}
	if claimString(claims, claimType) != chatTokenType {
		return ChatToken{}, echo.NewHTTPError(http.StatusUnauthorized, "invalid chat token")
	}
	info := ChatToken{
		BotID:             claimString(claims, claimBotID),
		ChatID:            claimString(claims, claimChatID),
		RouteID:           claimString(claims, claimRouteID),
		UserID:            claimString(claims, claimUserID),
		ChannelIdentityID: claimString(claims, claimChannelIdentityID),
	}
	if strings.TrimSpace(info.UserID) == "" {
		info.UserID = strings.TrimSpace(info.ChannelIdentityID)
	}
	return info, nil
}

func claimString(claims jwt.MapClaims, key string) string {
	raw, ok := claims[key]
	if !ok || raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(raw)
	}
}
