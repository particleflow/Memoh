# Durable owner admission pause

Base: `acdb3c7aba0931929243921271abbb63afc54024`. This extends the existing owner runtime service and operation store. It does not modify Agent runtime/contextview, Herdr, Pi, credentials, production configuration or Windows files. The previous [read-only reconciliation](../computer-operation-reconciliation/README.md) remains unchanged.

## Implemented contract

The existing authenticated account route now provides:

- `GET /users/me/runtimes/{runtime_id}/admission`: no native call, no mutation.
- `PUT /users/me/runtimes/{runtime_id}/admission`: strict JSON `{request_id, expected_revision, paused}`. `request_id` is a canonical nonzero UUID saved before sending; `expected_revision` is an exact nonnegative decimal string; `paused` is an explicit boolean. No caller-supplied owner, device, scope, destination or credential is accepted.

The response contains `connection_id`, `device_id`, `instance_id`, `request_id` when present, decimal `revision`, `paused`, and `scope: owner_connection_instance`. An absent gate reads as open/revision `0`. A response is **not a host stop permit or proof of drained work**.

Mutations use the existing owner MCP authorization and `workspace.list` probe. The database rechecks the same registered configuration before committing the pause. No source URL or new credential is supplied. GET and all namespace failures retain `Cache-Control: no-store` before JWT middleware.

Pause with a fresh request UUID and the read revision. Resume with that pause's UUID and current revision. An identical acknowledged transition can be read again after a lost response. A stale/foreign resume, or reuse of a released pause UUID to close admission again, fails. An unknown transport/database response is not permission to create a new request ID: read/reconcile the existing gate. There is no expiry-based automatic resume.

Stable failure codes are `computer.admission_paused`, `computer.admission_conflict`, and `computer.admission_scope_unverified` (HTTP 409). Tool submit/follow-up gate rejection reports `business_dispatched: false`, `retry.automatic: false`, and respectively `operation_created: false` or `request_reserved: false`. These describe the rejected call, not the outcome of an older operation. No phantom follow-up is added to its receipt.

## Coverage and persistence

Migration **0175** adds one `computer_admission_gates` row per current team, owner, existing MCP connection, authoritative device ID and instance ID. It is separate from execution state; the existing operation remains the only task ledger. Session, working directory, model, display name, Bot and mount ID do not subdivide the gate. All matching registrations/mounts in that owner/connection/native identity are covered.

Different MCP connections are distinct trust domains. A live registration with equal device/instance through another connection makes mutation coverage unverifiable and is rejected; names, IPs, URLs and credentials are never used to infer equivalence. A new ambiguous alias with those IDs cannot bypass an existing closed gate. This does not identify every physical host reachable through arbitrary brokers, different native IDs or other owners. It must not be represented to Windows as machine-wide stop authority.

A short owner-scoped PostgreSQL transaction lock serializes gate changes with:

1. New operation INSERT/claim.
2. New follow-up request reservation on an existing operation.
3. Reconfiguration of an already paused native registration.

Triggers protect these boundaries for existing SQL writers as well as this service version. An input committed before pause remains admitted; it may still dispatch afterward and must be accounted for during handoff. Exact retry of an already claimed operation returns its receipt without a new dispatch. Existing reserved follow-up progress, result/log collection, delivery metadata and `recovery_reader` ambiguity remain writable and are not reset. Unknown or missing target mapping cannot establish exemption when its owner has a live pause.

The table uses forced team RLS. Changing services/processes does not reopen the gate. Migration down refuses a live pause, inspects across teams rather than silently relying on the migration caller's RLS view, and preserves all operation/receipt data. Resume/reconcile before rollback; do not remove the gate table or triggers as an operational workaround. Stop old writers before schema rollback. Old servers have no pause management endpoint and may report generic errors for trigger rejection; do not roll back while paused or assume an absent API means admission is safely open.

## Windows integration order and remaining boundary

Use the already established owner -> registered MCP -> approved Agenvo device/instance control direction. File-transfer credentials are not a control identity. No Windows -> NAS credential is introduced.

1. The existing Memoh owner saves a pause request UUID, reads the gate, then requests pause. Preserve the returned identity/request/revision.
2. Enumerate relevant owned registrations/mounts and original operations using the existing task/owner stores. Finish the existing bounded result/diagnostic/delivery collection required by the agreed preservation policy. `completed`, queue idle, process exit, a local receipt or a validated receipt alone is not sufficient. Unknown dispatch/follow-up/reader creation stays blocked, never replayed.
3. Reconcile each original operation's ID/revision/TargetFingerprint, attempt/generation/request hash/Pi session, original pane/terminal and receipt/history/file facts using the existing reconciliation. The gate does not freeze those revisions or replace their ownership checks.
4. Only an explicit, aligned existing host handoff can authorize stopping/switching components. This patch sends **no host command**, creates no control pane and issues no stop permit. Native `pane.run` acceptance is only input delivery; durable host acknowledgement and reconnect/readback remain distinct.
5. After a separately verified host transition and control recovery, resume the same gate using its UUID/revision. Unknown outcomes do not trigger repeat stop, task replay or a new pause intent.

