package transferclient

import (
	"errors"
	"os"
)

// Internal cause, projected as the existing capacity class; no paths/OS errors
// enter the CLI response. A full metadata bucket remains a different condition.
var errDiskSpace = errors.Join(ErrCapacity, errors.New("download filesystem space unavailable"))

func (q *DiskQueue) checkDownloadSpace(bytes int64) error {
	check := q.downloadSpaceCheck
	if check == nil {
		check = func(n int64) error { return queueFreeSpace(q.root, n) }
	}
	if err := check(bytes); err != nil {
		if errors.Is(err, ErrCapacity) {
			return errors.Join(errDiskSpace, err)
		}
		return err
	}
	return nil
}

type downloadFile struct {
	*os.File
	queue                *DiskQueue
	remaining, allowance int64
}

func (q *DiskQueue) downloadStaging(f *os.File, remaining int64) (*downloadFile, error) {
	out := &downloadFile{File: f, queue: q, remaining: remaining}
	if remaining > 0 {
		out.allowance = min(remaining, int64(MaxChunkBytes))
		if err := q.checkDownloadSpace(out.allowance); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// At most one existing protocol chunk is checked at a time. A check is not a
// reservation against other applications: real ENOSPC/EDQUOT is also retained
// as capacity pressure. Unconfirmed tails are truncated to the synced offset
// by Download on the next invocation, without discarding the durable prefix.
func (f *downloadFile) Write(p []byte) (int, error) {
	if f.allowance < int64(len(p)) {
		f.allowance = min(f.remaining, int64(MaxChunkBytes))
		if err := f.queue.checkDownloadSpace(f.allowance); err != nil {
			return 0, err
		}
	}
	n, err := f.File.Write(p)
	f.remaining -= int64(n)
	f.allowance -= int64(n)
	if queueDiskFull(err) {
		return n, errDiskSpace
	}
	return n, err
}

func (f *downloadFile) Sync() error {
	err := f.File.Sync()
	if queueDiskFull(err) {
		return errDiskSpace
	}
	return err
}
