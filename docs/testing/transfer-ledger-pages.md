# Per-file operation ledger and bounded producer pages

This increment continues PR77's metadata-only registration. It removes the
production operation ledger's 128-entry / 2 MiB aggregate limit by normalizing
that same ledger into PostgreSQL rows. It does not create another task service,
identity system, queue root or deployment component.

## Storage and compatibility

Migration 0174 copies existing v1–v4 entry JSON, including receipt, unknown-create
and pending verification evidence, to `computer_delivery_entries`. The existing
operation contains only `{"version":5,"storage":"rows"}`. Entry schemas and
HTTP Create/PATCH/Complete/Commit/Resolve stay unchanged. Each mutation updates
one entry and the original operation revision CAS in one SQL statement inside
the existing authorization transaction. Receipt history is never removed to
make room. Cancellation is checked against that same operation for all upload
pages, even when no original producer declaration was embedded. Already committed
references remain queryable after cancellation.

The usual database upgrade performs the conversion; users do not migrate queues
or choose new locations. Stop all old writers and back up the database before
upgrading. The database trigger rejects old JSON writers, including creating a
new old-format ledger. A missing row table is an error, never an old-writer
fallback. Canonical 0001 and incremental 0174 represent the same schema. Migration
173 remains intact. No production migration was performed.

Rollback reconstructs the exact legacy entries only if they fit its 128-entry /
2 MiB representation and compatible proof version. Otherwise it fails in the
migration transaction, retaining all rows and RLS. In particular, a v1 download
proof cannot be relabeled as a newer local-publication proof. After growth,
rollback requires a coordinated backup restore; never delete history to force it.
Metadata uses real disk and database capacity, not infinite physical storage.

## Consumer contract

The Windows component's finite `enqueue-results` action reads a JSON object
with `version: 1` and `files` on stdin.

`files` contains 1–16 **objects** of the existing `deliverymeta.Closed` schema:
`version`, original `task`, `artifact`, `closed:true`, `sha256`, `size_bytes`.
The total message is bounded at 16 times the existing 16 KiB single-record
limit. All records in a page must have the same original Task; duplicate or
Windows case-alias source names within the page are rejected before mutation.
A page has no new ID. The established operation/attempt/generation/source
identity derives each delivery ID. Preserve the same canonical logical source
name across pages; a page boundary does not create a new source version.

Output is `{version:1,files:[{delivery_id,registered,associated,phase,
error_class,error_code?}]}` without credentials, roots, file bytes or raw errors.
Each registration is independently durable. Exit 1 may include successful files;
retry the original page, not a fresh business task. A crash can leave a prefix
registered. A source missing before initial registration is reported and must
remain in the producer's retained manifest; it has not been acknowledged as
registered. Once registered, replay does not require the original file to exist;
subsequent advancement reports missing/changed source without mixing versions.
No all-files copy, aggregate pending-byte admission gate or per-file token is
introduced. Existing finite `advance-results` / lifecycle wakeups do the transfer.

Authorized `computer_task_get` accepts optional `delivery_after` as a string.
`""` starts a page; it is mutually exclusive with `history_offset`. The response
is `{version:1,operation_id,revision,files:[{delivery_id,state,reference?}],
next_after,has_more}` with at most 16 files. Only committed entries expose a
reference. Cursors are internal append ordinals encoded as strings; keep them
opaque. Repeated reads are safe; late insertions are not missed because their
names sort before previous files. Updates retain their ordinal: refresh exact
in-flight IDs with existing Resolve, or rescan pages for current status.
`has_more:false` is not whole-task completion or a frozen snapshot. Permission
checks run again for every read. A file-scoped HTTP principal does not gain
operation-wide enumeration. No new public HTTP route is introduced.

The original `producer-context.json` remains immutable. `enqueue-results` now
validates its Task against that context before registering a page. Missing or
foreign context is an explicit error; a supplied operation ID is not sufficient.
Existing single-file declarations and v1 feedback remain readable.

