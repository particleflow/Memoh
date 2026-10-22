# Task upload receipt lifecycle

The generic `/bots/{bot_id}/media/uploads` contract retains its fixed 24-hour
receipt lifetime from [resumable uploads](resumable-uploads.md). Generic expired
ready receipt GC never deletes the published original. Task transfer routes
have a separate retention policy under the same UploadStore, root and quotas.

## Task evidence and progress

Authorized task Create uses `UploadStore.CreateTransfer`. New private receipts
record `progress_lease: 2`. The public Upload response is unchanged; no new client
flag, credential, endpoint, table or root is introduced. Only positive verified
append progress renews `expires_at`, atomically with the synced offset. First
Complete also renews it; GET, rejected chunks, failed or repeated Complete and
cancellation do not. The timestamp does not move backwards.

For task receipts the deadline is progress metadata, **not deletion authority**.
Startup, hourly and pre-Create Sweep retain uploading, ready and canceled task
records, including idle/unknown outcomes beyond the deadline. Resume uses the
original ID and fresh current authorization. An active writer holds the existing
receipt lock. Prior `progress_lease: 1` records are upgraded to 2 under that lock
by the same Sweep path without changing offset, deadline or payload. Records
without a policy remain generic legacy records; their history cannot safely be
inferred from a hash or filename. Missing legacy evidence is not recreated.

The original 128 receipt, 16 active, four per-owner and byte/file/chunk limits
remain. Unknown or unconfirmed records can cause explicit backpressure. They
are never evicted to admit new work. Terminal payload leftovers after a crash
still count toward reserved bytes until safely removed.

## Committed retirement

The existing task Commit/Resolve response path obtains
`CommittedTransferReady` from the authorized operation CAS/ledger. This must be
the exact server-captured ready proof **and** a durable committed association.
Ready alone, client assertions, matching original bytes, canceled or pending
outcomes do not authorize retirement. A committed legacy entry without proof
remains queryable, but cannot authorize filesystem cleanup.

`UploadStore.RetireTaskReceipt` is an internal trusted adapter port, not an HTTP
proof-submission API. It acquires admission then the permanent receipt lock,
checks owner/Bot, upload ID, ready state, size, offset, hash, MIME and timestamp,
then removes only that receipt's temporary payload/temp/JSON, metadata last,
and syncs the directory. It never touches originals, operation ledger or locks.
The 65 existing lock stripes remain bounded and are never removed, preventing
old handles from creating a second lock. In-flight Append/Complete blocks
retirement; later stale writes find no receipt and cannot resurrect it.

Filesystem retirement follows DB commitment. A failed unlink/sync is an error,
not an acknowledged cleanup; the original authorized Resolve retries it. If
metadata is already absent, unexplained payload/temp files cause failure;
otherwise a fresh directory sync reconciles a lost final acknowledgment.
Corruption and owner/ref mismatch fail without deleting evidence. Response-time
identity revalidation still applies. The authorization snapshot is not a lease
that freezes grants throughout filesystem IO; a later revoke denies subsequent
requests even if safe cleanup of an already committed receipt occurred.

After retirement, task Get, repeated Create/Complete and Commit resolve the
original exact ID/ref from the existing ledger proof. They do not create a new
receipt or business operation. Local unconfirmed snapshots remain protected by
the separate queue rules. Published originals are not pinned by this protocol.

## Upgrade and scope

Run all writers on the same version. Older lease-aware readers reject policy 2;
older writers that predate policies are unsupported on this root. Do not mix
writers or downgrade while task-held evidence remains. No SQL migration,
production directory/configuration change or authentication enablement occurs.
Rollback requires preserving the ledger and raw receipts, not rewriting policy
numbers to let an old sweeper discard them.

This NAS increment removes completed temporary receipt pressure, not all scale
limits. Windows terminal journal history (1024), native publication object/byte
capacity, 1–16 producer files, unknown Create and original/ledger storage growth
remain distinct limits. Removing user-owned published objects or unknown
records is not a capacity solution. Large-file throughput, many-file long runs
and physical power loss are separate acceptance work.

## Verification

Storage tests cycle 391 tiny synthetic files across store reopenings, check
bounded staging entries/bytes, original contents, repeated retirement and stale
write rejection. Separate fixtures fill 128 unconfirmed ready receipts and
confirm the 129th remains rejected after TTL, without eviction. Deterministic
Append/Sweep/retirement lock tests cover crossing the deadline while a writer is
paused. Owner/hash mismatch, corrupted metadata, ENOSPC and lost directory-sync
acknowledgment fail safely. Generic TTL and original hash checks remain covered.

The formal-migration PostgreSQL integration uses the real stable device registry,
HTTPS task handlers and async validator across two 12-file batches by default.
`MEMOH_TEST_RECEIPT_TURNOVER_EXTENDED=1` runs twelve 12-file batches under an
explicit three-minute test timeout; this is a fixture switch, not product
configuration. It checks
original-ID Create/Complete/Commit replay after retirement, server/store restart,
lost Complete acknowledgment, revocation, cancellation and corrupted cleanup
with an exact synthetic backup. These tests do not connect to a real PC/NAS or
perform physical power cuts. Native Windows and CLI evidence is reported
separately; fixture composition is not production acceptance.
