# Shared computer-task file observation

Base: `2140d1b78719db1cbb6f477f63c50396f8893724` (PR79). This change only
consumes the existing read-only task delivery endpoint. It does not change
transfer bytes/queueing, Pi declarations, task execution, storage, authorization,
server routes, SDK schemas, or official Agent core.

## Delivered and remaining boundary

The existing task dialog previously showed only a summary. It now displays
persisted file delivery states and committed references, automatically continuing
server pages while visible. These are file delivery facts, not task/business
completion. An empty tail does not mean execution succeeded. Pending, reserved,
failed and canceled entries never expose a reference as a delivered file.

The initial observation-only checkpoint (`2ce6f81e`) incorrectly treated the
absence of a task-specific download URL as a required authorization gap. That
assumption is superseded: the user's existing Bot media permission is the
intended native access boundary. The current UI maps only committed references
to chat attachments with the **reader's explicit owner Bot**, and reuses native
attachment resolution, cards, asset preview, image lightbox and browser download.
No new server endpoint, task credential, per-file permission or HEAD preflight is
added. `ServeMedia → mediaUploadBot → authorizeBotAccess` checks the logged-in
user's `chat` permission; `OpenSeekable(botID, hash)` resolves that Bot's media.
Revocation, anonymous access and a different Bot still go through that route's
existing checks. Specialist readers do not fall back to the global supervisor Bot.
Existing delegation-scoped attachment handling is unchanged.

The API has no original filename. Cards/downloads use the delivery ID as a label,
not a claimed original name. The native preview host may offer its normal download
fallback when it cannot identify the file type. A committed delivery is a recorded
fact; neither a visible link nor reaching the last metadata page proves business
completion or current download success. There is no production permission change,
deployment or denied P1 repair in this patch.

## Shared host contract

- `useSessionTasks(scope, visible, paused?)` now returns `deliveries`, a computed
  `ComputerTaskDeliveryView` from `useComputerTaskDeliveries`.
- Pass `:deliveries="reader.deliveries.value"` when `reader` is a nested object
  (or the unwrapped `deliveries` ref when destructured) to the existing
  `session-task-results.vue`. Continue using `reader.back()` for host navigation.
- The same body mounts under the Web Dialog or a native HTML dialog. Hosts retain
  their accessible title/description, visibility, token identity and selection
  scope. No DialogRoot dependency or phone-only copy is introduced.
- Root and server-returned specialist sessions use their own readers. A parent
  relation error pauses/clears the downstream reader. Scope/identity/selection
  changes abort old requests; late success and failure cannot replace new state.
- Reads are serial, one bounded server page at a time, via the generated SDK and
  existing API base/auth. No page button, queue or task submission is added. Byte reads occur through
  the existing media route when native attachment behavior requests them. Refresh starts at the beginning so earlier state updates are visible.
  The last complete view remains during a refresh to preserve scrolling; errors
  clear it. 401/403/404 stop auto polling until explicit reload or scope change.
  Network errors may recover via the existing visible polling behavior.
- `botId`, `files`, `loading`, `loaded`, `error`, `refresh()` describe observation only.
  `loaded` means that read reached the current tail, not task completion. Reference
  direction distinguishes NAS receipt from PC receipt. Delivery IDs are labels,
  not claimed original filenames. No local/global persistent cache is introduced.

Harmony source/package identity was not changed or rebuilt. Its host must pass
the new optional prop to opt into this shared view; existing host markup still
works. The native-container test is a Vue/HTML integration test, not an installed
Harmony device acceptance test.

## Verification

Only synthetic data and the existing isolated development Server/PG were used.
The backend executable remains the fixed base product. No PC or paid model was
called. The fixture uploaded one 67-byte synthetic text file through existing
Media, then seeded a **new** session/operation with 18 delivery metadata rows
(16 committed references to that single asset, one pending, one failed). These
rows are not evidence that a PC executed or transferred 18 files. Existing rows,
source media and databases were retained.

| Check | Result | Evidence |
|---|---|---|
| Four related Vitest files, with `NODE_OPTIONS=--no-experimental-webstorage` | exit 0; 80 tests | `tests-final.txt` |
| Web typecheck | exit 0 | `typecheck-final.txt` |
| Affected ESLint | exit 0; 2 existing `one-component-per-file` warnings in task tests | `lint-final.txt` |
| UI contract guard | exit 0; 3 existing warnings and grandfathered baselines retained | `ui-contract.txt` |
| Web build | exit 0; existing >500 kB chunk warning retained | `build.txt` |
| Real Web → JWT Server → PG file reads | all 18 seeded rows from two HTTP pages; no manual page action | `browser-final.txt`, `browser-functional-proof.json` |
| Browser offline/reconnect | content clears and same selected detail recovers | same |
| Old route compatibility | synthetic route-only 404, stops polling; explicit reload returns to real backend | same, `browser-unsupported.png` |
| Shared Web/native HTML dialog mounts | no DialogRoot dependency; same scoped reader | included in 80 tests |
| 390px Chinese dark view | no horizontal overflow or page errors | `browser-narrow-proof.json`, `browser-narrow-zh.png` |

