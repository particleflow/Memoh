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
	"os"
	"time"

	"github.com/felinics/memoh/internal/transferclient"
)

func main() { os.Exit(run()) }

func run() int {
	kind := flag.String("action", "probe", "probe, enqueue-result, collect-result, advance-results, enqueue-upload, enqueue-download, advance, advance-next, reconcile, inspect, cancel, retire")
	id := flag.String("delivery", "", "logical delivery ID")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected arguments")
		return 2
	}
	switch *kind {
	case "enqueue-result", "collect-result", "advance-results", "probe", "enqueue-upload", "enqueue-download", "advance", "advance-next", "reconcile", "inspect", "cancel", "retire":
	default:
		fmt.Fprintln(os.Stderr, "unsupported action")
		return 2
	}
	// Resolve the official host locations once. No cwd or legacy fallback.
	locations := map[string]string{"HERDR_PLUGIN_CONFIG_DIR": os.Getenv("HERDR_PLUGIN_CONFIG_DIR"), "HERDR_PLUGIN_STATE_DIR": os.Getenv("HERDR_PLUGIN_STATE_DIR")}
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
		if transferclient.PublishProducerFeedback(ctx, getenv, "native-profile-v1", limits, status.Job, time.Now()) != nil {
			return "unavailable"
		}
		return "recorded"
	}
	if *kind == "probe" {
		q, err := transferclient.OpenHerdrQueue(getenv, "native-profile-v1", limits)
		if err != nil {
			fmt.Fprintln(os.Stderr, "storage unavailable")
			return 1
		}
		if q.Close() != nil {
			fmt.Fprintln(os.Stderr, "storage close failed")
			return 1
		}
		fmt.Println(`{"storage":"available","device_configuration":"required","profile":"native-profile-v1"}`)
		return 0
	}

	if *kind == "enqueue-result" || *kind == "collect-result" || *kind == "advance-results" {
		var status transferclient.QueueStatus
		var err error
		switch *kind {
		case "enqueue-result":
			input, readErr := readBoundedProducer(ctx, os.Stdin)
			if readErr != nil {
				fmt.Fprintln(os.Stderr, "invalid delivery metadata")
				return 2
			}
			status, err = transferclient.EnqueueProducer(workCtx, getenv, "native-profile-v1", limits, bytes.NewReader(input), nil, time.Now())
		case "collect-result":
			status, err = transferclient.CollectProducer(workCtx, getenv, "native-profile-v1", limits, nil, time.Now())
		case "advance-results":
			status, err = transferclient.AdvanceProducer(workCtx, getenv, "native-profile-v1", limits, nil, time.Now())
		}
		feedback := publish(status)
		if errors.Is(err, transferclient.ErrProducerSource) {
			collection := "unavailable"
			if errors.Is(err, transferclient.ErrProducerManifest) {
				collection = "invalid_manifest"
			}
			_ = writeStatusWithFeedback(status, collection, feedback)
			return 1
		}
		if errors.Is(err, transferclient.ErrNoWork) {
			fmt.Println(`{"state":"idle"}`)
			return 0
		}
		if err != nil {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"state": "pending_or_unavailable", "feedback_status": feedback})
			return 1
		}
		return writeStatusWithFeedback(status, "", feedback)
	}
	action := transferclient.ConfiguredAction{Kind: *kind, DeliveryID: *id}
	if *kind == "enqueue-upload" || *kind == "enqueue-download" {
		// Metadata arrives on stdin from the trusted local producer; do not echo it.
		input, err := readAdmission(ctx, os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, "invalid delivery metadata")
			return 2
		}
		action.Job, action.SourceRef = input.Job, input.SourceRef
		if action.DeliveryID == "" {
			action.DeliveryID = input.Job.DeliveryID
		}
	}
	status, err := transferclient.InvokeConfiguredHerdr(workCtx, getenv, "native-profile-v1", limits, action, nil, time.Now())
	feedback := publish(status)
	if errors.Is(err, transferclient.ErrNoWork) {
		fmt.Println(`{"state":"idle"}`)
		return 0
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
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"state": code, "feedback_status": feedback})
		return 1
	}
	// Bounded status/reference only. No source locator or execution task metadata.
	return writeStatusWithFeedback(status, "", feedback)
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

func writeStatusWithFeedback(s transferclient.QueueStatus, source, feedback string) int {
	out := struct {
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
	}{feedback, source, s.PayloadState, s.Job.DeliveryID, s.Job.Phase, s.Job.Offset, s.Admitted, s.Associated, s.AssociationState, s.Job.ErrorCode, s.Job.Ready}
	if json.NewEncoder(os.Stdout).Encode(out) != nil {
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
