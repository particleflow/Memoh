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

1. Record a version 3 `release_pending` state with the durable committed ack.
   Until payload deletion and directory sync complete, quota stays reserved.
2. Delete only queue caches and sync the queue directory.
3. Write one checksummed `released` frame to a bounded `.release` sibling and
   sync it and the directory. This is a compaction witness for the same journal.
4. Truncate/sync the original journal and write/sync the compact released frame.
   A restart can recover an empty/torn journal from the complete witness.
5. Remove the witness and sync the directory. Repeated release is a no-op.

The compact journal retains the immutable job, snapshot seal metadata, original
receipt ID and exact reference. A repeated hook/enqueue with the same metadata
returns that record without reading the source or creating another upload.
Different content for the same ID remains rejected. Explicit retirement of a
confirmed result also preserves this receipt. The active queue's configured job
and byte limits exclude only `released` records. In-progress cleanup remains
conservatively counted. A released record has at most 16 KiB JSON plus its
36-byte checksum frame. A queue admits at most 1024 total active + retained
records; reaching that bound fails explicitly rather than dropping deduplication
proofs. This is bounded capacity recovery, not unlimited historical retention.

The six process-exit regression points cover intent, deletion, durable witness,
journal truncate, compact journal sync and witness removal. These model process
termination with the OS/filesystem intact, not arbitrary device failure or a
physical power-loss guarantee. Existing NTFS handle, private-directory,
filesystem identity, sharing and sync requirements remain in force.

## Compatibility and acceptance

Version 2 active records remain readable. Version 3 is used only once durable
commit is recorded; old strict readers reject it. Stop old queue writers before
using this version; do not mix versions or roll back a state directory containing
version 3 records. There is no automatic downgrade, directory relocation or
silent fallback. Existing immutable locator/config bindings remain unchanged.

Queue tests use real local files/locks/process exit with an HTTPS protocol
fixture and synthetic association acknowledgement. The native workflow runs
these on Windows NTFS; successful Linux tests are not Windows evidence. Existing
Server/PostgreSQL producer tests and native PostgreSQL download-publication tests
remain separate integration combinations. No production credential, installed
Herdr route, real PC-to-NAS transfer or UI/human acceptance is implied.

The producer supports bounded multiple declarations while preserving these
per-file receipts and cleanup rules. A crash before a closed manifest exists
remains observably pending/unknown and never reruns Pi to guess an output.
