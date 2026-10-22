# Verified media ranges

This stage removes repeated whole-original scans from resumed downloads on the
existing canonical file provider. It adds no service, database table, persistent
index, credentials, storage root, or migration. Agent runtime/contextview are
unchanged. The existing HTTP routes, Range/If-Range/HEAD semantics, content hash,
Bot authorization and default upload limit remain unchanged.

## Measured baseline

Main `75a70fe961808ed5ec2284767cb92ea85f30890f` hashes the original in
`localfs.Originals.Open`, then hashes it again in `media.Service.OpenSeekable`.
Every reconnect repeats both scans. Linux `/proc/self/io` measurements of actual
read syscall bytes, requesting 64 KiB near the end of a synthetic original:

| Original | Baseline first read | Baseline resumed read | New cold read | New resumed read |
| --- | ---: | ---: | ---: | ---: |
| 200 MiB | 419,496,049 | 419,496,050 | 210,763,889 | 1,048,689 |
| 512 MiB | 1,073,807,476 | 1,073,807,477 | 537,919,604 | 1,048,692 |

These counts include small accounting/metadata overhead. They measure filesystem
read calls including page-cache hits, not physical disk traffic or NAS throughput.
A fresh provider again performs one complete verification; its next resumed read
returns to about 1 MiB. Reproduce with an isolated process:

```sh
MEMOH_RANGE_VOLUME=1 go test ./internal/media -run '^TestSeekableReadVolume$' -count=1 -v
```

## Integrity and authorization boundaries

1. **Write admission stays complete.** Ordinary `Originals.Open`, Put, final
   upload SHA, dedup admission and durability barriers keep their prior behavior.
   A download optimization does not acknowledge storage durability or allow a
   cached partial read to replace full verification during write admission.
2. **Cold range open establishes trust.** `OpenVerifiedRange` pins a descriptor,
   verifies its complete SHA against the requested content hash, and derives
   1 MiB block SHA-256 digests from those same bytes. File identity/change metadata
   must match before and after the scan. Cancellation publishes no readable index;
   bounded unfinished work can be resumed as described below. Failed integrity
   or I/O checks discard unfinished work.
3. **Warm range reads use guarded evidence.** Cached evidence is bound to the
   provider instance, full Bot routing key, device, inode, size, mode, mtime and
   ctime. File descriptors must be regular and single-linked; final symlinks are
   rejected. Rooted path resolution and Bot-root checks remain in force.
   A changed identity invalidates the cache and forces full verification.
   ctime also detects same-size modification with restored mtime in the tests.
   Timestamps are not cryptographic evidence: coarse clock resolution can hide
   rapid metadata changes, so checking the requested block remains mandatory.
4. **Requested bytes are verified again.** Each newly read block is hashed in a
   bounded private buffer before any of its bytes reach the caller. Metadata is
   checked before reads and after filling a block. A verified buffer can satisfy
   later reads from the same block. Read-time bit flips are rejected even when
   metadata has not changed. This also closes the old gap between a pre-scan and
   an unchecked later file read.
5. **Authorization is never cached.** The HTTP handler still authenticates and
   checks current Bot permissions before opening media. Cross-Bot references,
   missing credentials and revoked access remain rejected with a warm cache.
   Media accepts the optional verified-range contract only when its content hash
   matches the resolved asset. Other providers retain full verification/spooling.

This is not a claim that chmod, an inode or a filename is an immutable seal.
The cache trusts a completed SHA computation, kernel change metadata and block
digests together. There is no persisted integrity authority to trust after a
restart. The operator-owned originals root must remain inaccessible to workspace
writers as before; a privileged adversary able to falsify kernel metadata or
modify Server memory is outside this boundary.

## Bounded costs and fail-closed behavior

- Cached digests have a 16 MiB budget and at most 128 entries (plus small cache
  bookkeeping). Each index is at most 8 MiB: 256 GiB at 1 MiB/block. Larger files
  keep the previous full-verification path; their admission limit is not changed.
- Each active reader allocates one 1 MiB data buffer on demand. A cold verifier
  uses one scan buffer plus its bounded index. Active streams may retain an index
  evicted from the shared cache; these are per-stream bounds, not a global RSS
  or concurrent-download admission policy.
- Same-key cold verification is coalesced through context-aware striped locks.
  Waiting requests and scans honor cancellation between bounded reads. The code
  cannot interrupt a kernel read blocked inside a stalled filesystem.
- Only Linux ext/btrfs/xfs/zfs/f2fs/tmpfs/overlay are eligible. Unknown, network
  and FUSE filesystems, or other OSes, keep complete-stream verification.
- External changes that alter the change stamp invalidate it and require a
  complete recheck. Corruption that does not alter observable metadata (including
  silent bit flips or a timestamp-resolution collision) is discovered when its
  block is requested; a warm partial read does not promise a fresh scrub of all
  unrequested bytes. Returned blocks still match the fully verified original.
- Cold verification errors occur before response success. A mutation or block
  error during an already-started HTTP response stops supplying bytes; the
  response can be truncated after headers. Consumers must check byte counts and
  the final content hash and must not treat a status code alone as completion.
- Cached trust does not survive eviction, a new provider instance or restart.
  The first verification can still exceed a two-minute budget on sufficiently
  large/slow storage. Requests can now resume unfinished verification within the
  same process; no single cold-open latency or retry-count guarantee is made.

## Compatibility and storage location

