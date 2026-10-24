# Native Agent content and follow-up in existing task details

This increment follows product checkpoint `24d9bf5d3f2c043db45b835dcdee864b4246b440` on PR79. It does not replace or publish the frozen `91af53f43566cb827d1fc58d18102e8ad29383bc` images.

## Scope and API

The existing task detail previously projected current metadata but could not show terminal text or call the existing native follow-up operation. Two adapters now reuse the computer-task service:

- `GET /bots/{bot_id}/sessions/{session_id}/tasks/computer_operation/{task_id}/content`: current authorized `agent.get` + `agent.read`, using the existing 200-line `recent-unwrapped` request and 16 KiB output cap. Returns `supported`, `availability`, `followup_available`, `session_binding`, existing `followups`, and optional `observation`. `output_fidelity=terminal_snapshot_unverified`, `truncated`, native completion sequence and the explicit lack of task-success verification remain intact. Polling does not append samples to the evidence ledger.
- `POST .../followup`: only `{request_id, mode, task}`; unknown fields are rejected. Reuses existing policy, CAS reservation, original operation run attribution, native get→prompt and deduplication. The HTTP caller cannot supply another target, pane, run, user or receiver. `accepted` means input delivery, never business completion. Errors after possible input remain `unknown`; no automatic replay.

Both routes use existing login, Bot/session ancestry and computer grants, with rechecks after the operation and `no-store` before JWT rejection. No new credential, authority model, Agent runtime, database migration or Windows protocol is introduced. The SDK and OpenAPI are generated from the handlers.

Batch Pi (`pi-json-v1`) content/follow-up is unsupported here. Existing batch receipts/results and file delivery remain separate. The native API's get→prompt identity race, optional session metadata and existing cumulative 64-follow-up limit are unchanged. A Herdr restart that changes terminal identity is not silently reattached or restarted. This change does not resolve batch recovery after such a restart.

## Shared Web and phone-host contract

`useSessionTasks(...).nativeTask` supplies `NativeTaskView`; pass it as the optional `native-task` prop of `session-task-results.vue`. The Web task dialog does so for its own and delegated specialist tasks. A host that omits this prop keeps its existing read-only rendering. The body has no DialogRoot dependency; the host continues to own dialog, navigation and visibility.

Content is loaded only after **Open terminal sample**, then polled every five seconds while that task is visible. It is plaintext in a bounded scroll region, not Markdown/HTML, a complete log or per-instruction output. Failed reads clear the old sample. 401/403/404 stop automatic content polling; explicit refresh can recover. Unsupported batch responses also stop polling.

Before POST, the browser saves a UUID in sessionStorage, under a key scoped to the login identity digest, Bot, session and operation. No token or instruction is stored. Storage failure prevents POST, but does not prevent read-only content retrieval. Unknown keeps the UUID through same-tab reload; GET reconciles existing receipts without sending input again. Existing server-side unresolved-request blocking still applies if browser state is lost or a different tab opens. `bound_session` is the default; `current_agent` explicitly permits uncertain logical-session continuity and does not claim strict resume.

This source integration is available to the phone host; no Harmony source/package or physical phone was modified or tested here.

## Verification and evidence

Current server binary + Vite ran on loopback against the existing isolated synthetic PostgreSQL database. Herdr was replaced by a loopback MCP fixture; no real PC, provider or model was called. The browser used the actual page, JWT middleware, task ownership/ancestry lookup, PostgreSQL operation CAS and existing Agenvo adapter. It was not a page-data mock.

| Check | Result |
| --- | --- |
| Go handler/service targeted command below | exit 0; 92 PASS records including subtests, zero SKIP |
| Five affected Web files | exit 0; 79 tests passed |
| Component after scroll-region-only adjustment | exit 0; 3 tests passed |
| Web and SDK typechecks | exit 0 |
| Changed Web files ESLint / Go formatter | exit 0 |
| golangci-lint 2.10.1 on both affected Go packages, all configured rules | exit 0 after environment recovery |
| UI contract | exit 0; three existing warnings retained |
| Current Go server build / Web build | exit 0; existing large-chunk warning retained |
| Actual browser + PG + synthetic MCP | exit 0; bounded escaped text, accepted vs unknown, offline/reconnect, reload dedup, 390 px view |

