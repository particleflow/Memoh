package boot

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/felinics/memoh/internal/config"
	"github.com/felinics/memoh/internal/timezone"
)

type RuntimeConfig struct {
	JwtSecret    string `json:"-"`
	JwtExpiresIn time.Duration
	// RefreshTokenIdleTTL slides a device session's expiry on each refresh.
	RefreshTokenIdleTTL time.Duration
	// InternalTokenTTL is the lifetime of local channel runtime callback tokens.
	InternalTokenTTL     time.Duration
	ServerAddr           string
	ContainerdSocketPath string
	ContainerBackend     string // "docker", "containerd", or "apple"
	Timezone             string
	TimezoneLocation     *time.Location
}

func ProvideRuntimeConfig(cfg config.Config) (*RuntimeConfig, error) {
	if strings.TrimSpace(cfg.Auth.JWTSecret) == "" {
		return nil, errors.New("jwt secret is required")
	}

	jwtExpiresIn, err := time.ParseDuration(cfg.Auth.JWTExpiresIn)
	if err != nil {
		return nil, fmt.Errorf("invalid jwt expires in: %w", err)
	}
	refreshTokenIdleTTL, err := positiveDuration(cfg.Auth.RefreshTokenIdleTTL, config.DefaultRefreshTokenIdleTTL)
	if err != nil {
		return nil, fmt.Errorf("invalid refresh token idle ttl: %w", err)
	}
	internalTokenTTL, err := positiveDuration(cfg.Auth.InternalTokenTTL, config.DefaultInternalTokenTTL)
	if err != nil {
		return nil, fmt.Errorf("invalid internal token ttl: %w", err)
	}

	backend := normalizeContainerBackend(cfg.Container.Backend)
	if backend == "" {
		return nil, errors.New("container backend is required; set [container].backend to docker, containerd, or apple")
	}

	tzName := strings.TrimSpace(cfg.Timezone)
	if envTZ := strings.TrimSpace(os.Getenv("TZ")); envTZ != "" {
		tzName = envTZ
	}
	tzLocation, resolvedTZ, err := timezone.Resolve(tzName)
	if err != nil {
		return nil, err
	}

	ret := &RuntimeConfig{
		JwtSecret:            cfg.Auth.JWTSecret,
		JwtExpiresIn:         jwtExpiresIn,
		RefreshTokenIdleTTL:  refreshTokenIdleTTL,
		InternalTokenTTL:     internalTokenTTL,
		ServerAddr:           cfg.Server.Addr,
		ContainerdSocketPath: cfg.Containerd.SocketPath,
		ContainerBackend:     backend,
		Timezone:             resolvedTZ,
		TimezoneLocation:     tzLocation,
	}

	if value := os.Getenv("HTTP_ADDR"); value != "" {
		ret.ServerAddr = value
	}

	if value := os.Getenv("CONTAINERD_SOCKET"); value != "" {
		ret.ContainerdSocketPath = value
	}
	return ret, nil
}

// positiveDuration parses a configured duration, using fallback when the value
// is unset. The result must be positive.
func positiveDuration(value, fallback string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, errors.New("must be positive")
	}
	return d, nil
}

func normalizeContainerBackend(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "apple", "containerd", "docker":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return strings.TrimSpace(value)
	}
}
