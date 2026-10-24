package transferclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// HostConfig resolves the existing Herdr config location once per invocation.
// Config, credentials, source files and queue/publications keep separate owners.
// Credentials never enter Job, request JSON, argv, diagnostics or stdout.
type HostConfig struct {
	Origin      string
	Binding     string
	root        *os.Root
	guard       *os.File
	sources     *os.Root
	sourceGuard *os.File
	sourcePath  string
}

type hostConfigWire struct {
	Version    int    `json:"version"`
	Origin     string `json:"origin"`
	SourceRoot string `json:"source_root"`
}

func privateHostRoot(path string) (*os.Root, *os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, nil, ErrBinding
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return nil, nil, ErrBinding
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, nil, ErrBinding
	}
	if err = checkQueueRoot(root); err != nil {
		_ = root.Close()
		return nil, nil, errors.Join(ErrBinding, err)
	}
	guard, err := guardQueueRoot(root)
	if err != nil {
		_ = root.Close()
		return nil, nil, errors.Join(ErrBinding, err)
	}
	return root, guard, nil
}

func OpenHostConfig(getenv func(string) string) (*HostConfig, error) {
	if getenv == nil {
		return nil, ErrBinding
	}
	root, guard, err := privateHostRoot(getenv("HERDR_PLUGIN_CONFIG_DIR"))
	if err != nil {
		return nil, err
	}
	h := &HostConfig{root: root, guard: guard}
	good := false
	defer func() {
		if !good {
			_ = h.Close()
		}
	}()
	raw, err := h.readPrivate("transfer.json", 8192)
	if err != nil {
		return nil, err
	}
	var cfg hostConfigWire
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&cfg) != nil || d.Decode(new(any)) != io.EOF || cfg.Version != 1 {
		return nil, ErrVersion
	}
	// Reuse the same HTTPS policy as every client; no alternate TLS path.
	c, err := New(cfg.Origin, nil, func(context.Context) (string, error) { return "", ErrAuthorization })
	if err != nil {
		return nil, ErrBinding
	}
	c.CloseIdleConnections()
	h.Origin = cfg.Origin
	sum := sha256.Sum256(raw)
	h.Binding = hex.EncodeToString(sum[:])
	if cfg.SourceRoot != "" {
		if !filepath.IsAbs(cfg.SourceRoot) || filepath.Clean(cfg.SourceRoot) != cfg.SourceRoot {
			return nil, ErrBinding
		}
		for _, other := range []string{getenv("HERDR_PLUGIN_CONFIG_DIR"), getenv("HERDR_PLUGIN_STATE_DIR")} {
			if rootsOverlap(cfg.SourceRoot, other) {
				return nil, ErrBinding
			}
		}
		h.sourcePath = cfg.SourceRoot
	}
	good = true
	return h, nil
}

func rootsOverlap(a, b string) bool {
	if !filepath.IsAbs(a) || !filepath.IsAbs(b) {
		return true
	}
	for _, pair := range [][2]string{{a, b}, {b, a}} {
		rel, err := filepath.Rel(pair[0], pair[1])
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (h *HostConfig) Close() error {
	closeQueueGuard(h.sourceGuard)
	if h.sources != nil {
		_ = h.sources.Close()
	}
	closeQueueGuard(h.guard)
	return h.root.Close()
}

func (h *HostConfig) readPrivate(name string, maxBytes int64) ([]byte, error) {
	f, err := openPrivateFile(h.root, name, os.O_RDONLY, 0)
	if err != nil {
		return nil, ErrBinding
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil || int64(len(raw)) > maxBytes {
		return nil, ErrBinding
	}
	return raw, nil
}

// Authorization rereads the same private credential on every request, allowing
// an explicitly managed rotation. It never refreshes, signs or persists a token.
func (h *HostConfig) Authorization(ctx context.Context) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	raw, err := h.readPrivate("device.credential", 4096)
	defer clear(raw)
	if err != nil {
		return "", ErrAuthorization
	}
	s := strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r")
	if len(s) == 0 || strings.ContainsAny(s, " \t\r\n") {
		return "", ErrAuthorization
	}
	return "Memoh-Transfer " + s, nil
}

// OpenSource accepts one logical filename, not an arbitrary path. The configured
// private source root is resolved once and held; links and hardlinks are rejected
// by the existing native private-file/immutable snapshot guards.
func (h *HostConfig) OpenSource(ref string) (*os.File, error) {
	if h.sourcePath == "" || !validID(ref) {
		return nil, ErrBinding
	}
	if err := h.openSources(); err != nil {
		return nil, err
	}
	return openPrivateFile(h.sources, ref, os.O_RDONLY, 0)
}

func (h *HostConfig) openSources() error {
	if h.sourcePath == "" {
		return ErrBinding
	}
	if h.sources == nil {
		var err error
		h.sources, h.sourceGuard, err = privateHostRoot(h.sourcePath)
		if err != nil {
			return err
		}
	}
	return nil
}
