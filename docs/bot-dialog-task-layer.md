# Bot contact and task execution contract

Status: local result-origin, contact-navigation and persisted-directory slices,
incomplete. Base commit:
`a0a821a4cdaa32121a945d04e80c76bba4b6f2b4`. No production deployment,
model configuration, migration, package signing or paid model invocation.

This dedicated owner received the unified line's read-only v1 contract ZIP
(`libfile_8b87f13c26308191b33a362ae78b6c89`, SHA256
`5e1ed60c199fe41c015d8f1cbef439259d35f8257602b1c60f7b29f774d95a50`).
All 21 fixed source hashes in its evidence match a0. Neither line could read
the former navigation thread directly; its supplied diagnostic summary is
requirements evidence, not a claim to have read the original report.

## Separation and identities

The user's main entrance resolves the existing contact route for the authorized
team, authenticated account and Bot. Explicit original execution/history links
keep their targets. Task tabs, runtime engine selection and saved layout must
not replace contact identity. Existing historical chats are not automatically
adopted, merged, retitled or deleted.

The contact coordinates short conversation turns. Existing background Agent
tasks and authorized cross-Bot handoffs own independent execution contexts.
The long workload must release the contact's slot; its final result becomes
authorized model context for a natural assistant reply. Tool execution logs stay
in the original task context. A prompt preference alone is not a concurrency or
permission guarantee.

Same-Bot work must use native background tasks, not a fabricated cross-Bot
self-delegation: both authority and schema173 reject source==target.
`runtime_type=codex/acp`, a session title and a configured `bot_agents` row are
not task roles or proof of a task admission.

## Directory contract (four persisted sources implemented)

Use one additive read projection at `/bots/{bot_id}/tasks` on existing handlers,
services and persisted models. Do not rename `/activity` or the per-session task
endpoint into a complete directory. Keep their compatibility contracts intact.

Each item needs an original typed opaque task reference, safe name, actual source
state and a verified original destination, or an unavailable destination while
retaining the real row. The compact UI renders only name, state and navigation.
No task transcripts or diagnostic columns are added to the list.

| Source | Original task identity | Exact association |
| --- | --- | --- |
| Native Agent task | Actual background `task_id` | All matching `session_runs.invocation_id=subagent:<task_id>`, with owned child ancestry and submission evidence |
| Bot handoff | `bot_delegations.id` | Stored initiating and target Bot/session/run, incoming and outgoing under both live grants |
| Computer operation | `computer_operations.id` | Stored caller/session/target/fingerprint; provider `run_id` remains opaque TEXT |
| Schedule fire | Actual `schedule_logs.id` / FireID | Exact `schedule:<schedule_id>:<FireID>` invocation; never nearest-time matching |
| Other supported background work | Actual accepted original job/tool/task ID | Existing admission or receipt evidence; no guessed relationships |

Do not substitute each session's latest run or a conversation itself for a task.
Ordinary chat and synchronous reads do not automatically create directory tasks.
Keep all authorized failed/unknown/history records, multiple tasks per session,
and rows lacking a destination. Linked execution runs must not duplicate the
source task. Recheck all source-specific user/team/session/ancestor/target grants.
Page cursors must support opaque IDs, freeze creation order and remain bound to
principal/Bot/scope. A bounded page scan must not become a total-history cap.

The native background map and its pre-admission queued entries are not durable.
The next implementation must account for that gap through existing admission
models; this slice does not claim restart-complete native task discovery.

## Implemented slice: internal result origin

The existing authorized delegation host, target sessions, actual run observation,
result summary projection and deterministic follow-up identity remain in use.
No alternate execution service or task ledger is introduced.

An authorized result retains `DelegationResultID` across admission and queue
consumption. The host reconstructs result text and original attachments from its
live-authorized durable record, supplies model-only extraction instructions and
suppresses title generation and memory extraction for the result turn. Applying
the host's returned request before runtime dispatch also covers external-runtime
coordinators. Model behavior still requires live acceptance.

The machine input is persisted for model history under the existing
`message_source=internal_feedback` marker, with
`internal_input_kind=bot_task_result` and the original `delegation_id`.
Unlike mid-turn feedback, a task result leads its own durable history turn.
Live admission and persisted UI conversion omit this input. The assistant answer
remains visible. Ordinary user text is never hidden by matching its words.

