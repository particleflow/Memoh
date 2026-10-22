# Device cache policy and Web state integration

This integration is based on frozen `813d50ff97f22f613e0967fac2528b5c2fb5400e`.
It combines #36 (`ba7da909e49454b85bd49021d81aa1ba5cf859fc`) with #35
(`44310848ffa3841c296ce121ce5337d9616a25aa`, including #33). Both source
increments were independently reviewed before integration.

## Scope and merge checks

- Private device responses are no-store before JWT and routing, including
  rejected requests within the two device namespaces. Adjacent routes retain
  their existing cache policy.
- The Bot list shows safe load errors and supports retry. Network status uses
  the generated DTO and actual query loading state, with no overlapping polling.
  Desktop settings, attachment gallery types and MCP path annotations include
  the previously reviewed type-contract corrections.
- The merge had no conflicts. The only overlapping files were generated SDK
  types and three Swagger files. Swagger leaf changes are disjoint and the
  merged document is their exact union. Regenerating Swagger and the SDK leaves
  no diff. All response definitions and frozen task API paths match 813d50ff.
- All prior 4521/813 integration ancestors remain. No migrations changed:
  local PostgreSQL remains clean at schema 170. The official Agent runtime and
  contextview remain byte-identical to
  `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046`.

One additional change is limited to the PostgreSQL image integration fixture.
The original test reused a globally unique Bot name and attempted cleanup with
`t.Context()`, which Go cancels before cleanup. Repeated runs therefore failed
with `idx_bots_name`; the same failure reproduced on frozen 813d50ff in the same
database. The fixture now uses its UUID in the name and a bounded independent
cleanup context, reporting cleanup errors. Two consecutive race-enabled runs
passed, with unchanged fixture row counts before/after and no removal of prior
rows. No application write path changed.

## Verification

The current merged Go application, Vite UI and local PostgreSQL were healthy.
Login and authenticated Bot/provider/template queries passed through both
`http://127.0.0.1:18080` and the UI proxy at `http://127.0.0.1:18082`.
Only local synthetic data and a loopback native-service fixture were used.

- Race suites passed for Agenvo, computer tasks, image jobs, media, background
  tasks, session tasks and server. Focused handler/tool race tests cover task
  permissions, workspace observation, image queues and private device/OAuth
  cache policy. Real production Server/JWT middleware tests cover anonymous,
  expired/revoked identity, cross-user and missing-resource responses.
- Real PostgreSQL ancestry/cursor integration passed. The image integration
  passed twice after the fixture correction: parent/child edit refs, queued
  Origin refs, separate root turns, replay/replacement, unknown-result reads
  without redispatch, stable image hashes and wrong-parent/revocation denial.
  The registered tools and queue are real; image provider and child completion
  are synthetic. This is not production model or full image-edit acceptance.
- Ten Web test files passed: 56 tests for Bot error/retry, network polling,
  API identity handling, MCP paths, media/gallery, task views, workspace targets,
  image refs and OAuth. SDK typecheck passed. Full Web typecheck still reports
  **145 errors** (exit 2), matching #35's known baseline; it did not pass.
- Scoped Go lint (including the integration-tag fixture), changed-source ESLint,
  UI contract guard, Go server build and Web build passed. Existing UI guard
  warnings and the Web bundle-size warning remain. Seven known external-network
  image download failures were not repeatedly rerun.
- Current application task HTTP regression passed 16 outcome/projection checks
  and 17 permission/pagination/revocation/PC-selection checks. GETs left operation
  ledgers and receipts unchanged; no internal fields leaked; responses were
  no-store. Device HTTP checks covered six status observations and ten routing/
  authentication boundaries; actual browser device responses were also checked.
  Authentication unavailable, explicit offline and runtime unavailable remained
  distinct. Native calls were limited to `workspace.list`; fixture configuration
  was restored and operation rows were unchanged.
- The existing full application's unknown account descendant can return 500 via
  `/users/:id`; the baseline comparison confirms this is unchanged, and the
  response now has no-store. This integration does not change fallback routing.

## Browser evidence

Fresh Chromium captures below were visually inspected against this combination.
The Bot list and network scenarios inject synthetic GET responses into the
running application; device status uses the running server and a loopback native
fixture. They do not establish real PC/network/provider availability.

- Bot 403 displays a generic error without raw diagnostic content. Retry is
  disabled while pending and restores the empty-list creation tile on success.
  A network failure stays readable at 480px in Chinese/dark mode. A synthetic
  401 passes through the real API client, clears credentials and redirects to
  login. The login screen initializes the cleared token to an empty string.
- A delayed second network request spans a polling interval without overlap.
  Native `ready` renders **Connected**, then polling stops; unmount also stops
  polling. No browser errors or business-write requests occurred.
- The device account view displays **Authentication unavailable**, distinct from
  a physical offline diagnosis. Cache and authorization claims use HTTP
  assertions rather than screenshots.

[Bot load error](bots-error.png) ·
[Chinese narrow network error](bots-network-narrow.png) ·
[Network ready](network-ready.png) ·
[Device authentication unavailable](device-auth.png)

This is agent verification, not human QA. No production data, private address,
credential, configuration or log was uploaded. Live PC reconnect/recovery,
production image provider and mobile acceptance remain outside these checks.
No candidate dispatch, deployment, main merge or promotion is claimed here.
