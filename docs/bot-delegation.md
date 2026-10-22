# Authorized Bot delegation

This draft integrates a supervisor Bot with an existing specialist Bot through
ordinary sessions and native turns. It is not a separate execution platform.
The implementation remains under validation; fixture and HTTP tests are not
production, live-model, PC, or mobile acceptance.

## HTTP contract

Base: `/bots/{bot_id}/sessions/{session_id}/delegations`, where the Bot and session
identify the initiating user's supervisor conversation. All responses in this
namespace, including JWT failures and route 404/405 responses, use
`Cache-Control: no-store`.

| Method and suffix | Request | Response |
| --- | --- | --- |
| `POST` | `request_id`: UUID, `target_bot_id`: UUID, `message`: nonempty text up to 64 KiB, optional `attachments`: array of `{content_hash}` | `202`, one View. Acceptance is not completion. |
| `GET` | `limit`: 1–100 (default 20), optional opaque `cursor` | `200`, `{items: View[], next_cursor?: string}` |
| `GET /{delegation_id}` | none | `200`, one View |
| `POST /{delegation_id}/delivery` | none | `200`, current View after delivery-only retry. Unknown execution returns `409`. |
| `GET /{delegation_id}/attachments/{content_hash}` | authenticated request | `200`, original bytes with download disposition and `nosniff`; no public link or copied original |

View fields:

- `delegation_id`, `request_id`, `source_session_id`, `target_bot_id`: UUID strings.
- `target_session_id`: UUID string, omitted before a target session is persisted.
- `state`: `accepted | running | unknown | completed | failed`.
- `delivery_state`: `pending | accepted | unknown | unavailable`. Accepted means
  the supervisor follow-up queue accepted the result, not that the supervisor
  finished processing it.
- Optional `summary`: `{text, truncated, source: "assistant_final"}`.
- `attachments`: array of `{content_hash, mime?, size_bytes?, url?}`. `url` is the
  authenticated delegation-scoped route above, never a storage path or raw URL.
- `created_at`, `updated_at`: RFC3339 timestamps.

Errors use existing Problem Details: malformed input/cursor `400`, missing or
invalid JWT `401`, source Bot access denial `403`, unavailable/foreign/revoked
handoff `404`, request conflict or nonterminal delivery retry `409`, infrastructure
failure `500`. Causes and provider diagnostics are not public response fields.

Pagination orders immutable `(created_at, id)` descending and freezes a creation
snapshot. Status remains live. Each candidate is authorized against current
permissions. A scan stops after 200 candidates; follow a nonempty `next_cursor`
even when `items` is empty. Cursors are bound to the caller, Bot, conversation and
current source selection. GET never starts work, polls a PC, imports logs, retries
execution, or retries delivery.

The first acceptance covers text and one attachment; the current input bound is
16 distinct references, not a permanent one-attachment product limit.

## Tools and native execution

`list_delegate_bots`, `delegate_to_bot`, `get_bot_delegation`, and
`retry_bot_delivery` use the actual authenticated tool session. Bot names are not
hardcoded. A tool call derives its request ID from the trusted call identity;
HTTP clients supply their own stable request ID. An identical unresolved task
also deduplicates across new request IDs. A changed replay conflicts.

The sole successful durable claim creates a target-owned chat session and calls
`application.Service.StartTurn` with the target Bot's normal configuration. It
stores the actual run ID and observes its persisted terminal fact. No API submit
acknowledgement is treated as completion. Restarts and uncertain runs remain
unknown and are never automatically executed again.

A result uses only the last final assistant text parts, bounded to 16 KiB. It does
not expose reasoning, raw tool output, provider JSONL or private paths. This is
field projection, not universal secret redaction of user-visible assistant text.

Completed result references include properly owned same-Bot subagent descendants,
excluding foreign-user branches and hidden/non-assistant messages. Inputs retain
the source original's owner even when returned unchanged. New outputs retain the
target original's owner. Both are resolved through `media.Service`; delegation
creates no per-file ticket, broad media alias, or second copy of NAS bytes.

The supervisor receives a complete follow-up command with actual attachment
references. The queue invocation and item identity are stable on delivery retry;
retry never calls the specialist. The queued result carries the delegation ID
for fresh authorization before consumption. Existing queue durability still
applies: accepted delivery is not proof of receipt after server/Redis loss.
Attachment metadata preserves `delegation_url` for the existing Web attachment
renderer after history reload. Invalid scoped metadata never falls back to the
broader Bot/hash route.

## Authority and device boundary

A handoff is not a grant. Each access checks the live user, both Bot chat grants,
same team, complete owned source ancestry, target ownership, and unchanged source
selection. Source PC-A can use only the specialist's mount of that same RuntimeID
and registration fingerprint. Source NAS uses the existing resource intersection.
Selection metadata is read without requiring execution permission; actual device
reads, writes and execution retain their separate permission checks.

The target's fresh turn reconstructs its scope from the durable ledger; it does
not clear or replace a source ComputerScope. Later target turns, same-Bot children,
queue continuations and tool effects reconstruct or recheck that scope. Ordinary
nondelegated conversations retain their existing ownership policy.

Authorized NAS cloud connectors/MCP remain available. Existing PC restrictions
and managed computer endpoint guards remain in effect; computer operations use
the constrained service and the current Bot/device intersection.

## Persistence, integration and limits

Migration `0173_bot_delegations` is reserved for this feature. It records immutable
source/destination associations, deduplication keys, device constraints, result
references and delivery state under forced team RLS and composite foreign keys.
Deployment must coordinate the complete 171/172/173 sequence with the PC transfer
line. This branch does not supply or deploy 171/172. No production migration or
candidate publication is authorized by a local database test.

The first implementation handles one supervisor → specialist → supervisor hop;
further cross-Bot relays require a durable authority chain. Same-Bot child agents
retain native behavior. Cancellation, specialist retry, arbitrary workspace
management and new transfer protocols are not exposed.

The protected official `internal/agent/runtime` and `internal/contextview` trees
must remain unchanged. Host orchestration and turn ports supply the adapter.
Integration with cleanup main must preserve removal of image-job/discovery and
withdrawn image edit/result modes.

## Validation status

Implemented synthetic checks cover authority/grant revocation, shared runtime
mapping across different Bot mounts, source original preservation, descendant
asset queries, deduplication, delivery-only retry, bounded cursors and live status,
GET side-effect absence, real JWT/handler response policy, and Web scoped URLs.
PostgreSQL uses a newly created isolated test database. Native model/queue end-to-
end execution and real browser attachment opening remain required before this
draft can be considered a complete handoff. Production PC, OAuth and mobile
acceptance are separate and are not claimed here.

## Product references

The comparison uses official [Bot roles](https://docs.x.ai/grok-bot/bots),
[asynchronous collaboration](https://docs.x.ai/grok-bot/chat-and-collaboration), and
[files/results](https://docs.x.ai/grok-bot/files-and-results). These references do
not establish a NAS ACL implementation or general DAG API. Actual shared-file
transport and authorization are verified independently in this project.
