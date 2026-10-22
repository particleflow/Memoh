# Native workspace observation validation

All data and screenshots here are synthetic. No model request, real Pi task,
production PC connection, pane creation, credential change or deployment was
performed. This is agent verification, not human QA.

## Environment and interactions

- Local Go server at `http://127.0.0.1:18080`, Vite at
  `http://127.0.0.1:18082`, PostgreSQL schema 170. The development server was
  rebuilt from this change; server/Web health and real login were checked.
- Browser login, `/settings/runtimes`, and
  `/settings/bots/session-tasks-synthetic?tab=remote-runtime` used the actual
  application and actual HTTP responses. A temporary loopback MCP fixture
  represented the native upstream, not a real Windows/Agenvo deployment.
  The browser did not replace or mock Memoh API responses.
- The fixture returned HTTP 401, explicit `device_offline`, or
  `runtime_unavailable`. Six actual runtime/target HTTP checks and three
  browser flows distinguished these states. Both runtime `online` and target
  `online` stayed false. Only `workspace.list` reached the native fixture;
  protocol discovery/initialization did not execute business work.
- Account and Bot views showed authentication unavailable. Configured Pi
  task capability was visible on PC A; PC B retained its command capability.
  Chinese dark mode at 480 px had no horizontal overflow. No browser page
  errors were observed. Captured screenshots were visually inspected.
- Operation records were identical before/after these reads. Temporary
  synthetic MCP/profile fixture changes were restored. The optional local
  native Server Workspace remains unavailable, which is honestly shown as
  offline in the Bot screenshot; no workspace execution was attempted.

## Automated and HTTP checks

- Go race tests passed for `internal/agenvo`, `internal/computertask`,
  `internal/userruntime`, `internal/workspace`, `internal/handlers`,
  `internal/sessiontask`, `internal/server`, and `cmd/internal/core`.
  The final identity-projection change also passed focused race tests.
- Standard MCP SDK over HTTP verified read-only discovery/recovery, PC scope,
  wrong user/Bot/session denial, revoked/mismatched registration denial,
  revocation during a native read, and immutable operation records.
  Native method assertions forbid execution, terminal input and pane creation.
- Missing pane/control pane/workspace, transport/auth failure, reference
  mismatch, malformed identity, legacy protocol, missing receipt and cancel
  intent cases preserve honest unknown states. Persisted verified results
  survive loss of native metadata. Unselected native fields and causes are
  absent from the response.
- Real MCP HTTP tests distinguish observed 401 during `tools/call` after a
  successful initialization from 502/error text mentioning 401.
- Workspace projection caps at 100, flags truncation, and rejects malformed,
  duplicate or cross-native-session responses.
- Sixteen existing parent-task HTTP GET checks across eight persisted Pi
  outcomes passed: `no-store`, field projection and unchanged operation ledger.
  The PostgreSQL owned-ancestor/stable-cursor integration test passed.
- Scoped golangci-lint and ESLint passed. Swagger/SDK generation, SDK typecheck,
  18 relevant Vitest tests and the Web production build passed. The UI contract
  guard passed with its three existing warnings; the build retains its existing
  chunk-size warning.
- Full Web typecheck still reports **442 existing errors**, with no errors in
  this change's Web files. It is not an all-green check.
- HTTP paths and all parent-task schema definitions equal the `4521d323`
  baseline. The only REST schema addition is runtime `availability`.
  Official Agent runtime/contextview remain identical to
  `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046`.

## Screenshots

- [Authentication unavailable](workspace-auth.png): account view, with separate
  Pi-task and command capability labels.
- [Bot authentication unavailable](workspace-bot-auth.png): selected Bot's
  existing computer settings surface, with native workspace status preserved.
- [Chinese dark narrow view](workspace-auth-zh-narrow.png): 480 px layout.
- [Confirmed device offline](workspace-offline.png): explicit relay evidence.
- [Native service unavailable](workspace-runtime.png): not device offline.

This verifies the Memoh adapter and UI against fixtures. Real PC reconnect,
real native workspace discovery and real Pi receipt recovery still require
separate acceptance. Explicit workspace reuse, control-pane restoration,
attachments, cancel/retry controls and mobile integration are not implemented
by this increment.
