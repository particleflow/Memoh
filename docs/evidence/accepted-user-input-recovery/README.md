# Accepted ask-user recovery

Prerequisite: `daba5a37a4804cd6c7659a1bdc290a24ddbe03d2`, based on
`9d91623e1f3325066adddfa8a11db16cef0d46a7`. That prerequisite makes newly
parked retry/edit replacements visible before an answer continues them.

## Confirmed failure and scope

A native retry run can have a persisted submitted ask-user answer and an
accepted continuation checkpoint while its original step rows remain hidden
and unbound. Looking up its admitted history turn then returns NoRows before
the answer tool result or any following model/tool call. The retained original
retry source and server-authored `resume.query` provide the original input;
the latest message in the conversation is not a substitute.

This recovery is deliberately restricted to that proven native **retry**
checkpoint. It does not recover arbitrary provider failures, executing tool
approvals, unknown side effects, edit runs, other runtimes or missing original
input. Such requests fail closed without a provider call.

## UI contract

Use the generated SDK function
`postBotsByBotIdSessionsBySessionIdDecisionsByDecisionIdRecover` for an
**explicit click on the existing failed-response prompt**, after identifying
the original run and submitted decision. Do not infer eligibility solely from
`runtime_run_failed`; the server revalidates eligibility. Do not add a button or
automatically call on deployment, rendering, reconnect or polling.

```http
POST /bots/{bot_id}/sessions/{session_id}/decisions/{decision_id}/recover
Content-Type: application/json

{"original_run_id":"<original failed run UUID>"}
```

The current account JWT and owned native chat session are required. Chat-route
tokens are rejected. The body cannot replace answers, prompts, models,
workspace identities or credentials. Existing Bot permissions, active-account
validation and session ownership apply; permission is rechecked before history
repair and immediately before starting the model stream.

HTTP 202 returns only:

- `original_run_id`, `decision_id`: unchanged original identities;
- `recovery_run_id`, `invocation_id`: the normal continuation admission to
  observe through the existing session runtime/invocation interface;
- `state`, `started`, `replayed`: actual admission facts, not business success.

The original run remains failed. A continuation has its own normal run rather
than rewriting the original terminal history. The invocation key is stable per
decision: concurrent clicks and later replays return the same run. A replay
whose state is failed/lost/unknown does not start another execution. HTTP 409
means the required safe checkpoint is unavailable or changed; never fall back
to whole-turn `retry_message` or resubmit the original answer. HTTP 403/404 and
other errors retain the normal application authorization/error contract.
Refetch session history through the existing query after admission, then
observe `recovery_run_id` through the existing runtime/invocation reads.

## Persistence and execution boundary

- Current runtime admission/fencing remains the only execution owner.
- Inside its fenced transaction, lock the session and compare the original
  run's failed state, fencing token and `updated_at`; lock the submitted
  decision. Require its one accepted user-input checkpoint.
- Reject newer unrelated runs, a changed retry source, an already persisted
  answer result, any post-answer output, foreign unbound suffix rows, stale
  ownership or oversized history under the existing configured history budget.
- Select only the exact original replacement through the existing history
  replacement SQL. Preserve row contents and original failed run/answer facts.
  Persist the already accepted answer as one tool result with origin metadata.
- Save the existing resume context in that same transaction only after history
  repair. A crash before repair must not auto-restart the original query.
- Run the native model from repaired history and the accepted answer using the
  existing native stream/step persistence. Do not run earlier tools again.
- A subsequent ask-user in the continuation uses the server-authored original
  input anchor, with scope/fence checks; no latest-input fallback.
- Use the existing required-history mechanism to retain that original input
  and its dependent tool history even outside the ordinary recent-history
  window. This does not append a replacement user message or raise budgets.
- An uncertain database commit starts no model. The same action's next request
  reports its existing run instead of generating again.

There is no new queue, scheduler, model adapter, credential store, migration or
Agent-core change. The existing server composition already passes the
application service through `SetAgentRuntimeService`.

## Validation boundary

Tests use a fresh synthetic PostgreSQL 18 database, actual runtime admission,
fenced history/decision transactions and a controlled HTTP model. HTTP gate
tests separately use signed synthetic account/chat-route JWTs. These are not
real-provider or specialist-delegation acceptance.

The directed tests cover concurrent/replayed recovery, exact answer/history
preservation, prior completed tool facts, account/ownership rejection, state
and CAS changes, lost fencing, foreign history, later runs, idempotency-key
collision, existing effects, commit uncertainty and private-error redaction.
The current directed race selections passed 34 cases plus the historical-input
window regression and two existing continuation-config regressions. Swagger
generation, SDK generation/type checking and server composition compilation
passed. Raw command output and failed fixture/tool-setup attempts are retained
in the private task workspace; no private logs or production payloads are
published here. The ordinary aggregate's initial 180-second Go lint budget
expired. A retry uses the documented per-command 600-second cold-cache budget;
no check rule or repository timeout default was changed.

Application UI/screenshots, original production-request recovery and paid
provider calls have not been performed. Deployment and reconnect do not invoke
this action. Existing native shutdown recovery still has its own unknown-effect
boundaries; this patch does not claim global exactly-once model/tool execution
across process crashes. It is not a Ready declaration.
