# Native delivery validation package

This is a review/test package, not a production installation. It contains a
finite Herdr plugin executable, manifest, and isolated Windows test executable.
It installs no service, daemon, Memoh runtime or Agent modification.

Version 0.4 writes checkpoint-aware queue record v4. Stop older writers before
updating; v2/v3 records remain readable, but an old executable cannot read a
record after its first v4 write. Active journals compact through a durable
checkpoint without changing payloads, receipt IDs or retry/unknown outcomes.
There is no automatic downgrade or repair of an already-full legacy journal.
See `docs/delivery-active-journal.md` for process-crash and compatibility bounds.

## Unified Windows endpoint: implemented and missing

The transfer helper is an internal component of the Windows execution endpoint.
The intended user experience is one installation/configuration/maintenance flow;
this bundle alone does not implement that flow. It must not be presented as a
complete endpoint installer or require the user to assemble separate services.

| Concern | Present in this component | Remaining endpoint integration |
| --- | --- | --- |
| Dependencies | The prebuilt transfer executable needs no Go/Python runtime. The plugin declares Herdr >= 0.9.5; action paths are relative to its package. Fixture acceptance uses PowerShell. | Memoh Runtime/Herdr configuration, Pi, Node and the Git Bash tools used by the existing execution envelope are external to this bundle. It neither installs nor inventories them. |
| Configuration and data | Herdr supplies config/state locations. One `HostConfig` reads `transfer.json`; the separate private credential is read on requests. Queue/publications use the supplied state root; source files keep their existing owner. | No endpoint-wide setup currently provisions these locations and the registered task `WorkDir` together. `source_root` is a separately supplied value checked against the task root hash, not an automatically shared registration setting. |
| Relative paths | Plugin executable paths are package-relative; source references are logical filenames. All roots must be clean absolute paths. Config/state remain private local NTFS; the task source can use an already authorized native SMB UNC root. See [task source boundaries](../../../docs/delivery-task-source.md). | No endpoint root relocation tool exists. The queue binds the configuration digest and refuses changes pending drain/migration; this is detection, not automatic relocation. |
| Startup checks | `verify-storage` checks/opens native queue storage. Task launch checks Pi, Node and required Bash utilities; the producer hook checks Herdr when collecting. | There is no combined startup health check for all dependencies, plugin installation, device authorization and real HTTPS trust. `probe` explicitly reports `device_configuration: required`; it does not prove connectivity. |
| Compatibility | The package records its exact source SHA and hashes. Config, locator and queue versions fail closed; Server requires the complete 171/172 schema and device verifier. | The static plugin version is not a negotiated Server/host compatibility matrix. There is no coordinated version selection or automatic upgrade migration. |
| Update/rollback/uninstall | Queue evidence, retained receipts and NAS associations survive process restart. Only durably committed queue payloads are automatically released. | No updater, atomic package switch, rollback coordinator or uninstall ownership manifest is supplied. Replacement must preserve config/state and unresolved evidence; uninstall must never imply deleting producer files or NAS originals. |

The task envelope currently checks `base64`, `cygpath`, `sha256sum`, `wc`, `mkdir`,
`mv`, `rm`, and `rmdir` in its Bash environment. Its Node projector uses built-in
modules. A successful transfer fixture does not establish that this whole
execution environment is installed, version-compatible or healthy. Adding that
coordinated setup belongs to the existing endpoint's packaging/configuration
owner; it does not require a new transfer service or Agent-core modification.

## Run the binary-only acceptance fixture

Obtain the Actions artifact from the reviewed commit. Compare its source SHA
and hashes using the trusted Actions artifact/PR; a checksum file bundled beside
binaries alone is not an authenticity signature. Extract it to a disposable local
NTFS directory. Run `./verify-fixtures.ps1` with normal user permissions. Do not
change execution policy or elevate to get a passing result; report an existing
policy or permissions failure instead. The PC needs no Go compiler or source.

The tests create synthetic files in private temporary directories and loopback
TLS servers. They do not use NAS addresses, real credentials, plugin state,
production ACLs or persistent identities. Expected tests cover actual directory
flush, ownership/DACL checks, identity/sharing, process locks and process death,
resumable admission/cancellation, TLS upload/lost response/outbox reconciliation,
and Range/hash download. This does not prove physical power-loss behavior or
validate a real PC/NAS route. Report test names, exit code, OS/filesystem and
bounded failure messages; never send originals, full production logs or secrets.

