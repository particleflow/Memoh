# Native Agent follow-up: supported operations and boundaries

Base: `83483850061f4c908c28ff5ef340209cf9e718d5`. The test/documentation checkpoint
`52d7401ca725f2fae45c5807031e793f26049082` remains in history. Its conclusion that
an atomic incarnation API must precede all follow-up development was too strong.
Ordinary `agent.prompt` is supported; strict atomic identity and automatic
restoration of an old Pi conversation are not. This increment implements the
former with explicit observation limits.

## Reused application boundary

`computertask.Source` exposes `computer_task_followup` through existing standard
MCP and native tool adapters. Its host-owned binding supplies user, Bot,
conversation, run and selected computer. `Service.load` rechecks ownership,
selection, grants and registration fingerprint. The existing Pi authorizer
rechecks read/write/exec and target policies before reservation and again before
dispatch; the relay resolves authority for each native call. No new permission
mapping or CrossBot integration is introduced on this base.

`agenvo.SubmitAgent` still uses workspace.create, tab.create, agent.start(kind=pi),
agent.get and agent.prompt. The only addition to creation is retaining valid
optional `agent_session` metadata from its existing readiness observation.
Follow-up uses only agent.get and agent.prompt against the saved association.
It does not create an Agent/workspace/pane, replay the original task, or switch
`pi-json-v1` operations into the native branch.

| Identity | Meaning |
| --- | --- |
| `operation_id` | Original scoped Memoh operation, retained for every input. |
| Native Agent name | Herdr managed name; not an immutable process incarnation. |
| Workspace/pane/terminal | Saved references compared before input and in its receipt. |
| Registered target session | Native Herdr service session, not the Memoh conversation or Pi session. |
| `agent_session {source,agent,kind,value}` | Optional official logical conversation metadata: a private path or ID. |
| `PiSessionID` | Separate batch correlation value; never a native Agent ID. |
| Request UUID/fingerprint, operation revision | Host deduplication and CAS; never a native generation or atomic prompt token. |

## Explicit input modes

`computer_task_followup` requires `operation_id`, a canonical nonzero UUID
`request_id`, `mode` and `task`. Callers cannot supply target/native references
or session paths. There is no default mode:

The caller must save the intent UUID **before** sending, retain the exact
arguments, and reuse that UUID after a timeout/lost response. Generating a new
UUID for the same intent is not a retry: after an accepted receipt it can send a
duplicate new instruction. Server deduplication cannot infer intent equivalence
across different UUIDs.

| Mode | Precondition and meaning |
| --- | --- |
| `current_agent` | Send a new instruction to the currently verified name/kind/workspace/pane/terminal association. Logical session metadata may be missing or changed; the response says so. |
| `bound_session` | Additionally require valid original saved official session metadata to match the current observation. Missing/changed historical binding rejects before input, without downgrade or rewriting the original binding. |

Both require an accepted original native prompt, ready Agent, current authority,
no cancellation intent and no unresolved follow-up. Neither restores/resumes a
Pi session. Historical operations cannot acquire an original session binding
retroactively. `session_relation` is `unbound`, `unavailable`, `changed` or
`observed_match`. Session values stay in private operation JSON, never tool
results. Follow-up prompt text is not stored in this ledger, only its fingerprint.

Read the selected `followup.state` for this input. Top-level operation state
still describes the original operation, not this round. `followups` exposes the bounded history;
`computer_task_get` reads it without remote effects. All follow-up results keep
`business_outcome=unknown`, `continuity_verified=false`,
`atomic_identity_guaranteed=false` and `strict_resume_available=false`. Acceptance
is not proof that this round's business result has been collected, business
completion, file delivery, or proof of original Pi conversation
continuity. UI wiring and real PC acceptance remain separate work.

## Durable dispatch and uncertainty

The original operation's existing JSON has a **cumulative hard limit of 64**
follow-up records. Every reserved request consumes capacity, including one that
later becomes rejected; this is not indefinite follow-up or a rolling window.
Each record contains:
request UUID, fingerprint, initiating run, mode, progress, optional private
session observation and completion baseline. No schema migration is needed.
The writer patches only this key, retaining original operation/workspace/delivery
references and other JSON fields.

