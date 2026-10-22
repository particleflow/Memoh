# Native Agent follow-up feasibility checkpoint

Base: `83483850061f4c908c28ff5ef340209cf9e718d5`.
This checkpoint adds synthetic contract/application tests only. It does not
enable a follow-up tool or API. The required guarantee that input reaches the
original native Agent cannot be established with the inspected interfaces.
The native Agent management gap therefore remains open.

## Reusable application boundary

`computertask.Source` already binds standard MCP and native tools to the same
host-owned Bot, user, conversation, run and computer selection. The service's
`load` rechecks ownership, selection, grants and the registration fingerprint.
`ComputerOperationStore` persists scoped records with revision checks.

`agenvo.SubmitAgent` already creates a workspace/tab, starts the official Pi
through Herdr, saves its native references, checks readiness and submits one
prompt. Its `agentInfo` and `validatePromptReceipt` functions compare the saved
name, kind, workspace, pane and terminal. These would be reusable checks for a
follow-up if the native mutation could also enforce instance identity. The
existing creation procedure was not rewritten.

The identities have different meanings:

| Identity | Meaning |
| --- | --- |
| `operation_id` | Memoh's owned durable operation record. |
| `AgentProgress.Name` | Herdr's live Agent name, scoped to a native service lifetime; not a durable Agent incarnation ID. |
| Workspace/pane/terminal | Native references retained for observation; matching them does not prove process continuity. |
| Registered `Target.Session` | Herdr service session; not a Memoh conversation. |
| `PiSessionID` | Separate `pi-json-v1` batch correlation value; not the native Agent name or an atomic Herdr generation. |
| Operation revision / target fingerprint | Memoh ledger concurrency / registration checks; neither fences a native process. |

`ExecutionMode=pi-json-v1` still uses the existing `SubmitBatch` branch. Nothing
switches it to native Agent mode, adopts it as an Agent, or replays its task.
Native records do not acquire batch completion or delivery claims. Existing
batch artifact delivery remains a separate ledger and is unchanged.

## Inspected official contracts

The versioned references already used by this repository were checked against
their tag commits:

