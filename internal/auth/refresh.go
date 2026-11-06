package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"strings"

	"github.com/google/uuid"
)

// A refresh token is the device session credential on the wire:
// rt_<session id>.<base64url secret>. Only the hash of the secret is stored.
const (
	refreshTokenPrefix = "rt_"
	refreshSecretBytes = 32
)

// NewRefreshSecret returns a fresh 256-bit refresh secret and the hash to
// store for it.
func NewRefreshSecret() (secret, hash string, err error) {
	buf := make([]byte, refreshSecretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	secret = base64.RawURLEncoding.EncodeToString(buf)
	return secret, HashRefreshSecret(secret), nil
}

// FormatRefreshToken renders the refresh token of a device session.
func FormatRefreshToken(sessionID, secret string) string {
	return refreshTokenPrefix + sessionID + "." + secret
}

// ParseRefreshToken splits a refresh token into its session id and secret. It
// reports false for anything that is not a well-formed refresh token.
func ParseRefreshToken(token string) (sessionID, secret string, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(token), refreshTokenPrefix)
	if !found {
		return "", "", false
	}
	id, secret, found := strings.Cut(rest, ".")
	if !found || secret == "" {
		return "", "", false
	}
	parsed, err := uuid.Parse(id)
	if err != nil {
		return "", "", false
	}
	return parsed.String(), secret, true
}

// HashRefreshSecret is the stored form of a refresh secret:
// base64url(sha256(secret)). The secret is 256 random bits, so a slow hash
// adds nothing.
func HashRefreshSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// RefreshSecretMatches compares a presented secret with a stored hash in
// constant time.
func RefreshSecretMatches(secret, hash string) bool {
	return subtle.ConstantTimeCompare([]byte(HashRefreshSecret(secret)), []byte(hash)) == 1
}
