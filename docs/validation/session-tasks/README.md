# Read-only conversation task validation

Revalidated on 2026-10-22 after integrating actual main
`eeb1fdabf50ec94d48f08302c9da3f5b3811293e` (merged PR #7 and PR #8). The original
implementation checkpoint is `ea424d52680e9707fb6dfe3a1534db62ff740145`.
Integration adds a Pi batch projection regression and refreshes these records;
the task service and frozen mobile API behavior are unchanged.
The only merge conflicts with PR #8 were generated SDK files, resolved by
regenerating Swagger and SDK with the merged generator configuration.
API contract: [session-tasks-api.md](../../session-tasks-api.md).

## Cache policy correction (2026-10-22)

The previous implementation set `Cache-Control: no-store` inside the task
handler's authorization helper. A request rejected earlier by global JWT
middleware therefore returned 401 without that header. The existing full
Echo/JWT/handler test reproduced the failure once its header assertion was
extended from successful responses to every response.

The task handler now registers a narrowly scoped Echo pre-routing middleware.
It applies the header before global authentication and route dispatch for the
`/bots/:bot_id/sessions/:session_id/tasks` namespace, including unsupported
descendant paths and methods. It uses Echo's own raw-path selection. Adjacent
message/session paths and similarly named `tasks-extra` paths are unaffected.
No auth rules, payload fields, SDK definitions or Agent code changed.

Regression coverage exercises the actual JWT middleware and Problem renderer
through `Echo.ServeHTTP`, not direct handler calls: missing/malformed tokens,
wrong signatures, expired tokens, revoked accounts, denied Bot grants, hidden
tasks, invalid parameters and unsupported methods. All task responses carry
`no-store`; adjacent route policies are preserved. Target resolution and
receipt mutation assertions still require zero dispatch and unchanged state.

## Environment and boundaries

- Isolated Linux cloud checkout, native current-source Go Server at
  `http://127.0.0.1:18080`, Vite Web at `http://127.0.0.1:18082`, local
  PostgreSQL 18 and pgvector development containers. Schema `170`, clean.
- Used the prepared `/workspace/.memoh-cloud/start.sh` workflow; rebuilt the
  server and restarted its owned processes after implementation changes.
  HEAD `/health`, real login and authenticated database queries passed.
- Go 1.25.7, Node 25.9.0, pnpm 10.27.0, golangci-lint 2.10.1. sqlc was generated
  with the exact repository command `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate`.
  `mise run sqlc-generate` initially attempted an unrelated Rust install into
  read-only `~/.rustup`; direct execution avoided that environment change.
  The generator automatically selected Go 1.26.10 for sqlc's tool requirement.
- Only synthetic Bot/session/operation/MCP registration fixtures were used.
  The synthetic endpoint is unused. No actual device, model, submission,
  cancellation, media ingestion or task execution was requested.
- No NAS/Windows production access, new production credentials, server ports,
  migrations, control plane or Agent changes. PR #7 is now part of the main
  baseline together with PR #8. Neither PR was merged or published by this task.

## Automated checks

Passed again on the integrated source (not reused from the old checkpoint):

```text
go test -race -count=1 -p 3 ./internal/sessiontask ./internal/handlers ./internal/auth ./internal/server ./internal/apperror
go test -tags=integration -p 2 ./internal/db -run TestSessionTasksPostgres -count=1 -v
go build -p 3 -o <cloud-server> ./cmd/agent
golangci-lint run ./internal/handlers/...
pnpm --filter @memohai/sdk typecheck
NODE_OPTIONS=--no-experimental-webstorage pnpm exec vitest run apps/web/src/utils/api-error.test.ts apps/web/src/composables/api/sse-error.test.ts --maxWorkers=3
bash scripts/check-agent-core-boundary.sh
git diff --check
```

- Frontend error tests: 2 files, 123 tests passed. The prior `1efb4a0b`
  integration also passed the Agenvo/computer/media/localfs race suites and
  broader Go lint, plus repository ESLint (0 errors, 18 existing warnings).
  This header-only correction changes no frontend, generated files or DB code;
  its rerun commands above target the HTTP/auth boundary and task regressions.
- Authorization tests cover user/Bot/session/PC mismatch, intermediate
  ownership/deletion/cycles, inherited PC scope, target grant/source permission
  revocation, changed registration, malformed selection metadata, primary
  fallback and workdir priority. JWT HTTP tests use the actual middleware,
  Problem renderer and Agenvo grant resolver, asserting **zero relay calls**.
- Cursor tests cover equal creation times, late state updates, later inserts,
  mismatched/tampered cursors, revocation during pagination and empty filtered
  continuation pages. Revision is tested beyond JavaScript's safe integer range.
- Projection tests inject synthetic secrets/reasoning/output/artifact hashes
  into persisted data and assert none are returned. Pi result remains unknown
  even if an arbitrary extra JSON property claims success. Cancellation does
  not claim process termination; foreground exit does not claim business success.
  The added integration regression uses PR #7's nested batch receipt, files,
  chunks and parsed result shape: these stay outside the summary response,
  and list/detail leave the stored operation unchanged.
- Regenerated Swagger and SDK agree with the automatic merge. Both task path
  definitions and the `sessiontask.Task` / `sessiontask.Page` definitions are
  identical to the frozen `276c0b54` contract.
- PostgreSQL integration creates and cleans its own fresh database. It tests
  owned recursive aggregation, sibling/foreign-user/foreign-Bot/team isolation,
  deleted-ancestor filtering, type discrimination and creation-key pagination.
  Schema stays 170; no migration is added or modified.

## Real HTTP and browser checks

The rebuilt real server passed 17 HTTP checks: unauthenticated list/detail rejection,
multi-page traversal, grandchild-owned operation detail under a parent,
cross-root refusal, PC-A-only aggregation, foreign-PC refusal through a child
root, unavailable attachments, runtime revocation and restoration, persisted
unbound selection, primary-PC fallback and explicit NAS selection. Workdir
priority also remains covered by the service tests. The operation IDs, revisions, states, cancel flags and
details digests were identical before and after all GET requests. Every checked
response, including 401/404, carried `Cache-Control: no-store`.

In Chromium, authenticated through the normal login UI using the existing
local development account. Opened the
synthetic Bot and parent conversation, expanded its persisted `spawn_agent`
entry, then clicked `synthetic-agent` to open the actual child conversation in
the existing right-hand pane. Reused only the previous synthetic history and
fixtures; no conversation was executed. The first browser assertion timed out
because it matched an abbreviated sentence exactly. The actual longer child
message was already visible; correcting the test selector passed without
application changes.

In the same authenticated running page, imported the generated SDK and called
both new endpoints. List returned 200, one item and a continuation; detail
returned 200 with the actual grandchild session ID, `unknown`, cancellation
intent and both content/attachment availability flags false. No browser page
errors occurred in the final flow. Both screenshots below were recaptured and
inspected on the integrated source.
The same page also fetched the task list without authorization and task detail
with an invalid JWT through the real Vite proxy: both returned 401 with
`Cache-Control: no-store`.

![Actual parent conversation in the running Web app](parent.png)

![Clicking the synthetic spawn entry opens its child in the existing pane](child.png)

These are screenshots of the running application, not a new mobile task UI.
The child screenshot retains the real optional-workspace-unreachable toast;
persisted conversation reads and the task endpoints still completed. This
environment does not establish workspace or live execution readiness.
Live model execution, actual computer work and HarmonyOS UI integration were
**not** verified. The mobile client is separate.

## Remaining product limits

V1 provides safe summaries only, with no raw execution content or attachments.
Native runtime liveness and Pi protocol success remain unknown. No scheduler
queue is invented. A phone-only target selection not yet persisted is outside
the server's view; the existing free picker can change, while workdir-bound
targets remain fixed. Existing broad media endpoints are unchanged, and no
task hash/URL is exposed through this API. Human QA remains unchecked.

Runtime/contextview match official `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046`
exactly; all `internal/agent` paths are unchanged from this PR's main base.
Remote CI is recorded separately in the PR and is not implied by these local
results. No merge, release, candidate publish or deployment was performed.
