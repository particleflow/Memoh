package fallback

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/felinics/memoh/internal/storage"
)

// Durable keeps originals in canonical storage. Workspace copies are disposable.
// Legacy objects are copied, never moved, on their first successful read.
type Durable struct {
	*Provider
	canonical storage.Provider
	workspace storage.Provider
	legacy    storage.Provider
}

func NewDurable(workspace, canonical, spill storage.Provider) *Durable {
	legacy := New(workspace, spill)
	return &Durable{
		Provider:  New(canonical, legacy),
		canonical: canonical, workspace: workspace, legacy: legacy,
	}
}

func (p *Durable) Put(ctx context.Context, key string, reader io.Reader) error {
	// Never acknowledge a workspace-only write when the original cannot be saved.
	return p.canonical.Put(ctx, key, reader)
}

// EnsureDurable must reach the canonical provider, never the workspace copy.
func (p *Durable) EnsureDurable(ctx context.Context, key string) error {
	ensurer, ok := p.canonical.(storage.DurabilityEnsurer)
	if !ok {
		return errors.New("canonical storage cannot confirm publication durability")
	}
	return ensurer.EnsureDurable(ctx, key)
}

func (p *Durable) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	reader, err := p.canonical.Open(ctx, key)
	if err == nil || !errors.Is(err, fs.ErrNotExist) {
		return reader, err
	}
	source, err := p.legacy.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	defer func() { _ = source.Close() }()
	if err := p.canonical.Put(ctx, key, source); err != nil {
		return nil, fmt.Errorf("preserve legacy media: %w", err)
	}
	return p.canonical.Open(ctx, key)
}

func (p *Durable) OpenVerifiedRange(ctx context.Context, key string) (storage.VerifiedRangeReader, error) {
	opener, ok := p.canonical.(storage.VerifiedRangeOpener)
	if !ok {
		return nil, storage.ErrVerifiedRangeUnsupported
	}
	r, err := opener.OpenVerifiedRange(ctx, key)
	if err == nil || !errors.Is(err, fs.ErrNotExist) {
		return r, err
	}
	// Keep the existing copy-on-first-read migration. Corrupt or changed
	// canonical objects must never silently select a workspace copy.
	legacy, err := p.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	_ = legacy.Close()
	return opener.OpenVerifiedRange(ctx, key)
}

func (p *Durable) AccessPath(ctx context.Context, key string) string {
	value, _ := p.EnsureAccessPath(ctx, key)
	return value
}

// ListPrefix resolves preserved originals without contacting a workspace. This
// keeps hash references readable while that workspace is absent or restarting.
func (p *Durable) ListPrefix(ctx context.Context, prefix string) ([]string, error) {
	keys, err := tryListPrefix(ctx, p.canonical, prefix)
	if len(keys) > 0 || (err != nil && !errors.Is(err, fs.ErrNotExist)) {
		return keys, err
	}
	return tryListPrefix(ctx, p.legacy, prefix)
}

func (p *Durable) EnsureAccessPath(ctx context.Context, key string) (string, error) {
	source, err := p.Open(ctx, key)
	if err != nil {
		return "", err
	}
	defer func() { _ = source.Close() }()
	// Refresh from the verified original even if a writable workspace copy
	// exists: tools may have modified it since the previous materialization.
	if err := p.workspace.Put(ctx, key, source); err != nil {
		return "", fmt.Errorf("materialize media: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	value := strings.TrimSpace(p.workspace.AccessPath(ctx, key))
	if value == "" {
		return "", storage.ErrAccessPathUnavailable
	}
	return value, nil
}
