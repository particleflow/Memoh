# Pi JSON invocation receipts (candidate, not deployed)

> Current routing: legacy Agenvo enrollment and execution are retired. Use an owned native Memoh Runtime with `/users/me/runtimes/{id}/native-herdr`. Agenvo wire examples below are historical protocol references, not installation/OAuth instructions. The shared `internal/agenvo` ledger remains in use by native Herdr/Pi. See [retirement audit](agenvo-retirement-audit.md).

This independent change builds on the computer task MCP service. It does not
modify Pi, Memoh's Agent runtime or context composition, install a hook, or add
a persistent RPC process. The production PR #6 TUI observation contract remains
historical evidence; its terminal snapshots cannot prove a task completed.

## Configuration and transport

An owner can opt an existing registered computer into `pi-json-v1` through the
`PUT /users/me/runtimes/{id}/native-herdr` endpoint. The following is the `agent` subobject within the existing complete native Herdr profile:

```json
{
  "execution_mode": "pi-json-v1",
  "provider": "<configured-provider>",
  "model": "<exact-model-id>",
  "config_dir": "<private-Windows-Pi-config-directory>",
  "path": "<private-PATH-if-required>"
}
```

These are deployment references, not credentials. The existing Pi configuration
owns the Magpie endpoint and credential command. The registered Git Bash path
remains private deployment configuration. This change creates no credential,
grant or default provider. Native registrations require `execution_mode: "pi-json-v1"`; unsupported modes are rejected, never retried through another protocol. `computer_targets.agent_protocol` reports the distinction.

The unchanged standard MCP tools remain the public boundary. A trusted host
binds the authenticated turn to its conversation scope. The model supplies only
target selection, `herdr_pi_task`, task text and a root-relative working
directory. It cannot submit a native method, device ID, provider override or
receipt path. PC conversations remain fixed to that PC; NAS conversations remain
limited to Bot grants. The same checks apply to result, cancellation and refs.

## Invocation and evidence

1. Validate the connector command-size limit before reservation or remote writes.
2. Persist the operation, fresh attempt ID, invocation nonce, request hash and
   exact Pi session key before creating remote resources. Persist each step
   before its mutation. An uncertain dispatch is never repeated.
3. Create a dedicated Herdr workspace and job tab. The initial shell pane is a
   separate control channel and never hosts Pi. The tab receives only the
   registered non-secret Pi configuration directory and optional PATH.
4. The existing PowerShell terminal launches the explicitly configured Git Bash.
   Business execution is Bash. Task text is base64-decoded to a file and supplied
   to official `pi --mode json` on stdin; it is neither shell source nor CLI flags.
   Provider and model are explicit. Pi's normal configured tools are retained.
5. Use an owned root-relative session directory and exact `--session-id`, never
   `--continue` or a latest-session search. Exclusive attempt creation prevents
   duplicate invocation; an exclusive session lock prevents two wrappers from
   writing that session. Stale locks are not stolen. TUI must not share these
   owned batch session directories.
6. stdout and stderr go to separate attempt files. After the invocation exits,
   the envelope hashes both files and atomically renames `receipt.tmp` to
   `receipt.json`. It emits a complete nonce-bound frame with attempt, request,
   session, workspace/pane/terminal identity, real exit code, sizes and hashes.
7. Observation first reads the job terminal without typing. If the frame has
   scrolled away, a fixed read on the separate control shell retrieves the
   receipt. No receipt means unknown. No kill, close, focus or interrupt is sent.
