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
| Dependencies | The prebuilt transfer executable needs no Go/Python runtime. The plugin declares Herdr >= 0.9.5; action paths are relative to its package. Fixture acceptance uses PowerShell. | Herdr/Agenvo pairing, Pi, Node and the Git Bash tools used by the existing execution envelope are external to this bundle. It neither installs nor inventories them. |
| Configuration and data | Herdr supplies config/state locations. One `HostConfig` reads `transfer.json`; the separate private credential is read on requests. Queue/publications use the supplied state root; source files keep their existing owner. | No endpoint-wide setup currently provisions these locations and the registered task `WorkDir` together. `source_root` is a separately supplied value checked against the task root hash, not an automatically shared registration setting. |
| Relative paths | Plugin executable paths are package-relative; source references are logical filenames. Config/state/source roots must be clean absolute private paths, never CWD-relative or silently inferred. | No endpoint root relocation tool exists. The queue binds the configuration digest and refuses changes pending drain/migration; this is detection, not automatic relocation. |
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
an existing private local NTFS parent. Its fixed `native-profile-v1` quota profile
is immutable in the locator. It never repairs ACLs, changes roots or falls back.

The executable exposes finite `enqueue-upload`, `enqueue-download`, `advance`,
`advance-next`, `reconcile`, `inspect`, `cancel`, and `retire` actions. Explicit
job actions select `-delivery <logical-id>`. The manifest's `advance-delivery`
runs `advance-results`: at most 64 existing queue steps on one due job within
two minutes, then exits. It does not poll, schedule,
reset retry budgets, create a business attempt, or run a background service.
Herdr/Agenvo can invoke the installed command through its existing command path;
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
   token or the Agenvo control/OAuth credential. No per-file token or pairing is
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
5. For uploads only, configure `source_root` to the producer's approved private
   NTFS export directory. It must not overlap config or queue state. Producers
   close output writers and supply one logical filename (`source_ref`, letters,
   digits, underscore or hyphen), declared hash/size/MIME and existing task
   selectors. Downloads publish under the existing queue's retained native
   publication store; no arbitrary overwrite destination is exposed.

`transfer.json` is resolved once per invocation. Its digest is recorded in the
existing queue journal. Changing the endpoint or source root (even formatting)
requires explicit drain/migration; old jobs fail `configuration_changed`, never
silently switch directories or send their credential to a different endpoint.
The queue's `native-profile-v1` limits remain 8 jobs, 256 MiB reserved bytes,
8 MiB per journal and one worker. This finite profile is not a claimed maximum
protocol file size; changing quotas requires a reviewed locator migration.

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

Reinvoke the same enqueue/ID/metadata after admission timeout to resume sealing;
do not invent a new ID. Completed admission uses its immutable snapshot even if
the producer file or export directory has since been removed. New or incomplete
upload admission still requires the original authorized source. Once admitted,
invoke `advance` or the fixed manifest
`advance-delivery` action. Each call performs bounded work then exits; the existing
host owns when to invoke again. Backoff and unknown outcomes persist in the
queue. `cancel` records intent; subsequent advance reconciles publication/remote
commit and can report that delivery already won. `reconcile` is an explicit
recovery action, not a retry-budget reset. The original create-unknown protocol
fence remains; never guess a new receipt after uncertain creation.

## Bounded task producer

The optional task declaration `artifact: {source_ref, mime}`, or `artifacts` with
1–16 distinct entries (never both), is available for
`herdr_pi_task` / `pi-json-v1` at `work_dir: "."`. The registered task root must
match the existing private `source_root`; a mismatch fails explicitly. Write and
close the declared files before Pi exits. The envelope supplies non-secret
`MEMOH_DELIVERY_CONTEXT_FILE`, closes a metadata manifest, and invokes the fixed
`collect-result` action. This does not change Agent code or its prompt.

`enqueue-result` accepts only bounded closed metadata on stdin. The official
Herdr CLI does not forward arbitrary stdin: its fixed `collect-result` action
opens the manifest through the configured root and feeds that same decoder.
After durable admission the original snapshot/receipt are reused; repeated task
result queries can wake finite advancement through the dedicated control pane.
No host timer or daemon is added. NAS `delivery.reference` is available to the
parent task only after committed association, separately from Pi completion.

The bounded list retains one stable delivery/closed manifest per file, within
the existing eight-job /
256 MiB total snapshot limit. Finite host actions automatically release only
confirmed committed queue payloads and retain the original delivery/receipt.
Unknown, canceled and blocked jobs are not automatically cleared. The compact
receipt catalog stops new admissions at 1024 records; proofs are never silently
evicted. See `docs/delivery-queue-retention.md` for recovery and upgrade bounds.
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
