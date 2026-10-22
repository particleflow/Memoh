# Resumable attachment uploads

This additive Server API takes over attachment byte transfer before a chat turn.
It uses the existing `media.Service` and canonical per-Bot originals. Chat,
Computer task JSON and model context continue to carry `content_hash`; neither
physical paths, upload receipts nor partial bytes belong in a model request.
There is no second filesystem API and no database migration (schema 170 remains).

## Existing implementation and scope

Before this change, `POST /bots/{bot_id}/media` already streamed multipart data
through `media.Service.Ingest`, computed SHA-256, and returned a Bot-scoped hash.
`Resolve`, `Open`, `OpenSeekable`, and `EnsureAccessPath` already supported existing
references and disposable workspace copies. There was no durable upload session
or reusable resumable protocol. This change adds the five operations below and
shares the existing publication/deduplication code with `Ingest`.

## Deployment and limits

Configure `[files].originals_root` to an operator-owned dedicated originals
subdirectory of the private storage mount, outside Bot workspaces. Never point
it at all of Public. Keep deployment configuration in the existing configuration
location; do not commit private hostnames, mount paths, secrets or private files.
The service creates a private `.uploads` child of that configured root. No new
port, credential, permission, database table or migration is required.

`max_upload_bytes` remains authoritative for **both** upload protocols. Omitted
or zero still means **200 MiB**; this change does not increase production limits.
The limit is checked at creation; already-admitted receipts retain their size
when the policy changes. A synthetic 512 MiB test uses an explicit finite value `536870912`. The existing
`GET /bots/{bot_id}/media/limits` adds `resumable_enabled` and `chunk_max_bytes`.
Resumable upload is disabled (503) without an independent originals root.

The filesystem must support Unix advisory locks, atomic same-directory rename,
and file/directory fsync. This implementation is supported on Unix servers; it
fails initialization on unsupported platforms. NFS/SMB lock semantics, sudden
power loss and NAS deployment have **not** been validated by cloud-local tests.
All processes serving the same originals must use the same mount and limits.
The dedicated root is trusted operator-owned storage, not writable by users or
workspace containers. Payload/journal/lock opens reject symlinks, nonregular
files and hard links; root handles confine relative operations.

## Client contract

Every operation requires existing authenticated identity and Bot `chat` access.
A receipt is additionally bound to its creating identity and Bot; sharing a Bot
or being an administrator does not transfer receipt ownership. Unauthorized
receipt lookup and mutation return the same 404. Bot authorization is rechecked
on every call, including after a permission change.

All paths below start with `/bots/{bot_id}/media/uploads`:

| Method and suffix | Request | Successful response |
| --- | --- | --- |
| `POST` (no suffix) | JSON `{size_bytes, sha256, mime}`; positive exact size and lowercase final SHA-256 | 201 receipt with `state: uploading`, `offset: 0` |
| `GET /{id}` | No body | 200 durable receipt, including `offset` and `expires_at` |
| `PATCH /{id}` | Raw bytes; `Upload-Offset`, `Upload-Length`, `Upload-SHA256` headers | 200 receipt at the new acknowledged offset |
| `POST /{id}/complete` | No body | 200 `state: ready` and `content_hash`, only after final verification/publication |
| `DELETE /{id}` | No body | 200 `state: canceled`; repeated cancellation is safe |

Use no trailing slash for the collection POST. Each chunk is 1 byte–8 MiB. The HTTP chunk read deadline is two minutes; a
stalled transfer releases its lock and can resume from the last receipt. The header
length must equal the actual body size. The chunk checksum is mandatory. Chunks
are sequential; out-of-order or repeated offsets return `http.conflict` (409).
The client must query status after any lost response and resume at the returned
offset. If a PATCH returned 429 (`http.too_many_requests`), retry with bounded
backoff; lock stripes can briefly serialize unrelated upload IDs too.

Cross-origin preflight explicitly permits the three `Upload-*` headers above;
the existing origin and credentials policies are unchanged. If the HTTP writer
cannot enforce a read deadline, PATCH returns `http.service_unavailable` (503)
before reading the body or acquiring an upload lock. A failed/stalled body never
advances the acknowledged offset; reconnect and GET before retrying.

