# Persisted task summaries and explicit native observation

Base: `b932464363c97ba56331b53f968b14eec202768a`. This change uses the existing task detail, observation/content readers and MCP OAuth service. No migration, SDK, provider configuration, Agent core, native endpoint, or task identity contract changes.

## Reproduced failures

- `red.log`: opening the shared persisted task detail invoked native observation four times in fifteen seconds. The targeted red test failed; the six unselected cases were skipped intentionally at this red-test stage.
- `gateway-red.log`: an OAuth `invalid_grant` response did not stop MCP initialization; two MCP HTTP requests reached the synthetic remote. This is evidence of connection attempts, not completed business execution.

## Behavior

The existing Refresh computer action starts observation for the selected visible task. Merely opening persisted summaries, relationships or file references does not start native observation or terminal-content reads. The existing terminal-sample action remains separate. Native reads still poll after explicit activation and recover from transient network failures using GET for the same operation.

HTTP 200 with `authentication_unavailable` is unavailable authentication, not device success. This response, or HTTP 401/403/404, pauses automatic observation/content reads, including across parent-summary revalidation. The existing Refresh action retries after access/server support has been restored. Changing identity/task or closing the view retires reads and resets activation. No authorization dialog, new approval, business submission or replay was added.

OAuth preparation now returns before HTTP/SSE MCP initialization on any token acquisition failure, missing OAuth service/token, or refreshed-token persistence failure. Static or differently cased Authorization headers cannot act as fallback. Typed missing/rejected credentials map to the existing authentication-unavailable marker. DNS, TLS, timeout, malformed responses, server failures and persistence failures do not prove revoked authorization and do not receive that marker. Logs retain only existing safe categories/status/code, never raw errors, token bodies or endpoints.

## Checks and actual runtime evidence

- Shared UI regression: 10 files, 141 tests passed (`ui.log`), including the seven shared-body activation tests (`entry.log`). This is a targeted set, not all Web tests.
- `go test -race -count=1 -run '^TestFederation' ./internal/handlers`: passed (`gateway.log`). HTTP and SSE rejected/missing/malformed/upstream/persistence cases, zero MCP requests; rejected tokens have zero writes. Persistence-failure case attempts one failed write and preserves the stored row. Explicit recovery lists tools with fresh auth and no `tools/call` replay.
- `go test -race -count=1 ./internal/mcp`: passed (`oauth.log`).
- `go test -race -p=1 -count=1 -run '(SessionTaskObservation|ReadOnlyRecovery|NativeContent|Observation|Availability)' ./internal/handlers ./internal/agenvo ./internal/computertask`: all three packages passed (`observation-backend.log`). Auth-unavailable observation remains business-unknown, and the persisted receipt is unchanged.
- `pnpm --filter @memohai/web typecheck`: exit 0 (`web-types.log`). Web build exit 0 (`web-build.log`), retaining the existing chunk-size warning. UI contract passed with three existing warnings (`ui-contract.log`).
- Existing isolated current-source Server/PG/Web stack was reused: Web `http://127.0.0.1:18402`, Server loopback `:18400`, synthetic database schema 175. Server was rebuilt with this gateway fix and restarted only in that owned cloud stack. No data or database reset.
- Actual browser (`browser.log`, `browser-proof.json`): login to the isolated existing synthetic account, open the synthetic conversation → Tasks → original operation. Real persisted task detail/delivery requests return 200; fifteen seconds produce zero observation/content calls. Existing Refresh starts live observation; auth-unavailable stops further polls for two intervals. Explicit recovery and a transient disconnect return to GET of the same task. Closing/reopening requires a fresh activation. Zero mutation, content or device/OAuth/MCP requests; no page errors.

The browser uses real Server/PG reads for persisted tasks and delivery lists. Its live observation responses are intercepted synthetic transport; workspace-target discovery is also stubbed to prevent external activity. Gateway rejection/recovery is separately verified by actual loopback OAuth/MCP servers in Go tests. These checks are not real Herdr/Windows, production OAuth recovery, phone acceptance, human QA or full business completion.

Screenshots were captured from and visually inspected on the current application:

- [Persisted summary](summary.png): existing summary/relationships/file list without contacting the computer.
- [Authentication polling paused](auth-paused.png): visible pause notice and existing Refresh action.
- [Same-task reconnection](reconnected.png): current observation returns; business completion remains unknown.

The portable browser runner requires `EVIDENCE_DIR`, `WEB_URL`, `FIXTURE_PATH` (synthetic JSON with `bot`, `session`, `operation`), `TOKEN_PATH` (existing isolated test account only), and optionally `CHROMIUM_PATH`. It expects an existing synthetic conversation named `Task file observation synthetic`; it never creates data or credentials. The local executed runner used equivalent fixed task paths; the published copy parameterizes those paths and uses the standard Playwright import. Do not run it against production.

## Preserved failures and limits

Initial aggregate and backend-race linking failed from lack of disk space (`aggregate-initial-space-failure.log`, `observation-backend-initial-space-failure.log`). Ten task-owned, regenerable old Go cache archives totaling 1,780,953,464 bytes were removed across two bounded cleanups (`cache-recovery.json`); source, databases, binaries and original logs were preserved. Sequential server build and the affected backend regression then passed. The initial server-build attempt also failed due to space; its redirected file was empty, so there is no claimed readable compiler log for it.

The first aggregate lint reported unchecked return/formatting/HTTP literal issues in tests (`aggregate-lint-failure.log`); these were fixed without rule changes. The first browser script waited for a fallback error sentence while the shared resolver showed the normal network-error wording (`browser-initial-label-timeout.log`); it was corrected to inspect connection-state clearing and same-task recovery, then passed. These failed attempts are not reported as successful runs.

`manifest.json` records original/published hashes and exact text normalization. Original local logs remain available; published normalized logs are not described as byte-for-byte originals. The corrected aggregate Go lint passed (130.66s), but its package tests hit a second disk-full event: MCP compilation failed and two large-upload fixtures returned 500 (`aggregate-test-space-failure.log`). After the second bounded cleanup, `node .husky/checks.mjs go-test` passed both complete packages (handlers 4.324s, MCP 0.335s; `aggregate-test.log`). No test or rule was removed. These package runs may retain opt-in environment skips; they are not new real-PG acceptance. The error-handling boundary checks also passed: `go test -p=1 -count=1 ./internal/apperror/... ./internal/server/...` (`error-boundary.log`) and the existing `api-error`/`sse-error` Vitest files (123 tests; `error-ui.log`). Remote CI, deployment, real-device checks and OAuth recovery are not established by these local results.
