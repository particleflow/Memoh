# Persisted task result observation

Dependency: compact-result #12, fixed `f38b59c556da0d774d6f5adc089d2fbbfd2dcc4b`,
on main `75a70fe961808ed5ec2284767cb92ea85f30890f` (#11). This is a stacked,
read-only HTTP/Web change. No deployment or promotion was performed.

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

## Verification (2026-10-22 UTC)

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