For native producers, the envelope exports `MEMOH_DELIVERY_CONTEXT_FILE` and
`MEMOH_DELIVERY_PAGE_PREFIX`. An output producer may close additional bounded
`ClosedPage` files under the same configured source root: write a temporary
metadata file after closing the explicitly selected outputs, then publish it as
the prefix plus SHA256 of the exact written JSON bytes (no Go-specific
serialization requirement; `ProducerPageName(raw)` implements it).
The original finite `collect-result` recognizes these pages, validates the
original context, and uses the same registration path. It consumes only a fully
registered metadata page; partial/unknown pages remain. It does not scan arbitrary
output content or append unbounded names to the launch command. The unified
endpoint can instead stream the same objects to `enqueue-results`; users do not
perform either handoff manually. Herdr's native action API still does not forward
stdin, and no such capability or Agent modification is assumed.

Additional declarations survive in existing per-file queue metadata (original
Task, source reference, expected hash/size). No separate page index or ledger is
created. When a delivery is outside the initial page, feedback is reconstructed
only from its matching durable record, never from caller-supplied Job metadata.
The fixed `delivery-feedback.json` becomes a v2 bounded advisory containing `page`
(the current Plan); `plan_hash` continues to bind the original context. Task,
attempt, generation, root and file identity are checked. A v2 advisory cannot
claim delivery or supply a NAS reference. The Server supports both v1 and v2,
continues reading after the first page completes, and projects the current page
as `delivery.scope=page`. This is not a whole-producer aggregate. Authoritative
history remains the paged NAS ledger and exact Commit/Resolve. Update Server and
helper together for v2 advisory consumption; old strict readers reject it rather
than treating it as success. The queue/root/HTTP protocol and migration 174 are
unchanged by this follow-up.

The one fixed feedback file is a current bounded observation, not a retained
history of every local advisory. The queue retains each file's state and the NAS
ledger retains committed references. Unified endpoint lifecycle wakeups drain
registered work independently of conversation polling; this change does not
install that endpoint or manufacture automatic result selection for arbitrary
programs. The producer still explicitly closes the files it wants to deliver.

## Executed checks and limits

- One operation with **10,000 synthetic ledger metadata entries**, 626 bounded
  page reads, operation JSON **512 bytes**, **12.216954633 seconds**. Store reopen,
  original-ID Commit/Resolve, stale CAS rejection, repeated pages and a late
  lexically earlier insertion passed. These entries reference one underlying
  small test asset: this is not 10,000 network uploads or 10 TiB transfer.
- Actual finite CLI registration in fresh child processes: 18 small source files
  in 16+2 pages; missing file at first and middle positions; independent files
  continue; replay after restoring the source; replay after an acknowledged
  source disappears; same-ID hash conflict and oversized single page rejection.
- Production HTTPS Server routes + PostgreSQL + filesystem + actual CLI:
  three 17 MiB uploads across pages, child killed at the second durable PATCH,
  server/store reopened, both pages replayed. Each file keeps Create/Complete/
  Commit count 1, confirmed offsets are not resent, complete hashes and originals
  are retained. This is Linux isolated-process evidence, not a Windows PC/NAS
  acceptance, physical power loss or a bandwidth benchmark.
- Full existing `TestComputerDeliveryPostgres.*` regression with race detector
  passed in **53.156 seconds**, including ready-proof loss recovery, receipt
  turnover, HTTP partial outcomes, download Range/HEAD and upgrade from schema170.
- Additional focused checks cover revoked page reads, wrong-owner empty reads,
  malformed cursors, file-principal scope, mixed legacy proof rollback rejection,
  repeated migration and cancellation before future pages arrive.

The existing `files.max_upload_bytes` default 200 MiB, local download object-byte
budget, active HTTP/verification slots, complete-hash indexing cost and real
client integration are independent limits/dependencies and are not removed here.
This increment neither changes production configuration nor claims the original
10,000 × 1 GiB end-to-end goal has already been validated.

## Readable local evidence

Logs contain only isolated synthetic fixtures and test output. They were scanned
for credentials/URLs before inclusion; hashes below are of the unmodified bytes.
The scale and full-regression logs precede the final focused permission/rollback
assertions; the final focused PG and race logs cover those additions.

