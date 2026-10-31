# Direct parent references in session task observation

Base: `b7f478bc1b79209207b0061bdf5af874406d6399`. This is a later source checkpoint; it does not change the fixed `2895ce47` image release.

## Contract

The existing recursive task query follows persisted `bot_sessions.parent_session_id`. The existing task projection already loads and authorizes the complete ancestor path. It now includes one optional field from that path, without new queries, schema, authorization rules, runtime state or task writes:

| Field | Meaning |
| --- | --- |
| `task_id` | Original task/session or computer operation identifier, unchanged. |
| `session_id` | The task's owning session, unchanged. |
| `parent_session_id` | Requested aggregation root, unchanged; **not** the immediate parent. |
| `direct_parent_session_id` | Persisted direct parent of `session_id`, after ancestor authorization. |

Both existing task list and detail endpoints supply this projection. The generated SDK marks the new field optional. All 369 existing HTTP operations and existing required task fields are retained (`contract.json`). No SQL, migration, Agent runtime, contextview, provider or auth changes.

The shared `session-task-results.vue` body displays explicit owning-session and direct-parent references in list rows, and the query root separately in detail. Web and native-dialog hosts reuse the same body. Missing fields (old server or no parent) display “No reference provided”; they do not infer a relationship. A parent absent from the current page remains an explicit reference, not a fabricated node. Names, order, execution status and page boundaries never create ancestry or imply completion. This is a reference view, not a complete task-tree renderer.

## Verification

All data was synthetic. The current Server binary and Web source ran in the existing isolated cloud environment with its PostgreSQL database. Browser URL: `http://127.0.0.1:18402`; API: `http://127.0.0.1:18400`. No NAS, real provider, PC, Herdr, OAuth or phone operation was performed.

| Check | Result |
| --- | --- |
| `go test -race -count=1 -v ./internal/sessiontask` | Exit 0; three levels, identical names, distinct query roots, limit-1 deduplication, unchanged operation records, unavailable ancestors/owner/Bot/orphan/cycle rejection. |
| `go test -race -tags=integration -count=1 -timeout=90s -v -run '^TestSessionTasksPostgresOwnedTreeAndStableCursor$' ./internal/db` | Exit 0, no skips; real migrated PostgreSQL, real session service, direct parent projection, cross-team rejection and deleted-ancestor exclusion. Existing cursor checks retained. |
| `go test -race -count=1 -run '^TestSessionTask' ./internal/handlers` | Exit 0; existing task authorization, revocation, no-store and HTTP contracts. |
| Five affected UI suites | 83/83 passed; after test-only optional-array-access correction, new relation suite 2/2 passed again. Web and native HTML dialog hosts covered. |
| Web and SDK typecheck | Exit 0. |
| `go generate ./internal/handlers`, `pnpm run generate-sdk` | Exit 0; source-driven schema/SDK generation. |
| Server build and Web production build | Exit 0; existing large-chunk warning retained in `web-build.log`. |
| Actual Server/PG HTTP | Three newly seeded sessions, two same-name subagents; limit-1 pages unique; nested detail query preserves direct parent; anonymous 401/no-store; full original operation rows unchanged before/after. Only these three fixture sessions removed afterwards. |
| Actual browser | Real task endpoint responses, list → detail → Chinese narrow-screen reload, exact IDs and unknown execution state; zero observed mutation/device calls and zero page errors. Unrelated workspace-target discovery was stubbed to avoid device refresh; task responses were not stubbed. |

The staged aggregate passed (exit 0): selected Web ESLint, Go lint and the four selected Go package tests. UI contract passed with three existing warnings. Logs are in `aggregate.log` / `ui-contract.log`; exact exit summary is in `checks.json`. This is local verification, not remote CI or human QA.

## Reviewable evidence and initial failures

Selected logs are included with hashes/byte sizes in `evidence.json`. Five logs have only trailing horizontal whitespace and excess blank EOF lines normalized; per-file original hashes and exact rules are recorded (raw bytes remain in product commit `73a71323` and the private scratch evidence). Other logs are verbatim; no private credentials/configuration, raw server logs or DB snapshots are included. `browser.mjs` is the exercised runner with filesystem/URL inputs parameterized; seed synthetic sessions with real parent relationships and provide a fixture JSON containing `bot`, `root`, `child`, `grandchild`, `title`. Reusable database fixtures are also committed in the Go integration test.

Initial failures are retained: the new PG test first used the wrong queries adapter; the new UI test's substring assertion matched `grandchild-session`; typecheck caught optional array accesses; browser selectors matched a child ID in both title and parent reference, then expected the wrong capitalization of the existing unknown-state label. These test/harness defects were corrected without changing product semantics.

The first aggregate lint attempt failed writing Go cache with `no space left on device`. Four older, regenerable Go archive cache entries (668,400,260 bytes) were removed from this task's cache, preserving source, logs, DB and runtime binaries; see `cache-recovery.json`. An earlier attempt to verify an obsolete binary via `/proc/<pid>/exe` received filesystem permission denied, so that cleanup did not execute and was not retried. No permissions were changed.

Screenshots were captured and inspected from the actual application; they are agent verification, not human QA:

- [List](list.png): owning and immediate-parent references distinguish the two descendants.
- [Detail](detail.png): grandchild ownership, immediate parent and requested root are separate.
- [Chinese narrow screen](narrow-zh.png): 390px dark-mode detail, complete IDs, no horizontal overflow.

Remaining: real PC/Herdr/phone ancestry and end-to-end business execution are not established by this synthetic check. This change does not add stop/cancel actions, new polling, deployments, or cross-Bot delegation ancestry semantics.
