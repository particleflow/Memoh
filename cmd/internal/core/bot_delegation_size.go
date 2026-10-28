package core

import (
	"context"
	"errors"
	"io"

	"github.com/felinics/memoh/internal/botdelegation"
	"github.com/felinics/memoh/internal/media"
)

// Locator metadata may intentionally omit size. Measure the same authorized,
// verified original rather than trusting a tool-supplied number or copying it.
func delegationOriginalSize(ctx context.Context, open func(context.Context) (media.ReadSeekCloser, error)) (size int64, err error) {
	if err = ctx.Err(); err != nil {
		return 0, err
	}
	reader, err := open(ctx)
	if reader != nil {
		defer func() { err = errors.Join(err, reader.Close()) }()
	}
	if err != nil {
		return 0, err
	}
	if reader == nil {
		return 0, botdelegation.ErrDenied
	}
	size, err = reader.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	if size < 0 {
		return 0, errors.New("verified attachment size is unavailable")
	}
	return size, ctx.Err()
}
