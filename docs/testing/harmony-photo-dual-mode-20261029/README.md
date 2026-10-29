# Photo conversion lifecycle and verified snapshot handoff

Status: source candidate, host regression tests passed; **Harmony API26 compilation, actual phone URI grants, real affected UI interaction, and screenshots remain unverified**. No package was built, signed, installed, or deployed. This work does not establish the cause of the reported 08:22 generic attachment error.

## Contract and source

Compression/re-encoding when the original checkbox is unchecked is expected functionality. Hash changes in that mode are expected and are not failures. With the original checkbox checked, the selected bytes and metadata must remain unchanged. This change preserves both modes, the checkbox, the existing 2048px/PNG-or-JPEG-quality-85 policy, and all successful encoded fixture outputs.

Baseline: `particleflow/Memoh@12faf4c06db01d8fffc29aad3a247b4a85e884e7`, the published 1.0.28 source, not proof of the phone's installed version. Its `NativePhotoSelection.ets` SHA-256 is `7de4bca9669a9217e9aa5f48bfe88b3322f256529f9f6eacbe3c26fb0f6415f0`. The supplied entry-source overlay ZIP was checked at 278973 bytes and SHA-256 `564de7cf9619bf216090b3702e882bf63e64e7528bcb1145fd56298cec549327`; it contains the Web selector entry but not this selector class. The selector and snapshot classes were read from the exact Git baseline.

