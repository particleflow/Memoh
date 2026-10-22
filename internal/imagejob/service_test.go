package imagejob

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
)

func syntheticPNG(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			im.SetRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type failingOriginals struct {
	*localfs.Originals
	fail atomic.Bool
}

func (p *failingOriginals) Put(ctx context.Context, key string, r io.Reader) error {
	if p.fail.Load() {
		return errors.New("PRIVATE_DISK_CAUSE")
	}
	return p.Originals.Put(ctx, key, r)
}

type imageHarness struct {
	service       *Service
	media         *media.Service
	journal       *media.ImageTaskJournal
	store         *failingOriginals
	binding       Binding
	request       Request
	input, output []byte
	root          string
	allowed       atomic.Bool
}

func harness(t *testing.T) *imageHarness {
	t.Helper()
	h := &imageHarness{root: t.TempDir(), binding: Binding{TeamID: "team-fixture", BotID: "bot-fixture", SessionID: "child-fixture", UserID: "owner-fixture"}}
	h.store = &failingOriginals{Originals: localfs.NewOriginals(h.root)}
	h.media = media.NewService(nil, h.store)
	h.media.SetRequirePersistence(true)
	if err := h.media.EnableUploads(h.root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.media.Uploads().Close() })
	var err error
	h.journal, err = media.OpenImageTaskJournal(h.root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.journal.Close() })
	h.input = syntheticPNG(t, color.RGBA{R: 255, A: 255})
	h.output = syntheticPNG(t, color.RGBA{B: 255, A: 255})
	asset, err := h.media.Ingest(context.Background(), media.IngestInput{BotID: h.binding.BotID, Mime: "image/png", Reader: bytes.NewReader(h.input)})
	if err != nil {
		t.Fatal(err)
	}
	h.request = Request{Key: "synthetic-task", Mode: Edit, Prompt: "change red to blue", InputRefs: []string{asset.ContentHash}}
	h.allowed.Store(true)
	h.service = New(h.media, h.journal, h.authorize)
	return h
}

func (h *imageHarness) authorize(_ context.Context, b Binding, refs []string) error {
	if !h.allowed.Load() || b != h.binding {
		return ErrDenied
	}
	for _, ref := range refs {
		if ref != h.request.InputRefs[0] {
			return ErrDenied
		}
	}
	return nil
}