The remaining cross-end contract must bind the endpoint program and hostRoot, host run/configuration/native ownership, the gate revision and reconciled operation revision set to the existing host journal. At this gate checkpoint no program/hostRoot locator was configured; the subsequent [owner location increment](endpoint-location.md) adds only independent registration configuration and does not authorize stopping. The workspace root must not be guessed as hostRoot. Existing trusted owner transport does not make a local JSON file an identity credential. No nonexistent `host.stop` or `plugin.action.invoke` method is assumed. The Windows author owns that endpoint interface; no provisional wire acknowledgement format is invented here.

## Verification record

The accompanying outputs record synthetic isolated tests; they are not actual Herdr/Windows stop/restart acceptance. Historical failed runs remain separate from successful runs. No production, user file, paid model or real PC task was used. Human QA remains unchecked.

Executed locally on the current changes:

| Check | Actual result |
| --- | --- |
| `go test ./internal/agenvo ./internal/computertask -run 'TestAdmission\|TestComputerAdmission' -count=1 -v` | Exit 0; [service/tool output](evidence/unit.log). No native dispatch on gate rejection. |
| Real global HTTP/JWT middleware tests | Owner-only success, unknown JSON field rejection, anonymous/invalid/revoked authentication and no-store passed. The initial combined command failed only the new catalog golden registration, which was then registered normally; [initial output](evidence/http-and-initial-catalog.log). |
| `go test ./internal/apperror -count=1` | Exit 0 after adding/sorting all three codes in both existing Web and IM locale catalogs; [output](evidence/catalog-complete.log). Earlier missing IM/ordering failures retained in [package output](evidence/packages-before-locale-fix.log). |
| `scripts/test-computer-operations-postgres.sh` with the existing isolated PG identity | Exit 0, six top-level tests / ten named PASS / zero SKIP, race enabled, 18.841 seconds; [raw mixed command/JSON output](evidence/postgres-race-gate.log). The existing CAS/ownership/RLS test is still required, alongside admission tests. No workflow permission or timeout was changed. |
| Affected Go lint (v2.10.1) | Exit 0, [empty successful stdout](evidence/lint-final.log). Initial ENOSPC and subsequent EOF/format findings are retained separately. No rule was disabled. |
| Staged `node .husky/checks.mjs` | Lint stages passed; Go tests failed only the existing >200 MiB upload test while `/tmp` had about 417 MiB free. [Full aggregate output](evidence/aggregate.log). After reclaiming only this task's old regenerable caches/stopped binaries, about 828 MiB was free and the unchanged failed test passed: [targeted output](evidence/large-upload-restored-space.log), exit 0. The aggregate was not rerun or relabeled green. |
| SDK and Web typechecks | Both exit 0: [SDK](evidence/sdk-types.log), [Web](evidence/web-types.log). |
| Desktop typechecks | Existing dependencies were missing in this checkout. Reused installed dependencies, with workspace links pointing at current source; no install or version change. Node check passed, then Web check passed with the existing `vue-tsc` tool on PATH: [node/initial Web-path result](evidence/desktop-node-pass-web-path-missing.log), [Web success](evidence/desktop-web-types.log). |
| Web i18n regression | Three tests passed, exit 0: [output](evidence/web-i18n.log). JSON-only ESLint reports ignored-file warnings and is not claimed as locale analysis; the Go catalog guard performs that verification. |
| Official core boundary | Exit 0; [output](evidence/core.log), official baseline `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046`. |

The pinned sqlc v1.31.1 `go run` generator requires Go 1.26 while the repository uses Go 1.25.7. Generation used the same official v1.31.1 Linux binary, verified against its release SHA256 `497ae4fcdfa64c5b0c311ffe4c2bd991e43991e82e5367792ed78bc2dca27354` for the archive; no generated Go file was hand-edited or generator downgraded. Swagger and SDK were generated using the repository commands. The current Server built successfully.

Real runtime/browser evidence used the existing cloud development listeners (Server loopback `18400`, Vite loopback `18402`) and the existing synthetic `integration_web_types` PG database. A complete private dump was retained before migration. The current binary applied schema175 and kept every existing operation byte-identical. A temporary loopback MCP test peer allowed only `workspace.list`; no real Windows, Herdr binary, paid model or production credential was used. The existing synthetic MCP URL was restored and the peer stopped afterward.

[Runtime/browser output](evidence/runtime-restart-browser-final.log) and [structured assertions](evidence/browser-proof.json) prove owner pause/deduplication, rejected foreign resume, anonymous 401/no-store, **actual Server process restart retaining the exact paused gate**, existing task observation while paused, and resumption with the same UUID. Seven native read-only probes, zero task dispatches, zero page errors, and unchanged operation snapshots. An earlier restart test encountered a reused Node HTTP socket being closed; it is retained in [failure output](evidence/runtime-restart-socket-failure.log). The harness used a fresh connection after restart; no product change or automatic mutation retry was introduced.

[Current task-detail screenshot](evidence/task-while-paused.png) shows the existing operation still observable while paused, with native/result ownership unknown. It does not show a stop permit, a new pause UI, successful Windows handoff or delivered business result. The screenshot was inspected. All published evidence is synthetic; private configuration, tokens, database dump and fixture identifiers used by orchestration remain outside the repository. Published files are byte-for-byte copies of the selected outputs; [checksums](evidence/checksums.json) cover them.
