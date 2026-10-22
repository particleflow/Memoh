# Queryable terminal queue history

Plugin 0.6 / queue locator v3 removes completed-history pressure from the active
DiskQueue catalog. It does not enlarge active job/byte limits or NativePublication.
Only an admitted, associated, NAS-committed `ready` record with the exact expected
reference and `PayloadReleased` may leave the active layout. Unknown, blocked,
canceled, unassociated or unreleased records retain their original evidence.
No original producer file, NAS object, operation ledger or native published object
is addressed by this operation.

## Location and capacity

The existing immutable locator remains the only root: Herdr resolves
`HERDR_PLUGIN_STATE_DIR`, and DiskQueue derives `deliveries-v1/history-v1/<bucket>`.
The bucket is the first SHA-256 byte of the logical delivery ID. There is no root
configuration, registry, database migration or additional service. History files
use the existing private-file and directory identity checks, Windows NTFS handles
and permanent job lock stripes. The complete framed QueueStatus is retained,
including task/device binding, attempt, operation, receipt, reference and seal.

Active enumeration and due selection do not scan history. Original-ID Inspect
and Admit check both layouts and reject conflicting copies or admissions. Producer
feedback reads history only for its at-most-16 declared logical file IDs. Same-ID
replay needs no source bytes; it does not create another upload or business task.

Budgets are finite: 256 buckets, each at most 1024 files and 1 MiB framed metadata
(256 MiB total maximum plus directory/allocation overhead). One record is at most
16,420 bytes. Distribution and record sizes can exhaust a bucket sooner; this is
explicit capacity backpressure, not a promise of 262,144 admissions. A full bucket
preserves both the terminal source journal and any partial destination. Complete
history is never evicted to make room. The existing 1024 active-record limit still
protects unknown and unfinished work. Long-term history export/removal is outside
this change; changing the budget/layout requires a compatible version transition.

## Migration and crash model

1. Stop every old writer (including idle processes holding an old queue handle).
   Save a complete raw state backup, including payloads, journals and lock files.
   The supplied component does not install itself or orchestrate this stop/backup.
2. Run the finite `migrate-history` action with the unchanged host locator and
   profile. Exact root, revision and limits must match. Normal opens refuse old
   locator v2 and never migrate or fall back silently.
3. Under admission and all worker locks, persist and sync the exact old locator
   in `locator-v2.backup`, then update only its version digit to v3 and sync again.
   Partial backup recovery requires an exact matching original. Corruption fails
   closed. Active workers produce `ErrBusy`, not a forced takeover.
4. Each finite maintenance/advance call archives at most 16 eligible old records;
   a newly confirmed release archives that record immediately. Hold admission
   and the permanent job stripe throughout. Write the full checksummed destination,
   sync it and its directory, then unlink only the original active journal and
   sync the active directory. Never delete or replace lock files.
5. If both records remain, every destination byte must equal the source frame.
   A matching prefix can be completed only from the intact original. A source-less
   incomplete history, malformed full frame or conflicting copy fails closed.
   A complete archived record can be replayed without allocating a new file.

The locator backup is not a substitute for the full pre-migration raw backup.
Once records move, never downgrade by editing a locator or starting an old writer.
Rollback requires a separately reviewed stopped-writer recovery that accounts for
post-backup activity; restoring an old snapshot over newer evidence is unsafe.

Tests retain a raw original-layout copy, cross 1024 with synthetic compact terminal
records, and exercise process exits around target sync/source unlink and version
migration. These are real queue/filesystem operations with synthetic acknowledgments,
not 1152 NAS transfers or physical power-cut testing. File/directory sync and the
existing NTFS platform contract remain the durability boundary; no absolute
physical-device power-loss guarantee is added.

NativePublication still owns an independent 128-record/retained-byte budget.
Its objects and journal layout are unchanged. Producer lists remain bounded at
16 files, and the default queue still uses 8 active jobs, 256 MiB and one worker.
