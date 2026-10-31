# Historical Agent input receipts and protocol boundaries

New TUI startup and follow-up input are retired. Native execution uses the
configured Memoh Runtime and `pi-json-v1`; see [native contract](native-runtime-herdr.md).
`computer_task_followup` can only read a persisted receipt when the original
request ID, mode and task fingerprint match. It creates no reservation and sends
no input. `reserved` and `dispatching` project as unknown without rewriting the
stored receipt. Old Agent progress, session references, unknown states and input
receipts remain under their original identity. The content view offers no new
follow-up action.

The material below records the earlier implementation and its fixed upstream
protocol evidence. It is historical evidence, not current operation instructions
or acceptance of this cleanup head. The former implementation and dispatch tests
remain available in Git at `910589c10e363545dd220cc3b9c212def0c815ec`.

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
