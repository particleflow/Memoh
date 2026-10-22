# Shared Web delegation result delivery retry

The shared observer accepts an optional delivery action. The Web host uses the existing authenticated SDK POST for the selected delegation. Hosts without this capability remain read-only. The action is named **Retry result delivery** (Chinese: **重试结果投递**); it never submits a specialist task. Server authorization and execution state remain authoritative. An accepted delivery acknowledgement does not confirm that the supervisor processed the result.

## Fixed inputs and source boundary

- Frozen backend product: `9bd82f57d66846a6d6c36c3515f0344acf16a9f7`.
- Frozen observer: `8022eca5dd00df20f407451a935269f002256027`.
- Composition baseline: `42b25dabcd06c83288731bcadfca58821602c0bc`, with those two direct parents. Regenerating the combined API contract produced the same specification and SDK bytes as the frozen backend.
- Feature changes relative to that baseline: ten Web source/test/locale files, listed with exact SHA-256 values in [source manifest](source-manifest.json). Documentation and the selected evidence below are additional files.
- No backend, API contract, generated SDK, official Agent, authorization policy, task execution, attachment serving, or private host implementation changes.

The host adapter locks repeated clicks synchronously, captures bot/session/delegation/login identity, aborts obsolete local requests, discards late responses, and retires pre-action GET requests before reading fresh server state. Aborting a browser request cannot undo server acceptance. Terminal `completed`/`failed` tasks are eligible; execution `accepted`/`running`/`unknown` and already accepted delivery have no retry action. A source-session mismatch or a permission denial blocks mutation. There is no automatic POST retry.

Existing dialog composition remains: task identity, execution state, delivery state, optional action and request response, final visible text, attachment availability. This adds no cancellation, execution recovery, attachment fallback, or new service. The action uses the existing Button and CalloutBanner owners and localized English, Chinese, and Japanese text.

## Executed checks

| Check | Actual result | Evidence |
| --- | --- | --- |
| Unmodified observer tests on composition baseline | 54 passed, 3 files, zero skips | [baseline output](baseline-observer-tests-node25.txt) |
| Final related frontend suites | 244 passed, 8 files, zero skips | [test output](related-tests-final.txt) |
| ESLint on seven changed TypeScript/Vue files | Exit 0, no diagnostics | [empty raw output](eslint-final.txt) |
| UI contract guard | Passed, 3 existing warnings | [guard output](ui-contract-final.txt) |
| Web production build | Passed, existing large-chunk warning | [build output](web-build.txt) |
| Full Web typecheck | **Failed on both baseline and feature: 148 diagnostic headers, identical, zero added/removed** | [comparison](typecheck-comparison.json), [baseline raw output](typecheck-baseline.txt), [feature raw output](typecheck-final.txt) |
| Real Web POST to fixed backend, existing completed task | One POST for two clicks, same login, empty body; accepted acknowledgement shown accurately | [browser output](ui-final.txt), [database and request evidence](ui/results.json) |
| Real permission revocation before forwarding POST | Backend 404 `session.not_found`; old detail cleared and action hidden; explicit fixture grant restored | [browser output](ui-denial.txt), [assertions](ui/real-revocation.json) |
| Shared component hosted without action capability | No retry button and zero API requests | [browser output](readonly-ui.txt) |

The related suites include existing reader/host/SDK authentication/error handling assertions, plus delivery action cases: duplicate clicks, bot/session/selection/login/task changes, dismissal/disposal, late success and denial, pre-action GET races, 401/403/404, execution unknown, failed execution, unavailable/pending/unknown delivery responses, and accepted acknowledgement versus supervisor processing. HTTP 401/403 are covered by frontend fixtures and existing SDK tests; the actual scoped backend denial observed here was 404.

### Commands

From the repository root, using the saved environment's Go 1.25.7, Node 25.9.0, pnpm, and cached dependencies:

