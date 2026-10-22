# Finite Herdr delivery adapter: integration handoff

Date: 2026-10-22. Base: merged PR #8,
`eeb1fdabf50ec94d48f08302c9da3f5b3811293e` (same tree as `a6149e63`).
PR #10 remains a draft. This is an implemented disk adapter and finite action
boundary with isolated tests, **not an installed Windows plugin or a production
file-delivery system**. No production acceptance claims are added here.

## Concrete host and ownership

Herdr v0.9.5 has a real plugin command/startup/event mechanism in
[runtime.rs](https://github.com/herdrdev/herdr/blob/2ecf19a92fc3a7fff75b6ce3655ef04f4625ec01/src/app/api/plugins/runtime.rs).
Its [environment binding](https://github.com/herdrdev/herdr/blob/2ecf19a92fc3a7fff75b6ce3655ef04f4625ec01/src/app/api/plugins/env.rs)
supplies `HERDR_PLUGIN_CONFIG_DIR` and `HERDR_PLUGIN_STATE_DIR` from centralized
[plugin paths](https://github.com/herdrdev/herdr/blob/2ecf19a92fc3a7fff75b6ce3655ef04f4625ec01/src/plugin_paths.rs).
The official [manifest documentation](https://github.com/herdrdev/herdr/blob/2ecf19a92fc3a7fff75b6ce3655ef04f4625ec01/docs/next/website/src/content/docs/plugins.mdx)
permits finite argv commands; plugins own their durable state. Startup hooks
are one-shot and do not establish an idle-time retry scheduler.

`transferclient.InvokeHerdr` implements the finite action boundary: enqueue an
upload/download, advance one job, inspect, request cancellation, or retire an
acknowledged delivery. Each invocation reopens the disk store. There is no
looping worker, detached process, listener, daemon or task-status service.
The existing plugin command embedding this Go adapter is **still to be packaged**;
no fictitious executable or installable manifest is supplied. The actual PC's
Herdr version, installed plugin inventory and wake-up source remain unverified.

The sole local locator input is Herdr's already-resolved STATE_DIR. The adapter
derives one private `deliveries-v1` namespace there; no second root configuration,
path registry, cwd fallback, global temp spool or per-module root exists.
Its immutable header binds schema version, locator revision, canonical location
and queue limits. Moving it or changing its version/limits fails explicitly;
an offline migration is not implemented. CONFIG_DIR, Herdr logs and sessions,
user data, server Config → storage.Provider/media.Service and NAS original
locations retain their existing ownership. No production directories move.

Local persistence is necessary because the public Herdr plugin API does not
offer a transactional transfer queue. The adapter uses `os.Root`, OS locks and
ordinary private files rather than introducing a general filesystem service.
The core thread continues to own PC execution-completion receipts. This queue
records only file transfers/outbox state; `operation_id` is correlation, never
authorization or a duplicate execution lifecycle.

## Implemented behavior and limits

| Concern | Current adapter behavior |
| --- | --- |
| Stable source | Producer supplies an already-authorized regular-file handle plus expected size/hash. Stream into a private snapshot, verify the declared SHA-256, fsync, then admit. Subsequent source writes cannot change the copy. A mixed/inconsistent source cannot match the declared digest. |
| Reopen | Validate containment, regular file, owner/private Unix permissions and single hardlink; rehash every reopened upload snapshot before network mutation. Snapshot files become read-only. Trusted same-user processes must respect ownership; this is not a sandbox against malicious code running as that user. |
| Persistence | Length + SHA-256 + bounded JSON journal; append and fsync before acknowledgement. Recover only a torn final frame. Full corrupt frames, incompatible versions and exhausted journal budgets stop. Directory sync covers admission and retirement on Linux. |
| Locking | Nonblocking cross-process OS lock for the entire job invocation and separate bounded worker slots. Stable striped lock files are never unlinked, so retirement cannot split a lock. Colliding stripes serialize harmlessly. |
| Backpressure | At most 128 retained jobs, 4 workers, 1 TiB configured reserved bytes and 8 MiB per journal; actual limits are explicit and immutable. Failed admissions reserve quota until reconciliation. Conservative free-space check includes reservations plus 16 MiB margin; later ENOSPC still fails the checkpoint. No silent eviction. |
| Bounded execution | One job action with a two-minute context budget; host callbacks must honor cancellation and OS disk calls can outlive the deadline. Upload at most one new chunk/complete/cancel. No sleeps or autonomous wake-ups. HTTP and memory remain bounded by the existing client; source copies use 256 KiB. |
| Resume/retry | Reuse PR8 GET authoritative receipt before PATCH/complete. Persist finite retry due times/budgets. Create response loss remains `create_unknown`; no auto-create replay, new protocol or server change. |
| Cancellation | Persist intent under the job lock; active owner can cancel its context first. Known active receipts use existing DELETE. Ready cancellation suppresses unassociated outbox publication, never deletes NAS originals. Unknown or blocked upload cancellation returns a binding/reconciliation error; it is not falsely reported as server-canceled. |
| Result outbox | Ready stays unassociated until `Associate` atomically reauthorizes the current task and deduplicates the stable delivery reference. Lost response retries the same reference, at most 12 attempts. No real registrar exists in this patch; a nil binding errors explicitly. |
| NAS → PC | Persist private staging offsets only after fsync, resume Range/If-Range, verify full hash, and recheck authenticated HEAD/hash even at full offset. Only then expose a bounded reader to the authorized existing destination storage binding; no remote filename is used. |
| Retirement | Explicitly remove local bytes/journal only after association acknowledgement or confirmed cancellation. Never touch NAS originals. Registration must retain delivery-id/content binding after local retirement. Unknown/failed jobs are retained. |

The immutable snapshot check intentionally rereads the whole file on every
finite upload invocation. With N chunks it can read approximately N times the
file size; this is a known disk/CPU throughput cost, not a claim of NIC saturation.
Optimizing it requires a reviewed immutable-handle identity strategy and actual
Windows/storage measurements, not weakening the hash/containment checks.

## Reproducible cloud verification

Toolchain: Go 1.25.7, golangci-lint 2.10.1. Temporary local Linux filesystem and
httptest TLS only. All tokens/accounts/authorization results are explicit fixtures.
No real PC, NAS, production identity, runtime installation or certificate changes.

The final local race runs passed: 18 client/disk/store tests with 42 subtests
([log](queue-tests.txt)), and both actual-handler integration tests
([log](merged-handler-tests.txt)). Client vet, formatter and Agent boundary
checks passed. Windows amd64 test binary cross-compilation passed; it was not
executed. [Benchmark output](queue-benchmark.txt) records the synthetic disk-read
cost, separately from the protocol correctness tests.

```sh
go test -p 2 -race -tags pr8contract -count=1 -v ./internal/transferclient
go test -p 2 -race -run '^(TestPR10ClientAgainstIntegratedPR8Handlers|TestHerdrActionAgainstMergedUploadHandlers)$' -count=1 -v ./internal/handlers
go vet ./internal/transferclient
go test -run '^$' -bench '^BenchmarkDiskSnapshotReopen$' -benchtime=1x -count=3 ./internal/transferclient
golangci-lint run ./internal/transferclient ./internal/handlers
GOOS=windows GOARCH=amd64 go test -c ./internal/transferclient
bash scripts/check-agent-core-boundary.sh
```

The original protocol suite remains intact. New disk tests cover post-admission
source mutation, snapshot tamper, wrong hash, hardlinks/symlinks, changed locator
or revision, retained quota, journal capacity, corrupt/full and torn/partial
frames, cross-instance job/worker locks, abrupt child-process exit without
unlock/close, disk reopen, lost PATCH response, create unknown, queued cancel,
retirement, registration response loss, revoked association, retry exhaustion,
download Range/unacknowledged-tail truncation and full-offset hash/permission
failure before association. Process exit is **not physical power-loss testing**.

`internal/handlers/transferclient_compat_test.go` now runs against the actual
merged NewServer/JWT/MessageHandler routes. The original real-handler protocol
fixture checks lost create/PATCH/complete responses, idempotent complete/cancel,
cross-Bot isolation, revocation and Range/hash. The new finite-action test reopens
the real disk queue every invocation, uploads through those handlers, rejects
a revoked ready association through an explicit registrar fixture, and downloads
the same bytes through the real media handlers into a verified reader.
Task registration is still a fixture, not a new production authorization API.

Local snapshot verification benchmark (three single-iteration runs per size):
1 MiB took 0.92–0.99 ms and 512 MiB took 0.546–0.684 s. Both allocated exactly
262,400 B/op in 7 allocations. This measures cached local-file hash verification,
not cold-disk throughput, end-to-end RSS, TLS transport or NAS/NIC speed.

## Exact remaining integration conditions

1. **Windows native durability/ACL binding is unavailable.** `OpenHerdrQueue`
   returns `ErrBinding` before reading its environment or writing files on
   Windows. `LockFileEx`/reparse and single-link checks compile, but file flush
   alone is not proof of durable directory creation/private DACL. No Windows
   test was executed. Supply/review the native private-directory and durable
   admission/retirement and case-insensitive name/lock identity implementation,
   then test NTFS crashes, junctions,
   antivirus/file locks, disk-full and large files on a real approved PC.
   macOS code also has not been executed here.
2. **Plugin executable and wake-up integration remain uninstalled/unpackaged.**
   The action boundary and tests are concrete; actual finite argv packaging,
   authorized producer hook and retry wake-up require the verified PC host.
   Herdr startup/events alone do not promise unattended retries while idle.
3. **Memoh task-scoped identity and result registrar are missing.** Use the
   existing ComputerOperationStore owner/Bot/session/target-fingerprint checks
   and revision CAS for authorized idempotent association. `Associate` must
   recheck authority in the same operation as registration/publication; checking
   a flag in this client or a prior GET is insufficient. Coordinate this binding
   with core; do not create another task service. Download destination resolution
   belongs to its existing storage owner, not a path in a model message.
4. **Approved encrypted origin/identity delivery are not demonstrated.** Current
   user-described IPv6 HTTP is not encrypted. The client still requires trusted
   HTTPS and matching hostname/IP SAN, rejects redirects/insecure verification,
   and never treats Agenvo OAuth or a broad Bot/account token as PC authority.
   Proposed short-lived revocable capabilities must bind owner/Bot/session/
   target/operation/direction/hash/size/receipt/expiry and preserve receipt owner
   identity across refresh. No keys, certificates, tokens, port openings or
   network/security settings were created or changed in this work.
5. **Performance/real UI acceptance are incomplete.** Repeat disk-only snapshot,
   hash-only CPU, approved-route network baseline and full protocol runs on the
   same fixed sizes, recording concurrency, chunk sizes, RSS, CPU, disk bytes,
   retries and end-to-end seconds. Warm-cache Linux/loopback numbers cannot
   establish Windows/NAS/NIC throughput. AGENTS.md requires current dev-stack,
   real UI and screenshots; this task forbids Memoh runtime installation and
   the environment lacks mise. No UI evidence was produced or substituted;
   keep draft and Human QA unchecked.

Create-idempotency remains a compatible follow-up proposal only: advertise a
new capability before using a client-persisted key; bind it server-side to owner,
Bot, authorized task and canonical spec in the existing receipt journal under
its admission lock. Replays must reauthorize and return the original receipt;
different specs conflict. Retention/tombstone rules must outlive the retry window,
because PR8's 24h GC otherwise erases dedupe evidence. This patch changes none
of that protocol and never infers receipt creation from content-hash dedupe.
