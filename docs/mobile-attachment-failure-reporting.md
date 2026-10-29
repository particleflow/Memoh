# Scoped attachment failure reporting candidate

This follows `7e765e3e917747288b4f687dfaac97cbdd01a99d` (published-snapshot
preservation) and `6a476bb843c607517bd2bcca48af863c321e08bd` (structured bridge
errors). It closes the cloud-tested gap between that error and an authenticated
NAS-readable record. It is **not deployed** and does not establish the reported
phone's installed version, historical first error, or actual upload acceptance.

## Architecture and permission boundary

1. The existing native `upload.*` response returns the first fixed stage,
   integer SDK code and time. The original frame, active/ready and lifecycle
   gates remain. Existing token/generation checks reject late-account replies.
2. `nativeAttachmentCommand` attaches its existing operation ID to the validated
   failure and queues it before throwing. `HarmonyAttachments.vue` preserves
   `{message, failure}` in its existing error state rather than a message alone.
   The original displayed message and layout stay the same. Common capture also
   covers callers outside that component, before later string conversion.
3. `harmony-upload-failures.ts` keeps a reactive pending/reported/conflict state
   and a bounded-lifetime metadata outbox under
   `memoh:harmony:upload-failures:v1`. Before persistence or delivery it binds the
   record to SHA-256(API base + current already-loaded account ID). Neither the
   base nor the account ID nor token is persisted in the outbox. A missing
   initialized identity stops persistence/reporting; it is never guessed. The
   original structured component error still exists in that case.
4. The client makes an authenticated metadata-only PUT to the existing configured
   NAS origin through the existing API fetch/auth handling. Redirects are
   rejected. No certificate rule, credential, address, permission or port is
   added. There are no timers or file-transfer retries. A subsequent existing
   mount/activation restores only that identity's metadata outbox. A different
   API binding is never transmitted; known foreign-binding disk rows are
   preserved until normal expiry or the existing explicit user-state clear.
5. The existing `MessageHandler.uploadScope` applies the same JWT, current Bot
   chat permission and current identity extraction as resumable uploads, before
   request decoding or storage. Owner is derived from JWT and Bot from the
   authorized route. Neither can be supplied in the JSON payload. Reads filter
   both even for a caller who can access another Bot or is an administrator.
6. `UploadStore` stores records in the existing private `.uploads` root as the
   fixed `failures.journal`, with its existing no-follow/single-link opens,
   admission lock, file fsync, atomic rename and directory fsync. This is part of
   the existing file module, not a new service or public log. It never calls
   Create/Append/Complete/Ingest, launches an agent/model, creates a chat/session,
   changes file references or opens original bytes.
7. A same-identity authenticated client can GET these reports over the existing
   public NAS origin, including when the phone is remote. No USB/HDC, LAN access,
   menu invention, private-log export or user file retransmission is required.

## Resource and data whitelist

| Method | Existing file-module resource | Semantics |
| --- | --- | --- |
| PUT | `/bots/{bot_id}/media/upload-failures/{operation}` | Immutable same-ID client report; exact repetition returns the original acknowledgement |
| GET | `/bots/{bot_id}/media/upload-failures` | Current owner/Bot's unexpired reports |
| GET | `/bots/{bot_id}/media/upload-failures/{operation}` | Current owner/Bot's matching unexpired report, or 404 |

This is an authenticated application resource, not an anonymous/public log
port. A user with Bot access cannot read another identity's report. Revocation
is checked on every PUT and GET, including after a record is warm or persisted.
No broadening to all users, devices or administrators is implemented.

The PUT JSON accepts only:

| Field | Source and validation |
| --- | --- |
| `version` | Exactly 1 |
| `phase` | The fixed native/bridge phase whitelist |
| `code` | The fixed existing machine-code whitelist or `UNCLASSIFIED` |
| `sdk_code` | Integer 0..2147483647, zero when unavailable |
| `at` | Positive safe-integer client timestamp; not server-attested time |
| `handle` | Optional already-existing 32-hex native attachment handle |

`operation` is the existing 32-hex bridge command ID, supplied in the route. It
is not a new upload ID or an execution ID. Server acknowledgement adds only
`operation`, `received_at` and `expires_at`; the server keeps owner/Bot solely for
its existing scope checks. A 2 KiB body bound and strict unknown-field rejection
exclude filenames, URI/path, text, file data/hash, credentials, SDK messages,
stack/cause, arbitrary tags and client-supplied identity. The Web serializer also
reconstructs only this whitelist. The local outbox additionally contains Bot ID,
binding hash, report state and expiry; it never persists display messages.

All records are **client reports**, not proof that a file exists, a hash matches,
a receipt committed, an operation executed, or an OS/network diagnosis is true.
The server does not act on a phase/code beyond retaining it for scoped reading.
Image-specific picker/URI/decoder work remains with the image owner; this change
does not modify that implementation or invent image substages.

## Idempotence, restart, deletion and retention

The tuple (authenticated owner, authorized Bot, existing operation ID) identifies
one immutable first report. Exact repeat PUT returns the same timestamps and
record; changing stage, code, time or handle returns 409 with zero replacement.
The client marks conflict and stops automatic retries. A lost acknowledgement
leaves the same local record pending; restoration retries only the identical
metadata PUT with the identical ID. It cannot create a file receipt or rerun a
task. GET repairs only the existing journal durability barrier; it inserts no
report or transfer proof and does not refresh expiry.

