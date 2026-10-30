# Session identity and input source isolation

Candidate fix for the user's 2026-10-30 request to display **and inject** each Bot conversation's actual input source, including automatic tasks. This is the dedicated identity/source workstream. Production acceptance is **incomplete**; no deployment or device acceptance was performed.

Independent review found a P2 in the initial live assistant projection; the original candidate was not admitted. See the [live projection follow-up](projection-followup/README.md) for its exact-turn correction, same-fixture red/green tests, actual application before/after evidence and unchanged resume v4 compatibility boundary. The earlier history-only verification below does not cover that live seam.

Branch: `fix/session-input-origin`. Exact parent: `9270da0252128a296d9a641235cdfd32ab44cfda` (the existing unified PR79 head). The parent subsequently reported that Server/Web 9270 were deployed; this source work is not in that deployment. No changes to the official Agent runtime/contextview, permissions, TLS, providers, credentials, database schema, or management/navigation controls. The source display is the explicit new user request; the existing copy baseline was updated only for that display.

## Findings and repairs

| Existing behavior | Repair and source |
| --- | --- |
| A schedule carried its authorization owner through sender resolution and lacked a durable, typed source. | `application/service_trigger.go` and `service_runtime_turn.go` record the actual plan/fire/run; `service_store.go` separates authorization from speaker identity. |
| Delegation could show the authorizing account instead of the initiating Bot/session. Result extraction had a different input identity again. | `cmd/internal/core/bot_delegation.go`, `task_result_delivery.go`, and `schedule_result_delivery.go` reconstruct source from the already-authorized ledger/receipt on admission and recovery. |
| Ordinary model requests did not consistently carry stable receiving Bot/session identifiers. | `application/input_source.go` and `service.go` add a required, noncached source section through the existing context-fragment contract; external runtime context uses the existing host section contract. |
| Native queued subagents used a separate model/persistence path. | `cmd/internal/core/input_source.go` decorates existing host ports. `native_task_queue.go` restores exact task/parent/source-run scope. The optional resume checkpoint interface remains forwarded. The runtime itself is unchanged. |
| Retry/continuation could attribute the original input to the account performing the retry; interrupted work could lose its source. | `service_retry_edit.go` and `service_continuation.go` read the exact original message; `session_resume.go` preserves the source. Version 4 prevents an older consumer from treating newly annotated automated inputs as owner-authored input. Existing result-version authorization remains in force. |
| A later input lacking its own verifiable actor could borrow the enclosing turn's actor. | `service_store.go` explicitly marks that input and subsequent replies unknown; it clears borrowed speaker IDs. It does not invent a source for missing provenance. |
| Historical messages and UI projections lacked corresponding source fields. | `context/history/db_message.go` replays recorded provenance as user-role context without rewriting raw history/hash. `view/uimessage*` exposes only the typed, scoped source. Web transcript normalization and `message-item.vue` use that same field for labels/tooltips. |

`internal/inputsource/source.go` contains only routing identifiers. It is context/display data, never an authorization grant or runtime selector. The internal request field is `json:"-"`; a user body cannot set it. Source annotations are checked against the receiving Bot and session. Unknown, malformed, foreign, and old unannotated rows remain unknown. User/task content stays in its original message role; neither task output nor a quoted source header becomes a system instruction. No historical rows are migrated or rewritten.

## Model injection and display contract

| Input | Trusted origin | Model and persistence | Visible source |
| --- | --- | --- | --- |
| Direct user | Authenticated ingress user/channel identity | Receiving Bot/session plus actual actor/channel | Bot, user input, channel, actor |
| Schedule | Existing schedule payload/occurrence | Plan ID, exact fire ID and admitted task run; owner is not speaker | Bot, automatic task, plan ID; exact fire/run in tooltip |
| Bot delegation | Existing authorized delegation record | Receiving Bot/session, delegation ID, initiating Bot/session | Bot, Bot delegation, task ID; origin in tooltip |
| Native task | Existing durable native queue receipt and restored authority | Child Bot/session, task ID/run, original parent/session/run | Bot, background task, task ID; origin in tooltip |
| Persistent task result | Existing authorized result receipt/ledger | Receiving original conversation, actual child/task/fire/run | Bot, task result, task ID; origin in tooltip |
| Old/unknown | No verified annotation | Scoped `unknown`; no guessed actor or source | Bot, source unknown |

The model system section contains source identifiers and fixed role/scope guidance only. Historical source annotations use user-role context. The visible UI reads server fields, never parses provenance out of body text. Labels follow current names only for presentation; scope comes from native IDs. Tooltips are keyboard accessible. The local TooltipProvider is necessary because Dockview renders the chat in a detached component root.

## Verification performed

