// transferqueue is a finite command in the existing Herdr plugin host.
// Credentials and source bytes are never CLI arguments or output.
package main

import (
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
	kind := flag.String("action", "probe", "probe, enqueue-upload, enqueue-download, advance, advance-next, reconcile, inspect, cancel, retire")
	id := flag.String("delivery", "", "logical delivery ID")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected arguments")
		return 2
	}
	switch *kind {
	case "probe", "enqueue-upload", "enqueue-download", "advance", "advance-next", "reconcile", "inspect", "cancel", "retire":
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
	status, err := transferclient.InvokeConfiguredHerdr(ctx, getenv, "native-profile-v1", limits, action, nil, time.Now())
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
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"state": code})
		return 1
	}
	// Bounded status/reference only. No source locator or execution task metadata.
	return writeStatus(status)
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

func writeStatus(s transferclient.QueueStatus) int {
	out := struct {
		Delivery    string                    `json:"delivery_id"`
		Phase       string                    `json:"phase"`
		Offset      int64                     `json:"offset"`
		Admitted    bool                      `json:"admitted"`
		Associated  bool                      `json:"associated"`
		Association string                    `json:"association_state,omitempty"`
		Error       string                    `json:"error_code,omitempty"`
		Reference   *transferclient.Reference `json:"reference,omitempty"`
	}{s.Job.DeliveryID, s.Job.Phase, s.Job.Offset, s.Admitted, s.Associated, s.AssociationState, s.Job.ErrorCode, s.Job.Ready}
	if json.NewEncoder(os.Stdout).Encode(out) != nil {
		return 1
	}
	return 0
}
