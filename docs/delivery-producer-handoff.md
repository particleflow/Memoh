# Single-file producer handoff (candidate, disabled until setup)

This increment connects an explicitly declared Pi JSON result to the existing
file queue. It does not install Herdr, change Agent or Agenvo, issue per-file
credentials, enable transfers, or start a service. Device enrollment and HTTPS
trust remain the separately approved setup in [device-transfer-setup.md](device-transfer-setup.md).

## Declaration and ownership

`computer_task_submit` accepts optional `artifact: {source_ref, mime}` for
`herdr_pi_task` using `pi-json-v1`, with `work_dir: "."`. This first increment
supports one nonempty file, not a directory or automatic import of logs. The
source reference is a flat logical filename (letters, digits, underscore,
hyphen; Windows device names are rejected). The task must write that declared
file and close all writers before it exits. A pre-existing file prevents Pi from
starting; it is not reused as this task's result.

The operation is allocated before dispatch. Its existing `BatchProgress` stores
versioned, non-secret producer metadata: original operation, Bot, user, session,
target, run, attempt and generation. The deterministic delivery ID uses the
original operation/attempt/generation/source reference, never a new retry ID.
The metadata is not authority; the Server verifies the configured reusable
device credential and rechecks the operation and current grants on requests.

The existing registered execution root and private `transfer.json` `source_root`
must denote the same Windows location. A normalized location digest detects
configuration mismatch; it cannot resolve paths or authorize access. There is no
additional root setting or fallback. Existing attempt paths hold the context
file; existing source storage holds the explicitly closed manifest; existing
Herdr state storage owns snapshots/journals. Credentials remain in the separate
private Herdr config directory. No production directories or ACLs are changed.

## Finite host actions and recovery

The existing execution envelope exposes `MEMOH_DELIVERY_CONTEXT_FILE` to the
process without modifying the prompt or Agent. After Pi exits, it streams
SHA-256/size for the declared file and closes a bounded JSON manifest. It invokes
`herdr plugin action invoke collect-result --plugin memoh.delivery`; hook output
stays in the existing attempt directory. No file bytes, credential or raw
transport diagnostics enter argv, terminal receipts or model messages.

The official CLI invocation has no arbitrary stdin forwarding. The plugin's
fixed `collect-result` command opens the closed manifest through the configured
private source root and passes its metadata reader to the same bounded decoder
as `transferqueue -action enqueue-result` on stdin. Source bytes are read only
by the existing guarded snapshot admission. The collector consumes a manifest
only after durable admission; duplicate manifests recover the original sealed
job. Source changes after admission cannot replace that snapshot. If the source
root is later unavailable, collect still attempts bounded advancement of the
already sealed queue. It reports `source_collection: unavailable` with nonzero
exit even when a queued delivery advances; no queued work does not turn a missing
source into an idle or successful collection.

Each collect handles at most one manifest and at most 64 existing queue steps
within two minutes. `advance-results` selects one due job with persisted backoff.
After a verified execution receipt, `computer_task_result` may wake this same
finite action through the verified dedicated control pane, including after the
business result is complete. It never types into Pi or resubmits the task.
`pane.run` acceptance is only a wakeup, not delivery evidence. Queue locks and
original receipt reconciliation handle overlapping or lost wakeups. There is no
new daemon or timer; pending work needs a later task query or host action.

`Result.delivery` is independent of `pi_result`: only an upload entry committed
in the original operation's NAS ledger yields `state: delivered` and `reference`.
Pending validation, ready uploads, a local receipt, or plugin acceptance cannot
yield a delivered reference. A persisted operation cancellation fences every
uncommitted declared upload in the same transaction/revision CAS as reservation
and final scan commit, including a delayed exit hook. A committed winner stays
queryable; cancellation does not retract it. Completed execution can still
request cancellation of a pending producer delivery. The client receives a
permanent denial for the fenced upload; existing local cancel/retire actions
remain responsible for snapshot cleanup. Bytes already in flight may remain in
NAS staging/retention, but cannot become a committed producer reference. Reference reads retain existing task authorization
and target-fingerprint checks. NAS media retention/pinning is unchanged.

## Explicit limits and remaining integration

- This is a single-file opt-in milestone. Multi-file declarations, automatic
  retention/retirement of acknowledged local jobs and automatic admission
  recovery before a closed manifest exists remain follow-up work. Existing
  `cancel`/`retire` commands retain their reviewed semantics; this patch does not
  claim that `computer_task_cancel` retracts already delivered files.
- A process crash before the closed manifest is written can leave an execution
  receipt without a delivery. That remains pending; the system does not infer a
  result or rerun Pi. Once admitted, the existing recoverable sealing, source
  identity/hash checks, bounded retries, cancellation and association-unknown
  protocol apply. No physical power-loss guarantee is added for the shell's
  pre-admission manifest.
- Current installed-command limits remain eight jobs / 256 MiB total snapshots,
  with bounded journals and one worker. Capacity keeps the manifest and allows
  existing work to advance. Explicit retirement is currently required to free
  acknowledged entries. No throughput or unlimited-file claim is made.
- Source enumeration is capped at 4096 entries; malformed manifests fail
  explicitly. Manifests are metadata, not a second task registry. Old binaries
  cannot safely edit operations containing producer metadata; do not mix old
  and new operation writers when enabling this feature. Existing migrations
  171/172 and their rollback restrictions still apply; this increment adds no
  migration or production configuration.
- Native Windows queue tests use an HTTPS protocol fixture. Linux integration
  uses the actual Server, device registry, restricted PostgreSQL role and NAS
  handlers. These are separate combinations, not proof of a real Windows PC
  talking to the production NAS. Herdr installation, actual Windows shell file
  ownership, HTTPS certificate trust, human/UI acceptance and production enable
  remain unperformed.

Official interface evidence: [Herdr CLI binding](https://github.com/herdrdev/herdr/blob/5bd900d88c80d95f5b145397e45bbeea398b8165/src/cli/plugin/bindings.rs)
and [native plugin invocation](https://github.com/herdrdev/herdr/blob/5bd900d88c80d95f5b145397e45bbeea398b8165/src/app/api/plugins/mod.rs).
The invocation merges host context and starts the configured command; this
adapter uses neither context as task authority nor a fabricated control proof.
