package alibabacloud

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	sdk "github.com/felinics/twilight/sdk"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newTestProvider(t *testing.T, transport http.RoundTripper) *Provider {
	t.Helper()
	provider, err := New("synthetic-key", "https://asr.example.invalid/v1")
	if err != nil {
		t.Fatal(err)
	}
	provider.client.Transport = transport
	return provider
}

func asrOKResponse(r *http.Request) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"synthetic transcript","annotations":[{"type":"audio_info","language":"zh"}]}}],"usage":{"seconds":42}}`)),
		Request:    r,
	}
}

// TestTranscribeSendsPayloadsBeyondTheFormerTenMegabyteCap pins the ruling
// that the encoded-input size gate is gone: a payload larger than the deleted
// 10 MB encoded limit reaches the upstream call instead of being refused.
func TestTranscribeSendsPayloadsBeyondTheFormerTenMegabyteCap(t *testing.T) {
	t.Parallel()

	payload := make([]byte, 12*1024*1024)
	copy(payload, "RIFF")
	sentBytes := 0
	provider := newTestProvider(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		sentBytes = len(body)
		return asrOKResponse(r), nil
	}))

	result, err := provider.DoTranscribe(context.Background(), sdk.TranscriptionParams{
		Audio: payload, Filename: "long.wav", ContentType: "audio/wav",
	})
	if err != nil {
		t.Fatalf("DoTranscribe() error = %v", err)
	}
	if result.Text != "synthetic transcript" {
		t.Fatalf("unexpected transcript %q", result.Text)
	}
	// Base64 inflates by 4/3, so the request body must carry the whole payload.
	if minimum := len(payload) / 4 * 3; sentBytes < minimum {
		t.Fatalf("upstream body = %d bytes, want at least %d for a %d-byte payload", sentBytes, minimum, len(payload))
	}
}

// TestRequestBudgetGrowsLinearlyWithPayload pins the linear budget: no fixed
// deadline caps a longer recording, and each KiB adds the same share.
func TestRequestBudgetGrowsLinearlyWithPayload(t *testing.T) {
	t.Parallel()

	small := asrRequestBudget(1024)
	medium := asrRequestBudget(2 * 1024)
	large := asrRequestBudget(1024 * 1024)
	huge := asrRequestBudget(64 * 1024 * 1024)

	if got, want := small, asrBudgetBase+asrBudgetPerKiB; got != want {
		t.Fatalf("1 KiB budget = %v, want %v", got, want)
	}
	if medium-small != asrBudgetPerKiB {
		t.Fatalf("each extra KiB must add %v, got %v", asrBudgetPerKiB, medium-small)
	}
	if large-asrBudgetBase != 1024*asrBudgetPerKiB {
		t.Fatalf("1 MiB budget share = %v, want %v", large-asrBudgetBase, 1024*asrBudgetPerKiB)
	}
	if huge <= large {
		t.Fatal("a larger payload must extend the deadline instead of hitting a cap")
	}
	// A partial KiB still gets its whole share, so the budget never shrinks
	// below the linear bound.
	if partial := asrRequestBudget(1025); partial != 2*asrBudgetPerKiB+asrBudgetBase {
		t.Fatalf("partial KiB budget = %v, want %v", partial, 2*asrBudgetPerKiB+asrBudgetBase)
	}
}

// TestRequestBudgetBoundsTheCall checks the deadline reaches the request
// context rather than being ignored.
func TestRequestBudgetBoundsTheCall(t *testing.T) {
	t.Parallel()

	var deadline time.Time
	provider := newTestProvider(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		deadline, _ = r.Context().Deadline()
		return asrOKResponse(r), nil
	}))

	start := time.Now()
	if _, err := provider.DoTranscribe(context.Background(), sdk.TranscriptionParams{
		Audio: make([]byte, 4096), Filename: "clip.wav", ContentType: "audio/wav",
	}); err != nil {
		t.Fatalf("DoTranscribe() error = %v", err)
	}
	if deadline.IsZero() {
		t.Fatal("request carried no deadline")
	}
	if got, want := deadline.Sub(start), asrRequestBudget(4096); got > want+time.Second || got < want-time.Second {
		t.Fatalf("request deadline = %v from start, want about %v", got, want)
	}
}
