//go:build linux || darwin

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
	"reflect"
	"strconv"
	"testing"
	"time"
)

func TestRetryContractServerDeadline(t *testing.T) {
	for _, header := range []string{"3600", time.Now().UTC().Add(time.Hour).Format(http.TimeFormat), "Wed, 01 Jan 3000 00:00:00 GMT"} {
		t.Run(header, func(t *testing.T) {
			now := time.Now().UTC()
			resp := &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {header}}}
			j := newJob([]byte("original"))
			err := statusError(resp)
			if e := fail(&j, err, now, func(Job) error { return nil }); e != nil {
				t.Fatal(e)
			}
			want := now.Add(time.Hour - time.Second)
			if header == "Wed, 01 Jan 3000 00:00:00 GMT" {
				want = time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC)
			}
			if j.NextAttempt.Before(want) {
				t.Fatalf("server deadline shortened: header=%s due=%s want>=%s", header, j.NextAttempt, want)
			}
		})
	}
}

func retryChildInspect(t *testing.T, path string) QueueStatus {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, "-test.run=^TestRetryContractInspectChild$") //nolint:gosec // G204: OS-resolved current test executable, fixed child selector, no shell or caller-controlled program/arguments.
	cmd.Env = append(os.Environ(), "MEMOH_RETRY_INSPECT_QUEUE="+path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("inspect child: %v %s", err, out)
	}
	var r QueueStatus
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("child output: %v %s", err, out)
	}
	return r
}

func TestRetryContractInspectChild(t *testing.T) {
	path := os.Getenv("MEMOH_RETRY_INSPECT_QUEUE")
	if path == "" {
		t.Skip("subprocess only")
	}
	q := openQueue(t, path, queueLimits())
	r := inspectQueue(t, q, "delivery-1")
	if err := json.NewEncoder(os.Stdout).Encode(r); err != nil {
		t.Fatal(err)
	}
	_ = q.Close()
	os.Exit(0)
}

func TestRetryContractTransferSurvivesFailuresAndRestart(t *testing.T) {
	path := t.TempDir()
	q := openQueue(t, path, queueLimits())
	original, _ := admitSource(t, q, []byte("actual bytes under original hash"))
	f, c := newFixture(t)
	r := runQueue(t, q, c, nil)
	receipt := r.Job.ReceiptID
	f.setStatus(503)
	for range 15 {
		r = runQueue(t, q, c, nil)
	}
	if r.Job.Phase != "uploading" || r.Job.Attempts != 15 || !r.Job.NextAttempt.After(time.Now()) {
		t.Fatalf("recoverable transfer terminated: %+v", r.Job)
	}
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	restored := retryChildInspect(t, path)
	if !reflect.DeepEqual(r, restored) {
		t.Fatal("child lost persisted retry/identity")
	}
	q = openQueue(t, path, queueLimits())
	if err := q.RunOne(t.Context(), "delivery-1", c, nil, r.Job.NextAttempt.Add(-time.Nanosecond)); err != nil {
		t.Fatal(err)
	}
	if f.count("GET /bots/bot-1/media/uploads/"+receipt) != 15 {
		t.Fatal("early retry")
	}
	f.setStatus(0)
	for n := 0; n < 20 && r.Job.Phase != "ready"; n++ {
		r = runQueue(t, q, c, nil)
	}
	_, data := f.current()
	if r.Job.Phase != "ready" || r.Job.ReceiptID != receipt || r.Job.DeliveryID != original.DeliveryID || r.Job.Spec != original.Spec || digest(data) != original.Spec.SHA256 || f.count("POST /bots/bot-1/media/uploads") != 1 {
		t.Fatal("recovery changed receipt, identity, hash or Create count")
	}
	t.Logf("failures=15 child_inspection=true create=1 bytes=%d hash=%s", len(data), digest(data))
}