## Herdr boundary

The manifest uses Herdr v0.9.5's existing argv action mechanism. No startup worker
or timer is registered. `verify-storage` invokes `transferqueue.exe -action probe`:
it validates/initializes its queue only under `HERDR_PLUGIN_STATE_DIR`, requiring
an existing private local NTFS parent. Its `native-profile-v1` locator binds the
resolved root and legacy quota profile. New upload registration does not require
changing that profile. It never repairs ACLs, changes roots or falls back.

The executable exposes finite `enqueue-upload`, `enqueue-download`, `advance`,
`advance-next`, `reconcile`, `inspect`, `cancel`, and `retire` actions. Explicit
job actions select `-delivery <logical-id>`. The manifest's `advance-delivery`
runs `advance-results`: at most 64 existing queue steps on one due job within
two minutes, then exits. It does not poll, schedule,
reset retry budgets, create a business attempt, or run a background service.
The configured native Memoh Runtime/Herdr path can invoke the installed command through its existing command path;
no connector or Agent core change is needed. Input acceptance alone is not a
completion receipt.

## One-time connection checklist (not executed by this package)

1. Approve the Memoh HTTPS origin and its valid hostname/certificate trust. The
   helper requires HTTPS and normal certificate verification. Existing HTTP is
   not encryption and cannot be configured as a shortcut.
2. Upgrade the fully integrated Server through migrations 171/172 and approve
   enabling `files.device_transfers_enabled`. The existing runtime registry
   includes the stable credential backend and Server verifier. The runtime
   owner enrolls the already paired connection/device/instance once through
   `/users/me/runtimes/:id/transfer-credential`; only its digest is stored on
   Server. Choose expiry/revoke/rotation policy and privately provision the
   one-time returned purpose-specific device credential. Do not use a PC account JWT, broad Bot
   token or any legacy control/OAuth credential. No per-file token or pairing is
   required. Missing verifier means unavailable, never account-auth fallback.
3. After installation approval, install this bundle through the existing Herdr
   plugin mechanism, without a service. Herdr supplies `HERDR_PLUGIN_CONFIG_DIR`
   and `HERDR_PLUGIN_STATE_DIR`. Both must already be private local NTFS folders
   belonging to the invoking user. The helper validates and refuses unsuitable
   locations; it does not repair ACLs, elevate or configure trust.
4. Privately create `transfer.json` in that config directory from the packaged
   `transfer.example.json`, and a separate private `device.credential` containing
   only the approved opaque credential (optional final newline). This package
   does not create either real file. Secret files never go into the repository,
   bundle, argv, model context or terminal output. Rotation replaces that same
   private credential through the approved administrative process. Requests read
   it afresh; server revocation is checked on every request and after scans.
5. For uploads only, configure `source_root` to the producer's approved task
   directory: local NTFS or the existing authorized native SMB UNC location.
   It must not overlap config or queue state. Producers
   close output writers and supply the exact flat filename including its actual
   extension (`source_ref`, 1–64 ASCII letters, digits, underscores, hyphens and
   single separating dots), declared hash/size/MIME and existing task
   selectors. Downloads publish under the existing queue's retained native
   publication store; no arbitrary overwrite destination is exposed.

The Server and this adapter must both support explicit filename extensions.
No suffix inference or directory search occurs. An existing task's declaration,
context, delivery ID and configuration digest must not be rewritten to add an
extension. Historical mismatch recovery uses the separate explicit owner entry
described in [result-name recovery](../../../docs/delivery-result-name-recovery.md),
never automatic suffix inference or a new task.

`transfer.json` is resolved once per invocation. Its digest is recorded in the
existing queue journal. Changing the endpoint or source root (even formatting)
requires explicit drain/migration; old jobs fail `configuration_changed`, never
silently switch directories or send their credential to a different endpoint.
## Metadata-first uploads (plugin 0.9)

New upload enqueue persists the existing job identity, expected complete hash,
size and a private logical source reference plus file identity/mtime. It does
not hash/copy file bytes, reserve their aggregate size, or consume a transfer
slot. `registered: true` acknowledges this durable registration; `admitted: true`
still means the block index has been verified and transfer can proceed. This is
an additive CLI status field, not a new Server or phone wire protocol.

