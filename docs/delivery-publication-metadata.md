# Bounded native publication metadata

Plugin 0.7 retains NativePublication's original logical references, object paths,
NTFS file identities, task authorization callback and prepared/published/fenced
state machine. The change is local metadata layout and explicit maintenance; it
adds no service, transfer protocol, credential or configurable second root.

## Budgets and lookup

The existing queue locator resolves `publications-v1`. Final `.object` files stay
there. Publication locator **v2** derives `records-v2/<bucket>` for journals and
usage caches. All 64 buckets and their usage records exist before the v2 locator
is committed; missing buckets, a missing catalog or missing usage records are
errors, never an unused namespace. Only new-store initialization or closed v0
migration may seed empty buckets. An interrupted initial creation that has not
committed its locator remains explicitly unavailable for inspection; normal opens
do not recreate missing state. The bucket is the low six bits of the first SHA-256 byte of the
logical delivery ID. A record retains its full original checksummed journal,
reference, state and FileID; published history and cancellation tombstones are
never evicted. Resolve/OpenPublished use the original ID and exact reference.

The versioned defaults are 64 buckets, at most 128 journals per bucket, at most
64 KiB per journal. Thus the layout admits at most 8192 records and at most
512 MiB journal metadata, plus at most 2 MiB for usage records and dirty witnesses
(each bounded to 16 KiB), locators and
filesystem allocation overhead. Skew can fill a bucket earlier; the next distinct
record then receives capacity backpressure. Existing-ID replay still works.
These are explicit defaults encoded by locator v2, not a promise of infinite
history or a silent quota increase for an old locator. Changing them requires a
compatible locator transition; no independent config file or root is introduced.

**Object bytes remain separately limited by the existing immutable QueueLimits.Bytes**
(default CLI 256 MiB). Prepared and published bytes count; fenced reservations do
not. Queue Retire and payload/history reclamation do not remove publication files
or their accounting. This change cannot unblock an actually full object-byte
budget. Ownership transfer, object deletion, history eviction and unlimited large
file throughput are outside this change.

Each mutation holds the same permanent `publication.lock`. A finite scan pins
and validates the catalog directory once, then checks each bucket; no identity,
authorization or usage is cached across invocations. Only a brand-new store before
its first locator commit may seed empty usage directly. Existing-state mutation
and migration still use the witness protocol; a fresh initialization attempt on
an existing catalog fails without changing it. Capacity reads at most
64 small usage summaries instead of replaying all historical journals. A mutation
rebuilds its own bucket (at most 128 journals / 8 MiB). Summaries are internal,
rebuildable accounting caches, never another task or delivery authority. Complete
journals remain authoritative for original-ID results and file ownership.

## Dirty-cache crash protocol

1. Persist the previous acknowledged usage and sorted journal membership in a
   private checksummed `usage.dirty` witness and sync its directory before creating
   or appending a journal. Never replace an existing witness during recovery.
2. Append/sync the original checksummed state transition and sync the directory.
3. Validate that bucket's journals and reconstruct count, reserved object bytes,
   metadata bytes and membership. Every previously acknowledged key in the witness
   must remain present; a lost journal fails explicitly rather than shrinking history. Write/sync its checksummed usage record, bound to the bucket
   directory identity, and sync the directory.
4. Remove the dirty marker and sync the directory before returning success.

A crash while the witness exists reconstructs the summary from complete journal
states only after verifying that its prior membership has not shrunk. A missing
journal named by a clean summary cannot become an absent delivery, and an untracked
journal cannot be accepted as a valid result. Torn usage can be rebuilt with the
intact witness; damaged witnesses are retained and require inspection. Existing torn-tail recovery may retain the preceding complete state;
full-frame corruption fails closed. An empty/unknown first journal is preserved
and cannot be interpreted as a free slot or successful cancellation. A corrupt
summary without a dirty marker also fails explicitly. A full bucket, byte budget,
failed write or corruption never removes history or final objects to gain space.
The publication and metadata locks are not removed or replaced.

## Explicit upgrade

Queue locator v3 (plugin 0.6 terminal history) is a prerequisite. Publication
locator version is a different gate:

- v1: original flat, global 128-record publication layout;
- v0: closed maintenance state, rejected by ordinary old/new opens;
- v2: bucketed metadata with the defaults above.

Stop **all** old publication/queue writers, including idle handles, and retain a
complete raw state backup. The existing admission, worker and publication locks reject an active caller
with busy; they cannot revoke
an idle old process. Never mix old and new binaries or edit a version for rollback.

Invoke the finite fixed `migrate-publications` CLI/plugin action under the same
Herdr state locator/profile. It needs no NAS credential or source path. It saves
and syncs the exact old locator as `locator-v1.backup`, then durably changes v1 to
v0. Ordinary actions remain unavailable during migration.

A call copies at most 16 old journals. Every destination must exactly equal the
original raw journal; matching partial prefixes can be completed only while the
intact original remains. Conflicting copies fail and both are retained. Only once
all destinations and their usage are durable does a call remove up to 16 old
journal paths. It never moves, rewrites or deletes an object. Once the old journal
paths are exhausted, it syncs the locator transition to v2. Repeated invocations
resume the same journals and IDs, without another task or download.

CLI output is `{"publication_layout":0,"migration":"pending"}` until complete,
then `{"publication_layout":2,"migration":"complete"}`. Pending requires another
finite maintenance invocation; ordinary advance must not bypass it. Errors use
the existing lifecycle classification. Complete means metadata migration, not a
new integrity attestation of every retained object. Normal authorized Resolve and
OpenPublished still validate file identity, size and hash. The locator backup
alone is not a full rollback; post-backup evidence must not be overwritten.

## Test and product boundary

Fixtures preserve original-layout copies and test exact IDs, prepared/published/
fenced state, byte limits, full buckets, corruption, dual copies, repeated calls,
lock identity and process exits around migration and cache synchronization. More
than 128 synthetic records exercise the layout; they are not that many real NAS
transfers. Actual Windows execution, packaged CLI verification and Windows+PG
coverage are reported by the fixed-head native workflow. Linux metadata fixtures
and cross-compilation alone are not Windows execution evidence.

No physical power cut, production PC/NAS route, new trust configuration, retained
object cleanup, sustained large-file throughput or final endpoint-product acceptance
is implied. Hardware that does not honor flush completion remains outside the
existing NTFS durability model.
