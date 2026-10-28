# Bot contact and task execution contract

Status: first local result-origin slice, incomplete. Base commit:
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

## Proposed directory seam (not implemented)

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

Still unimplemented: contact/default/layout precedence, durable self-background
admission integration, the complete Bot task directory, and unknown-state
notification/reconciliation. Current unknown results remain persisted and are
not automatically redispatched, but their existing no-notification gap remains.
These are implementation gaps, not tasks completed by this origin fix.

Next turn: use the same dedicated task with the parent-authorized Astra/high
review. Audit this slice's architecture, dual-Bot authority, history/queue/resume
compatibility and remaining task sources before expanding or combining versions.
The unified line owns composition only after a fixed, independently reviewed
module handoff. No field work is assigned to this owner.
