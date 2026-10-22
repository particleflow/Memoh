//go:build linux || darwin

package transferclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHTTP499AssociationReopenCancelAndAuthorization(t *testing.T) {
	for _, resolveStatus := range []int{http.StatusOK, 499, http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(strconv.Itoa(resolveStatus), func(t *testing.T) {
			path := t.TempDir()
			q, _ := readyAssociationQueue(t, path)
			before := inspectQueue(t, q, "delivery-1")
			var commits, resolves atomic.Int64
			https := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.Header.Get("Authorization") != syntheticAuth || !strings.HasPrefix(r.URL.Path, "/internal/computer-deliveries/operation-1/delivery-1/") {
					t.Error("association scope changed")
				}
				if strings.HasSuffix(r.URL.Path, "/commit") {
					commits.Add(1) // Simulates a durable commit before canceled response.
					w.WriteHeader(499)
					return
				}
				resolves.Add(1)
				var body struct {
					Cancel bool `json:"cancel"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !body.Cancel {
					t.Error("cancel intent not retained", err)
				}
				w.WriteHeader(resolveStatus)
				if resolveStatus == http.StatusOK {
					_ = json.NewEncoder(w).Encode(map[string]any{"version": 2, "outcome": "committed", "revision": "4"})
				}
			}))
			defer https.Close()
			client, err := New(https.URL, https.Client(), func(context.Context) (string, error) { return syntheticAuth, nil })
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			binding := client.DeliveryBinding(nil)
			r := runQueue(t, q, client, binding)
			if r.AssociationState != AssociationUnknown || r.AssociationBlocked || r.AssociationDue.IsZero() {
				t.Fatal("canceled commit lost reconciliation state")
			}
			_ = q.Close()
			q = openQueue(t, path, queueLimits())
			if err := q.RequestCancel("delivery-1"); err != nil {
				t.Fatal(err)
			}
			for range 14 {
				if err := q.RequestCancel("delivery-1"); err != nil {
					t.Fatal(err)
				}
				r = runQueue(t, q, client, binding)
				if r.Associated {
					break
				}
			}
			if commits.Load() != 1 || r.Job.OperationID != before.Job.OperationID || r.Job.DeliveryID != before.Job.DeliveryID || !r.Job.CancelRequested {
				t.Fatal("reconciliation reissued commit or changed business identity")
			}
			if resolveStatus == http.StatusOK {
				if !r.Associated || r.AssociationState != AssociationCommitted || resolves.Load() != 1 {
					t.Fatal("cancel hid previously committed result")
				}
				return
			}
			wantCalls := int64(1)
			if resolveStatus == 499 {
				wantCalls = 12 // First cancel intent starts one bounded reconciliation budget.
			}
			if r.Associated || !r.AssociationBlocked || r.AssociationState != AssociationUnknown || resolves.Load() != wantCalls {
				t.Fatal("unknown outcome or authorization retry bound lost", resolves.Load())
			}
		})
	}
}
