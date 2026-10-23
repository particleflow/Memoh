# Child run output and scoped delegation downloads

Product commit: `ff0f1736676f35f72753c4b313c57104a4c205aa`.
Product tree: `4edd6fff3908f253c99cce29e7012a91ed228d8a`.
Stacked on PR #70 at `6ace867cd87936347ea8f65f007030fdd6965249`;
that checkpoint and its evidence remain unchanged.

## Behavior and boundaries

The existing task reader previously returned `unknown` for every child session,
including a child whose own persisted run was already completed. The read
projection now uses the existing latest-session-run query and its reset/deletion
fences. It never borrows the parent Bot's completion. `run_id` identifies the
projected run; a later accepted run cannot reuse the previous run's text.
Unknown/missing states remain unknown, and `business_state` remains unknown.

The latest visible message must belong to that same run and be a visible,
non-feedback assistant message. Only text parts are projected, at most 16 KiB
of valid UTF-8, from an envelope bounded at 1 MiB. Reasoning, tool bodies and
metadata are not exposed. This is not general secret redaction: assistant text
may itself contain sensitive content and remains behind the existing session
authorization. Text is rendered literally. A newer tool/non-assistant message
does not cause fallback to an older assistant message. The view polls persisted
output while open; this is neither token streaming nor a full execution log.

Web opts into an optional shared-component download capability. Only a returned
attachment whose URL exactly matches the currently selected source session and
delegation is eligible. The existing scoped HEAD endpoint checks authorization
before the browser downloads through the same scoped GET endpoint, which checks
again. There is no broad Bot/hash fallback, whole-file Blob buffering or new
file service. Pending duplicate clicks coalesce; late HEAD responses are ignored
after scope/selection/visibility changes. The component does not claim a
download succeeded merely because it opened the browser download.

Hosts that do not supply `downloadAction` remain observation-only. Subagent task
details still do not invent attachments; actual downloads are on the specialist
handoff's returned references. This does not implement arbitrary child artifact
extraction. Existing delivery acceptance remains separate from supervisor
processing or business success. No new cancellation or business-retry action
was added.

The existing task poller stops automatically after 401/403/404. Explicit Reload
can try the same read again. Scope changes retain the existing invalidation and
AbortController behavior. There is no new task ledger, queue or log service.

## Runtime and browser verification

The cloud-only environment reused the isolated PostgreSQL database at schema
173 and the existing local workspace fixture. API: `http://127.0.0.1:18100`;
Web: `http://127.0.0.1:18102`. The queue was the in-process memory queue, **not
Valkey**. The model endpoint was a deterministic local HTTP/SSE test double,
not a paid model or a Windows Agent.

One new synthetic user request ran through the real supervisor Bot, actual
`delegate_to_bot`, specialist `spawn_agent`, child runtime and specialist `send`.
It uploaded and returned a 7,400-byte synthetic text original through the
existing media service. No task rows were preseeded. The seven model requests
and selected tools are recorded in [the call log](web-task-results-model-calls.json).
This proves the application integration, not autonomous real-model routing,
image generation or PC execution. The old PR #70 business task was not rerun.

Chromium opened the actual task dialog, specialist detail and downstream child
detail, downloaded the returned original, read the downloaded bytes and checked
SHA-256 `0d346e40c3f898dcec8424c14b0d34a6c003699666be2bd9509d877eabe68219`.
The child showed its own completed run and `DOWNSTREAM_VISIBLE_FINAL_42`.
Observation made no POST requests; the recorded 34 API responses were 200 and
there were no browser page errors. The browser's download navigation is verified
by the downloaded bytes, not counted as an application fetch in that list.

After the product commit, its exact source was built, the idle local server was
restarted once, HEAD health returned 200, and the same browser observations and
download were repeated without dispatching any business work. See
[runtime identity](web-task-results-final-runtime.json),
[browser proof](web-task-results-browser-proof.json) and
[browser output](web-task-results-observe-final.log).

Actual scoped file routes also verified full GET 200, matching If-Range 206,
stale If-Range full 200, anonymous 401 with no-store, and wrong-source-scope
denial. Withdrawing only this synthetic source session's ownership returned
404/no-store; restoring it returned the same bytes. Run count did not change.
This is source-session ownership withdrawal, **not a real non-admin Bot-grant
revocation test**. The separate HTTP fixture covers the existing grant-revocation
case. [File route proof](web-task-results-file-route-proof.json).

Screenshots were captured from the final product runtime and inspected:

| Evidence | What it establishes |
| --- | --- |
| [Scoped download](web-task-results-download.png) | Real specialist result and scoped download action |
| [Child completion/output](web-task-results-child-completed.png) | Own child run, plain visible text, polling/business boundary |
| [Chinese, dark, 420 px](web-task-results-child-dark-zh.png) | Shared detail layout and localized boundary |

## Checks and exact outcomes

Commands below ran locally in this cloud environment. No remote CI result is
inferred. Existing untouched full-suite/PG evidence belongs to earlier
checkpoints; it was not rerun or relabeled as a pass here.

