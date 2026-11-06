# Current computer observation from a conversation

Base: `91af53f43566cb827d1fc58d18102e8ad29383bc`. Its published images stay unchanged.
This follow-up connects an existing read-only capability, not a new runtime,
queue, log service, task ledger, or Windows protocol.

## Confirmed coverage and gaps

| User flow | Existing implementation and actual boundary | This increment |
| --- | --- | --- |
| Supervisor → specialist → child/PC tasks | `session-tasks.vue` opens the specialist session after revalidating its delegation; `sessiontask.Service` aggregates authorized descendants. The list is recorded tasks, not a scheduler queue or a complete visual execution tree. | Preserved. |
| Persisted child content | A child run's latest visible assistant text and run state are projected. This is polling persisted messages, not a live terminal stream. | Preserved. |
| Same PC work in Web/shared phone view | Only final summaries and receipts were visible; existing `computer_task_recover` could query original Herdr references but had no conversation HTTP/UI adapter. | Added the adapter and shared observation body. |
| Device/workspace attribution | The recovery service already returns target/workspace/pane/Pi-session IDs. | Display these existing IDs and the response time. No unverified friendly name or device discovery is invented. |
| Native state / reconnect | Existing recovery checks workspace/pane/terminal identity and returns typed availability. | Visible-detail polling, manual refresh, stale response retirement, old-server 404, and distinct unknown/mismatch/offline states. |
| Running PC content/logs | Batch mode supplies a verified **final** visible reply; native `agent.read` is unstructured terminal text, not a safe visible-assistant stream or a per-instruction completion proof. Raw logs/hidden reasoning are not part of the task HTTP projection. | **Still missing** for running PC work. This increment does not claim live output. |
| Computer management | MCP follow-up exists with a retained request UUID and explicit session mode; cancel records intent, not verified termination. Workspace discovery is read-only. | No pretend Stop/Replay/Delete controls. A safe Web follow-up adapter is not part of this read-only increment. Existing specialist result-delivery retry remains unchanged. |
| Herdr restart | Existing recovery detects retained pane IDs with changed terminal IDs. It does not rebind, resume, drain, or recover uncollected results automatically. | Display mismatch; never restart a task from observation. Real Windows restart/continuity remains unaccepted. |

The running-content gap needs coordination with the Windows owner: first confirm
whether the deployed official Herdr/Pi interfaces expose a bounded **visible
assistant text** read for the original invocation/session, with distinguishable
missing/changed identity and continuation position. Current `agent.get/read`
metadata and terminal snapshots alone do not establish per-input result ownership.
No new endpoint/protocol, terminal-input query, daemon, or Agent patch is introduced
here. If the installed native interfaces cannot meet this read contract, the
adapter boundary must be agreed before either end changes it.

## HTTP and shared-host contract

`GET /bots/{bot_id}/sessions/{session_id}/tasks/computer_operation/{task_id}/observation`
returns the existing `computertask.Recovery` projection. Native state remains
separate from persisted process/Pi/business results. `done`, `idle`, and matching
references are not task success. `receivedAt` in the UI means response receipt
time, not a native event timestamp.

The handler derives the real operation's owner session and fixed target after
JWT, Bot chat permission, ancestor, scope, grant and registration checks. It calls
the existing recovery service, then repeats Bot permission and ancestry checks
before releasing the response. Model/query/header-supplied native references are
not used. Normal denied responses and early JWT failures have `no-store`.

`useSessionTasks(...)` now returns `observation` alongside `deliveries`. Hosts
pass its current value as `:observation` to `SessionTaskResults`; the existing Web
root and specialist paths are wired. The shared body still has no DialogRoot
requirement. A Harmony host must pass this property through its existing reader;
no mobile package has been rebuilt or verified here. The host still owns scope,
visibility, navigation and `reader.back()`.

Only a visible selected computer task polls (5-second existing reader cadence,
single flight, 15-second request cancellation; native recovery is bounded to 10
seconds). Disconnection clears current observation. A 401/403/404 stops automatic
polling until explicit refresh or a scope change. An unsupported observation
route does not remove the independent persisted task view. Neither this route
nor the UI submits, collects result files, retries work, types in a pane, cancels,
or updates a receipt.

## Executed verification

- Go: `go test -count=1 -v ./internal/handlers ./internal/sessiontask ./internal/computertask -run 'Test(SessionTask|ParentAggregates|ReadOnlyRecovery|WorkspaceDiscoveryAndRecovery|NativeAgentRecovery)'` — exit 0, three packages, nine top-level tests, no SKIP. Includes forged scope arguments, early/late revocation, no-store, unchanged receipts and existing native recovery boundaries.
- Four affected Vitest files with the repository's existing `NODE_OPTIONS=--no-experimental-webstorage` test environment — exit 0, **76 tests**. Covers Web and native HTML-dialog mounts, A→B→A late success/rejection, scoped cancellation, disconnect/reconnect, unsupported routes, single flight and status separation.
- Web and SDK typecheck — exit 0. Go server build and Web build — exit 0. Targeted product ESLint — 0 errors/warnings; the subsequent complete affected-file lint also includes the existing test files and exits 0 with six component-per-file warnings. Both logs are retained. UI contract — exit 0, three existing warnings. Web chunk-size warning retained.
- Existing OpenAPI paths all retained; exactly one read-only path added. SDK/spec generated by the existing `go generate ./internal/handlers` and `pnpm run generate-sdk` commands.
- Actual current server + JWT + existing isolated PostgreSQL + real shared Web. Herdr is an **explicit synthetic MCP metadata fixture**, not a PC, model or Agent execution. One synthetic operation was seeded; only `workspace.list`, `pane.get`, `agent.get` were called. Zero business dispatches. Original synthetic connection configuration restored; operation state/revision/details unchanged.
- Browser: working identity, native done without business-success inference, explicit offline, changed terminal identity, network reconnection, unsupported route/manual refresh, and 390px Chinese dark view. Screenshots were inspected. This is not a Harmony binary or Windows/Herdr/Pi end-to-end acceptance.

Earlier harness outcomes are not erased: the first Vitest invocation omitted the
existing Node WebStorage flag; subsequent fixture assertions used an ambiguous
Reload label (now the computer refresh has its own label). The first browser
script expected a nested error after the parent reader had correctly cleared the
whole detail on disconnection. The current desktop evidence records five passing
flows followed by an invalid Playwright `context.setViewportSize` call; the
separate corrected narrow run passed. An initial direct-narrow navigation could
not select the hidden desktop sidebar, so the scoped body was opened then resized,
as in the existing shared-body acceptance. The synthetic MCP fixture did not
implement optional `server/discover`; metadata reads still completed. These are
not claims that the entire application or all optional MCP methods were tested.

Evidence copies normalize only repository/fixture path prefixes; `evidence.json`
records original and published hashes. Private configuration, tokens, raw server
logs and database contents are not published. No source changes occur in official
`internal/agent/runtime`, `internal/contextview`, or `packages/runtime`.

Production P1/OAuth/installation holds are unchanged. This source checkpoint is
not a new image, a deployment, or completed PC management.