func TestRetryContractAssociationUnknownSurvivesFailuresAndRestart(t *testing.T) {
	path := t.TempDir()
	q, c := readyAssociationQueue(t, path)
	original := inspectQueue(t, q, "delivery-1")
	commits, resolves := 0, 0
	binding := &AssociationBinding{Commit: func(context.Context, string, Reference, io.Reader) error {
		commits++
		return statusError(&http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {"3600"}}})
	}, Resolve: func(context.Context, string, Reference, bool) (AssociationOutcome, error) {
		resolves++
		return "", &HTTPError{Status: 503, RetryAfter: time.Hour}
	}}
	r := runQueue(t, q, c, binding)
	if r.AssociationDue.Before(r.LastRun.Add(time.Hour)) {
		t.Fatal("commit Retry-After shortened")
	}
	for range 15 {
		r = runQueue(t, q, c, binding)
	}
	if r.AssociationBlocked || r.AssociationState != AssociationUnknown || r.AssociationAttempts != 16 || commits != 1 || resolves != 15 {
		t.Fatalf("unknown stopped or recommitted: %+v commits=%d resolves=%d", r, commits, resolves)
	}
	if !r.AssociationDue.After(r.LastRun.Add(time.Hour - time.Second)) {
		t.Fatal("association Retry-After shortened")
	}
	_ = q.Close()
	restored := retryChildInspect(t, path)
	if !reflect.DeepEqual(r, restored) {
		t.Fatal("association journal changed on restart")
	}
	q = openQueue(t, path, queueLimits())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := q.RunOne(ctx, "delivery-1", c, binding, r.AssociationDue); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled action continued", err)
	}
	if after := inspectQueue(t, q, "delivery-1"); !reflect.DeepEqual(r, after) {
		t.Fatal("canceled action altered journal")
	}
	if err := q.RequestCancel("delivery-1"); err != nil {
		t.Fatal(err)
	}
	if err := q.ResumeAssociation("delivery-1"); err != nil {
		t.Fatal(err)
	}
	canceled := inspectQueue(t, q, "delivery-1")
	if !canceled.AssociationDue.Equal(r.AssociationDue) {
		t.Fatal("cancel/resume shortened server deadline")
	}
	if err := q.RunOne(t.Context(), "delivery-1", c, binding, r.AssociationDue.Add(-time.Nanosecond)); err != nil {
		t.Fatal(err)
	}
	if resolves != 15 {
		t.Fatal("early resolve after cancel/resume")
	}
	binding.Resolve = func(_ context.Context, _ string, ref Reference, cancel bool) (AssociationOutcome, error) {
		resolves++
		if !cancel || ref != *original.Job.Ready {
			t.Fatal("immutable reference/cancel lost")
		}
		return OutcomeCommitted, nil
	}
	r = runQueue(t, q, c, binding)
	if !r.Associated || r.AssociationState != AssociationCommitted || commits != 1 || resolves != 16 || r.Job.Spec != original.Job.Spec {
		t.Fatal("existing result not reconciled")
	}
	t.Log("commit=1 resolve=16 failures=16 child_inspection=true cancel=true same_reference=true")
}

func TestRetryContractAssociationCommitAndAuthorization(t *testing.T) {
	q, c := readyAssociationQueue(t, t.TempDir())
	commits, resolves := 0, 0
	binding := &AssociationBinding{Commit: func(context.Context, string, Reference, io.Reader) error { commits++; return ErrTransport }, Resolve: func(context.Context, string, Reference, bool) (AssociationOutcome, error) {
		resolves++
		return OutcomeAbsent, nil
	}}
	var r QueueStatus
	for range 30 {
		r = runQueue(t, q, c, binding)
	}
	if r.AssociationBlocked || commits != 15 || resolves != 15 || r.AssociationState != AssociationRetryCommit {
		t.Fatalf("retry_commit was count-terminated: %+v %d/%d", r, commits, resolves)
	}
	denied := 0
	binding.Commit = func(context.Context, string, Reference, io.Reader) error {
		denied++
		return &HTTPError{Status: 403, RetryAfter: time.Hour}
	}
	r = runQueue(t, q, c, binding)
	if !r.AssociationBlocked || r.AssociationState != AssociationUnknown || r.Associated {
		t.Fatal("authorization refusal lost unknown or retried")
	}
	for range 2 {
		runQueue(t, q, c, binding)
	}
	if denied != 1 || commits != 15 || resolves != 15 {
		t.Fatal("refusal retried")
	}
}

