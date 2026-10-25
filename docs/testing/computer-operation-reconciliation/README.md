# Owner reconciliation before Windows upgrade

Base: `68be06092a1ea85d961115aac404082982bdecf4`. This independent local branch does not move the candidate publishing branch. No production, Windows endpoint, Herdr/Pi binary, credential, schema or admission policy is changed.

## Implemented boundary

The existing `computer_task_recover` tool and authenticated
`GET /bots/{bot_id}/sessions/{session_id}/tasks/computer_operation/{operation_id}/observation`
now include `reconciliation`. Both use the existing host/session binding and current grants. No new endpoint or authentication mechanism is introduced.

| Field | Meaning |
| --- | --- |
| Existing `operation_id`, `target_id`, `attempt`, `pi_session_id`, `workspace_id`, `pane_id` | Original operation and execution identity; never replaced by recovery-reader IDs |
| `reconciliation.revision` | Exact decimal string of the operation CAS revision, safe above JavaScript's integer range |
| `target_fingerprint`, `generation`, `request_hash` | Stored registration and original invocation identity; generation remains an invocation nonce, not a native Herdr generation |
| `terminal_id`, `control_pane_id`, `control_terminal_id` | Original references, never the replacement reader's identity |
| `receipt_state`, optional `receipt` | `missing`, `invalid`, `validated`, or `not_applicable`. Only a receipt validated against the original progress is returned. It includes stdout/stderr hash/size and historical session evidence. |
| `files[].meta`, `confirmed_offset`, `state`, optional `recorded_content_hash` | Previously persisted diagnostic collection checkpoint. States are `not_collected`, `collecting`, `recorded`, `empty`, `invalid`. Empty files have no fabricated media reference. |
| `files[].current_availability` | Always `not_rechecked`: this read does not open media objects, transfer bytes, or assert that a previously collected object is still available. |

`pi_result`, `result_summary`, `continuity`, native availability and declared-artifact delivery remain separate existing contracts. A completed operation with only an exit receipt can still have no semantic result or collected logs. Diagnostic collection is not made a prerequisite for normal business-result handling. A receipt's history evidence describes the invocation, not a fresh session-file check.

An operation revision change during native observation now returns the existing unknown error without exposing the stale reconciliation. Current ownership/grant/registration checks still run. No operation is written, task replayed, resource created/closed, or permission added by this read. **These facts do not pause target admission and are not stop/upgrade authorization.** Old clients may ignore the additive property; old servers omit it, which cannot be interpreted as a verified handoff.

## Remaining coordination contract (not implemented)

Source inspection establishes a real gap, rather than a missing native configuration:

- `internal/handlers/user_runtime.go` exposes account-owned registration, listing, revocation, discovery and Agent profile configuration. It has no admission-pause/drain operation.
- `internal/agenvo/service.go` fingerprints the complete registered Target and rechecks live mount, permission and registration for reads. Disabling the Agent profile changes that fingerprint; revocation/unmount removes access. Neither safely pauses new work while preserving collection of old work.
- `internal/computertask/service.go` claims new operations through `ComputerOperationStore`; `followup.go` reserves native prompts using the original operation CAS. No durable target admission check is shared by these boundaries.
- `db/postgres/queries/user_runtimes.sql` deduplicates exact provider configurations, not every registration pointing at the same physical Herdr instance. A Bot mount ID is not a device-wide pause key. Account-level `ListGrantsByRuntimeOwner` can enumerate mounts, but is not itself a gate or stop lease.
- `ec501` recovery-reader reservation remains unchanged. Its `creating` ambiguity cannot be reconciled by label search or by creating another reader.

The minimum next cross-end agreement must identify the existing authenticated caller that will invoke the Memoh owner before Windows preflight. A file-transfer credential, local receipt, model-supplied scope or native `idle` status cannot substitute for that identity. The supplied Windows `98da8` preflight result does not specify such an owner endpoint/authentication binding; no unverified Windows API is invented here.

With that binding agreed, the existing owner/store needs a durable admission fence covering all applicable mounts/registrations for the controlled target, atomic with new claims **and** follow-up reservations. Already admitted dispatch/collection must be accounted for before a stop decision; a list of `completed` rows is insufficient. Gate identity/revision and the operation revision set must stay current until the existing host finishes the stop/version switch. This is coordination inside the existing services, not another runtime or approval system. Persistence/schema allocation and physical-target alias coverage must be agreed before implementing that fence.

Safe call order is therefore:

1. Owner establishes the target admission fence using the existing authenticated host relationship (missing today).
2. Owner finishes bounded collection of each existing operation using the existing result/log/artifact paths as required by the agreed preservation policy. Recovery-reader creation remains the existing CAS operation, only during actual collection.
3. Owner obtains/revalidates the same operation identities, revisions, receipt/result/history/collection facts. Unfinished/unknown dispatches, stale revisions, unresolved reader creation, missing data or unverifiable ownership remain blocked; no Pi replay.
4. Existing Windows host receives a current owner-bound handoff and applies its own native checks before stop/switch (host interface still needs alignment). This read-only response alone must leave `task_handoff_unverified` in effect.

No timestamp timeout, local queue idle, process exit alone or manual override is proposed to fill the missing gate.

## Executed verification

- `go test ./internal/computertask -run 'TestRecoveryReconciles|TestRecoveryRejectsConcurrent|TestReconciliationRejects|TestReadOnlyRecovery|TestWorkspaceDiscoveryAndRecovery' -count=1 -v`: exit 0; [output](targeted.txt).
- Race variant additionally includes the existing four restart recovery cases: exit 0 after bounded cache recovery, 8.747s; [output](race-after-cleanup.txt). The first race attempt was stopped at `/tmp` pressure, exit 143; [partial output](race.txt) is preserved and is not a pass. Only that command's temporary `go-build` directory was removed: [cleanup](cache-cleanup.json).
- HTTP handler/host-binding/late-revocation regression: exit 0; [output](http-tests-final.txt). Includes exact string revision propagation and no stop-authority field.
- SDK and Web typechecks: exit 0; [SDK](sdk-types.txt), [Web](web-types.txt). Swagger and SDK were generated through the existing Go generator and `pnpm run generate-sdk`; no manual generated-code edits.
- Current Server `go build -o <temporary binary> ./cmd/agent`: exit 0; [build](build-agent.txt). An initial mistaken `./cmd/server` target failed before compiling; [failure](build-server.txt) retained.
- Actual current Server on the existing isolated loopback configuration/PG, Web at `http://127.0.0.1:18402`: existing task list → task detail → observation. Two existing synthetic PG operations expose the exact stored revision/fingerprint, missing batch receipt stays unknown; anonymous GET is 401 with `no-store`. Browser sends zero mutating API requests. [Log](browser.txt), [proof](browser-proof.json), [screenshot](task-reconciliation.png).
- All six existing synthetic operation records remained byte-identical: [persistence proof](persistence-proof.txt). No model/PC task was dispatched. The existing loopback MCP peer is unavailable, visibly reported as such; no healthy peer, real Herdr restart, Windows upgrade or stop handoff is claimed.

Screenshot inspected: original workspace/pane remain visible, native state is unknown, connection unavailable and business completion unverified. The additive owner metadata is verified in the real API response, not invented as new UI controls. Human QA is not claimed. Published source/evidence must not include private configuration, token files, fixture details or server logs.