- Agenvo v0.2.0: `a89415e557c415faf5626354f8e87233eb69d877`.
  [`herdr.ts`](https://github.com/Xuanwo/agenvo/blob/a89415e557c415faf5626354f8e87233eb69d877/apps/herdr/src/herdr.ts#L13-L31)
  scopes names/IDs to a live service and permits reuse after restart.
  [`agent.prompt`](https://github.com/Xuanwo/agenvo/blob/a89415e557c415faf5626354f8e87233eb69d877/apps/herdr/src/herdr.ts#L341-L356)
  has a strict schema containing session, name and text. It invokes the native
  CLI without a caller-supplied expected incarnation or prompt deduplication key.
- Herdr v0.9.5: `7573e821895d32c8d1f78b3f6c9a2f85a46df4ac`.
  [`AgentPromptParams`](https://github.com/herdrdev/herdr/blob/7573e821895d32c8d1f78b3f6c9a2f85a46df4ac/src/api/schema/agents.rs#L165-L170)
  contains target, text and optional wait options. Agent metadata includes
  revision, completion and optional agent-session information; the prompt does
  not accept those as expected identity conditions.
  [`validate_agent_prompt` / `submit_agent_prompt`](https://github.com/herdrdev/herdr/blob/7573e821895d32c8d1f78b3f6c9a2f85a46df4ac/src/app/api/agents.rs#L130-L268)
  validates the current native target. That validation does not compare it to
  the instance previously recorded by Memoh.

Agenvo's endpoint-generation calculation is internal connector/startup/event
tracking. It is not an expected Agent incarnation checked atomically by the
published prompt operation. The [connector dispatcher](https://github.com/Xuanwo/agenvo/blob/a89415e557c415faf5626354f8e87233eb69d877/packages/connector/src/main.ts#L269-L295)
checks instance configuration and transport request IDs, then invokes the
adapter. That does not compare a saved Agent incarnation either. Even an endpoint
identity alone would not identify a Pi process replaced inside that lifetime.

These findings are limited to the inspected contracts. They are not a claim
about every future upstream version or an installed PC's actual version.

## Counterexample and resulting boundary

`TestAgentFollowupIdentityBoundary` models a proposed get-then-prompt procedure
using the existing adapter and receipt validator. The provider's actual
incarnation is private fixture state, never a fabricated protocol field.

| Synthetic scenario | Precheck | Input recipient | Receipt validation |
| --- | --- | --- | --- |
| Original Agent remains | Matches | Original | Accepted |
| References reused before get | Matches | Replacement | Accepted |
| References reused between get and prompt | Matches | Replacement | Accepted |
| Terminal changes between get and prompt | Matches | Replacement | Unknown, after input was already sent |

Thus a second read, a random name, ledger revision, request hash, local lock or
post-input receipt comparison cannot provide the missing atomic identity check.
The counterexample establishes a limitation of the available contract; it does
not execute or prove a real Herdr replacement on a PC. Existing read-only
observations must continue to be understood as observations, not lifetime proof.

No follow-up state is invented in the ledger. The minimal mutation is deferred
rather than recording a false `submitted_to_original_agent` outcome. In
particular, the feature must not be exposed as a working continuation in a UI.

## Verified existing behavior

`TestNativeAgentLedgerReconstructionDoesNotResumePrompt` runs the real service
and standard Go MCP SDK over local HTTP with a synthetic Agenvo transport. It
checks accepted and applied-but-response-lost submissions, reconstructs the
service from serialized records, and verifies that repeated requests return
the original receipt. Different task text conflicts with the unsettled original
record. Neither case creates another workspace/Agent or sends another prompt.
Saved native references are retained and are not converted into Pi batch IDs.
Cancellation remains an intent with `process_stopped=false`.

`TestNativeAgentRecoveryRechecksOwnerSessionTargetAndGrant` verifies the real
Source/service path for same-owner recovery, NAS scope with a grant, changed
user/Bot/conversation, another selected PC despite its separate grant, revoked
runtime, removed grant and changed registered Herdr session. Authorized recovery
only uses workspace/pane/agent metadata; denied cases do not contact the
provider. Both preserve the original operation. This reconstruction fixture
is in memory and is not itself evidence of database durability.

The existing PostgreSQL integration test separately exercises concurrent
claims, reconstructed stores, revision conflicts, scope isolation and RLS in
an isolated synthetic database. Successful tests do not mean real PC acceptance.

Local validation on 2026-10-22:

- The three new tests and their 15 subcases passed.
- `go test -p 3 -race -count=1 ./internal/agenvo ./internal/computertask
  ./internal/toolcontext ./cmd/internal/core ./internal/agent/application`
  passed, including the existing batch/delivery regressions.
- `go test -p 3 -race -tags=integration -count=1 -v ./internal/db
  -run '^TestComputerOperationsMigrationAndDurableIsolation$'` passed against
  the prepared local PostgreSQL service. The test created and removed its own
  synthetic database, without stepping the application database backward.
- A broader run also included `./internal/agent/tool`; seven pre-existing image
  download fixture tests failed because the cloud environment's HTTP proxy
  conflicted with their SSRF-safe synthetic download transport. No proxy controls
  or image code were changed. This is not an all-packages pass.
- The focused computer-scope regression passed with
  `go test -p 3 -race -count=1 ./internal/agent/tool
  -run '^Test(Computer|Agenvo|PCSession)'`.
- `scripts/check-agent-core-boundary.sh` passed against official commit
  `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046`; `git diff --check` passed.

## Required next step

Resume implementation only against a verified official contract that can bind
the mutation atomically to the previously recorded native Agent incarnation,
including native restarts and replacement in the same pane. This checkpoint
does not request or make changes to Herdr, Agenvo, Pi or the Agent core.

At that point the minimal host change can append scoped follow-up receipts to
the original operation, retaining the original workspace and delivery history.
It needs a durable request identity, a pre-dispatch reservation and explicit
accepted/rejected/unknown states. Repeating a request must return its recorded
state; changed content under the same request identity must conflict. If input
may have been sent, unknown must block another dispatch, including after a
server restart or response persistence failure. A completion sequence or prompt
acceptance must not become business success. These are requirements for future
work, not implemented API names or promised upstream semantics.

No real PC/NAS, credentials, OAuth renewal, private artifact download, runtime
installation, deployment or stop operation was used. No UI was changed or
verified; there are no new application screenshots. Runtime/UI verification and
human QA remain outstanding, and this checkpoint must remain a Draft.
