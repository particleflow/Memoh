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
evidence will be recorded separately. Browser persistence does not prove an
on-device upgrade; signed installation, real device picker/opening, live NAS
transfer, and end-to-end Bot/Agent execution remain unverified.