The existing finite `advance-results`/`advance` action performs source indexing
and upload. It reads at most 64 MiB per indexing step, resumes the persisted hash
state, and reuses the existing 1 MiB block-proof tree. It writes no second full
source copy. Transfer buffers remain at most 8 MiB, with one worker in the
installed profile. Pending metadata has no aggregate content-byte or file-count
quota. Directory reads are paged; only selected files allocate index scratch.
Actual metadata/index disk exhaustion remains an explicit capacity failure with
records retained. This does not promise unlimited disk or zero indexing cost.

Windows holds the existing validated source handle without write/delete sharing
during each finite call. Reopens verify identity, size and modification time;
block proofs verify bytes even if those attributes were preserved. Source change
or loss records `source_changed`/`source_missing`, keeps the original job/receipt
and index, and never creates a new business attempt or silently changes versions.
User sources are never deleted. NAS durable association permits release of the
queue-owned index/checkpoint and keeps the original queryable receipt.

Existing v2/v3/v4 snapshot journals remain readable, and previously sealed
snapshots keep their old source-independent semantics. New registrations use
queue record v5 in the same layout/root and unchanged locator/HostBinding.
No budget edit or `migrate-budget` is needed to enqueue large upload metadata.
Older binaries reject v5 records; a normal endpoint package update must replace
old finite writers, not run both versions. Rollback must retain the state and
use a reader supporting v5; it cannot reinterpret indexed sources as snapshots.

The historical 8-job/256 MiB reservation profile and optional
`queue-budget.json` are retained for legacy snapshot policy and unchanged
locator compatibility. They do not limit new upload registration/source size,
download size/backlog, or the cumulative bytes of retained download objects.
Server `[files].max_upload_bytes` defaults to 200 MiB independently; metadata
capacity and task-specific contracts remain separate. Consequently metadata registration is not a claim that 10,000 GiB files
have been transferred or that every three-end product constraint is solved.

The Server still requires the complete hash before Create. Closed producer
metadata already supplies that hash; active indexing verifies it in another
bounded read before networking. Upload then reads requested blocks from the
source. These necessary reads have not been disguised as zero overhead; no BEP
compatibility or embedded Syncthing engine is claimed by this increment.

The Linux scale regression uses 10,000 distinct sparse 1 GiB source files for
metadata only. Smaller cross-platform fixtures cover source changes, cancellation,
worker exclusion, process exit before registration acknowledgement, original-ID
replay, and lost PATCH response without a snapshot. The existing real Server/PG/
CLI process recovery tests retain their hash/receipt/Commit assertions. None of
these tests establishes physical power-loss durability or real PC/NAS bandwidth.

## Producer and finite-command handoff

Enqueue reads **metadata only** from stdin, at most 16 KiB within the two-minute
command budget. The trusted local producer supplies `job` (version 1), including
`delivery_id`, `snapshot_id` (same ID), `bot_id`, `operation_id`,
`locator_revision: native-profile-v1`, `phase: queued`, `spec` and `device_task`.
The latter contains `user_id`, `session_id`, `target_id`, `run_id`, `attempt` and
`generation` from the existing operation handoff. Add `source_ref` for uploads.
Do not put a credential or file bytes in this JSON. These IDs are lookup
selectors, not authorization; the Server verifies them against its current
operation, Bot permissions and authenticated device.

Replaying enqueue uses the original metadata and identity. The collector consumes
a closed manifest only after durable registration; indexing can then resume via
`advance-results` without resubmitting the manifest. Pending indexing remains
runnable in lifecycle summary. Old incomplete snapshot admissions retain their
existing same-input recovery path. Each call performs bounded work then exits;
the existing host owns when to invoke again. Backoff and unknown outcomes persist in the
queue. `cancel` records intent; subsequent advance reconciles publication/remote
commit and can report that delivery already won. `reconcile` is an explicit
recovery action, not a retry-budget reset. The original create-unknown protocol
fence remains; never guess a new receipt after uncertain creation.

## Bounded task producer and metadata pages

The optional task declaration `artifact: {source_ref, mime}`, or `artifacts` with
1–16 distinct entries in the existing declaration (never both), is available for
`herdr_pi_task` / `pi-json-v1` at `work_dir: "."`. The registered task root must
match the existing authorized `source_root`; a mismatch fails explicitly. Write and
close the declared files before Pi exits. The envelope supplies non-secret
`MEMOH_DELIVERY_CONTEXT_FILE`, closes a metadata manifest, and invokes the fixed
`collect-result` action. This does not change Agent code or its prompt.

