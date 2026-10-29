# Mobile attachment staging: failure attribution and recovery

Source audited: `12faf4c06db01d8fffc29aad3a247b4a85e884e7` (the
1.0.28 source candidate). The installed phone version is **unknown**. The field
writer reports healthy Server/Web `5a42`, one media limits 200 at 08:18:24 UTC
on 2026-10-29, and no observed completed upload request or new media. This does
not prove that a write was never attempted. The three pre-login 401s cannot be
attributed to this phone. No field operation was performed for this change.

## What the screenshot establishes

`mobile/client/mobile/memoh/harmony-resumable.ts`,
`nativeAttachmentCommand`, supplies the screenshot's generic failure text for
any native error not explicitly mapped there. That text does not identify a
hash, permission, network, URI or storage failure.

`NativeClient.request` preserves uppercase machine error messages but maps
other messages to `VOICE_UNAVAILABLE`, including upload failures. A controlled
execution of the actual native router and frontend source confirmed:

| Injected boundary failure | Native reply | Existing frontend branch |
| --- | --- | --- |
| `MEDIA_SHA_MISMATCH` | Same code | Screenshot's generic branch |
| `UPLOAD_HTTP_403` | Same code | Authorization branch, different text |
| URI permission SDK error with code 13900012 | `VOICE_UNAVAILABLE` | Generic branch |
| Network SDK error with code 1007900007 | `VOICE_UNAVAILABLE` | Generic branch |
| Directory sync SDK error with code 13900020 | `VOICE_UNAVAILABLE` | Generic branch |

The numeric errors above are synthetic API-boundary inputs, not captured phone
errors. Raw messages and synthetic private-detail markers did not reach the
frontend. This change does not alter the bridge response or UI copy.

## Pre-upload path and the confirmed preservation bug

`NativeResumableUpload.select` reads owner and media limits before opening the
picker. It resolves the selected name/MIME, opens the granted URI, copies bytes
into its app-private snapshot, hashes that snapshot, and re-verifies the source.
It publishes the bounded journal by file fsync, rename, then directory fsync.
Selection itself does not call upload Create/PATCH/Complete.

The old implementation marked the snapshot committed only after `save`
returned. If rename succeeded but the subsequent directory open/fsync failed,
both picker/recording staging and bounded Web File staging deleted the already
published journal and its verified snapshot. This was reproduced on unmodified
product source with real filesystem writes and an injected directory-sync
failure; the new assertion failed because the published journal disappeared.

The fix marks the local publication immediately after successful rename. A
later directory-sync error still fails the command; it cannot turn into upload
success. It leaves the published journal and source available for an explicit,
reauthorized restore. A failure before rename still reclaims the unpublished
copy. No ID, journal version, upload receipt, owner/Bot/scope, hash check, TLS,
network policy, or remote write behavior changes. Explicit discard/abort keeps
its existing semantics.

This protects recovery after a local staging failure. It is **not** evidence
that directory fsync caused the reported phone incident, or that this phone can
now upload successfully.

## Existing readback capability: no remote user entry

The phone is remote, outside the LAN. USB, HDC, a connected PC, and a private
sandbox dump are not prerequisites or instructions for this investigation.

The fixed source has no user-facing diagnostic menu, share/export operation,
remote diagnostic endpoint, or bridge command that reads
`client-diagnostics.jsonl`. `ClientDiagnostics.event` only writes the app-private
file and HiLog. `EntryAbility.onDump` returns image timing data through the
system dump mechanism; it neither exports attachment failures nor establishes a
phone-user diagnostic entry. Voice worker diagnostics stay inside the worker
and native process. These are not supported public-network readback channels.

The existing backend `AccessLog` records method, sanitized URI, route, HTTP
status, latency and result attributes; it does not consume attachment-stage
request headers. The existing limits handler has no client-failure input.
`nativeAttachmentCommand` uses `createApiFetch` with a bridge-backed fetch
function: its `/users/me` argument is not an automatic HTTP error report. The
earlier successful limits request cannot carry a later local failure stage.
There is no existing event or authenticated request-metadata path that returns
that local first error to the server. No reporting headers, query parameters,
extra requests, new endpoint or publicly readable log have been added.