- [ledger-pages-scale-pg.txt](evidence/ledger-pages-scale-pg.txt), SHA256 `15618873c1412c60a0c34b5a8f640a33fc57e5c169f1bbf68c446e5b41db41f5`.
- [ledger-pages-full-pg.txt](evidence/ledger-pages-full-pg.txt), SHA256 `0782b0ab53c8b5ac9b3d9f2aab64f0012ab2c7acb54863f6eb76f3074abd37f1`.
- [ledger-pages-final-pg.txt](evidence/ledger-pages-final-pg.txt), SHA256 `e256b8f741248e80b0d1a36cfee2d0b1e0e9db163950e1ce32592892099549da`.
- [ledger-pages-final-race.txt](evidence/ledger-pages-final-race.txt), SHA256 `d35a65d259a0cf18ad9c37792df12769f858ae8938915527e9454919fd1b2e13`.

## Second-page closure regression and recovery

The 0f4 checkpoint admitted later pages but rejected their feedback against the
initial 16-file declaration. A regression reproduced `producer feedback
unavailable` for a durable second-page record before the fix. The fix preserves
that original context, validates appended closed pages, and consumes their
existing queue metadata for bound v2 advisory feedback.

- Real HTTPS Server/PG/filesystem and production CLI test now sends **18 files**
  (three 17 MiB + 123 byte files and fifteen 1027–1041 byte files). The original
  context contains only the first 16. Every invocation rejects unavailable
  feedback. Client process kill, NAS handler/store reopen, duplicate pages,
  source/context retention and original-ID replay pass. Every file has exactly
  one Create/Complete/Commit and matches full source/download SHA256. Local run:
  30.451s, including 26.28s in the batch scenario. This is Linux cloud evidence.
- Existing host collector processes 18 files from two closed pages, including
  first-source absence, independent progress and restoration. Latest targeted
  race run: 7.436s. NAS in this host-storage test is an explicit HTTPS protocol
  fixture; Windows execution must be read from the exact-head Native CI.
- The actual Bash producer envelope was exercised with synthetic Pi/Herdr tools
  writing 18 outputs and two closed pages. Original Task/context, fixed collector
  invocation and exclusive attempt guard remain; repeated launch does not rerun
  the synthetic Pi. This is not real installed Herdr acceptance.
- All affected packages passed race before the final collector-selection check:
  transferclient 55.569s, deliverymeta 1.138s, CLI 19.877s, computertask 27.854s,
  agenvo 23.291s. Later collector changes have the targeted race result above.
- Observer tests retain a committed first page while accepting a later pending
  page, recheck revocation/CAS, and wake transfer without replaying business. The
  store fixture now models the existing production SQL preservation of ledger
  data; no production database code or migration changed in this follow-up.

- [producer-second-page-red.txt](evidence/producer-second-page-red.txt), unmodified SHA256 `fb6dfe23b6efccc783fee46ab09a85d55101b99df70b06772c9c77f4975d9c77`.
- [producer-second-page-green.txt](evidence/producer-second-page-green.txt), unmodified SHA256 `cdebc1dd3c7dba67d954a22d7e0b37d94afffd0b61ef424552b9b4a752e13c11`.
- [producer-pages-18-pg.txt](evidence/producer-pages-18-pg.txt), unmodified SHA256 `0d961eede3fb64664bf0c8fd2770fade2d341fb286baf4d0f2b351e6022919b5`.
- [producer-pages-final-race.txt](evidence/producer-pages-final-race.txt), unmodified SHA256 `24cd52fb12d25e5e2e8be3339f5be1f22c77cacc0d05368d40ee68ad240d9405`.
- [producer-pages-collector-race.txt](evidence/producer-pages-collector-race.txt), unmodified SHA256 `f5b956c668f9e9a90880c8e2906d86e5c275ca80490ae1cca48226a87309ccc1`.

Final collector race after raw-JSON filename interoperability and partial-page
scheduling checks: **12.989s**, PASS. No final production code changed after this
check; incremental golangci-lint also exited 0.

- [producer-pages-collector-final.txt](evidence/producer-pages-collector-final.txt), SHA256 `f3cd9fc2f792f746362a0ae732cb5e0644f7b8b3edd4d2830a78919c388bc9fb`.