func TestRetryContractLegacyBlockedUnknownAndCancel(t *testing.T) {
	src := frozen("unchanged")
	f, c := newFixture(t)
	s := storeJob(t, newJob(src))
	advance(t, c, s, src)
	for _, phase := range []string{"blocked", "create_unknown"} {
		j := s.load()
		j.Phase = phase
		j.ErrorCode = "retry_budget"
		j.Attempts = 11
		if phase == "create_unknown" {
			j.ReceiptID = ""
		}
		calls := f.count("POST /bots/bot-1/media/uploads")
		if err := c.Advance(t.Context(), j, func(Job) error { t.Fatal("legacy record auto resumed"); return nil }, src, time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		if f.count("POST /bots/bot-1/media/uploads") != calls {
			t.Fatal("unknown create replayed")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	j := s.load()
	if err := c.Advance(ctx, j, func(Job) error { t.Fatal("canceled advance persisted"); return nil }, src, time.Now()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestRetryContractHTTPDeadlinePersists(t *testing.T) {
	for _, header := range []string{"3600", "Wed, 01 Jan 3000 00:00:00 GMT"} {
		t.Run(header, func(t *testing.T) {
			requests := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != http.MethodGet || r.URL.Path != "/bots/bot-1/media/limits" || r.Header.Get("Authorization") != syntheticAuth {
					t.Error("wrong original scope")
				}
				w.Header().Set("Retry-After", header)
				w.WriteHeader(http.StatusTooManyRequests)
			}))
			defer server.Close()
			c, err := New(server.URL, server.Client(), func(context.Context) (string, error) { return syntheticAuth, nil })
			if err != nil {
				t.Fatal(err)
			}
			path := t.TempDir()
			q := openQueue(t, path, queueLimits())
			admitSource(t, q, []byte("unchanged original"))
			now := time.Now().UTC()
			r := runQueue(t, q, c, nil)
			want := now.Add(time.Hour)
			if header != "3600" {
				want = time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC)
			}
			if r.Job.NextAttempt.Before(want) || r.Job.Phase != "queued" || r.Job.ReceiptID != "" {
				t.Fatal("HTTP deadline lost", r.Job)
			}
			_ = q.Close()
			child := retryChildInspect(t, path)
			if !reflect.DeepEqual(r, child) {
				t.Fatal("exact deadline changed in child")
			}
			q = openQueue(t, path, queueLimits())
			if err := q.RunOne(t.Context(), "delivery-1", c, nil, r.Job.NextAttempt.Add(-time.Nanosecond)); err != nil {
				t.Fatal(err)
			}
			if requests != 1 {
				t.Fatal("GET before server due")
			}
			t.Logf("TLS limits=1 no_create=true restored_exact_due=%s", r.Job.NextAttempt)
		})
	}
}

func TestRetryContractCounterAndHeaderRepresentation(t *testing.T) {
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	for _, raw := range []string{"18446744073709551616", "9999999999999999999999999999999999999", "9223372036854775807"} {
		resp := &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Retry-After": {raw}}}
		err := statusErrorAt(resp, now)
		if !errors.Is(err, ErrProtocol) {
			t.Fatal("unrepresentable deadline accepted", raw, err)
		}
		j := newJob([]byte("original"))
		if err := fail(&j, err, now, func(Job) error { return nil }); err != nil {
			t.Fatal(err)
		}
		if j.Phase != "blocked" || j.ErrorCode != "protocol" || !j.NextAttempt.IsZero() {
			t.Fatal("invalid header permits earlier retry")
		}
		resp.StatusCode = http.StatusForbidden
		if transientAssociation(statusErrorAt(resp, now)) {
			t.Fatal("invalid deadline hid permission refusal")
		}
		if err := fail(&j, statusErrorAt(resp, now), now, func(Job) error { return nil }); err != nil {
			t.Fatal(err)
		}
		if j.ErrorCode != "authorization" {
			t.Fatal("permission precedence lost")
		}
	}
	for _, raw := range []string{"", "invalid", "-1", "0", "Fri, 30 Oct 2026 00:00:00 GMT"} {
		var h *HTTPError
		err := statusErrorAt(&http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Retry-After": {raw}}}, now)
		if !errors.As(err, &h) || h.RetryAfter != 0 {
			t.Fatal("absent/past/invalid header cannot add a deadline", raw)
		}
	}
	last := time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
	seconds := last.Unix() - now.Unix()
	var latest *HTTPError
	err := statusErrorAt(&http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {strconv.FormatInt(seconds, 10)}}}, now)
	if !errors.As(err, &latest) || !latest.RetryAt.Equal(last) {
		t.Fatal("representable deadline was shortened or refused", err)
	}
	err = statusErrorAt(&http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {strconv.FormatInt(seconds+1, 10)}}}, now)
	if !errors.Is(err, ErrProtocol) {
		t.Fatal("deadline beyond journal representation accepted", err)
	}
	n := int(^uint(0) >> 1)
	if incrementAttempts(n) != n || incrementAttempts(n-1) != n {
		t.Fatal("diagnostic count overflow")
	}
	due := retryDue(now, n, ErrTransport)
	if !due.After(now) || due.After(now.Add(maxRetryDelay)) {
		t.Fatal("unbounded or zero local backoff")
	}
	j := newJob([]byte("original"))
	j.Attempts = n
	if err := fail(&j, ErrTransport, now, func(Job) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if !j.valid() || j.Attempts != n || j.Phase != "queued" {
		t.Fatal("representation limit became a retry budget")
	}
}

