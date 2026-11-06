//go:build windows

package transferclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

var publicationCrashBytes = []byte("native publication process-crash fixture")

func publicationNetworkFixture(t *testing.T, data []byte) *Client {
	t.Helper()
	j := newJob(data)
	https := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer isolated-native-publication" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/bots/") {
			w.Header().Set("ETag", `"`+j.Spec.SHA256+`"`)
			http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
			return
		}
		var request struct {
			Publication string `json:"publication"`
			Cancel      bool   `json:"cancel"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		outcome := OutcomeAbsent
		if strings.HasSuffix(r.URL.Path, "/commit") {
			if request.Publication != "published" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			outcome = OutcomeCommitted
		}
		if request.Cancel {
			if request.Publication != "fenced" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			outcome = OutcomeCanceled
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"version": 2, "revision": "1", "outcome": outcome})
	}))
	t.Cleanup(https.Close)
	client, err := New(https.URL, https.Client(), func(context.Context) (string, error) { return "Bearer isolated-native-publication", nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.CloseIdleConnections)
	return client
}

func TestWindowsNativePublicationProcessCrash(t *testing.T) {
	stages := map[string]int{"after_prepare": 71, "after_rename": 72, "after_commit": 73}
	if root := os.Getenv("MEMOH_PUBLICATION_FIXTURE_ROOT"); root != "" {
		stage := os.Getenv("MEMOH_PUBLICATION_FIXTURE_STAGE")
		code, ok := stages[stage]
		if !ok {
			t.Fatal("invalid fixture stage")
		}
		q := nativeQueue(t, root)
		j := newJob(publicationCrashBytes)
		j.SnapshotID = j.DeliveryID
		if err := q.Admit(t.Context(), "download", j, nil); err != nil {
			t.Fatal(err)
		}
		client := publicationNetworkFixture(t, publicationCrashBytes)
		if err := q.RunOne(t.Context(), j.DeliveryID, client, nil, time.Now()); err != nil {
			t.Fatal(err)
		}
		r, err := q.Inspect(j.DeliveryID)
		if err != nil || r.Job.Ready == nil {
			t.Fatal("verified staging", err)
		}
		p := nativePublication(t, q, *r.Job.Ready)
		p.hook = func(at string) {
			if at == stage {
				os.Exit(code)
			}
		}
		if err := q.RunOne(t.Context(), j.DeliveryID, client, client.DeliveryBinding(p), time.Now()); err != nil {
			t.Fatal(err)
		}
		t.Fatal("crash stage not reached")
	}
	for stage, code := range stages {
		t.Run(stage, func(t *testing.T) {
			path := nativeFixtureRoot(t, false)
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestWindowsNativePublicationProcessCrash$") //nolint:gosec // Current test binary, fixed test name, private fixture directory.
			cmd.Env = append(os.Environ(), "MEMOH_PUBLICATION_FIXTURE_ROOT="+path, "MEMOH_PUBLICATION_FIXTURE_STAGE="+stage)
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != code {
				t.Fatalf("crash child: %v %s", err, output)
			}
			q := nativeQueue(t, path)
			j := newJob(publicationCrashBytes)
			var status QueueStatus
			for attempt := 0; attempt < 20; attempt++ {
				status, err = q.Inspect(j.DeliveryID)
				if !errors.Is(err, ErrBusy) {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			if err != nil || status.AssociationState != AssociationInFlight || status.Job.Ready == nil {
				t.Fatalf("durable in-flight: %+v %v", status, err)
			}
			p := nativePublication(t, q, *status.Job.Ready)
			if stage == "after_prepare" {
				out, err := p.Resolve(t.Context(), *status.Job.Ready, false)
				if err != nil || out != OutcomeAbsent {
					t.Fatal("before rename", out, err)
				}
			}
			if err := q.RequestCancel(j.DeliveryID); err != nil {
				t.Fatal(err)
			}
			client := publicationNetworkFixture(t, publicationCrashBytes)
			if err := q.RunOne(t.Context(), j.DeliveryID, client, client.DeliveryBinding(p), time.Now()); err != nil {
				t.Fatal(err)
			}
			status, err = q.Inspect(j.DeliveryID)
			if err != nil {
				t.Fatal(err)
			}
			if stage == "after_prepare" {
				if status.Job.Phase != "canceled" || status.Associated {
					t.Fatalf("pre-rename cancellation: %+v", status)
				}
			} else {
				if !status.Associated || status.Job.Phase == "canceled" {
					t.Fatalf("published file lost to cancellation: %+v", status)
				}
				reader, err := p.OpenPublished(t.Context(), *status.Job.Ready)
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(reader)
				_ = reader.Close()
				if err != nil || !bytes.Equal(data, publicationCrashBytes) {
					t.Fatal("recovered content", err)
				}
			}
		})
	}
}

func TestWindowsNativePublicationPublishCancelRace(t *testing.T) {
	for range 12 {
		q := nativeQueue(t, nativeFixtureRoot(t, false))
		ref, source := publicationFixtureSource(t, q, []byte("publication race"))
		publisher := nativePublication(t, q, ref)
		canceler := nativePublication(t, q, ref)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for _, publish := range []bool{false, true} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				var out AssociationOutcome
				var err error
				if publish {
					out, err = publisher.Publish(t.Context(), ref, source)
				} else {
					out, err = canceler.Resolve(t.Context(), ref, true)
				}
				if err != nil && !errors.Is(err, ErrBusy) {
					t.Error(err)
				}
				if err == nil && out != OutcomeCommitted && out != OutcomeCanceled {
					t.Error("invalid race outcome", out)
				}
			}()
		}
		close(start)
		wg.Wait()
		source.active = false
		_ = source.file.Close()
		winner, err := publisher.Resolve(t.Context(), ref, true)
		if err != nil || winner != OutcomeCommitted && winner != OutcomeCanceled {
			t.Fatal("race reconciliation", winner, err)
		}
		again, err := canceler.Resolve(t.Context(), ref, false)
		if err != nil || again != winner {
			t.Fatal("split local outcome", winner, again, err)
		}
	}
}
