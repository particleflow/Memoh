package application

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/models"
	"github.com/felinics/memoh/internal/storage"
)

// An attachment passes admission by opening its original. If that original can
// no longer be read when the gateway is prepared, the turn has to fail with a
// code the client can act on. Answering without the file would tell the model
// the user sent nothing, which is the silent drop this guard exists to stop.
func TestPrepareGatewayAttachments_FailsWhenAdmittedOriginalIsGone(t *testing.T) {
	t.Parallel()

	resolver := &Service{
		logger: slog.Default(),
		assetLoader: &fakeGatewayAssetLoader{
			accessPathFn: func(context.Context, string, string) (string, error) {
				return "", media.ErrAssetNotFound
			},
			openFn: func(context.Context, string, string) (io.ReadCloser, string, error) {
				return nil, "", media.ErrAssetNotFound
			},
		},
	}
	model := models.GetResponse{}
	req := ChatRequest{
		BotID: "bot-1",
		Attachments: []ChatAttachment{{
			Type:        "file",
			Mime:        "application/pdf",
			ContentHash: "gone-hash",
			Name:        "report.pdf",
		}},
	}

	_, err := resolver.routeAndMergeAttachments(context.Background(), model, req)
	if err == nil {
		t.Fatal("routeAndMergeAttachments() = nil error, want the turn to fail")
	}
	if code := apperror.CodeOf(err); code != apperror.CodeMediaAttachmentGone {
		t.Fatalf("error code = %q, want %q", code, apperror.CodeMediaAttachmentGone)
	}
}

// The status distinguishes the three ways an admitted original can become
// unreadable, so the client knows whether to resend or retry.
func TestAttachmentUnreachableError_NamesTheCause(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want apperror.Code
	}{
		{"bytes gone", media.ErrAssetNotFound, apperror.CodeMediaAttachmentGone},
		{"closed file", &notExistError{}, apperror.CodeMediaAttachmentGone},
		{"no access path", storage.ErrAccessPathUnavailable, apperror.CodeMediaAttachmentUnreadable},
		{"storage failure", errors.New("disk unavailable"), apperror.CodeMediaAttachmentUnavailable},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := attachmentUnreachableError(tt.err)
			if code := apperror.CodeOf(err); code != tt.want {
				t.Fatalf("code = %q, want %q", code, tt.want)
			}
			definition, ok := apperror.Lookup(tt.want)
			if !ok {
				t.Fatalf("%q is not in the catalog", tt.want)
			}
			// The client reads the catalog copy, never this error's own text.
			if strings.Contains(definition.Detail, "loader") || strings.Contains(definition.Detail, "media store") {
				t.Fatalf("catalog copy names an internal component: %q", definition.Detail)
			}
		})
	}
}

// A reachable original keeps today's behaviour: the turn prepares the same
// attachment and produces no error.
func TestPrepareGatewayAttachments_ReachableOriginalIsUnchanged(t *testing.T) {
	t.Parallel()

	resolver := &Service{
		logger: slog.Default(),
		assetLoader: &fakeGatewayAssetLoader{
			accessPathFn: func(_ context.Context, botID, contentHash string) (string, error) {
				if botID != "bot-1" || contentHash != "present-hash" {
					t.Fatalf("unexpected lookup: bot=%q hash=%q", botID, contentHash)
				}
				return "/data/.memoh/media/aa/present.pdf", nil
			},
		},
	}
	req := ChatRequest{
		BotID: "bot-1",
		Attachments: []ChatAttachment{{
			Type:        "file",
			Mime:        "application/zip",
			ContentHash: "present-hash",
			Name:        "bundle.zip",
		}},
	}

	merged, err := resolver.routeAndMergeAttachments(context.Background(), models.GetResponse{}, req)
	if err != nil {
		t.Fatalf("routeAndMergeAttachments() error = %v", err)
	}
	if len(merged) != 1 {
		t.Fatalf("routeAndMergeAttachments() = %d attachments, want 1", len(merged))
	}
	item, ok := merged[0].(gatewayAttachment)
	if !ok || item.Transport != gatewayTransportToolFileRef || item.Payload != "/data/.memoh/media/aa/present.pdf" {
		t.Fatalf("merged attachment = %#v, want the reachable original as a file reference", merged[0])
	}
}

// A not-found return from the file system is the same "bytes are gone" answer
// as the media store's own sentinel, so both map to the same code.
type notExistError struct{}

func (*notExistError) Error() string { return "file does not exist" }

func (*notExistError) Unwrap() error { return fs.ErrNotExist }