Existing originals need no rewrite, sidecar, eager scan, directory move or index
of Public. Their first range read builds an in-memory index; failure leaves the
original untouched. Completed indexes contain only digests; unfinished SHA state
can retain up to one SHA-256 block of input in process memory. No sidecar or disk
state is written. Rollback discards only process-local optimization state;
original bytes and stable references are unchanged.

The existing resolved `cfg.Files.OriginalsRoot` remains the only location input.
Replacing an original or opening a different mounted object cannot reuse a cache
entry with a different file identity. A new process verifies bytes again instead
of loading a disk cache. This is content identity validation, not a certificate
that the operator mounted the intended volume: the existing absent-root/legacy
import policy and lack of a durable volume identity marker remain separate
deployment concerns. Canonical integrity errors never select a legacy copy;
only the existing missing-object import policy remains available.

PR18's result-Commit wait behavior is independent. This stage does not modify
its authoring area, introduce a generic registry, or change task scheduling.

## Validation scope

Automated checks cover warm/cold/recreated-provider byte counts, full SHA and
cross-block seek behavior, external in-place changes with restored mtime,
file/root replacement, hardlinks, read-time bit flips without metadata changes,
concurrent cold opens, cancellation/lock release, LRU/count/byte limits, and
legacy import without masking canonical corruption. A real HTTP fixture using
NewServer/JWT checks warm-cache reads, anonymous/cross-Bot access and permission
revocation. These are synthetic cloud tests; production NAS and Windows
throughput, physical power loss and persistent sealed metadata are not tested.

The failed-block-fill test also checks that seeking back cannot reuse a buffer
overwritten by a failed read. Seek errors preserve their Go cause while hiding
filesystem paths from standard-library HTTP error text.

The rebuilt native Server, Vite and isolated PostgreSQL ran at loopback ports
18080/18082. Actual HTTP 200/512 MiB transfers were interrupted after 1 MiB and
resumed with Range; the complete reconstructed SHA matched both before and
after restarting the owned Server. RSS values in the logs are samples, not a
global peak or a load-test admission guarantee. Reading the separate Server's
`/proc/<pid>/io` was denied by the cloud environment; no privilege escalation or
alternate access was attempted. Consequently HTTP logs do not invent a Server
I/O count; the isolated process measurement above establishes read-volume cost.

Real Chromium completed login, selected a synthetic text attachment and showed
its 501-line pending preview. An authenticated browser request spanning a block
boundary returned exact bytes with 206 and the expected Content-Range, with no
page errors. No chat send or model turn was performed. The
[inspected screenshot](validation/media-ranges/attachment-selected.png) proves
that UI flow; the HTTP and SHA logs provide separate backend evidence.

Relevant storage/media/handlers/server/core race tests, targeted lint and the
official Agent core boundary check passed. Linux 386, Windows amd64 and Darwin
arm64 storage test binaries were cross-compiled only, not executed. Existing
ordinary Open and durability fault regressions remain in the race suite.

Test-harness corrections were recorded rather than treated as product failures:
the first upload check expected 200 instead of the API's successful 201 (the
created file was reused); the first legacy fixture lacked prefix-list support
and was corrected to test the storage contract directly; browser network-idle
waiting was replaced with DOM/attachment readiness because the app keeps live
connections. No production configuration or business code was weakened for
these checks. See the [validation evidence](validation/media-ranges/README.md).

## Incremental cold-scan continuation

The follow-up to PR #21 retains unfinished verification in the existing LRU,
separate from a readable `rangeIndex`. It records the same file stamp, block
digests, completed-block cursor and incremental SHA-256 state. Only the verifier
holding the key's existing stripe can remove and own this mutable work. A later
request opens and validates the file again, checks the stamp, then continues at
the next complete block. The existing HTTP handler authorizes that new request
before reaching storage. A canceled waiter cannot steal an active verifier's work.

On cancellation, a final descriptor stat must still match before progress is
retained. I/O errors, hash mismatches, changed stamps and invalid file/link types
never turn partial work into readable evidence. No Range reader is returned until
the aggregate SHA matches the requested content hash and the final stamp matches.
Every subsequently returned block is still checked against that completed index.
An old prefix combined with a new suffix cannot bypass either the final SHA or
the per-read block checks. Kernel timestamp limitations remain as documented above.

Unfinished and completed entries share the same 16 MiB digest/cache accounting
budget and 128-entry LRU. A paused entry retains no descriptor or data buffer.
Active verification remains limited by the 32 existing stripes, each holding at
most an 8 MiB index and 1 MiB scan buffer; active reader/index bounds above still
apply. There is no timer, worker, detached scan or new service. Eviction/restart
loses unfinished progress and forces complete verification. File, Bot or root
replacement does not inherit a mismatching file identity's progress.

This improves progress across bounded requests, not completion under arbitrary
limits. A process restart, cache contention, repeated file mutation, a stalled
kernel call, or a caller budget too short to finish even one block can still
prevent progress. A file needing more scan attempts than the client's finite
retry budget can still exhaust it. Transfer scheduling, HTTP 499 classification
and blocked-queue reconciliation remain owned by the transfer client workstream.
Browser cache policy is unchanged: authorization on new Server requests cannot
revoke bytes already held by a client.

The [cold-scan evidence](validation/cold-scan-resume/README.md) records the
deterministic deadline reproduction, bounded continuation, fault/concurrency
checks and synthetic large-file measurements. No production timeout, upload
limit, mount option or integrity requirement is weakened.
