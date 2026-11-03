package media

import (
	"errors"
	"fmt"
	"io"
)

const (
	// MaxAssetBytes bounds legacy inline and channel attachment fetches that
	// must buffer a remote body. It is no longer the upload admission default.
	MaxAssetBytes int64 = 200 * 1024 * 1024
	// MaxUploadCeilingBytes is the default single-file upload ceiling: the
	// largest size the configuration and JSON limits can represent (the same
	// bound files.max_upload_bytes validates). It is a protocol numeric
	// ceiling, not an artificial throttle.
	MaxUploadCeilingBytes int64 = (1 << 53) - 1
)

// ReadAllWithLimit reads from reader and rejects payloads larger than maxBytes.
func ReadAllWithLimit(reader io.Reader, maxBytes int64) ([]byte, error) {
	if reader == nil {
		return nil, errors.New("reader is required")
	}
	if maxBytes <= 0 {
		return nil, errors.New("max bytes must be greater than 0")
	}
	limited := &io.LimitedReader{
		R: reader,
		N: maxBytes + 1,
	}
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: max %d bytes", ErrAssetTooLarge, maxBytes)
	}
	return data, nil
}
