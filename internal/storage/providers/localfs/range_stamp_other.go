//go:build !linux

package localfs

import (
	"os"

	"github.com/felinics/memoh/internal/storage"
)

func originalStamp(os.FileInfo) (fileStamp, error) {
	return fileStamp{}, storage.ErrVerifiedRangeUnsupported
}

func rangeFilesystem(*os.File) error { return storage.ErrVerifiedRangeUnsupported }