func (h *imageHarness) adapter(t *testing.T, status int) (*OpenAIAdapter, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/images/edits" {
			t.Errorf("unexpected provider request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer func() { _ = r.MultipartForm.RemoveAll() }()
		if _, ok := r.MultipartForm.Value["response_format"]; ok {
			t.Error("unsupported legacy response_format was sent")
		}
		file, _, err := r.FormFile("image[]")
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		data, err := io.ReadAll(file)
		_ = file.Close()
		if err != nil || !bytes.Equal(data, h.input) || r.FormValue("prompt") != h.request.Prompt {
			t.Error("provider did not receive the exact authorized input")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status != 200 {
			_, _ = w.Write([]byte(`{"error":{"message":"PRIVATE_PROVIDER_CAUSE"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(h.output)}}})
	}))
	t.Cleanup(s.Close)
	adapter, err := NewOpenAIAdapter("fixture-provider/model-v1", "synthetic-image-editor", s.URL, "", s.Client(), false, true)
	if err != nil {
		t.Fatal(err)
	}
	return adapter, &calls
}

func TestEditHTTPPublishesReadyReferenceAndDoesNotResubmit(t *testing.T) {
	h := harness(t)
	a, calls := h.adapter(t, 200)
	r, err := h.service.Submit(context.Background(), h.binding, h.request, a)
	if err != nil || r.State != "ready" || len(r.Artifacts) != 1 {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	reader, _, err := h.media.Open(context.Background(), h.binding.BotID, r.Artifacts[0].ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || !bytes.Equal(got, h.output) || r.Artifacts[0].SizeBytes != int64(len(h.output)) {
		t.Fatal("ready reference does not resolve to provider output")
	}
	for range 2 {
		repeat, err := h.service.Submit(context.Background(), h.binding, h.request, a)
		if err != nil || repeat.Artifacts[0] != r.Artifacts[0] {
			t.Fatalf("repeat=%+v err=%v", repeat, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
	raw, _ := json.Marshal(r)
	if bytes.Contains(raw, []byte("base64")) || bytes.Contains(raw, []byte("PRIVATE")) {
		t.Fatal("result leaked content")
	}
}

func TestPublicationFailureResumesAfterJournalReopenWithoutProvider(t *testing.T) {
	h := harness(t)
	a, calls := h.adapter(t, 200)
	h.store.fail.Store(true)
	r, err := h.service.Submit(context.Background(), h.binding, h.request, a)
	if err != nil || r.State != "publication_pending" || len(r.Artifacts) != 0 {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	journal, err := media.OpenImageTaskJournal(h.root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = journal.Close() }()
	restarted := New(h.media, journal, h.authorize)
	h.allowed.Store(false)
	if _, err = restarted.Result(context.Background(), h.binding, h.request.Key); !errors.Is(err, ErrDenied) {
		t.Fatal("revocation must block recovery")
	}
	h.allowed.Store(true)
	h.store.fail.Store(false)
	r, err = restarted.Result(context.Background(), h.binding, h.request.Key)
	if err != nil || r.State != "ready" || len(r.Artifacts) != 1 || calls.Load() != 1 {
		t.Fatalf("result=%+v err=%v calls=%d", r, err, calls.Load())
	}
	h.allowed.Store(false)
	if _, err = restarted.Result(context.Background(), h.binding, h.request.Key); !errors.Is(err, ErrDenied) {
		t.Fatal("ready cache bypassed revocation")
	}
}

func TestUnknownBlocksSameAndDifferentSubmissionAfterRestart(t *testing.T) {
	h := harness(t)
	a, calls := h.adapter(t, 503)
	r, err := h.service.Submit(context.Background(), h.binding, h.request, a)
	if err != nil || r.State != "unknown" {
		t.Fatalf("result=%+v err=%v", r, err)
	}
	journal, err := media.OpenImageTaskJournal(h.root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = journal.Close() }()
	restarted := New(h.media, journal, h.authorize)
	r, err = restarted.Submit(context.Background(), h.binding, h.request, a)
	if err != nil || r.State != "unknown" {
		t.Fatalf("repeat=%+v err=%v", r, err)
	}
	changed := h.request
	changed.Key = "different-task"
	changed.Prompt = "try a different color"
	r, err = restarted.Submit(context.Background(), h.binding, changed, a)
	if err != nil || r.Code != "image.blocked_by_task" || r.TaskID != h.request.Key || calls.Load() != 1 {
		t.Fatalf("changed=%+v err=%v calls=%d", r, err, calls.Load())
	}
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), "PRIVATE_PROVIDER") {
		t.Fatal("provider diagnostic leaked")
	}
}

func TestAuthorizationAndEditCapabilityFailBeforeDispatch(t *testing.T) {
	h := harness(t)
	a, calls := h.adapter(t, 200)
	a.CanEdit = false
	if _, err := h.service.Submit(context.Background(), h.binding, h.request, a); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unsupported=%v", err)
	}
	a.CanEdit = true
	other := h.binding
	other.SessionID = "other-session"
	if _, err := h.service.Submit(context.Background(), other, h.request, a); !errors.Is(err, ErrDenied) {
		t.Fatal("other session was accepted")
	}
	request := h.request
	request.InputRefs = []string{strings.Repeat("f", 64)}
	if _, err := h.service.Submit(context.Background(), h.binding, request, a); !errors.Is(err, ErrDenied) {
		t.Fatal("ungranted input accepted")
	}
	if calls.Load() != 0 {
		t.Fatal("rejection dispatched a provider request")
	}
}

func TestGenerateIsExplicitAndHasNoEditInput(t *testing.T) {
	h := harness(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/images/generations" {
			t.Error("wrong generation endpoint")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if _, ok := body["image"]; ok {
			t.Error("generation received an edit input")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(h.output)}}})
	}))
	defer server.Close()
	adapter, err := NewOpenAIAdapter("fixture/generation", "fixture-image", server.URL, "", server.Client(), true, false)
	if err != nil {
		t.Fatal(err)
	}
	request := h.request
	request.Mode = Generate
	request.InputRefs = nil
	r, err := h.service.Submit(context.Background(), h.binding, request, adapter)
	if err != nil || r.State != "ready" || calls.Load() != 1 {
		t.Fatalf("result=%+v err=%v calls=%d", r, err, calls.Load())
	}
}
