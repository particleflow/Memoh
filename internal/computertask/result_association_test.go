package computertask

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/felinics/memoh/internal/agenvo"
	"github.com/felinics/memoh/internal/db"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/deliverymeta"
	"github.com/felinics/memoh/internal/mcp"
	"github.com/felinics/memoh/internal/media"
	"github.com/felinics/memoh/internal/storage/providers/localfs"
	"github.com/felinics/memoh/internal/toolcontext"
)

type associationReadAssets struct {
	*media.Service
	after   func()
	corrupt bool
	size    int
}

func (a associationReadAssets) Open(ctx context.Context, bot, hash string) (io.ReadCloser, media.Asset, error) {
	r, m, e := a.Service.Open(ctx, bot, hash)
	if a.after != nil {
		a.after()
	}
	if e == nil && a.corrupt {
		_ = r.Close()
		return io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), a.size))), m, nil
	}
	return r, m, e
}

// The current row layout supplies the same scoped ledger facts as legacy JSON.
type associationRowStore struct {
	*fixtureStore
	entry dbstore.ComputerDeliveryEntry
}

func (s *associationRowStore) ReadComputerDeliveryEntry(ctx context.Context, r dbstore.ComputerOperationRecord, id string) (dbstore.ComputerDeliveryEntry, error) {
	if _, err := s.ReadComputerOperation(ctx, r); err != nil {
		return dbstore.ComputerDeliveryEntry{}, err
	}
	if id != s.entry.DeliveryID {
		return dbstore.ComputerDeliveryEntry{}, db.ErrNotFound
	}
	return s.entry, nil
}

func (*associationRowStore) SaveComputerDeliveryEntry(context.Context, dbstore.ComputerOperationRecord, dbstore.ComputerDeliveryEntry) (dbstore.ComputerOperationRecord, error) {
	panic("result association must not mutate the delivery ledger")
}

func (s *associationRowStore) ListComputerDeliveryEntries(context.Context, dbstore.ComputerOperationRecord, string, int) ([]dbstore.ComputerDeliveryEntry, error) {
	return []dbstore.ComputerDeliveryEntry{s.entry}, nil
}

