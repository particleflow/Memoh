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

**Opening/downloading a computer-task result is NOT implemented by this patch.**
The fixed endpoint exposes hash/size/MIME/direction but no authorized attachment
URL (`internal/computertask/delivery_page.go`). The existing browser attachment
resolver supports Bot-scoped media and delegation-scoped attachment URLs; the
former does not revalidate task/session/target scope. The task transport route
requires a device transfer principal, not the current Web login. Reusing that
credential, inventing a delegation URL, or widening a task reference to Bot/hash
would not be a frontend-only fix under the current scope. No such fallback or
nonfunctional download button was added. Existing conversation attachments and
specialist delegation attachments keep their current open/download behavior.
A compatible task-scoped attachment read reference remains the specific missing
server contract, not a generic file transfer or pagination blocker. No production
access, permission change, deployment, or denied P1 repair was attempted.

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
  existing API base/auth. No page button, queue, task submission or byte download
  is added. Refresh starts at the beginning so earlier state updates are visible.
  The last complete view remains during a refresh to preserve scrolling; errors
  clear it. 401/403/404 stop auto polling until explicit reload or scope change.
  Network errors may recover via the existing visible polling behavior.
- `files`, `loading`, `loaded`, `error`, `refresh()` describe observation only.
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
credential. Human QA, real PC execution, real phone integration, task download,
NAS deployment and production legacy P1 remain unverified/unresolved.