| Persisted state | Meaning and duplicate behavior |
| --- | --- |
| `reserved` | CAS acquired ownership before provider calls. Interrupted reservation is exposed as unknown, never resumed automatically. |
| `dispatching` | Prechecks passed and dispatch intent was saved before the single prompt RPC. Exposed as unknown pending durable receipt. |
| `accepted` | Exact prompt receipt and sampled native/session references matched. Duplicate returns recorded acceptance without another RPC. |
| `rejected` | No input was sent, or native response proves pre-input rejection. Duplicate returns recorded rejection. A new explicit request may recheck conditions. |
| `unknown` | Input may have been sent: transport loss, partial input, malformed/changed receipt or interrupted dispatch. Duplicate preserves uncertainty. |

Same ID with changed arguments conflicts. Any reserved/dispatching/unknown entry
blocks another request ID. There is no automatic resend, timeout reset, worker
takeover, ledger pruning or administrative uncertainty resolution. The 64-entry
bound refuses further input. A reservation write error never authorizes input;
the service reads the actual ledger. Finishing patches its own record under CAS
and preserves concurrent cancellation. Receipts may be saved privately after
revocation but are not released to a caller with invalid scope/grants.

Existing result/recover paths remain observations. They do not settle a follow-up
or associate terminal output with a specific round. Completion baseline does not
prove round completion. Existing delivery history remains attached to the original
operation; no native producer/delivery protocol is added. Cancellation remains
intent with `process_stopped=false`; no stop/kill/close/terminal keys are added.

## Official contracts and residual race

