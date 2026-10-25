package transferclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

// StagingFile is supplied from the host's existing authorized locator. The host
// holds exclusive ownership until this call returns. No remote filename/path is
// used, and this package never publishes, renames or removes local files.
type StagingFile interface {
	io.ReadWriteSeeker
	Truncate(int64) error
	Sync() error
}

// Download resumes into a private staging handle. checkpoint must persist only
// fsynced offsets as INCOMPLETE. A nil return is the separate full-hash-verified
// signal; the host must then durably publish before reporting a ready result.
// In particular, offset == size alone is not a completion acknowledgement.
func (c *Client) Download(ctx context.Context, bot, hash string, size, offset int64, file StagingFile, checkpoint func(int64) error) error {
	path, err := mediaPath(bot)
	if err != nil || !validHash(hash) || size <= 0 || offset < 0 || offset > size || file == nil || checkpoint == nil {
		return ErrInvalid
	}
	// Do not extend a short/corrupt local checkpoint with zero-filled bytes.
	end, err := file.Seek(0, io.SeekEnd)
	if err != nil || end < offset {
		return ErrIntegrity
	}
	if file.Truncate(offset) != nil {
		return ErrCheckpoint
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return ErrCheckpoint
	}
	if offset == size {
		// A fully downloaded but unpublished checkpoint still needs a current
		// grant. Local bytes are not authority to publish a revoked task input.
		resp, err := c.request(ctx, http.MethodHead, path+"/"+hash, nil, nil)
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return statusError(resp)
		}
		if resp.ContentLength != size || resp.Header.Get("ETag") != `"`+hash+`"` {
			return ErrProtocol
		}
	}
	if offset < size {
		headers := http.Header{}
		if offset > 0 {
			headers.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
			headers.Set("If-Range", `"`+hash+`"`)
		}
		resp, err := c.request(ctx, http.MethodGet, path+"/"+hash, nil, headers)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()
		want := http.StatusOK
		if offset > 0 {
			want = http.StatusPartialContent
		}
		if resp.StatusCode != want {
			if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
				return ErrProtocol
			}
			return statusError(resp)
		}
		if resp.Header.Get("ETag") != `"`+hash+`"` || resp.ContentLength != size-offset || resp.Header.Get("Content-Encoding") != "" && resp.Header.Get("Content-Encoding") != "identity" {
			return ErrProtocol
		}
		if offset > 0 && resp.Header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", offset, size-1, size) {
			return ErrProtocol
		}
		buffer := make([]byte, copyBytes)
		durable := offset
		for offset < size {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			n, readErr := resp.Body.Read(buffer[:min(int64(len(buffer)), size-offset)])
			if n > 0 {
				written, err := file.Write(buffer[:n])
				if errors.Is(err, errDiskSpace) {
					return err
				}
				if err != nil || written != n {
					return ErrCheckpoint
				}
				offset += int64(n)
			}
			if offset-durable >= MaxChunkBytes || offset == size {
				if err := file.Sync(); err != nil {
					if errors.Is(err, errDiskSpace) {
						return err
					}
					return ErrCheckpoint
				}
				if checkpoint(offset) != nil {
					return ErrCheckpoint
				}
				durable = offset
			}
			if readErr != nil && (readErr != io.EOF || offset < size) {
				return ErrTransport
			}
		}
		// A framed response must end here; never consume an unbounded body.
		var extra [1]byte
		if n, err := resp.Body.Read(extra[:]); n != 0 || err != io.EOF {
			return ErrProtocol
		}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return ErrCheckpoint
	}
	return Verify(ctx, file, Spec{SizeBytes: size, SHA256: hash, Mime: "application/octet-stream"})
}

// Verify hashes a bounded stream and rejects truncation and trailing bytes.
// A host must also prevent source mutation and validate OS handle identity.
func Verify(ctx context.Context, source io.Reader, spec Spec) error {
	if !spec.valid() {
		return ErrInvalid
	}
	h := sha256.New()
	buffer := make([]byte, copyBytes)
	remaining := spec.SizeBytes
	for remaining > 0 {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n, err := io.ReadFull(source, buffer[:min(int64(len(buffer)), remaining)])
		if err != nil {
			return ErrIntegrity
		}
		_, _ = h.Write(buffer[:n])
		remaining -= int64(n)
	}
	var extra [1]byte
	if n, err := source.Read(extra[:]); n != 0 || err != io.EOF || hex.EncodeToString(h.Sum(nil)) != spec.SHA256 {
		return ErrIntegrity
	}
	return nil
}
