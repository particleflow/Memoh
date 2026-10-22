# Persisted task result observation

Dependency: compact-result #12, integrated through
`bcd5ac439b9d7bd320c6dd53edd5e6f9979482a1`, on main
`75a70fe961808ed5ec2284767cb92ea85f30890f` (#11). The initial review/mobile
checkpoint `20d91381cc107eb917debf4ce6b89c14c5370401` remains in history.
Merge `44d3a241b50f594079958b06370a50cfc22fe4ec` adds the dependency's
connection-preflight classification without changing the frozen task API.
The next saved checkpoint `7c6fa8a0b5e18f127414d05b0ecbbc9f5c0fd374` adds
immediate auth-event cleanup and passed Go CI, ESLint and Docker PR. A further
normal merge integrates #12's `46b4a8a6`/`bcd5ac43` preflight/OAuth diagnostics.
This is a stacked, read-only HTTP/Web change. No deployment or promotion was
performed.

## Contract and scope

[API contract](../../session-tasks-api.md). The two existing GET endpoints now
project validated persisted version-1 Pi facts and optional bounded final
assistant text. They do not collect results from a PC. Old records remain
unknown; no complete log, artifact URL, cancel/retry action or new migration.
The original operation store write paths, JSONL helper and official Agent core
are unchanged. One shared pure validator was extracted from #12's Go adapter.
The CommonJS helper has a file-specific ESLint import exception; its contents
are unchanged.

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

## Latest #12 integration (2026-10-22 UTC, `bcd5ac43`)

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
