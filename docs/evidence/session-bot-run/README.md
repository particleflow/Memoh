# Native Bot run visibility

The existing Tasks dialog listed subagent and computer operations, but omitted the parent Bot's direct native run. The shared execution view now reads the existing authorized conversation WebSocket projection and the exact run/turn transcript. It shows the latest recorded run ID, status, timestamp, visible content and tool calls. It introduces no API, ledger, execution or subscription. Historical run browsing beyond the latest native snapshot is not added.

A terminal run with a missing tool receipt displays **Tool result unconfirmed**; it neither claims delivery nor reruns the tool. Confirmed receipts use the chat's existing exact-run reconciliation. A disconnected, gapped or unsubscribed snapshot is not presented as current. Another Bot/session cannot reuse the cached view. The body mounts without DialogRoot for shared hosts; this is not a rebuilt or installed Harmony package.

## Verification

Base: `8a2422935916f56b5bacfac2f991824d1cb9bd9f`. Only shared Web changes for this checkpoint. Official runtime/contextview/SDK protocol and backend unchanged.

- `pnpm exec vitest run --project web` with session-bot-run (component and helper), session-tasks, runtime-client and realtime tests: **81 passed**, exit 0. See `tests.log` for exact files/count/duration.
- Web and SDK typecheck: exit 0. Shared UI contract: exit 0, three pre-existing warnings retained. Vite build: exit 0, existing large-chunk warning retained; `build-summary.log` is explicitly the final 12 lines, not the full original log.
- Existing isolated Server/PostgreSQL/native WebSocket, current Vite source: persisted synthetic aborted image run displayed with the same run/call IDs; reload restored them without dispatch. Input and absent receipt were shown truthfully. Desktop and 390px dark screenshots inspected. Browser proof records only runtime_subscribe messages, zero business mutations, zero native-device reads and no page errors.
- The previous maintenance test verified actual Tasks HTTP 503/network-error recovery. The direct image run's Tasks endpoint returns 200/empty because that API lists subagents/PC; the new view does not relabel this as a backend failure or fabricate a child task. Existing synthetic mixed list and anonymous 401/no-store checks still pass; pre-existing ledger digest unchanged.
- Initial browser failures were test errors: the expected translated label omitted its prefix, and our synthetic history used `args` instead of the official tool-call `input`. Both failed logs are retained. Only the owned synthetic fixture was corrected; no product or existing data was changed to satisfy it.

Log copies normalize local repository/verification paths, trim trailing line whitespace and redundant final blank lines; local originals are retained. Screenshots contain synthetic content. Full local build output is retained in the original verification directory. This is an automated isolated UI/backend check, not human QA, real provider generation, Windows/Herdr execution or production validation. The original user's run has not been retried or modified. No container images or deployment channels are published by this change.

## Unified candidate integration

Normal additive commits preserve all previous checkpoints:

- `fd8418ec1ec792672f67417e5023001f87f5ab69`: parent Bot run view.
- `3c29adb0`: exact cherry-pick of `79047657f2cecef4b11bed7189c54ef82c91b10a`, three retired-Agenvo SELECT filters and generated Go, tests/evidence.
- `ed450430c119b54e5a673910f75145a98b07ca67`: exact cherry-pick of `a35b9127d8185313bb4931a900d485f3a3d6f700`, image-card terminal/receipt preservation. Source branches were not merged wholesale.

No conflicts. Schema stays 176. The 8a Windows native 10-minute aggregate/35-minute job budget is unchanged. Official agent runtime, contextview and packages/runtime have zero diff from 8a. This candidate does not contain the separately owned attachment upload/download redesign.

Combined checks:

- 170 tests in eight relevant Web files plus five existing runtime-integration tests: all passed, exit 0. The existing task-observation CI step now explicitly includes these regression files; no gate or assertion removed.
- Web typecheck/build, SDK types and the exact changed-Web `lint-staged --diff 8a...HEAD` check passed. Full Web ESLint: 0 errors, 39 warnings. UI contract retains three warnings; build retains its large-chunk warning.
- Desktop's package script initially failed because that package has no local `vue-tsc` executable in this existing environment. Running the **same desktop tsconfig** using the already-installed Web package's `vue-tsc` passed (exit 0); no install, dependency change or check relaxation.
- Combined `go test -p=1 -tags=integration -count=1 -v -timeout=180s -run '^TestNativeRuntimeCutoverStopsLegacyAutomaticOAuthPaths$' ./internal/db`: PASS in 3.177s, no skip. Uses the owned isolated loopback PG, with a uniquely named fixture DB. Native and unrelated active targets remain; retired target omitted; old OAuth/transport calls zero; original history and unknown transfer preserved.
- An additional `golangci-lint run --concurrency=2 --build-tags=integration ./internal/db` returned 19 diagnostics in eight files whose blobs exactly match 8a. The full diagnostic output and unchanged-file comparison are retained. No baseline analyzer rerun is claimed, no rule suppressed, no unrelated test changed. This extra-tag lint is distinct from the repository's ordinary CI lint invocation. The ordinary `golangci-lint run --concurrency=2 ./internal/db` passed with no diagnostics (exit 0).
- Current Web plus unchanged native-run Server APIs, real PG and native WebSocket were rechecked after integration: exact aborted run and tool-call identity survive reload; missing receipt remains unconfirmed; no execution, provider/device call or mutation. The running Server was not rebuilt for the unrelated retired-target SQL; that integration was checked by the real PG test above, and the source branch separately supplies its real UI evidence.

Remaining acceptance: final exact-head remote CI; parent-controlled candidate publication/deployment; read-only observation of the user's original aborted run after deployment. This turn did not access that run or retry generation. Harmony's host can embed `session-bot-run.vue` with `{ botId, sessionId, visible }` and the existing shared chat store; no HAP build/install was performed. The view covers the latest native run, not a new historical run index or every external runtime. Raw tool responses remain subject to the existing authorized conversation's visibility; this is not a general secret-redaction feature.
