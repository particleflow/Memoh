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

	"github.com/felinics/memoh/internal/deliverymeta"
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
	kind := flags.String("action", "probe", "lifecycle-summary, migrate-budget, migrate-history, migrate-publications, probe, recover-result, enqueue-result, enqueue-results, collect-result, advance-results, advance-snapshots, advance-snapshot, enqueue-upload, enqueue-download, advance, advance-next, reconcile, inspect, cancel, retire")
	id := flags.String("delivery", "", "logical delivery ID")
	if flags.Parse(args) != nil {
		return 2
	}
	if flags.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "unexpected arguments")
		return 2
	}
	switch *kind {
	case "recover-result", "migrate-budget", "migrate-publications", "migrate-history", "lifecycle-summary", "enqueue-result", "enqueue-results", "collect-result", "advance-results", "advance-snapshots", "advance-snapshot", "probe", "enqueue-upload", "enqueue-download", "advance", "advance-next", "reconcile", "inspect", "cancel", "retire":
	default:
		_, _ = fmt.Fprintln(stderr, "unsupported action")
		return 2
	}
	// Resolve the official host locations once. No cwd or legacy fallback.
	locations := map[string]string{"HERDR_PLUGIN_CONFIG_DIR": env("HERDR_PLUGIN_CONFIG_DIR"), "HERDR_PLUGIN_STATE_DIR": env("HERDR_PLUGIN_STATE_DIR")}
	getenv := func(key string) string { return locations[key] }
	limits, err := transferclient.LoadHostQueueLimits(getenv)
	if err != nil {
		_ = json.NewEncoder(output).Encode(transferclient.LifecycleFailure(err))
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	// Reserve a bounded tail of the same invocation for advisory persistence.
	workCtx, stopWork := context.WithTimeout(ctx, 115*time.Second)
	defer stopWork()
	if *kind == "advance-snapshots" || *kind == "advance-snapshot" {
		if *kind == "advance-snapshots" && *id != "" || *kind == "advance-snapshot" && *id == "" {
			return 2
		}
		var status transferclient.QueueStatus
		var err error
		if *kind == "advance-snapshots" {
			status, err = transferclient.AdvanceSnapshotProducer(workCtx, getenv, "native-profile-v1", limits, transport, now())
		} else {
			status, err = transferclient.InvokeConfiguredHerdr(workCtx, getenv, "native-profile-v1", limits, transferclient.ConfiguredAction{Kind: "advance-snapshot", DeliveryID: *id}, transport, now())
		}
		// No producer feedback projection/source-root access in this capability.
		// Its scope cannot be mistaken for the general queue lifecycle summary.
		return writeSnapshotStatus(output, status, err)
	}
	if *kind == "enqueue-results" {
		if *id != "" {
			return 2
		}
		raw, err := readBoundedProducerLimit(ctx, input, deliverymeta.ProducerPageMaxBytes)
		if err != nil {
			return 2
		}
		page, err := transferclient.EnqueueProducerPage(workCtx, getenv, "native-profile-v1", limits, bytes.NewReader(raw), transport, now())
		if len(page.Files) == 0 && err != nil {
			_ = json.NewEncoder(output).Encode(transferclient.LifecycleFailure(err))
			return 1
		}
		if json.NewEncoder(output).Encode(page) != nil || err != nil {
			return 1
		}
		return 0
	}
	if *kind == "migrate-budget" {
		if err := transferclient.MigrateHerdrBudget(workCtx, getenv, "native-profile-v1", limits); err != nil {
			_ = json.NewEncoder(output).Encode(transferclient.LifecycleFailure(err))
			return 1
		}
		return writeFixed(output, `{"budget_migration":"complete"}`)
	}
	publish := func(status transferclient.QueueStatus) string {
		// Only a validated durable download is outside the outbound producer
		// projection. Missing/foreign upload context remains a feedback failure.
		// This says nothing about transfer/association success or retryability.
		if status.Direction == "download" {
			return "not_applicable"
		}
		if status.Job.Task.Attempt == "" {
			return ""
		}
		if transferclient.PublishProducerFeedback(ctx, getenv, "native-profile-v1", limits, status.Job, now()) != nil {
			return "unavailable"
		}
		return "recorded"
	}
	if *kind == "recover-result" {
		if *id != "" {
			return 2
		}
		raw, err := readBoundedProducer(ctx, input)
		if err != nil {
			return 2
		}
		var request deliverymeta.ResultRecovery
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		if d.Decode(&request) != nil || d.Decode(new(any)) != io.EOF {
			return 2
		}
		observation, err := transferclient.RecoverResultSource(workCtx, getenv, "native-profile-v1", limits, request, transport)
		if err != nil {
			_ = json.NewEncoder(output).Encode(map[string]string{"state": "recovery_blocked", "error_class": transferclient.LifecycleFailure(err).ErrorClass})
			return 1
		}
		if observation.ReceiptObserved != nil {
			// No client mutation or feedback write. The existing GET handler may
			// still persist the original ready proof; its response cannot tell us
			// whether that happened, so never promise a write-free server read.
			return writeFixed(output, `{"state":"receipt_observed","recovery_scope":"receipt_get_only","server_effects":"ready_proof_may_be_recorded"}`)
		}
		return writeStatus(output, observation.Queue, "", publish(observation.Queue), nil)
	}
	if *kind == "migrate-publications" {
		q, err := transferclient.OpenHerdrQueue(getenv, "native-profile-v1", limits)
		if err != nil {
			_ = json.NewEncoder(output).Encode(transferclient.LifecycleFailure(err))
			return 1
		}
		defer func() { _ = q.Close() }()
		done, err := q.MigrateNativePublication(workCtx)
		if err != nil {
			_ = json.NewEncoder(output).Encode(transferclient.LifecycleFailure(err))
			return 1
		}
		if done {
			return writeFixed(output, `{"publication_layout":2,"migration":"complete"}`)
		}
		return writeFixed(output, `{"publication_layout":0,"migration":"pending"}`)
	}
	if *kind == "migrate-history" {
		if err := transferclient.MigrateHerdrQueue(workCtx, getenv, "native-profile-v1", limits); err != nil {
			_ = json.NewEncoder(output).Encode(transferclient.LifecycleFailure(err))
			return 1
		}
		return writeFixed(output, `{"queue_layout":3,"history":"enabled"}`)
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
		if errors.Is(err, transferclient.ErrAdmissionPending) {
			// The immutable admission has a durable checkpoint. Continue the
			// same enqueue/collect input; this is not an unknown remote outcome
			// or a runnable upload for advance-results yet.
			_ = json.NewEncoder(output).Encode(map[string]string{"state": "pending", "feedback_status": feedback, "error_class": "transient"})
			return 1
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

func writeSnapshotStatus(output io.Writer, status transferclient.QueueStatus, err error) int {
	if errors.Is(err, transferclient.ErrNoWork) {
		return writeFixed(output, `{"state":"idle","advance_scope":"retained_upload_snapshots"}`)
	}
	var buffer bytes.Buffer
	code := writeStatus(&buffer, status, "", "", nil)
	var fields map[string]json.RawMessage
	if json.Unmarshal(buffer.Bytes(), &fields) != nil {
		return 1
	}
	fields["advance_scope"] = json.RawMessage(`"retained_upload_snapshots"`)
	if err != nil {
		fields["error_class"], _ = json.Marshal(transferclient.LifecycleFailure(err).ErrorClass)
		code = 1
	}
	if json.NewEncoder(output).Encode(fields) != nil {
		return 1
	}
	return code
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
		Registered  bool                      `json:"registered"`
		Associated  bool                      `json:"associated"`
		Association string                    `json:"association_state,omitempty"`
		Error       string                    `json:"error_code,omitempty"`
		Reference   *transferclient.Reference `json:"reference,omitempty"`
	}{summary, feedback, source, s.PayloadState, s.Job.DeliveryID, s.Job.Phase, s.Job.Offset, s.Admitted, s.Registered, s.Associated, s.AssociationState, s.Job.ErrorCode, s.Job.Ready}
	if json.NewEncoder(output).Encode(out) != nil {
		return 1
	}
	return 0
}

func readBoundedProducer(ctx context.Context, input io.Reader) ([]byte, error) {
	return readBoundedProducerLimit(ctx, input, 16384)
}

func readBoundedProducerLimit(ctx context.Context, input io.Reader, limit int) ([]byte, error) {
	type result struct {
		raw []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		raw, err := io.ReadAll(io.LimitReader(input, int64(limit)+1))
		if len(raw) > limit {
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