An incomplete completion is 409. A malformed input or checksum mismatch is
`request.field_invalid` (400, field `upload`); exceeding the configured file size is
`http.payload_too_large` (413). All responses use the existing Problem catalog;
underlying filesystem diagnostics do not appear in the public response.

`content_hash` is absent while uploading/canceled. Successful completion is
idempotent for the lifetime of the receipt, including a retry after publication
but before receipt acknowledgement. The receipt expires **24 hours after
creation**; calls after expiry are 404, including repeated completion. The
original and its stable hash survive receipt expiry. Canceling a ready receipt
returns 409 and never deletes the original. Clients should retain the ready hash
and MIME/size and submit the normal hash attachment to the chat API.

For the PC task author: operation JSON should carry the same Bot-scoped stable
hash after `ready`; do not treat upload ID, host path or workspace path as the
original's durable identity. Generated-result registration is a separate gap.

The separately authenticated computer-transfer adapter adds a progress lease for
new task uploads; see [task upload lifecycle](delivery-upload-lifecycle.md). It
does not change the fixed-expiry behavior of these generic upload endpoints or
retroactively change existing receipts.

## Durability and resource model

Each chunk is streamed with a 256 KiB copy buffer into one private staging file,
verified, fsynced, and then checkpointed with atomic journal rename plus directory
fsync. The journal advances only after durable byte verification. Restart repair
truncates any unacknowledged tail to the stored offset. A final pass computes the
whole-file SHA; the existing canonical provider independently verifies its
atomic publication. Completion avoids a second whole-file temporary spool.

Readable files alone are not proof of durable publication. Deduplication repairs
the canonical file and directory fsync barriers before acknowledging an existing
original. Journal reads likewise fsync the visible journal and its directory
before returning an offset/ready receipt or allowing terminal payload cleanup.
This repairs a process interruption or lost acknowledgement between rename and
directory fsync. A failed barrier returns an error and preserves staging until
recovery; it cannot authorize deleting the only pending copy. These guarantees
depend on the filesystem honoring its fsync contract.

Cross-process nonblocking locks cover append, completion, cancellation and
collection. Receipt admission has a separate lock. The current bounded policy
allows 16 active uploads globally, four per identity (across Bots), active
reserved bytes totaling four times the configured per-file limit, and 128 total
receipts until expiration. Exceeding a quota returns 429. Admission reserves the
entire declared file size. Publication may temporarily require both staging and
canonical copies (up to two copies of that file); the operator must still budget
storage for retained originals. This is not an originals disk-quota system.

Collection runs at startup, hourly, and before admission. It scans only the
private staging directory, retains active locked transfers, removes expired
staging/receipts and crash-left temporary journals, and reclaims ready/canceled
payloads left by an interrupted cleanup. It never calls the originals Delete
method and never walks the Public tree. Existing Media is preserved.

## Compatibility, rollout and rollback

No migration or index rebuild is needed. Existing single-request multipart
uploads, inline limits, file reads/ranges and chat attachment references retain
their behavior. Existing clients can ignore the two additional limits fields.
The generated SDK includes the new operations; raw chunk callers must send
binary data with `application/octet-stream` (not JSON/base64).

Deploy Server first, then let clients detect `resumable_enabled`. A client must
persist its receipt ID, expected SHA, size and local source identity to resume
across client restarts; this server change does not implement that client work.
On rollback, old servers continue reading completed originals but cannot serve
pending receipts. Preserve `.uploads` so a compatible Server can resume within
24 hours; do not delete Media or the configured originals directory.

## Deferred gaps

- Harmony/client integration, client background upload and source revalidation.
- Durable registration/return of generated PC/media results.
- Full-buffer workspace `FSRead` and some preview paths.
- Media gallery expansion, media understanding and monitoring workflows.
- Parallel/out-of-order chunks, multi-node/distributed filesystem certification,
  configurable per-tenant quotas and client-side content hashing UX.

See [measured validation](validation/resumable-uploads/README.md). No NAS,
Windows production environment or private client project was touched. Agent
runtime, context and contextview files must remain identical to upstream
`bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046`.