The [official picker reference](https://github.com/openharmony/docs/blob/master/en/application-dev/reference/apis-media-library-kit/arkts-apis-photoAccessHelper-class.md#photoselectoptions) describes the original checkbox/result flags. The [official ImageKit encoding options](https://github.com/openharmony/docs/blob/master/en/application-dev/reference/apis-image-kit/arkts-apis-image-i.md#packingoption) describe output format and quality. No new codec options or SDK APIs are introduced.

## Confirmed implementation defects

1. **Successful conversion caches are never released on the native path.** `NativeResumableUpload.select` creates a one-shot `NativePhotoSelection`, calls `select`, and passes each returned URI to its existing `stagePath`. That method copies to a durable snapshot and verifies its hash, but the caller discards the selector without `close()`. Every successful compressed selection leaves its `memoh-photo-*` directory/file in the app cache. This is a resource lifecycle defect; tests do not establish that a real phone exhausted its cache.
2. **Current failed output is omitted from cleanup.** The selector only cleans conversion URIs already appended to `selected`. If creating the current output file fails after its directory was created, no current URI has been returned yet, so that directory remains. Output-close or late errors have the same ownership gap.
3. **Cleanup can mask the actual conversion failure and lose retry ownership.** The batch catch directly calls `remove`; a cleanup error replaces the original decode/write error. `remove` deletes its tracking entry in a `finally` block even when deletion fails, so a later `close()` cannot retry cleanup.

These facts are independent of networking/authentication and are not evidence of a failed HTTP status or server hash mismatch in the phone incident.

## Minimal correction

- Track each newly created conversion path in the current selector call before attempting to open its output. On failure, clean exactly that call's paths after the output/source descriptors have reached their `finally` cleanup, including the current unreturned path. Log cleanup failures without replacing the primary conversion error.
- Remove a path from the selector's owned set only after file and directory cleanup succeed. A busy file can be retried by a subsequent `close()`.
- Retain the selector in `NativeResumableUpload.select` and call `close()` in its existing finalization, after `stagePath` has returned or failed. Successful snapshots and journals remain available after the conversion cache is removed. Selector creation remains after the pre-existing recording-path early return.

Production changes are limited to `NativePhotoSelection.ets` and six image-lifecycle lines in `NativeResumableUpload.ets`. The latter only retains/releases the image selection helper; it does not change `stagePath`, journal persistence/fsync, authentication, TLS, upload protocol, endpoints, error mapping, permissions, UI, old cache files, or historical queue entries. In particular, it does not duplicate the other owner's journal-fsync correction.

## Reproduction

Requirements: Node, TypeScript (this run used 6.0.3), Python, and Pillow (12.3.0). From the repository root:

```sh
mkdir -p /tmp/harmony-photo-baseline
git show 12faf4c06db01d8fffc29aad3a247b4a85e884e7:mobile/client/mobile/harmony/app/entry/src/main/ets/common/NativePhotoSelection.ets > /tmp/harmony-photo-baseline/NativePhotoSelection-baseline.ets
git show 12faf4c06db01d8fffc29aad3a247b4a85e884e7:mobile/client/mobile/harmony/app/entry/src/main/ets/common/NativeResumableUpload.ets > /tmp/harmony-photo-baseline/NativeResumableUpload-baseline.ets
HARMONY_TEST_TYPESCRIPT=/path/to/typescript HARMONY_PHOTO_BASELINE_DIR=/tmp/harmony-photo-baseline node mobile/client/tests/stage2/harmony_photo_dual_mode.cjs --baseline
HARMONY_TEST_TYPESCRIPT=/path/to/typescript node mobile/client/tests/stage2/harmony_photo_dual_mode.cjs
```

The harness executes the actual selector, `NativeResumableUpload.select`, unmodified `stagePath`, and `NativeMediaFile` copy/verification code after TypeScript transpilation. File creation, short writes, descriptor closure, snapshot persistence, journal writes/fsync, and SHA-256 operate on the real Linux filesystem. Only owner/limits responses are supplied directly; no upload HTTP/protocol work is repeated.

**Evidence boundary:** picker grants, CoreFileKit/crypto calls, and ImageKit decoding/encoding are host adapters. Pillow supplies real, valid encoded fixture bytes through the old and new SDK boundaries. This does not measure device ImageKit codec compatibility, real PhotoViewPicker output, actual OS permission lifetime, HEIC/DNG support, or public-IPv6 connectivity.

## Results and positive/negative comparison

Five real synthetic fixture formats are used: EXIF orientation/artist JPEG, alpha/text PNG, two-frame GIF, lossless WebP, and tagged TIFF. Fifteen main branches cover five original photo selections, five compressed photo selections, and five image selections through the existing document path. Expected compressed MIME/extension, size, hash, decodability, dimension limit, and unchanged source bytes are checked against the actual staged snapshot. Short writes are exercised rather than assumed to write a full buffer.

| Check | Baseline | Corrected |
| --- | --- | --- |
| Original mode, bytes/metadata/hash retained | 5/5 pass | 5/5 pass |
| Compressed mode, valid output and matching snapshot MIME/size/hash | 10/10 pass | 10/10 pass |
| Every successful fixture output byte/hash/MIME/size/metadata identical to baseline policy | Reference | 15/15 identical |
| Conversion directories retained after 10 successful compressed handoffs | 10 | 0 |
| Two-image batch: snapshots valid after handoff | Pass, 2 caches remain | Pass, 0 caches remain |
| Granted source URI including escaped query passed to source open | Pass | Pass |
| Decode/encode/empty-output/write failure | Rejects; no false success | Rejects; no false success |
| Output-open failure after mkdir | Rejects but leaves a directory | Rejects and removes current directory |
| Busy cache deletion followed by another close | Retry ownership lost | Retry succeeds |
| Second decode failure plus cleanup failure | Reports cleanup error | Preserves decode error; cleanup remains retryable |
| Explicit original selection after each codec/output failure | Original snapshot succeeds | Original snapshot succeeds |
| Expired source grant at snapshot open, injected at boundary | Rejects; no receipt | Rejects; no receipt |

The test does not silently fall back to original on a compressed conversion failure; the user retains the original-checkbox choice for an explicit subsequent selection. Input files are never rewritten. A smaller output is not required for every tiny fixture, because the existing compression policy is preserved rather than redesigned.

[baseline.json](baseline.json) and [corrected.json](corrected.json) record all input/output dimensions, MIME types, metadata, sizes, hashes, cache counts, source-file hashes, and eight fault cases. [comparison.json](comparison.json) verifies identical successful output between baseline and corrected code. `node --check`, Python compilation, and `git diff --check` also passed.

## Remaining verification and incident boundary

The repository's required runtime/UI/screenshot checks are incomplete. This environment lacks the Harmony API26 toolchain, generated native app rawfiles, and a controllable physical phone. The documented local dev environment was attempted in this investigation: initial read-only mise/buildx paths were redirected to task directories, after which Docker actually built until Debian package connections to `deb.debian.org:80` were refused, exiting 100 before the application started. The unchanged infrastructure blocker cannot be repaired by modifying image code. No current native UI URL or screenshot exists to report.

The existing Harmony owner must consume and compile the source, then verify both checkbox modes and conversion-cache release after staging through an already authorized real-device path. The remote phone uses public IPv6; no LAN, USB, HDC, phone control, user-image retransmission, or new permissions are assumed or requested. Real picker URI/MIME and OS grant evidence remain needed to identify a device-specific transfer failure.

The 08:22 screenshot contains a generic attachment error. The known 08:15–08:22 NAS window only includes `media/limits` 200 at 08:18:24, without a known failed status/upload ID. Neither that screenshot nor these host tests can causally attribute the incident to conversion, cache lifetime, auth, or networking. This source change fixes proven lifecycle/error-preservation defects while keeping the requested dual-mode functionality.