### Read-only evidence boundary for the sole field writer

Do not select or upload the user's attachment again to collect evidence.

1. Read the actual installed app version/source identity, if an existing
   authorized app diagnostic channel exposes it. The source candidate version
   alone is insufficient.
2. Only if an already authorized supported diagnostic channel actually exists
   on the installation, read timestamped records from
   `context.filesDir/client-diagnostics.jsonl` and its `.previous` rotation
   matching `component=photo-selection` or, after this patch is integrated,
   `component=attachment-stage`. `context.filesDir` is the installed app's own
   context path; it is not a guessed NAS, PC, or shell path. No new permission,
   sandbox bypass, or private-file dump is authorized by this document.
3. Retain only `at`, `area`, `component`, `phase`, and sanitized `detail`.
   The new failure record contains a fixed phase plus numeric SDK code and a
   machine enum; it contains no URI, filename, token, account ID, Bot ID, scope,
   original bytes, hash, upload ID, or authorization header.
4. Existing unmodified source has no attachment-stage failure logger. Its
   `photo-selection` `converted`/`complete` events can bound progress, but the
   absence of those events cannot supply a missing first error. The patch cannot
   retroactively reconstruct the old SDK code. If no existing supported read
   channel is available, report that exact read capability as unavailable.
5. If already authorized queue inspection is available, report only whether a
   valid matching snapshot/journal exists and whether its recorded size/hash
   still verify. Preserve all original IDs and records. An empty NAS media list
   does not authorize a new upload or replacement receipt.

New staging failure phases are `owner-read`, `limits-read`, `picker`,
`source-metadata`, `source-open`, `snapshot-create`, `snapshot-copy`,
`snapshot-hash`, `source-verify`, `journal-publish`, and `journal-dir-sync`.
For example, `source-verify` with `MEDIA_SHA_MISMATCH` separates a failed source
recheck from `source-open` with an SDK permission code; `limits-read` separates
metadata networking from local IO. `journal-dir-sync` means rename returned
successfully, not that durability or upload completion was confirmed. Some
startup/finalizer failures occur outside these staged-operation catches; absence
of a record does not prove that the command succeeded.

Once a valid snapshot is published, subsequent restore reads that snapshot and
the original journal. It does not reopen the old picker URI. Failure of a
temporary grant before publication and loss/change of an owned snapshot after
publication are different boundaries; neither permits guessing a replacement.

## Undeployed candidate: preserve failure metadata on the existing bridge

An optional `failure` member now carries a staged-operation error through the
existing version-1 `upload.*` error response. No command or network protocol is
added. The existing `error` value, `ok=false`, upload identity and all success
responses keep their semantics. Old consumers ignore the optional member; new
consumers accept old responses without it.

```json
{
  "version": 1,
  "ok": false,
  "error": "VOICE_UNAVAILABLE",
  "failure": {
    "version": 1,
    "phase": "source-open",
    "sdkCode": 13900012,
    "at": 1793261904000
  }
}
```

This example is synthetic. Only the fixed phase whitelist, SDK integer in
0..2147483647, and positive integer timestamp are returned. SDK messages, paths,
URIs, filename, original bytes, hash, credential, Bot/scope/owner identifiers,
upload ID, stack and cause are excluded. The native bridge and Web consumer
both reconstruct the whitelist, dropping extra properties. Unknown or malformed
metadata is ignored without turning failure into success. The first staged
error is retained if later cleanup fails; secondary cleanup is logged locally.
The native error getter stops exposing its metadata after its lifecycle epoch
changes or authorization closes. Existing frame and ready gates still apply.