| Command/check | Exit/result | Readable output |
| --- | --- | --- |
| `pnpm --filter @memohai/web exec vitest run` on `useSessionTasks`, `useBotDelegationDownload`, `session-tasks`, `bot-delegation-tasks` | 0; 4 files, 81 tests | [UI](web-task-results-ui-final.log) |
| `go test ./internal/sessiontask ./internal/db/postgres/store` | 0 | [Go packages](web-task-results-go-first.log) |
| Targeted `go test` for sessiontask and session-task/delegation HTTP tests | 0 | [Go/HTTP](web-task-results-go-targeted.log) |
| `go test -tags=integration -count=1 -v -run '^TestSessionTasksPostgresLatestChildRunAndMessage$' ./internal/db` | 0; real PostgreSQL, no skip | [PG](web-task-results-pg-final.log) |
| `pnpm --filter @memohai/web typecheck` | 0 | [Web types](web-task-results-typecheck.log) |
| `pnpm --filter @memohai/sdk typecheck` | 0 | [SDK types](web-task-results-sdk-type.log) |
| `GOGC=30 GOFLAGS=-p=1 golangci-lint run --concurrency=1 ./internal/sessiontask ./internal/db/postgres/store ./internal/handlers` | 0, no output | [Go lint](web-task-results-go-lint-verified.log) |
| ESLint on changed Web files | 0 errors; 2 existing test component warnings | [ESLint](web-task-results-eslint-final.log) |
| `node scripts/check-ui-contract.mjs` | 0; 3 inherited warnings | [UI contract](web-task-results-ui-contract.log) |
| `pnpm --filter @memohai/web exec vite build --outDir /tmp/web-management-checks/results-web-dist` | 0; existing chunk warning | [Build](web-task-results-web-build.log) |
| `go build -ldflags='-s -w' -o /tmp/web-management-checks/results-server-final ./cmd/agent` | 0 | [Server build](web-task-results-build-final.log) |
| sqlc v1.31.1, Swagger and SDK generation | 0; generated files, no hand editing | Generated diff in product commit |
| `git diff --check` | 0 | No whitespace errors |
| official `bf0318c5` vs product `internal/agent/runtime`, `internal/agent/contextview`, `packages/runtime` | zero diff | No Agent core change |

The new PG test uses an explicit test DSN, requires an already migrated schema
173+, and rolls back its entire synthetic fixture transaction. It tests own-run
state/message, next-run isolation, reader recreation, team/Bot mismatch, hidden
messages and history reset. The existing CI scoped-delegation PG gate now also
requires this test's actual PASS; all previous gates remain. The test does not
replace the older fresh-database migration/owned-tree tests.

## Failures and fixture limits retained

- The initial component run failed 22 tests because Node 25's experimental
  webstorage replaced jsdom's localStorage. Running the same tests with
  `NODE_OPTIONS=--no-experimental-webstorage` resolved it; the final 81 passed.
  [Initial output](web-task-results-ui-first.log).
- Both attempted fresh-database PG tests failed ENOSPC before assertions.
  The new read test was moved to a rollback-only fixture on the existing migrated
  test database. Its first SQL fixture then failed parameter type inference;
  explicit UUID/bigint types fixed it. The older owned-tree test was **not**
  successfully rerun this turn. [Disk failure](web-task-results-pg.log),
  [SQL fixture failure](web-task-results-pg-transaction.log).
- Initial Go lint found five genuine errors/style issues; these were fixed.
  Its second run failed compiling the handlers test package due to ENOSPC.
  The final scoped lint completed with exit 0.
  [First lint](web-task-results-go-lint.log),
  [resource failure](web-task-results-go-lint-final.log).
- Rebuilding sqlc downloaded/expanded an extra Go toolchain and exhausted scratch
  space. The already cached executable was verified as the required sqlc v1.31.1
  and generated successfully. SDK generation also initially failed creating its
  output directory. Four empty inactive cache directories were renamed to
  recover the output directories; generation with cleaning temporarily disabled
  succeeded, and the generation config was restored byte-for-byte. Only failed
  compiler cache entries and the unneeded expanded toolchain were removed; its
  downloaded archive was retained. An old generated server binary was compressed
  and its restored hash verified before removing that uncompressed copy. No
  database, original, volume, source or global cache was pruned.
  [sqlc failure](web-task-results-sqlc.log), [SDK failure](web-task-results-sdk.log).
- File-route script failures were preserved: header key case, missing test-user
  tenant setup, then an invalid assumption that changing a target owner revokes
  administrator access. That owner change did not revoke admin access and was
  restored; temporary test users were removed. The final test instead withdrew
  the actual source-session ownership and restored it in `finally`.
  [Header error](web-task-results-file-route.log),
  [fixture error](web-task-results-file-route-final.log),
  [incorrect revocation assertion](web-task-results-file-route-passed.log),
  [corrected actual check](web-task-results-file-route-verified.log).
- Reading an old process's `/proc/.../environ` was denied by the OS. It was not
  retried or read through another route. The server was started using the
  already documented environment and saved local test configuration. An earlier
  GET health check used the wrong method; the final restart uses HEAD correctly.
- Bot creation's full-overlay mkdir failure, Alpine/glibc bridge mismatch and
  private UDS fixture accommodation remain documented in
  [PR #70 evidence](web-management-task-drilldown.md). Code inspection confirms
  Docker normally supplies a TCP BridgeTarget, while the observed failing
  fixture fell back to UDS; the reason for that fallback is not yet proven.
  No default Docker bootstrap fix or default-deployment readiness is claimed.

The [manifest](web-task-results-manifest.json) lists exact selected copies and
hashes. These are raw local fixture outputs, including failures; no path/line
transformations were applied. Synthetic UUIDs and local scratch paths remain.
Private server logs, configuration and credentials are excluded. Known-secret
and credential-shaped scans found no matches; screenshots were visually checked.
This is not a claim of a universal secret detector.

## Remaining gates

No real provider, Windows/Pi execution, Valkey restart delivery, arbitrary child
attachment publishing, full diagnostic log view or full Web suite was tested
here. Existing OAuth and legacy media P1 holds are unchanged. No production
operation, deployment, promotion, Ready or main merge was performed. Remote
checks must be reported from GitHub separately; no disabled Actions setting or
permission was changed and no workflow was manually dispatched.