Both queue text fallbacks ignore internal origin, including legacy complete
`command` envelopes already carrying `delegation_result_id`. New internal payloads
use `internal_command` with empty `text`; the a0 decoder sees no executable command
and no display text. Editing a machine queue input is rejected. UI retry/edit of
the resulting internal request is rejected before replacement publication, so it
cannot be reintroduced as ordinary user speech without the host's authority.

Existing terminal result invocation keys and their submission fields remain
unchanged. This avoids conflicting with results admitted before the upgrade.
The host's derived instructions are not a new task execution or an acceptance
receipt.

Shutdown resume stores internal origin and route in resume version3. A new
consumer rechecks the host's result authority before admission and again before
model execution. A0's version1/2 reader rejects version3 rather than downgrading
its identity. Old history readers already omit the shared internal source marker.
This fail-closed compatibility does not guarantee delivery under backend retreat:
retain the compatible consumer for recovery. No DB down/force/dump replacement.

## Validation and current blockers

New tests cover queue fallback/legacy decoding, live projection, early and normal
persistence, result-only history, two separately correlated queued results,
duplicate delivery, preservation of host-derived input, resume origin and
revocation before admission, and UI retry/edit rejection. The a0 differential
reproduces live user-bubble and queue-text leaks. A real isolated PostgreSQL
schema179 test confirms a result can lead its own turn and retain model history.

The modified application was built and started with real Server/Web and an
isolated schema179 database. Chromium logged in and sent an ordinary message.
That real flow failed before reaching any model: the local test Bot has no
workspace bridge socket and the normal user-message hook returned
`hook.user_message_failed`. The environment has no workspace image. No hook,
workspace readiness or permission check was disabled to bypass this failure.
The actual failure screenshot was captured and inspected. Therefore the
background-result natural reply UI flow is **not verified**, and this slice is
not ready for integration or production acceptance. A scripted loopback provider
was prepared but did not receive a model request.

Still unimplemented: durable self-background admission integration,
the remaining sources of the complete Bot task directory, and unknown-state
notification/reconciliation. Current unknown results remain persisted and are
not automatically redispatched, but their existing no-notification gap remains.
These are implementation gaps, not tasks completed by this origin fix.

Continue in the same dedicated task. The parent reports configuring Astra/high;
the worker cannot independently inspect the serving model, which is not a
product-work prerequisite. Audit the result slice's architecture, dual-Bot
authority, history/queue/resume compatibility and remaining task sources before
combining versions.
The unified line owns composition only after a fixed, independently reviewed
module handoff. No field work is assigned to this owner.

## Implemented slice: primary contact navigation

A plain Bot route now resolves the account's server-owned contact even when the
stored Bot already matches. Initial route intent takes priority over persisted
task or draft focus. Explicit task/history URLs keep their exact original session;
malformed session parameters fail closed. Primary contact navigation calls the
existing resolve endpoint directly, without adopting even a unique historical
candidate. The explicit historical binding APIs remain unchanged.

While a contact entrance is pending, layout restoration keeps its panels but
cannot replace global selection, and an empty layout cannot create a fallback
draft that cancels contact navigation. Once the contact panel opens, ordinary
manual task-tab selection works again. A later user selection invalidates an
outstanding contact response. The page masks stale task content during contact
resolution and shows errors instead of displaying that task under a failed
primary entrance. No schema, runtime/core or backend authority changes.

Differential checks against 27d2352f reproduce six failing assertions covering
same-Bot entry, restored task/draft priority, layout overwrite and automatic
history adoption. The changed frontend has 134 directed contact/navigation/dock
tests and 151 adjacent store/navigation tests passing, plus Web typechecking.
The neighboring legacy-server fixture now marks both contact endpoints absent;
the not-found page fixture stubs the already-existing contact dialog.

Real Chromium at `http://127.0.0.1:28082/bot/local-worker`, against the existing
isolated schema179 database and actual Server/Web, verified: explicit delegated
execution history; clicking the same Bot returns to its existing contact; the
task tab survives and remains readable; cold plain-Bot reload overrides saved
task focus; task deep links and reload retain the original execution; a 390px
browser uses the same contact identity. Inspected screenshots and
`contact-ui-evidence.json` preserve these observations. No model call was made.
The narrow-browser check is not native mobile or production acceptance.

## Implemented slice: persisted Bot task directory