All identifiers in these artifacts are synthetic. No paid model call was made.

- The actual native Agent/SDK assembled HTTP requests were captured by an isolated local provider fixture. The tests assert exact source JSON in system context, forged body text exclusively in user context, and equality with persisted source and the actual Go UI converter. Two random Bot/session/actor groups run for direct input, schedule-shaped input, delegation and task result. A separate actual `TriggerSchedule` test checks plan/fire/admitted run; native SpawnAdapter/provider captures cover two more scopes.
- Real PostgreSQL native queue restart, concurrent duplicate claims, revoked access, cancellation/unknown/no-replay and task/schedule result crash-window fixtures passed. Their source assertions use the actual restored receipts and exact original runs. No source annotation bypasses existing admission checks.
- Cancellation captures exactly one model request and preserves the original actor; retry/resume/foreign-scope and unattributed mid-turn inputs have dedicated checks. Existing steer and resume tests were retained.
- Final race log: **25 top-level tests and 8 subtests passed** across application/core, including the three real PostgreSQL fixtures (not skipped). See [race-final.log](race-final.log).
- The final lint review caught four new issues (contextcheck, redundant nil check, test string concatenation and an unused test parameter), all corrected. Source capture plus all subagent step-commit tests were rerun with `-race`: **10 top-level tests and 4 subtests passed**. This additionally verifies that a later callback carrying a different actor cannot replace the admitted task source, including the interrupted checkpoint: [source-and-step-final.log](source-and-step-final.log). The fixed source prompt explicitly limits the top-level annotation to the admitted input; later inputs without separate host provenance cannot inherit that actor. Such unattributed inputs remain unknown rather than receiving an invented identity.
- Final golangci-lint 2.10.1 checked all seven changed Go packages with staged changes and `--new-from-rev=HEAD`: exit 0, no new issues. The first unfiltered scan also reported an existing unused `modelID` parameter in `createWSChatSession`, outside the changed lines; [go-lint-initial.log](go-lint-initial.log) retains that result. A subsequent attempt failed from task-cache disk exhaustion, not a code or permission rejection: [go-lint-disk-full.log](go-lint-disk-full.log). Only the task's disposable Go build cache was cleared (5.3 GB); records and source were preserved. The affected lint then passed. No claim of a clean unfiltered full-repository lint is made.
- Selected existing application/channel/handler/core regression tests passed: [regression.log](regression.log). Full history and view package tests passed: [history-view.log](history-view.log).
- Web: **5 files / 105 tests passed** (99 in [web-tests.log](web-tests.log), 6 in [web-identity-tests.log](web-identity-tests.log)), including all six source kinds, body spoofing, source propagation, task self-identity and a mismatched selected Bot for every source kind. A current selected Bot's display name is used only when its ID matches the recorded receiving Bot; otherwise the source's actual ID is shown. Vue typecheck, Go vet, final server build and `git diff --check` passed. ESLint: zero errors, three existing test-mock default-prop warnings. UI contract: passed with three inherited warnings. Copy policy: passed, 2033 reviewed surfaces. Official Agent boundary: passed. See [checks.txt](checks.txt), [eslint.log](eslint.log), [ui-contract.log](ui-contract.log).

The repository-pinned Go 1.25 toolchain and pnpm 10.27.0 were used in task-specific temporary directories. No lockfile or dependency version was changed.

Representative repeatable test commands, with explicitly supplied isolated migrated PostgreSQL DSNs in the environment:

```sh
go test -race -count=1 -v ./internal/agent/application ./cmd/internal/core \
  -run 'InputSource|NativeTaskQueuePostgres|TaskResultPostgres|ScheduleResultPostgres|Resume|.*Steer'
go test ./internal/agent/context/history ./internal/agent/view
go vet ./internal/agent/application ./cmd/internal/core
node scripts/check-ui-copy-policy.mjs
node scripts/check-ui-contract.mjs
bash scripts/check-agent-core-boundary.sh
```

## Actual running UI evidence

Local Server `http://127.0.0.1:28080`, Vite `http://127.0.0.1:28083`, actual isolated PostgreSQL database `memoh_input_source_20261030`, migrated through 181. The current server was rebuilt from the final code and the source views were rechecked. Chromium rendered the real application through its actual REST/SDK/Dockview path; these are not component mocks or static previews.

Provider-test source captures were deliberately rehydrated as **synthetic history fixtures** in that isolated database. This checks presentation/API/source equality, not a complete production end-to-end turn. Eleven conversations cold-opened in fresh browser contexts: two users, two schedule-shaped sources, two delegations, two results, one real schedule fire/run, one native task, and one unknown legacy row. Every API source matched the expected captured source; all source labels were translated; tooltips contained the exact receiving and origin IDs; no page errors occurred. Both schedule and user narrow/dark keyboard-tooltip checks passed at 390×844 without document overflow.

