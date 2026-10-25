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

Log copies normalize only local repository/verification paths. Screenshots contain synthetic content. Full local build output is retained in the original verification directory. This is an automated isolated UI/backend check, not human QA, real provider generation, Windows/Herdr execution or production validation. The original user's run has not been retried or modified. No images or deployment channels are published by this change.
