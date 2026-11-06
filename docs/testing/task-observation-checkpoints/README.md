# Shared computer-task checkpoints

Base: `24f65a6cff9cb12b901be0a14beb06b258aa8cd0`.

The task observation already exposes `reconciliation` and `continuity`.
The shared task detail previously displayed only current native state and four
identity references. `computer-task-checkpoints.vue` now projects the existing
response in four distinct groups:

- Current connection and original target/workspace/pane/Pi-session references.
- Persisted receipt validation, process/Pi result, exact string revision and
  original attempt/generation. Pi completion does not confirm business success.
- Historical continuity status/reason and the last invocation's before/after
  observations. Historical verification does not claim present restoration.
- File collection state, confirmed byte offset/reported size, current
  availability and recorded hash. The wire field is `files[].state`.

Missing or unsupported values remain unknown. Zero bytes are retained;
unsafe numeric byte values are unknown. `recorded` and `not_rechecked` never
create download links or imply current file availability. Existing file delivery
and attachment rendering are unchanged.

The child component has no API/client/store, request, timer, listener or action.
The existing parent still owns loading/errors/refresh and hides the child on
access loss. Existing observation scope cancellation and GET-only recovery are
unchanged. No backend, provider, OAuth, SDK, Agent core or task-tree changes.

## Validation

All commands below exited 0 on the final product files:

```sh
NODE_OPTIONS=--no-experimental-webstorage pnpm exec vitest run \
  apps/web/src/pages/home/components/computer-task-checkpoints.test.ts \
  apps/web/src/pages/home/components/session-task-results.test.ts \
  apps/web/src/pages/home/components/session-tasks.test.ts \
  apps/web/src/pages/home/composables/useComputerTaskObservation.test.ts \
  apps/web/src/pages/home/composables/useSessionTasks.test.ts
pnpm --filter @memohai/web typecheck
pnpm --filter @memohai/web build
node scripts/check-ui-contract.mjs
node .husky/checks.mjs
```

The five test files passed 89 tests, including both Web/native shared hosts,
English/Chinese/Japanese labels, exact revision beyond JavaScript's safe integer
range, zero/partial bytes, unknown fields, escaped text, no checkpoint actions,
stale data hidden on 403, scope retirement and existing reconnect behavior.
ESLint had 0 errors and 4 existing multi-component-test warnings. The UI guard
passed with 3 pre-existing warnings; the build retained its large-chunk warning.
The aggregate selects staged frontend lint and no Go packages for this UI-only
change. It is not a full-repository test result or remote CI result.

## Synthetic runtime boundary

A fresh single-tab browser exercised the actual development Web application
with an existing isolated synthetic Server/PG login/session. Every task HTTP
request was intercepted with controlled responses; no task observation reached
a device or OAuth refresh path. The run recorded 24 task GETs and zero task
mutations, model calls or device/OAuth requests.

Verified: four groups; offline connection distinct from persisted Pi success;
disconnect clears checkpoints; reconnect reads the same identity; 403 clears
stale data and stops automatic polling; explicit refresh recovers; missing
fields remain unknown; reload retains identity; Chinese dark mode at 390px
width wraps long IDs/hashes without horizontal overflow. Current screenshots
were captured and inspected.

The first browser attempt failed before login reached Server because the test
launcher used the wrong proxy environment variable. The existing worktree
launcher corrected the proxy and shared dependency asset allowlist; no product
configuration was changed. That failure log is retained separately.

This does not verify live PC restart/history restoration, real device output,
production deployment, or human QA. Integrate independently after the provider
security release checkpoint; do not replace that release branch.
