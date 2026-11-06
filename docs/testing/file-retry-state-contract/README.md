# File recovery scheduling contract

This isolated source change is based on producer `212ae2445fe7a5fbe95479a4f2024069bf83332b`. It addresses audit HC-01/HC-02. It does not activate a Windows producer, change a production pin, or establish NAS/device acceptance.

## State and clocks

`Job.Attempts` and `AssociationAttempts` are independent diagnostic/backoff counters, not total execution budgets. Retryable transport/HTTP failures keep the original recoverable phase. The transfer clock is `Job.NextAttempt`; the publication reconciliation clock is `AssociationDue`. Successful transfer progress retains its existing counter-reset behavior. These counters must not be replaced by a larger fixed terminal threshold. They saturate at the Go integer representation limit instead of wrapping.

The existing local exponential backoff ceiling remains a pacing default in one shared helper. It never overrides a later server deadline. Each invocation remains finite: no sleep, new worker, polling service, or retry loop is introduced. The existing host schedules another invocation only when due. Context cancellation returns before starting an already-canceled action; an interrupted in-flight mutation still retains its original unknown-outcome semantics. A caller that stops invoking the adapter stops automatic progress.

For `Retry-After`, delta-seconds and HTTP dates are both honored. The internal HTTP error also retains the absolute server deadline, so a date beyond `time.Duration`'s range cannot wrap or be truncated. `NextAttempt`/`AssociationDue` persist the later of local backoff and the server lower bound. Cancel intent and explicit association resume preserve an existing deadline. Malformed or past headers fall back to local pacing. A numeric deadline outside the journal's JSON `time.Time` representation (years 1..9999) refuses automatic retry as a protocol error; it never becomes an earlier retry. 401/403 retain authorization precedence even with an unrepresentable header.

Unknown association outcomes still Resolve before any subsequent Commit. Only an authorized `absent` outcome permits the existing same-ID idempotent retry-commit path. Committed/canceled outcomes, authorization and integrity refusals, receipt expiry/unavailability, immutable identity/reference checks, hash verification, CAS, snapshot protection, retention and admission limits retain their existing roles. No new attempt budget or approval is added; existing capacity configuration is not repurposed as a retry budget. This producer has no existing configurable total-retry policy to wire through.

## Historical and consumer compatibility

No task, receipt, source reference, HostBinding, root fingerprint, queue location or journal version is replaced. Existing `blocked/retry_budget` records stay paused. Existing `create_unknown` records are never re-POSTed. An existing blocked association is resumed only through the existing explicit resume action and original Resolve/authorization contract, without erasing unknown outcome or shortening its due time.

New writers can persist counters greater than 12. Old binaries reject these records under their old validation rule, including while scanning a queue. This is a real downgrade boundary: preserve the original queue and records; do not delete, edit, cap counters, or silently fall back to an old producer to make it read them. Deployment/pin coordination belongs to the original owners and is not performed by this qualification.

Windows consumer handoff (no Windows repository files changed):

- Keep using the same producer CLI, receipt-binding result and existing lifecycle/feedback fields. No new HTTP endpoint, wire request or credentials are introduced. `HTTPError.RetryAt` is an internal Go error field, not a new server protocol.
- Honor `Job.NextAttempt` / `AssociationDue` (and the existing feedback `retry_at`) as persisted not-before times. A per-invocation timeout or a host restart counter is a separate layer and must not turn these records into terminal failures or reset their clocks.
- Do not clear unknown association state, assign a new delivery/operation identity, or invoke Commit because a local attempt counter has reached a threshold. Preserve original receipt-binding and authorization/CAS ports.
- Consume this source through the normal qualified producer build/pin process. This change does not claim the installed Windows binary already contains it. Web and Harmony do not consume this Go journal; their own retry paths require their owners' independent verification.

## New validation only

The evidence directory contains the original-producer red run and the affected green runs. All use the cached Go 1.25.7 toolchain with `-mod=readonly`, `GOPROXY=off`, `GOTOOLCHAIN=local`; no dependency downloads, model calls or production build ran.

- Original producer fails the new server-deadline, consecutive-transfer, unknown-association, retry-commit and already-canceled-action assertions.
- Fifteen consecutive upload failures keep the same receipt. A real child process opens the same persisted queue and reads the exact state; the parent then reopens and completes the actual 32 bytes/hash with Create=1. The child is an independent journal reader, not a claim that it executed a transfer.
- Unknown association survives sixteen failed calls and child readback; Commit=1, Resolve=16 including final committed reconciliation. Long commit/resolve deadlines and cancel intent are preserved. Thirty alternating commit/absent-reconcile calls remain recoverable; authorization then stops calls.
- Actual local TLS limits responses with delta-seconds=3600 and year-3000 HTTP date persist exact not-before timestamps across child readback; one request only before due. These are synthetic endpoints, not field addresses or TLS acceptance.
- Fifteen download failures retain their own clock/count, then 43 actual bytes/hash are verified and the existing publication port commits once. The association counter begins independently at one. Revocation, cancellation, old blocked/unknown states, malformed/overflow headers and counter overflow receive targeted assertions.
- Only affected old retry assertions were updated/run. The earlier 100,663,395-byte endpoint recovery qualification is reused, not rerun.

The upload hash is `16231252a03f3d5df8678c98e1fab3356124a9128b24165f669e33f4c091713d`; the download hash is `28c70ab1474324df940bba2399cbb475c01735f4c97f68ff7dc681f4dc150e8a`.

This is Linux isolated source qualification. No new real PostgreSQL ledger test, Windows/NTFS/UNC execution, actual NAS transfer, phone test, running application/UI screenshot, production package, deployment or field upgrade is claimed. Existing server CAS/ledger integration evidence is reused; the association tests here use the existing injected synthetic binding.

Audit material: Library `libfile_34f4bd2b7acc8191b0bb1fa7d506eb4f` v1, 97137 bytes, SHA256 `6bf9e33515720f5b0f01aecd6ad67ab83e929dbd7c6aec36462a8b5a0dfd20c8`, with all 25 internal manifest entries matching. The original `ff0f...` digest identifies the earlier version; the parent confirmed the normal version update. No corrupt-material claim remains.