The final fixture performed two intentional synthetic prompt calls: one accepted and one lost-confirmation/unknown. Page reload retained the latter UUID and issued no third prompt. The operation stayed `submitted` at revision 6, with accepted and unknown follow-up receipts; it did not become completed. Its original predecessor operation was unchanged. Temporary synthetic MCP connection/profile changes were restored and verified. The fixture seeded a separate child session and task to preserve the existing unsettled operation rather than clearing it.

Browser screenshots (agent automation, not human QA):

- [Accepted instruction](native-content-accepted.png): acceptance explicitly leaves work completion unverified.
- [Unknown instruction](native-content-unknown.png): pending UUID and disabled resubmission.
- [Narrow shared view](native-content-narrow.png): escaped bounded sample and retained unknown request; not a phone binary test.

`evidence.json` lists the published unmodified log/image hashes. Private configs, tokens, full server logs and DB dumps are not published. These logs describe local checks, not GitHub CI; the parent follows exact-head CI through its authorized GitHub connection. This task's previously failed `gh` API authentication was not retried or replaced.

### Commands

```sh
go test -count=1 -v ./internal/handlers ./internal/computertask \
  -run '^Test(SessionTask(Native|Observation|NoStore)|Native(Content|View|Followup|AgentRecovery)|IntegratedNativeFollowup)'
NODE_OPTIONS=--no-experimental-webstorage pnpm exec vitest run \
  apps/web/src/pages/home/composables/useNativeTask.test.ts \
  apps/web/src/pages/home/components/native-task-content.test.ts \
  apps/web/src/pages/home/composables/useSessionTasks.test.ts \
  apps/web/src/pages/home/components/session-task-results.test.ts \
  apps/web/src/pages/home/components/session-tasks.test.ts
pnpm --filter @memohai/web typecheck
pnpm --filter @memohai/sdk typecheck
pnpm --filter @memohai/web build
go build -o <task-output>/server ./cmd/agent
```

### Earlier failures retained

`failure-log-index.json` identifies local originals, not a claim that those failures passed:

- Two handler test links ran out of task scratch disk. Only verified task-owned obsolete build archives and the stopped prior server binary were removed; source, PG and evidence were kept. The subsequent targeted Go test/build passed.
- One HTTP test assertion accidentally matched `prompt_accepted:false` while checking the top-level state; corrected to inspect `state` specifically. One UI assertion needed to await real asynchronous form validation. A fixture `Ref` annotation was corrected after typecheck. All relevant subsequent results are listed above.
- Browser-storage regression initially exposed a key being assigned before storage access succeeded; the key is now committed only after successful read, and storage failure cannot submit input.
- Fixture setup first collided with the existing unsettled-operation uniqueness constraint, then used nonexistent session columns. Both transactions rolled back. The corrected fixture uses a separate child session and the actual schema; it does not modify prior operations.
- The first synthetic prompt fixture omitted the required `cli:agent:prompt` receipt identifier. The real adapter correctly persisted unknown. That fixture task was retained and not replayed; the corrected fixture uses an independent synthetic task. This was a fixture error, not a relaxed product parser.
- Initial local lint could not create its root-disk cache; after selecting a task-owned cache, package loading reported no files. Direct reproduction located a Go telemetry mmap SIGBUS on the full root disk. A process-local writable configuration directory was used for the follow-up check, which completed with exit 0; no dependency, rule or product source was changed to mask it.
- The synthetic Bot has no workspace bridge: unrelated sidebar container/files requests still return 404/503. This check does not establish fresh workspace provisioning or a clean default deployment.

The official Agent runtime/contextview/toolexec trees are unchanged in this increment. No image publication, production migration, NAS/PC deployment, process termination or legacy-media remediation is performed.
