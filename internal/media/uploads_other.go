//go:build !unix

package media

import (
	"context"
	"errors"
	"os"
)

var errUploadPlatform = errors.New("resumable uploads require Unix advisory locks")

func openUploadFile(*os.Root, string, int, os.FileMode) (*os.File, error) {
	return nil, errUploadPlatform
}
func (*UploadStore) lock(context.Context, string) (func(), error) { return nil, errUploadPlatform }
