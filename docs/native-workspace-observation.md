# Native workspace observation and operation recovery

> Current routing: legacy Agenvo enrollment and execution are retired. Use an owned native Memoh Runtime with `/users/me/runtimes/{id}/native-herdr`. Agenvo wire examples below are historical protocol references, not installation/OAuth instructions. The shared `internal/agenvo` ledger remains in use by native Herdr/Pi. See [retirement audit](agenvo-retirement-audit.md).

This increment starts from `4521d323050e27d12a0e496ff51f0bd9bea938f6`.
It adds two tools to the existing authenticated computer-task MCP source.
It does not add a control plane, HTTP endpoint, grant, credential, migration,
connector extension, Agent runtime change, or automatic business retry.
The existing mobile 1.0.7 parent-task list/detail HTTP contract is unchanged.

## Native API sources

The adapter uses the published Agenvo v0.2.0 methods in
[`apps/herdr/src/herdr.ts`](https://github.com/Xuanwo/agenvo/blob/v0.2.0/apps/herdr/src/herdr.ts):

| Method | Arguments | Use |
| --- | --- | --- |
| `workspace.list` | `{session}` | Native workspace metadata, marked `readOnly` upstream. |
| `pane.get` | `{session, paneId}` | Check the saved pane, workspace and terminal IDs. |
| `agent.get` | `{session, name: savedPaneID}` | Read agent state by pane ID after reconnect. The upstream method accepts a name **or pane ID**. |

`workspace.get` and `agent.list` also exist upstream; they are not required by
this minimal flow. `session` here is the configured **Herdr service session**,
not a Memoh conversation or a Pi session ID. The adapter does not enumerate
other service sessions. Herdr v0.9.5 response fields are defined in
[`workspaces.rs`](https://github.com/herdrdev/herdr/blob/v0.9.5/src/api/schema/workspaces.rs),
[`panes.rs`](https://github.com/herdrdev/herdr/blob/v0.9.5/src/api/schema/panes.rs), and
[`agents.rs`](https://github.com/herdrdev/herdr/blob/v0.9.5/src/api/schema/agents.rs).
The native status enum is `idle | working | blocked | done | unknown`.

The relay's explicit `device_offline` and `runtime_unavailable` failures come
from [`packages/relay/src/core.ts`](https://github.com/Xuanwo/agenvo/blob/v0.2.0/packages/relay/src/core.ts).
Transport error text is never used to classify these states.

## Tool contract

Both tools return the existing MCP success envelope `{ "computer": <value> }`.
Extra input properties are rejected. The caller cannot supply native refs,
connector/device IDs, commands, credentials, or a replacement operation owner.
General public MCP requests cannot forge a trusted active conversation binding.

### `computer_workspaces`

Input: `{ "target_id"?: string }`. Omission uses the selected PC, or requires an
unambiguous authorized target in NAS scope. A selected-PC conversation cannot
list another PC, even if the Bot has a grant to that other PC.

Output:

```json
{
  "target_id": "authorized-mount-id",
  "availability": "online",
  "workspaces": [
    {"workspace_id":"workspace:1","label":"Synthetic workspace","pane_count":2,"agent_status":"idle"}
  ],
  "truncated": false
}
```

At most 100 workspaces are projected; `truncated: true` means more were
reported, not that a continuation cursor exists. Native labels are bounded
user-visible metadata, not trusted instructions. IDs are limited to 256 UTF-8
bytes and labels to 512, with no NUL/CR/LF. Invalid/duplicate IDs or malformed
responses return an unavailable observation with an empty list, not a partial
successful inventory. Paths, titles, tokens and worktree details are omitted.

### `computer_task_recover`

Input: `{ "operation_id": string }`. Ownership remains the existing exact
Bot/user/conversation boundary for computer-task tools. An operation in a child
conversation is not adopted by its parent; the existing authorized parent-task
HTTP overview continues to provide aggregate observation separately.

Output fields:

| Field | Meaning |
| --- | --- |
| `operation_id`, `target_id` | Original operation and authorized mount. |
| `state` | Stored operation state; a stale `dispatching` reservation is shown as `unknown`. |
| `process_state` | `unknown`, `exited`, or `not_started`, based only on a valid saved receipt. |
| `pi_result` | `unknown`, `not_started`, or the existing validated compact-result outcome. |
| `business_outcome` | Always `unknown`; neither process exit nor Pi success proves business completion. |
| `result_reason` | `receipt_missing`, `legacy_result_protocol`, `receipt_mismatch`, `verified_receipt`, `verified_persisted_result`, `completion_result_unavailable`, or `legacy_observation_only`. |
| `cancel_requested` | Stored intent; it is not evidence of termination. |
| `attempt`, `pi_session_id`, `workspace_id`, `pane_id` | Optional bounded original references; no substituted workspace or new attempt. |
| `result_summary` | Optional `{text,truncated,source:"pi_final_assistant"}` from the existing validated result, bounded by its 2048-byte contract. |
| `native` | Current metadata observation, separate from all durable result facts. |

Attempt and Pi session IDs must retain their original 32/64 lowercase hex
formats; malformed identity text is omitted, never echoed as a path or cause.

`native` contains `availability`, `workspace`, `pane`, `control_pane`,
`agent_status`, `reference_match`, and `reason`.
Presence fields are `unknown | present | missing`; pane fields can additionally
be `mismatch`. `reference_match` is `unknown | observed | mismatch`.
An observed match checks saved workspace/pane/terminal IDs, **not** OS process
lifetime or a Herdr server generation. Native IDs can be reused after restart.
No native status, including `done`, creates a new result receipt.

Reasons include `native_references_missing`, `native_references_invalid`,
`native_query_unavailable`, `native_response_unverified`, `workspace_missing`,
`execution_pane_missing`, `execution_pane_unverified`, `execution_pane_mismatch`,
the corresponding `control_pane_*` values, `control_reference_missing`,
`agent_missing`, `agent_metadata_unavailable`, `agent_reference_mismatch`, and
`receipt_not_inferred_from_native_state`. A missing receipt stays unknown with
`result_reason`, even if every native reference can currently be observed.

The existing receipt validator matches attempt, generation, request hash,
Pi session, workspace, pane and terminal. The existing compact-result validator
provides Pi facts; this increment does not parse JSONL again. A previously
verified result survives native disconnection or pane disappearance. Legacy
records are neither deleted nor rewritten into new-protocol receipts.

## Availability and authorization

Availability is `online | authentication_unavailable | offline |
runtime_unavailable | connection_unavailable`. Only an explicit relay
`device_offline` in a rejected/not-started response means `offline`.
Observed HTTP 401 means authentication unavailable, including 401 during
`tools/call` after successful MCP initialization. Generic timeouts, malformed
responses, or error text containing “401” are connection-unverified states.

Account runtime responses add optional `availability`; existing `online`
remains true only for a successful probe. Workspace targets reuse `status`.
Web account, access-list, Bot and chat target surfaces distinguish these
states, and configured Pi capability is labelled separately from commands.

Before and after native reads, the adapter rechecks current Bot permission,
mount ownership, target selection and registration fingerprint. Revocation
or replacement rejects the entire tool response, including previously loaded
summary text. Reads have a ten-second overall budget, no automatic retry and
an explicit native-method allowlist. No operation claim/save, pane creation,
terminal input, submit, close, delete, log ingestion or file transfer occurs.

This is field projection, not general-purpose secret redaction. Final assistant
text and workspace labels are user-visible content; unselected reasoning,
stdout/stderr, raw JSONL, private paths, tokens and diagnostic causes are never
read/projected by these tools. Explicit workspace selection/reuse for new work,
control-pane restoration, attachments, cancellation and retry are later work.
No production pane restoration or real Pi execution was performed here.