Unit tests also cover partial pages, malformed/nonadvancing pages, uncommitted
reference removal, earlier-entry updates/late inserts, refresh stability, 401/403,
A→B→A late successes/rejections, identity changes, hidden/disposed views and
subagents without computer-file reads. New tests join the existing Web CI step;
no rules/checks were removed or relaxed.

Initial local typechecking resolved workspace SDK links to a different checkout;
the local dependency links were corrected without a product/SDK change. An initial
attempt to normalize all scoped dependency links caused ELOOP; it was reverted
locally, then only workspace package links were corrected. SDK regeneration
produced zero diff. The initial browser run passed functional assertions but its
Chinese reload was reset to English by its init script; a separate narrow attempt
could not click the collapsed mobile sidebar. Those failures remain in
`browser-initial.txt` / `browser-narrow-initial.txt`. Corrected harness opens the
session first, then checks narrow layout; it does not claim mobile navigation QA.
Existing development font requests can emit Vite filesystem-allowlist warnings;
this patch did not change the dev server access policy.

Logs are text copies with worktree/evidence paths normalized, not raw byte-identical
originals. Private fixture IDs/token/config and SQL remain outside Git. Screenshots
show synthetic records only. `browser*.mjs` are reproducible read-only observation
scripts using explicit environment inputs; they require an already-seeded fixture
JSON (`bot`, `session`, `operation`) and the existing login token file, not a new
credential. Human QA, real PC execution, real phone integration,
NAS deployment and production legacy P1 remain unverified/unresolved.

## Native attachment follow-up (current product)

The unchanged backend runs the fixed `2140d1b` executable against the retained
isolated synthetic PG database. Web uses this worktree's current source. A second
69-byte 1×1 PNG was uploaded through native Media and one metadata row appended
to the same **test-owned** operation: now 19 rows, 17 committed references to two
assets, one pending, one failed. No business submission, PC execution, model call,
new identity or production access occurred.

- `native-tests-verified.txt`: six related Vitest files; final 95 tests, exit 0.
- `native-typecheck-verified.txt`, `native-build-verified.txt`: exit 0. Build retains
  its existing >500 kB chunk warning.
- `native-lint-verified.txt`: exit 0, five multiple-component test-fixture warnings
  (two existing in session-tasks and three in shared-body mount tests). No suppression.
- `native-ui-contract.txt`: exit 0; three existing contract warnings retained.
- `native-media-auth.txt`: actual loopback HTTP tests, exit 0: full/Range/If-Range/
  HEAD integrity, anonymous 401, wrong-Bot 404, revoked-user 403 then restored 206.
  Test identities/permissions are synthetic; the implementation was not changed.
- `native-browser-pass2.txt` / `native-browser-proof.json`: real Web → JWT Server
  → PG / native Media; downloaded text bytes and SHA-256 match the original,
  uploaded PNG decodes in the original lightbox, asset dock opens and closes the
  task dialog, anonymous native media GET returns 401. Offline recovery and
  synthetic old-route 404/reload still pass. All are reads of seeded task metadata,
  not evidence of Pi delivery or a real device task.
- `native-browser-narrow.txt` / `native-browser-narrow-proof.json`: actual 390px
  Chinese dark Web detail, without horizontal overflow. Not a Harmony build.

Screenshots: `native-browser-results.png` (committed downloads),
`native-browser-image.png` (existing lightbox, 1×1 synthetic image),
`native-browser-open.png` (native dock's unknown-name preview fallback),
`native-browser-narrow-zh.png` (shared body narrow view). Existing isolated
workspace availability notices are retained, not presented as fixed here.

The initial native browser harness assumed the browser would not add `.txt` to a
MIME-derived download filename, then used an incomplete English placeholder
label. Both harness failures remain in the two initial native logs. A real UI
failure then exposed the gallery close control being clipped by the task scroll
body. The native lightbox now accepts an optional teleport target (default still
`body`); this caller targets its closest dialog, outside the scrolling body and
inside a native HTML dialog's top layer. The corrected real-browser close click
passes, without forced clicks. These failures are not hidden by final successes.
Two initial typecheck logs document fixture/argument fixes; no strictness was
relaxed. Evidence copies normalize local paths only; originals remain private.

### Shared host integration

Continue passing `reader.deliveries.value`; `botId` comes from that reader scope,
not a UI-selected global Bot or model argument. The shared body needs no new API
or mobile-specific downloader. Hosts with an existing `openAssetPreviewKey`
provider can handle `@asset-opened` to dismiss their task shell (Web does so).
Without that host, the native attachment component uses its existing browser
anchor download fallback. Images reuse the existing lightbox inside the closest
host dialog; downloads use the same resolver/API base/login. The fallback does
not claim a native phone permission, save dialog or package integration has been
verified. `reader.back()` remains the navigation contract.