`enqueue-result` accepts only bounded closed metadata on stdin. The official
Herdr CLI does not forward arbitrary stdin: its fixed `collect-result` action
opens the manifest through the configured root and feeds that same decoder.
After durable registration the original source version/receipt are reused; repeated task
result queries can wake finite advancement through the dedicated control pane.
No host timer or daemon is added. NAS `delivery.reference` is available to the
parent task only after committed association, separately from Pi completion.

The component CLI also accepts `-action enqueue-results` with a version 1
`ClosedPage` on stdin: 1–16 existing version 1 closed-file records, all bound to
the same original Task. Sixteen bounds one message, not the task's total file
count. Submit subsequent pages through the same finite component; never put a
whole backlog into argv, one JSON ledger or the model conversation. Per-file
`registered` acknowledgements are durable registration, not delivery success.
A partial response has exit code 1; independent valid files still register.
Replay the same page after interruption or lost output; do not invent an attempt
or change delivery IDs. No full source copy or network call occurs in this action.
The unified endpoint consumes this internal interface; it is not a manual user
workflow. Herdr's existing action API still cannot forward arbitrary stdin, and
this change does not claim that a native plugin RPC can do so.

The original Pi `artifact`/`artifacts` declaration and its bounded aggregate
remain compatible and cover that declared page only. Automatic generation and
submission of arbitrarily many Pi result pages is not added here. The authorized
`computer_task_get` query accepts `delivery_after` (empty string for page 1,
then `next_after`) to read the same NAS ledger, at most 16 entries per response.
Use existing exact Commit/Resolve for individual in-flight status. `has_more`
means more currently stored rows, not that all producer output has arrived.
See [the page contract and migration evidence](../../../docs/testing/transfer-ledger-pages.md).

Each file retains one stable delivery/closed manifest. New upload metadata does
not consume the legacy eight-job/256 MiB snapshot reservation. Finite host actions
release only confirmed committed queue scratch; unknown, canceled and blocked
evidence and user sources remain intact. Existing terminal history is still
queryable and has its separately documented storage bounds.
See `docs/delivery-terminal-history.md` for budgets, recovery and rollback bounds.
Each finite hook still handles at most one manifest; later task queries/host
actions continue remaining work. Multi-file aggregate `partial` preserves each
committed reference; it does not mean all files delivered. Recovery from a crash
before a closed manifest exists is still pending/unknown and never reruns Pi. A pending delivery is not a
successful file transfer, and task cancellation does not retract delivery.

CLI stdout contains bounded phase/offset/association/reference data only. It is
not execution success and does not contain source locations, credentials, file
bytes or full logs. Producer wiring is implemented and cloud tested. Actual installed Herdr/PC
ownership, HTTPS trust and the real PC/NAS route still require acceptance.
Windows producer tests use a protocol fixture; Linux upload Server/PG and
Windows download publication/PG tests are separate combinations.

