# Native contact continuation workspace restoration

A native Web conversation with persisted `workspace_target_id=native` could execute its first explicit turn, but a queued user message and later result-only turns lost that workspace selection. The ordinary resolver then selected the Bot primary mount. If that unrelated mount was offline, the native turn failed before model execution with `remote runtime is offline`.

Product commit: `766a99b1c5a331f595e864554be82d90e2d270ea`, directly based on server `046023913592b00951d7f28fe54842080c5cdc51`. Cherry-pick this product/test commit; the documentation commit is optional. Web task-tab fix 2c0e277 remains separate and has no file overlap. No migration, official Agent-core change, service, scheduler configuration, model selection or runtime substitution.

## Cause and minimal repair

- `apps/web/src/pages/home/components/chat-pane.vue:4274` sends plain text entered while the supervisor is streaming to the existing follow-up queue (line4304). It does not start a second concurrent run in the same supervisor session.
- `internal/agent/application/session_queue.go:64` reconstructs queued text with its authenticated sender/Bot/session, but without workspace or channel. Result commands in `cmd/internal/core/bot_delegation_route.go:75` likewise contain no workspace selection.
- In 046, `cmd/internal/core/bot_delegation.go:514` returned immediately for a root session without incoming delegation. Source contact placement was not restored. `service_workspace_target.go:32` then called the ordinary resolver; `internal/workspace/manager.go:325` chose the Bot primary when the context/request lacked a target. `internal/workspace/remote.go:414` can produce the observed error. `runtime_type=native` does not encode this separate workspace selection.
- The repaired host preparation restores only an explicitly persisted native placement on an owned native Web root chat. It reuses current owner/team/chat authority and strict metadata validation. Channel-less text queues must resolve to an existing Web route. Workdir bindings, explicit request targets, remote placements and other runtime/channel types keep their original paths. Missing metadata never invents native placement. Conflicting metadata and revoked authority fail closed.

## Differential evidence

`unit-red.log` demonstrates fresh contexts losing saved native placement to an offline primary. Final targeted Go tests:6 top-level groups/41 subtests, no skips. `go vet ./cmd/internal/core`, gofmt, diff check and the changed backend build passed. golangci-lint is not installed in this environment; it was not claimed as run. The local backend used `-buildvcs=false` because automatic worktree VCS stamping failed; exact source commit/tree and binary SHA are recorded separately.

Actual Chromium/Web and PostgreSQL at `http://127.0.0.1:28111/bot/local-dialog`, API28090, schema181 clone `memoh_contact_runtime_20261030`; all model responses from a loopback scripted server at28120. No production or paid calls. Zero enabled schedules. The red backend reused37e: the host preparation, application workspace/queue continuation and workspace manager/remote source paths are byte-identical to046. Final green backend was built from766a99b1c. Web source was046; no frontend changes/build were needed for this backend repair.

1. Red: explicit-native supervisor dispatches one specialist; actual UI Enter queues B while A remains active. After A ends, B fails with runtime_run_failed; the server diagnostic matched B's run ID and remote-offline cause. Specialist finishes once. Three natural result attempts fail; event reaches attention/attempt3 with zero assistant publication receipts.
2. Final green: identical controlled ordering. UI queues B; releasing only the supervisor barrier lets B finish while the specialist is still running. Then releasing the specialist produces one reported result with one assistant receipt at attempt1. Provider counters:delegate1/worker1/reply_B1/summary1. The receipt-bearing hidden input is distinct from the single assistant publication; counting all roles would incorrectly report2.
3. Final process restart: same saved publication/message/run IDs, no new loopback model calls, old red attention unchanged, no machine result envelope rendered in the contact.

[Reply while specialist is held](final-green-parallel.png) and [saved report after final process restart](final-green-reported-restart.png) were visually inspected. The screenshots include earlier synthetic red-case history retained in the same isolated contact, not duplicated execution of the final green task.

Harness development initially assumed task titles contained the message and raw WS busy rejection automatically queued; those incomplete local probes were retained and allowed to settle, not treated as passes. The final red case used the actual UI queue. Its last readback corrected a JSON-column and diagnostic-field assumption without re-executing that task. Final green and restart scripts completed with exit0. Private config/auth and raw backend logs are excluded. Scripts require the existing private local fixture harness, not distributed credentials.

## Serial queue boundary and field follow-up

`delegate_to_bot` dispatches asynchronously (`internal/botdelegation/service.go:173`); it does not wait for the specialist result. A second input queues while the supervisor's own run remains active, and `queue_continuation.go` consumes it at that run's terminal boundary. This patch repairs the subsequent runtime preparation; it does not force a supervisor model/tool step to terminate or make two supervisor runs concurrent. The controlled barrier proves chat can resume while the independently running specialist remains active. It does not prove the production model will finish its initial supervisor turn within that same window.

The remaining minimal read-only field evidence is the original supervisor run's created_at/owner_since/finish_proposed_at/updated_at plus existing post-delegation tool/model-step start/end timestamps, and B's queue enqueue time/enqueued_during_run_id. Extract only names/times/IDs, not secrets or full prompts. This will distinguish supervisor model/tool time (including the reported create_memory) from a queue-consumption delay. No new task, paid request, pause or scheduler mutation is required.

Existing attention/attempt3 records are deliberately not rearmed, deleted or replayed. The current RetryDelivery path calls reconcileResult, whose existing attempt budget stops at3; simply deploying this fix or invoking that retry cannot be claimed to recover the old missing report. Controlled explicit reporting recovery is a separate remaining gap; the original specialist must not be dispatched again. Production paid retry and field operations remain with the unique field writer/coordinator, not this owner.

- [ ] Human QA passed
