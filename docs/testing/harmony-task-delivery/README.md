# Harmony shared task attachments

The phone source now pins complete shared Web/SDK commit
`91af53f43566cb827d1fc58d18102e8ad29383bc`. This includes the PR79 task-delivery
view and the earlier observation recovery. All 5,738 archive Git blobs matched
that commit. The existing UI submodule and dependency lock are unchanged.

The existing platform builder adapts three shared components. Task-result save
buttons and attachment cards without an asset dock call `saveOriginal`; task
images supply `originalBlob` to the existing lightbox. The lightbox retains its
verified Blob when polling replaces an item with the same scoped media identity.
It still releases the Blob on identity change or closure. No phone component,
native method, endpoint, permission model, or transfer service was added.

The real native command surface is `download.fetch`, `download.save`, and
`download.abort`. There is no generic native open-file command. Image/text
previews use the existing shared preview host, while saving uses the existing
native document picker path. Owner Bot comes from the task reader; it never
falls back to the selected chat Bot. Existing delegated attachments retain
their explicit delegation URL and fail closed on scoped denial.

## Checks performed

- First shared/Bot run: 186 passed, 3 failed, 14 files. Failures were the upstream
  DOM fixtures expecting raw image URLs and omitting the phone original-reader
  callback. After phone adaptations, the 3 affected suites passed all 17 tests.
  The unchanged Bot list/create/progress, model options, settings interaction,
  compaction models, delegation and task readers passed in the first run.
- Full `@memohai/web typecheck` passed after the final gallery change.
- Changed shared components linted with 0 errors and 6 inherited formatting
  warnings. The broad UI contract guard reports 4 violations in unchanged voice
  components; running it against the previous context produces identical output.
  This is not a claim that every repository check is green.
- Existing frontend/native command test and upload preparation test passed
  against the actual pinned SDK/store/phone adapters with controlled boundaries.
  Actual native source with Node filesystem/crypto/RCP/picker adapters saved and
  hashed 8 MiB, verified scoped denial/no fallback, bounded I/O, cancellation,
  hash mismatch, and cleanup. It did not run on Harmony hardware.
- Actual phone Vite UI in Chromium at 390×844 passed task hierarchy, pending
  delivery status, downstream running/failed output, transient read recovery,
  parent denial, verified synthetic PNG preview, unchanged Blob across polling,
  and explicit-owner native fetch/save command routing. The API was intercepted
  synthetic data and the OS picker was a controlled seam. One login survived
  page reload and browser-process restart with configuration unchanged.
- Source manifest: 116 entries / 125 public files, no missing held source paths.
  The 19 historical later edits remain unverified; this is not a reconstruction
  of the inaccessible historical Git tree.

`prepare-platform-tests.py VALIDATION_CONTEXT` applies exact-count guarded
fixture changes to a freshly extracted phone context. It mocks only component
file I/O boundaries. The separate native/upload tests retain real production
adapter code. Successful logs and the initial failed shared run are adjacent.

## Boundaries

The shared reader currently aggregates all pages and restarts from page one on
refresh. Single/small-result checks passed; large-ledger memory/poll cost is not
validated. The coordinating task explicitly confirmed this existing design
tradeoff does not block the single-attachment package. No pagination UI or
separate phone reader was introduced. A last page remains distinct from task
or business completion, and old-server 404 remains an error/unsupported state.

Native save still requires a positive known size, as before; zero-byte originals
are not covered by the native save tests. No cross-process resume is claimed.

This source checkpoint retains `cloud.begonia.workassistant`, version `1.0.7`,
code `1000007`, existing permissions and login storage. Compilation/package
evidence is recorded below. Browser persistence does not prove an
on-device upgrade; signed installation, real device picker/opening, live NAS
transfer, and end-to-end Bot/Agent execution remain unverified.


## Unified unsigned package

Source commit: `f6403deaded6e76c0788aa66cffcaac5fef0badd`.
One build using the retained official Linux API26 SDK `26.0.0.105` completed
with compile and qualification exit code 0. The build verified all 786 Web
resources, the 95-file native source snapshot, compiled ArkTS ABC, LibRaw
licenses, both native ELF architectures, and unchanged toolchain metadata.
No SDK download, signing, installation, Windows transfer, or NAS change occurred.

- File: `memoh-harmony-1.0.7-1000007-f6403de-api26-unsigned.hap`
- Size: `36908394` bytes
- SHA-256: `da95c24335f6074852cc621edde5b6362a6604239fb582b978817d107fdc964c`
- Source fingerprint: `4da9a6a6b3147328b685cc1156ebddf4eff8cb21bddeba10b29cbd4893da5239`
- Identity: `cloud.begonia.workassistant`, `1.0.7`, version code `1000007`.
  The module/pack metadata and native library bytes match the previous 1.0.7.
  This code exceeds the reported installed 1.0.6 code `1000006`; successful
  signed upgrade and retained login on the phone still require device testing.

The new file was saved as a distinct private Library artifact and read back
using the current official Library flow on the same cloud environment. Exact
size/SHA-256, byte equality, ZIP CRC and Library identity metadata passed.
Private Library identity is retained with the artifact and not published here.
The prior named HAPs, source ZIPs, private configuration and SDK remain intact.

The newly compiled Web bundle also passed actual Chromium Bot list/settings,
create/cancel, duplicate-submit single request, 403 refusal, 403/404/500 detail
handling and return-to-chat checks at 390×844 with synthetic API interception.
No page errors occurred. This checks client behavior, not successful live Bot
creation or Agent execution. See `compiled-bot-navigation.json`,
`native-build-summary.json`, and `hap-verification.json`.