func TestRetryContractDownloadCounterAndAssociationClock(t *testing.T) {
	data := []byte("NAS original bytes after transient failures")
	failures := true
	requests, commits := 0, 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") != syntheticAuth || r.URL.Path != "/bots/bot-1/media/"+digest(data) {
			t.Error("download binding changed")
		}
		if failures {
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("ETag", `"`+digest(data)+`"`)
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
	}))
	defer server.Close()
	c, err := New(server.URL, server.Client(), func(context.Context) (string, error) { return syntheticAuth, nil })
	if err != nil {
		t.Fatal(err)
	}
	path := t.TempDir()
	q := openQueue(t, path, queueLimits())
	j := newJob(data)
	j.SnapshotID = j.DeliveryID
	if err := q.Admit(t.Context(), "download", j, nil); err != nil {
		t.Fatal(err)
	}
	var r QueueStatus
	for range 15 {
		r = runQueue(t, q, c, nil)
	}
	if r.Job.Phase != "queued" || r.Job.Attempts != 15 || r.AssociationAttempts != 0 || !r.AssociationDue.IsZero() || requests != 15 {
		t.Fatal("transfer count altered association clock", r)
	}
	_ = q.Close()
	child := retryChildInspect(t, path)
	if !reflect.DeepEqual(r, child) {
		t.Fatal("download retry lost on reopen")
	}
	q = openQueue(t, path, queueLimits())
	failures = false
	r = runQueue(t, q, c, nil)
	if r.Job.Phase != "ready" || r.Job.Attempts != 0 || r.AssociationAttempts != 0 || r.Job.Spec != j.Spec {
		t.Fatal("download not ready under original hash")
	}
	binding := &AssociationBinding{Commit: func(_ context.Context, direction string, ref Reference, content io.Reader) error {
		commits++
		got, err := io.ReadAll(content)
		if err != nil || !bytes.Equal(got, data) || direction != "download" || ref.ContentHash != j.Spec.SHA256 || ref.DeliveryID != j.DeliveryID {
			t.Fatal("publication got wrong bytes/reference", err)
		}
		return nil
	}, Resolve: func(context.Context, string, Reference, bool) (AssociationOutcome, error) {
		t.Fatal("unexpected resolve")
		return "", ErrProtocol
	}}
	r = runQueue(t, q, c, binding)
	if !r.Associated || commits != 1 || r.AssociationAttempts != 1 || r.Job.Attempts != 0 {
		t.Fatal("separate clocks/counts lost", r)
	}
	t.Logf("download_failures=15 requests=%d commit=1 bytes=%d hash=%s", requests, len(data), digest(data))
}

func TestRetryContractLegacyAssociationPauseIsExplicit(t *testing.T) {
	q, c := readyAssociationQueue(t, t.TempDir())
	due := time.Now().Add(time.Hour).UTC()
	lifecycleEdit(t, q, "delivery-1", func(r *QueueStatus) {
		r.AssociationState = AssociationUnknown
		r.AssociationAttempts = 12
		r.AssociationBlocked = true
		r.AssociationDue = due
	})
	before := inspectQueue(t, q, "delivery-1")
	commits, resolves := 0, 0
	binding := &AssociationBinding{Commit: func(context.Context, string, Reference, io.Reader) error { commits++; return nil }, Resolve: func(context.Context, string, Reference, bool) (AssociationOutcome, error) {
		resolves++
		return OutcomeCommitted, nil
	}}
	if err := q.RunOne(t.Context(), "delivery-1", c, binding, due.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if commits != 0 || resolves != 0 {
		t.Fatal("legacy pause automatically resumed")
	}
	if err := q.ResumeAssociation("delivery-1"); err != nil {
		t.Fatal(err)
	}
	after := inspectQueue(t, q, "delivery-1")
	if after.AssociationState != AssociationUnknown || !after.AssociationDue.Equal(due) || after.Job.ReceiptID != before.Job.ReceiptID || after.Job.Spec != before.Job.Spec {
		t.Fatal("explicit resume erased outcome/identity/server due")
	}
	if err := q.RunOne(t.Context(), "delivery-1", c, binding, due.Add(-time.Nanosecond)); err != nil {
		t.Fatal(err)
	}
	if resolves != 0 {
		t.Fatal("explicit resume bypassed server due")
	}
	if err := q.RunOne(t.Context(), "delivery-1", c, binding, due); err != nil {
		t.Fatal(err)
	}
	if commits != 0 || resolves != 1 || !inspectQueue(t, q, "delivery-1").Associated {
		t.Fatal("explicit resume did not resolve original receipt")
	}
}
