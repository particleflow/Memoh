package computertask

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/deliverymeta"
)

func TestResultRecoveryOriginalOwnerPreparationAndControlEntry(t *testing.T) {
	f, service, ctx := newNativeTaskFixture(t)
	service.assets = nil
	first, err := service.submit(ctx, f.binding, Submit{Kind: "herdr_pi_task", Task: "synthetic original no-suffix declaration", Artifact: &deliverymeta.Artifact{SourceRef: "legacy_result", Mime: "image/png"}})
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		got, err := service.observe(ctx, f.binding, first.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		if got.PiResult != nil {
			break
		}
	}
	before, _ := json.Marshal(f.store.records[first.ID])
	prepare := func() (deliverymeta.ResultRecovery, error) {
		return service.PrepareProducerResultRecovery(ctx, f.binding, first.ID, "legacy_result", "explicit.png", strings.Repeat("a", 64), 131109, strings.Repeat("b", 64))
	}
	request, err := prepare()
	if err != nil {
		t.Fatal("authorized completed operation", err)
	}
	p, ok := request.FilePlan()
	if !ok || p.Artifact.SourceRef != "legacy_result" || request.PhysicalName != "explicit.png" || request.RemoteOutcome != "absent" {
		t.Fatal("original declaration changed")
	}
	commands := 0
	service.relay.SetNativeCaller(func(callCtx context.Context, scope agenvo.Scope, target agenvo.Target, method string, args map[string]any) (agenvo.Receipt, error) {
		if method != "pane.run" {
			return f.invoke(callCtx, scope, target, method, args)
		}
		if args["paneId"] != "control" {
			t.Fatal("recovery targeted Pi pane")
		}
		script := string(unpackTaskInputScript(t, args["command"].(string)))
		if !strings.Contains(script, "-action recover-result") || strings.Contains(script, "collect-result") || strings.Contains(script, "pi --") {
			t.Fatal("unexpected action in recovery entry")
		}
		match := regexp.MustCompile(`printf '%s' '([A-Za-z0-9+/=]+)' \| base64 --decode`).FindStringSubmatch(script)
		if len(match) != 2 {
			t.Fatal("missing bounded stdin envelope")
		}
		raw, err := base64.StdEncoding.DecodeString(match[1])
		if err != nil {
			t.Fatal(err)
		}
		var received deliverymeta.ResultRecovery
		if json.Unmarshal(raw, &received) != nil || received.SourceRef != request.SourceRef || received.ContextHash != request.ContextHash || received.OperationFingerprint != request.OperationFingerprint || received.Plan.Task != request.Plan.Task || received.TargetFingerprint != agenvo.TargetFingerprint(target) {
			t.Fatal("original task/owner proof changed in dispatch")
		}
		commands++
		return agenvo.Receipt{Execution: "accepted", Result: json.RawMessage(`{"result":{"type":"accepted"}}`)}, nil
	})
	if err := service.RecoverProducerResult(ctx, f.binding, first.ID, "legacy_result", "explicit.png", strings.Repeat("a", 64), 131109, strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(f.store.records[first.ID])
	if !bytes.Equal(before, after) || commands != 1 || f.batch.runs != 1 {
		t.Fatal("preparation/acceptance rewrote operation or reran Pi")
	}
	f.denied = true
	if _, err := prepare(); err == nil {
		t.Fatal("revoked owner prepared a recovery")
	}
	f.denied = false
	wrong := f.binding
	wrong.SessionID = "foreign-session"
	if _, err := service.PrepareProducerResultRecovery(ctx, wrong, first.ID, "legacy_result", "explicit.png", strings.Repeat("a", 64), 131109, strings.Repeat("b", 64)); err == nil {
		t.Fatal("foreign owner accepted")
	}
	// A trusted owner sees an unknown-create fence, not absence. Preparation is
	// still read-only; the local entry must refuse mapping until it is resolved.
	r := f.store.records[first.ID]
	var details map[string]json.RawMessage
	_ = json.Unmarshal(r.Details, &details)
	entry := deliveryEntry{Reference: DeliveryRequest{Version: 1, Direction: "upload", ContentHash: request.SHA256, SizeBytes: request.SizeBytes, Mime: p.Artifact.Mime}, Scope: strings.Repeat("c", 64), Outcome: "reserved", At: time.Now(), Transfer: &transferAccess{Creating: true}}
	details["delivery_ledger"], _ = json.Marshal(deliveryLedger{Version: 3, Entries: map[string]deliveryEntry{p.DeliveryID(): entry}})
	r.Details, _ = json.Marshal(details)
	f.store.records[first.ID] = r
	unknown, err := prepare()
	if err != nil || unknown.RemoteOutcome != "unknown" || unknown.KnownReceipt != "" {
		t.Fatal("empty upload ID erased unknown-create fence", err)
	}
	if err := service.RecoverProducerResult(ctx, f.binding, first.ID, "legacy_result", "explicit.png", strings.Repeat("a", 64), 131109, strings.Repeat("b", 64)); err == nil || commands != 1 {
		t.Fatal("unknown upload outcome dispatched recovery")
	}
	entry.Transfer.Creating, entry.Transfer.UploadID = false, "original-receipt"
	details["delivery_ledger"], _ = json.Marshal(deliveryLedger{Version: 3, Entries: map[string]deliveryEntry{p.DeliveryID(): entry}})
	r.Details, _ = json.Marshal(details)
	f.store.records[first.ID] = r
	known, err := prepare()
	if err != nil || known.RemoteOutcome != "existing" || known.KnownReceipt != "original-receipt" {
		t.Fatal("existing original receipt was not retained", err)
	}
	t.Log("trusted original owner load + full completion validation + original control-pane dispatch; ledger creating fence/receipt retained; no operation writes/Pi redispatch; grant and owner denial checked")
}
