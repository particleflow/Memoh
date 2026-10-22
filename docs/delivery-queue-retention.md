# Confirmed queue payload retention

Finite Herdr actions reuse the existing resolved `STATE_DIR`, `DiskQueue` journal,
queue-wide admission lock and per-delivery lock. No background service, timer,
root configuration or task registry is added. After the NAS association port
confirms committed for the original reference, the finite advance/reconcile
releases the queue payload. Enqueue and advance-next/advance-results also recover
interrupted releases before selecting work. No extra credential or request is
needed to reclaim an already durably acknowledged result.

## Evidence and deletion boundary

Eligibility requires admitted + associated + association committed + ready,
with the exact delivery, operation, Bot, hash, size and MIME from the immutable
job. Upload completion/ready alone, a successful Pi result, cancel intent,
association unknown, blocked/retry exhaustion, and elapsed TTL are insufficient.
If commit won before cancellation, resolving the original NAS reference may
confirm delivery and allow release. Cancellation never retracts that result.

Only the job's queue-owned `.snapshot`, `.tree`, `.admission` and `.part` names
are removed through the existing storage binding. SourceRef is never used for
cleanup. User sources, NAS originals and the native publication store (published
download objects and publication receipts) are untouched. Unknown/canceled work
keeps its reservation and needs the existing explicit reconciliation/cancel/
retire policy. Automatic release must not be used to infer cancellation.

## Crash protocol and bounded metadata

1. Record a `release_pending` state with the durable committed ack (current
   queue record version 4; version 3 was introduced with payload release).
   Until payload deletion and directory sync complete, quota stays reserved.
2. Delete only queue caches and sync the queue directory.
3. Write one checksummed `released` frame to a bounded `.release` sibling and
   sync it and the directory. This is a compaction witness for the same journal.
4. Truncate/sync the original journal and write/sync the compact released frame.
   A restart can recover an empty/torn journal from the complete witness.
5. Remove the witness and sync the directory. With locator v3, persist the
   complete terminal journal into queryable history before removing its active
   copy; see [terminal history](delivery-terminal-history.md). Repeated release
   preserves the same record.

The compact journal retains the immutable job, snapshot seal metadata, original
receipt ID and exact reference. A repeated hook/enqueue with the same metadata
returns that record without reading the source or creating another upload.
Different content for the same ID remains rejected. Explicit retirement of a
confirmed result also preserves this receipt. The active queue's configured job
and byte limits exclude only `released` records. In-progress cleanup remains
conservatively counted. A released record has at most 16 KiB JSON plus its
36-byte checksum frame. The active catalog remains bounded at 1024 records.
Locator v3 gives confirmed released history a separate finite budget within the
same queue; older locator v2 counted retained records toward the active catalog.
Neither layout silently evicts deduplication proofs. History bucket exhaustion
preserves the source journal and returns capacity backpressure.

The six process-exit regression points cover intent, deletion, durable witness,
journal truncate, compact journal sync and witness removal. These model process
termination with the OS/filesystem intact, not arbitrary device failure or a
physical power-loss guarantee. Existing NTFS handle, private-directory,
filesystem identity, sharing and sync requirements remain in force.

## Compatibility and acceptance

Active record versions 2/3/4 remain readable; current writes use version 4.
Locator version 3 is a separate layout gate introduced for terminal history.
Stop all old queue writers and save the complete raw layout before the explicit
`migrate-history` action upgrades locator v2. Do not mix versions or edit a locator
back for rollback. There is no automatic downgrade, relocation or silent fallback;
the resolved root, revision and limits must still match exactly.

Queue tests use real local files/locks/process exit with an HTTPS protocol
fixture and synthetic association acknowledgement. The native workflow runs
these on Windows NTFS; successful Linux tests are not Windows evidence. Existing
Server/PostgreSQL producer tests and native PostgreSQL download-publication tests
remain separate integration combinations. No production credential, installed
Herdr route, real PC-to-NAS transfer or UI/human acceptance is implied.

The producer supports bounded multiple declarations while preserving these
per-file receipts and cleanup rules. A crash before a closed manifest exists
remains observably pending/unknown and never reruns Pi to guess an output.
