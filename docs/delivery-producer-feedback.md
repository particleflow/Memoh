# Advisory producer feedback

The existing finite Herdr command writes an advisory `delivery-feedback.json` in
its original execution attempt directory. This is part of the existing producer
handoff, not a new service, task registry or credential. `deliverymeta` owns the
shared attempt-relative path; `source_root` is still resolved once by HostConfig
and must match the original plan's root digest. No production directory moves or
ACL repair are performed.

The command reserves five seconds of its existing two-minute limit for this
projection. The queue admission lock and private queue validation are reused;
source handles are confined to the existing authorized attempt, its original
producer context and fixed feedback file. The job must retain the exact config
binding. Source ACLs are not rewritten. See [task source boundaries](delivery-task-source.md).
An unfinished admission retains its original identity for reporting;
it does not become an admitted job. Reports contain at most 16 ordered delivery
IDs, allowlisted status/reason/retryability, retry time and opaque change digests,
plus the original non-secret task binding. They cannot contain a successful
reference, source path, credential, raw diagnostic or file bytes. Output says
`feedback_status: unavailable` when the projection cannot be persisted.

## CLI feedback applicability

Component 0.10.1 adds one value to the existing `feedback_status` field:

| Value | Meaning |
| --- | --- |
| `recorded` | Outbound producer feedback was validated against its original task/attempt/declaration and persisted. |
| `not_applicable` | The returned durable queue record is a **download**. The outbound producer projection does not apply. No producer context is read or created. |
| `unavailable` | Applicable feedback could not be validated or written, including missing/foreign upload context and actual storage failure. |
| absent/empty | No applicable task result was returned, preserving existing idle/error and legacy behavior. This is not proof of an inbound download. |

The producer is `cmd/transferqueue`; the consumer is the unified Windows endpoint
invoking the existing finite actions. In particular `advance-results` may select
an inbound download from the same queue. Applicability comes from that validated
durable record's direction, never a missing file, CLI action name or untrusted
stdin hint. It remains `not_applicable` while the download is queued or blocked;
it does **not** acknowledge delivery, authorization or local publication.

Consumers may bypass only the outbound feedback guard for the explicit
`not_applicable` value. They must still check command exit, lifecycle/backoff,
phase, association and original delivery/reference. Keep `unavailable` handling
unchanged. Do not translate old binaries' `unavailable` to `not_applicable`:
upgrade/pin the producer and consumer contract together. Older consumers may
continue to refuse inbound automation safely until they accept the new value.
The v1 producer context/feedback JSON, task IDs, queue format, HostBinding and
NAS protocol are unchanged; no data migration or new configuration is needed.

`TestCLIPathFeedbackApplicability` covers no/foreign inbound context, valid v1
outbound feedback, missing/foreign outbound context, an actual feedback-file
open failure, repeat registration/inspect and an unknown delivery. The existing
outbound TLS restart fixture still requires `recorded`. The Windows-only
`TestCLIPathInboundFeedbackRestart` uses fresh CLI-dispatch processes, real TLS
and NTFS publication, checks complete bytes/hash/reference and verifies two
completed reopen/re-enqueue rounds issue no network requests. Its NAS is an
explicit protocol fixture; it does not claim real Server/PG or installed EXE
system-trust acceptance.

The Server reads only the fixed filename through the original registered control
pane, its checked pane/terminal/workspace identity and a fresh nonce. It requires
the complete original task, declaration digest and exact ordered delivery IDs.
The bounded JSON is saved in the existing operation details with revision CAS
and the existing permission checks. Command acceptance is never completion.
These are observations from the authorized PC, not hardware attestation.

A NAS committed ledger entry always supplies the successful reference, even if
an older advisory says blocked. For an uncommitted file, local `blocked` or
`unknown` is exposed with `needs_action: true` and `retryable: false`. An unchanged
block does not trigger more `collect-result` invocations. Explicit task queries
still read the small receipt, so a later local reconciliation can resume the
same ID. Pending backoff is respected. Cancellation remains possible for blocked
and unknown uncommitted files; it does not retract a committed delivery.

The projection is advisory, not another journal. A process death during its
small synchronized rewrite can leave invalid JSON. Readers reject partial,
foreign, missing or unsupported reports and retain the previous valid advisory;
no queue evidence is deleted. A subsequent finite local action can rewrite it.
An old host or a crash before any report exists retains the previous pending /
closure-or-admission-unknown behavior. This change does not invent evidence that
an absent manifest was closed or automatically rerun the producer.

Independent files continue when both job slots and byte quota permit. A blocked
file still occupies its slot and bytes. With one slot, a blocked first file can
prevent admission of later files; those are reported as `capacity_blocked` when
no queued work can release capacity. Sources, closed manifests, unknown receipts
and blocked journals are not automatically deleted. The existing 1024 retained
receipt cap is unchanged. Manual reconciliation or a capacity/configuration
change requires an explicit existing host action to refresh the projection;
there is no polling daemon. Upgrade the command and Server observer together;
older observers ignore the additive advisory and may continue harmless wakes.

## Validation boundaries

Queue tests use isolated HTTPS NAS fixtures and actual private host storage.
Observer tests use the real operation service with synthetic Agenvo/control-pane
transport and CAS storage. They do not claim a deployed Herdr/PC/NAS route.
Native Windows CI must execute the storage fixtures; Linux execution or Windows
cross-compilation alone is not Windows acceptance. Real PC installation, initial
credentials, trust and production verification remain outside this patch.

## Unattributable collection errors

A retained oversized, malformed, unreadable, filename-mismatched or foreign-root
manifest cannot supply a trustworthy task identity. The finite collector skips
such a candidate and tries later names in the existing bounded enumeration
(maximum 4096 directory entries, at most 16385 bytes read per candidate, existing
two-minute invocation deadline). It still admits at most one valid manifest and
uses the existing bounded advance loop. If none is valid, it may advance one
already sealed queue job. It does not rename, delete, quarantine or import the
invalid metadata.

Progress on a valid delivery is returned alongside `ErrProducerSource` /
`ErrProducerManifest`; the CLI exposes `source_collection: invalid_manifest` and
exits nonzero. This is not idle or a claim that the bad file was delivered. The
small receipt for a valid job reports only its verified original plan. When that
plan explicitly declares a different malformed manifest, its existing per-file
projection can say `manifest_invalid`; unknown unrelated metadata never creates
a task entry. A source containing only unbound bad metadata therefore still
cannot stop a particular task's unknown wakeups. That observable unknown remains
until attribution is established through an existing valid handoff or explicit
local repair. No task is guessed from a bad body, and no cleanup frees its quota.
