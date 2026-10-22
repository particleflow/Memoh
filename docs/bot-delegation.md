# Bot delegation implementation checkpoint

This document describes an implementation in progress. There is no registered
delegation HTTP endpoint or execution tool yet. Do not bind clients to these
proposed routes or interpret the package tests as an end-to-end acceptance.

## Existing integration points

- `application.Service.Chat` runs a native model turn with the destination Bot's
  configuration and persists its conversation. A successful REST channel submit
  alone does not provide a correlated completed result.
- `thread.Service.Create` creates a destination-owned session. Cross-Bot work
  must not be attached to `parent_session_id`, whose task-reader contract assumes
  a complete same-Bot, same-user ancestry.
- `application.Service.EnqueueFollowUp` provides asynchronous wakeup with an
  invocation ID, user and team. Its public input is currently text-only and its
  queue is memory/Redis state. It is not a durable delegation ledger or an
  attachment transfer API.
- `media.Service.Stat/Open` resolve stable originals. A handoff can read the
  source original through an authorized reference without creating a permanent
  destination Bot/hash alias or copying bytes.
- Existing Bot grants, computer mounts and Agenvo registration fingerprints
  supply authorization. No new organizational role system is needed.

## First increment

`internal/botdelegation` currently contains read-only authority, attachment and
final-text projection components, without production wiring:

1. Check the initiating user's live account, both Bot chat grants, same team,
   complete source-session ancestry, and exact destination session ownership.
2. Compare mounts through their shared runtime identity. A source PC-A selection
   constrains the source mount; the specialist may hold a different mount ID for
   that same PC. Recheck both mounts, owners, grants and registration fingerprints.
3. Limit shared references to the recorded inputs/results and their actual
   conversation asset links. Recheck authorization for metadata/open/read; expose
   no storage key, private path, raw URL or model-inline bytes in the descriptor.
4. Project only text parts of the actual final assistant message after confirmed
   terminal completion. Earlier narration, reasoning, tool output and unknown
   part types do not become a final summary. This is not general secret redaction.

Tests use synthetic in-memory collaborators. Real JWT, database, native Agent,
browser, queue wakeup, and device integration remain untested for delegation.

## Proposed transport (not frozen)

The intended namespace is
`/bots/{bot_id}/sessions/{session_id}/delegations`.

- `POST`: `request_id` (UUID), `target_bot_id` (UUID), `message` (nonempty text),
  `attachments` (initially zero or one `{content_hash}`). Return a durable
  delegation ID and accepted state with HTTP 202, never an execution-success
  claim. Reusing the same request ID and payload returns the existing record;
  a changed payload conflicts and does not dispatch.
- `GET`: list by immutable creation order with bounded `limit` and opaque cursor.
- `GET /{delegation_id}`: current authorized facts, source/destination session
  identifiers, execution and result-delivery states, optional final summary and
  authorized attachment descriptors. No side effects.
- `GET /{delegation_id}/attachments/{content_hash}`: resolve a recorded shared
  reference under fresh authorization. Do not return a broad original-media URL.

All responses, including JWT failures and namespace 404/405 responses, must use
`Cache-Control: no-store`. Unknown execution is separate from accepted, running,
completed and failed; result delivery is separate from business completion.
Cancellation, retry, arbitrary workspace management and new file transfer are
outside this increment. Read endpoints cannot repair, resubmit or poll devices.

## Remaining dependencies before wiring

- A durable, server-owned delegation table is required for request deduplication,
  immutable source/destination links, device constraints, result references and
  delivery receipts. Ordinary session metadata is client-editable and cannot
  supply authority. Migration numbering is being coordinated with the parent;
  this branch has not allocated or edited any migration, including 171/172.
- The trusted execution adapter must map a source PC mount to the destination
  mount without reusing or weakening the source's host computer binding. Existing
  `WithComputerScope`/`WithComputerBinding` deliberately cannot be replaced by
  child contexts. The fixture-level authority tests do not prove this adapter.
- Native/NAS delegation must retain the current intersection for every device
  operation and prevent raw aggregate MCP/connector paths from bypassing it.
  Persisted delegated sessions also need the constraint on later turns and
  queue continuations, not only on the original in-process context.
- The result notification must carry actual attachment references through the
  existing turn/queue port and survive uncertain delivery without rerunning the
  specialist. A text mentioning a hash is not attachment delivery.
- Production adapters, HTTP handlers, tool registration, persistence and the
  end-to-end text/single-attachment fixtures are still to be implemented.

The anticipated shared files with cleanup PR #43 are
`cmd/internal/core/providers.go` (tool and asset adapter registration) and
`internal/agent/application/service_workspace_target.go` (trusted turn scope).
Neither file has been edited by this branch. No image-job/discovery code is
copied into delegation. Agent runtime/contextview remain unchanged.

## Product references

The product comparison is limited to the official
[Bot roles](https://docs.x.ai/grok-bot/bots),
[asynchronous collaboration](https://docs.x.ai/grok-bot/chat-and-collaboration),
and [files/results](https://docs.x.ai/grok-bot/files-and-results) documentation.
These sources do not establish a NAS ACL implementation or a general DAG API.
Bot-to-group handoff is documented as text-only; actual shared attachments must
be implemented and verified independently here.
