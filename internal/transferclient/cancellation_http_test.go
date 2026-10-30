package transferclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTP499AfterMutationReconcilesWithoutBlindReplay(t *testing.T) {
	for _, mutation := range []string{"create", "patch", "complete"} {
		t.Run(mutation, func(t *testing.T) {
			f := &fixture{calls: map[string]int{}}
			path := "/bots/bot-1/media/uploads/receipt-1"
			method := http.MethodPatch
			switch mutation {
			case "create":
				method, path = http.MethodPost, "/bots/bot-1/media/uploads"
			case "complete":
				method, path = http.MethodPost, path+"/complete"
			}
			var delivered atomic.Bool
			https := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == method && r.URL.Path == path && !delivered.Swap(true) {
					f.serve(httptest.NewRecorder(), r) // Server mutation succeeds first.
					w.WriteHeader(499)
					return
				}
				f.serve(w, r)
			}))
			defer https.Close()
			client, err := New(https.URL, https.Client(), func(context.Context) (string, error) { return syntheticAuth, nil })
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			source := frozen("abcd")
			initial := newJob(source)
			store := storeJob(t, initial)
			var final Job
			for range 7 {
				final = advance(t, client, store, source)
			}
			if final.DeliveryID != initial.DeliveryID || final.OperationID != initial.OperationID || final.SnapshotID != initial.SnapshotID {
				t.Fatal("retry rebound business identity")
			}
			if f.count(method+" "+path) != 1 {
				t.Fatal("mutating request blindly replayed")
			}
			if mutation == "create" {
				if final.Phase != "create_unknown" || final.ReceiptID != "" {
					t.Fatalf("ambiguous create lost: %+v", final)
				}
			} else if final.Phase != "ready" || final.Ready == nil {
				t.Fatalf("known receipt did not reconcile: %+v", final)
			}
		})
	}
}

func TestHTTP499DownloadRetainsRangeAndHash(t *testing.T) {
	data := []byte("0123456789")
	hash := digest(data)
	var requests atomic.Int64
	https := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bots/bot-1/media/"+hash || r.Header.Get("Range") != "bytes=4-" || r.Header.Get("If-Range") != `"`+hash+`"` || r.Header.Get("Authorization") != syntheticAuth {
			t.Error("resume changed scope or range")
		}
		if requests.Add(1) == 1 {
			w.WriteHeader(499)
			return
		}
		w.Header().Set("ETag", `"`+hash+`"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(data)-4))
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 4-%d/%d", len(data)-1, len(data)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[4:])
	}))
	defer https.Close()
	client, err := New(https.URL, https.Client(), func(context.Context) (string, error) { return syntheticAuth, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	file, err := os.CreateTemp(t.TempDir(), "part")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	if _, err := file.Write(data[:4]); err != nil {
		t.Fatal(err)
	}
	job := newJob(data)
	job.Offset = 4
	store := storeJob(t, job)
	checkpoint := func(n int64) error { job.Offset = n; return store.save(job) }
	err = client.Download(t.Context(), job.BotID, hash, job.Spec.SizeBytes, job.Offset, file, checkpoint)
	if err == nil {
		t.Fatal("canceled download was accepted")
	}
	if err := fail(&job, err, time.Now(), store.save); err != nil {
		t.Fatal(err)
	}
	job = store.load()
	if job.Offset != 4 || job.Phase == "blocked" || job.NextAttempt.IsZero() || job.ErrorCode != "connection" {
		t.Fatal("cancellation lost resumable state")
	}
	if err := client.Download(t.Context(), job.BotID, hash, job.Spec.SizeBytes, job.Offset, file, checkpoint); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(file)
	if err != nil || string(got) != string(data) || store.load().Offset != int64(len(data)) || requests.Load() != 2 {
		t.Fatal("resumed bytes not verified")
	}
}

func TestHTTP499RetryIsScheduledAndAuthorizationStaysBlocked(t *testing.T) {
	for _, status := range []int{499, http.StatusUnauthorized, http.StatusForbidden} {
		f, client := newFixture(t)
		source := frozen("abcd")
		initial := newJob(source)
		store := storeJob(t, initial)
		advance(t, client, store, source) // Admit exactly once before failures.
		f.setStatus(status)
		var final Job
		for range 14 {
			final = advance(t, client, store, source)
		}
		want, calls := "authorization", 1
		if status == 499 {
			want, calls = "connection", 14
		}
		phase := "blocked"
		if status == 499 {
			phase = "uploading"
		}
		if final.Phase != phase || final.ErrorCode != want || final.DeliveryID != initial.DeliveryID || final.OperationID != initial.OperationID || final.ReceiptID != "receipt-1" {
			t.Fatalf("status %d classification: %+v", status, final)
		}
		if f.count("GET /bots/bot-1/media/uploads/receipt-1") != calls || f.count("POST /bots/bot-1/media/uploads") != 1 {
			t.Fatal("failure changed the request schedule or re-admitted work", status)
		}
	}
	for _, err := range []error{errors.Join(ErrAuthorization, context.DeadlineExceeded), errors.Join(&HTTPError{Status: 403}, context.Canceled)} {
		if transientAssociation(err) {
			t.Fatal("cancellation masked authorization refusal")
		}
	}
}
