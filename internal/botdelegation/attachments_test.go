package botdelegation

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/media"
)

type originalFixture struct {
	asset        media.Asset
	content      string
	opens, stats int
	owner        string
}

func (f *originalFixture) Stat(_ context.Context, owner, _ string) (media.Asset, error) {
	f.stats++
	f.owner = owner
	return f.asset, nil
}

func (f *originalFixture) Open(_ context.Context, owner, _ string) (io.ReadCloser, media.Asset, error) {
	f.opens++
	f.owner = owner
	return io.NopCloser(strings.NewReader(f.content)), f.asset, nil
}

func attachmentFixture() (*AttachmentSet, *authorityFixture, *originalFixture, string) {
	a, f, b, _ := fixture()
	hash := strings.Repeat("a", 64)
	f.assets["manager/root/"+hash] = true
	o := &originalFixture{asset: media.Asset{ContentHash: hash, BotID: "manager", Mime: "text/plain", SizeBytes: 7, StorageKey: "SECRET/internal/path"}, content: "fixture"}
	return &AttachmentSet{Authority: a, Originals: o, Identity: b, Inputs: []string{hash}}, f, o, hash
}

func TestHandoffDescribesAndReadsSameOriginalWithoutCopying(t *testing.T) {
	s, _, o, hash := attachmentFixture()
	got, err := s.Describe(t.Context(), hash, false)
	if err != nil || got.ContentHash != hash || got.SizeBytes != 7 {
		t.Fatalf("descriptor=%+v error=%v", got, err)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "SECRET") || strings.Contains(string(raw), "fixture") || o.opens != 0 {
		t.Fatalf("descriptor leaked original/path or opened bytes: %s", raw)
	}
	r, err := s.Open(t.Context(), hash, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	data, err := io.ReadAll(r)
	if err != nil || string(data) != "fixture" || o.owner != "manager" || o.opens != 1 {
		t.Fatalf("original=%q owner=%s error=%v", data, o.owner, err)
	}
}

func TestHandoffAttachmentRevocationStopsOpenStreamAndFutureMetadata(t *testing.T) {
	s, f, o, hash := attachmentFixture()
	r, err := s.Open(t.Context(), hash, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	buf := make([]byte, 2)
	if n, err := r.Read(buf); err != nil || n != 2 {
		t.Fatalf("initial read=%d %v", n, err)
	}
	f.denied["specialist/chat"] = true
	if n, err := r.Read(buf); n != 0 || !errors.Is(err, ErrDenied) {
		t.Fatalf("read after revocation=%d %v", n, err)
	}
	if _, err := s.Describe(t.Context(), hash, false); !errors.Is(err, ErrDenied) {
		t.Fatalf("metadata after revocation=%v", err)
	}
	if _, err := s.Open(t.Context(), hash, false); !errors.Is(err, ErrDenied) {
		t.Fatalf("reopen after revocation=%v", err)
	}
	if o.opens != 1 || o.stats != 0 {
		t.Fatalf("storage accessed after revocation: %+v", o)
	}
}

func TestHandoffRejectsUndelegatedHashEvenWhenUserOwnsIt(t *testing.T) {
	s, f, o, _ := attachmentFixture()
	other := strings.Repeat("b", 64)
	f.assets["manager/root/"+other] = true
	if _, err := s.Open(t.Context(), other, false); !errors.Is(err, ErrDenied) {
		t.Fatalf("undelegated=%v", err)
	}
	if o.opens != 0 {
		t.Fatal("undelegated original opened")
	}
}

func TestResultAttachmentReadsSpecialistOriginalWithBothGrants(t *testing.T) {
	s, f, o, hash := attachmentFixture()
	s.Results = []string{hash}
	f.assets["specialist/specialist-session/"+hash] = true
	o.asset.BotID = "specialist"
	r, err := s.Open(t.Context(), hash, true)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	if o.owner != "specialist" {
		t.Fatalf("owner=%q", o.owner)
	}
	f.denied["manager/chat"] = true
	if _, err := s.Open(t.Context(), hash, true); !errors.Is(err, ErrDenied) {
		t.Fatalf("result after source revocation=%v", err)
	}
}