The exact API/UI source records are [ui-evidence.json](ui-evidence.json) and [ui-user-evidence.json](ui-user-evidence.json). [wire-display-crosscheck.json](wire-display-crosscheck.json) checks each known displayed source against the captured test logs; unknown has no fabricated capture. Capture logs: [provider-captures.log](provider-captures.log), [provider-captures-extra.log](provider-captures-extra.log), and [race-final.log](race-final.log).

| Screenshot | Demonstrates |
| --- | --- |
| [User 1](source-user-1.png), [User 2](source-user-2.png) | Different Bot/session/actor IDs remain separate. |
| [Schedule](source-schedule-1.png), [Exact fire/run](source-scheduled-fire.png) | Automatic source is distinct from its authorization owner; exact plan/fire/run shown. |
| [Delegation](source-bot_delegation-1.png) | Receiving and initiating Bot/session remain separate. |
| [Task result](source-task_result-1.png), [Native task](source-native-task.png) | Saved result/child task ownership remains visible. |
| [Legacy unknown](source-legacy-unknown.png) | Forged body JSON does not produce a trusted actor. |
| [Schedule narrow/dark](source-schedule-narrow-dark.png), [User narrow/dark](source-user-narrow-dark.png) | Keyboard tooltip, readable wrapping and no horizontal overflow. |

## Incomplete acceptance and blocked actions

1. **History navigation/refresh:** same-browser switching to another Bot's history, or reloading a history panel, can leave that panel empty although the message API returns the correct two rows. Cold opens work. This is explicitly **not passed**, and no navigation fix was added to this workstream. Evidence for the original navigation owner: [navigation-second-bot-empty.png](navigation-second-bot-empty.png), [navigation-second-bot-empty.json](navigation-second-bot-empty.json). Both UI evidence JSON files mark refresh unverified.
2. **Complete direct app → workspace Hook → model turn:** the synthetic Bot's workspace bridge is absent. Original failed run `ba147952-f14a-4ebf-9ac3-2a15a010c9eb` stopped at `hook.user_message_failed`, before model invocation. The original failure/history is preserved; hooks were not disabled. Separate native SDK/HTTP capture and actual UI checks do not claim to replace this missing end-to-end acceptance. The unique field writer must perform that step on the authorized working environment.
3. **Local contact binding:** `POST /bots/d471170b-57d7-4ab1-968c-dd96bec87f13/sessions/contact/bind` returned HTTP 403 (`http.forbidden`, request ID `6751df01-08c2-4a2b-b4b7-d4db66b5d62d`). The action was **denied and stopped**. No ACL change, SQL binding, alternate route or retry was used.
4. **One old development process:** identity verification for cleanup read `/proc/27592/cwd` and the OS returned `PermissionError: [Errno 13]`. That read/cleanup action was **denied and stopped**, without an alternate-tool read or kill. The old task Vite process on 28082 (launcher 27589, child 27592, original exec session 83492) remains pending cleanup by the environment maintainer. No shared PostgreSQL process or stored history was removed. Normal source sessions were used for the later disposable runtime processes.
5. This workstream performed no production Server/Web release, NAS/Windows mutation, Harmony phone acceptance, external model call, or human QA. The parent's later 9270 deployment receipt is separate evidence; this source work has not been deployed. Existing PR79 CI was not cancelled, rerun or modified. This branch is a composable candidate, not a claim of completed field acceptance.

At this workstream's natural handoff boundary, the compatibility incident register was read at exact commit `3a2e5d97e7f26fdcd47195f0f89bbb5c852b01a5`. C05 applies to the separate denied process-metadata read above; no retry or bypass was made. C11's field first-load failure is parent-reported and distinct from the local history-switch/reload observation above: the observations do not establish a shared root cause. C01/C02/C03 remain outside this workstream; no Library materialization/read was attempted. Managed terminal Ctrl-C did not reliably stop the launched child here, even when using `exec env`; that attempt is not a proven cleanup method. The later task-specific Server, Vite and synthetic provider were identified by their original receipts and unique absolute binary/config/script arguments, then sent normal SIGTERM. The earlier denied Vite cleanup was not retried. No production recovery or broader cleanup grant is inferred from these separate local operations.

Human QA: not verified.

For the unified owner: [compatibility-register.patch](compatibility-register.patch) adds this workstream's separate C05 observation and the verified task-cache recovery (C12) to the exact registry at `3a2e5d9`. It is a zero-context patch: check/apply with `git apply --unidiff-zero` on the unified documentation line after review. This branch does not replace or recreate that concurrently maintained register.
