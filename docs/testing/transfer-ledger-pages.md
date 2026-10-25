# Per-file operation ledger and bounded producer pages

## Scope correction: withdrawn additional-declaration experiment

The additional producer-declaration experiment at
`bb582ec1680c4e23552857af7fd1d698cb555dcf` is withdrawn by a normal successor
commit, without rewriting history. Executable code, existing tests and the
plugin contract are restored byte-for-byte to
`0f4a4ae8b615098f8a4f34cf187160c37c689996` (component version 0.10.0).
Metadata registration and the per-delivery ledger described below remain.

The old feedback rejection was reproduced only after a fixture explicitly
registered an ID outside the initial declaration. The original producer closes
only declared files; a failure in its normal workflow was not reproduced.
The 18-file experiment constructed extra declarations itself. Its successful
local run therefore does not establish that a new producer protocol is needed.
Task declarations, task-result feedback, generic file-byte transfer and UI
lists are different boundaries. A declaration or ledger count alone does not
prove a generic transfer limit or a loss of accessible committed files.

Removed: the additional closed-page filename/prefix and environment variable,
collector entry point, feedback v2/page projection, context requirement added
to the existing stdin registration action, and their experimental tests.
Existing v1 feedback, original producer flow, stdin registration and the
three-file process-recovery scenario are restored. No Windows or Harmony
consumer change, new migration, deployment or release accompanies this revert.

The six `producer-pages-*` / `producer-second-page-*` logs added at bb582ec
remain as historical experiment evidence, not validation of the restored head.
Their code and explanatory document are still available at that immutable
commit. [The CI failure excerpt](evidence/producer-pages-withdrawn-ci-failure.txt)
also remains explicit: Go run 40100974269 failed in the 18-file scenario with
`original interrupted delivery did not resume`; subsequent PostgreSQL checks
were skipped. Native run 40100974370, Docker run 40100974322 and contribution
run 40100974332 passed. That mixed outcome is not rewritten as all-green.
The failure was not diagnosed as a production defect, and the withdrawn
scenario is not being adjusted or rerun to obtain a passing result.

## Retained increment

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

The old single-file action and Pi's one bounded `artifact`/`artifacts` declaration
remain compatible. Their aggregate describes only that declared page. A unified
endpoint can use the new page interface; its automatic page production/consumption
and arbitrary-length Pi output enumeration are still separate integration work.
Herdr's existing native action API does not forward arbitrary stdin; this change
does not invent that capability or modify official Agent code.

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

## Unified candidate HTTP projection

The follow-on candidate exposes the same bounded ledger through the existing
session task HTTP namespace; see [the client contract and integration evidence](unified-ledger174/README.md).
The original MCP query and transfer credential endpoints are unchanged. The new
HTTP read route does not provide file download authority or automatically add a
file browser to Web/phone clients.