- Agenvo v0.2.0 commit `a89415e557c415faf5626354f8e87233eb69d877`:
  [`agent.prompt`](https://github.com/Xuanwo/agenvo/blob/a89415e557c415faf5626354f8e87233eb69d877/apps/herdr/src/herdr.ts#L341-L356)
  accepts session, name and text, without expected incarnation or deduplication key.
- Herdr v0.9.5 annotated tag object `7573e821895d32c8d1f78b3f6c9a2f85a46df4ac`
  points to commit `2ecf19a92fc3a7fff75b6ce3655ef04f4625ec01`.
  [`AgentSessionInfo` and `AgentPromptParams`](https://github.com/herdrdev/herdr/blob/2ecf19a92fc3a7fff75b6ce3655ef04f4625ec01/src/api/schema/agents.rs)
  separate optional session metadata from mutation parameters. No expected
  session/incarnation comparison is atomic with prompt delivery.
- The official [Pi state extension](https://github.com/herdrdev/herdr/blob/2ecf19a92fc3a7fff75b6ce3655ef04f4625ec01/src/integration/assets/pi/herdr-agent-state.ts#L74-L130)
  reports `source=herdr:pi`, agent pi and a session path/ID. Its
  [installation prerequisite](https://github.com/herdrdev/herdr/blob/2ecf19a92fc3a7fff75b6ce3655ef04f4625ec01/docs/next/website/src/content/docs/integrations.mdx#L70-L84)
  is unverified on the user's PC. No extension was installed here.
- [Managed name retention](https://github.com/herdrdev/herdr/blob/2ecf19a92fc3a7fff75b6ce3655ef04f4625ec01/src/terminal/state.rs#L2353-L2388)
  permits the name to survive same-kind session switches. Pi `/new` or `/resume`
  can change the conversation despite matching name/pane. Ordinary client
  detach/attach does not restart the process. Server cold restore
  [allocates a new terminal ID](https://github.com/herdrdev/herdr/blob/2ecf19a92fc3a7fff75b6ce3655ef04f4625ec01/src/persist/restore.rs#L592-L625).
- Herdr's separate [Pi resume arguments](https://github.com/herdrdev/herdr/blob/2ecf19a92fc3a7fff75b6ce3655ef04f4625ec01/src/agent_resume.rs#L135-L151)
  use `--session`. This feature does not invoke them or attempt session recovery.

There is a get→prompt race, including between the last authorization check and
native input. A session/reference change visible in the receipt becomes unknown:
input may already have reached a different session. Same-reference/session ABA
replacement is undetectable. Metadata may be absent or stale; matching
observations do not establish strict continuity. These are also boundaries of
the first-prompt procedure; a wrapper nonce cannot fix them. Callers requiring
strict original-conversation delivery cannot obtain that guarantee here.

The checkpoint counterexample models private provider incarnation changes with
reused references. It proves the absence of an atomic contract, not normal Herdr
restart behavior or impossibility of ordinary follow-up. No official Agent core,
runtime or contextview code is changed.

## Verification and remaining acceptance

Synthetic provider tests run the real service and standard Go MCP SDK over local
HTTP. They check original Agent/workspace reuse, session modes, retained history,
restart/deduplication, concurrent ownership, lost receipts, failed persistence,
missing/replaced/unready Agents, authority changes and cancellation around
dispatch. They do not execute Pi or establish real PC acceptance. Existing
PostgreSQL tests separately cover the reused durable CAS, ownership and RLS.

The product baseline is `fd8fe44f13ea250a1dbefe6c1628f3b27b042106`.
Checkpoint `7c2ee45b526cc8db75f13fe360ae95a4283ccb00` changed only tests,
the CI gate and documentation, and remains in history.
The earlier seven-green Go CI run did **not** select
`TestComputerOperationsMigrationAndDurableIsolation` in its PostgreSQL step.
Its success is therefore not evidence that the operation CAS/RLS test ran in
CI (that test had only been run locally).

Go CI now appends a separate **Require computer operation CAS and RLS (no skips)**
step using `scripts/test-computer-operations-postgres.sh`. It requires a DSN and
parses Go's JSON test events for the exact test's PASS, rejecting missing
selection, SKIP and failed/skipped subtests. The test creates its own database
and exercises concurrent claims, stale revisions, reconstruction, owner scopes
and a non-superuser RLS role. Its schema rollback never targets the service's
shared database. This tests the actual SQL contract underlying the application
ledger; it does not claim a full native follow-up against a real PC.

Additional controlled application tests use two separate Service instances and
a read barrier: both capture the same revision before either can reserve. They
cover the same UUID, distinct UUIDs, and competing requests for slot 64 with both
accepted and rejected outcomes. Exactly one reservation dispatches; old history
survives and rejected slot 64 still prevents slot 65. Dispatch-save response-loss
tests commit the dispatching record before dropping its acknowledgement: no
prompt is sent. They separately cover durable rejection and failed finalization,
where the persisted dispatching record stays unknown across reconstruction and
blocks a new UUID. None of these tests changes product behavior or adds recovery.

A subsequent narrow correction also compares fingerprints after a failed CAS
reload, matching initial deduplication. If another Service won with the same
UUID but different task or mode, the loser returns `request_conflict=true` and
the existing invalid-request error, never an unqualified receipt for that other
intent. Two additional same-revision barrier cases first reproduced the missing
conflict on the checkpoint, then passed with the correction. They verify that
only the winner's task is sent once, the persisted mode/fingerprint belong to
that winner, earlier entries survive, and retrying the loser neither rewrites
the ledger nor sends input. No schema, restoration or other dispatch semantics
changed; the non-skipping PostgreSQL gate is retained.

When combining Go workflows, append this gate while retaining **all** existing
memory/file-ref/delegation/transport and other contract gates. Do not replace
the combined workflow with this older baseline's workflow file.

Validation commands:

```sh
go test -p 3 -race -count=1 ./internal/agenvo ./internal/computertask ./internal/toolcontext ./cmd/internal/core ./internal/agent/application
go test -p 3 -race -count=1 ./internal/agent/tool -run '^Test(Computer|Agenvo|PCSession)'
bash scripts/test-computer-operations-postgres.sh # requires TEST_POSTGRES_DSN
bash scripts/check-agent-core-boundary.sh HEAD
git diff --check
```

The earlier broad tool-package run had seven pre-existing image-download fixture
failures due to the cloud proxy conflicting with its SSRF-safe synthetic
transport. No proxy/image code was changed; this is not a full-suite pass.
Exact committed-head CI results belong in the Draft PR.

No real PC/NAS, private artifact, credentials, OAuth renewal, runtime installation,
deployment or production stop was used. UI/browser/runtime acceptance and human
QA remain outstanding. The PR remains Draft. The parallel batch-history task may
share `internal/computertask/service.go` Details/Result wiring; batch behavior and
migrations are outside this increment.