```sh
NODE_OPTIONS=--no-experimental-webstorage pnpm exec vitest run \
  apps/web/src/pages/home/composables/useBotDelegationDelivery.test.ts \
  apps/web/src/pages/home/composables/useBotDelegations.test.ts \
  apps/web/src/pages/home/composables/useSessionTasks.test.ts \
  apps/web/src/pages/home/components/bot-delegation-tasks.test.ts \
  apps/web/src/pages/home/components/session-tasks.test.ts \
  apps/web/src/lib/api-client.test.ts \
  apps/web/src/utils/api-error.test.ts \
  apps/web/src/composables/api/sse-error.test.ts
pnpm exec eslint \
  apps/web/src/pages/home/composables/useBotDelegationDelivery.ts \
  apps/web/src/pages/home/composables/useBotDelegationDelivery.test.ts \
  apps/web/src/pages/home/composables/useBotDelegations.ts \
  apps/web/src/pages/home/components/bot-delegation-tasks.vue \
  apps/web/src/pages/home/components/bot-delegation-tasks.test.ts \
  apps/web/src/pages/home/components/session-tasks.vue \
  apps/web/src/pages/home/components/session-tasks.test.ts
node scripts/check-ui-contract.mjs
pnpm --filter @memohai/web build
pnpm --filter @memohai/web typecheck
```

The initial test invocation without the Node flag failed because Node 25's experimental global `localStorage` lacked `clear`; the unchanged observer baseline reproduced that environment issue. The final invocation disables that Node global, using the test environment's storage implementation. No product workaround was added. The full typecheck remains failing; this is not an all-green validation claim.

## Real browser and backend boundary

The current Web UI ran on loopback port 18286 in the saved isolated cloud fixture, proxying the fixed backend above, with real PostgreSQL 18 and Valkey 8 persistence. Chromium/Playwright used the existing synthetic member login. It opened the existing supervisor conversation, Tasks, Specialist Bot handoffs, and the existing completed delegation. No new business task was created.

The fixture deliberately changed only the completed delegation's delivery acknowledgement from `accepted` to `unknown` (plus its revision), retaining all task/request/run/result identities. This simulates a lost acknowledgement; it is **not** evidence of a naturally occurring queue failure. The browser forwarded the actual POST to the actual backend and held its real response long enough to inspect the disabled pending button. Two immediate clicks produced exactly one POST, with the original SDK login and no request body. After its real accepted response and a six-second observation window, the three run records and their fencing tokens, delegation/request/run identities, and eight history asset links were unchanged. A local fixture model HTTP guard observed **zero requests**. No paid model was called.

The separate denial case revoked the synthetic member's existing specialist grant immediately before forwarding the POST. The real backend returned 404. The subsequent read cleared stale detail. The script restored the same explicit permissions and the original accepted acknowledgement in `finally`. No global ACL was widened.

Browser scripts and all original diagnostics remain in the saved environment at `/workspace/delegation-delivery-retry/`: `verify-ui.py`, `verify-ui-denial.py`, `readonly-host-fixture.html`, and the first-failure outputs. They read the existing private fixture state without printing credentials. They are environment-bound acceptance drivers, not standalone deployment or fresh-business harnesses. Published files are an explicit reviewed selection, not an upload of that private directory.

### Screenshots inspected

- [Retry available](ui/retry-available.png): completed execution, unknown delivery, correctly named action.
- [Pending](ui/retry-pending.png): disabled action while the real POST response is held.
- [Accepted](ui/retry-accepted.png): actual response and explicit warning that supervisor processing is unconfirmed.
- [Dark, narrow Chinese view](ui/dark-narrow-zh.png): 390-pixel viewport, localized states and acknowledgement warning.
- [Read-only host](ui/readonly-host.png): the same shared public component without action capability, terminal/unknown state, no action.
- [Real revoked permission](ui/real-revocation-denied.png): inaccessible detail replaces the old content.

The first browser selector failed before any POST: the shared Button's leading spinner contributed `Loading` to its accessible name even while idle. [Original accessibility probe](probe-accessibility.txt) records `Loading Retry result delivery`. The action now supplies an explicit localized `aria-label`; final tests and all successful screenshots above include that correction. The underlying UI library was not changed.

## Limits and provenance

This increment does not rerun the specialist business or the earlier SIGTERM/SIGKILL recovery matrix, verify a new busy-queue recovery, or claim all module CI is green. It does not exercise physical PCs, production NAS, Windows, or private Harmony host code. The read-only host check is a synthetic host of the public shared component. Human QA has not been performed. No PR creation, PR metadata update, merge, or deployment is part of this increment.

Text outputs, JSON assertions, and screenshots linked here contain synthetic data and public code paths. They are unchanged copies of the selected local files, not redacted reconstructions; [evidence hashes](evidence-sha256.json) identify their bytes. Empty ESLint output is intentional. The source manifest identifies the exact tested Web files; the documentation commit itself is not a claim that unrelated tests were rerun.
