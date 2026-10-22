// transferqueue is a finite command in the existing Herdr plugin host.
// Credentials and source bytes are never CLI arguments or output.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/felinics/memoh/internal/transferclient"
)

func main() { os.Exit(run()) }

func run() int {
	return runCommand(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv, nil, time.Now)
}

// Tests inject only an isolated trusted TLS client and the invocation clock.
// Production has no flag or environment override for TLS trust or credentials.
func runCommand(args []string, input io.Reader, output, stderr io.Writer, env func(string) string, transport *http.Client, now func() time.Time) int {
	flags := flag.NewFlagSet("transferqueue", flag.ContinueOnError)
	flags.SetOutput(stderr)
	kind := flags.String("action", "probe", "lifecycle-summary, probe, enqueue-result, collect-result, advance-results, enqueue-upload, enqueue-download, advance, advance-next, reconcile, inspect, cancel, retire")
	id := flags.String("delivery", "", "logical delivery ID")
	if flags.Parse(args) != nil {
		return 2
	}
	if flags.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "unexpected arguments")
		return 2
	}
	switch *kind {
	case "lifecycle-summary", "enqueue-result", "collect-result", "advance-results", "probe", "enqueue-upload", "enqueue-download", "advance", "advance-next", "reconcile", "inspect", "cancel", "retire":
	default:
		_, _ = fmt.Fprintln(stderr, "unsupported action")
		return 2
	}
	// Resolve the official host locations once. No cwd or legacy fallback.
	locations := map[string]string{"HERDR_PLUGIN_CONFIG_DIR": env("HERDR_PLUGIN_CONFIG_DIR"), "HERDR_PLUGIN_STATE_DIR": env("HERDR_PLUGIN_STATE_DIR")}
	getenv := func(key string) string { return locations[key] }
	limits := transferclient.QueueLimits{Jobs: 8, Bytes: 256 << 20, JournalBytes: 8 << 20, Workers: 1}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	// Reserve a bounded tail of the same invocation for advisory persistence.
	workCtx, stopWork := context.WithTimeout(ctx, 115*time.Second)
	defer stopWork()
	publish := func(status transferclient.QueueStatus) string {
		if status.Job.Task.Attempt == "" {
			return ""
		}
		if transferclient.PublishProducerFeedback(ctx, getenv, "native-profile-v1", limits, status.Job, now()) != nil {
			return "unavailable"
		}
		return "recorded"
	}
	if *kind == "lifecycle-summary" {
		summary, err := transferclient.LifecycleSummary(ctx, getenv, "native-profile-v1", limits, now())
		if json.NewEncoder(output).Encode(summary) != nil || err != nil {
			return 1
		}
		return 0
	}
	if *kind == "probe" {
		q, err := transferclient.OpenHerdrQueue(getenv, "native-profile-v1", limits)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "storage unavailable")
			return 1
		}
		if q.Close() != nil {
			_, _ = fmt.Fprintln(stderr, "storage close failed")
			return 1
		}
		return writeFixed(output, `{"storage":"available","device_configuration":"required","profile":"native-profile-v1"}`)
	}

	if *kind == "enqueue-result" || *kind == "collect-result" || *kind == "advance-results" {
		var status transferclient.QueueStatus
		var err error
		switch *kind {
		case "enqueue-result":
			input, readErr := readBoundedProducer(ctx, input)
			if readErr != nil {
				_, _ = fmt.Fprintln(stderr, "invalid delivery metadata")
				return 2
			}
			status, err = transferclient.EnqueueProducer(workCtx, getenv, "native-profile-v1", limits, bytes.NewReader(input), transport, now())
		case "collect-result":
			status, err = transferclient.CollectProducer(workCtx, getenv, "native-profile-v1", limits, transport, now())
		case "advance-results":
			status, err = transferclient.AdvanceProducer(workCtx, getenv, "native-profile-v1", limits, transport, now())
		}
		feedback := publish(status)
		if *kind == "advance-results" {
			summary, summaryErr := transferclient.LifecycleSummary(ctx, getenv, "native-profile-v1", limits, now())
			if err != nil && !errors.Is(err, transferclient.ErrNoWork) {
				failure := transferclient.LifecycleFailure(err)
				// A process/storage failure is not permission to spin on a due
				// record that did not persist the failure. Keep only a real backoff.
				if summary.State != "waiting" || failure.ErrorClass != "transient" && failure.ErrorClass != "busy" {
					summary = failure
				}
			}
			exit := writeStatus(output, status, "", feedback, &summary)
			if err != nil && !errors.Is(err, transferclient.ErrNoWork) || summaryErr != nil {
				return 1
			}
			return exit
		}
		if errors.Is(err, transferclient.ErrProducerSource) {
			collection := "unavailable"
			if errors.Is(err, transferclient.ErrProducerManifest) {
				collection = "invalid_manifest"
			}
			_ = writeStatus(output, status, collection, feedback, nil)
			return 1
		}
		if errors.Is(err, transferclient.ErrNoWork) {
			return writeFixed(output, `{"state":"idle"}`)
		}
		if err != nil {
			_ = json.NewEncoder(output).Encode(map[string]string{"state": "pending_or_unavailable", "feedback_status": feedback, "error_class": transferclient.LifecycleFailure(err).ErrorClass})
			return 1
		}
		return writeStatus(output, status, "", feedback, nil)
	}
	action := transferclient.ConfiguredAction{Kind: *kind, DeliveryID: *id}
	if *kind == "enqueue-upload" || *kind == "enqueue-download" {
		// Metadata arrives on stdin from the trusted local producer; do not echo it.
		input, err := readAdmission(ctx, input)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "invalid delivery metadata")
			return 2
		}
		action.Job, action.SourceRef = input.Job, input.SourceRef
		if action.DeliveryID == "" {
			action.DeliveryID = input.Job.DeliveryID
		}
	}
	status, err := transferclient.InvokeConfiguredHerdr(workCtx, getenv, "native-profile-v1", limits, action, transport, now())
	feedback := publish(status)
	if errors.Is(err, transferclient.ErrNoWork) {
		return writeFixed(output, `{"state":"idle"}`)
	}
	if err != nil {
		// Never emit paths, endpoints, request bodies or raw transport diagnostics.
		code := "unavailable"
		switch {
		case errors.Is(err, transferclient.ErrVersion):
			code = "configuration_changed"
		case errors.Is(err, transferclient.ErrAdmissionPending), errors.Is(err, context.DeadlineExceeded):
			code = "pending"
		case errors.Is(err, transferclient.ErrBusy):
			code = "busy"
		case errors.Is(err, transferclient.ErrCapacity):
			code = "capacity"
		}
		_ = json.NewEncoder(output).Encode(map[string]string{"state": code, "feedback_status": feedback})
		return 1
	}
	// Bounded status/reference only. No source locator or execution task metadata.
	return writeStatus(output, status, "", feedback, nil)
}

