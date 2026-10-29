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

## Minimal first-error readback for the sole field writer

Do not select or upload the user's attachment again to collect evidence.

1. Read the actual installed app version/source identity, if an existing
   authorized app diagnostic channel exposes it. The source candidate version
   alone is insufficient.
2. Through that same supported channel, read only timestamped records from
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

## Narrow verification

Only the new failure subset was run. No old upload matrix, full build, HAP
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
