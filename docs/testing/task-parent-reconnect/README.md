# Resume native task observation after parent revalidation

Product commit: `0d9ecd2db9acf06cf85833c9cd7f960a378ca668`.
Base: `5e55c45c85659f04b339b4df42aeff72966542a8`.
This is an increment on the existing integration branch / draft PR, not a
deployment or replacement of the production candidate.

## Confirmed gap and minimal change

`useNativeTask` already recovered a failed content read. However,
`useSessionTasks` passed parent-detail availability as visibility: a failed
parent GET, or temporary specialist-delegation revalidation, set `enabled=false`
and erased the user's opened-content choice. When the same parent recovered,
the task detail returned but terminal observation remained closed. The existing
content-only reconnect test did not exercise this composition.

Two new composition tests failed on the base (`red-configured.txt`). The two
production composables now distinguish a temporary parent pause from closing
the view. Pausing aborts requests and clears output. A successful parent read
must revalidate the same task before an already-open sample resumes. Identity,
selection and visibility changes still invalidate it completely. Unknown
follow-up UUIDs remain in the existing browser storage and are reconciled by
the existing server receipt; no new request ID or prompt is submitted.

No markup, API, provider, task ledger, permission, configuration schema,
Windows/Harmony source, migration, or Agent core changed.

## Actual checks

All commands ran in the existing cloud checkout with its installed Node/pnpm.
`NODE_OPTIONS=--no-experimental-webstorage` is the environment's existing test
setting. `evidence.json` records the exact commands, exits and byte-identical
published log hashes; logs retain historical failures.

| Check | Observed result |
| --- | --- |
| New composition regression before fix | 2 failed, specifically `opened=false` after parent recovery |
| Five affected task composable/component files | 81 tests passed, exit 0 |
| Final new regression file after adding required fixture summary | 10 tests passed, exit 0 |
| Complete Web typecheck | exit 0 |
| Changed TypeScript ESLint | exit 0 |
| Web build | exit 0; existing large-chunk warning retained |
| UI contract | exit 0; 3 existing warnings retained |
| Official runtime/contextview boundary | exit 0 against `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046` |
| Actual browser + existing Server + PG | exit 0; described below |

The ten new cases cover parent 503 recovery, specialist-parent pause, retained
unknown UUID, parent 401/403/404 stopping automatic reads until explicit
successful reload, close/identity/task changes, a late sample, and a late
follow-up response. Input acceptance remains distinct from task completion.

The first command accidentally used the environment's fallback pnpm, which
attempted a store initialization on the full root filesystem and failed before
tests (`startup-failure.txt`). Selecting the already-installed project toolchain
resolved this without installing packages or changing dependencies. Initial
typecheck found the new test fixture missing the required `summary`; that field
was added and final typecheck passed. Both failures are retained.

## Browser and persistence evidence

The test reused the existing loopback Web application, Server and isolated
`integration_web_types` PostgreSQL fixture. It did not connect to production.
The running Server is the prior native-content fixture build; the task HTTP,
native-content and authorization paths exercised here are unchanged by this
Web-only increment. This is not a fresh final-server-binary or new-deployment
bootstrap acceptance.

The existing synthetic child operation was re-exposed using its original
synthetic native profile and an uncredentialed loopback peer. The peer accepts
only `workspace.list`, `pane.get`, `agent.get`, and `agent.read`; no real model,
Windows, Herdr or Pi process was invoked. Its task payload remains `submitted`,
revision 6, while the native sample says `working` and
`terminal_snapshot_unverified`. This does not manufacture task success.

The actual browser loaded the parent conversation's task list, selected that
persisted child operation, and opened the native sample through the real
authenticated HTTP handlers. Only the parent detail response was fault-injected
as HTTP 503. Three failed detail reads cleared/hid child content and stopped
native content reads. A subsequent real HTTP 200 restored the same opened
sample without another click or business submission. The content endpoint
returned two HTTP 200s across this sequence; task POST count was zero.

- [Parent unavailable](parent-unavailable.png): stale child content removed.
- [Same task recovered](same-task-recovered.png): automatically restored bounded,
  escaped sample, without asserting business completion.
- [Narrow dark view](same-task-narrow-dark.png): 390 px, English, no document
  horizontal overflow. This run does not claim Chinese or a native phone binary.

Closing/reopening the dialog required explicit opening of the sample again.
No page JavaScript error occurred. `relationship-proof.json` verifies the
operation's persisted child session, original parent, Bot and native references.
`pg-proof.json` verifies the complete operation row was unchanged and both
temporary synthetic configuration values were restored. No credential was
created or modified. The executed browser and peer scripts are included for
review; their private fixture inputs are deliberately excluded and they must
only be used with the existing isolated synthetic setup.

The environment initially had 190 MiB free in `/tmp`. Exactly four verified,
task-owned, regenerable Go build-cache entries were removed (558,949,480 bytes).
No source, Git history, database, original file or evidence was removed. This
was not a global prune; the removal record is in `cache-cleanup.json`.

## Coverage map and remaining acceptance

| Requirement | Current implementation/evidence boundary |
| --- | --- |
| Supervisor → specialist → child task navigation | Existing delegation reader validates the returned specialist session and pauses descendants during relation revalidation; prior [business/UI evidence](../web-management-task-drilldown.md) and current 81-test selection are reused. This run does not dispatch another specialist job. |
| Child/PC task identity and state | Existing task API aggregates owned descendants. This run used a real persisted child operation and kept `submitted` separate from native `working`; [child-run projection evidence](../web-task-results-and-downloads.md) is not rerun as a new result. |
| Execution content | Interactive native Agents expose bounded terminal samples via `agent.get/read`. Full logs and per-instruction proof are not implied. Batch Pi is explicitly unsupported by this interactive content/follow-up endpoint; its persisted result/summary path is separate. |
| Workspace and device ownership | Existing observation checks recorded mapping/workspace/pane/terminal identity and grants. This run used the synthetic native peer; real PC ownership and authentication still require onsite acceptance. |
| Management actions | Existing native follow-up and delegation result-delivery retry remain their existing operations. No queue scheduler, kill, workspace deletion, business replay or new cancel action was added. Recorded task lists are not evidence of scheduler queue positions. |
| Reconnect | Parent failure → same-task content recovery is newly verified here; unknown input UUIDs cannot be resent. Closing or switching identity/task does not revive old output. |
| Herdr restart / batch collection | Existing `ec501` recovery-reader code and [its synthetic evidence](../batch-recovery-reader/README.md) are unchanged. Real Herdr restart, non-atomic generation checks and unknown resource ownership limitations remain. |
| Artifacts | Existing committed-reference rendering/download evidence is unchanged; no file transfer or artifact delivery was executed in this increment. |

No onsite browser, production configuration, image publication, NAS deployment,
real provider/PC task, or previously blocked legacy-media repair was touched.
No backend/PG suite, entire Web suite or old candidate build was rerun. Remote
exact-head CI is separate and is not inferred from local passes; the previously
failed `gh` API authentication was not retried. Human QA remains unconfirmed.