Server expiry uses the existing `media.UploadTTL` (24 hours from first server
receipt), and exact repetition does not extend it. Expired records are absent
from GET. Physical pruning runs on the original public `UploadStore.Sweep` or
subsequent report admission. This matches the existing queue's activity-driven
GC; it is not a new continuously running cleanup service. Server-clock
`received_at` is distinct from device-clock `at`.

The metadata journal reuses `maxUploadSessions` as its bounded record window;
there is no new numeric transfer byte budget. It does not consume active-upload
slots or reserve any file bytes. A full metadata window returns the existing
quota response and leaves the client pending; it does not evict old reports or
block ordinary upload admission. Original receipt parsing/quotas remain separate
and do not interpret this fixed `.journal` as a receipt. Invalid/linked metadata
journals fail closed for metadata while ordinary file admission remains intact.

The client initially uses the same 24-hour lifetime, then adopts acknowledged
server expiry. Same-identity process restart restores the outbox without a new
operation. Reported entries do not send again. Logout/identity clear aborts
in-flight reporting, clears this user-scoped local outbox and component error,
and prevents late responses from reintroducing records. Foreign identity/origin
bindings are never sent. Successful original consume/discard clears associated
local error rows by handle and aborts pending reports for those rows. As with an
existing server receipt, a server report remains scoped until expiry; no new
delete UI or destructive recovery is added. A pre-snapshot failure with no
handle follows the operation's normal expiry.

Unknown/corrupt local outbox bytes are preserved, never exported or silently
rewritten. The client may keep a new whitelisted failure in memory and report
it, but will not overwrite the unknown stored format. This does not alter or
delete any native original, upload journal, old queue configuration or task.
Storage denied/full and unavailable identity remain explicit limits to local
durability; neither is permission to misattribute a report or retry a file.

An old/unupdated NAS that rejects this resource leaves metadata pending. It
does not cause file retransmission or a replacement upload ID. Historical errors
already reduced to a string by older HAP code cannot be reconstructed by this
candidate. No claim of real-device collection is made.

The existing persistent `UploadStore` must already be enabled; otherwise the
same upload scope returns 503 and metadata stays pending. The candidate neither
creates an alternative store nor changes configuration. The existing
`/bots/{bot_id}/media/limits` `resumable_enabled` field identifies that storage
prerequisite, but does not by itself prove these new routes were deployed.

## New differential verification

Only new reporting tests were run; the original seven staging tests, previous
upload matrices, HAP builds, model calls and deployments were not rerun.

```sh
HARMONY_TEST_TYPESCRIPT=/path/to/existing/typescript \
go test ./internal/media ./internal/handlers -run '^TestUploadFailure' -count=1 -race -v

HARMONY_TEST_TYPESCRIPT=/path/to/existing/typescript \
HARMONY_UPLOAD_FAILURE_REPORT_ONLY=1 \
node mobile/client/tests/stage2/harmony_resumable_frontend.cjs
```

The Go tests exercise persistent storage/reopen, immutable repetition and
conflict, wrong owner/Bot, TTL/GC with unchanged real committed-original bytes
and hash, unknown acknowledgement recovery, linked-journal denial, bounded
metadata admission without consuming file slots, and real signed JWT HTTP
read/write/revocation gates. Unknown/private fields and oversized bodies are
rejected without echoing them. Denied reads/writes leave prior readback equal.

The Node fixture executes the actual component script, bridge consumer and
reporter with controlled native errors. Its unit storage boundary checks fresh
module/VM restoration with retained WebStorage, lost-ack same-ID recovery, conflict, logout,
foreign binding, expiry, unknown stored-byte preservation, and late response
after consume without resurrection. These checks perform no upload.run or
original-byte transfer.

`TestUploadFailureClientToAuthenticatedHandler` connects that actual client flow
to the real local HTTP handler and signed JWT middleware. The first independent
Node process receives a controlled native error, saves the WebStorage outbox
through a real file/fsync adapter, sends the metadata, then loses its
acknowledgement after the server saved it. A second independent process restores
that exact outbox, sends the identical operation, and verifies GET readback.
The first server record and times must remain identical; the second process
issues zero native file commands and sends zero file bytes. This is real process
restart with a controlled storage/native boundary, **not ArkWeb or phone restart**.
The synthetic evidence is `phase=source-open`, `sdk_code=13900012`,
`pending -> reported`, `fileBytesSent=0`; final run used process IDs `67611 -> 67623`, operation
`5aff10654b8b4df2bcd4baaf1cf7e7c1`, with an unchanged first server record.
It is not a phone/NAS field result or real OS diagnosis.

Swagger and SDK were generated from the final handlers. SDK typecheck, targeted
strict TypeScript checks (with API-boundary declarations), actual Vue script
compilation and the source inventory guard are separate source checks. They do
not substitute for official ArkTS/HAP build, real UI/device acceptance, or
deployment. Preserve the presentation owner's removal of unwanted small text
when combining; this functional patch adds no new copy/button or image logic.
