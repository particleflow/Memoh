# Delivery handoff candidate

This increment depends on the unmerged delivery client/native-host stack and
the operation association store. Its integration base is
`b4b5555ee91f8ea8a8ff5473f7eacf864f91ea98` (PR #16 plus frozen PR #15).
It does not register production routes or change Agent code, Media, roots,
credentials, deployment, or the frozen Windows acceptance bundle.

## Verified identity boundary

`toolcontext.HostComputerBinding` and `WithDeliveryIdentity` are process-local
capabilities. The JWT middleware authenticates accounts/chat routes, not PC
attempts. `agentcredential` stores upstream credentials. The user-runtime API
key authenticates a runtime, not an operation/device/attempt. None supplies an
approved incoming PC identity. An eventual authenticator must verify and bind
Bot/user/session/target/run/operation/device/instance/attempt/generation, and
support current validity checks for finite background verification. Missing
bindings fail closed. Synthetic fixtures are not production authentication.

## Finite verification in the existing operation

Persist `pending` before scanning Media, using the existing operation revision
CAS and a versioned delivery ledger. Use the existing in-process `job` unit
with two concurrent scans, a ten-minute cooperative deadline and a persisted
lease epoch. At most three lease claims are allowed per delivery; exhausted or
failed verification requires explicit diagnosis, not a new delivery ID retry.
Commit returns promptly; Resolve reports pending, committed, canceled or failed.
An unknown database commit is an error, never absence. Request cancellation
does not cancel accepted verification. A process crash leaves a queryable
lease; a later authorized request can reclaim an expired lease with a bounded
attempt budget. There is no daemon, polling loop, independent task registry or
guaranteed idle scheduling. Verification may restart after a crash; unchanged
Media can still scan twice. This removes the two-minute HTTP deadline from the
scan, not disk costs or physical I/O stalls.

Fresh authorization runs before scanning and again in the final transaction.
The lease epoch must match before any result is published. Cancellation fences
an upload even while a scan is in flight. A failed or exhausted attempt remains
explicitly visible; retries cannot turn it into absent. Existing queue retry
budgets may pause reconciliation and require its explicit ResumeAssociation.

## Download publication and cancellation

The host destination storage supplies an idempotent, durable publication port
keyed by the full immutable reference. Its Publish and Resolve(cancel=true)
must atomically choose published or fenced, with a recoverable unknown state,
and reject later publication after a fence. Both use the same host job lock and
destination ownership checks. No new destination root or local registry is
introduced here. Missing destination binding is an error.

The queue adapter checks server authorization before publishing. The candidate request includes a Bot ID which must equal the verified host
binding (the ID grants no authority). It sends a
published attestation only after local publication is durable, and a fenced
attestation only after local cancellation is durable. A lost publication reply
is resolved locally before asking the NAS to cancel. The server requires these
attestations for download commit/cancel; a NAS tombstone alone does not prove
that Windows prevented publication. These are verified-host assertions, not
credentials or proof against a dishonest authorized host. Published downloads
remain published when cancellation arrives; association may still be pending
or denied. Business execution completion, local publication and NAS association
are three separate facts.

## Consistency and rollout

Authorization reflects the final database transaction snapshot. Revocation
committed before that snapshot denies publication of an association; concurrent
revocation after it can race with acknowledgement. Windows publication similarly
cannot offer instantaneous cross-machine revocation. Files are verified but not
pinned against retention/deletion. A reference is not a retention guarantee.

A database trigger rejects removal or version downgrade of an existing ledger;
current execution SQL preserves it. All writers must still be upgraded before
enabling the candidate. Do not mix old association binaries with a newer ledger
or roll back its version. Explicit local migration/backup review is required for
legacy download cancellation records: they did not attest a local fence.

Required fixtures cover lost submit replies, slow verification beyond request
deadlines, restart/lease recovery, cancellation races, revocation during scans,
concurrent execution receipt CAS, old SQL rejection, and local publication
unknown/cancel races. Production HTTPS, real PC identity and destination binding,
Windows acceptance, runtime/UI screenshots and human QA remain outstanding.

## Candidate protocol and evidence

The async handler keeps request reference version 1 and adds required `bot_id`
and optional `publication`. Responses use version 2; synchronous version-1
responses are rejected by the new queue adapter. All routes remain unregistered
in the application. Request bodies are limited to 4096 bytes and all candidate
responses use no-store. Queue journal version 2 and its single resolved Herdr
state directory remain unchanged; no second task state service is added.

`computer_delivery_handoff_integration_test.go` uses an isolated PostgreSQL role
without RLS bypass, the actual Media upload/Range and association handlers, TLS
certificate verification, and the actual disk queue. Its credential verifier
and local publication store are explicitly synthetic. Its local store is a
Linux fixture, not a Windows destination adapter or a production ACL contract.
The native Windows runner separately exercises the existing queue binding; it
does not prove a deployed Herdr plugin has authenticated or published a file.

No new numeric throughput claim is made. The prior cached local Media benchmark
still applies only to disk verification. Async admission removes the caller's
wait for two full scans; it does not reduce those reads, pin files, bound a
stalled kernel I/O call, or prove a network/disk throughput ceiling. Real source,
NAS and network benchmarks and approved HTTPS/PC identity remain acceptance
prerequisites.

The unchanged Media Range GET still performs full verification before its first
response bytes. A slow NAS may exceed the Windows two-minute invocation budget
there. This increment fixes association Commit admission only; Range preflight
requires a separately reviewed Media change or a bounded verified-handle reuse
contract. No timeout bypass, unverified download, or blanket large-file readiness
is claimed. The current permission to leave Media unchanged remains in force.