func TestCommittedResultAssociationVerifiesBytesAndLiveLedger(t *testing.T) {
	for _, mode := range []string{"committed", "committed-rows", "partial", "pending", "ready", "forged-json", "target-changed", "wrong-provider", "foreign-bot", "foreign-session", "unknown", "failed", "canceled", "foreign-plan", "corrupt-bytes", "revoked-on-open", "foreign-user", "ledger-changed"} {
		t.Run(mode, func(t *testing.T) {
			source, store, _, _ := setupBatch(t)
			b := toolcontext.ComputerBinding{BotID: "20000000-0000-0000-0000-000000000001", UserID: "20000000-0000-0000-0000-000000000002", SessionID: "20000000-0000-0000-0000-000000000003", TargetID: "20000000-0000-0000-0000-000000000004", RunID: "source-run"}
			mount := store.mounts["pc-a"]
			mount.ID, mount.BotID = b.TargetID, b.BotID
			store.mounts = map[string]dbstore.BotRemoteRuntimeBindingRecord{b.TargetID: mount}
			source.service.relay = agenvo.NewService(store, producerPermissions{}, nil)
			ctx, err := toolcontext.WithComputerBinding(t.Context(), b)
			if err != nil {
				t.Fatal(err)
			}
			originals := media.NewService(nil, localfs.NewOriginals(t.TempDir()))
			data := []byte("real original bytes for committed file association")
			asset, err := originals.Ingest(ctx, media.IngestInput{BotID: b.BotID, Mime: "text/plain", Reader: bytes.NewReader(data)})
			if err != nil {
				t.Fatal(err)
			}
			ref := DeliveryRequest{Version: 1, Direction: "upload", ContentHash: asset.ContentHash, SizeBytes: int64(len(data)), Mime: asset.Mime}
			target := fixtureTarget(store.mounts[b.TargetID])
			r := dbstore.ComputerOperationRecord{ID: "operation", BotID: b.BotID, UserID: b.UserID, SessionID: b.SessionID, TargetID: b.TargetID, RunID: b.RunID, State: "completed", TargetFingerprint: agenvo.TargetFingerprint(target), Revision: 3}
			p := &deliverymeta.Plan{Version: 1, Task: deliverymeta.Task{OperationID: r.ID, BotID: r.BotID, UserID: r.UserID, SessionID: r.SessionID, TargetID: r.TargetID, RunID: r.RunID, Attempt: strings.Repeat("a", 32), Generation: strings.Repeat("b", 32), RootHash: deliverymeta.RootHash(`C:\Fixture`)}, Artifact: deliverymeta.Artifact{SourceRef: "result", Mime: ref.Mime}}
			// Operation IDs in the producer contract are UUIDs.
			r.ID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
			p.Task.OperationID = r.ID
			d := Details{Kind: "herdr_pi_task", Batch: &BatchDetails{Progress: agenvo.BatchProgress{CompletionVersion: agenvo.BatchCompletionVersion, Attempt: p.Task.Attempt, Generation: p.Task.Generation, Producer: p}, Receipt: &agenvo.BatchReceipt{Started: true}, Result: &agenvo.PiJSONResult{Outcome: "succeeded"}}}
			entry := deliveryEntry{Reference: ref, Scope: strings.Repeat("d", 64), At: time.Now(), Outcome: "committed"}
			switch mode {
			case "partial":
				p.Version = 2
				p.Artifacts = []deliverymeta.Artifact{p.Artifact, {SourceRef: "second", Mime: p.Artifact.Mime}}
				p.Artifact = deliverymeta.Artifact{}
			case "ready":
				entry.Outcome = "ready"
			case "forged-json":
				d.Batch.Progress.Producer = nil
			case "pending":
				entry.Outcome = "pending"
				entry.Lease = "lease"
				entry.Until = time.Now().Add(time.Minute)
				entry.Attempts = 1
			case "unknown":
				r.State = "unknown"
			case "failed":
				d.Batch.Result.Outcome = "failed"
			case "canceled":
				r.CancelRequested = true
			case "foreign-plan":
				p.Task.UserID = "other-user"
			}
			raw, _ := json.Marshal(d)
			var details map[string]any
			if err := json.Unmarshal(raw, &details); err != nil {
				t.Fatal(err)
			}
			entries := map[string]deliveryEntry{}
			if mode == "partial" {
				entries[p.FilePlans()[0].DeliveryID()] = entry
			} else {
				entries[p.DeliveryID()] = entry
			}
			details["delivery_ledger"] = deliveryLedger{Version: 3, Entries: entries}
			r.Details, _ = json.Marshal(details)
			store.records[r.ID] = r
			if mode == "committed-rows" {
				details["delivery_ledger"] = deliveryLedger{Version: 5}
				r.Details, _ = json.Marshal(details)
				store.records[r.ID] = r
				entryData, _ := json.Marshal(entry)
				source.service.store = &associationRowStore{fixtureStore: store, entry: dbstore.ComputerDeliveryEntry{DeliveryID: p.DeliveryID(), Version: 3, Data: entryData, Ordinal: 1}}
			}
			if mode == "target-changed" {
				changed := mount
				target.InstanceID = "changed-instance"
				changed.RuntimeProviderConfig = fixtureConfig(target)
				store.mounts[b.TargetID] = changed
			}
			if mode == "wrong-provider" {
				changed := mount
				changed.RuntimeProvider = "unknown-provider"
				store.mounts[b.TargetID] = changed
			}
			source.service.assets = associationReadAssets{Service: originals, corrupt: mode == "corrupt-bytes", size: len(data), after: func() {
				if mode == "revoked-on-open" {
					store.revoked = true
				}
				if mode == "ledger-changed" {
					changed := store.records[r.ID]
					changed.Revision++
					store.records[r.ID] = changed
				}
			}}
			links := 0
			source.SetResultAssociator(func(_ context.Context, got dbstore.ComputerOperationRecord, refs []DeliveryRequest) error {
				links++
				if got.RunID != b.RunID || got.UserID != b.UserID || got.SessionID != b.SessionID || len(refs) != 1 || refs[0].ContentHash != asset.ContentHash {
					t.Fatal("association scope or verified original changed")
				}
				return nil
			})
			caller := b
			if mode == "foreign-user" {
				caller.UserID = "other-user"
			}
			if mode == "foreign-bot" {
				caller.BotID = "other-bot"
			}
			if mode == "foreign-session" {
				caller.SessionID = "other-session"
			}
			err = source.associateResult(ctx, caller, r.ID)
			if (mode == "committed" || mode == "committed-rows" || mode == "partial") && err == nil {
				// Exercise the actual tool adapter, including repeated observations. Only
				// typed ledger projections reach the hook; no JSON hash extraction occurs.
				session := mcp.ToolSessionContext{BotID: b.BotID, UserID: b.UserID, SessionID: b.SessionID, RunID: b.RunID, RunContext: ctx, RequireActiveRun: true, RuntimeActive: true}
				tools := []string{"computer_task_result", "computer_task_result", "computer_task_get", "computer_task_artifact"}
				if mode == "partial" {
					tools = []string{"computer_task_get", "computer_task_get", "computer_task_get", "computer_task_artifact"}
				}
				for _, name := range tools {
					output, toolErr := source.CallTool(ctx, session, name, map[string]any{"operation_id": r.ID})
					if toolErr != nil || output["isError"] == true {
						t.Fatalf("get result adapter: %v %v", output, toolErr)
					}
				}
			}
			if mode == "committed" || mode == "committed-rows" || mode == "partial" {
				wantLinks := 4
				if mode == "partial" {
					wantLinks = 2
				}
				if err != nil || links != wantLinks {
					t.Fatalf("committed=%v links=%d", err, links)
				}
			} else if links != 0 {
				t.Fatalf("mode=%s linked=%d error=%v", mode, links, err)
			}
		})
	}
}
