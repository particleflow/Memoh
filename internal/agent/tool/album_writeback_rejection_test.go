package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	sdk "github.com/felinics/twilight/sdk"

	"github.com/felinics/memoh/internal/agent/toolexec"
	"github.com/felinics/memoh/internal/albumwriteback"
	"github.com/felinics/memoh/internal/apperror"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/media"
)

func albumRejectionHash(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

// A rejected queue call answers with a machine code and its catalog detail. No
// instruction sentence is written for the model: the pairing label is the
// user's statement, so the model must not be handed copy that explains it.
func TestAlbumWritebackRejectionCodes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		err  error
		code apperror.Code
	}{
		{albumwriteback.ErrInvalid, apperror.CodeAlbumQueueRejected},
		{albumwriteback.ErrAssetUnavailable, apperror.CodeAlbumAssetUnavailable},
	} {
		out, ok := albumWritebackRejection(tc.err)
		if !ok {
			t.Fatalf("%v did not map to a code", tc.err)
		}
		fields := rejectionFields(t, out)
		if fields["code"] != string(tc.code) || fields["ok"] != false {
			t.Fatalf("%v fields = %+v", tc.err, fields)
		}
		definition, ok := apperror.Lookup(tc.code)
		if !ok || fields["detail"] != definition.Detail || fields["message"] != definition.Detail {
			t.Fatalf("%v detail = %v, want %q", tc.err, fields["detail"], definition.Detail)
		}
	}
	if _, ok := albumWritebackRejection(context.Canceled); ok {
		t.Fatal("an unrelated error was reported as a queue rejection")
	}
}

// The tool itself must report the rejection as a normal tool result, not as a
// Go error: the model reads the code and decides what to say.
func TestAlbumWritebackToolReportsARejectionWithoutError(t *testing.T) {
	t.Parallel()

	provider := NewAlbumWritebackProvider(nil, albumwriteback.NewService(nil, albumToolStore{}, albumToolAssets{}))
	session := goldenSession
	session.UserID = "11111111-1111-1111-1111-111111111111"
	tools, err := provider.Tools(context.Background(), session)
	if err != nil || len(tools) != 1 {
		t.Fatalf("tools = %v, %v", tools, err)
	}
	out, err := tools[0].Execute(&toolexec.ToolExecContext{Context: context.Background()}, toolexec.ArgumentsFromValue(map[string]any{
		"pair_key":       "has space",
		"content_hashes": []string{albumRejectionHash("x")},
	}))
	if err != nil {
		t.Fatalf("validation-only rejection must not be a Go error: %v", err)
	}
	fields := rejectionFields(t, out)
	if fields["code"] != string(apperror.CodeAlbumQueueRejected) || fields["ok"] != false {
		t.Fatalf("fields = %+v", fields)
	}
}

// albumToolStore and albumToolAssets only satisfy the service's dependencies:
// the rejected call never reaches either.
type albumToolStore struct{}

func (albumToolStore) EnqueuePhoneAlbumWriteback(context.Context, dbstore.PhoneAlbumWritebackRecord) (dbstore.PhoneAlbumWritebackRecord, error) {
	return dbstore.PhoneAlbumWritebackRecord{}, nil
}

func (albumToolStore) ListPendingPhoneAlbumWritebacks(context.Context, string) ([]dbstore.PhoneAlbumWritebackRecord, error) {
	return nil, nil
}

func (albumToolStore) MarkPhoneAlbumWritebackWritten(context.Context, string, string, string) (dbstore.PhoneAlbumWritebackRecord, error) {
	return dbstore.PhoneAlbumWritebackRecord{}, db.ErrNotFound
}

type albumToolAssets struct{}

func (albumToolAssets) Stat(context.Context, string, string) (media.Asset, error) {
	return media.Asset{}, media.ErrAssetNotFound
}

// rejectionFields decodes a tool output object for assertions.
func rejectionFields(t *testing.T, out sdk.ToolOutput) map[string]any {
	t.Helper()
	var fields map[string]any
	if err := json.Unmarshal(out.JSON, &fields); err != nil {
		t.Fatalf("decode output %q: %v", out.Text, err)
	}
	return fields
}