8. Import files in at most 2 KiB chunks through that control shell, with unique
   response frames. Verify chunk readiness and the complete file hash before
   exposing content refs. Persist only refs, offsets and a bounded text preview,
   never the complete JSONL in conversation/operation context. One observation
   imports at most 16 pages within its time budget; another observation resumes.
   Agenvo 0.2.0 [pane.read schema](https://github.com/Xuanwo/agenvo/blob/v0.2.0/apps/herdr/src/herdr.ts#L67-L73)
   permits only 1–500 rendered rows; `recent-unwrapped` does not change that
   unit. Start with eight rows and wait for this response's exact nonce end
   marker before expanding, so delayed responses do not expand into unrelated
   historical frames. A connector `result_too_large` narrows only the read.
   The snapshot never exceeds 500 rows and no input is repeated in that poll.
   Data pages are 2 KiB; complete receipt recovery has an independent 4 KiB
   budget, including long native IDs. Existing 16 KiB chunks remain valid:
   continuation uses their saved byte offsets without resetting the attempt.
   Synthetic coverage includes 16/43/44/80/240-column panes and very wide
   oversized history. Arbitrarily narrow terminals or long prompt decoration
   can still leave observation unknown/pending; there is no guarantee that
   every terminal geometry can carry a complete frame. Read rejection such as
   `invalid_params` is not evidence that the Pi invocation did not start.

`generation` is a wrapper invocation nonce, **not** Herdr's internal generation.
Control pane metadata is checked before use, but Herdr does not expose an atomic
expected-generation input operation. Check-then-use is not a guarantee across
native restart/ID reuse. Changed identities fail closed; uncertain recovery does
not start another Pi or close any workspace. Concurrent read observations can
fail their frame/CAS checks and remain pending; they never repeat the business
task. High-privilege local programs can access the running account's files: this
protocol is not an OS sandbox or protection from a malicious local process.

A future user task panel must consume a separate allowlisted projection: raw Pi
JSONL may contain internal reasoning and is not a user-facing activity log.
Current task tools enforce operation/session/target scope, but the existing media
GET authorizes Bot plus content hash, not that narrower operation scope. Raw hash
refs therefore must not be presented as task-scoped download authorization.
Session-tree listing, safe projections and operation-authorized attachments are
separate follow-up work, not part of this reader correction.

## Result semantics

- `state=completed` and `process_stopped=true` mean the correlated Pi invocation
  exited. They do not prove AI success, cancellation, or termination of arbitrary
  detached descendants. This evidence may release only this operation's slot.
- `pi_exited_output_pending` means exit is known but complete output is not ready.
- `pi_result.outcome` is `succeeded`, `failed`, `aborted`, `incomplete`,
  `invalid_protocol`, `not_started`, or `output_limit`. It describes the official
  invocation's protocol, not independently verified completion of the user's
  real-world objective. The caller must still inspect task-specific evidence.
- Exit 0 is insufficient. Complete UTF-8 LF-framed JSONL must have the exact
  session header, matching provider/model, closed message/turn/tool/run events,
  a final assistant `message_end` and `agent_settled`. Early retry failures can
  recover; an isolated `agent_end` is not final. Partial/unrecognized protocol
  cannot become success. The preview is at most 16 KiB.
- Full stdout/stderr import is limited to 64 MiB per file. Larger output yields
  `output_limit`, with known exit but no imported artifact ref or success claim.
- Empty stdout/stderr uses an explicit `stdout_empty`/`stderr_empty` flag after
  verifying the empty SHA-256 and zero size in the receipt. Media rejects empty
  assets, so no unopenable hash ref is invented. Empty stdout cannot establish
  Pi success; a never-started invocation remains `not_started`.
- A cancellation request preserves intent; without an atomic safe stop contract
  the process is not killed. A later real receipt can establish exit without
  rewriting the earlier cancellation as successful cancellation.
- Storage/transport failure never fabricates ready refs or triggers another
  business submission. Public errors omit private storage diagnostics.

There is no schema change after 170. Existing operation JSON stores the batch
details; migration 170 and unresolved records must be preserved. Do not downgrade
and resubmit unresolved work through an older execution path. Configuration
changes invalidate pinned operations; finish/reconcile work before changing the
registered target/profile. No automatic workspace deletion or session-lock
recovery is included.

## Fixed vendor basis and validation limits

The contract is based on official Pi **1.2.0**
[JSON events](https://github.com/earendil-works/pi/blob/v1.2.0/packages/coding-agent/docs/json.md),
[CLI integration](https://github.com/earendil-works/pi/blob/v1.2.0/packages/coding-agent/docs/cli-integration.md),
[print-mode lifecycle](https://github.com/earendil-works/pi/blob/v1.2.0/packages/coding-agent/src/modes/print-mode.ts),
and [exact session selection](https://github.com/earendil-works/pi/blob/v1.2.0/packages/coding-agent/src/main.ts#L295-L418).
Herdr operations retain the existing Agenvo 0.2.0 / Herdr 0.9.5 mapping.

Cloud tests use synthetic standard MCP clients, provider events and relay
responses; the Bash envelope executes against synthetic local utilities.
Windows Git Bash, real Pi/Magpie compatibility and task-specific output evidence
still require a controlled deployment acceptance. A green fixture does not claim
those real programs ran. This change adds no desktop GUI or arbitrary PC file
transfer capability.
