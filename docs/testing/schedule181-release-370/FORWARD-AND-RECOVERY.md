# Forward-only release and latest-history recovery

## Scope and source order

Use this as a later independent release batch. Do not replace the unified owner's current fast-path/HAP head with this branch. If that head advances beyond370, combine this exact370 delta on it, preserve its intervening changes, inspect overlapping files and run only the resulting new seams plus normal release CI. This package already resolves the known370 shared-result conflict and supplies full source.

The candidate source order is370 → six schedule commits → test-only181 guard alignment. Deployment is of the final source, never the intermediate181 variants. Database forward order is existing clean180 → the complete final0181_schedule_result_delivery.up.sql in one migration transaction → final binary. No split execution of earlier181 versions is intended.0001 is a fresh-install snapshot, never an upgrade script for an existing database.

Final181 adds schedule.result_origin, schedule.limit_disabled, schedule.registration_generation; schedule_logs.result_origin/result_delivery/scheduled_for/claim_deadline/claim_state; and the unique(team_id,schedule_id,scheduled_for) partial index. It does not backfill requestor/contact, create fires, change enabled state, merge roots or delete history. Missing trusted origins stay legacy. Defaults are{} / false /0 /NULL / empty state as appropriate.

## Writer boundary and forward procedure

These steps are a future authorized operator procedure; none was executed on production here.

1. Read the actual version/dirty flag and schema shapes; confirm180 clean and that181 has not been occupied by another batch. Preserve the latest DB backup/WAL position, exact source/image/Web/HAP versions and original schedule/fire/run/invocation/delivery identities. The parent-reported370/schema180 is not a substitute for this release-time readback.
2. Coordinate a writer maintenance boundary. Allow active work to finish where possible, then stop every old application/scheduler instance that can admit or publish against this DB. Verify process termination, not merely a completed stop request or an empty UI. The in-process cron starts from NewService and Bootstrap; a second 'read-only' old server still creates a scheduler unless independently proven otherwise.
3. Shutdown is cancellation plus bounded worker waiting (schedule.Service.Shutdown), not a promise to drain every active external effect to success. If work is interrupted, retain its original identities and receipts; do not infer failure from a missing reply or create replacement execution. Do not toggle all plan rows or clear queues to simulate a maintenance barrier.
4. With old writers stopped, use the final candidate's existing `memoh-server migrate version`, then `memoh-server migrate up`, then version/read-only schema verification against the intended deployment config. This source's highest migration is181. The tool's `up` applies all bundled forward migrations; do not invent an `up 181` command. Abort on dirty status or an unexpected source/version, rather than forcing the marker.
5. Check complete final181 fields/index, original row counts/identities and receipt/history samples. Start only the compatible final scheduler writer(s) according to the existing deployment topology. Multi-instance same-due protection applies to this new claimed path; it does not make old/new mixed writers safe.
6. Check original contact, directory and retained unknown records, then perform the narrow matching-version acceptance listed in the evidence matrix. Do not manually replay overdue or unknown records merely to show success. Bootstrap arms future dues; no historical catch-up was added.

If an isolated or future environment already has version181 from an earlier candidate, the marker alone is insufficient. Compare the complete final field/index manifest. Prepare a separately reviewed additive follow-up using the next available migration if required. Do not run production down/up, force180, or drop fields to make the marker appear correct.

## Callback/fire compatibility

| State at cutover/recovery | Required behavior |
|---|---|
| Old schema180 callback captured only in memory, no persistent grant | Stop the old process before new admission. There is no durable occurrence identity to recover from that old callback; do not invent one or claim missed-dues catch-up. Existing legacy plans remain legacy, not silently opted in. |
| New181 callback has not won the claim | It carries the registration's persisted generation and immutable due. Pause/explicit enabled writes change generation; stale callbacks create no count/fire/session/run after resume. Same due can be claimed only once. |
| Claimed fire, no bound run yet | Committed claim is the original admission grant and reserves count/budget. Pause/resume does not revoke it. After process loss, consult the exact schedule:<plan>:<fire> native admission across the same Bot/team. Before its original deadline it remains pending; after expiry without original proof it becomes unknown, never a replay lease. |
| Original run exists or fire already finished | Reuse that original execution identity and result. No latest-run fallback, no new execution due to a missing callback. Result delivery uses its own stable event/attempt/publication receipt. |
| Definite native busy before admission | Exact no-original-invocation CAS retains a failed busy fire and refunds count once. Only recorded automatic limit stop may re-enable; explicit pause remains paused. Lost refund ACK is resolved by exact readback, not another write or execution. No retry of the same due. |
| Unknown/lost or uncertain external effect | Retain record/count/identity and report uncertainty through the existing opted-in path after live authorization. Do not refund/reclaim/reopen solely because a new process or generation exists. Read original action evidence before any future separately authorized action. |
| Summary pending, final not committed | Existing bounded summary mechanism may resume the original result delivery; it cannot execute the original task. Progress, empty output and admission are not publication receipts. |
| Final summary committed, acknowledgement missing | Read its original publication receipt and acknowledge it; do not send a second final reply. Account/Bot/contact/route/workspace authorization is rechecked. |
| Legacy non-opted history | Keep readable under trusted historical relation and permissions. No automatic source guessing, unknown notification enrollment, forced merge or new run link. |

## Recovery without losing newer history

Prefer stopping affected new writers and repairing/restarting a compatible181 binary on the current DB. Keep every message, fire, claim, invocation and publication receipt committed since upgrade. The full bundles let the operator recover exact source; they are not database backups.

Do not restore a pre-upgrade snapshot over the current database: that discards newer conversations and side-effect evidence. If storage itself requires recovery, use the platform's validated backup/WAL process to recover the latest recoverable committed state, then reconcile original external-action credentials read-only. This task did not verify that operational restore facility and cannot promise zero data loss from an arbitrary older backup.

Do not use `migrate down`, `force`, receipt deletion, call-count reset, generation reset or unknown replay as rollback. The included181 down is intentionally a preservation no-op; running it through a migration manager can still move the version marker and is NOT a supported production recovery procedure. Physical fields retained is not proof an older binary understands their semantics.

In particular,370 lacks schedule-origin delivery, due claims, refund accounting and generation checks. Running its scheduler on newly opted-in181 plans can bypass these protections. There is no separately certified370-compatible fallback consumer in this package. If returning UI/service code is unavoidable, keep scheduler writers stopped until a compatible recovery plan is reviewed; do not describe arbitrary binary downgrade as seamless. This is an explicit remaining operational boundary, not new development requested of the unified owner.

The isolated forward test used an actual180 clone with181 columns absent, applied only the candidate's forward CLI, and retained9 table hashes/335 messages and disabled legacy plan/log data. Earlier isolated181 down/up preservation evidence is retained only as a data-preservation test, never as the production recipe. No live migration, restore, plan creation, permissions change, paid call or deployment happened in this task.
