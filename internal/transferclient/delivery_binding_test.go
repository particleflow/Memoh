package transferclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type publicationFixture struct{ outcome AssociationOutcome }

func (p publicationFixture) Resolve(context.Context, Reference, bool) (AssociationOutcome, error) {
	return p.outcome, nil
}

func (p publicationFixture) Publish(context.Context, Reference, io.Reader) (AssociationOutcome, error) {
	return p.outcome, nil
}

func TestDeliveryBindingLocalCancelEvidence(t *testing.T) {
	for _, item := range []struct {
		name                string
		local, remote, want AssociationOutcome
		publication, action string
		wantErr             error
	}{
		{"unknown-local", OutcomePending, OutcomeCanceled, OutcomePending, "", "", nil},
		{"absent-does-not-fence", OutcomeAbsent, OutcomeCanceled, OutcomePending, "", "", nil},
		{"published-wins", OutcomeCommitted, OutcomePending, OutcomePending, "published", "commit", nil},
		{"fenced", OutcomeCanceled, OutcomeCanceled, OutcomeCanceled, "fenced", "resolve", nil},
		{"conflicting-histories", OutcomeCanceled, OutcomeCommitted, "", "fenced", "resolve", ErrProtocol},
	} {
		t.Run(item.name, func(t *testing.T) {
			calls := 0
			https := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var body struct {
					BotID       string `json:"bot_id"`
					Publication string `json:"publication"`
					Cancel      bool   `json:"cancel"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.BotID != "bot" || body.Publication != item.publication || body.Cancel != (item.action == "resolve") || !strings.HasSuffix(r.URL.Path, "/"+item.action) {
					t.Errorf("incorrect local evidence: %+v %s", body, r.URL.Path)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"version": 2, "outcome": item.remote, "revision": "4"})
			}))
			defer https.Close()
			client, err := New(https.URL, https.Client(), func(context.Context) (string, error) { return "Bearer synthetic", nil })
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			ref := Reference{DeliveryID: "result", OperationID: "operation", BotID: "bot", ContentHash: strings.Repeat("a", 64), SizeBytes: 1, Mime: "application/octet-stream"}
			got, err := client.DeliveryBinding(publicationFixture{item.local}).Resolve(t.Context(), "download", ref, true)
			if !errors.Is(err, item.wantErr) || got != item.want {
				t.Fatalf("got %s %v, want %s %v", got, err, item.want, item.wantErr)
			}
			if (calls == 0) != (item.action == "") {
				t.Fatalf("unexpected NAS call count: %d", calls)
			}
		})
	}
}

func TestDeliveryBindingRequiresLocalStore(t *testing.T) {
	binding := (*Client)(nil).DeliveryBinding(nil)
	if err := binding.Commit(t.Context(), "download", Reference{}, strings.NewReader("fixture")); !errors.Is(err, ErrBinding) {
		t.Fatal(err)
	}
	if _, err := binding.Resolve(t.Context(), "download", Reference{}, true); !errors.Is(err, ErrBinding) {
		t.Fatal(err)
	}
}
