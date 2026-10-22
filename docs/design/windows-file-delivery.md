# Windows file delivery: frozen client boundary

Integration update (2026-10-22): PR #8 is merged at
`eeb1fdabf50ec94d48f08302c9da3f5b3811293e`; its tree matches `a6149e63`.
The original checkpoint below is retained as history. The finite Herdr action,
disk queue implementation, source references and explicit Windows/auth blockers
are recorded in [the integration handoff](../validation/windows-file-delivery/queue-integration.md).
No production host, identity, TLS or directory configuration has been changed.

Status: isolated client prototype, not connected to a Windows host or production.
Base: `a819c75ed94ae06a3a8196e9416302b18d8dd34f`.
Server contract: PR #8 at `2bf79bbb8a2be4f1f08eefcadde850b2361e29c4`,
[`docs/resumable-uploads.md`](https://github.com/particleflow/Memoh/blob/2bf79bbb8a2be4f1f08eefcadde850b2361e29c4/docs/resumable-uploads.md).
PR #8 owns the server implementation and its rebase. This branch neither copies
nor modifies that implementation, generated SDK, Agent, runtime or contextview.

## First checkpoint: evidence and missing facts

| Verified source | Consequence |
| --- | --- |
| `internal/handlers/media_upload.go`, `media.Service`, `storage/providers/localfs/originals.go` | Reuse ingest, canonical per-Bot hashes and originals; no second filesystem API. |
| Frozen PR #8 `media/uploads.go`, `handlers/media_resumable.go` | Sequential 1 byte–8 MiB chunks, per-chunk/final SHA-256, durable receipts, 24h TTL, idempotent complete, cancel, current Bot permission checks. Default file limit stays 200 MiB. |
| `handlers/message.go:ServeMedia`, `media_range_test.go` | Authenticated GET/HEAD, Range, If-Range and hash ETag already exist. Avoid public-image/preview paths and full-buffer workspace FSRead. |
| `computertask/source.go`, `service.go` | Existing artifact action saves a bounded terminal snapshot. It does not register arbitrary Windows output files or a multi-result delivery manifest. |
| `auth/jwt.go`, `docs/design/agenvo-execution-target.md` | Memoh account/chat authentication and Agenvo command authorization are different. No task-scoped PC upload credential or encrypted PC↔NAS route is demonstrated. |

Still unverified: current authorized Windows host's language and durable store;
stable-source/NTFS ACL and reparse-point implementation; approved TLS/private
tunnel and Memoh credential; operation/result registration endpoint; actual NAS
mount lock/fsync behavior, quota and free space; real Windows/NAS throughput.
The user-described IPv6 HTTP deployment is not encrypted. Source inspection
does not establish the live configuration. No real PC or NAS is contacted.

## Reuse and single source of data locations

Server configuration parsing, `[files].originals_root` (including its existing
environment override), `provideMediaStorage`, and `media.Service` remain the
only original-data locator. The operator chooses the existing dedicated originals
subdirectory within the agreed NAS Public mount; never configure all of Public
as the originals root. Existing Media and Docker/config locations stay intact.
The transfer client receives an API origin from the host's existing connection
configuration, opaque snapshot IDs, and already-opened download staging handles.
It has no root-directory setting, path builder, directory scan or storage registry.

The Windows host must supply its existing durable transaction/locking facility
for the queue and its existing centrally resolved, ACL-protected staging area
for snapshots. Queue metadata, temporary file bytes, logs, sessions, credentials
and user configuration retain separate ownership/permissions even if resolved
by one configuration service. Reuse that service, not a universal data directory.
Host identity and locator revision are bound when a job is admitted. Relocation
requires a versioned migration under the host's exclusive job lock, verification
of both old and new bytes, atomic metadata switch, and read/write tests. Fail on
a stale locator revision; never fall back silently to the old directory.
No such cross-module migration or production path change is in this branch.

The parent storage audit identified existing gaps at the base SHA:
`config.resolvePaths` does not resolve relative `Files.OriginalsRoot`; an empty
root enables legacy fallback; media spool creation can use the system temp
directory; Pi attempt paths are assembled in two places. These are not solved
by this adapter and remain separately coordinated changes. A configured storage
failure must not be interpreted as permission to use a legacy directory.

## Minimal integration and durable queue contract

`internal/transferclient` is an unconnected, standard-library client adapter.
There is no CLI, daemon, scheduler, credential issuer or new server route.
An existing Windows host calls one bounded `Advance` at a time and persists the
provided Job through its own durable store. Before calling it, that host must
hold a cross-process exclusive job lock for the whole call. A lease alone is not
sufficient unless fenced across network effects. The `Save` callback must commit
atomically and durably before returning and must not log paths, secrets or bytes.
In-memory callbacks are test fixtures, not a production queue implementation.

1. Producer closes an output and asks the host to admit it under a stable
   delivery key `(owner, Bot, target, operation, artifact ordinal/version)`.
   Unique insertion prevents duplicate results. Bind the target fingerprint,
   current grant, locator version, expected size, SHA-256 and MIME. Do not put
   private host paths in operation JSON or model context.
2. Host freezes an immutable snapshot before admitting the job. On Windows use
   an opened handle with write/delete sharing denied while copying, or an
   equivalent existing snapshot facility. Check file identity/length before and
   after, flush the private snapshot, and hash it with bounded memory. A mutable
   path plus size/mtime is not a stable snapshot. A changed source is a new
   artifact version; never reuse its old receipt/hash.
3. Host persists the queue record and applies count/reserved-byte/free-space
   admission limits before starting network work. The adapter detects PR #8
   support and preserves its limit. Each worker is sequential; start at two
   workers per identity (below PR #8's four active uploads/identity), bounded by
   host-wide byte and worker budgets. Use the existing host executor, not a new
   persistent component. Windows host selection/store wiring remains a blocker.
4. Persist `creating` before POST. A lost response or crash before saving the
   returned receipt becomes `create_unknown`: no blind retry. PR #8 has no
   create-idempotency key or receipt lookup by client key. Reconcile explicitly
   or wait beyond the possible receipt's 24h lifetime before readmitting; keep
   the snapshot. A received 429 is a definite rejection and may back off.
5. For a known receipt, GET before every new PATCH/complete attempt. Trust only
   server-confirmed offsets, including rollback after a restart. Never advance
   on timeout or connection loss. Bounded exponential jitter/backoff is stored
   with the job; the host schedules later without a sleeping worker. 401/403
   pause for authorization; 404 is unavailable/expired/hidden, not permission to
   recreate; 400/hash mismatch and 413 stop. 429 and transient server/network
   errors retry with a cap. Save failure stops further effects immediately.
6. Only `ready` with the expected full hash, size and MIME is locally deliverable.
   Persist that reference before returning it. Completion response loss recovers
   by GET/complete with the same receipt. Receipt expiry never deletes originals.
7. Host's existing durable outbox registers the ready reference under the same
   delivery key. This endpoint is not present yet: the parent must coordinate a
   scoped `computertask`/handler extension, outside Agent code. It must verify
   owner/Bot/session/operation/target fingerprint and current grant, resolve the
   hash in media, and return the same association on repeated registration.
   Treat wrong-task hash reuse as unauthorized; hash equality alone is not task
   authority. The current adapter does not claim server task binding.
   Prefer the existing `ComputerOperationStore` receipt/revision transaction for
   bounded associations and a media-backed manifest for large result batches;
   do not invent a separate operation or data-location registry. Exact schema,
   pagination and authorization changes require parent coordination first.
8. Chat gets only a bounded manifest/reference (IDs, hash, size, MIME, status).
   The existing attachment path can access original bytes as needed; transfer
   never routes originals, base64 or full logs through a model or terminal.

Cancellation intent is saved by the host before advancing. For known receipts,
query current state, then DELETE; lost replies are reconciled on the next GET.
If completion already won, preserve its ready hash and report that publication
cannot be undone by cancel. Canceling a transfer never claims the producer or
Herdr process stopped. Unknown creates cannot claim successful remote cleanup.

## Reverse direction and local filesystem trust boundary

NAS→Windows uses existing authenticated `/bots/{bot}/media/{content_hash}`.
The expected hash and size come from an authorized task manifest. Resume with
Range and If-Range; verify exact ETag, Content-Range, byte count and final hash.
Reject 200-on-resume, an inconsistent 206, encoded bodies and unexpected redirects.
Only fsynced bytes may enter a durable download checkpoint. Truncate any
unacknowledged tail before resume. Publish the destination with the host's atomic
rename only after full SHA-256 verification; a download checkpoint is not ready.
The adapter accepts a staging handle and never interprets a server filename.

Host filesystem code must reject traversal, alternate data streams, drive/UNC
escape, device/reserved names, symlinks/junctions/reparse points and unsupported
hard links at the handle boundary. Resolve below the authorized locator root
with race-resistant handles; lexical path checks alone are insufficient. Keep
staging ACLs private and exclude adversarial writes. No Windows path-confinement
or power-loss durability claim is made by the portable adapter tests.

Memory is bounded by the selected chunk (maximum 8 MiB), JSON response limit
(64 KiB) and download/hash copy buffer (256 KiB), excluding HTTP/TLS and caller
overhead. Host reserves staging bytes and a safety margin before admission,
checks free disk again while writing, and retains retryable snapshots on failure.
Server quotas reserve upload size but not all retained originals; budget for
staging plus canonical publication copies. ENOSPC must stop, never mark ready.

## Security decision that must precede production

First inspect, through already authorized administration, whether the existing
host has a reusable encrypted route and appropriately scoped Memoh identity.
Neither is proved here. Do not reuse Agenvo OAuth as a Memoh credential or hand
a broad Bot/account token to the PC. Hashes provide integrity, not encryption.

If missing, propose for explicit action-time approval: existing TLS ingress with
certificate/hostname validation (or an approved encrypted private tunnel), plus
a short-lived revocable Memoh transfer capability bound to owner, Bot, target
fingerprint, operation, direction, size/hash budget, allowed receipt and expiry.
Grant rechecks apply to create/read/append/complete/cancel/registration and
download. Refresh requires the same current task grant; persistent enrollment,
certificate issuance, trust-store changes, firewall/ports and production routing
each need concrete approval. Current PR #8 does not enforce this proposed
capability; an isolated fake credential does not close that gap.

The prototype requires HTTPS, follows no redirects and acquires authorization
from a caller callback for each request. It stores no credential. Tests use an
ephemeral loopback TLS server and synthetic authorization only. No credentials,
certificates, security settings or deployment are changed in production.

## Reproducible performance acceptance, after authorization

Record hardware, NIC negotiated link, IPv4/IPv6 path/RTT, cipher, OS, mount type,
server/client versions, file limit and free space. Use synthetic incompressible
200 MiB, 512 MiB (only with an approved finite limit), and approved larger files;
include small-file batches. Run warm/cold cases and at least five repetitions.

Measure separately: local source/snapshot read+hash+fsync; NAS durable sequential
write/read; network-only encrypted stream through the intended path; full PR #8
upload/complete and Range download. Vary 1/2/4 MiB and 8 MiB chunks and 1/2 workers
without exceeding quotas. Record verified useful bytes/sec, phase latency,
p50/p95, retries, CPU, RSS/heap, network bytes and disk bytes/IO wait. Completion
performs whole-file verification; existing media reads may verify the whole file
before Range, so repeated small ranges can become disk-bound. Do not attribute
that to the NIC without isolated measurements.

Inject disconnects, stalled bodies, process kills, server restarts, lost
create/PATCH/complete/DELETE/registration responses, source changes, corruption,
permission revocation, quota exhaustion, disk full and cancellation races. After
every case verify final hash, one logical result association and bounded
resources. Loopback timings are fixture overhead, not NAS throughput. No
line-rate claim is made until this acceptance is executed on the approved path.

## Verification boundary

Repository `AGENTS.md` requires a running development environment, real UI and
screenshots for every change. The task forbids installing/integrating Memoh
runtime; no waiver is inferred. Isolated protocol tests do not satisfy that
requirement. This work remains incomplete/draft until permitted full application
verification, Windows host integration, scoped security and NAS acceptance are
resolved. No merge, release, publication or production deployment is performed.
