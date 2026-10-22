# Conversation tasks and persisted Pi results

Original base: main `e6f4999dd294a09b4171fdaae0176714405971d5`, schema 170.
Integration base: main `eeb1fdabf50ec94d48f08302c9da3f5b3811293e`
(merged Pi JSON #7 and resumable upload #8).
The additive result projection depends on compact-result #12 at
`f38b59c556da0d774d6f5adc089d2fbbfd2dcc4b`. It reads persisted facts only; it
does not invoke completion collection, attachment or resumable-upload paths.
This API is an HTTP read model, not a scheduler or a new control plane.

## Frozen HTTP contract

Use the normal authenticated server API and existing team context:

```text
GET /bots/{bot_id}/sessions/{session_id}/tasks?limit=20&cursor={opaque}
GET /bots/{bot_id}/sessions/{session_id}/tasks/{task_type}/{task_id}
```

- `session_id` in the path is the aggregation root, owned by the caller.
- `limit`: default 20, range 1–100; invalid input is 400.
- `task_type`: `subagent` or `computer_operation`. An operation is never a
  native session, and a blocked submission does not create another task row.
- List: `{ "items": Task[], "next_cursor"?: string }`. Details return `Task`.
- Every response is `Cache-Control: no-store`. Do not reuse an old detail or
  attachment after authorization fails. The client must clear the old view.
- Error bodies use the existing `server.Problem` shape. Missing authentication:
  401; revoked Bot chat permission: 403; inaccessible/mismatched session or
  task, fixed PC restriction, revoked selected PC: 404 `session.not_found`.
  Revoked individual PC operations disappear from a NAS-root list and return
  404 on detail. Invalid cursor: 400 `request.field_invalid` with `cursor`.
- There is no task attachment endpoint. Appending `/attachments` returns 404.
  Do not construct a media URL or fall back to raw Bot messages/logs to fill
  this view when content is unavailable.

`Task` has the following fields (timestamps are RFC 3339):

| Field | Type / values | Presence and meaning |
| --- | --- | --- |
| `task_type` | `subagent`, `computer_operation` | Always; discriminator |
| `task_id` | UUID string | Always; session ID or operation ID |
| `session_id` | UUID string | Always; actual owning session, including child/grandchild |
| `parent_session_id` | UUID string | Always; requested aggregation root, not a rewritten stored parent |
| `created_at` | timestamp | Always; immutable ordering key |
| `updated_at` | timestamp | Always; persisted session/receipt update time, not a liveness signal |
| `status` | `unknown`, `submitted`, `process_exited`, `not_started`, `rejected` | Always |
| `summary` | string | Always; fixed server-authored safe explanation, not task output |
| `content_available` | boolean | True only when `result_summary.text` is nonempty |
| `attachments_available` | boolean | Always `false` in v1 |
| `target_id` | UUID string | Computer only; Memoh Bot mount ID, never physical device ID |
| `operation_kind` | `foreground_bash`, `herdr_pi_task`, `unknown` | Computer only |
| `revision` | decimal string | Computer only; preserves integers beyond JS precision |
| `cancel_requested` | boolean | Computer only; included even when false |
| `process_state` | `unknown`, `exited`, `not_started` | Computer only |
| `pi_result` | `unknown`, `not_applicable`, `succeeded`, `failed`, `aborted`, `incomplete`, `not_started`, `invalid_protocol`, `output_limit` | Computer only; Pi invocation outcome, not business completion |
| `business_state` | `unknown` | Computer only |
| `result_summary` | `{text: string, truncated: boolean, source: "pi_final_assistant"}` | Optional on both list and detail, only with available final assistant text; at most 2048 UTF-8 bytes |

Example (all identifiers synthetic):

```json
{
  "task_type": "computer_operation",
  "task_id": "10000000-0000-4000-8000-000000000005",
  "session_id": "10000000-0000-4000-8000-000000000004",
  "parent_session_id": "10000000-0000-4000-8000-000000000003",
  "created_at": "2026-10-21T16:00:00Z",
  "updated_at": "2026-10-21T16:01:00Z",
  "status": "unknown",
  "summary": "Cancellation requested; this does not confirm that the process stopped.",
  "content_available": false,
  "attachments_available": false,
  "target_id": "10000000-0000-4000-8000-000000000006",
  "operation_kind": "herdr_pi_task",
  "revision": "3",
  "cancel_requested": true,
  "process_state": "unknown",
  "pi_result": "unknown",
  "business_state": "unknown"
}
```

## Authorization and pagination

The Bot chat grant is rechecked on every HTTP request. The root, every
intermediate ancestor and each descendant must belong to the same Bot and
caller; manage/admin does not bypass conversation ownership. Only persisted
subagent edges are walked, to a maximum depth of 64. Deleted/mismatched/cyclic
paths fail closed. Operation Bot/user/session identity remains unchanged.

Workspace scope is reconstructed from the whole ancestor chain, including
ancestors above a requested child root. A fixed PC cannot broaden to NAS or a
different PC. A workdir binding takes priority. For unbound sessions the
existing persisted picker selection applies; without one the top ancestor
uses the Bot's current primary, and children inherit. Selections are never
grants and cannot broaden a PC ancestor. The existing unbound picker can be
changed by its owner; only Folder/workdir bindings are immutable. A selection
changed locally in the phone but not yet persisted is not visible to this API.
Malformed or contradictory selection metadata fails closed. Every computer
read checks the current Bot mount, runtime owner,
revocation, `workspace_exec` permission, owned MCP connection and registration
fingerprint using the existing Agenvo resolver. Resolution does not contact
the device. Grants are never created by reads.

Order is `(created_at DESC, task_type DESC, task_id DESC)`. The page chain has
a creation-time upper bound; status/revision changes remain live and do not
reorder records. Start without a cursor to see newly created tasks. This is
keyset pagination, not a database snapshot: deletion/revocation removes rows;
a transaction committing late with an older creation time may appear on a
later page. A maximum of 200 candidates is authorized per request. An empty
page can still have `next_cursor`; continue until it is absent. The cursor is
authenticated and encrypted using the existing server secret with domain
separation. It is bound to user/Bot/root and does not confer authorization;
secret rotation invalidates it. Never parse it in the client.

## Honest state and safe content limits

Native subagent sessions are `unknown`: persisted metadata is not proof that
a runtime is currently executing. A computer `dispatching` receipt is also
`unknown`; `submitted` means only submission was recorded. Only a persisted
completed foreground command with an exit receipt projects `process_exited`.
For `herdr_pi_task`, a completed operation with `completion_version=1` and a
valid matching receipt projects `process_exited` / `exited` when started, or
`not_started` for both status/process and Pi outcome when invocation never began.
A valid exit receipt without a validated result keeps `pi_result=unknown`.
A validated persisted `batch.result` supplies the Pi outcome enum above. The
same bounded completion validator is shared with #12 ingestion; GET never
reads files, parses JSONL, contacts a PC or repairs missing facts. Version 0,
missing/mismatched receipt, unsupported version and uncompleted rows retain
unknown Pi results and unavailable content. Receipt/result validation failure
is not an HTTP error: any independently valid process exit fact remains visible.

`summary` remains a fixed server explanation, including cancellation intent.
`business_state` remains `unknown` even for `pi_result=succeeded`. Cancellation
intent never implies `aborted`; only a persisted validated Pi result does.
`invalid_protocol` and `output_limit` have no display content. A valid result
may also have empty final text: outcome is known but `content_available=false`
and `result_summary` is omitted. `truncated=true` means the bounded summary is
partial; it never authorizes fetching the rest through a media URL.

The only newly displayed text is `batch.result.output`, produced by #12's
final assistant **text** projection, bounded to 2048 UTF-8 bytes. List and
detail return identical result facts; there is no separate content endpoint.
Render the summary as plain untrusted text (no HTML, embedded media or auto
fetching links). This is a structural field whitelist, **not general-purpose
secret redaction of arbitrary assistant prose**. No thinking blocks, prompts,
tool bodies, raw JSONL/stdout/stderr, observation, arbitrary metadata, private
runtime paths, credentials from internal fields, hashes or download URLs are
projected. Top-level operation `output` is never a fallback. GET cannot mutate
receipts, import logs, create queue rows, submit, cancel or retry tasks.

Example of the additive fields after a valid persisted completion:

```json
{
  "status": "process_exited",
  "process_state": "exited",
  "pi_result": "succeeded",
  "business_state": "unknown",
  "content_available": true,
  "attachments_available": false,
  "result_summary": {
    "text": "Synthetic final answer.",
    "truncated": false,
    "source": "pi_final_assistant"
  }
}
```

All existing route, pagination, identity, grant, fingerprint, error and
`no-store` rules remain unchanged. Clients must treat unfamiliar outcome
values as unknown and absent `result_summary` as unavailable. Older servers
omit the object and retain the old enum. Clear old content on authorization
failure, scope changes, close/unmount and failed refresh; do not use cached
success as a fallback. Native subagent content remains unavailable here.

Existing Bot+hash media endpoints are broader than operation scope. This API
does not expose hashes or create attachments, so it provides no new route to
internal logs. Full execution content/attachment support still requires a separately
reviewed projection and operation-scoped download design, including protection
against direct access through existing broad media routes. Existing media
permissions and legacy endpoints are not changed here.

The generated OpenAPI in `spec/swagger.json` and TypeScript SDK are the machine
contract. The mobile client is integrated separately. Agent runtime/contextview are unchanged.
