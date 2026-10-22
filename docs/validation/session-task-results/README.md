# Persisted task result observation

Dependency: compact-result #12, integrated through
`828a35ec8d7e634495915c358f1972c7beaca56a`, on main
`75a70fe961808ed5ec2284767cb92ea85f30890f` (#11). The initial review/mobile
checkpoint `20d91381cc107eb917debf4ce6b89c14c5370401` remains in history.
Merge `44d3a241b50f594079958b06370a50cfc22fe4ec` adds the dependency's
connection-preflight classification without changing the frozen task API.
The next saved checkpoint `7c6fa8a0b5e18f127414d05b0ecbbc9f5c0fd374` adds
immediate auth-event cleanup and passed Go CI, ESLint and Docker PR. A further
normal merge integrates #12's `46b4a8a6`/`bcd5ac43` preflight/OAuth diagnostics.
That `a0c1c938` source/candidate checkpoint remains preserved. Merge
`98dd7a210f06339c3448749b4666c0210580b258` integrates the fixed `31e44e63`
dependency, followed by the narrow OAuth SDK path-declaration correction below.
This is a stacked, read-only HTTP/Web change. No deployment or promotion was
performed.

## Contract and scope

[API contract](../../session-tasks-api.md). The two existing GET endpoints now
project validated persisted version-1 Pi facts and optional bounded final
assistant text. They do not collect results from a PC. Old records remain
unknown; no complete log, artifact URL, cancel/retry action or new migration.
The original operation store write paths, JSONL helper and official Agent core
are unchanged. One shared pure validator was extracted from #12's Go adapter.
The CommonJS helper uses #12's file-specific whitelist for four Node builtins;
its contents are unchanged.

The source tree had no Web consumer of this task API. The Web increment composes
existing Dialog/Item primitives next to the conversation controls:

```text
Chat [Tasks] -> task list [Next page / Back to newest]
            -> [Back] task details [Close]
               process / Pi outcome
               cancellation intent (if recorded)
               final assistant summary OR unavailable
               business outcome unverified; attachments unavailable
```

Task responses live only in the mounted view, outside persistent/shared query
caches. Five-second refreshes run only while the view is visible. Close, hide,
scope/identity changes, denied/failed reads and unmount clear display content;
generation checks reject late responses. Only the generated SDK is called.
No raw message/media fallback, HTML rendering, URL expansion or file action.
Mobile implementation is outside this PR.

## Initial checkpoint verification (2026-10-22 UTC, `20d91381`)

- Go race: `internal/sessiontask`, `internal/handlers`, `internal/agenvo`,
  `internal/computertask`, `internal/auth`, `internal/server`, `internal/apperror`.
- Tests cover success/failure/abort/incomplete, no-start, empty/truncated output,
  protocol/output-limit failure, legacy/future version, mismatched/missing
  receipt, contradictory success, size bound, live late result across a cursor,
  current grants, ancestor ownership, target scope/fingerprint and no dispatch.
- Real Echo/JWT/Problem handler tests carry a displayable synthetic completion
  through 200/401/403/404/400/405 and revocation paths, with `no-store` throughout.
- PostgreSQL 18 isolated integration: owned ancestor tree and stable keyset
  pagination pass. No schema change; development database remains schema 170.
- Current native Server rebuilt and restarted, Vite Web and PostgreSQL healthy;
  authenticated login/readiness checks pass both directly and through the proxy.
- 17 real authenticated HTTP scope/pagination/revocation checks pass. Another
  16 list/detail GETs cover 8 persisted outcome scenarios; no internal fixture
  fields leaked, every response was `no-store`, and operation state/revision/
  cancellation/details digests were unchanged across GETs.
- Browser: Chromium, actual Vite app at `http://127.0.0.1:18082`, synthetic Bot
  `/bot/session-tasks-synthetic`. Login, chat task entry, list, completion detail,
  back, native unknown/unavailable, close, Chinese narrow (480px), dark mode and
  a real synthetic runtime-grant revocation were exercised. The next detail
  refresh returns 404 and removes the previously visible text. No page errors.
  A separate run inserted 21 temporary synthetic child rows, exercised actual
  page two and return-to-newest, then an empty persisted conversation. Loading
  was captured by delaying (not replacing) the actual HTTP response. All
  temporary paging/empty-session rows were removed afterward.
- Vitest: 127 assertions across the new lifecycle/pagination/race tests and the
  existing API/SSE error suites pass. SDK typecheck and production Web build pass.
- ESLint: 0 errors, 18 existing warnings. UI contract guard passes with 3
  existing warnings. Affected Go lint passes. Generated OpenAPI/SDK checked.
- Full Web typecheck remains report-only: 442 errors outside this increment
  (the prepared upstream baseline already reports 443). No assertion or type
  rule was disabled to conceal them. The new task component/composable has no
  type errors. The original chat-pane errors are outside the added entry.
- `check-agent-core-boundary.sh bf0318c5...` passes; full runtime/contextview tree
  diff is empty. ComputerOperationStore writes, compact JSONL helper and
  migrations have zero diff against #12.

## Dependency integration and identity regression (2026-10-22 UTC)

- Integrated the exact #12 `2316d2c` through a normal merge, without rewriting
  the initial checkpoint. No change to task routes, schema, fields, cursor or
  result projection; the frozen mobile contract remains compatible.
- A mounted-component regression reproduced that an auth-clear notification
  did not immediately abort a pending detail read. Login emits this notification
  before replacing the token; same-tab 401 handling need not emit a storage
  event. The task dialog now directly listens to the existing auth event and
  closes synchronously, invalidating pending requests before parent teardown.
- Six real-component tests cover literal HTML/Markdown/URL rendering, a
  cross-tab token replacement, and login/logout/unauthorized/token-cleared
  events. Sixteen additional composable cases cover late list/detail successes
  and failures across identity, Bot, session and PC selection changes. Together
  with existing lifecycle and API/SSE error suites, 162 tests pass. Mocked
  requests intentionally settle even after abort to exercise generation guards.
- Rebuilt/restarted the combined native Server; direct/proxy authenticated
  readiness passed. Go race and affected Go lint passed for `cmd/internal/core`,
  `internal/computertask`, `internal/sessiontask`, `internal/handlers`, and
  `internal/agenvo`, including preflight and real JWT/handler denial/no-store
  regressions. PostgreSQL owned-tree/cursor integration passed again.
- Repeated 17 actual HTTP scope/pagination/revocation checks with a displayable
  completion and 16 list/detail GETs across 8 outcomes. GETs did not change
  operation state, revision, cancellation or details digests.
- Current Chromium runtime rendered a synthetic summary containing HTML,
  script text, Markdown and URLs literally: zero generated active elements,
  zero probe URL requests, zero script execution and zero page errors. Delayed
  an actual authorized detail response, emitted the app's auth-clear event,
  then released the response: the dialog closed and the old summary stayed
  absent. All three task requests were GET; the ledger digest was unchanged.
  This is a synthetic auth-event browser check, not a real second-account
  login or production test. Component tests separately isolate the mounted
  dialog from parent teardown. The temporary summary was restored afterward.
- Production Web build, changed-file ESLint and UI guard passed (3 existing UI
  warnings; existing bundle-size warning). Full Web
  typecheck still reports **442 errors**, with none in the task component,
  composable or their tests. This is not an all-green typecheck claim.
- Agent runtime/contextview still matches official `bf0318c5...` with zero diff.
  No operation write-path, parser, migration, credential or execution changes.

## Earlier #12 integration (2026-10-22 UTC, `bcd5ac43`)

- Added the exact fixed dependency through another conflict-free merge on the
  same branch; the `20d91381` and `7c6fa8a0` checkpoints remain ancestors. This
  adds standard MCP preflight structured errors and bounded OAuth refresh
  diagnostics from #12. The task routes, result projection, OpenAPI/SDK and
  frozen mobile contract have zero diff from `20d91381`. Web source is unchanged
  from `7c6fa8a0`; its 162 passing tests and 442 full-typecheck errors remain the
  applicable frontend verification, not a claim that typecheck is green.
- Repeated Go race tests for `cmd/internal/core`, `internal/computertask`,
  `internal/sessiontask`, `internal/handlers`, `internal/agenvo` and
  `internal/mcp`: all passed, including standard MCP structured preflight and
  safe OAuth category/transport/logger regressions. Task authorization,
  revocation, no-store and GET-no-dispatch tests passed on this combined tree.
  Affected Go lint passed for the same six package trees.
- Rebuilt/restarted the native Server; direct and proxy login/readiness passed.
  Repeated 17 real HTTP permission/pagination/revocation checks and 16 outcome
  GETs: all passed with unchanged ledger digests and no-store throughout.
- Repeated current-runtime Chromium list/detail/back/unknown, dark Chinese
  480px and real synthetic grant revocation checks. Also repeated literal
  adversarial-summary and delayed-auth-response checks: zero page errors,
  active content, probe URL requests or GET ledger changes. The literal/auth
  screenshots below were refreshed from this combined runtime. All temporary
  synthetic detail/grant mutations were restored.
- No OAuth credentials were operated on by this task. OAuth request/response
  behavior is covered by isolated automated fixtures in #12; there was no
  production credential repair, new task submission or deployment here.
- Official Agent runtime/contextview boundary remains zero diff.

## Prior #12 integration (2026-10-22 UTC, `31e44e63`)

- Preserved `a0c1c938` and merged the exact fixed dependency on the same branch.
  This includes the unknown-operation/preflight regression and current OAuth
  transaction verification. There were two textual conflicts: the CommonJS
  lint exception now uses #12's stricter builtin whitelist, and the SDK index
  was regenerated from combined OpenAPI rather than choosing either side.
- Contract comparison against `20d91381`: both task GET paths and all three
  task definitions are identical, as are the 13 generated task SDK declarations.
  All three task locale blocks and the task component/composable, including
  identity cleanup and late-response guards, are unchanged. OAuth definitions
  coexist with task-result definitions in the generated SDK.
- Full Web typecheck first exposed four new OAuth caller errors because three
  existing routes omitted their required `bot_id` path annotation. Added only
  those three annotations (discover, authorize, status), regenerated Swagger
  and SDK, and reran SDK/Web typechecks. SDK passes; Web returns to exactly the
  prior **442 diagnostics**, compared after removing shifted line numbers.
  This corrects documentation/types, not authentication behavior, and does not
  make the full Web typecheck green.
- Go race passed for core, computertask, sessiontask, handlers, agenvo, mcp,
  apperror and server. Affected Go lint passed. The combined frontend suite has
  **173 passing tests**, covering task identity/late-response behavior, OAuth
  transaction flow, API auth and API/SSE errors. ESLint has zero errors and 18
  existing warnings; UI guard passes with 3 existing warnings. SDK typecheck
  and production Web build pass.
- Rebuilt/restarted the actual native Server with current Vite source at
  `http://127.0.0.1:18082`; direct/proxy authenticated readiness passed. All 33
  actual task HTTP checks passed, including scope, revocation, pagination,
  outcomes, no-store and unchanged ledger digests. Chromium repeated task
  list/detail/back/unknown, Chinese dark 480px, grant revocation, literal markup
  and delayed-auth-response checks. No page errors, active summary elements,
  probe URL requests or task GET writes. Fresh literal/auth screenshots were
  captured and inspected; synthetic mutations were restored.
- OAuth-specific automated tests ran, but this integration does not claim
  production OAuth verification. Task-route no-store results are not evidence
  for the separate OAuth namespace. No credentials, grants, database schema,
  operation write paths or Agent core were changed by the integration fix.
- This checkpoint is for integration review and ordinary PR CI only. No new
  candidate workflow was dispatched; the earlier `a0c1c938` candidate remains
  separate. No merge into main, promotion or deployment was performed.

## Combined OAuth and verified Range integration (2026-10-22 UTC)

- Preserved `fbad9a74` and integrated #12 `828a35ec` with merge `26bf2fd4`,
  then #21 `8b5db5a7` with merge `0d38a587`. Both merges were conflict-free.
  The Range storage/media/HTTP implementation is identical to #21; OAuth
  differs from #12 only by the three previously reviewed `bot_id` annotations.
  No rewrite or routing architecture was added. Main remains unchanged.
- Both task paths and all three task definitions still equal `20d91381`.
  Task API docs, generated SDK, projection, Web component/composable and
  identity/late-response guards are unchanged from `fbad9a74`. No migration,
  operation write-path, parser or official Agent core change was introduced.
- Go race passed for localfs, fallback, media, sessiontask and server, plus
  handler tests matching `Test(SessionTask|MCPOAuth|Media)`. These cover real
  JWT/Problem middleware, task permissions/revocation/no-dispatch, OAuth
  rejection/cache boundaries and warm media authorization. Affected Go lint
  passed. PostgreSQL owned-tree/stable-cursor integration passed.
- All 173 frontend tests passed again; SDK typecheck and UI guard passed
  (3 existing UI warnings). Frontend source and generated types did not change;
  the previous full Web typecheck result remains **442 existing errors**.
  This integration does not claim a green full Web typecheck.
- Rebuilt/restarted the native development Server and verified direct/proxy
  login and readiness. All 33 actual task HTTP checks passed: outcomes, scope,
  cursor, revocation, no-store and unchanged operation ledger digests.
- Before and after a further process restart, 18 OAuth HTTP checks each passed:
  anonymous 401 on status/discover/authorize/unknown, authenticated status 200,
  denied Bot/unknown-route 404, method 405, both callback prefixes 400/405/404,
  and unchanged neighboring-path cache policy. These are synthetic development
  requests, not a production OAuth authorization or credential-repair result.
- Eight media HTTP checks each passed before/after restart: cross-block/suffix
  206, If-Range mismatch/full 200, unsatisfiable 416, anonymous 401, HEAD, a
  client disconnect and resumed download whose complete SHA-256 matches the
  uploaded synthetic original. Operation ledger digests remained unchanged.
- The isolated 200/512 MiB read-volume test passed: 64 KiB warm responses read
  approximately 1 MiB; first/new-provider reads additionally scan the complete
  original. Measurements are own-process Linux read syscall bytes, not physical
  disk I/O, separate Server process accounting or NAS performance. Warm checks
  validate requested blocks; they do not scrub untouched blocks. Cold full
  verification, canonical originals and fallback integrity behavior remain.
- Chromium repeated actual task list/detail/back/unknown, Chinese dark 480px,
  grant revocation, literal adversarial markup and delayed-auth-response
  checks. Zero page errors, active summary elements, external probe requests or
  task GET writes. Current screenshots were inspected. Synthetic detail/grant
  mutations were restored; no PC, model or chat execution was submitted.
- Browser attachment selection and an authenticated cross-block Range request
  through the Vite proxy passed. The selected file was not sent as a message.
  [Combined attachment preview](combined-range-selected.png) shows this UI
  state; its HTTP byte check is separate from the screenshot.
- Candidate publication is handled separately after this fixed tree's ordinary
  CI succeeds. No merge into main, promotion or production deployment is part
  of this verification. The combined candidate still needs production HTTP
  acceptance. Task attachments, cancel and retry remain unavailable.

All fixtures and screenshots are synthetic. No real model, Pi or Windows task
was launched. The occasional workspace-unreachable toast belongs to the
unavailable optional local workspace; it is retained in screenshots and does
not prevent persisted task reads. Screenshots prove the UI only, not real Pi
execution or human QA. No private production configuration, logs or addresses
are included.

## Screenshots

- [List](result-list.png): native and computer tasks are distinct.
- [Completion detail](result-light.png): process/Pi facts, bounded final text,
  cancellation intent and unverified business outcome are separate.
- [Native unknown](result-unknown.png): no fabricated summary.
- [Dark, narrow, Chinese](result-dark-narrow-zh.png): no horizontal overflow.
- [Revoked detail](result-revoked.png): old text cleared after an actual 404.

- [Loading](result-loading.png): actual request delayed, content not fabricated.
- [Page two](result-page-two.png): real keyset continuation and newest-page return.
- [Empty](result-empty.png): existing conversation with no task records.
- [Literal summary](result-literal-summary.png): integrated runtime displays
  adversarial markup as text without activating it.
- [Auth clear](result-auth-cleared.png): integrated runtime removes task content
  and ignores the delayed authorized response after the auth-clear event.
