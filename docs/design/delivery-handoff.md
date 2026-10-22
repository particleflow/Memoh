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
with bounded concurrency, a finite deadline and a persisted lease epoch.
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

The queue adapter checks server authorization before publishing. It sends a
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