The main Tasks control now reads the Bot-wide directory, including from an empty
contact. It no longer depends on the selected session or the active-only
`/activity` projection. The existing Bots control and old per-session APIs retain
their compatibility behavior. The additive SQL query unions original native
Agent admissions, computer operation receipts, incoming/outgoing handoffs and
schedule fire logs. It reads every occurrence, not each session's latest run.
An ordinary conversation run or a configured Agent contributes no task row.

Native task IDs are opaque and can recur across processes; the original run UUID
is a separate `record_id`, also used in the scoped keyset cursor. The original
background ID remains `task_id`. Admission input and authorized ancestry establish
the native relationship. A computer provider run ID remains opaque TEXT.
Schedule links use the exact schedule/fire invocation, and a fire without an
execution session remains visible. An unfinished fire is not successful merely
because the log defaults its status to `ok`.

Both list and detail reject chat-scoped tokens, use account authorization and
`no-store`, and recheck readable ancestry plus existing source-specific grants.
Handoff metadata checks original source identity, both live Bot grants and
ownership without exporting results or requiring yesterday's execution binding
to equal today's selection. Computer receipts retain their stricter original
owner/target/fingerprint checks. Schedule logs retain Manage authorization.
The 200-candidate scan limit is per page; an empty filtered page can continue.

The dialog displays names and states. Missing destinations disable navigation
without deleting the task. It rechecks the exact occurrence and original session
before pushing the explicit history route. That route alone owns selection and
tab opening: a real browser regression caught the competing old primary URL
when the dialog first updated global Bot selection. Account/Bot changes, closing
the dialog and logout invalidate pending reads. It never submits work.

Directed validation: four service tests, one delegation authority test and one
HTTP boundary test; seven frontend tests, Web typecheck/build and UI contract
checks. A rollback-only test on the existing isolated schema179 database checks
nine original occurrences, repeated native IDs, multiple runs per session,
opaque computer run IDs, incoming handoffs and unfinished/failed/completed fires.
It excludes ordinary and prefix-only forged runs. No migration was added.
The initial generated SQL alias error and an invalid schedule test-fixture
status were caught by real PostgreSQL, corrected, and the test passed.

Actual Server/Web at ports 28080/28082 returns three original handoffs in both
Bots, with failed/completed states. The unchanged active-only API returns zero.
Chromium opens the correct original execution, then returns to the fixed contact;
desktop and 390px screenshots were inspected. The task reads leave the original
11 runs and three delegations unchanged. Unknown rendering is covered by the
component fixture and PostgreSQL test, not a claimed production unknown run.

## Review findings and remaining acceptance gates

The prior internal-result slice's authority remains in the host: preparation
re-reads the durable record and delivery route at admission/resume and again at
execution, reconstructs model context, and preserves the original delivery key.
Queue/live/history origin filters and resume-v3 rejection of older consumers
remain intact. Existing two-result/replay and isolated model evidence are reused,
not promoted to proof of all concurrent collision orders.

The following are unresolved implementation or acceptance work, not tool denials:

- Native `background.Manager` still keeps queued entries and exec/spawn/video
  tasks in an in-memory map. Its event callback returns no error; observing a
  started event is not a fail-closed durable admission. The directory currently
  covers native work only once an original `session_runs` admission exists.
  Existing tool-result/history and dependency-operation ledgers need a separate
  exact-association adapter, with unknown retained after restart. Do not invent
  self-delegation, silently infer task roles from runtime type, or modify the
  official Agent core to hide this gap.
- Delegation `execute` still returns after saving `unknown`; `RetryDelivery`
  and host preparation currently admit completed/failed results only. A separate
  idempotent unknown notification and observation path is needed; neither this
  directory nor its detail endpoint retries execution.
- Cancellation/late-result suppression and user-message versus result publication
  ordering need controlled-barrier tests. Acceptance, result delivery and final
  assistant publication have different identities; text/time matching cannot
  prove exactly-once response or that a newer user message was answered.
- Autonomous same-Bot task selection, contact execution-log suppression beyond
  internal result inputs, loss of wake-ups/restart recovery, real model behavior,
  media/computer and native-mobile acceptance remain unproven. The first local
  end-to-end delegated task was submitted through the real HTTP API, not selected
  autonomously by a real model.

The parent's Grok/Rakazo research is additional design input, not code adopted
or independently reproduced here. Hiding execution logs is this user's explicit
requirement; this work does not claim all Grok behavior hides them.