type admissionInput struct {
	Job       transferclient.Job `json:"job"`
	SourceRef string             `json:"source_ref,omitempty"`
}

func readAdmission(ctx context.Context, input io.Reader) (admissionInput, error) {
	type result struct {
		value admissionInput
		err   error
	}
	done := make(chan result, 1)
	go func() {
		var r result
		decoder := json.NewDecoder(io.LimitReader(input, 16385))
		decoder.DisallowUnknownFields()
		r.err = decoder.Decode(&r.value)
		if r.err == nil && decoder.Decode(new(any)) != io.EOF {
			r.err = transferclient.ErrInvalid
		}
		done <- r
	}()
	select {
	case <-ctx.Done():
		return admissionInput{}, ctx.Err()
	case r := <-done:
		return r.value, r.err
	}
}

func writeStatus(output io.Writer, s transferclient.QueueStatus, source, feedback string, summary *transferclient.Lifecycle) int {
	out := struct {
		*transferclient.Lifecycle
		Feedback    string                    `json:"feedback_status,omitempty"`
		Source      string                    `json:"source_collection,omitempty"`
		Payload     string                    `json:"payload_state,omitempty"`
		Delivery    string                    `json:"delivery_id"`
		Phase       string                    `json:"phase"`
		Offset      int64                     `json:"offset"`
		Admitted    bool                      `json:"admitted"`
		Associated  bool                      `json:"associated"`
		Association string                    `json:"association_state,omitempty"`
		Error       string                    `json:"error_code,omitempty"`
		Reference   *transferclient.Reference `json:"reference,omitempty"`
	}{summary, feedback, source, s.PayloadState, s.Job.DeliveryID, s.Job.Phase, s.Job.Offset, s.Admitted, s.Associated, s.AssociationState, s.Job.ErrorCode, s.Job.Ready}
	if json.NewEncoder(output).Encode(out) != nil {
		return 1
	}
	return 0
}

func readBoundedProducer(ctx context.Context, input io.Reader) ([]byte, error) {
	type result struct {
		raw []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		raw, err := io.ReadAll(io.LimitReader(input, 16385))
		if len(raw) > 16384 {
			err = transferclient.ErrInvalid
		}
		done <- result{raw, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-done:
		return r.raw, r.err
	}
}

func writeFixed(output io.Writer, text string) int {
	if _, err := fmt.Fprintln(output, text); err != nil {
		return 1
	}
	return 0
}