Starting with component 0.10.1, a durable inbound download returns
`feedback_status: not_applicable`: it has no outbound producer projection.
`recorded` and `unavailable` retain their outgoing feedback meanings, including
strict original attempt/declaration checks and actual write failures. The unified
endpoint must explicitly accept the new value without treating it as transfer
success or weakening exit/lifecycle/association checks. Do not reinterpret an old
component's `unavailable` as success. No queue/config migration is required; see
[the exact consumer contract](../../../docs/delivery-producer-feedback.md#cli-feedback-applicability).

Do not install/link or configure this package against production as part of
fixture acceptance. The API fault model and limitations are documented in
`docs/design/windows-native-binding.md` and `docs/design/device-delivery-binding.md`.
CI success is not real UI or human QA. The PR remains draft pending those checks.

## Lifecycle consumer handoff (0.5.0)

The finite `lifecycle-summary` action and `advance-results` expose the exact
`memoh.delivery.lifecycle.v1` decision fields documented in
[delivery-lifecycle-summary](../../../docs/delivery-lifecycle-summary.md).
The unified lifecycle owner is `particleflow/windows-endpoint`; this plugin does
not start a daemon or implement endpoint timers. The summary covers admitted
queue state, not unclosed/unadmitted producer outputs.

The review bundle now has eight members including `SHA256SUMS.json`: it adds
`transferqueue-cli.test.exe` beside the existing component test binary. Its
`TestCLIPathTLSRestartAndLostResponses` runs the real CLI dispatch/stdin/JSON in
fresh processes against a small isolated HTTPS NAS protocol fixture. Trust is
injected only by test code; production accepts no TLS bypass flag. The test does
not establish production Server/PG acceptance or physical power-loss behavior.

The manifest pins every payload hash and source commit. GitHub Actions artifacts
remain expiring review artifacts, not a durable installation/update channel.
Do not silently substitute a new archive for an older pinned consumer hash or
change extraction allowlists without a versioned handoff. Durable distribution
must be reviewed separately; no release or production installation occurs here.

## Terminal history maintenance (0.6.0)

The fixed `migrate-history` action uses `HERDR_PLUGIN_STATE_DIR` and the existing
`native-profile-v1` limits. No network credential or source file is required.
It changes only queue locator v2 to v3 after saving and syncing the exact old
locator; it then archives at most 16 confirmed released records. Repeated
maintenance and normal finite advancement drain the remaining eligible records.
Success outputs `{"queue_layout":3,"history":"enabled"}`; it does not claim all
old records have moved. Failure uses the existing lifecycle classification.

Stop **all** old CLI/plugin writers and take a full raw state backup first.
Migration checks active worker locks but cannot revoke idle handles in an old
process. Normal v3 opens reject v2, and v2 binaries reject the new locator. Do not
edit the version back or run mixed writers. Existing roots/configuration and NAS
schema 171/172 are unchanged. This component action is not a host updater.

History holds the complete original terminal record (task/attempt/reference,
receipt and seal), supports original-ID inspect/admission without producer
bytes, and never deletes unknown evidence. It has 256 deterministic buckets,
each capped at 1 MiB framed metadata and 1024 files (filesystem overhead extra).
Bucket exhaustion fails with capacity and preserves the source journal. There
is no automatic history deletion. Plugin 0.7 added explicit publication metadata
migration described below. Current downloads retain the same metadata layout,
with actual filesystem space governing payload writes.

## Publication metadata maintenance (0.7.0)

`migrate-publications` upgrades the existing private publication locator v1 through
closed maintenance state v0 to v2. Stop all old writers and save a complete raw
state backup first. Queue locator v3 is required; use the separate history action
first when upgrading a v2 queue. Ordinary publication opens reject old or pending
layouts. No installer, credential or NAS call is added by this action.

The action returns `{"publication_layout":0,"migration":"pending"}` while more
finite maintenance is required, and `{"publication_layout":2,"migration":"complete"}`
when the metadata layout is ready. Each call copies or removes at most 16 old
journal paths, after synchronizing the destination first. Final files, their
identity and original published/fenced records remain retained. Do not infer a
successful download from the maintenance result.

Locator v2's explicit defaults are 64 buckets of at most 128 records, each journal
at most 64 KiB (512 MiB maximum journal metadata plus filesystem/cache overhead).
A full bucket returns capacity and never evicts a proof. Queue Retire does not
delete retained objects. Current writers count their bytes for integrity but do
not impose the old cumulative 256 MiB payload quota. See
`docs/delivery-publication-metadata.md` for historical layout/crash boundaries
and `docs/testing/transfer-native-large-byte-recovery.md` for the default-download
correction. Ownership-based cleanup remains unimplemented. This component does
not perform endpoint installation or upgrades.

## Default download space handling

Download enqueue writes metadata and an empty private part; it neither reserves
the whole file nor allocates a second full copy. Existing workers stream with
64 KiB buffers, checking actual free space in the existing checkpoint window.
Insufficient payload space retains the synced offset and reports the existing
summary fields `state: waiting`, `error_class: capacity`, and `next_due_at`. The
finite host action can retry when due without exhausting network attempts. It
does not delete source, unfinished data, unknown evidence, or published objects.
Actual metadata exhaustion still fails explicitly; physical disk is not unlimited.

No budget change or migration is required for this correction. Default locator
bytes, origin/HostBinding, task identity and publication layout stay unchanged.
Update all component writers before downloading beyond an old byte allowance;
older binaries still reject such records/retained objects. Do not mix old/new
writers or delete state to force a downgrade. Component replacement belongs to
the unified endpoint, not an extra per-file user action.
