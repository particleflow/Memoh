package botdelegation

import (
	"context"
	"io"
	"slices"

	"github.com/felinics/memoh/internal/media"
)

// Originals is media.Service's existing read-only surface. Delegation neither
// copies an original into the destination Bot nor creates a broad Bot+hash alias.
type Originals interface {
	Stat(context.Context, string, string) (media.Asset, error)
	Open(context.Context, string, string) (io.ReadCloser, media.Asset, error)
}

type Attachment struct {
	ContentHash string `json:"content_hash"`
	Mime        string `json:"mime,omitempty"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
}

// AttachmentSet is constructed from a server-owned delegation record. Reusing
// a NAS reference transfers no permanent grant and requires no per-file ticket.
// An HTTP/tool adapter must never populate these fields from caller metadata.
type AttachmentSet struct {
	Authority *Authority
	Originals Originals
	Identity  Identity
	Inputs    []string
	Results   []string
}

func (s *AttachmentSet) authorize(ctx context.Context, hash string, result bool) (string, error) {
	if s == nil || s.Authority == nil || s.Originals == nil {
		return "", ErrDenied
	}
	refs, owner := s.Inputs, s.Identity.SourceBotID
	if result {
		refs, owner = s.Results, s.Identity.TargetBotID
	}
	if !slices.Contains(refs, hash) {
		return "", ErrDenied
	}
	if err := s.Authority.AuthorizeAsset(ctx, s.Identity, hash, result); err != nil {
		return "", err
	}
	return owner, nil
}

// Describe returns only metadata; no URL, storage key, private path, or inline
// bytes can escape into the public projection or model context.
func (s *AttachmentSet) Describe(ctx context.Context, hash string, result bool) (Attachment, error) {
	owner, err := s.authorize(ctx, hash, result)
	if err != nil {
		return Attachment{}, err
	}
	a, err := s.Originals.Stat(ctx, owner, hash)
	if err != nil {
		return Attachment{}, err
	}
	if a.BotID != owner || a.ContentHash != hash {
		return Attachment{}, ErrDenied
	}
	return Attachment{ContentHash: hash, Mime: a.Mime, SizeBytes: a.SizeBytes}, nil
}

// Open opens that same owner's original, not a copied or destination-scoped
// object. The next read after revocation fails even on an already-open stream.
func (s *AttachmentSet) Open(ctx context.Context, hash string, result bool) (io.ReadCloser, error) {
	owner, err := s.authorize(ctx, hash, result)
	if err != nil {
		return nil, err
	}
	r, a, err := s.Originals.Open(ctx, owner, hash)
	if err != nil {
		return nil, err
	}
	if r == nil || a.BotID != owner || a.ContentHash != hash {
		if r != nil {
			_ = r.Close()
		}
		return nil, ErrDenied
	}
	return &authorizedReader{ReadCloser: r, authorize: func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err := s.authorize(ctx, hash, result)
		return err
	}}, nil
}

type authorizedReader struct {
	io.ReadCloser
	authorize func() error
}

func (r *authorizedReader) Read(p []byte) (int, error) {
	if err := r.authorize(); err != nil {
		return 0, err
	}
	return r.ReadCloser.Read(p)
}