The Web helper `nativeUploadFailure(error)` in `harmony-resumable.ts` returns
the validated metadata plus an allowlisted machine code derived from the
existing `error` enum. Other codes become `UNCLASSIFIED`; a plain legacy error
returns `undefined`. Current 401/403 handling remains the shared auth handler,
before metadata attachment. The original generation/token check runs before
consuming a reply. A late reply from another login is rejected without exposing
its metadata. None of this reads or uploads an original, creates a new receipt,
retries an operation, publishes an event, persists error metadata in Web Storage,
or contacts a diagnostic server.

### Exact handoff to the existing presentation owner

`HarmonyAttachments.vue` currently catches the selection error in `action` and
immediately assigns `cause.message` to a string. `reopenPreviews` also reduces
the error to its message. Those are the attachment-presenter consumption seams:
call `nativeUploadFailure(cause)` before string conversion, and let the existing
error surface localize the fixed stage/code. The existing send failure catch in
`apps/web/src/store/chat/send.ts` similarly receives the original `Error` before
message conversion. This patch changes no UI file, user-facing copy, menu or
button. It does not claim that these presenters already consume the metadata.

Image picker/URI/format decoding belongs to the separate image owner. No image
implementation was changed. The current common contract exposes its existing
`picker` boundary, not a fabricated decoder substage; any finer enum must be
agreed across native producer, bridge and frontend validators before integration.

This candidate makes future first errors available to the current authorized
Web document through its existing request response. It is not server-side
telemetry, a public log channel, or a remote historical-error retrieval feature.
It cannot recover the already discarded error from the reported incident, and
does not survive a phone process restart as an exported failure record. The
existing private diagnostic journal remains private. It is **not deployed**;
official HAP build, presenter consumption and installed-phone acceptance remain
unverified. The user's remote location is not a reason to stop cloud development
or to request that they retransmit their attachment.

### New envelope checks only

Use the existing TypeScript dependency and set
`HARMONY_UPLOAD_FAILURE_ENVELOPE_ONLY=1` for the existing
`harmony_resumable.cjs` and `harmony_resumable_frontend.cjs` fixtures. The native
subset runs the actual router and uploader with real disk/local HTTP and
synthetic Kit faults. It checks source-open SDK denial, source verification hash
failure, metadata network failure, preservation of the first hash error across
a cleanup failure, and expiry of failure visibility after native close. The
frontend subset checks three stage/code combinations, legacy replies, malformed
versions/phases/numbers, dropping extra private fields, shared 403 handling and
late-account rejection. There are no diagnostic network requests. Counts of
upload Create/PATCH/Complete are zero. These are isolated results, not evidence
of the actual phone's installed version or its historical first error.

## Narrow verification

For the original preservation fix, only its new failure subset was run. Those
seven cases were not rerun for the envelope follow-up. No old upload matrix, full build, HAP
build, model call, deployment, or real user transfer was run.

```sh
HARMONY_TEST_TYPESCRIPT=/path/to/existing/typescript \
HARMONY_UPLOAD_STAGING_FAILURE_ONLY=1 \
node mobile/client/tests/stage2/harmony_resumable.cjs 0.25003528594970703
```

Actual ArkTS is transpiled into independent Node processes with controlled Kit
boundaries, real disk and local HTTP. The fixture uses 262,181 fully written
bytes, SHA-256
`bb394a044c3e4358b667f31f471ea40a6344d9fe984dd07b632421ac323032fe`.

The seven new cases cover the shared granted-source/recording staging method
(via the recording-path fixture, without a real picker) and bounded File staging at rename
failure and post-rename directory-sync failure; granted-source open denial;
actual same-size source mutation between copy and verification; and a metadata
network failure. Post-publication failures retain identical bytes and journal
in a new process, including when the picker source pathname is withdrawn.
Wrong-owner and revoked-credential restore are denied; the preceding queue
entry remains byte-for-byte unchanged. Recorded Create/PATCH/Complete counts
are all zero. Failure logs identify the expected phase and SDK/enum, with no
injected private-detail markers.

This is cloud isolation evidence. Official ArkTS SDK/HAP build, installed-phone
first-error attribution, and real phone acceptance remain with their owners.
Web and Windows transfer acceptance are not established by this phone patch.
