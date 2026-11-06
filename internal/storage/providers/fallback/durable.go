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

// ListPrefixAcrossBots resolves a Bot-independent storage-key prefix against
// the canonical originals first, mirroring ListPrefix's precedence. The
// workspace is never contacted: access copies are not ownership.
func (p *Durable) ListPrefixAcrossBots(ctx context.Context, storageKeyPrefix string) ([]string, error) {
	keys, err := tryListPrefixAcrossBots(ctx, p.canonical, storageKeyPrefix)
	if len(keys) > 0 || (err != nil && !errors.Is(err, fs.ErrNotExist)) {
		return keys, err
	}
	return tryListPrefixAcrossBots(ctx, p.legacy, storageKeyPrefix)
}

func tryListPrefixAcrossBots(ctx context.Context, p storage.Provider, storageKeyPrefix string) ([]string, error) {
	lister, ok := p.(storage.CrossBotPrefixLister)
	if !ok {
		return nil, nil
	}
	return lister.ListPrefixAcrossBots(ctx, storageKeyPrefix)
}

func (p *Durable) EnsureAccessPath(ctx context.Context, key string) (string, error) {
	return p.MaterializeAccessPath(ctx, key, key)
}

// MaterializeAccessPath writes the verified original at sourceKey into the
// workspace under targetKey and returns the target's consumer-visible path.
// The canonical store is never written: a target in another Bot's namespace is
// a reading consumer's access copy of a reference, not a second owner of the
// bytes. Like EnsureAccessPath, the copy is refreshed from the verified
// original even when one already exists.
func (p *Durable) MaterializeAccessPath(ctx context.Context, targetKey, sourceKey string) (string, error) {
	source, err := p.Open(ctx, sourceKey)
	if err != nil {
		return "", err
	}
	defer func() { _ = source.Close() }()
	if err := p.workspace.Put(ctx, targetKey, source); err != nil {
		return "", fmt.Errorf("materialize media: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	value := strings.TrimSpace(p.workspace.AccessPath(ctx, targetKey))
	if value == "" {
		return "", storage.ErrAccessPathUnavailable
	}
	return value, nil
}
